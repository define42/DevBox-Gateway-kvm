package avc

import (
	"fmt"
	"image"
	"testing"
)

// BenchmarkScrollPreparation isolates conversion and scroll detection from
// encoding. Alternating frames exercise both motion directions; static pane
// edges require retained lists of unchanged macroblocks.
func BenchmarkScrollPreparation(b *testing.B) {
	const width, height, shift = 1920, 1080, 18
	for _, border := range []int{0, 128} {
		b.Run(fmt.Sprintf("1080p/Border%d", border), func(b *testing.B) {
			var frames [2]*image.RGBA
			for frame := range frames {
				frames[frame] = image.NewRGBA(image.Rect(0, 0, width, height))
				paintScrollPane(frames[frame], frames[frame].Rect, frame*shift, border)
			}
			encoder := Encoder{
				width: width, height: height, threads: 1, i420: make([]byte, width*height*3/2),
				surface: surfaceTracker{enabled: true},
			}
			encoder.convertPreparedRegion(frames[0], frames[0].Rect, true)
			regions := []image.Rectangle{frames[0].Rect}
			prepare := func(frame int) {
				encoder.surface.begin()
				encoder.mapped = encoder.mapped[:0]
				encoder.convertDamage(frames[frame%2], regions, true)
			}
			prepare(1)
			prepare(2)
			matches := encoder.surface.matches
			if len(matches) != 1 || matches[0].shift != -shift || len(matches[0].bands) == 0 ||
				border != 0 && len(matches[0].unchanged) == 0 {
				b.Fatalf("fixture did not detect the expected pane scroll: %+v", matches)
			}
			frame := 1
			b.ReportAllocs()
			for b.Loop() {
				prepare(frame)
				frame++
			}
		})
	}
}
