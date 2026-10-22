//go:build amd64

package processing

import (
	"unsafe"

	"golang.org/x/sys/cpu"
)

//go:noescape
func vaaSadRowAVX2(cur, ref *byte, stride int, sad *[4]int32, macroblocks int) int32

// VAACalcSad_avx2 preserves the SSE2 wrapper's validation and per-row frame
// commits while calculating two adjacent macroblocks per 32-byte load.
// A final odd macroblock uses exact 16-byte loads in the same assembly call.
func VAACalcSad_avx2(pCurData []uint8, iCurOff int, pRefData []uint8, iRefOff int, iPicWidth int32,
	iPicHeight int32, iPicStride int32, pFrameSad *int32, pSad8x8 [][4]int32) {
	if !cpu.X86.HasAVX2 {
		VAACalcSad_sse2(pCurData, iCurOff, pRefData, iRefOff, iPicWidth, iPicHeight,
			iPicStride, pFrameSad, pSad8x8)
		return
	}
	*pFrameSad = 0
	if iPicWidth <= 0 || iPicHeight <= 0 {
		return
	}
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
	// The legacy kernel reads and commits one macroblock at a time. Loading
	// two blocks together is equivalent only when result stores cannot alter
	// later pixel reads. Keep its original row/store order for input aliases.
	out := uintptr(unsafe.Pointer(&pSad8x8[0]))
	outEnd := out + uintptr(mbWidth)*uintptr(mbHeight)*16
	cur := uintptr(unsafe.Pointer(&pCurData[iCurOff]))
	ref := uintptr(unsafe.Pointer(&pRefData[iRefOff]))
	if (out < cur+uintptr(frameSpan) && cur < outEnd) ||
		(out < ref+uintptr(frameSpan) && ref < outEnd) {
		VAACalcSad_sse2(pCurData, iCurOff, pRefData, iRefOff, iPicWidth, iPicHeight,
			iPicStride, pFrameSad, pSad8x8)
		return
	}
	for y := 0; y < mbHeight; y++ {
		curRow := pCurData[iCurOff : iCurOff+int(rowSpan)]
		refRow := pRefData[iRefOff : iRefOff+int(rowSpan)]
		rowSad := pSad8x8[y*mbWidth : (y+1)*mbWidth]
		*pFrameSad += vaaSadRowAVX2(&curRow[0], &refRow[0], int(iPicStride), &rowSad[0], mbWidth)
		iCurOff += int(iPicStride) * 16
		iRefOff += int(iPicStride) * 16
	}
}
