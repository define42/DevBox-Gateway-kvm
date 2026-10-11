//go:build nvenc && cgo

package nvenc

import "testing"

func TestNewRejectsInvalidParameters(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                         string
		width, height, frameRate, qp int
	}{
		{"zero-width", 0, 720, 60, 20},
		{"odd-height", 1280, 721, 60, 20},
		{"oversized", 4098, 720, 60, 20},
		{"zero-frame-rate", 1280, 720, 0, 20},
		{"invalid-quantizer", 1280, 720, 60, 52},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoder, err := New(test.width, test.height, test.frameRate, test.qp)
			if err == nil || encoder != nil {
				if encoder != nil {
					encoder.Close()
				}
				t.Fatalf("New returned %v, %v", encoder, err)
			}
		})
	}
}

func TestClosedEncoder(t *testing.T) {
	t.Parallel()
	encoder := &Encoder{}
	encoder.Close()
	if _, err := encoder.Encode(nil, false, 0); err == nil {
		t.Fatal("Encode on a closed encoder succeeded")
	}
}
