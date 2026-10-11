package openh264

import (
	"math"
	"os"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

func psnr(a, b []byte) float64 {
	var sse float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sse += d * d
	}
	if sse == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255*float64(len(a))/sse)
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	const w, h = 320, 192
	yuv, err := os.ReadFile("../../res/CiscoVT2people_320x192_12fps.yuv")
	if err != nil {
		t.Skip(err)
	}
	frameSize := w * h * 3 / 2
	nFrames := len(yuv) / frameSize
	if nFrames > 30 {
		nFrames = 30
	}

	p, err := DefaultEncoderParams(w, h, 500000, 12)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncoder(p)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	dec, err := NewDecoder(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	var inputs []*Frame
	var outputs []*Frame
	for i := 0; i < nFrames; i++ {
		raw := yuv[i*frameSize : (i+1)*frameSize]
		f := &Frame{Width: w, Height: h, Y: raw[:w*h], U: raw[w*h : w*h*5/4], V: raw[w*h*5/4:], Timestamp: int64(i * 83)}
		bs, ft, err := enc.Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && ft != api.VideoFrameTypeIDR {
			t.Fatalf("first frame type = %d, want IDR", ft)
		}
		if ft == api.VideoFrameTypeSkip {
			continue
		}
		inputs = append(inputs, f)
		out, err := dec.Decode(bs)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if out != nil {
			outputs = append(outputs, out)
		}
	}
	rest, err := dec.Flush()
	if err != nil {
		t.Fatal(err)
	}
	outputs = append(outputs, rest...)
	if len(outputs) != len(inputs) {
		t.Fatalf("decoded %d frames, encoded %d", len(outputs), len(inputs))
	}
	for i := range outputs {
		if outputs[i].Width != w || outputs[i].Height != h {
			t.Fatalf("frame %d: size %dx%d", i, outputs[i].Width, outputs[i].Height)
		}
		if q := psnr(inputs[i].Y, outputs[i].Y); q < 30 {
			t.Errorf("frame %d: luma PSNR %.2f dB too low", i, q)
		}
	}
}

func TestEncoderRejectsWrongSize(t *testing.T) {
	p, err := DefaultEncoderParams(64, 64, 100000, 15)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncoder(p)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	if _, _, err := enc.Encode(NewFrame(32, 32)); err == nil {
		t.Fatal("expected size mismatch error")
	}
}

func TestDecoderCapability(t *testing.T) {
	c, err := GetDecoderCapability()
	if err != nil {
		t.Fatal(err)
	}
	if c.IProfileIdc != 66 || c.ILevelIdc != 32 {
		t.Fatalf("unexpected capability %+v", c)
	}
}
