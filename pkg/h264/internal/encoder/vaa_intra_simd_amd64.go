//go:build amd64

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"golang.org/x/sys/cpu"
)

//go:noescape
func intraVaaSSSE3(src *byte, stride int) int32

func initIntraVaaSIMD(funcs *SWelsFuncPtrList, flags uint32) {
	if flags&common.WELS_CPU_SSE2 != 0 {
		funcs.pfUpdateMbMv = UpdateMbMv_sse2
	}
	if flags&common.WELS_CPU_SSSE3 != 0 && cpu.X86.HasSSSE3 {
		funcs.pfGetVarianceFromIntraVaa = AnalysisVaaInfoIntra_ssse3
	}
}

// AnalysisVaaInfoIntra_ssse3 computes the variance of sixteen floored 4x4
// means. Every source row is checked before the exact sixteen-byte SIMD load.
func AnalysisVaaInfoIntra_ssse3(src []byte, off int, stride int32) int32 {
	// Retain the scalar signed-stride path and its int32-derived row offsets
	// when multiplying the stride by four would overflow that representation.
	if !cpu.X86.HasSSSE3 || stride < 0 || stride > (1<<31-1)/4 {
		return AnalysisVaaInfoIntra_c(src, off, stride)
	}
	checkSampleSIMDBlock(src, off, stride, 16, 16)
	return intraVaaSSSE3(&src[off], int(stride))
}
