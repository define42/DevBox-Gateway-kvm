package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// This entire file uses pre-existing APIs and can be copied unchanged into
// round7 for a complete-caller benchmark without a compatibility shim.
type chromaSkipCallerFixture struct {
	ctx          sWelsEncCtx
	layer        SDqLayer
	pps          SWelsPPS
	funcs        SWelsFuncPtrList
	mb           SMB
	cache        SMbCache
	coefficients [384]int16
	levels       SDCTCoeff
	input        [64]int16
	offset       int
	uv           int32
}

func newChromaSkipCallerFixture(qp int, uv int32, content string) *chromaSkipCallerFixture {
	f := &chromaSkipCallerFixture{mb: SMB{uiLumaQp: uint8(qp)}, uv: uv, offset: 256}
	if uv != 1 {
		f.offset = 320
	}
	f.ctx.pCurDqLayer, f.ctx.pFuncList = &f.layer, &f.funcs
	f.layer.sLayerInfo.pPpsP = &f.pps
	f.cache.pCoeffLevel, f.cache.pDct = f.coefficients[:], &f.levels
	WelsInitEncodingFuncs(&f.funcs, common.WELS_CPU_SSE2)
	for i := range f.coefficients {
		f.coefficients[i] = int16(i*31 + 19)
	}
	for i := range f.levels.iChromaBlock {
		f.levels.iChromaBlock[i] = int16(i*13 + 71)
	}
	chromaQP := common.G_kuiChromaQpTable[qp]
	ff, mf := int32(g_kiQuantInterFF[chromaQP][0]), int32(g_kiQuantMF[chromaQP][0])
	threshold := 65535/(mf>>1) - (ff << 1)
	switch content {
	case "DCReject":
		f.input[0] = int16(threshold + 1)
	case "DCBoundary":
		f.input[0] = int16(threshold)
	case "SparseAC":
		for _, offset := range []int{1, 17, 33, 49} {
			f.input[offset] = int16((65536+mf-1)/mf - ff)
		}
	case "ACReject":
		f.input[1] = 32767
	case "Dense":
		for i := range f.input {
			f.input[i] = int16(i*71 - 2000)
		}
	case "Extremes":
		for i := range f.input {
			f.input[i] = []int16{-32768, 32767, -1, 0, 1}[i%5]
		}
	}
	return f
}

func (f *chromaSkipCallerFixture) reset() { copy(f.coefficients[f.offset:f.offset+64], f.input[:]) }

var chromaSkipCallerBenchmarkSink bool

func BenchmarkChromaSkipCaller(b *testing.B) {
	for _, qp := range []int{0, 26, 51} {
		for _, uv := range []int32{1, 2} {
			for _, content := range []string{"Zero", "DCReject", "DCBoundary", "SparseAC", "ACReject", "Dense"} {
				b.Run(fmt.Sprintf("QP%d/UV%d/%s", qp, uv, content), func(b *testing.B) {
					f := newChromaSkipCallerFixture(qp, uv, content)
					b.ReportAllocs()
					for b.Loop() {
						f.reset()
						chromaSkipCallerBenchmarkSink = WelsTryPUVskip(&f.ctx, &f.mb, &f.cache, f.uv)
					}
				})
			}
		}
	}
}
