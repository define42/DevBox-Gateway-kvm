//go:build amd64

package encoder

import (
	"fmt"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

func TestDctLumaMbFastEligibility(t *testing.T) {
	var coefficients [256]int16
	pixels := make([]byte, 256)
	if handled := dctLumaMbFast(coefficients[:], pixels, 0, 16, pixels, 0, WelsDctFourT4_sse2); handled != cpu.X86.HasAVX2 {
		t.Fatalf("builtin handled=%t, AVX2=%t", handled, cpu.X86.HasAVX2)
	}
	for _, transform := range []PDctFunc{nil, WelsDctFourT4_c,
		func(d []int16, a []byte, ao int, as int32, b []byte, bo int, bs int32) {
			WelsDctFourT4_sse2(d, a, ao, as, b, bo, bs)
		}} {
		if dctLumaMbFast(coefficients[:], pixels, 0, 16, pixels, 0, transform) {
			t.Fatal("non-builtin callback accepted")
		}
	}
}

func TestDctLumaMbFastRejectedSpansDoNotWrite(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, tc := range []struct {
		name                               string
		coeffLen, sourceLen, predictionLen int
		sourceOffset, predictionOffset     int
		stride                             int32
	}{
		{"short-output", 255, 256, 256, 0, 0, 16},
		{"short-source", 256, 255, 256, 0, 0, 16},
		{"short-prediction", 256, 256, 255, 0, 0, 16},
		{"negative-stride", 256, 256, 256, 240, 0, -16},
		{"negative-source-offset", 256, 256, 256, -1, 0, 16},
		{"negative-prediction-offset", 256, 256, 256, 0, -1, 16},
		{"source-offset-overflow", 256, 256, 256, int(^uint(0) >> 1), 0, 16},
		{"prediction-offset-overflow", 256, 256, 256, 0, int(^uint(0) >> 1), 16},
		{"large-stride", 256, 256, 256, 0, 0, 1<<31 - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got, want [256]int16
			for i := range got {
				got[i], want[i] = -12345, -12345
			}
			pixels := make([]byte, 256)
			if dctLumaMbFast(got[:tc.coeffLen], pixels[:tc.sourceLen], tc.sourceOffset, tc.stride,
				pixels[:tc.predictionLen], tc.predictionOffset, WelsDctFourT4_sse2) {
				t.Fatal("invalid span accepted")
			}
			if got != want {
				t.Fatal("rejected transform modified output")
			}
		})
	}
}

func TestWelsDctMbAliasesPreserveAccessOrder(t *testing.T) {
	t.Parallel()
	for _, stride := range []int{0, 1, 16, 31, -31} {
		for _, outputOffset := range []int{0, 16, 128, 300} {
			for _, input := range []int{1, 2, 3} {
				t.Run(fmt.Sprintf("stride%d/output%d/input%d", stride, outputOffset, input), func(t *testing.T) {
					var got, want [1024]int16
					gotBytes := unsafe.Slice((*byte)(unsafe.Pointer(&got[0])), len(got)*2)
					wantBytes := unsafe.Slice((*byte)(unsafe.Pointer(&want[0])), len(want)*2)
					external := make([]byte, len(gotBytes))
					for i := range gotBytes {
						gotBytes[i], external[i] = byte(i*71+29), byte(i*43+17)
					}
					copy(wantBytes, gotBytes)
					gotSource, gotPred, wantSource, wantPred := external, external, external, external
					if input&1 != 0 {
						gotSource, wantSource = gotBytes, wantBytes
					}
					if input&2 != 0 {
						gotPred, wantPred = gotBytes, wantBytes
					}
					offset := 32
					if stride < 0 {
						offset -= 15 * stride
					}
					referenceDctLumaMb(want[outputOffset:], wantSource, offset, int32(stride), wantPred, 32, WelsDctFourT4_sse2)
					WelsDctMb(got[outputOffset:], gotSource, offset, int32(stride), gotPred, 32, WelsDctFourT4_sse2)
					if got != want {
						t.Fatal("output alias changed the original per-quadrant access order")
					}
				})
			}
		}
	}
}
