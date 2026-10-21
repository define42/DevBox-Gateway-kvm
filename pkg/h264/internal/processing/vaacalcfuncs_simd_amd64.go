package processing

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

// vaaSadRowSSE2 processes complete 16x16 macroblocks, retaining each 8x8
// quadrant's sum. The caller validates both input spans and the output length.
//
//go:noescape
func vaaSadRowSSE2(cur, ref []byte, stride int, sad [][4]int32) int32

func initVaaSIMDFuncs(funcs *SVaaFuncs, cpuFlags uint32) {
	if cpuFlags&common.WELS_CPU_SSE2 != 0 {
		funcs.pfVAACalcSad = VAACalcSad_sse2
	}
}

// VAACalcSad_sse2 batches a macroblock row into one checked assembly call.
// A 16-byte PSADBW retains both horizontal 8x8 sums, halving load instructions
// compared with four separate SAD calls per macroblock.
func VAACalcSad_sse2(pCurData []uint8, iCurOff int, pRefData []uint8, iRefOff int, iPicWidth int32,
	iPicHeight int32, iPicStride int32, pFrameSad *int32, pSad8x8 [][4]int32) {
	*pFrameSad = 0
	if iPicWidth <= 0 || iPicHeight <= 0 {
		return
	}
	// Preserve the reference implementation's partial-block behavior.
	if iPicWidth&15 != 0 || iPicHeight&15 != 0 {
		VAACalcSad_c(pCurData, iCurOff, pRefData, iRefOff, iPicWidth, iPicHeight,
			iPicStride, pFrameSad, pSad8x8)
		return
	}
	mbWidth := int(iPicWidth >> 4)
	mbHeight := int(iPicHeight >> 4)
	rowSpan := int64(iPicStride)*15 + int64(iPicWidth)
	frameSpan := int64(iPicStride)*int64(iPicHeight-1) + int64(iPicWidth)
	if iPicStride < 0 || iCurOff < 0 || iRefOff < 0 ||
		frameSpan > int64(len(pCurData))-int64(iCurOff) ||
		frameSpan > int64(len(pRefData))-int64(iRefOff) ||
		int64(mbWidth)*int64(mbHeight) > int64(len(pSad8x8)) {
		panic("h264: VAA buffer span out of range")
	}
	for y := 0; y < mbHeight; y++ {
		cur := pCurData[iCurOff : iCurOff+int(rowSpan)]
		ref := pRefData[iRefOff : iRefOff+int(rowSpan)]
		*pFrameSad += vaaSadRowSSE2(cur, ref, int(iPicStride), pSad8x8[y*mbWidth:(y+1)*mbWidth])
		iCurOff += int(iPicStride) * 16
		iRefOff += int(iPicStride) * 16
	}
}
