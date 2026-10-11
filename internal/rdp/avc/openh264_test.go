package avc

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	openh264 "github.com/define42/DevBox-Gateway-kvm/pkg/h264"
)

func TestOpenH264RoundTrip(t *testing.T) {
	t.Parallel()
	encoder, err := NewEncoder(64, 48)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	decoder, err := openh264.NewDecoder(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()

	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	var first, firstCopy []byte
	var packets [][]byte
	var expected [][]byte
	for index := range 3 {
		fillColorBars(img, index)
		var regions []image.Rectangle
		if index == 1 {
			regions = []image.Rectangle{image.Rect(16, 16, 32, 32)}
		}
		data, err := encoder.EncodeDamage(img, regions, index == 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) == 0 || !bytes.HasPrefix(data, []byte{0, 0, 0, 1}) {
			t.Fatalf("frame %d lacks Annex-B start code: %x", index, data)
		}
		if index != 1 && (!hasNAL(data, 7) || !hasNAL(data, 8) || !hasNAL(data, 5)) {
			t.Fatalf("frame %d lacks SPS/PPS/IDR NAL units", index)
		}
		if index == 1 && !hasNAL(data, 1) {
			t.Fatal("interframe did not produce a P-frame")
		}
		if index == 0 {
			first, firstCopy = data, bytes.Clone(data)
		}
		packets = append(packets, data)
		want := make([]byte, 64*48*3/2)
		scaleI420(want, 64, 48, img)
		expected = append(expected, want)
	}
	if !bytes.Equal(first, firstCopy) {
		t.Fatal("a later encode mutated an earlier access unit")
	}

	var decoded []*openh264.Frame
	for index, packet := range packets {
		frame, err := decoder.Decode(packet)
		if err != nil {
			t.Fatalf("decode frame %d: %v", index, err)
		}
		if frame != nil {
			decoded = append(decoded, frame)
		}
	}
	rest, err := decoder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	decoded = append(decoded, rest...)
	if len(decoded) != len(expected) {
		t.Fatalf("decoded %d pictures, want %d", len(decoded), len(expected))
	}
	for index, frame := range decoded {
		if frame.Width != 64 || frame.Height != 48 {
			t.Fatalf("frame %d dimensions = %dx%d", index, frame.Width, frame.Height)
		}
		want := expected[index]
		pixels := appendFrameI420(nil, frame)
		if len(pixels) != len(want) {
			t.Fatalf("frame %d decoded length = %d, want %d", index, len(pixels), len(want))
		}
		var difference int
		for i, value := range want {
			difference += max(int(value)-int(pixels[i]), int(pixels[i])-int(value))
		}
		if average := float64(difference) / float64(len(want)); average > 3 {
			t.Fatalf("frame %d average sample error = %f", index, average)
		}
	}
}

func appendFrameI420(dst []byte, frame *openh264.Frame) []byte {
	dst = append(dst, frame.Y...)
	dst = append(dst, frame.U...)

	return append(dst, frame.V...)
}

func fillColorBars(img *image.RGBA, frame int) {
	colors := []color.RGBA{
		{R: 230, G: 30, B: 40, A: 255},
		{R: 20, G: 220, B: 40, A: 255},
		{R: 30, G: 40, B: 230, A: 255},
		{R: 240, G: 240, B: 240, A: 255},
	}
	for y := range img.Rect.Dy() {
		for x := range img.Rect.Dx() {
			value := colors[x/16]
			if x >= 16 && x < 32 && y >= 16 && y < 32 {
				value.R += byte(frame * 15)
			}
			img.SetRGBA(x, y, value)
		}
	}
}

func hasNAL(data []byte, kind byte) bool {
	for i := 0; i+4 < len(data); i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 && data[i+3]&31 == kind {
			return true
		}
	}

	return false
}
