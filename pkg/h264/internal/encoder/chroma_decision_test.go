package encoder

import (
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type chromaDecisionEvent struct {
	kind             byte
	length, capacity int
	size             int32
	cbp, qp          uint8
	counts           [24]int8
	residual         [64]int16
	factors          [8]uint16
}

type chromaDecisionState struct {
	backing                 [1024]int16
	residual, alternate     [80]int16
	dct, alternateDCT       SDCTCoeff
	counts, alternateCounts [32]int8
	mb                      SMB
	cache                   SMbCache
	functions               SWelsFuncPtrList
	res                     []int16
	events                  []chromaDecisionEvent
}

func newChromaDecisionState(input [80]int16, qp, cbp uint8, intra bool, mutation int) *chromaDecisionState {
	s := &chromaDecisionState{residual: input}
	for i := range s.dct.iLumaBlock {
		s.dct.iLumaBlock[i] = int16(3000 + i)
	}
	for i := range s.dct.iChromaBlock {
		s.dct.iChromaBlock[i] = int16(-7000 - i)
	}
	s.dct.iChromaDc = [2][4]int16{{11, 13, 17, 19}, {23, 29, 31, 37}}
	s.alternateDCT = s.dct
	for i := range s.counts {
		s.counts[i], s.alternateCounts[i] = int8(i+17), int8(i+71)
	}
	for i := range s.alternate {
		s.alternate[i] = int16(1000 + i)
	}
	s.res = s.residual[:64]
	s.mb = SMB{uiMbType: common.MB_TYPE_16x16, uiChromaQp: qp, uiLumaQp: 23, uiCbp: cbp, pNonZeroCount: s.counts[:24]}
	if intra {
		s.mb.uiMbType = common.MB_TYPE_INTRA16x16
	}
	s.cache = SMbCache{pCoeffLevel: s.res, pDct: &s.dct}
	WelsInitEncodingFuncs(&s.functions, 0)
	WelsInitReconstructionFuncs(&s.functions, 0)
	s.functions.pfGetNoneZeroCount = WelsGetNoneZeroCount_c
	record := func(kind byte, data []int16, size int32, factors []uint16) {
		event := chromaDecisionEvent{kind: kind, length: len(data), capacity: cap(data), size: size, cbp: s.mb.uiCbp, qp: s.mb.uiChromaQp}
		copy(event.counts[:], s.mb.pNonZeroCount)
		copy(event.residual[:], data)
		copy(event.factors[:], factors)
		s.events = append(s.events, event)
	}
	mutate := func(data []int16) {
		if mutation == 0 {
			return
		}
		data[1] ^= 0x123
		data[63] ^= 0x321
		if mutation == 2 {
			panic("chroma decision callback")
		}
		s.mb.uiCbp ^= 0xc0
		s.mb.uiChromaQp = 255 // DC reconstruction must retain the entry QP.
		s.mb.uiMbType = common.MB_TYPE_SKIP
		s.mb.pNonZeroCount = s.alternateCounts[:24]
		if mutation == 3 {
			s.mb.pNonZeroCount = s.alternateCounts[:18]
		}
		s.cache.pCoeffLevel, s.cache.pDct = s.alternate[:], &s.alternateDCT
	}
	s.functions.pfSetMemZeroSize64 = func(data []int16, size int32) {
		record('z', data, size, nil)
		WelsSetMemZero_c(data, size)
		mutate(data)
	}
	s.functions.pfDequantizationFour4x4 = func(data []int16, factors []uint16) {
		record('d', data, 128, factors)
		WelsDequantFour4x4_c(data, factors)
		mutate(data)
	}
	return s
}

type chromaDecisionWriter func(*SWelsFuncPtrList, *SMB, *SMbCache, []int16, int32)

func callChromaDecision(fn chromaDecisionWriter, s *chromaDecisionState, uv int32) (failure string) {
	defer func() {
		if value := recover(); value != nil {
			failure = fmt.Sprintf("%T: %v", value, value)
		}
	}()
	fn(&s.functions, &s.mb, &s.cache, s.res, uv)
	return
}

func compareChromaDecision(t *testing.T, got, want *chromaDecisionState, uv int32, reference chromaDecisionWriter) string {
	t.Helper()
	gotPanic, wantPanic := callChromaDecision(WelsEncRecUV, got, uv), callChromaDecision(reference, want, uv)
	if gotPanic != wantPanic || got.backing != want.backing || got.residual != want.residual || got.alternate != want.alternate ||
		got.dct != want.dct || got.alternateDCT != want.alternateDCT || got.counts != want.counts || got.alternateCounts != want.alternateCounts ||
		got.mb.uiCbp != want.mb.uiCbp || got.mb.uiChromaQp != want.mb.uiChromaQp || got.mb.uiLumaQp != want.mb.uiLumaQp || got.mb.uiMbType != want.mb.uiMbType ||
		!reflect.DeepEqual(got.events, want.events) || !slices.Equal(got.mb.pNonZeroCount, want.mb.pNonZeroCount) || cap(got.mb.pNonZeroCount) != cap(want.mb.pNonZeroCount) ||
		!slices.Equal(got.cache.pCoeffLevel, want.cache.pCoeffLevel) || cap(got.cache.pCoeffLevel) != cap(want.cache.pCoeffLevel) ||
		(got.cache.pDct == &got.alternateDCT) != (want.cache.pDct == &want.alternateDCT) {
		t.Fatalf("UV%d state/trace mismatch: panic=%q/%q CBP=%#x/%#x events=%v/%v", uv, gotPanic, wantPanic, got.mb.uiCbp, want.mb.uiCbp, got.events, want.events)
	}
	return gotPanic
}

func chromaDecisionUnitInput(t *testing.T, qp, index, level int, intra bool) int16 {
	t.Helper()
	ffQP := qp
	if intra {
		ffQP += 6
	}
	ff, mf := int(g_kiQuantInterFF[ffQP][index&7]), int(g_kiQuantMF[qp][index&7])
	value := (level*65536+mf-1)/mf - ff
	if value < 0 || value > 32767 || int(wels_NEW_QUANT(int16(value), int16(ff), int16(mf))) != level {
		t.Fatalf("cannot construct QP%d index%d level%d intra%t", qp, index, level, intra)
	}
	return int16(value)
}

func TestChromaDecisionThresholdParity(t *testing.T) {
	// AC scan positions, independently listed from the production mask path.
	scan := [...]int{1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}
	masks := [...]uint16{0, 1 << 3, 1 << 1, 1, 1 | 1<<4, 1 | 1<<2, 3, 3 | 1<<6}
	for qp := range 52 {
		for _, scores := range [][4]int{{0, 0, 0, 0}, {6, 0, 0, 0}, {6, 1, 0, 0}, {5, 1, 0, 0}, {5, 2, 0, 0}, {0, 6, 0, 1}, {7, 6, 5, 4}} {
			for _, intra := range []bool{false, true} {
				for _, dc := range []bool{false, true} {
					t.Run(fmt.Sprintf("QP%d/scores%v/intra%t/DC%t", qp, scores, intra, dc), func(t *testing.T) {
						var input [80]int16
						for block, score := range scores {
							for position, index := range scan {
								if masks[score]&(1<<position) != 0 {
									input[block*16+index] = chromaDecisionUnitInput(t, qp, index, 1, intra)
									if (block+position)&1 != 0 {
										input[block*16+index] = -input[block*16+index]
									}
								}
							}
							if dc {
								input[block*16] = [...]int16{2047, -1023, 511, -511}[block]
							}
						}
						for uv := int32(1); uv <= 2; uv++ {
							cbp := [...]uint8{0, 0x10, 0x2f, 0xe5}[(qp+int(uv))&3]
							got := newChromaDecisionState(input, uint8(qp), cbp, intra, 0)
							want := newChromaDecisionState(input, uint8(qp), cbp, intra, 0)
							if failure := compareChromaDecision(t, got, want, uv, coefficientUVReference); failure != "" {
								t.Fatalf("valid threshold fixture panicked: %s", failure)
							}
						}
					})
				}
			}
		}
	}
}

func TestChromaDecisionRandomAndCallbacks(t *testing.T) {
	rng := rand.New(rand.NewSource(0x2648c))
	for qp := range 52 {
		for fixture := range 12 {
			var input [80]int16
			for i := range input {
				switch fixture % 4 {
				case 0:
					input[i] = int16(rng.Uint32())
				case 1:
					input[i] = [...]int16{-32768, -32767, -1, 0, 1, 32766, 32767}[rng.Intn(7)]
				case 2:
					if i%17 == 0 {
						input[i] = int16(rng.Intn(33) - 16)
					}
				}
			}
			for uv := int32(1); uv <= 2; uv++ {
				for mutation := range 4 {
					got := newChromaDecisionState(input, uint8(qp), uint8(fixture*37), fixture&1 != 0, mutation)
					want := newChromaDecisionState(input, uint8(qp), uint8(fixture*37), fixture&1 != 0, mutation)
					compareChromaDecision(t, got, want, uv, chromaDecisionFrozen)
				}
			}
		}
	}
}

func TestChromaDecisionAliasParity(t *testing.T) {
	for _, qp := range []uint8{0, 20, 32, 51} {
		for uv := int32(1); uv <= 2; uv++ {
			for _, shift := range []int{-32, 0, 8, 32, 64} {
				for _, countsOffset := range []int{-1, 0, 30, 120} {
					for _, mutation := range []int{0, 1, 3} {
						setup := func() *chromaDecisionState {
							s := newChromaDecisionState([80]int16{}, qp, 0xa5, shift&8 != 0, mutation)
							for i := range s.backing {
								s.backing[i] = int16(i*713 + int(qp)*37 + 19)
							}
							// SDCTCoeff is entirely int16 arrays. Alias the residual
							// with the current plane's scan storage or adjacent DC.
							s.cache.pDct = (*SDCTCoeff)(unsafe.Pointer(&s.backing[128]))
							start := 128 + 272 + int(uv-1)*64 + shift
							s.res = s.backing[start : start+64]
							s.cache.pCoeffLevel = s.res
							if countsOffset >= 0 {
								s.mb.pNonZeroCount = unsafe.Slice((*int8)(unsafe.Pointer(&s.backing[start])), 160)[countsOffset : countsOffset+24]
							}
							return s
						}
						got, want := setup(), setup()
						compareChromaDecision(t, got, want, uv, chromaDecisionFrozen)
					}
				}
			}
		}
	}
}

func TestChromaDecisionMalformedParity(t *testing.T) {
	for uv := int32(1); uv <= 2; uv++ {
		for _, keep := range []bool{false, true} {
			var input [80]int16
			if keep {
				for i := range input {
					input[i] = int16(i*37 + 1000)
				}
			}
			for length := 0; length <= 64; length++ {
				got, want := newChromaDecisionState(input, 20, 0xa5, false, 0), newChromaDecisionState(input, 20, 0xa5, false, 0)
				got.res, want.res = got.residual[:length], want.residual[:length]
				compareChromaDecision(t, got, want, uv, chromaDecisionFrozen)
			}
			for length := 0; length <= 24; length++ {
				got, want := newChromaDecisionState(input, 20, 0xa5, false, 0), newChromaDecisionState(input, 20, 0xa5, false, 0)
				got.mb.pNonZeroCount, want.mb.pNonZeroCount = got.counts[:length], want.counts[:length]
				compareChromaDecision(t, got, want, uv, chromaDecisionFrozen)
			}
			got, want := newChromaDecisionState(input, 20, 0xa5, false, 0), newChromaDecisionState(input, 20, 0xa5, false, 0)
			got.functions.pfSetMemZeroSize64, want.functions.pfSetMemZeroSize64 = nil, nil
			got.functions.pfDequantizationFour4x4, want.functions.pfDequantizationFour4x4 = nil, nil
			compareChromaDecision(t, got, want, uv, chromaDecisionFrozen)
		}
	}
	for _, uv := range []int32{-1 << 31, -1, 0, 3, 1<<31 - 1} {
		got, want := newChromaDecisionState([80]int16{}, 20, 0, false, 0), newChromaDecisionState([80]int16{}, 20, 0, false, 0)
		compareChromaDecision(t, got, want, uv, chromaDecisionFrozen)
	}
	for _, qp := range []uint8{52, 255} {
		got, want := newChromaDecisionState([80]int16{}, qp, 0, false, 0), newChromaDecisionState([80]int16{}, qp, 0, false, 0)
		compareChromaDecision(t, got, want, 1, chromaDecisionFrozen)
	}
	got, want := newChromaDecisionState([80]int16{}, 20, 0, false, 0), newChromaDecisionState([80]int16{}, 20, 0, false, 0)
	got.cache.pDct, want.cache.pDct = nil, nil
	compareChromaDecision(t, got, want, 1, chromaDecisionFrozen)
}
