//go:build amd64

package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

func sampleSatdWrapperCases() []struct {
	width, height int
	fn            PSampleSadSatdCostFunc
} {
	return []struct {
		width, height int
		fn            PSampleSadSatdCostFunc
	}{
		{8, 8, WelsSampleSatd8x8_avx2},
		{16, 8, WelsSampleSatd16x8_avx2},
		{8, 16, WelsSampleSatd8x16_avx2},
		{16, 16, WelsSampleSatd16x16_avx2},
	}
}

func sampleSatdWrapperScalar(a []byte, aOff int, aStride int32, ref []byte, refOff int, refStride int32, width, height int) int32 {
	var cost int32
	for y := 0; y < height; y += 4 {
		for x := 0; x < width; x += 4 {
			cost += WelsSampleSatd4x4_c(a, aOff+y*int(aStride)+x, aStride,
				ref, refOff+y*int(refStride)+x, refStride)
		}
	}
	return cost
}

func TestSampleSatdWrapperParity(t *testing.T) {
	requireSampleAVX2(t)
	rng := rand.New(rand.NewSource(0x264517))
	for _, tc := range sampleSatdWrapperCases() {
		for _, strides := range [][2]int32{{32, 32}, {1984, 1984}, {-32, -32}, {-1984, 1984}, {32, -1984}, {0, 0}, {1, -1}, {7, 3}} {
			for _, alias := range []bool{false, true} {
				for pattern := range 4 {
					offsets := [2]int{5, 11}
					var spans [2]int
					for n, stride := range strides {
						if stride < 0 {
							offsets[n] -= (tc.height - 1) * int(stride)
						}
						spans[n] = offsets[n] + tc.width
						if stride > 0 {
							spans[n] += (tc.height - 1) * int(stride)
						}
					}
					a := make([]byte, max(spans[0], spans[1])+7)
					ref := make([]byte, len(a))
					for n, buf := range [][]byte{a, ref} {
						rng.Read(buf)
						for i := range buf {
							switch pattern {
							case 0:
								buf[i] = 0
							case 1:
								buf[i] = byte(n * 255)
							case 2:
								buf[i] = byte(((i + n) & 1) * 255)
							}
						}
					}
					if alias {
						ref = a
					}
					beforeA, beforeRef := bytes.Clone(a), bytes.Clone(ref)
					got := tc.fn(a, offsets[0], strides[0], ref, offsets[1], strides[1])
					legacy := sampleSatdBlockAVX2(a, offsets[0], strides[0], ref, offsets[1], strides[1], tc.width, tc.height)
					want := sampleSatdWrapperScalar(a, offsets[0], strides[0], ref, offsets[1], strides[1], tc.width, tc.height)
					if got != legacy || got != want || !bytes.Equal(a, beforeA) || !bytes.Equal(ref, beforeRef) {
						t.Fatalf("%dx%d strides=%v alias=%v pattern=%d: got=%d legacy=%d scalar=%d or input changed", tc.width, tc.height, strides, alias, pattern, got, legacy, want)
					}
				}
			}
		}
	}
}

type sampleSatdWrapperResult struct {
	cost  int32
	panic string
}

func sampleSatdWrapperCall(fn func() int32) (result sampleSatdWrapperResult) {
	defer func() {
		if value := recover(); value != nil {
			result.panic = fmt.Sprintf("%T: %v", value, value)
		}
	}()
	result.cost = fn()
	return result
}

func TestSampleSatdWrapperSpanParity(t *testing.T) {
	requireSampleAVX2(t)
	maxInt := int(^uint(0) >> 1)
	for _, tc := range sampleSatdWrapperCases() {
		t.Run(fmt.Sprintf("%dx%d", tc.width, tc.height), func(t *testing.T) {
			compare := func(name string, a []byte, aOff int, aStride int32, ref []byte, refOff int, refStride int32, wantPanic bool) {
				t.Helper()
				beforeA, beforeRef := bytes.Clone(a[:cap(a)]), bytes.Clone(ref[:cap(ref)])
				got := sampleSatdWrapperCall(func() int32 { return tc.fn(a, aOff, aStride, ref, refOff, refStride) })
				want := sampleSatdWrapperCall(func() int32 {
					return sampleSatdBlockAVX2(a, aOff, aStride, ref, refOff, refStride, tc.width, tc.height)
				})
				if got != want || (got.panic != "") != wantPanic {
					t.Fatalf("%s: got=%+v legacy=%+v wantPanic=%v", name, got, want, wantPanic)
				}
				if !bytes.Equal(a[:cap(a)], beforeA) || !bytes.Equal(ref[:cap(ref)], beforeRef) {
					t.Fatalf("%s: input backing changed", name)
				}
			}
			for _, stride := range []int32{int32(tc.width + 3), -int32(tc.width + 3), 0, 1, -1} {
				off, span := 0, (tc.height-1)*int(stride)
				if span < 0 {
					off, span = -span, -span
				}
				span += tc.width
				a, ref := make([]byte, span+16), make([]byte, span+16)
				for i := range a {
					a[i], ref[i] = byte(i*17+3), byte(i*31+5)
				}
				// Keep excess capacity while varying only the visible length.
				for _, length := range []int{0, span - 1, span, span + 1} {
					compare(fmt.Sprintf("stride%d/sourceLen%d", stride, length), a[:length], off, stride, ref[:span], off, stride, length < span)
					compare(fmt.Sprintf("stride%d/referenceLen%d", stride, length), a[:span], off, stride, ref[:length], off, stride, length < span)
				}
				compare("offsetBeforeSlice", a, -1, stride, ref, off, stride, true)
				compare("offsetAfterSlice", a, off, stride, ref, len(ref), stride, true)
			}
			valid := make([]byte, tc.width*tc.height)
			for _, malformed := range []struct {
				off    int
				stride int32
			}{
				{maxInt, int32(tc.width)}, {-maxInt - 1, int32(tc.width)},
				{0, 1<<31 - 1}, {0, -1 << 31},
				{maxInt, 1<<31 - 1}, {-maxInt - 1, -1 << 31},
			} {
				compare(fmt.Sprintf("malformedSource/%v", malformed), valid, malformed.off, malformed.stride, valid, 0, int32(tc.width), true)
				compare(fmt.Sprintf("malformedReference/%v", malformed), valid, 0, int32(tc.width), valid, malformed.off, malformed.stride, true)
			}
			compare("nilSource", nil, 0, 0, valid, 0, int32(tc.width), true)
			compare("nilReference", valid, 0, int32(tc.width), nil, 0, 0, true)
			compare("bothNil", nil, 0, 0, nil, 0, 0, true)
		})
	}
}
