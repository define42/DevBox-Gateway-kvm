package virtio

import (
	"errors"
	"image"
	"testing"
	"time"

	"github.com/define42/gokvm/internal/rdp"
)

func TestRDPEncoderSliceLimits(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                     string
		cpus, guest, limit, want int
	}{
		{"automatic", 8, 2, 0, 1},
		{"one slice", 8, 2, 1, 1},
		{"explicit", 8, 2, 4, 4},
		{"host and guest reservation", 8, 2, 16, 5},
		{"small host", 1, 8, 4, 1},
		{"automatic on small host", 2, 1, 0, 1},
		{"default guest reservation", 8, 0, 16, 6},
		{"maximum", 32, 1, 16, 16},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := rdpEncoderSlices(tt.cpus, tt.guest, tt.limit); got != tt.want {
				t.Fatalf("slice count: got %d, want %d", got, tt.want)
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
	var latest vncFrame
	display.copyFrameChanges(&latest, false)
	if latest.pix[0] != 3 {
		t.Fatal("paced writer lost the latest guest publication")
	}
}

func TestRDPSliceConfigValidation(t *testing.T) {
	t.Parallel()
	for _, options := range []RDPConfig{
		{H264Threads: -1}, {H264Threads: 17, H264: true}, {H264Threads: 1},
	} {
		if _, err := NewRDPDisplayWithConfig("invalid-address", options); !errors.Is(err, errRDPThreads) {
			t.Fatalf("invalid slice options reached listener creation: %v", err)
		}
	}
}

func TestRDPGraphicsBitmapFallback(t *testing.T) {
	t.Parallel()
	writer := &rdpFrameWriter{session: &rdp.Session{}, graphics: true}
	if !writer.updateGraphics(nil) || writer.graphics {
		t.Fatal("closed graphics channel did not restore bitmap output")
	}
}
