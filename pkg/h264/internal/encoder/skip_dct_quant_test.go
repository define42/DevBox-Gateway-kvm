package encoder

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type skipDctQuantResult struct {
	accepted, panicked bool
	panicText          string
}

func callSkipDctQuant(fn func(*sWelsEncCtx, *SMB, *SMbCache) bool, s *skipDctQuantPixelsState) (result skipDctQuantResult) {
	defer func() {
		if p := recover(); p != nil {
			result.panicked, result.panicText = true, fmt.Sprint(p)
		}
	}()
	result.accepted = fn(&s.ctx, &s.mb, &s.cache)
	return
}

func compareSkipDctQuantPixels(t *testing.T, got, want *skipDctQuantPixelsState) {
	t.Helper()
	g := callSkipDctQuant(welsTryPYskipPixels, got)
	w := callSkipDctQuant(legacySkipDctQuantPixels, want)
	if g != w || got.coefficients != want.coefficients || got.prediction != want.prediction ||
		got.levels != want.levels || got.alias != want.alias || !slices.Equal(got.source, want.source) ||
		!reflect.DeepEqual(got.cache, want.cache) || !reflect.DeepEqual(got.mb, want.mb) || got.layer.iEncStride != want.layer.iEncStride {
		t.Fatalf("lazy result/panic/partial writes or state differs: got=%+v want=%+v", g, w)
	}
}

func TestSkipDctQuantPixelsQPAndPatterns(t *testing.T) {
	for qp := 0; qp < 52; qp++ {
		for _, stride := range []int32{0, 1, 16, 19, 32, 1984, -19} {
			for pattern := 0; pattern < 8; pattern++ {
				t.Run(fmt.Sprintf("QP%d/stride%d/pattern%d", qp, stride, pattern), func(t *testing.T) {
					got, want := newSkipDctQuantPixelsState(stride, uint8(qp), pattern), newSkipDctQuantPixelsState(stride, uint8(qp), pattern)
					compareSkipDctQuantPixels(t, got, want)
				})
			}
		}
	}
}

func TestSkipDctQuantPixelsMalformed(t *testing.T) {
	cases := []struct {
		name   string
		change func(*skipDctQuantPixelsState)
	}{
		{"nil-levels", func(s *skipDctQuantPixelsState) { s.cache.pDct = nil }},
		{"QP52", func(s *skipDctQuantPixelsState) { s.mb.uiLumaQp = 52 }},
		{"QP255", func(s *skipDctQuantPixelsState) { s.mb.uiLumaQp = 255 }},
		{"negative-offset", func(s *skipDctQuantPixelsState) { s.cache.SPicData.iEncMbOff[0] = -1 }},
		{"max-offset", func(s *skipDctQuantPixelsState) { s.cache.SPicData.iEncMbOff[0] = int(^uint(0) >> 1) }},
		{"min-stride", func(s *skipDctQuantPixelsState) { s.layer.iEncStride[0] = -1 << 31 }},
		{"max-stride", func(s *skipDctQuantPixelsState) { s.layer.iEncStride[0] = 1<<31 - 1 }},
		{"nil-transform", func(s *skipDctQuantPixelsState) { s.functions.pfDctFourT4 = nil }},
		{"nil-function-table", func(s *skipDctQuantPixelsState) { s.ctx.pFuncList = nil }},
	}
	for _, length := range []int{0, 63, 64, 127, 128, 191, 192, 255} {
		cases = append(cases, struct {
			name   string
			change func(*skipDctQuantPixelsState)
		}{
			fmt.Sprintf("coeff-len%d", length), func(s *skipDctQuantPixelsState) { s.cache.pCoeffLevel = s.cache.pCoeffLevel[:length] }})
	}
	for _, length := range []int{0, 7, 8, 119, 120, 127, 128, 247, 248, 255} {
		cases = append(cases, struct {
			name   string
			change func(*skipDctQuantPixelsState)
		}{
			fmt.Sprintf("pred-len%d", length), func(s *skipDctQuantPixelsState) { s.cache.pSkipMb = s.cache.pSkipMb[:length] }})
	}
	for _, length := range []int{0, 15, 7 + 7*32 + 7, 7 + 7*32 + 8, 7 + 15*32 + 15} {
		cases = append(cases, struct {
			name   string
			change func(*skipDctQuantPixelsState)
		}{
			fmt.Sprintf("source-len%d", length), func(s *skipDctQuantPixelsState) { s.cache.SPicData.pEncMb[0] = s.source[:length] }})
	}
	for _, tc := range cases {
		for _, pattern := range []int{0, 1, 4, 6} {
			t.Run(fmt.Sprintf("%s/pattern%d", tc.name, pattern), func(t *testing.T) {
				got, want := newSkipDctQuantPixelsState(32, 32, pattern), newSkipDctQuantPixelsState(32, 32, pattern)
				tc.change(got)
				tc.change(want)
				compareSkipDctQuantPixels(t, got, want)
			})
		}
	}
}

func TestSkipDctQuantPixelsAliases(t *testing.T) {
	for _, alias := range []string{"source-coeff", "prediction-coeff", "both-coeff", "levels-coeff", "levels-source", "levels-prediction"} {
		for _, shift := range []int{0, 1, 17, 63, 127, 255} {
			t.Run(fmt.Sprintf("%s/shift%d", alias, shift), func(t *testing.T) {
				got, want := newSkipDctQuantPixelsState(32, 32, 6), newSkipDctQuantPixelsState(32, 32, 6)
				for _, s := range []*skipDctQuantPixelsState{got, want} {
					bytes := unsafe.Slice((*byte)(unsafe.Pointer(&s.alias[0])), len(s.alias)*2)
					s.cache.pCoeffLevel = s.alias[128:512]
					if alias == "source-coeff" || alias == "both-coeff" {
						s.cache.SPicData.pEncMb[0], s.cache.SPicData.iEncMbOff[0] = bytes, shift
					}
					if alias == "prediction-coeff" || alias == "both-coeff" {
						s.cache.pSkipMb = bytes[shift : shift+384]
					}
					if alias == "levels-coeff" {
						s.cache.pDct = (*SDCTCoeff)(unsafe.Pointer(&s.alias[128+shift]))
					}
					if alias == "levels-source" {
						s.cache.pDct = (*SDCTCoeff)(unsafe.Pointer(&s.alias[shift]))
						s.cache.pCoeffLevel = s.coefficients[8:392]
						s.cache.SPicData.pEncMb[0], s.cache.SPicData.iEncMbOff[0] = bytes, 7
					}
					if alias == "levels-prediction" {
						s.cache.pDct = (*SDCTCoeff)(unsafe.Pointer(&s.alias[shift]))
						s.cache.pCoeffLevel = s.coefficients[8:392]
						s.cache.pSkipMb = bytes[:384]
					}
				}
				compareSkipDctQuantPixels(t, got, want)
			})
		}
	}
}

func TestSkipDctQuantPixelsCustomCallback(t *testing.T) {
	for _, mutation := range []string{"trace", "replace-transform", "replace-coeff", "replace-prediction", "nil-levels", "QP255"} {
		got, want := newSkipDctQuantPixelsState(32, 32, 5), newSkipDctQuantPixelsState(32, 32, 5)
		var traces [2][][5]int
		for index, s := range []*skipDctQuantPixelsState{got, want} {
			calls, original := 0, s.functions.pfDctFourT4
			s.functions.pfDctFourT4 = func(d []int16, a []byte, ao int, as int32, p []byte, po int, ps int32) {
				traces[index] = append(traces[index], [5]int{len(d), ao, int(as), po, int(ps)})
				original(d, a, ao, as, p, po, ps)
				calls++
				if calls != 1 {
					return
				}
				switch mutation {
				case "replace-transform":
					s.functions.pfDctFourT4 = WelsDctFourT4_c
				case "replace-coeff":
					s.cache.pCoeffLevel = s.alias[128:512]
				case "replace-prediction":
					s.cache.pSkipMb = s.source[:384]
				case "nil-levels":
					s.cache.pDct = nil
				case "QP255":
					s.mb.uiLumaQp = 255 // current attempt retains the entry QP
				}
			}
		}
		t.Run(mutation, func(t *testing.T) {
			compareSkipDctQuantPixels(t, got, want)
			if !reflect.DeepEqual(traces[0], traces[1]) {
				t.Fatal("custom DCT callback trace differs")
			}
		})
	}
}

func TestSkipDctQuantPixelsAllocations(t *testing.T) {
	for _, pattern := range []int{0, 1, 4, 5, 6} {
		s := newSkipDctQuantPixelsState(1984, 32, pattern)
		if got := testing.AllocsPerRun(100, func() { welsTryPYskipPixels(&s.ctx, &s.mb, &s.cache) }); got != 0 {
			t.Fatalf("pattern%d: lazy skip allocated %g times", pattern, got)
		}
	}
	for _, scenario := range []string{"Direct", "RejectLuma", "RejectU", "RejectV", "TransformSuccess"} {
		s := newSkipDctQuantCallerState(scenario, common.WelsCPUFeatureDetect(nil), true)
		// Compare to the unchanged complete caller, whose callback-related
		// allocations are outside this candidate's scope.
		want := testing.AllocsPerRun(100, func() { legacySkipDctQuantCallerWelsMdPSkipEnc(&s.ctx, &s.md, &s.blocks[1], &s.cache) })
		got := testing.AllocsPerRun(100, func() { WelsMdPSkipEnc(&s.ctx, &s.md, &s.blocks[1], &s.cache) })
		if got != want {
			t.Fatalf("%s: complete caller allocations %g, baseline %g", scenario, got, want)
		}
	}
}

func TestSkipDctQuantPixelsScoreBoundary(t *testing.T) {
	// At QP12, a uniform +1 4x4 residual quantizes to DC=1 (score 3).
	// Rows [1,1,-1,-1] quantize only horizontal AC=1 (score 2). These
	// establish the strict boundary directly, including cross-group carry.
	for _, second := range []int{1, 4, 15} {
		for _, ac := range []bool{true, false} {
			got, want := newSkipDctQuantPixelsState(32, 12, 0), newSkipDctQuantPixelsState(32, 12, 0)
			for _, s := range []*skipDctQuantPixelsState{got, want} {
				for _, block := range []int{0, second} {
					group, sub := block/4, block%4
					x, y := (group&1)*8+(sub&1)*4, (group>>1)*8+(sub>>1)*4
					for row := 0; row < 4; row++ {
						for col := 0; col < 4; col++ {
							v := byte(129)
							if block == second && ac && col >= 2 {
								v = 127
							}
							s.source[7+(y+row)*32+x+col] = v
						}
					}
				}
			}
			t.Run(fmt.Sprintf("second%d/AC%t", second, ac), func(t *testing.T) {
				compareSkipDctQuantPixels(t, got, want)
				if accepted := welsTryPYskipPixels(&got.ctx, &got.mb, &got.cache); accepted != ac {
					t.Fatalf("score5 must accept; score6 must reject: AC=%t accepted=%t", ac, accepted)
				}
			})
		}
	}
}
