package openh264

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

// The fake codec mutates one shared output allocation on every call, just as
// an actual codec may. Returning the metadata or storage directly would break
// callers that retain earlier access units.
type metadataTestCodec struct {
	api.ISVCEncoder
	data     [6]byte
	lengths  [1]int32
	calls    int
	result   int32
	panicNow bool
	stale    bool
}

func (c *metadataTestCodec) EncodeFrame(_ *api.SSourcePicture, info *api.SFrameBSInfo) int32 {
	c.stale = c.stale || !reflect.ValueOf(*info).IsZero()
	c.calls++
	c.data = [6]byte{0, 0, 0, 1, 0x41, byte(c.calls)}
	c.lengths[0] = int32(len(c.data))
	info.ILayerNum = 1
	info.EFrameType = api.VideoFrameTypeP
	info.SLayerInfo[0] = api.SLayerBSInfo{INalCount: 1, PNalLengthInByte: c.lengths[:], PBsBuf: c.data[:]}
	if c.panicNow {
		panic("test codec panic")
	}
	return c.result
}

func TestEncoderMetadataOwnership(t *testing.T) {
	t.Parallel()
	codec := &metadataTestCodec{}
	enc := &Encoder{e: codec, width: 16, height: 16}
	frame := &Frame{Width: 16, Height: 16, Y: make([]byte, 256), U: make([]byte, 64), V: make([]byte, 64), UnchangedMacroblocks: []bool{true}}
	first, kind, err := enc.Encode(frame)
	if err != nil || kind != api.VideoFrameTypeP {
		t.Fatalf("initial encode: %v, %v", kind, err)
	}
	expected := bytes.Clone(first)
	assertCleared := func() {
		t.Helper()
		if !reflect.ValueOf(enc.source).IsZero() || !reflect.ValueOf(enc.info).IsZero() {
			t.Fatal("completed call retained borrowed planes, hints, or bitstream metadata")
		}
	}
	assertCleared()
	for i := 0; i < 3; i++ {
		codec.result = int32(i % 2)
		_, _, err = enc.Encode(frame)
		if (err != nil) != (codec.result != 0) {
			t.Fatalf("result=%d error=%v", codec.result, err)
		}
		assertCleared()
	}
	codec.panicNow = true
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected injected panic")
			}
		}()
		_, _, _ = enc.Encode(frame)
	}()
	assertCleared()
	if codec.stale {
		t.Fatal("codec saw stale bitstream metadata on entry")
	}
	if !bytes.Equal(first, expected) {
		t.Fatal("later encode changed caller-owned output")
	}
}
