package avc

import (
	"image"
	"testing"
)

func TestEncoderUsesSoftware(t *testing.T) {
	t.Parallel()
	encoder, err := NewEncoderWithOptions(640, 480, Options{Threads: 2})
	if err != nil {
		t.Fatal(err)
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
