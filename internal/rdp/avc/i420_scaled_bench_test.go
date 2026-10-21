package avc

import (
	"image"
	"testing"
)

func BenchmarkI420Scaled(b *testing.B) {
	for _, test := range []struct {
		name                      string
		sourceWidth, sourceHeight int
		width, height             int
	}{
		{"upscale", 800, 600, 1024, 768},
		{"downscale", 1920, 1080, 1024, 768},
		{"vertical-only", 1024, 600, 1024, 768},
		{"1080p", 1280, 720, 1920, 1080},
	} {
		b.Run(test.name, func(b *testing.B) {
			img := image.NewRGBA(image.Rect(0, 0, test.sourceWidth, test.sourceHeight))
			for index := range img.Pix {
				img.Pix[index] = byte(index*19 + index/7)
			}
			for _, region := range []struct {
				name string
				rect image.Rectangle
			}{
				{"full", image.Rect(0, 0, test.width, test.height)},
				{"64x32", image.Rect(64, 64, 128, 96)},
				{"2x2", image.Rect(64, 64, 66, 66)},
			} {
				b.Run(region.name, func(b *testing.B) {
					for _, conversion := range []struct {
						name string
						fn   func([]byte, int, int, *image.RGBA, image.Rectangle) bool
					}{
						{"scalar", scaleI420RegionScalar},
						{"dispatch", scaleI420Region},
					} {
						b.Run(conversion.name, func(b *testing.B) {
							dst := make([]byte, test.width*test.height*3/2)
							b.ReportAllocs()
							b.SetBytes(int64(region.rect.Dx() * region.rect.Dy() * 4))
							for b.Loop() {
								conversion.fn(dst, test.width, test.height, img, region.rect)
							}
						})
					}
				})
			}
		})
	}
}
