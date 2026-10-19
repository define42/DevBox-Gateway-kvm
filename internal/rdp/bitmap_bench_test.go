package rdp

import (
	"image"
	"strconv"
	"testing"
)

func BenchmarkBitmapScaleRegion(b *testing.B) {
	for _, depth := range []int{16, 24, 32} {
		for _, test := range []struct {
			name   string
			output image.Point
			region image.Rectangle
		}{
			{name: "1080p", output: image.Pt(1920, 1080), region: image.Rect(0, 0, 1920, 1080)},
			{name: "64x32", output: image.Pt(1920, 1080), region: image.Rect(64, 64, 128, 96)},
			{name: "Scaled720p", output: image.Pt(1280, 720), region: image.Rect(0, 0, 1280, 720)},
		} {
			b.Run(strconv.Itoa(depth)+"/"+test.name, func(b *testing.B) {
				img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
				for index := range img.Pix {
					img.Pix[index] = byte(index*37 + index/4)
				}
				encoder, err := NewBitmapEncoder(test.output.X, test.output.Y, depth)
				if err != nil {
					b.Fatal(err)
				}
				encoder.pixels = make([]byte, encoder.width*encoder.height*encoder.bytesPerPixel)
				b.SetBytes(int64(test.region.Dx() * test.region.Dy() * 4))
				b.ReportAllocs()
				for b.Loop() {
					encoder.scaleRegion(img, test.region)
				}
			})
		}
	}
}
