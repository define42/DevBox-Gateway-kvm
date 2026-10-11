package processing

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

func initVaaSIMDFuncs(funcs *SVaaFuncs, cpuFlags uint32) {
	if cpuFlags&common.WELS_CPU_SSE2 != 0 {
		funcs.pfVAACalcSad = VAACalcSad_sse2
	}
}

// VAACalcSad_sse2 calculates the four 8x8 SAD values for each 16x16
// macroblock. The output order is top-left, top-right, bottom-left,
// bottom-right, matching VAACalcSad_c and the OpenH264 VAA layout.
func VAACalcSad_sse2(pCurData []uint8, iCurOff int, pRefData []uint8, iRefOff int, iPicWidth int32,
	iPicHeight int32, iPicStride int32, pFrameSad *int32, pSad8x8 [][4]int32) {
	tmpRef := iRefOff
	tmpCur := iCurOff
	mbWidth := iPicWidth >> 4
	mbHeight := iPicHeight >> 4
	mbIndex := 0
	stride8 := int(iPicStride << 3)
	step := int((iPicStride << 4) - iPicWidth)

	*pFrameSad = 0
	for mbY := int32(0); mbY < mbHeight; mbY++ {
		for mbX := int32(0); mbX < mbWidth; mbX++ {
			sad := &pSad8x8[mbIndex]
			sad[0] = common.WelsSampleSad8x8_sse2(pCurData, tmpCur, iPicStride, pRefData, tmpRef, iPicStride)
			sad[1] = common.WelsSampleSad8x8_sse2(pCurData, tmpCur+8, iPicStride, pRefData, tmpRef+8, iPicStride)
			sad[2] = common.WelsSampleSad8x8_sse2(pCurData, tmpCur+stride8, iPicStride, pRefData, tmpRef+stride8, iPicStride)
			sad[3] = common.WelsSampleSad8x8_sse2(pCurData, tmpCur+stride8+8, iPicStride, pRefData, tmpRef+stride8+8, iPicStride)
			*pFrameSad += sad[0] + sad[1] + sad[2] + sad[3]

			tmpRef += 16
			tmpCur += 16
			mbIndex++
		}
		tmpRef += step
		tmpCur += step
	}
}
