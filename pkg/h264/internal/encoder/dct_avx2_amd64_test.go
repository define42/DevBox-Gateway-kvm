//go:build amd64

package encoder

import (
	"fmt"
	"math/rand"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

func TestDCTFourAVX2MatchesScalar(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	t.Parallel()
	rng := rand.New(rand.NewSource(0x264a))
	for iteration := 0; iteration < 4000; iteration++ {
		s1, s2 := rng.Intn(96), rng.Intn(96)
		o1, o2 := rng.Intn(32), rng.Intn(32)
		p1, p2 := make([]byte, o1+7*s1+8), make([]byte, o2+7*s2+8)
		_, _ = rng.Read(p1)
		_, _ = rng.Read(p2)
		var want [64]int16
		var backing [68]int16
		for i := range backing {
			backing[i] = -12345
		}
		got := (*[64]int16)(backing[2:66])
		WelsDctFourT4_c(want[:], p1, o1, int32(s1), p2, o2, int32(s2))
		welsDctFourT4AVX2(got, &p1[o1], s1, &p2[o2], s2)
		if *got != want {
			t.Fatalf("iteration %d strides %d/%d: got %v, want %v", iteration, s1, s2, *got, want)
		}
		if backing[0] != -12345 || backing[1] != -12345 || backing[66] != -12345 || backing[67] != -12345 {
			t.Fatal("transform wrote outside coefficient output")
		}
	}
}

func TestDCTFourAVX2RetainsSSE2Aliases(t *testing.T) {
	t.Parallel()
	for _, stride := range []int{0, 1, 8, 19, -19} {
		for _, outputOffset := range []int{0, 16, 32, 64, 128} {
			for _, input := range []int{1, 2, 3} {
				t.Run(fmt.Sprintf("stride%d/output%d/input%d", stride, outputOffset, input), func(t *testing.T) {
					var actual, expected [256]int16
					aBytes := unsafe.Slice((*byte)(unsafe.Pointer(&actual[0])), len(actual)*2)
					eBytes := unsafe.Slice((*byte)(unsafe.Pointer(&expected[0])), len(expected)*2)
					for i := range aBytes {
						aBytes[i] = byte(i*71 + 29)
					}
					copy(eBytes, aBytes)
					external := make([]byte, len(aBytes))
					for i := range external {
						external[i] = byte(i*43 + 17)
					}
					a1, a2, e1, e2 := external, external, external, external
					if input&1 != 0 {
						a1, e1 = aBytes, eBytes
					}
					if input&2 != 0 {
						a2, e2 = aBytes, eBytes
					}
					offset := 32
					if stride < 0 {
						offset -= 7 * stride
					}
					oldDctFourT4SSE2(expected[outputOffset:], e1, offset, int32(stride), e2, offset, int32(stride))
					WelsDctFourT4_sse2(actual[outputOffset:], a1, offset, int32(stride), a2, offset, int32(stride))
					if actual != expected {
						t.Fatal("output alias changed the previous SSE2 access order")
					}
				})
			}
		}
	}
}

func TestDCTFourAVX2InputLength(t *testing.T) {
	for _, input := range []int{1, 2} {
		for _, stride := range []int32{8, 19, -19} {
			t.Run(fmt.Sprintf("input%d/stride%d", input, stride), func(t *testing.T) {
				span, offset := int(7*stride)+8, 0
				if stride < 0 {
					span, offset = int(-7*stride)+8, int(-7*stride)
				}
				p1, p2 := make([]byte, span), make([]byte, span)
				if input == 1 {
					p1 = p1[:len(p1)-1]
				} else {
					p2 = p2[:len(p2)-1]
				}
				var got [64]int16
				defer func() {
					if recover() == nil {
						t.Fatal("input slice capacity authorized an out-of-length read")
					}
				}()
				WelsDctFourT4_sse2(got[:], p1, offset, stride, p2, offset, stride)
			})
		}
	}
}

// Keep the pre-AVX2 entry point as an independent old-path oracle and benchmark.
func oldDctFourT4SSE2(dct []int16, p1 []byte, o1 int, s1 int32, p2 []byte, o2 int, s2 int32) {
	_ = dct[63]
	checkSampleSIMDBlock(p1, o1, s1, 8, 8)
	checkSampleSIMDBlock(p2, o2, s2, 8, 8)
	welsDctFourT4SSE2(dct, p1[o1:], int(s1), p2[o2:], int(s2))
}

func BenchmarkDCTFourAVX2(b *testing.B) {
	for _, stride := range []int{8, 64, 2048} {
		b.Run(fmt.Sprintf("stride%d", stride), func(b *testing.B) {
			p1, p2 := make([]byte, 7*stride+8), make([]byte, 7*stride+8)
			for i := range p1 {
				p1[i], p2[i] = byte(i*29+17), byte(i*13+71)
			}
			var output [64]int16
			b.Run("old-SSE2", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					oldDctFourT4SSE2(output[:], p1, 0, int32(stride), p2, 0, int32(stride))
				}
				dctBenchmarkSink = output[0]
			})
			b.Run("dispatch", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					WelsDctFourT4_sse2(output[:], p1, 0, int32(stride), p2, 0, int32(stride))
				}
				dctBenchmarkSink = output[0]
			})
			b.Run("raw-AVX2", func(b *testing.B) {
				if !cpu.X86.HasAVX2 {
					b.Skip("AVX2 unavailable")
				}
				b.ReportAllocs()
				for b.Loop() {
					welsDctFourT4AVX2(&output, &p1[0], stride, &p2[0], stride)
				}
				dctBenchmarkSink = output[0]
			})
		})
	}
}

// Digests come from OpenH264 2.6.0 WelsDctFourT4_avx2. The native harness uses
// the same xorshift32 corpus and hashes both bytes of every output coefficient.
func TestDCTFourAVX2NativeCorpus(t *testing.T) {
	for _, tc := range []struct {
		stride int
		want   uint64
	}{
		{8, 0xb30392e88e291ed2},
		{64, 0x735fa3f800503c1b},
		{2048, 0x7857315bf447fd23},
	} {
		t.Run(fmt.Sprintf("stride%d", tc.stride), func(t *testing.T) {
			p1, p2 := make([]byte, 7*tc.stride+8), make([]byte, 7*tc.stride+8)
			state := uint32(0x264ab)
			next := func() byte {
				state ^= state << 13
				state ^= state >> 17
				state ^= state << 5
				return byte(state)
			}
			hash := uint64(14695981039346656037)
			var output [64]int16
			for iteration := 0; iteration < 1024; iteration++ {
				for i := range p1 {
					p1[i], p2[i] = next(), next()
				}
				WelsDctFourT4_sse2(output[:], p1, 0, int32(tc.stride), p2, 0, int32(tc.stride))
				for _, coefficient := range output {
					hash = (hash ^ uint64(byte(coefficient))) * 1099511628211
					hash = (hash ^ uint64(byte(uint16(coefficient)>>8))) * 1099511628211
				}
			}
			if hash != tc.want {
				t.Fatalf("native DCT corpus digest: got %016x, want %016x", hash, tc.want)
			}
		})
	}
}
