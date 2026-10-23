package avc

import (
	"fmt"
	"testing"
	"time"
)

const policyBenchmarkFrames = 601

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
				codec, slices, err := newCodecEncoderWithConfig(1920, 1080, 4, FrameRate, DefaultQP, candidate.config)
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
