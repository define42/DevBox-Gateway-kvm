//go:build amd64

package processing

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"golang.org/x/sys/cpu"
)

func requireVAAAVX2(t *testing.T) {
	t.Helper()
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
}

func TestVAAAVX2Dispatch(t *testing.T) {
	for _, flags := range []uint32{0, common.WELS_CPU_SSE2, common.WELS_CPU_AVX2, common.WELS_CPU_SSE2 | common.WELS_CPU_AVX2} {
		t.Run(fmt.Sprintf("flags%x", flags), func(t *testing.T) {
			c := NewCVAACalculation(int32(flags))
			want := VAACalcSadFunc(VAACalcSad_c)
			if flags&common.WELS_CPU_SSE2 != 0 {
				want = VAACalcSad_sse2
			}
			if flags&common.WELS_CPU_AVX2 != 0 {
				want = VAACalcSad_avx2
			}
			if reflect.ValueOf(c.m_sVaaFuncs.pfVAACalcSad).Pointer() != reflect.ValueOf(want).Pointer() {
				t.Fatal("unexpected SAD dispatch")
			}
			if reflect.ValueOf(c.m_sVaaFuncs.pfVAACalcSadBgd).Pointer() != reflect.ValueOf(VAACalcSadBgd_c).Pointer() ||
				reflect.ValueOf(c.m_sVaaFuncs.pfVAACalcSadSsd).Pointer() != reflect.ValueOf(VAACalcSadSsd_c).Pointer() ||
				reflect.ValueOf(c.m_sVaaFuncs.pfVAACalcSadVar).Pointer() != reflect.ValueOf(VAACalcSadVar_c).Pointer() {
				t.Fatal("inactive non-SAD dispatch changed")
			}
		})
	}
}

func TestVAAAVX2RowParity(t *testing.T) {
	requireVAAAVX2(t)
	rng := rand.New(rand.NewSource(9711))
	for _, blocks := range []int{0, 1, 2, 3, 5, 7, 80, 119, 120} {
		for _, stride := range []int{0, 1, 17, 1984} {
			t.Run(fmt.Sprintf("blocks%d/stride%d", blocks, stride), func(t *testing.T) {
				span := 15*stride + blocks*16
				cur, ref := make([]byte, max(1, span)), make([]byte, max(1, span))
				rng.Read(cur)
				rng.Read(ref)
				got, old, want := make([][4]int32, blocks+2), make([][4]int32, blocks+2), make([][4]int32, blocks+2)
				for i := range got {
					got[i] = [4]int32{11, 13, 17, 19}
					old[i] = got[i]
					want[i] = got[i]
				}
				gc := vaaSadRowAVX2(&cur[0], &ref[0], stride, &got[1], blocks)
				oc := vaaSadRowSSE2(cur, ref, stride, old[1:1+blocks])
				var wc int32
				VAACalcSad_c(cur, 0, ref, 0, int32(blocks*16), 16, int32(stride), &wc, want[1:1+blocks])
				if gc != oc || gc != wc || !slices.Equal(got, old) || !slices.Equal(got, want) {
					t.Fatalf("row sum %d/%d/%d or quadrants differ", gc, oc, wc)
				}
			})
		}
	}
}

func TestVAAAVX2CompleteParity(t *testing.T) {
	for _, tc := range []struct {
		width, height, stride int32
		offset                int
	}{
		{16, 16, 16, 0}, {32, 16, 37, 3}, {48, 32, 65, 1}, {80, 48, 93, 7}, {304, 320, 320, 11}, {1280, 720, 1344, 13}, {1920, 1088, 1984, 17},
		{48, 32, 0, 5}, {48, 32, 1, 3}, {35, 33, 51, 7},
	} {
		for _, pattern := range []string{"random", "equal", "extreme", "checkerboard"} {
			t.Run(fmt.Sprintf("%dx%d/stride%d/%s", tc.width, tc.height, tc.stride, pattern), func(t *testing.T) {
				span := tc.offset + int(tc.stride)*int(tc.height) + int(tc.width) + 32
				cur, ref := make([]byte, span), make([]byte, span)
				rng := rand.New(rand.NewSource(17))
				rng.Read(cur)
				rng.Read(ref)
				for i := range cur {
					switch pattern {
					case "equal":
						ref[i] = cur[i]
					case "extreme":
						cur[i] = 0
						ref[i] = 255
					case "checkerboard":
						cur[i] = byte(i&1) * 255
						ref[i] = 255 - cur[i]
					}
				}
				cb, rb := slices.Clone(cur), slices.Clone(ref)
				count := int(tc.width>>4) * int(tc.height>>4)
				got, want, old := make([][4]int32, count+1), make([][4]int32, count+1), make([][4]int32, count+1)
				got[count], want[count], old[count] = [4]int32{1, 2, 3, 4}, [4]int32{1, 2, 3, 4}, [4]int32{1, 2, 3, 4}
				var gc, wc, oc int32
				VAACalcSad_avx2(cur, tc.offset, ref, tc.offset, tc.width, tc.height, tc.stride, &gc, got)
				VAACalcSad_sse2(cur, tc.offset, ref, tc.offset, tc.width, tc.height, tc.stride, &oc, old)
				VAACalcSad_c(cur, tc.offset, ref, tc.offset, tc.width, tc.height, tc.stride, &wc, want)
				if gc != oc || gc != wc || !slices.Equal(got, want) || !slices.Equal(got, old) || !bytes.Equal(cur, cb) || !bytes.Equal(ref, rb) {
					t.Fatalf("sum %d/%d/%d, quadrants or input bytes differ", gc, oc, wc)
				}
			})
		}
	}
}

func TestVAAAVX2FrameAndRowOverflow(t *testing.T) {
	for _, size := range [][2]int32{{528384, 16}, {2048, 4128}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			// Stride zero repeats a single row, reaching int32 overflow with
			// small backing allocations and exactly the same number of SADs.
			cur, ref := make([]byte, int(size[0])), make([]byte, int(size[0]))
			for i := range ref {
				ref[i] = 255
			}
			count := int(size[0]>>4) * int(size[1]>>4)
			got, old := make([][4]int32, count), make([][4]int32, count)
			var gc, oc int32
			VAACalcSad_avx2(cur, 0, ref, 0, size[0], size[1], 0, &gc, got)
			VAACalcSad_sse2(cur, 0, ref, 0, size[0], size[1], 0, &oc, old)
			want := int32(int64(size[0]) * int64(size[1]) * 255)
			if gc != oc || gc != want || gc >= 0 || !slices.Equal(got, old) {
				t.Fatalf("wrapped sum %d/%d want %d", gc, oc, want)
			}
			for _, mb := range got {
				if mb != [4]int32{16320, 16320, 16320, 16320} {
					t.Fatal("quadrant overflow")
				}
			}
		})
	}
}

func vaaAVX2Panic(call func()) (message string) {
	defer func() {
		if v := recover(); v != nil {
			message = fmt.Sprintf("%T: %v", v, v)
		}
	}()
	call()
	return
}

func TestVAAAVX2PartialFailureAndAliases(t *testing.T) {
	for _, scenario := range []string{"normal", "sadCur", "sadCurNextMB", "sadCurFutureRow", "sadRef", "frameCur", "frameRef", "frameSadFirst", "frameSadSecondMB", "frameSadLast", "sharedPixels", "zeroStride", "overlapStride", "negativeStride", "maxStride", "negativeCur", "negativeRef", "maxCur", "maxRef", "shortCur", "shortRef", "shortSad", "nilFrame", "zeroWidth", "negativeHeight", "partial", "partialShortSad", "partialShortCur"} {
		for _, masked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/masked%t", scenario, masked), func(t *testing.T) {
				var storage [2][]uint64
				var panicText [2]string
				for route, calc := range []VAACalcSadFunc{VAACalcSad_avx2, VAACalcSad_sse2} {
					storage[route] = make([]uint64, 4096)
					data := unsafe.Slice((*byte)(unsafe.Pointer(&storage[route][0])), 32768)
					for i := range data {
						data[i] = byte(i*37 + 11)
					}
					cur, ref := data[512:512+4096], data[8192:8192+4096]
					co, ro := 0, 0
					out := unsafe.Slice((*[4]int32)(unsafe.Pointer(&data[16384])), 16)
					frame := (*int32)(unsafe.Pointer(&data[30000]))
					width, height, stride := int32(48), int32(48), int32(64)
					switch scenario {
					case "sadCur":
						out = unsafe.Slice((*[4]int32)(unsafe.Pointer(&cur[0])), 16)
					case "sadCurNextMB":
						out = unsafe.Slice((*[4]int32)(unsafe.Pointer(&cur[16])), 16)
					case "sadCurFutureRow":
						out = unsafe.Slice((*[4]int32)(unsafe.Pointer(&cur[16*64])), 16)
					case "sadRef":
						out = unsafe.Slice((*[4]int32)(unsafe.Pointer(&ref[0])), 16)
					case "frameCur":
						frame = (*int32)(unsafe.Pointer(&cur[16*64]))
					case "frameRef":
						frame = (*int32)(unsafe.Pointer(&ref[16*64]))
					case "frameSadFirst":
						frame = &out[0][0]
					case "frameSadSecondMB":
						frame = &out[1][2]
					case "frameSadLast":
						frame = &out[8][3]
					case "sharedPixels":
						ref = cur
					case "zeroStride":
						stride = 0
					case "overlapStride":
						stride = 1
					case "negativeStride":
						stride = -1
					case "maxStride":
						stride = math.MaxInt32
					case "negativeCur":
						co = -1
					case "negativeRef":
						ro = -1
					case "maxCur":
						co = math.MaxInt
					case "maxRef":
						ro = math.MaxInt
					case "shortCur":
						cur = cur[:47*64+47]
					case "shortRef":
						ref = ref[:47*64+47]
					case "shortSad":
						out = out[:8]
					case "nilFrame":
						frame = nil
					case "zeroWidth":
						width = 0
					case "negativeHeight":
						height = -1
					case "partial":
						width = 35
						height = 33
					case "partialShortSad":
						width = 35
						height = 33
						out = out[:3]
					case "partialShortCur":
						width = 35
						height = 33
						cur = cur[:30*64+32]
					}
					if frame != nil {
						*frame = 12345
					}
					panicText[route] = vaaAVX2Panic(func() {
						if masked {
							mask := []bool{false, false, true, true, false, false, false, true, false}
							vaaCalcSadMasked(calc, cur, co, ref, ro, width, height, stride, frame, out, mask)
						} else {
							calc(cur, co, ref, ro, width, height, stride, frame, out)
						}
					})
				}
				if panicText[0] != panicText[1] || !slices.Equal(storage[0], storage[1]) {
					t.Fatalf("partial state/panic differs %q/%q", panicText[0], panicText[1])
				}
			})
		}
	}
}

func TestVAAAVX2StrategyAndMasks(t *testing.T) {
	requireVAAAVX2(t)
	for _, mode := range []string{"FullNoise", "FullEqual", "AllChangedMask", "AlternatingMask", "SparseOddRun", "AllUnchangedMask"} {
		t.Run(mode, func(t *testing.T) {
			got, want := newVAAActiveBenchmarkFixture(80, 48, 93, mode), newVAAActiveBenchmarkFixture(80, 48, 93, mode)
			want.calc.m_sVaaFuncs.pfVAACalcSad = VAACalcSad_sse2
			for iteration := range 2 {
				if iteration == 0 {
					got.calc.Set(0, &got.param)
					want.calc.Set(0, &want.param)
				}
				gr, wr := got.calc.Process(0, &got.current, &got.reference), want.calc.Process(0, &want.current, &want.reference)
				if gr != wr || got.result.IFrameSad != want.result.IFrameSad || !slices.Equal(got.result.PSad8x8, want.result.PSad8x8) || got.calc.m_sCalcParam.UnchangedMacroblocks != nil {
					t.Fatal("full strategy state or borrowed mask differs")
				}
			}
			allocs := testing.AllocsPerRun(100, func() { got.calc.Set(0, &got.param); got.calc.Process(0, &got.current, &got.reference) })
			if allocs != 0 {
				t.Fatalf("VAA strategy allocates %.0f times", allocs)
			}
		})
	}
}
