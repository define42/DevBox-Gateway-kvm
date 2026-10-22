//go:build amd64

package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestHalfSatdPairRawParity(t *testing.T) {
	requireSampleAVX2(t)
	rng := rand.New(rand.NewSource(0x2642))
	for _, shape := range sampleSatdWrapperCases() {
		w, h := shape.width, shape.height
		for _, strides := range [][2]int{{32, 32}, {2048, 31}, {0, 0}, {3, 7}, {-31, 23}, {23, -31}, {-31, -31}} {
			for pattern := range 32 {
				a, aOff := sampleFourPlane(rng, w, h, strides[0], 0, pattern)
				first, firstOff := sampleFourPlane(rng, w, h, strides[1], 1, pattern)
				second, secondOff := sampleFourPlane(rng, w, h, strides[1], 2, pattern)
				before := [3][]byte{bytes.Clone(a), bytes.Clone(first), bytes.Clone(second)}
				got0, got1 := satdTwoAVX2(&a[aOff], strides[0], &first[firstOff], &second[secondOff], strides[1], w, h)
				want0 := sampleSatdWrapperScalar(a, aOff, int32(strides[0]), first, firstOff, int32(strides[1]), w, h)
				want1 := sampleSatdWrapperScalar(a, aOff, int32(strides[0]), second, secondOff, int32(strides[1]), w, h)
				old0 := shape.fn(a, aOff, int32(strides[0]), first, firstOff, int32(strides[1]))
				old1 := shape.fn(a, aOff, int32(strides[0]), second, secondOff, int32(strides[1]))
				if got0 != want0 || got1 != want1 || got0 != old0 || got1 != old1 ||
					!bytes.Equal(a, before[0]) || !bytes.Equal(first, before[1]) || !bytes.Equal(second, before[2]) {
					t.Fatalf("%dx%d strides=%v pattern%d: got=(%d,%d) scalar=(%d,%d) old=(%d,%d), or input changed", w, h, strides, pattern, got0, got1, want0, want1, old0, old1)
				}
				a = append(a, 99)
				got0, got1 = satdTwoAVX2(&a[aOff], strides[0], &a[aOff], &a[aOff+1], strides[0], w, h)
				want1 = sampleSatdWrapperScalar(a, aOff, int32(strides[0]), a, aOff+1, int32(strides[0]), w, h)
				if got0 != 0 || got1 != want1 {
					t.Fatalf("alias: got=(%d,%d), want=(0,%d)", got0, got1, want1)
				}
			}
		}
		enc, first, second := make([]byte, w*h), make([]byte, w*h), make([]byte, w*h)
		for y := range h {
			for x := range w {
				first[y*w+x], second[y*w+(x+1)%w] = 255, 83
				got0, got1 := satdTwoAVX2(&enc[0], w, &first[0], &second[0], w, w, h)
				want0 := sampleSatdWrapperScalar(enc, 0, int32(w), first, 0, int32(w), w, h)
				want1 := sampleSatdWrapperScalar(enc, 0, int32(w), second, 0, int32(w), w, h)
				if got0 != want0 || got1 != want1 {
					t.Fatalf("%dx%d impulse=%d,%d: got=(%d,%d), want=(%d,%d)", w, h, x, y, got0, got1, want0, want1)
				}
				first[y*w+x], second[y*w+(x+1)%w] = 0, 0
			}
		}
	}
}

func TestHalfSatdPairExactSpans(t *testing.T) {
	requireSampleAVX2(t)
	for _, shape := range sampleSatdWrapperCases() {
		for _, secondOff := range []int{1, ME_REFINE_BUF_STRIDE} {
			enc := make([]byte, (shape.height-1)*31+shape.width+3)
			prediction := make([]byte, (shape.height-1)*ME_REFINE_BUF_STRIDE+shape.width+secondOff)
			for i := range prediction {
				prediction[i] = byte(i*37 + 3)
			}
			first, second, ok := sampleHalfSatdPair(enc, 3, 31, prediction, secondOff, int32(shape.width), int32(shape.height))
			want0 := shape.fn(enc, 3, 31, prediction, 0, ME_REFINE_BUF_STRIDE)
			want1 := shape.fn(enc, 3, 31, prediction, secondOff, ME_REFINE_BUF_STRIDE)
			if !ok || first != want0 || second != want1 {
				t.Fatalf("exact span %dx%d secondOff%d: (%d,%d,%v), want(%d,%d)", shape.width, shape.height, secondOff, first, second, ok, want0, want1)
			}
			for _, tc := range []struct {
				enc, pred []byte
				off       int
				stride    int32
			}{
				{enc[:len(enc)-1], prediction, 3, 31},
				{enc, prediction[:len(prediction)-1], 3, 31},
				{nil, prediction, 0, 31},
				{enc, prediction, -1, 31},
				{enc, prediction, 3, -31},
			} {
				if _, _, ok := sampleHalfSatdPair(tc.enc, tc.off, tc.stride, tc.pred, secondOff, int32(shape.width), int32(shape.height)); ok {
					t.Fatal("invalid or backwards span did not request legacy fallback")
				}
			}
		}
	}
}

func TestHalfSatdPairDispatch(t *testing.T) {
	var funcs SWelsFuncPtrList
	for _, flags := range []uint32{0, common.WELS_CPU_AVX2, common.WELS_CPU_SSE41 | common.WELS_CPU_SSSE3,
		common.WELS_CPU_SSE41 | common.WELS_CPU_SSSE3 | common.WELS_CPU_AVX2, 0} {
		WelsInitSampleSadFunc(&funcs, flags)
		s := &funcs.sSampleDealingFuncs
		s.pfMeCost = &s.pfSampleSatd
		want := flags == common.WELS_CPU_SSE41|common.WELS_CPU_SSSE3|common.WELS_CPU_AVX2
		if s.bSampleHalfSatdPair != want || sampleHalfSatdPairEnabled(s, BLOCK_8x8, 8, 8) != want {
			t.Fatalf("flags=%x capability=%v, want %v", flags, s.bSampleHalfSatdPair, want)
		}
		if sampleHalfSatdPairEnabled(s, BLOCK_8x4, 8, 4) || sampleHalfSatdPairEnabled(s, BLOCK_8x8, 16, 8) {
			t.Fatal("narrow/mismatched block opted in")
		}
		s.pfMeCost = &s.pfSampleSad
		if sampleHalfSatdPairEnabled(s, BLOCK_8x8, 8, 8) {
			t.Fatal("SAD metric opted in")
		}
	}
}

type halfPairOutcome struct {
	mv                      SMVUnitXY
	cost                    uint32
	pred, scratch, enc, ref []byte
	entryEnc, entryRef      []byte
	halfBuffers             [3][]byte
	bufferLengths           [5]int
	events                  []string
	panicText               string
}

func runHalfPairFixture(shape [3]int32, enabled bool, scenario string) (out halfPairOutcome) {
	f := newFractionalCallerFixture(shape[0], shape[1], uint8(shape[2]), 32, true)
	f.funcs.sSampleDealingFuncs.bSampleHalfSatdPair = enabled
	InitMeRefinePointer(&f.refine, &f.cache, 0)
	f.me.uiSatdCost = 123456
	entryEnc, entryRef := f.me.pEncMb, f.me.pRefMb
	defer func() {
		if value := recover(); value != nil {
			out.panicText = fmt.Sprintf("%T: %v", value, value)
		}
		out.mv, out.cost = f.me.sMv, f.me.uiSatdCost
		out.pred, out.scratch = bytes.Clone(f.pred), bytes.Clone(f.cache.pBufferInterPredMe)
		out.enc, out.ref = bytes.Clone(f.me.pEncMb), bytes.Clone(f.me.pRefMb)
		out.entryEnc, out.entryRef = bytes.Clone(entryEnc), bytes.Clone(entryRef)
		out.halfBuffers = [3][]byte{bytes.Clone(f.refine.pHalfPixH), bytes.Clone(f.refine.pHalfPixV), bytes.Clone(f.refine.pHalfPixHV)}
		out.bufferLengths = [5]int{len(f.refine.pHalfPixH), len(f.refine.pHalfPixV), len(f.refine.pHalfPixHV), len(f.refine.pQuarPixBest), len(f.refine.pQuarPixTmp)}
	}()

	winner := map[string]SMVUnitXY{"top": {0, -2}, "bottom": {0, 2}, "left": {-2, 0}, "right": {2, 0}}
	_, forceWinner := winner[scenario]
	if forceWinner || scenario == "halfTie" || scenario == "integerTie" {
		clear(f.me.pMvdCost)
		f.me.uSadPredISatd = 100000
		if forceWinner {
			for y := int32(0); y < shape[1]; y++ {
				for x := int32(0); x < shape[0]; x++ {
					f.me.pEncMb[f.me.iEncMbOff+int(y*32+x)] = byte(x*13 + y*7 + 19)
				}
			}
		} else {
			clear(f.me.pEncMb)
			clear(f.me.pRefMb)
			if scenario == "integerTie" {
				f.me.uSadPredISatd = 0
			}
		}
	}
	switch scenario {
	case "recomputeInteger":
		f.layer.bSatdInMdFlag = false
	case "sadMetric":
		f.funcs.sSampleDealingFuncs.pfMeCost = &f.funcs.sSampleDealingFuncs.pfSampleSad
	case "zeroStride":
		f.layer.iEncStride[0] = 0
	case "overlapStride":
		f.layer.iEncStride[0] = 3
	case "negativeStride":
		f.layer.iEncStride[0] = -32
		f.me.iEncMbOff += int(shape[1]-1) * 32
	case "encHalfAlias":
		f.me.pEncMb, f.me.iEncMbOff = f.cache.pBufferInterPredMe[640:], 0
	case "axisAlias":
		f.refine.pHalfPixH = f.refine.pHalfPixV
	case "shortEnc":
		f.me.pEncMb = f.me.pEncMb[: len(f.me.pEncMb)-1 : len(f.me.pEncMb)-1]
	case "mvdTop":
		f.me.iMvdCostOff = 1
	case "mvdBottom":
		f.me.pMvdCost = f.me.pMvdCost[:514]
	case "mvdLeft":
		f.me.iMvdCostOff, f.me.sMvp.iMvX = 2, 1
	case "mvdRight":
		f.me.iMvdCostOff, f.me.sMvp.iMvX = 2, -1
		f.me.pMvdCost = f.me.pMvdCost[:5]
	}
	wrap := func(name string, original common.PWelsLumaHalfpelMcFunc) common.PWelsLumaHalfpelMcFunc {
		return func(src []byte, srcOff int, srcStride int32, dst []byte, dstOff int, dstStride, width, height int32) {
			out.events = append(out.events, name)
			if forceWinner && name != "C" {
				for y := int32(0); y < height; y++ {
					for x := int32(0); x < width; x++ {
						dst[dstOff+int(y*dstStride+x)] = 231
					}
				}
				if (name == "V" && (scenario == "top" || scenario == "bottom")) ||
					(name == "H" && (scenario == "left" || scenario == "right")) {
					off := 0
					if scenario == "bottom" {
						off = ME_REFINE_BUF_STRIDE
					} else if scenario == "right" {
						off = 1
					}
					for y := int32(0); y < shape[1]; y++ {
						copy(dst[off+int(y*dstStride):][:int(shape[0])], f.me.pEncMb[f.me.iEncMbOff+int(y*32):][:int(shape[0])])
					}
				}
			} else {
				original(src, srcOff, srcStride, dst, dstOff, dstStride, width, height)
			}
			if scenario == "customFilters" {
				f.me.pEncMb[f.me.iEncMbOff] ^= 255
				if name == "H" {
					f.refine.pHalfPixV[0] ^= 127
				}
			}
			// Entry input headers, strides and the scoring callback are snapshots.
			// Filter output headers remain live and must be read after each filter.
			if name == "V" {
				samples := &f.funcs.sSampleDealingFuncs
				switch scenario {
				case "replaceEncHeader":
					f.me.pEncMb, f.me.iEncMbOff = nil, -1
				case "replaceRefHeader":
					f.me.pRefMb, f.me.iRefMbOff = nil, -1
				case "replaceStrides":
					f.layer.iEncStride[0], f.pic.iLineSize[0] = -32, -32
				case "replaceFuncList":
					f.ctx.pFuncList = nil
				case "replaceCostTable":
					samples.bSampleHalfSatdPair = false
					samples.pfMeCost = nil
				case "replaceCostEntry":
					samples.bSampleHalfSatdPair = false
					samples.pfSampleAvgSatd = nil
					samples.pfSampleSatd[shape[2]] = func([]byte, int, int32, []byte, int, int32) int32 {
						panic("replacement cost callback")
					}
				case "replaceVerticalHeader":
					f.refine.pHalfPixV = bytes.Clone(f.refine.pHalfPixV)
					for y := int32(0); y <= shape[1]; y++ {
						for x := int32(0); x < shape[0]; x++ {
							f.refine.pHalfPixV[y*ME_REFINE_BUF_STRIDE+x] ^= 255
						}
					}
				}
			}
			if scenario == "replaceHorizontalHeader" && name == "H" {
				f.refine.pHalfPixH = bytes.Clone(f.refine.pHalfPixH)
				for y := int32(0); y < shape[1]; y++ {
					for x := int32(0); x <= shape[0]; x++ {
						f.refine.pHalfPixH[y*ME_REFINE_BUF_STRIDE+x] ^= 255
					}
				}
			}
			if scenario == "shortVertical" && name == "V" {
				end := int(shape[1])*ME_REFINE_BUF_STRIDE + int(shape[0]) - 1
				f.refine.pHalfPixV = f.refine.pHalfPixV[:end:end]
			}
			if scenario == "shortHorizontal" && name == "H" {
				end := int(shape[1]-1)*ME_REFINE_BUF_STRIDE + int(shape[0])
				f.refine.pHalfPixH = f.refine.pHalfPixH[:end:end]
			}
		}
	}
	f.funcs.sMcFuncs.PfLumaHalfpelVer = wrap("V", f.funcs.sMcFuncs.PfLumaHalfpelVer)
	f.funcs.sMcFuncs.PfLumaHalfpelHor = wrap("H", f.funcs.sMcFuncs.PfLumaHalfpelHor)
	f.funcs.sMcFuncs.PfLumaHalfpelCen = wrap("C", f.funcs.sMcFuncs.PfLumaHalfpelCen)
	MeRefineFracPixel(&f.ctx, f.pred, 0, &f.me, &f.refine, shape[0], shape[1])
	return out
}

func TestHalfSatdPairFullCallerParity(t *testing.T) {
	requireSampleAVX2(t)
	for _, shape := range [][3]int32{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}, {4, 4, BLOCK_4x4}, {4, 8, BLOCK_4x8}} {
		for _, scenario := range []string{"motion", "top", "bottom", "left", "right", "halfTie", "integerTie", "recomputeInteger", "sadMetric", "zeroStride", "overlapStride", "negativeStride", "encHalfAlias", "axisAlias", "customFilters", "shortEnc", "shortVertical", "shortHorizontal", "mvdTop", "mvdBottom", "mvdLeft", "mvdRight"} {
			t.Run(fmt.Sprintf("%dx%d/%s", shape[0], shape[1], scenario), func(t *testing.T) {
				want := runHalfPairFixture(shape, false, scenario)
				got := runHalfPairFixture(shape, true, scenario)
				wantPanic := scenario == "shortEnc" || scenario == "shortVertical" || scenario == "shortHorizontal" || scenario == "mvdTop" || scenario == "mvdBottom" || scenario == "mvdLeft" || scenario == "mvdRight" ||
					(scenario == "negativeStride" && (shape[0] == 4 || shape[1] == 4))
				if !reflect.DeepEqual(got, want) || (want.panicText != "") != wantPanic {
					t.Fatalf("legacy mv=%v cost=%d events=%v panic=%q; pair mv=%v cost=%d events=%v panic=%q; full state parity=%v", want.mv, want.cost, want.events, want.panicText, got.mv, got.cost, got.events, got.panicText, reflect.DeepEqual(got, want))
				}
				if expected, ok := map[string]SMVUnitXY{"top": {0, -2}, "bottom": {0, 2}, "left": {-2, 0}, "right": {2, 0}, "halfTie": {0, -2}, "integerTie": {0, 0}}[scenario]; ok && got.mv != expected {
					t.Fatalf("selection/tie: mv=%v, want %v", got.mv, expected)
				}
				if scenario == "mvdLeft" || scenario == "mvdRight" || scenario == "shortHorizontal" {
					if !reflect.DeepEqual(got.events, []string{"V", "H"}) {
						t.Fatalf("horizontal failure has changed filter order: %v", got.events)
					}
				}
				if scenario == "mvdTop" || scenario == "mvdBottom" || scenario == "shortVertical" {
					if !reflect.DeepEqual(got.events, []string{"V"}) {
						t.Fatalf("horizontal filter ran before vertical failure: %v", got.events)
					}
				}
			})
		}
	}
}

// Use the complete caller so mutations after the first filter exercise the
// same cached inputs, live filter outputs and partial failure order as production.
func TestHalfSatdPairFilterSnapshots(t *testing.T) {
	requireSampleAVX2(t)
	for _, shape := range [][3]int32{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}, {4, 4, BLOCK_4x4}, {4, 8, BLOCK_4x8}} {
		for _, scenario := range []string{"replaceEncHeader", "replaceRefHeader", "replaceStrides", "replaceFuncList", "replaceCostTable", "replaceCostEntry", "replaceVerticalHeader", "replaceHorizontalHeader"} {
			t.Run(fmt.Sprintf("%dx%d/%s", shape[0], shape[1], scenario), func(t *testing.T) {
				want := runHalfPairFixture(shape, false, scenario)
				got := runHalfPairFixture(shape, true, scenario)
				wantPanic := scenario == "replaceEncHeader" || scenario == "replaceCostTable" || scenario == "replaceCostEntry"
				if !reflect.DeepEqual(got, want) || (want.panicText != "") != wantPanic {
					t.Fatalf("legacy mv=%v cost=%d events=%v panic=%q; pair mv=%v cost=%d events=%v panic=%q; full state parity=%v", want.mv, want.cost, want.events, want.panicText, got.mv, got.cost, got.events, got.panicText, reflect.DeepEqual(got, want))
				}
				// Even when a replaced field later causes quarter refinement to
				// fail, both half axes must finish using their entry snapshots.
				if len(got.events) < 2 || !reflect.DeepEqual(got.events[:2], []string{"V", "H"}) {
					t.Fatalf("filter mutation affected the saved half-pixel inputs: %v", got.events)
				}
				if scenario == "replaceCostEntry" && got.panicText != "string: replacement cost callback" {
					t.Fatalf("replacement callback was not observed by quarter refinement: %q", got.panicText)
				}
			})
		}
	}
}

func TestHalfSatdPairCustomCostOptOut(t *testing.T) {
	requireSampleAVX2(t)
	f := newFractionalCallerFixture(16, 16, BLOCK_16x16, 32, true)
	f.funcs.sSampleDealingFuncs.bSampleHalfSatdPair = false
	var events []string
	original := f.funcs.sSampleDealingFuncs.pfSampleSatd[BLOCK_16x16]
	f.funcs.sSampleDealingFuncs.pfSampleSatd[BLOCK_16x16] = func(a []byte, aOff int, aStride int32, b []byte, bOff int, bStride int32) int32 {
		events = append(events, "cost")
		return original(a, aOff, aStride, b, bOff, bStride)
	}
	wrap := func(name string, fn common.PWelsLumaHalfpelMcFunc) common.PWelsLumaHalfpelMcFunc {
		return func(src []byte, srcOff int, srcStride int32, dst []byte, dstOff int, dstStride, width, height int32) {
			events = append(events, name)
			fn(src, srcOff, srcStride, dst, dstOff, dstStride, width, height)
		}
	}
	f.funcs.sMcFuncs.PfLumaHalfpelVer = wrap("V", f.funcs.sMcFuncs.PfLumaHalfpelVer)
	f.funcs.sMcFuncs.PfLumaHalfpelHor = wrap("H", f.funcs.sMcFuncs.PfLumaHalfpelHor)
	f.run()
	if len(events) < 6 || !reflect.DeepEqual(events[:6], []string{"V", "cost", "cost", "H", "cost", "cost"}) {
		t.Fatalf("callback order changed: %v", events)
	}
}

func TestHalfSatdPairCallerZeroAllocs(t *testing.T) {
	requireSampleAVX2(t)
	for _, shape := range [][3]int32{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}, {4, 4, BLOCK_4x4}, {4, 8, BLOCK_4x8}} {
		for _, motion := range []bool{false, true} {
			f := newFractionalCallerFixture(shape[0], shape[1], uint8(shape[2]), 2048, motion)
			if got := testing.AllocsPerRun(100, f.run); got != 0 {
				t.Fatalf("shape=%v motion=%v: %.2f allocations, want zero", shape, motion, got)
			}
		}
	}
}
