package avc

import (
	"fmt"
	"image"
	"testing"
)

// Construction includes codec buffers and teardown, without encoding a frame.
func BenchmarkSoftwareEncoderConstruction(b *testing.B) {
	for _, slices := range []int{1, 4} {
		b.Run(fmt.Sprintf("1080p/%dSlices", slices), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				encoder, err := NewEncoderWithOptions(1920, 1080, Options{
					Backend: BackendSoftware, Threads: slices,
				})
				if err != nil {
					b.Fatal(err)
				}
				encoder.Close()
			}
		})
	}
}

// The preprocessing strategies initialize on the first frame, so startup must
// include that frame to measure their storage as well as constructor buffers.
func BenchmarkSoftwareEncoderStartup(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	for _, slices := range []int{1, 4} {
		b.Run(fmt.Sprintf("1080p/%dSlices", slices), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				encoder, err := NewEncoderWithOptions(1920, 1080, Options{
					Backend: BackendSoftware, Threads: slices,
				})
				if err != nil {
					b.Fatal(err)
				}
				data, err := encoder.Encode(img, true)
				encoder.Close()
				if err != nil || len(data) == 0 {
					b.Fatalf("initial encode returned %d bytes, error %v", len(data), err)
				}
			}
		})
	}
}
