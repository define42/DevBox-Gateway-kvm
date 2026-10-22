//go:build amd64

package encoder

// Each descriptor remains fully tracked by Go. The leaf assembly reads these
// four 32-byte records but never modifies or retains their pointers.
type sampleAvgSatdCandidate struct {
	a, b             *byte
	aStride, bStride int
}

//go:noescape
func satdAvgFourAVX2(enc *byte, encStride int, candidates *[4]sampleAvgSatdCandidate,
	width, height int, costs *[4]int32)

// Only meRefineQuarPixelFused calls this entry, after validating all input and
// output spans and excluding aliases that could alter sequential behavior.
func sampleAvgSatdFourValidated(enc *byte, encStride int, params *SQuarRefineParams,
	width, height int) (costs [4]int32) {
	var candidates [4]sampleAvgSatdCandidate
	for i := range candidates {
		stride := params.iStrideA
		if i >= 2 {
			stride = params.iStrideB
		}
		candidates[i] = sampleAvgSatdCandidate{
			a:       &params.pSrcA[i][params.iSrcAOff[i]],
			b:       &params.pSrcB[i][params.iSrcBOff[i]],
			aStride: ME_REFINE_BUF_STRIDE,
			bStride: int(stride),
		}
	}
	satdAvgFourAVX2(enc, encStride, &candidates, width, height, &costs)
	return costs
}
