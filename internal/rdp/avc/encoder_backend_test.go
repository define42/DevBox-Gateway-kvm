package avc

import (
	"errors"
	"image"
	"testing"
)

func TestEncoderBackendSelection(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		backend Backend
		wantErr error
	}{
		{name: "default software"},
		{name: "explicit software", backend: BackendSoftware},
		{name: "auto compatibility alias", backend: BackendAuto},
		{name: "removed hardware backend", backend: "nvenc", wantErr: ErrBackend},
		{name: "invalid", backend: "unknown", wantErr: ErrBackend},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoder, err := NewEncoderWithOptions(640, 480, Options{Threads: 2, Backend: test.backend})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if err != nil {
				if encoder != nil {
					encoder.Close()
					t.Fatal("failed construction returned an encoder")
				}

				return
			}
			defer encoder.Close()
			if encoder.Backend() != BackendSoftware || encoder.Threads() != 2 {
				t.Fatalf("backend = %s, slices = %d; want software with two slices", encoder.Backend(), encoder.Threads())
			}
			if _, ok := encoder.codec.(*openH264Encoder); !ok {
				t.Fatalf("codec = %T, want bundled Go codec", encoder.codec)
			}
			packet, err := encoder.Encode(image.NewRGBA(image.Rect(0, 0, 640, 480)), false)
			if err != nil || !hasNAL(packet, 7) || !hasNAL(packet, 8) || !hasNAL(packet, 5) {
				t.Fatalf("initial frame: bytes=%d error=%v", len(packet), err)
			}
			encoder.Close()
		})
	}
}

type failingCodec struct {
	codecEncoder
	err error
}

func (c *failingCodec) encode(pixels []byte, force bool, timestamp int64) ([]byte, error) {
	if c.err != nil {
		return nil, c.err
	}

	return c.codecEncoder.encode(pixels, force, timestamp)
}
