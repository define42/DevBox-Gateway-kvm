package virtio

import (
	"image"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
)

func TestRDPEncoderUsesFourSlices(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                string
		width, height, want int
	}{
		{name: "desktop", width: 1280, height: 720, want: 4},
		{name: "small desktop", width: 320, height: 240, want: 4},
		{name: "single macroblock", width: 16, height: 16, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			writer := &rdpFrameWriter{session: &rdp.Session{Width: test.width, Height: test.height}}
			t.Cleanup(writer.close)
			if _, err := writer.ensureAVC(); err != nil {
				t.Fatal(err)
			}
			if got := writer.actualSlices(); got != test.want {
				t.Fatalf("effective slices = %d, want %d", got, test.want)
			}
			data, err := writer.avc.Encode(image.NewRGBA(image.Rect(0, 0, test.width, test.height)), true)
			if err != nil {
				t.Fatal(err)
			}
			// Count actual IDR slices in the Annex-B stream, including either
			// three- or four-byte start codes. Escaped payloads cannot match.
			slices := 0
			for i := 0; i+3 < len(data); i++ {
				if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 && data[i+3]&31 == 5 {
					slices++
				}
			}
			if slices != test.want {
				t.Fatalf("encoded IDR slices = %d, want %d", slices, test.want)
			}
		})
	}
}

func TestRDPFramePacingKeepsLatestFrame(t *testing.T) {
	t.Parallel()
	display := newFramebuffer()
	defer display.shutdown()
	writer := &rdpFrameWriter{force: true, nextFrame: time.Now().Add(time.Hour)}
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for i := range 3 {
		img := image.NewRGBA(image.Rect(0, 0, 16, 16))
		img.Pix[0] = byte(i + 1)
		if err := display.flush(16, 16, img); err != nil {
			t.Fatal(err)
		}
		if paced, err := writer.writeWhenReady(display, timer); err != nil || paced != timer.C {
			t.Fatalf("pacing should wait on the frame timer: paced=%v err=%v", paced, err)
		}
		if len(writer.frame.pix) != 0 {
			t.Fatal("paced writer captured an obsolete image")
		}
	}
	var latest displayFrame
	display.copyFrameChanges(&latest, false)
	if latest.pix[0] != 3 {
		t.Fatal("paced writer lost the latest guest publication")
	}
}

func TestRDPGraphicsBitmapFallback(t *testing.T) {
	t.Parallel()
	writer := &rdpFrameWriter{session: &rdp.Session{}, graphics: true}
	if !writer.updateGraphics(nil) || writer.graphics {
		t.Fatal("closed graphics channel did not restore bitmap output")
	}
}
