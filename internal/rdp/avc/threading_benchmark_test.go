package avc

import (
	"fmt"
	"image"
	"testing"
	"time"
)

// BenchmarkSlicedDesktop includes persistent RGB-to-I420 conversion and H.264
// encoding. It alternates two deterministic desktop views, either scrolling the
// main content pane by eight pixels or changing a small widget. The timing and
// compressed-size metrics show the scaling and output-size effect of parallel
// slices. Only two RGBA images and one encoder are retained at a time.
func BenchmarkSlicedDesktop(b *testing.B) {
	for _, workload := range []string{"Scroll", "Widget"} {
		for _, threads := range []int{1, 2, 4} {
			b.Run(fmt.Sprintf("1080p/%s/%dSlices", workload, threads), func(b *testing.B) {
				frames, regions := slicedDesktopFrames(1920, 1080, workload == "Scroll")
				encoder, err := NewEncoderWithOptions(1920, 1080, Options{Threads: threads, Measure: true})
				if err != nil {
					b.Fatal(err)
				}
				defer encoder.Close()
				if _, err := encoder.Encode(frames[0], true); err != nil {
					b.Fatal(err)
				}
				var conversion, encoding time.Duration
				var bytes, frame int
				b.ReportAllocs()
				for b.Loop() {
					frame++
					data, err := encoder.EncodeDamage(frames[frame%2], regions, false)
					if err != nil || len(data) == 0 {
						b.Fatalf("encode returned %d bytes, error %v", len(data), err)
					}
					bytes += len(data)
					stats := encoder.LastStats()
					conversion += stats.Conversion
					encoding += stats.Encoding
				}
				b.ReportMetric(float64(conversion.Nanoseconds())/float64(b.N), "convert_ns/frame")
				b.ReportMetric(float64(encoding.Nanoseconds())/float64(b.N), "encode_ns/frame")
				b.ReportMetric(float64(bytes)/float64(b.N), "bytes/frame")
				b.ReportMetric(float64(encoder.Threads()), "slices")
			})
		}
	}
}

func slicedDesktopFrames(width, height int, scroll bool) ([2]*image.RGBA, []image.Rectangle) {
	region := image.Rect(224, 80, width-32, height-32)
	if !scroll {
		region = image.Rect(256, 96, 320, 128)
	}
	var frames [2]*image.RGBA
	for frame := range frames {
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := range height {
			for x := range width {
				r, g, blue := byte(240), byte(242), byte(245)
				contentY := y
				if scroll && image.Pt(x, y).In(region) {
					contentY += frame * 8
				}
				switch {
				case y < 80 || x < 224 || x >= width-32 || y >= height-32:
					r, g, blue = 45, 52, 63
				case contentY%240 < 64 && x%480 < 160:
					r, g, blue = byte(x/8), byte(contentY/8), 150
				case contentY%24 < 10 && x%16 < 11 && x%480 < 420-contentY%120:
					r, g, blue = 55, 60, 65
				}
				if !scroll && image.Pt(x, y).In(region) {
					r, g, blue = 60, byte(80+frame*80), 180
				}
				offset := y*img.Stride + x*4
				img.Pix[offset], img.Pix[offset+1], img.Pix[offset+2], img.Pix[offset+3] = r, g, blue, 255
			}
		}
		frames[frame] = img
	}

	return frames, []image.Rectangle{region}
}
