package encoder

import (
	"fmt"
	"math/bits"
	"math/rand"
	"reflect"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type interLumaCallbackEvent struct {
	kind     byte
	offset   int
	length   int
	size     int32
	cbp      uint8
	counts   [24]int8
	residual [64]int16
}

type interLumaBookkeepingState struct {
	backing         [1024]int16
	residual        [384]int16
	dct             SDCTCoeff
	counts          [32]int8
	alternateCounts [32]int8
	mb              SMB
	cache           SMbCache
	functions       SWelsFuncPtrList
	events          []interLumaCallbackEvent
}

func newInterLumaBookkeepingState(input [384]int16, qp, cbp uint8, mutate bool) *interLumaBookkeepingState {
	s := &interLumaBookkeepingState{residual: input}
	for i := range s.dct.iLumaBlock {
		s.dct.iLumaBlock[i] = -12345
	}
	for i := range s.dct.iChromaBlock {
		s.dct.iChromaBlock[i] = 23456
	}
	for i := range s.dct.iLumaI16x16Dc {
		s.dct.iLumaI16x16Dc[i] = int16(100 + i)
	}
	s.dct.iChromaDc = [2][4]int16{{11, 13, 17, 19}, {23, 29, 31, 37}}
	for i := range s.counts {
		s.counts[i], s.alternateCounts[i] = int8(17+i), int8(71+i)
	}
	s.mb = SMB{uiMbType: common.MB_TYPE_16x16, uiLumaQp: qp, uiChromaQp: 31, uiCbp: cbp, pNonZeroCount: s.counts[:24]}
	s.cache = SMbCache{pCoeffLevel: s.residual[:], pDct: &s.dct}
	WelsInitEncodingFuncs(&s.functions, 0)
	WelsInitReconstructionFuncs(&s.functions, 0)
	s.functions.pfGetNoneZeroCount = WelsGetNoneZeroCount_c
	record := func(kind byte, data []int16, size int32) {
		event := interLumaCallbackEvent{kind: kind, length: len(data), size: size, cbp: s.mb.uiCbp}
		event.offset = int(uintptr(unsafe.Pointer(&data[0]))-uintptr(unsafe.Pointer(&s.cache.pCoeffLevel[0]))) / 2
		copy(event.counts[:], s.mb.pNonZeroCount)
		copy(event.residual[:], data)
		s.events = append(s.events, event)
	}
	mutateState := func() {
		if mutate && len(s.events) == 1 {
			s.mb.uiCbp ^= 0x80
			s.mb.pNonZeroCount = s.alternateCounts[:24]
			s.cache.pCoeffLevel[192] = 17
		}
	}
	s.functions.pfSetMemZeroSize64 = func(data []int16, size int32) {
		record('z', data, size)
		WelsSetMemZero_c(data, size)
		mutateState()
	}
	s.functions.pfDequantizationFour4x4 = func(data []int16, factors []uint16) {
		record('d', data, 128)
		WelsDequantFour4x4_c(data, factors)
		mutateState()
	}
	return s
}

func compareInterLumaBookkeeping(t *testing.T, input [384]int16, qp, cbp uint8, mutate bool) {
	t.Helper()
	got := newInterLumaBookkeepingState(input, qp, cbp, mutate)
	want := newInterLumaBookkeepingState(input, qp, cbp, mutate)
	WelsEncInterY(&got.functions, &got.mb, &got.cache)
	// Existing frozen pre-fusion reference independently scans, scores and
	// counts coefficients instead of using the candidate's masks/bookkeeping.
	coefficientInterYReference(&want.functions, &want.mb, &want.cache)
	checkInterLumaBookkeepingStates(t, got, want)
}

func checkInterLumaBookkeepingStates(t *testing.T, got, want *interLumaBookkeepingState) {
	t.Helper()
	if got.backing != want.backing || got.residual != want.residual || got.dct != want.dct || got.counts != want.counts ||
		got.alternateCounts != want.alternateCounts || got.mb.uiCbp != want.mb.uiCbp ||
		got.mb.uiLumaQp != want.mb.uiLumaQp || got.mb.uiChromaQp != want.mb.uiChromaQp ||
		!reflect.DeepEqual(got.events, want.events) {
		t.Fatalf("state or callback trace differs: CBP=%#x/%#x events=%v/%v", got.mb.uiCbp, want.mb.uiCbp, got.events, want.events)
	}
	gotAlternate := &got.mb.pNonZeroCount[0] == &got.alternateCounts[0]
	wantAlternate := &want.mb.pNonZeroCount[0] == &want.alternateCounts[0]
	if gotAlternate != wantAlternate {
		t.Fatal("callback-selected nonzero buffer changed")
	}
}

func interLumaUnitInput(t *testing.T, qp, index, level int) int16 {
	t.Helper()
	ff, mf := int(g_kiQuantInterFF[qp][index&7]), int(g_kiQuantMF[qp][index&7])
	value := (level*65536+mf-1)/mf - ff
	if value < 0 || value > 32767 || int(wels_NEW_QUANT(int16(value), int16(ff), int16(mf))) != level {
		t.Fatalf("cannot construct QP%d position%d quantized level%d", qp, index, level)
	}
	return int16(value)
}

func TestInterLumaBookkeepingThresholds(t *testing.T) {
	// A lone block in each group has precisely this run score. Cases straddle
	// both the macroblock threshold six and the retained-group threshold four.
	masks := [...]uint16{0, 1 << 3, 1 << 1, 1, 1 | 1<<4, 1 | 1<<2, 3}
	scan := [...]int{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}
	for qp := range 52 {
		for _, scores := range [][4]int{{0, 0, 0, 0}, {3, 2, 0, 0}, {3, 3, 0, 0}, {4, 1, 0, 0}, {4, 2, 0, 0}, {5, 1, 0, 0}, {6, 0, 0, 0}, {1, 4, 5, 6}, {6, 5, 4, 3}} {
			t.Run(fmt.Sprintf("QP%d/scores%v", qp, scores), func(t *testing.T) {
				var input [384]int16
				for i := 256; i < len(input); i++ {
					input[i] = int16(i*17 + 3)
				}
				for group, score := range scores {
					for position, index := range scan {
						if masks[score]&(1<<position) != 0 {
							input[group*64+index] = interLumaUnitInput(t, qp, index, 1)
							if (group+position)&1 != 0 {
								input[group*64+index] = -input[group*64+index]
							}
						}
					}
				}
				for _, cbp := range []uint8{0, 0x30, 0xa5} {
					compareInterLumaBookkeeping(t, input, uint8(qp), cbp, false)
				}
			})
		}
	}
}

func TestInterLumaBookkeepingRandomAndCallbacks(t *testing.T) {
	rng := rand.New(rand.NewSource(0x26416))
	for qp := range 52 {
		for fixture := range 18 {
			var input [384]int16
			for i := range input {
				switch fixture % 3 {
				case 0:
					input[i] = int16(rng.Uint32())
				case 1:
					input[i] = [...]int16{-32768, -32767, -1, 0, 1, 32766, 32767}[rng.Intn(7)]
				case 2:
					if rng.Intn(4) == 0 {
						input[i] = int16(rng.Intn(513) - 256)
					}
				}
			}
			compareInterLumaBookkeeping(t, input, uint8(qp), uint8(fixture*37), fixture&1 != 0)
		}
	}
}

func TestInterLumaBookkeepingAliases(t *testing.T) {
	for _, qp := range []uint8{0, 20, 32, 51} {
		for _, shift := range []int{-64, 0, 4, 64} {
			for _, countsOffset := range []int{-1, 0, 31, 192} {
				for _, mutate := range []bool{false, true} {
					t.Run(fmt.Sprintf("QP%d/residual%d/counts%d/mutate%t", qp, shift, countsOffset, mutate), func(t *testing.T) {
						setup := func() *interLumaBookkeepingState {
							s := newInterLumaBookkeepingState([384]int16{}, qp, 0x30, mutate)
							for i := range s.backing {
								s.backing[i] = int16(i*713 + 19 + int(qp))
							}
							// SDCTCoeff contains only int16 arrays. This one backing
							// allocation deliberately crosses residual/scan boundaries.
							s.cache.pDct = (*SDCTCoeff)(unsafe.Pointer(&s.backing[256]))
							s.cache.pCoeffLevel = s.backing[256+shift : 256+shift+384]
							if countsOffset >= 0 {
								s.mb.pNonZeroCount = unsafe.Slice((*int8)(unsafe.Pointer(&s.backing[256+countsOffset])), 24)
							}
							return s
						}
						got, want := setup(), setup()
						WelsEncInterY(&got.functions, &got.mb, &got.cache)
						interLumaFrozenBookkeeping(&want.functions, &want.mb, &want.cache)
						checkInterLumaBookkeepingStates(t, got, want)
					})
				}
			}
		}
	}
}

// Frozen immediately before this bookkeeping change. Unlike the independent
// pre-fusion reference above, this retains already-established mask semantics
// when nonzero counts, scanned levels and residuals deliberately alias.
func interLumaFrozenBookkeeping(pFuncList *SWelsFuncPtrList, pCurMb *SMB, pMbCache *SMbCache) {
	pfSetMemZeroSize64 := pFuncList.pfSetMemZeroSize64
	pfDequantizationFour4x4 := pFuncList.pfDequantizationFour4x4
	pRes := pMbCache.pCoeffLevel
	iSingleCtrMb := int32(0)
	var iSingleCtr8x8 [4]int32
	pBlock := pMbCache.pDct.iLumaBlock[:]
	uiQp := pCurMb.uiLumaQp
	pMF := g_kiQuantMF[uiQp][:]
	pFF := g_kiQuantInterFF[uiQp][:]
	var aMax [16]int16
	var masks [16]uint16
	iNoneZeroCount := int32(0)

	quantScanLuma(pRes, pBlock, pFF, pMF, aMax[:], masks[:])

	iResOff := 0
	iBlockOff := 0
	for i := 0; i < 4; i++ {
		iSingleCtr8x8[i] = 0
		for j := 0; j < 4; j++ {
			if aMax[(i<<2)+j] != 0 {
				mask := masks[(i<<2)+j]
				if aMax[(i<<2)+j] > 1 {
					iSingleCtr8x8[i] += 9
				} else if iSingleCtr8x8[i] < 6 {
					iSingleCtr8x8[i] += coefficientScoreMask(mask)
				}
			}
			iResOff += 16
			iBlockOff += 16
		}
		iSingleCtrMb += iSingleCtr8x8[i]
	}
	iBlockOff -= 256
	iResOff -= 256

	clear(pCurMb.pNonZeroCount[:16])

	if iSingleCtrMb < 6 { //from JVT-O079
		pfSetMemZeroSize64(pRes[iResOff:], 768) // confirmed_safe_unsafe_usage
	} else {
		kpNoneZeroCountIdx := common.G_kuiMbCountScan4Idx[:]
		for i := 0; i < 4; i++ {
			if iSingleCtr8x8[i] >= 4 {
				for j := 0; j < 4; j++ {
					iNoneZeroCount = int32(bits.OnesCount16(masks[(i<<2)+j]))
					pCurMb.pNonZeroCount[kpNoneZeroCountIdx[0]] = int8(iNoneZeroCount)
					kpNoneZeroCountIdx = kpNoneZeroCountIdx[1:]
					iBlockOff += 16
				}
				pfDequantizationFour4x4(pRes[iResOff:], common.G_kuiDequantCoeff[uiQp][:])
				pCurMb.uiCbp |= uint8(1 << i)
			} else { // set zero for an 8x8 pBlock
				pfSetMemZeroSize64(pRes[iResOff:], 128) // confirmed_safe_unsafe_usage
				kpNoneZeroCountIdx = kpNoneZeroCountIdx[4:]
				iBlockOff += 64
			}
			iResOff += 64
		}
	}
}
