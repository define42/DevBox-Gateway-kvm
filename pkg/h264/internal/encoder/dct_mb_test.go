package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"slices"
	"testing"
)

// Keep the original complete caller as an independent order/partial-write
// oracle. Testing only the new assembly would miss callback dispatch failures.
func referenceDctLumaMb(coeff []int16, source []byte, sourceOffset int, stride int32,
	prediction []byte, predictionOffset int, transform PDctFunc) {
	transform(coeff, source, sourceOffset, stride, prediction, predictionOffset, 16)
	transform(coeff[64:], source, sourceOffset+8, stride, prediction, predictionOffset+8, 16)
	transform(coeff[128:], source, sourceOffset+8*int(stride), stride, prediction, predictionOffset+128, 16)
	transform(coeff[192:], source, sourceOffset+8*int(stride)+8, stride, prediction, predictionOffset+136, 16)
}

func TestWelsDctMbParity(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(0x16dc7))
	for iteration := 0; iteration < 5000; iteration++ {
		stride := rng.Intn(97)
		if iteration%5 == 0 {
			stride = -stride
		}
		sourceOffset, predictionOffset := rng.Intn(32), rng.Intn(32)
		span := 15 * stride
		if span < 0 {
			sourceOffset -= span
			span = 0
		}
		source := make([]byte, sourceOffset+span+16)
		prediction := make([]byte, predictionOffset+256)
		_, _ = rng.Read(source)
		_, _ = rng.Read(prediction)
		originalSource, originalPrediction := bytes.Clone(source), bytes.Clone(prediction)
		var want [256]int16
		var backing [260]int16
		for i := range backing {
			backing[i] = -12345
		}
		got := backing[2:258:258]
		referenceDctLumaMb(want[:], source, sourceOffset, int32(stride), prediction, predictionOffset, WelsDctFourT4_c)
		WelsDctMb(got, source, sourceOffset, int32(stride), prediction, predictionOffset, WelsDctFourT4_sse2)
		if !slices.Equal(got, want[:]) {
			t.Fatalf("iteration %d stride %d: coefficients differ", iteration, stride)
		}
		if backing[0] != -12345 || backing[1] != -12345 || backing[258] != -12345 || backing[259] != -12345 {
			t.Fatal("macroblock transform wrote outside coefficient output")
		}
		if !bytes.Equal(source, originalSource) || !bytes.Equal(prediction, originalPrediction) {
			t.Fatal("macroblock transform modified source pixels")
		}
	}
}

func TestWelsDctMbPreservesCallbacks(t *testing.T) {
	for _, scalar := range []bool{false, true} {
		t.Run(fmt.Sprintf("scalar=%t", scalar), func(t *testing.T) {
			transform := PDctFunc(WelsDctFourT4_sse2)
			if scalar {
				transform = WelsDctFourT4_c
			}
			var got, want [256]int16
			source, prediction := make([]byte, 5+15*31+16), make([]byte, 3+256)
			for i := range source {
				source[i] = byte(i*37 + 13)
			}
			referenceDctLumaMb(want[:], source, 5, 31, prediction, 3, transform)
			calls := 0
			wrapper := func(d []int16, src []byte, srcOff int, srcStride int32, pred []byte, predOff int, predStride int32) {
				x, y := (calls&1)*8, (calls>>1)*8
				if len(d) != 256-calls*64 || srcOff != 5+y*31+x || predOff != 3+y*16+x ||
					srcStride != 31 || predStride != 16 || &src[0] != &source[0] || &pred[0] != &prediction[0] {
					t.Fatal("callback arguments or order changed")
				}
				calls++
				transform(d, src, srcOff, srcStride, pred, predOff, predStride)
			}
			WelsDctMb(got[:], source, 5, 31, prediction, 3, wrapper)
			if calls != 4 || got != want {
				t.Fatalf("callback count %d or coefficient output changed", calls)
			}
		})
	}
	// Custom transforms are allowed to interpret inputs themselves, including
	// accepting slices that the builtin kernel would reject.
	var got [256]int16
	calls := 0
	WelsDctMb(got[:], nil, -7, -31, nil, -13,
		func(d []int16, _ []byte, _ int, _ int32, _ []byte, _ int, _ int32) {
			calls++
			d[0] = int16(calls)
		})
	if calls != 4 || got[0] != 1 || got[64] != 2 || got[128] != 3 || got[192] != 4 {
		t.Fatal("custom transform was bypassed or its inputs were rejected")
	}
}

func TestWelsDctMbBoundsPreservePartialWrites(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		coeffLen, sourceLen, predLen   int
		sourceOffset, predictionOffset int
		stride                         int32
	}{
		{"short-output", 255, 256, 256, 0, 0, 16},
		{"short-source", 256, 255, 256, 0, 0, 16},
		{"short-prediction", 256, 256, 255, 0, 0, 16},
		{"source-negative-offset", 256, 256, 256, -1, 0, 16},
		{"prediction-negative-offset", 256, 256, 256, 0, -1, 16},
		{"source-overflow-offset", 256, 256, 256, int(^uint(0) >> 1), 0, 16},
		{"prediction-overflow-offset", 256, 256, 256, 0, int(^uint(0) >> 1), 16},
		{"large-stride", 256, 256, 256, 0, 0, 1<<31 - 1},
		{"empty-output", 0, 256, 256, 0, 0, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got, want [256]int16
			source, prediction := make([]byte, 256), make([]byte, 256)
			for i := range source {
				source[i], prediction[i] = byte(i*17+23), byte(i*37+61)
			}
			call := func(fn func([]int16, []byte, int, int32, []byte, int, PDctFunc), coeff []int16) (panicked bool) {
				defer func() { panicked = recover() != nil }()
				fn(coeff[:tc.coeffLen], source[:tc.sourceLen], tc.sourceOffset, tc.stride,
					prediction[:tc.predLen], tc.predictionOffset, WelsDctFourT4_sse2)
				return false
			}
			wantPanic := call(referenceDctLumaMb, want[:])
			gotPanic := call(WelsDctMb, got[:])
			if !gotPanic || gotPanic != wantPanic || got != want {
				t.Fatalf("panic or writes changed: actual panic=%t reference panic=%t", gotPanic, wantPanic)
			}
		})
	}
}
