package avc

import (
	"fmt"
	"image"
	"testing"
	"time"
)

const policyBenchmarkFrames = 601

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

// BenchmarkCodecPolicy runs long enough for both candidate periodic IDR
// intervals to occur. One benchmark operation is a 601-frame sequence, so the
// standard B/op and allocs/op results can be divided by 601 for per-frame costs.
func BenchmarkCodecPolicy(b *testing.B) {
	configs := []struct {
		name   string
		config codecConfig
	}{
		{"IDR150/DeblockWithinSlices", codecConfig{intraPeriod: 150, loopFilterDisableIDC: 2}},
		{"IDR150/DeblockOff", codecConfig{intraPeriod: 150, loopFilterDisableIDC: 1}},
		{"IDR600/DeblockWithinSlices", codecConfig{intraPeriod: 600, loopFilterDisableIDC: 2}},
		{"IDR600/DeblockOff", codecConfig{intraPeriod: 600, loopFilterDisableIDC: 1}},
		{"IDRDisabled/DeblockWithinSlices", codecConfig{intraPeriod: 0, loopFilterDisableIDC: 2}},
		{"IDRDisabled/DeblockOff", codecConfig{intraPeriod: 0, loopFilterDisableIDC: 1}},
	}
	for _, workload := range []string{"Scroll", "Widget"} {
		for _, candidate := range configs {
			b.Run(fmt.Sprintf("1080p/%s/%s", workload, candidate.name), func(b *testing.B) {
				frames, regions := slicedDesktopFrames(1920, 1080, workload == "Scroll")
				codec, slices, err := newCodecEncoderWithConfig(1920, 1080, 4, FrameRate, candidate.config)
				if err != nil {
					b.Fatal(err)
				}
				encoder := &Encoder{
					codec: codec, frameRate: FrameRate, width: 1920, height: 1080,
					i420: make([]byte, 1920*1080*3/2), threads: slices, measure: true,
				}
				defer encoder.Close()
				if _, err := encoder.Encode(frames[0], true); err != nil {
					b.Fatal(err)
				}

				var conversion, encoding, predictedEncoding, idrEncoding time.Duration
				var bytes, predictedBytes, idrBytes, encoded, predicted, idrs int64
				var frame int
				b.ReportAllocs()
				for b.Loop() {
					for range policyBenchmarkFrames {
						frame++
						data, err := encoder.EncodeDamage(frames[frame%2], regions, false)
						if err != nil || len(data) == 0 {
							b.Fatalf("encode returned %d bytes, error %v", len(data), err)
						}
						frameBytes := int64(len(data))
						bytes += frameBytes
						encoded++
						stats := encoder.LastStats()
						conversion += stats.Conversion
						encoding += stats.Encoding
						if hasNAL(data, 5) {
							idrs++
							idrBytes += frameBytes
							idrEncoding += stats.Encoding
						} else {
							predicted++
							predictedBytes += frameBytes
							predictedEncoding += stats.Encoding
						}
					}
				}
				b.ReportMetric(float64(conversion.Nanoseconds())/float64(encoded), "convert_ns/frame")
				b.ReportMetric(float64(encoding.Nanoseconds())/float64(encoded), "encode_ns/frame")
				b.ReportMetric(float64(bytes)/float64(encoded), "bytes/frame")
				b.ReportMetric(float64(predictedBytes)/float64(predicted), "p_bytes/frame")
				b.ReportMetric(float64(predictedEncoding.Nanoseconds())/float64(predicted), "p_encode_ns/frame")
				if idrs != 0 {
					b.ReportMetric(float64(idrBytes)/float64(idrs), "idr_bytes/frame")
					b.ReportMetric(float64(idrEncoding.Nanoseconds())/float64(idrs), "idr_encode_ns/frame")
				}
				b.ReportMetric(float64(idrs)/float64(b.N), "idrs/sequence")
				b.ReportMetric(policyBenchmarkFrames, "frames/sequence")
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
