//go:build amd64

package common

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

type sampleSadFourFunc func([]uint8, int, int32, []uint8, int, int32, []int32)

var sadFourSIMDCases = []struct {
	name          string
	width, height int
	batched       sampleSadFourFunc
	single        sampleSadFunc
}{
	{"16x16", 16, 16, WelsSampleSadFour16x16_sse2, WelsSampleSad16x16_sse2},
	{"16x8", 16, 8, WelsSampleSadFour16x8_sse2, WelsSampleSad16x8_sse2},
	{"8x16", 8, 16, WelsSampleSadFour8x16_sse2, WelsSampleSad8x16_sse2},
	{"8x8", 8, 8, WelsSampleSadFour8x8_sse2, WelsSampleSad8x8_sse2},
	{"8x4", 8, 4, WelsSampleSadFour8x4_sse2, WelsSampleSad8x4_sse2},
	{"4x8", 4, 8, WelsSampleSadFour4x8_sse2, WelsSampleSad4x8_sse2},
	{"4x4", 4, 4, WelsSampleSadFour4x4_sse2, WelsSampleSad4x4_sse2},
}

func sadFourReference(a []byte, aOff, aStride int, ref []byte, refOff, refStride, width, height int) [4]int32 {
	var result [4]int32
	for direction, shift := range [...]int{-refStride, refStride, -1, 1} {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				diff := int(a[aOff+y*aStride+x]) - int(ref[refOff+shift+y*refStride+x])
				if diff < 0 {
					diff = -diff
				}
				result[direction] += int32(diff)
			}
		}
	}
	return result
}

func TestSampleSadFourBatchedParity(t *testing.T) {
	rng := rand.New(rand.NewSource(26404))
	for _, tc := range sadFourSIMDCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, strides := range [][2]int{{0, 0}, {1, 1}, {tc.width, tc.width}, {tc.width + 3, tc.width + 7}, {64, 33}} {
				for _, off := range []int{0, 1, 7, 15} {
					for _, alias := range []bool{false, true} {
						aStride, refStride := strides[0], strides[1]
						padding := max(refStride, 1)
						aLen := off + (tc.height-1)*aStride + tc.width
						refLen := off + 2*padding + (tc.height-1)*refStride + tc.width
						a, ref := make([]byte, aLen), make([]byte, refLen)
						if alias {
							backing := make([]byte, max(aLen, refLen)+3)
							a = backing[:aLen]
							ref = backing[3 : 3+refLen]
						}
						for iteration := 0; iteration < 8; iteration++ {
							for i := range a {
								a[i] = byte(rng.Intn(256))
							}
							for i := range ref {
								ref[i] = byte(rng.Intn(256))
							}
							if iteration == 0 && !alias {
								for i := range a {
									a[i] = 255
								}
								clear(ref)
							}
							aBefore, refBefore := bytes.Clone(a), bytes.Clone(ref)
							want := sadFourReference(a, off, aStride, ref, off+padding, refStride, tc.width, tc.height)
							got := [6]int32{-123, -1, -1, -1, -1, -456}
							tc.batched(a, off, int32(aStride), ref, off+padding, int32(refStride), got[1:5])
							if [4]int32(got[1:5]) != want || got[0] != -123 || got[5] != -456 {
								t.Fatalf("strides=%v off=%d alias=%t iteration=%d: got=%v want=%v", strides, off, alias, iteration, got, want)
							}
							if !bytes.Equal(a, aBefore) || !bytes.Equal(ref, refBefore) {
								t.Fatal("SAD modified an input buffer")
							}
						}
					}
				}
			}
		})
	}
}

func TestSampleSadFourBatchedBounds(t *testing.T) {
	for _, tc := range sadFourSIMDCases {
		t.Run(tc.name, func(t *testing.T) {
			stride := tc.width + 2
			a := make([]byte, (tc.height-1)*stride+tc.width)
			ref := make([]byte, (tc.height+1)*stride+tc.width)
			var out [4]int32
			for _, invalid := range []struct {
				name string
				fn   func()
			}{
				{"source tail", func() { tc.batched(a[:len(a)-1], 0, int32(stride), ref, stride, int32(stride), out[:]) }},
				{"source start", func() { tc.batched(a, -1, int32(stride), ref, stride, int32(stride), out[:]) }},
				{"source stride", func() { tc.batched(a, 0, -1, ref, stride, int32(stride), out[:]) }},
				{"reference tail", func() { tc.batched(a, 0, int32(stride), ref[:len(ref)-1], stride, int32(stride), out[:]) }},
				{"reference start", func() { tc.batched(a, 0, int32(stride), ref, stride-1, int32(stride), out[:]) }},
				{"reference stride", func() { tc.batched(a, 0, int32(stride), ref, stride, -1, out[:]) }},
				{"zero stride left", func() { tc.batched(a, 0, int32(stride), ref, 0, 0, out[:]) }},
				{"short output", func() { tc.batched(a, 0, int32(stride), ref, stride, int32(stride), out[:3]) }},
			} {
				t.Run(invalid.name, func(t *testing.T) { requireSIMDBoundsPanic(t, invalid.fn) })
			}
		})
	}
}

func BenchmarkSampleSadFourBatched(b *testing.B) {
	for _, tc := range sadFourSIMDCases {
		for _, stride := range []int{32, 2048} {
			b.Run(fmt.Sprintf("%s/stride%d", tc.name, stride), func(b *testing.B) {
				a, ref := make([]byte, stride*(tc.height+2)), make([]byte, stride*(tc.height+3))
				for i := range a {
					a[i] = byte(i*29 + 17)
				}
				for i := range ref {
					ref[i] = byte(i*31 + 11)
				}
				for _, batched := range []bool{false, true} {
					name := "FourSingles"
					if batched {
						name = "Batched"
					}
					b.Run(name, func(b *testing.B) {
						var out [4]int32
						b.ReportAllocs()
						b.SetBytes(int64(tc.width * tc.height * 5))
						if batched {
							for i := 0; i < b.N; i++ {
								tc.batched(a, 3, int32(stride), ref, stride+5, int32(stride), out[:])
							}
						} else {
							for i := 0; i < b.N; i++ {
								sampleSadFourSSE2(tc.single, a, 3, int32(stride), ref, stride+5, int32(stride), out[:])
							}
						}
						if out == [4]int32{} {
							b.Fatal("unexpected zero SAD")
						}
					})
				}
			})
		}
	}
}
