//go:build !amd64

package encoder

func initIntraVaaSIMD(_ *SWelsFuncPtrList, _ uint32) {}

func AnalysisVaaInfoIntra_ssse3(src []byte, off int, stride int32) int32 {
	return AnalysisVaaInfoIntra_c(src, off, stride)
}
