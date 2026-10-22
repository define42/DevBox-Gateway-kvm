//go:build amd64

package encoder

import "golang.org/x/sys/cpu"

//go:noescape
func vaaIntraSumsSSSE3(src *byte, stride int) (sumAvg, sumSqr int32)

// analysisVaaInfoIntraFast returns the 16x16 variance of the sixteen 4x4 block
// averages, or false when SSSE3 is missing or the block span is not a
// positive-stride range inside pDataY; the caller then runs the scalar code.
func analysisVaaInfoIntraFast(pDataY []uint8, iDataYOff int, kiLineSize int32) (int32, bool) {
	if !cpu.X86.HasSSSE3 || kiLineSize < 16 || iDataYOff < 0 || iDataYOff+15*int(kiLineSize)+16 > len(pDataY) {
		return 0, false
	}
	sum, sqr := vaaIntraSumsSSSE3(&pDataY[iDataYOff], int(kiLineSize))
	return sqr - ((sum * sum) >> 4), true
}
