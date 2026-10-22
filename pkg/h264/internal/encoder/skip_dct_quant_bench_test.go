package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type skipDctQuantPixelsState struct {
	ctx          sWelsEncCtx
	layer        SDqLayer
	functions    SWelsFuncPtrList
	mb           SMB
	cache        SMbCache
	coefficients [400]int16
	prediction   [400]byte
	levels       SDCTCoeff
	source       []byte
	alias        [1024]int16
}

func newSkipDctQuantPixelsState(stride int32, qp uint8, pattern int) *skipDctQuantPixelsState {
	s := &skipDctQuantPixelsState{}
	s.ctx.pFuncList, s.ctx.pCurDqLayer = &s.functions, &s.layer
	s.layer.iEncStride[0], s.mb.uiLumaQp = stride, qp
	WelsInitEncodingFuncs(&s.functions, common.WelsCPUFeatureDetect(nil))
	s.cache.pCoeffLevel, s.cache.pSkipMb = s.coefficients[8:392], s.prediction[8:392]
	s.cache.pDct = &s.levels
	absStride := int(stride)
	if absStride < 0 {
		absStride = -absStride
	}
	s.source = make([]byte, 7+15*absStride+16+9)
	for i := range s.source {
		s.source[i] = 128
	}
	for i := range s.prediction {
		s.prediction[i] = 128
	}
	for i := range s.coefficients {
		s.coefficients[i] = int16(i*101 + 17)
	}
	for i := range s.levels.iLumaBlock {
		s.levels.iLumaBlock[i] = int16(i*71 - 3001)
	}
	for i := range s.alias {
		s.alias[i] = int16(i*1701 + 13)
	}
	offset := 7
	if stride < 0 {
		offset += 15 * absStride
	}
	s.cache.SPicData.pEncMb[0], s.cache.SPicData.iEncMbOff[0] = s.source, offset
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			v := byte(128)
			switch {
			case pattern >= 1 && pattern <= 4:
				if (y/8)*2+x/8 == pattern-1 {
					v = 255
				}
			case pattern == 5:
				if (y*16+x)%53 == 0 {
					v = 129
				}
			case pattern == 6:
				v = byte(125 + (x*7+y*11)%7)
			case pattern == 7:
				v = byte((x*73 + y*151) & 255)
			}
			s.source[offset+y*int(stride)+x] = v
		}
	}
	return s
}

var skipDctQuantPixelsBenchmarkSink bool

// Baseline-compatible complete lazy caller: includes the function-identity
// check, full-span preflight, transform, quantization, score and scan work.
func BenchmarkSkipDctQuantPixels(b *testing.B) {
	for _, stride := range []int32{32, 1984} {
		for pattern, name := range []string{"Zero", "Reject0", "Reject1", "Reject2", "Reject3", "Sparse", "Dense", "FullRange", "WrappedZero"} {
			b.Run(fmt.Sprintf("%s/stride%d", name, stride), func(b *testing.B) {
				s := newSkipDctQuantPixelsState(stride, 32, pattern)
				if name == "WrappedZero" {
					dct := s.functions.pfDctFourT4
					s.functions.pfDctFourT4 = func(d []int16, a []byte, ao int, as int32, c []byte, co int, cs int32) {
						dct(d, a, ao, as, c, co, cs)
					}
				}
				var got bool
				b.ReportAllocs()
				for b.Loop() {
					got = welsTryPYskipPixels(&s.ctx, &s.mb, &s.cache)
				}
				skipDctQuantPixelsBenchmarkSink = got
			})
		}
	}
	b.Run("BackwardZero", func(b *testing.B) {
		s := newSkipDctQuantPixelsState(-32, 32, 0)
		var got bool
		b.ReportAllocs()
		for b.Loop() {
			got = welsTryPYskipPixels(&s.ctx, &s.mb, &s.cache)
		}
		skipDctQuantPixelsBenchmarkSink = got
	})
}
