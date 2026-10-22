//go:build amd64

package encoder

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type intra4DirectionalFunc = func([]byte, int, []byte, int, int32)

var intra4DirectionalCases = []struct {
	name         string
	mode         int
	fast, scalar intra4DirectionalFunc
	raw          func(*byte, uint64)
	reads        func(int) []int
	packing      []int
}{
	{"HD", common.I4_PRED_HD, WelsI4x4LumaPredHD_sse2, WelsI4x4LumaPredHD_c, intra4PredHDSSE2,
		func(s int) []int { return []int{-s - 1, -1, s - 1, 2*s - 1, 3*s - 1, -s, 1 - s, 2 - s} }, []int{4, 3, 2, 1, 0, 5, 6, 7}},
	{"HU", common.I4_PRED_HU, WelsI4x4LumaPredHU_sse2, WelsI4x4LumaPredHU_c, intra4PredHUSSE2,
		func(s int) []int { return []int{-1, s - 1, 2*s - 1, 3*s - 1} }, []int{0, 1, 2, 3, 3, 3, 3, 3}},
	{"VR", common.I4_PRED_VR, WelsI4x4LumaPredVR_sse2, WelsI4x4LumaPredVR_c, intra4PredVRSSE2,
		func(s int) []int { return []int{-s - 1, -1, s - 1, 2*s - 1, -s, 1 - s, 2 - s, 3 - s} }, []int{3, 2, 1, 0, 4, 5, 6, 7}},
}

func TestIntra4DirectionalDispatch(t *testing.T) {
	var f, scalar SWelsFuncPtrList
	WelsInitIntraPredFuncs(&scalar, 0)
	for _, flags := range []uint32{0, common.WELS_CPU_SSE2, common.WELS_CPU_SSSE3, common.WELS_CPU_AVX2, common.WELS_CPU_SSE2 | common.WELS_CPU_AVX2, 0} {
		WelsInitIntraPredFuncs(&f, flags)
		for mode, want := range scalar.pfGetLumaI4x4Pred {
			for _, tc := range intra4DirectionalCases {
				if mode == tc.mode && flags&common.WELS_CPU_SSE2 != 0 {
					want = tc.fast
				}
			}
			if reflect.ValueOf(f.pfGetLumaI4x4Pred[mode]).Pointer() != reflect.ValueOf(want).Pointer() {
				t.Fatalf("flags%x mode%d: unexpected predictor", flags, mode)
			}
		}
	}
}

func TestIntra4DirectionalValues(t *testing.T) {
	rng := rand.New(rand.NewSource(0x264444))
	for _, tc := range intra4DirectionalCases {
		t.Run(tc.name, func(t *testing.T) {
			const off, stride = 41, 13
			positions := tc.reads(stride)
			check := func(values []byte) {
				ref := bytes.Repeat([]byte{0xa5}, 128)
				for i, v := range values {
					ref[off+positions[i]] = v
				}
				before := bytes.Clone(ref)
				var packed uint64
				for i, source := range tc.packing {
					packed |= uint64(values[source]) << (8 * i)
				}
				got, raw, want := bytes.Repeat([]byte{0x73}, 32), bytes.Repeat([]byte{0x73}, 32), bytes.Repeat([]byte{0x73}, 32)
				tc.fast(got, 7, ref, off, stride)
				tc.raw(&raw[7], packed)
				tc.scalar(want, 7, ref, off, stride)
				if !bytes.Equal(got, want) || !bytes.Equal(raw, want) || !bytes.Equal(ref, before) {
					t.Fatalf("samples %v: checked%v raw%v scalar%v or input changed", values, got, raw, want)
				}
			}
			values := make([]byte, len(positions))
			for bits := 0; bits < 1<<len(values); bits++ {
				for i := range values {
					values[i] = byte((bits>>i)&1) * 255
				}
				check(values)
			}
			for position := range values {
				for value := 0; value < 256; value++ {
					for i := range values {
						values[i] = byte(i*37 + 11)
					}
					values[position] = byte(value)
					check(values)
				}
			}
			for range 1000 {
				rng.Read(values)
				check(values)
			}
		})
	}
}

func intra4DirectionalPanic(call func()) (result string) {
	defer func() {
		if p := recover(); p != nil {
			result = fmt.Sprintf("%T: %v", p, p)
		}
	}()
	call()
	return
}

func TestIntra4DirectionalAliasesAndStrides(t *testing.T) {
	for _, tc := range intra4DirectionalCases {
		for _, stride := range []int32{-1984, -32, -3, -1, 0, 1, 2, 3, 4, 13, 32, 1984} {
			positions := tc.reads(int(stride))
			low, high := positions[0], positions[0]
			for _, at := range positions {
				low, high = min(low, at), max(high, at)
			}
			refOff := 32 - low
			for _, alias := range []bool{false, true} {
				for _, dstRelative := range []int{low - 8, low, -1, 0, high - 8, high, high + 1} {
					t.Run(fmt.Sprintf("%s/stride%d/alias%t/dst%d", tc.name, stride, alias, dstRelative), func(t *testing.T) {
						backing := make([]byte, high-low+128)
						for i := range backing {
							backing[i] = byte(i*73 + i*i + 17)
						}
						wantBacking := bytes.Clone(backing)
						gotDst, wantDst := bytes.Repeat([]byte{0x91}, len(backing)), bytes.Repeat([]byte{0x91}, len(backing))
						if alias {
							gotDst, wantDst = backing, wantBacking
						}
						dstOff := refOff + dstRelative
						visible := refOff + high + 1
						tc.fast(gotDst, dstOff, backing[:visible:visible], refOff, stride)
						tc.scalar(wantDst, dstOff, wantBacking[:visible:visible], refOff, stride)
						if !bytes.Equal(backing, wantBacking) || !bytes.Equal(gotDst, wantDst) {
							t.Fatal("alias/stride state differs")
						}
					})
				}
			}
		}
	}
}

func TestIntra4DirectionalFailureOrder(t *testing.T) {
	for _, tc := range intra4DirectionalCases {
		for _, stride := range []int32{-32, -1, 0, 1, 13, 32, math.MinInt32, math.MaxInt32} {
			for _, refOff := range []int{-1, 0, 1, 40, 80, math.MinInt, math.MaxInt - 3} {
				for _, refLen := range []int{0, 1, 39, 40, 41, 53, 64, 80, 127, 128} {
					for _, dstOff := range []int{-1, 0, 17, 18, 31, math.MinInt, math.MaxInt - 7} {
						for _, alias := range []bool{false, true} {
							gotRef, wantRef := make([]byte, 128), make([]byte, 128)
							for i := range gotRef {
								gotRef[i] = byte(i*31 + 7)
								wantRef[i] = gotRef[i]
							}
							gotDst, wantDst := bytes.Repeat([]byte{0x89}, 33), bytes.Repeat([]byte{0x89}, 33)
							if alias {
								gotDst, wantDst = gotRef[:33:33], wantRef[:33:33]
							}
							got := intra4DirectionalPanic(func() { tc.fast(gotDst, dstOff, gotRef[:refLen], refOff, stride) })
							want := intra4DirectionalPanic(func() { tc.scalar(wantDst, dstOff, wantRef[:refLen], refOff, stride) })
							if got != want || !bytes.Equal(gotRef, wantRef) || !bytes.Equal(gotDst, wantDst) {
								t.Fatalf("%s stride%d ref%d/%d dst%d alias%t: panic %q/%q or partial output differs", tc.name, stride, refOff, refLen, dstOff, alias, got, want)
							}
						}
					}
				}
			}
		}
		for _, ref := range [][]byte{nil, bytes.Repeat([]byte{99}, 128)} {
			got := intra4DirectionalPanic(func() { tc.fast(nil, 0, ref, 40, 13) })
			want := intra4DirectionalPanic(func() { tc.scalar(nil, 0, ref, 40, 13) })
			if got != want {
				t.Fatalf("%s nil destination with reference length%d: panic %q/%q", tc.name, len(ref), got, want)
			}
		}
		// Legacy destination slicing permits extending a short visible length
		// to its capacity. Keep that exact contract after all reference reads.
		for visible := 0; visible <= 16; visible++ {
			got, want := make([]byte, 32), make([]byte, 32)
			ref := bytes.Repeat([]byte{99}, 128)
			tc.fast(got[:visible], 1, ref, 40, 13)
			tc.scalar(want[:visible], 1, ref, 40, 13)
			if !bytes.Equal(got, want) {
				t.Fatalf("%s destination visible%d differs", tc.name, visible)
			}
		}
	}
}
