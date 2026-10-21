//go:build linux && amd64

package encoder

import "testing"

func TestSampleSATDGuardPages(t *testing.T) {
	requireSampleAVX2(t)
	for _, size := range [][2]int{{8, 4}, {8, 8}, {8, 16}, {16, 8}, {16, 16}} {
		for _, padding := range []int{0, 3} {
			width, height := size[0], size[1]
			stride := width + padding
			span := (height-1)*stride + width
			enc, a, b := idctGuardedBuffer(t, span), idctGuardedBuffer(t, span), idctGuardedBuffer(t, span)
			for i := range enc {
				enc[i], a[i], b[i] = byte(i*17), byte(i*31+1), byte(i*13+2)
			}
			got := sampleAvgSatdAVX2(enc, 0, int32(stride), a, 0, int32(stride), b, 0, int32(stride), int32(width), int32(height))
			want := sampleAvgSatdReference(enc, 0, int32(stride), a, 0, int32(stride), b, 0, int32(stride), int32(width), int32(height))
			if got != want {
				t.Fatalf("average size=%v stride=%d got=%d want=%d", size, stride, got, want)
			}
			got = sampleSatdBlockAVX2(enc, 0, int32(stride), a, 0, int32(stride), width, height)
			var plain int32
			for y := 0; y < height; y += 4 {
				for x := 0; x < width; x += 4 {
					plain += WelsSampleSatd4x4_c(enc, y*stride+x, int32(stride), a, y*stride+x, int32(stride))
				}
			}
			if got != plain {
				t.Fatalf("plain size=%v stride=%d got=%d want=%d", size, stride, got, plain)
			}
		}
	}
}

func TestSampleSATDChecksLengthBeforeAssembly(t *testing.T) {
	requireSampleAVX2(t)
	for _, average := range []bool{false, true} {
		for input := 0; input < 3; input++ {
			if !average && input == 2 {
				continue
			}
			t.Run(string(rune('a'+input))+map[bool]string{false: "plain", true: "avg"}[average], func(t *testing.T) {
				inputs := [3][]byte{make([]byte, 256), make([]byte, 256), make([]byte, 256)}
				// Deliberately retain a sufficient capacity: assembly may
				// access only the Go-visible length.
				inputs[input] = inputs[input][:255]
				defer func() {
					if recover() == nil {
						t.Fatal("missing Go bounds panic")
					}
				}()
				if average {
					sampleAvgSatdAVX2(inputs[0], 0, 16, inputs[1], 0, 16, inputs[2], 0, 16, 16, 16)
				} else {
					sampleSatdBlockAVX2(inputs[0], 0, 16, inputs[1], 0, 16, 16, 16)
				}
			})
		}
	}
}
