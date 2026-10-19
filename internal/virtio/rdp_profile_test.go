package virtio

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"testing"
	"testing/synctest"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
)

func TestRDPDisplayProfiles(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, profile string
		fps           int
	}{
		{name: "default", fps: 60},
		{name: "office", profile: rdp.ProfileOffice, fps: 30},
		{name: "smooth", profile: rdp.ProfileSmooth, fps: 60},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			d := newRDPTestDisplayWithConfig(t, RDPConfig{H264: true, Profile: test.profile})
			if d.frameRate != test.fps || d.linearInterval != time.Second/time.Duration(test.fps) {
				t.Fatalf("profile %q: rate=%d capture=%v", test.profile, d.frameRate, d.linearInterval)
			}
		})
	}
	if _, err := NewRDPDisplayWithConfig(RDPConfig{Profile: "fast"}); !errors.Is(err, rdp.ErrProfile) {
		t.Fatalf("invalid profile reached listener creation: %v", err)
	}
}

func TestRDPProfileFrameIntervals(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		fps      int
		graphics bool
		want     time.Duration
	}{
		{name: "default AVC", graphics: true, want: time.Second / 60},
		{name: "office AVC", fps: 30, graphics: true, want: time.Second / 30},
		{name: "smooth AVC", fps: 60, graphics: true, want: time.Second / 60},
		{name: "office bitmap", fps: 30, want: time.Second / 30},
		{name: "smooth bitmap", fps: 60, want: time.Second / 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			writer := &rdpFrameWriter{frameRate: test.fps, graphics: test.graphics, session: &rdp.Session{}}
			if got := writer.frameInterval(); got != test.want {
				t.Fatalf("interval = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRDPOfficePacingCoalescesDamageBeforeCapture(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		display := newFramebuffer()
		defer display.shutdown()
		img := image.NewRGBA(image.Rect(0, 0, 128, 128))
		if err := display.flushDamage(128, 128, img, nil); err != nil {
			t.Fatal(err)
		}
		writer := &rdpFrameWriter{graphics: true, frameRate: 30, lastFrame: time.Now()}
		display.copyFrameChanges(&writer.frame, false)
		previous := bytes.Clone(writer.frame.pix)
		timer := time.NewTimer(time.Hour)
		timer.Stop()
		defer timer.Stop()

		for i, point := range []image.Point{{X: 2, Y: 3}, {X: 100, Y: 100}} {
			time.Sleep(5 * time.Millisecond)
			img.SetRGBA(point.X, point.Y, color.RGBA{R: byte(i + 1), A: 255})
			rect := image.Rectangle{Min: point, Max: point.Add(image.Pt(1, 1))}
			if err := display.flushDamage(128, 128, img, []image.Rectangle{rect}); err != nil {
				t.Fatal(err)
			}
			// Refresh requests are paced too; otherwise they could defeat the cap.
			writer.force = true
			if paced, err := writer.writeWhenReady(display, timer); err != nil || paced != timer.C {
				t.Fatalf("frame was not deferred before capture: %v, %v", paced, err)
			}
			if !bytes.Equal(writer.frame.pix, previous) || writer.avc != nil {
				t.Fatal("paced frame performed copying or encoder work")
			}
		}
		<-timer.C
		if got := time.Since(writer.lastFrame); got != time.Second/30 {
			t.Fatalf("pacing deadline = %v, want office interval", got)
		}
		if damage := display.copyFrameChanges(&writer.frame, false); len(damage) != 2 {
			t.Fatalf("skipped updates did not retain both damaged tiles: %v", damage)
		}
		if !bytes.Equal(writer.frame.pix, img.Pix) {
			t.Fatal("coalescing lost an intermediate or latest update")
		}
	})
}

// A transport-less Session rejects encoded output after codec work. That allows
// exercising real capture/encode pacing without a network clock or a test hook
// in production. Pending refreshes remain paced even when output cannot be sent.
func TestRDPProfilesBoundCaptureAndEncoding(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{rdp.ProfileOffice, rdp.ProfileSmooth} {
		t.Run(profile, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				fps, err := rdp.ProfileFrameRate(profile)
				if err != nil {
					t.Fatal(err)
				}
				display := newFramebuffer()
				defer display.shutdown()
				img := image.NewRGBA(image.Rect(0, 0, 16, 16))
				if err := display.flush(16, 16, img); err != nil {
					t.Fatal(err)
				}
				writer := &rdpFrameWriter{
					session:  &rdp.Session{Width: 16, Height: 16},
					graphics: true, frameRate: fps, force: true,
				}
				defer writer.close()
				timer := time.NewTimer(time.Hour)
				timer.Stop()
				defer timer.Stop()
				if _, err := writer.writeWhenReady(display, timer); err != nil {
					t.Fatal(err)
				}
				if writer.avc.FrameRate() != fps {
					t.Fatal("writer did not pass the profile rate to its encoder")
				}
				updates := time.NewTicker(time.Second / 120)
				defer updates.Stop()
				end := time.NewTimer(time.Second)
				defer end.Stop()
				captures := 0
				for {
					before := writer.lastFrame
					paced, err := writer.writeWhenReady(display, timer)
					if err != nil {
						t.Fatal(err)
					}
					if writer.lastFrame != before {
						captures++
					}
					select {
					case <-end.C:
						if captures != fps {
							t.Fatalf("captured/encoded %d frames in one second after initialization, want %d", captures, fps)
						}

						return
					case <-updates.C:
						img.Pix[0]++
						if err := display.flush(16, 16, img); err != nil {
							t.Fatal(err)
						}
					case <-paced:
					}
					timer.Stop()
				}
			})
		})
	}
}

func TestRDPResizeRetainsOfficeProfile(t *testing.T) {
	t.Parallel()
	writer := &rdpFrameWriter{
		session: &rdp.Session{Width: 128, Height: 128, BitsPerPixel: 32},
		width:   64, height: 64, graphics: true, frameRate: 30,
	}
	defer writer.close()
	if err := writer.updateSize(); err != nil {
		t.Fatal(err)
	}
	if got := writer.frameInterval(); got != time.Second/30 {
		t.Fatalf("resize changed profile interval: %v", got)
	}
	// Bitmap fallback must not raise the office ceiling.
	if !writer.updateGraphics(nil) || writer.frameInterval() != time.Second/30 {
		t.Fatal("bitmap fallback changed the office frame rate")
	}
}
