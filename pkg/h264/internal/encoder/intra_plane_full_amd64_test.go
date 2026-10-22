//go:build amd64

package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

// Independent widened reference: sum weighted differences around the unused
// center, then evaluate the plane at each output coordinate.
func planeFullReference(dst []byte, dstOff int, ref []byte, refOff int, stride int32, size int) {
	s, center := int(stride), size/2-1
	var horizontal, vertical int64
	for distance := 1; distance <= size/2; distance++ {
		horizontal += int64(distance) * (int64(ref[refOff-s+center+distance]) - int64(ref[refOff-s+center-distance]))
		vertical += int64(distance) * (int64(ref[refOff-1+(center+distance)*s]) - int64(ref[refOff-1+(center-distance)*s]))
	}
	if size == 16 {
		horizontal, vertical = (horizontal*5+32)>>6, (vertical*5+32)>>6
	} else {
		horizontal, vertical = (horizontal*17+16)>>5, (vertical*17+16)>>5
	}
	base := 16 * (int64(ref[refOff-s+size-1]) + int64(ref[refOff-1+(size-1)*s]))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			value := (base + horizontal*int64(x-center) + vertical*int64(y-center) + 16) >> 5
			if value < 0 {
				value = 0
			} else if value > 255 {
				value = 255
			}
			dst[dstOff+y*size+x] = byte(value)
		}
	}
}

func planeFullPredict(size int) PGetIntraPredFunc {
	if size == 16 {
		return intra16Plane
	}
	return intra8Plane
}

func planeFullLayout(size int, stride int32) (off, length int) {
	first, last, s := 0, 0, int(stride)
	for i := -1; i < size; i++ {
		first = min(first, i-s, i*s-1)
		last = max(last, i-s, i*s-1)
	}
	return 3 - first, 3 + last - first + 1
}

func TestIntraPlaneFullStrideAndPatternParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x26491a))
	for _, size := range []int{8, 16} {
		for _, stride := range []int32{-1984, -33, -1, 0, 1, 2, 7, 16, 17, 33, 1984} {
			t.Run(fmt.Sprintf("size%d/stride%d", size, stride), func(t *testing.T) {
				off, length := planeFullLayout(size, stride)
				ref := make([]byte, length)
				for pattern := 0; pattern < 40; pattern++ {
					for i := range ref {
						switch pattern {
						case 0, 1, 2:
							ref[i] = []byte{0, 128, 255}[pattern]
						case 3:
							ref[i] = byte((i & 1) * 255)
						case 4:
							ref[i] = byte(i * 17)
						default:
							ref[i] = byte(rng.Uint32())
						}
					}
					before := bytes.Clone(ref)
					want := bytes.Repeat([]byte{0xa5}, size*size+10)
					got := bytes.Clone(want)
					planeFullReference(want, 5, ref, off, stride, size)
					planeFullPredict(size)(got, 5, ref, off, stride)
					if !bytes.Equal(got, want) || !bytes.Equal(ref, before) {
						t.Fatalf("pattern%d: pixels, canaries, or source differ", pattern)
					}
				}
			})
		}
	}
}

func TestIntraPlaneFullWeightedLanesAndExtremes(t *testing.T) {
	for _, size := range []int{8, 16} {
		const stride = 33
		off, length := planeFullLayout(size, stride)
		ref := make([]byte, length)
		check := func(label string) {
			t.Helper()
			want, got := make([]byte, size*size), make([]byte, size*size)
			planeFullReference(want, 0, ref, off, stride, size)
			planeFullPredict(size)(got, 0, ref, off, stride)
			if !bytes.Equal(got, want) {
				t.Fatalf("size%d %s: prediction differs", size, label)
			}
		}
		for value := 0; value < 256; value++ {
			for i := range ref {
				ref[i] = byte(value)
			}
			check(fmt.Sprintf("constant%d", value))
		}
		for _, background := range []byte{0, 128, 255} {
			for i := range ref {
				ref[i] = background
			}
			for lane := -1; lane < size; lane++ {
				for _, index := range []int{off - stride + lane, off - 1 + lane*stride} {
					for _, value := range []byte{0, 1, 127, 128, 254, 255} {
						ref[index] = value
						check(fmt.Sprintf("lane%d value%d", lane, value))
					}
					ref[index] = background
				}
			}
		}
		for signs := 0; signs < 4; signs++ {
			for i := -1; i < size; i++ {
				top, left := byte(0), byte(0)
				if (i >= size/2) != (signs&1 != 0) {
					top = 255
				}
				if (i >= size/2) != (signs&2 != 0) {
					left = 255
				}
				ref[off-stride+i], ref[off-1+i*stride] = top, left
			}
			check(fmt.Sprintf("extreme signs%d", signs))
		}
	}
}

func TestIntraPlaneFullAliases(t *testing.T) {
	for _, size := range []int{8, 16} {
		for _, stride := range []int32{-33, -1, 0, 1, 2, 17, 33, 1984} {
			off, length := planeFullLayout(size, stride)
			for _, dstOff := range []int{0, max(0, off-int(stride)-1), off - 1, off, length - 1} {
				want := make([]byte, max(length, dstOff+size*size)+7)
				for i := range want {
					want[i] = byte(i*137 + 11)
				}
				got := bytes.Clone(want)
				planeFullReference(want, dstOff, want[:length], off, stride, size)
				planeFullPredict(size)(got, dstOff, got[:length], off, stride)
				if !bytes.Equal(got, want) {
					t.Fatalf("size%d stride%d destination%d: aliased storage differs", size, stride, dstOff)
				}
			}
		}
	}
}

// Preserve the old SIMD wrapper exactly for invalid-input behavior; the C
// predictor has different partial destination writes on short output slices.
func planeFullLegacy(dst []byte, dstOff int, ref []byte, refOff int, stride int32, size int) {
	base, horizontal, vertical := intraPlaneCoefficients(ref, refOff, stride, size)
	intraPredPlaneSSE2(dst[dstOff:dstOff+size*size], base, horizontal, vertical, size)
}

func planeFullPanic(call func()) (message string) {
	defer func() {
		if value := recover(); value != nil {
			message = fmt.Sprintf("%T: %v", value, value)
		}
	}()
	call()
	return ""
}

func TestIntraPlaneFullInvalidInputParity(t *testing.T) {
	const stride = 33
	for _, size := range []int{8, 16} {
		off, length := planeFullLayout(size, stride)
		type shape struct {
			refOff, refLen, dstOff, dstLen int
			stride                         int32
		}
		var cases []shape
		for n := 0; n <= length; n++ {
			cases = append(cases, shape{off, n, 3, size*size + 3, stride})
		}
		for n := 0; n <= size*size+3; n++ {
			cases = append(cases, shape{off, length, 3, n, stride})
		}
		for _, badOff := range []int{-1, 0, int(^uint(0) >> 1), -int(^uint(0)>>1) - 1} {
			cases = append(cases, shape{badOff, length, 0, 0, stride}, shape{off, length, badOff, size * size, stride})
		}
		for _, badStride := range []int32{-2147483648, 2147483647, 0, -33} {
			cases = append(cases, shape{off, length, 3, size*size + 3, badStride})
		}
		for _, tc := range cases {
			var want [2048]byte
			for i := range want {
				want[i] = byte(i*137 + 11)
			}
			got := want
			wantPanic := planeFullPanic(func() { planeFullLegacy(want[:tc.dstLen], tc.dstOff, want[:tc.refLen], tc.refOff, tc.stride, size) })
			gotPanic := planeFullPanic(func() { planeFullPredict(size)(got[:tc.dstLen], tc.dstOff, got[:tc.refLen], tc.refOff, tc.stride) })
			if gotPanic != wantPanic || got != want {
				t.Fatalf("size%d case%+v: panic%q/%q or partial state differs", size, tc, gotPanic, wantPanic)
			}
		}
	}
}
