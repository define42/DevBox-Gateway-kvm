//go:build amd64

package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

func sampleFourPlane(rng *rand.Rand, width, height, stride, seed, pattern int) ([]byte, int) {
	off, span := 3, (height-1)*stride
	if span < 0 {
		off -= span
		span = -span
	}
	buf := make([]byte, span+width+3)
	rng.Read(buf)
	for i := range buf {
		switch pattern {
		case 0:
			buf[i] = 0
		case 1:
			buf[i] = 255
			if seed == 0 {
				buf[i] = 0
			}
		case 2:
			buf[i] = 0
			if seed == 0 {
				buf[i] = 255
			}
		case 3:
			buf[i] = byte((i + seed) % 2 * 255)
		case 4:
			buf[i] = byte((i/4 + seed) % 2 * 255)
		case 5:
			buf[i] = byte(seed % 2)
		}
	}
	return buf, off
}

func TestSampleAvgSatdFourAVX2Parity(t *testing.T) {
	requireSampleAVX2(t)
	rng := rand.New(rand.NewSource(0x264f04))
	for _, size := range [][2]int{{8, 4}, {8, 8}, {8, 12}, {8, 16}, {16, 4}, {16, 8}, {16, 12}, {16, 16}} {
		for _, strides := range [][3]int{{32, 32, 32}, {23, 27, 31}, {0, 3, 5}, {-23, -27, -31}} {
			for pattern := range 48 {
				w, h := size[0], size[1]
				enc, encOff := sampleFourPlane(rng, w, h, strides[0], 0, pattern)
				var candidates [4]sampleAvgSatdCandidate
				var want [4]int32
				for i := range candidates {
					a, aOff := sampleFourPlane(rng, w, h, strides[1], i*2+1, pattern)
					b, bOff := sampleFourPlane(rng, w, h, strides[2], i*2+2, pattern)
					candidates[i] = sampleAvgSatdCandidate{&a[aOff], &b[bOff], strides[1], strides[2]}
					want[i] = sampleAvgSatdReference(enc, encOff, int32(strides[0]), a, aOff, int32(strides[1]),
						b, bOff, int32(strides[2]), int32(w), int32(h))
					old := satdAvgBlockAVX2(&enc[encOff], strides[0], &a[aOff], strides[1], &b[bOff], strides[2], w, h)
					if int32(old) != want[i] {
						t.Fatalf("existing scorer differs from scalar: size=%v stride=%v pattern=%d candidate=%d", size, strides, pattern, i)
					}
				}
				got := [6]int32{-1, -2, -3, -4, -5, -6}
				satdAvgFourAVX2(&enc[encOff], strides[0], &candidates, w, h, (*[4]int32)(got[1:5]))
				if got[0] != -1 || got[5] != -6 || [4]int32(got[1:5]) != want {
					t.Fatalf("size=%v strides=%v pattern=%d got=%v want=%v", size, strides, pattern, got, want)
				}
				// Read-only aliases, including the encoded source, are safe.
				for i := range candidates {
					candidates[i] = sampleAvgSatdCandidate{&enc[encOff], &enc[encOff], strides[0], strides[0]}
				}
				var same [4]int32
				satdAvgFourAVX2(&enc[encOff], strides[0], &candidates, w, h, &same)
				if same != [4]int32{} {
					t.Fatalf("identical input costs=%v, want zero", same)
				}
			}
		}
	}
}

func TestSampleAvgSatdFourRowImpulses(t *testing.T) {
	requireSampleAVX2(t)
	for _, size := range [][2]int{{8, 4}, {8, 8}, {8, 16}, {16, 8}, {16, 16}} {
		w, h := size[0], size[1]
		enc := make([]byte, w*h)
		for y := range h {
			for x := range w {
				var candidates [4]sampleAvgSatdCandidate
				var want [4]int32
				for i := range candidates {
					a, b := make([]byte, w*h), make([]byte, w*h)
					a[y*w+x] = byte(255 - i*31)
					b[y*w+(x+i)%w] = byte(i*37 + 1)
					candidates[i] = sampleAvgSatdCandidate{&a[0], &b[0], w, w}
					want[i] = sampleAvgSatdReference(enc, 0, int32(w), a, 0, int32(w), b, 0, int32(w), int32(w), int32(h))
				}
				var got [4]int32
				satdAvgFourAVX2(&enc[0], w, &candidates, w, h, &got)
				if got != want {
					t.Fatalf("size=%v impulse=%d,%d got=%v want=%v", size, x, y, got, want)
				}
			}
		}
	}
}

func TestQuarterRefineFourMatchesExistingPaths(t *testing.T) {
	requireSampleAVX2(t)
	for _, size := range [][3]int32{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}, {4, 4, BLOCK_4x4}, {4, 8, BLOCK_4x8}} {
		for scenario := range 15 {
			t.Run(fmt.Sprintf("%dx%d/scenario%d", size[0], size[1], scenario), func(t *testing.T) {
				run := func(route int) (f *quarterBatchFixture, calls int, panicText string) {
					// PixelAvg_sse2 rejects negative prediction strides, and the
					// narrow SSE4.1 SATD entries reject negative encoded strides.
					// Compare those existing fallbacks including partial state.
					defer func() {
						if value := recover(); value != nil {
							panicText = fmt.Sprintf("%T: %v", value, value)
						}
					}()
					f = newQuarterBatchFixture(uint8(size[2]))
					samples := &f.funcs.sSampleDealingFuncs
					samples.pfSampleAvgSatdValidated = func(enc *byte, encStride int,
						a *byte, aStride int, b *byte, bStride, width, height int) int {
						calls++
						return satdAvgBlockAVX2(enc, encStride, a, aStride, b, bStride, width, height)
					}
					if route > 0 {
						samples.bSampleAvgSatdFour = false
						if route == 2 {
							samples.pfSampleAvgSatd = nil
						}
					}
					stride := int32(31)
					switch scenario {
					case 1:
						f.params.iBestCost = 0
					case 2, 3:
						clear(f.me.pEncMb)
						for i := range 4 {
							clear(f.params.pSrcA[i])
							clear(f.params.pSrcB[i])
							f.params.iLms[i] = 20
							if scenario == 3 {
								f.params.iLms[i] -= int32(i * 5)
							}
						}
					case 4:
						f.params.pSrcA[2], f.params.iSrcAOff[2] = f.refine.pQuarPixTmp, 0
					case 5:
						f.params.pSrcB[3], f.params.iSrcBOff[3] = f.refine.pQuarPixBest, 1
					case 6:
						f.me.pEncMb = f.refine.pQuarPixTmp
					case 7:
						f.refine.pQuarPixTmp = f.scratch[1:]
					case 8:
						samples.pfMeCost = &samples.pfSampleSad
					case 9:
						f.params.iStrideA, f.params.iStrideB = 0, 0
					case 10:
						stride = 0
					case 11:
						f.params.iStrideA, f.params.iStrideB = -27, -29
						for i := range 4 {
							f.params.iSrcBOff[i] += int(size[1]-1) * (27 + 2*(i/2))
						}
					case 12:
						stride = -31
						f.me.iEncMbOff += int(size[1]-1) * 31
					case 13:
						stride = 3
					case 14:
						for i := range 4 {
							f.params.pSrcA[i], f.params.pSrcB[i] = f.me.pEncMb, f.me.pEncMb
						}
					}
					MeRefineQuarPixel(&f.funcs, &f.me, &f.refine, size[0], size[1], &f.params, stride)
					return f, calls, ""
				}
				got, calls, gotPanic := run(0)
				shouldPanic := scenario == 11 || (scenario == 12 && (size[0] == 4 || size[1] == 4))
				if (gotPanic != "") != shouldPanic {
					t.Fatalf("panic=%q, expected legacy rejection=%v", gotPanic, shouldPanic)
				}
				batch := size[0] >= 8 && (scenario < 4 || scenario == 14)
				if calls != 0 {
					t.Fatalf("whole batch route unexpectedly used %d per-candidate callbacks", calls)
				}
				for route := 1; route <= 2; route++ {
					want, oldCalls, wantPanic := run(route)
					wantCalls := 0
					if route == 1 && batch {
						wantCalls = 4
					}
					if oldCalls != wantCalls {
						t.Fatalf("route%d per-candidate calls=%d, want %d", route, oldCalls, wantCalls)
					}
					if gotPanic != wantPanic {
						t.Fatalf("route%d panic=%q, want %q", route, gotPanic, wantPanic)
					}
					if got.params.iBestCost != want.params.iBestCost || got.params.iBestQuarPix != want.params.iBestQuarPix ||
						(&got.refine.pQuarPixBest[0] == &got.scratch[0]) != (&want.refine.pQuarPixBest[0] == &want.scratch[0]) {
						t.Fatalf("route%d cost/mode/swap differs", route)
					}
					for y := int32(0); y < size[1]; y++ {
						first, last := y*ME_REFINE_BUF_STRIDE, y*ME_REFINE_BUF_STRIDE+size[0]
						if !bytes.Equal(got.refine.pQuarPixBest[first:last], want.refine.pQuarPixBest[first:last]) {
							t.Fatalf("route%d selected row%d differs", route, y)
						}
					}
					if (route == 1 || !batch) && !bytes.Equal(got.scratch, want.scratch) {
						t.Fatalf("route%d scratch differs", route)
					}
				}
			})
		}
	}
}

func TestQuarterRefineFourCallbackOptOut(t *testing.T) {
	requireSampleAVX2(t)
	for _, checked := range []bool{false, true} {
		f := newQuarterBatchFixture(BLOCK_16x16)
		samples := &f.funcs.sSampleDealingFuncs
		samples.bSampleAvgSatdFour = false
		var order []int
		if checked {
			samples.pfSampleAvgSatdValidated = nil
			samples.pfSampleAvgSatd = func(_ []byte, _ int, _ int32, a []byte, off int, _ int32,
				_ []byte, _ int, _ int32, _, _ int32) int32 {
				for i := range 4 {
					if &a[off] == &f.params.pSrcA[i][f.params.iSrcAOff[i]] {
						order = append(order, i)
					}
				}
				return 100
			}
		} else {
			samples.pfSampleAvgSatdValidated = func(_ *byte, _ int, a *byte, _ int, _ *byte, _, _, _ int) int {
				for i := range 4 {
					if a == &f.params.pSrcA[i][f.params.iSrcAOff[i]] {
						order = append(order, i)
					}
				}
				return 100
			}
		}
		MeRefineQuarPixel(&f.funcs, &f.me, &f.refine, 16, 16, &f.params, 31)
		if len(order) != 4 || [4]int(order) != [4]int{0, 1, 2, 3} {
			t.Fatalf("checked=%v callback order=%v", checked, order)
		}
	}
}

func TestQuarterRefineFourRetainsNarrowNegativeStridePanic(t *testing.T) {
	requireSampleAVX2(t)
	for _, block := range []uint8{BLOCK_4x4, BLOCK_4x8, BLOCK_8x4} {
		for route := range 3 {
			f := newQuarterBatchFixture(block)
			width, height := int32(4), int32(4)
			if block == BLOCK_8x4 {
				width = 8
			}
			if block == BLOCK_4x8 {
				height = 8
			}
			f.me.iEncMbOff += int(height-1) * 31
			if route > 0 {
				f.funcs.sSampleDealingFuncs.bSampleAvgSatdFour = false
			}
			if route == 2 {
				f.funcs.sSampleDealingFuncs.pfSampleAvgSatd = nil
			}
			panicked := false
			func() {
				defer func() { panicked = recover() != nil }()
				MeRefineQuarPixel(&f.funcs, &f.me, &f.refine, width, height, &f.params, -31)
			}()
			if !panicked {
				t.Fatalf("block%d route%d changed the existing narrow negative-stride behavior", block, route)
			}
		}
	}
}
