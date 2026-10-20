package virtio

import (
	"errors"
	"image"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/avc"
)

func TestRDPH264QPConfigValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		qp   int
		h264 bool
	}{
		{name: "negative", qp: -1, h264: true},
		{name: "excessive", qp: 52, h264: true},
		{name: "minimum without H264", qp: 0},
		{name: "default without H264", qp: 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewRDPDisplayWithConfig(RDPConfig{H264: test.h264, H264QP: &test.qp})
			if !errors.Is(err, errRDPQP) {
				t.Fatalf("invalid QP options reached listener creation: %v", err)
			}
		})
	}
}

func TestRDPH264QPConfiguration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		qp   *int
		want int
	}{
		{name: "default", want: 20},
		{name: "minimum", qp: new(0)},
		{name: "maximum", qp: new(51), want: 51},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			display := newRDPTestDisplayWithConfig(t, RDPConfig{H264: true, H264QP: test.qp})
			if test.qp != nil {
				*test.qp = 52 // A later caller mutation cannot change display settings.
			}
			if display.h264QP != test.want {
				t.Fatalf("display QP = %d, want %d", display.h264QP, test.want)
			}
		})
	}
}

func TestRDPWriterKeepsQPAcrossEncoderRestarts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		qp   *int
		want int
	}{
		{name: "default", want: 20},
		{name: "minimum", qp: new(0)},
		{name: "maximum", qp: new(51), want: 51},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			writer := &rdpFrameWriter{
				session:  &rdp.Session{Width: 16, Height: 16, BitsPerPixel: 32},
				graphics: true, h264QP: test.qp,
			}
			defer writer.close()
			for _, stage := range []string{"initial", "reactivated", "resized"} {
				if stage == "reactivated" {
					writer.close()
				}
				if stage == "resized" {
					writer.session.Width = 32
				}
				if err := writer.updateSize(); err != nil {
					t.Fatal(err)
				}
				frame := displayFrame{width: writer.width, height: writer.height}
				// A transport-less Session still exercises the real software encoder.
				if _, err := writer.write(frame, []image.Rectangle{image.Rect(0, 0, frame.width, frame.height)}, true); err != nil {
					t.Fatalf("%s encode: %v", stage, err)
				}
				if writer.avc == nil || writer.avc.QP() != test.want || writer.avc.Backend() != avc.BackendSoftware {
					t.Fatalf("%s encoder did not retain QP %d", stage, test.want)
				}
			}
		})
	}
}
