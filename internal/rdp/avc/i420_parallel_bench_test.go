package avc

import (
	"fmt"
	"image"
	"testing"
)

func BenchmarkI420Parallel(b *testing.B) {
	for _, test := range []struct {
		name                      string
		sourceWidth, sourceHeight int
		width, height             int
		region                    image.Rectangle
	}{
		{"Native1080p", 1920, 1080, 1920, 1080, image.Rect(0, 0, 1920, 1080)},
		{"Scaled1080p", 1280, 720, 1920, 1080, image.Rect(0, 0, 1920, 1080)},
		{"Partial1080p", 1920, 1080, 1920, 1080, image.Rect(64, 64, 1088, 834)},
		{"Widget1080p", 1920, 1080, 1920, 1080, image.Rect(64, 64, 128, 96)},
	} {
		b.Run(test.name, func(b *testing.B) {
			frames := benchmarkFrames(test.sourceWidth, test.sourceHeight)
			for _, workers := range []int{1, 2, 4} {
				b.Run(fmt.Sprintf("Workers%d", workers), func(b *testing.B) {
					e := Encoder{
						width: test.width, height: test.height,
						i420:    make([]byte, test.width*test.height*3/2),
						threads: workers,
					}
					// Warm the destination and goroutine stack cache before timing.
					for _, frame := range frames {
						e.convertI420Region(frame, test.region)
					}
					b.ReportAllocs()
					b.SetBytes(int64(test.region.Dx() * test.region.Dy() * 4))
					frame := 0
					for b.Loop() {
						e.convertI420Region(frames[frame%len(frames)], test.region)
						frame++
					}
				})
			}
		})
	}
}
