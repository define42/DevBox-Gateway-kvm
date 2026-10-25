package avc

import (
	"errors"
	"sync"
	"testing"
)

var errBenchmarkEmptyAccessUnit = errors.New("scroll frame produced empty access unit")

// BenchmarkOfficeProfileBudget measures the work budget of one nominal second
// for four software sessions at explicit 30/60-FPS codec rates. Both named RDP
// profiles now cap at 30 FPS; 60 FPS remains a reference workload. Each stream
// alternates the same two immutable scrolling pictures, encoding identical changes
// per frame. Sampling an alternating 60-Hz source at 30 Hz would instead select
// identical pictures and incorrectly measure unchanged-frame suppression.
//
// One operation encodes 4*rate full pictures concurrently, without real-time
// pacing. This isolates configured work budgets; it does not simulate paced
// traffic or establish achievable session FPS. Run profile measurements serially
// on an otherwise idle host, for example with -cpu=4 -benchtime=1x -count=10.
func BenchmarkOfficeProfileBudget(b *testing.B) {
	const streamCount = 4
	frames, regions := slicedDesktopFrames(1920, 1080, true)
	for _, profile := range []struct {
		name string
		rate int
	}{
		{name: "fps=60", rate: 60},
		{name: "fps=30", rate: 30},
	} {
		b.Run(profile.name, func(b *testing.B) {
			var streams [streamCount]struct {
				encoder *Encoder
				frame   int
				bytes   int64
				err     error
			}
			for index := range streams {
				encoder, err := NewEncoderWithOptions(1920, 1080, Options{
					Threads: 1, FrameRate: profile.rate,
				})
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(encoder.Close)
				streams[index].encoder = encoder
				if _, err := encoder.Encode(frames[0], true); err != nil {
					b.Fatal(err)
				}
			}
			var workers sync.WaitGroup
			var encodedBytes int64
			b.ReportAllocs()
			for b.Loop() {
				for index := range streams {
					stream := &streams[index]
					workers.Go(func() {
						stream.bytes, stream.err = 0, nil
						for range profile.rate {
							stream.frame = (stream.frame + 1) % len(frames)
							data, err := stream.encoder.EncodeDamage(frames[stream.frame], regions, false)
							if err != nil {
								stream.err = err

								return
							}
							if len(data) == 0 {
								stream.err = errBenchmarkEmptyAccessUnit

								return
							}
							stream.bytes += int64(len(data))
						}
					})
				}
				workers.Wait()
				for index, stream := range streams {
					if stream.err != nil {
						b.Fatalf("stream %d: %v", index, stream.err)
					}
					encodedBytes += stream.bytes
				}
			}
			b.ReportMetric(float64(streamCount*profile.rate), "frames/op")
			b.ReportMetric(float64(encodedBytes)/float64(b.N), "encoded_bytes/op")
		})
	}
}
