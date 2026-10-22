package common

// Port of codec/common/src/mc.cpp (C reference implementation only).
//
// Every pixel pointer is a (slice, offset) pair; `pSrc + n` becomes
// `pSrc, iSrcOff + n` and `pSrc[k]` becomes `pSrc[iSrcOff+k]`.

// PMcChromaWidthExtFunc mirrors the internal C typedef (only used by SIMD
// paths in C, kept for completeness).
type PMcChromaWidthExtFunc func(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	kpABCD []uint8, iHeight int32)

// PWelsSampleWidthAveragingFunc mirrors the internal C typedef.
type PWelsSampleWidthAveragingFunc func(pDst []uint8, iDstOff int, iDstStride int32, pSrcA []uint8, iSrcAOff int,
	iSrcAStride int32, pSrcB []uint8, iSrcBOff int, iSrcBStride int32, iHeight int32)

// PWelsMcWidthHeightFunc: (pSrc, iSrcStride, pDst, iDstStride, iWidth, iHeight).
type PWelsMcWidthHeightFunc func(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32)

/*------------------weight for chroma fraction pixel interpolation------------------*/
//iA = (8 - dx) * (8 - dy);
//iB = dx * (8 - dy);
//iC = (8 - dx) * dy;
//iD = dx * dy
var G_kuiABCD = [8][8][4]uint8{ //g_kA[dy][dx], g_kB[dy][dx], g_kC[dy][dx], g_kD[dy][dx]
	{
		{64, 0, 0, 0}, {56, 8, 0, 0}, {48, 16, 0, 0}, {40, 24, 0, 0},
		{32, 32, 0, 0}, {24, 40, 0, 0}, {16, 48, 0, 0}, {8, 56, 0, 0},
	},
	{
		{56, 0, 8, 0}, {49, 7, 7, 1}, {42, 14, 6, 2}, {35, 21, 5, 3},
		{28, 28, 4, 4}, {21, 35, 3, 5}, {14, 42, 2, 6}, {7, 49, 1, 7},
	},
	{
		{48, 0, 16, 0}, {42, 6, 14, 2}, {36, 12, 12, 4}, {30, 18, 10, 6},
		{24, 24, 8, 8}, {18, 30, 6, 10}, {12, 36, 4, 12}, {6, 42, 2, 14},
	},
	{
		{40, 0, 24, 0}, {35, 5, 21, 3}, {30, 10, 18, 6}, {25, 15, 15, 9},
		{20, 20, 12, 12}, {15, 25, 9, 15}, {10, 30, 6, 18}, {5, 35, 3, 21},
	},
	{
		{32, 0, 32, 0}, {28, 4, 28, 4}, {24, 8, 24, 8}, {20, 12, 20, 12},
		{16, 16, 16, 16}, {12, 20, 12, 20}, {8, 24, 8, 24}, {4, 28, 4, 28},
	},
	{
		{24, 0, 40, 0}, {21, 3, 35, 5}, {18, 6, 30, 10}, {15, 9, 25, 15},
		{12, 12, 20, 20}, {9, 15, 15, 25}, {6, 18, 10, 30}, {3, 21, 5, 35},
	},
	{
		{16, 0, 48, 0}, {14, 2, 42, 6}, {12, 4, 36, 12}, {10, 6, 30, 18},
		{8, 8, 24, 24}, {6, 10, 18, 30}, {4, 12, 12, 36}, {2, 14, 6, 42},
	},
	{
		{8, 0, 56, 0}, {7, 1, 49, 7}, {6, 2, 42, 14}, {5, 3, 35, 21},
		{4, 4, 28, 28}, {3, 5, 21, 35}, {2, 6, 14, 42}, {1, 7, 7, 49},
	},
}

//***************************************************************************//
//                          C code implementation                            //
//***************************************************************************//

func McCopyWidthEq2_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iHeight int32) {
	for i := int32(0); i < iHeight; i++ { // iWidth == 2 only for chroma
		copy(pDst[iDstOff:iDstOff+2], pSrc[iSrcOff:iSrcOff+2])
		iDstOff += int(iDstStride)
		iSrcOff += int(iSrcStride)
	}
}

func McCopyWidthEq4_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iHeight int32) {
	for i := int32(0); i < iHeight; i++ {
		copy(pDst[iDstOff:iDstOff+4], pSrc[iSrcOff:iSrcOff+4])
		iDstOff += int(iDstStride)
		iSrcOff += int(iSrcStride)
	}
}

func McCopyWidthEq8_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iHeight int32) {
	for i := int32(0); i < iHeight; i++ {
		copy(pDst[iDstOff:iDstOff+8], pSrc[iSrcOff:iSrcOff+8])
		iDstOff += int(iDstStride)
		iSrcOff += int(iSrcStride)
	}
}

func McCopyWidthEq16_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iHeight int32) {
	for i := int32(0); i < iHeight; i++ {
		copy(pDst[iDstOff:iDstOff+16], pSrc[iSrcOff:iSrcOff+16])
		iDstOff += int(iDstStride)
		iSrcOff += int(iSrcStride)
	}
}

//--------------------Luma sample MC------------------//

// HorFilterInput16bit_c applies the 6-tap filter to pSrc[iOff..iOff+5].
func HorFilterInput16bit_c(pSrc []int16, iOff int) int32 {
	iPix05 := int32(pSrc[iOff+0]) + int32(pSrc[iOff+5])
	iPix14 := int32(pSrc[iOff+1]) + int32(pSrc[iOff+4])
	iPix23 := int32(pSrc[iOff+2]) + int32(pSrc[iOff+3])

	return (iPix05 - (iPix14 * 5) + (iPix23 * 20))
}

// FilterInput8bitWithStride_c applies the 6-tap filter around pSrc[iOff]
// with sample distance kiOffset (h: kiOffset=1 / v: kiOffset=iSrcStride).
func FilterInput8bitWithStride_c(pSrc []uint8, iOff int, kiOffset int32) int32 {
	kiOffset1 := int(kiOffset)
	kiOffset2 := int(kiOffset << 1)
	kiOffset3 := kiOffset1 + kiOffset2
	kuiPix05 := uint32(pSrc[iOff-kiOffset2]) + uint32(pSrc[iOff+kiOffset3])
	kuiPix14 := uint32(pSrc[iOff-kiOffset1]) + uint32(pSrc[iOff+kiOffset2])
	kuiPix23 := uint32(pSrc[iOff]) + uint32(pSrc[iOff+kiOffset1])

	return int32(kuiPix05 - ((kuiPix14 << 2) + kuiPix14) + (kuiPix23 << 4) + (kuiPix23 << 2))
}

func PixelAvg_c(pDst []uint8, iDstOff int, iDstStride int32, pSrcA []uint8, iSrcAOff int, iSrcAStride int32,
	pSrcB []uint8, iSrcBOff int, iSrcBStride int32, iWidth, iHeight int32) {
	for i := int32(0); i < iHeight; i++ {
		for j := 0; j < int(iWidth); j++ {
			pDst[iDstOff+j] = uint8((int32(pSrcA[iSrcAOff+j]) + int32(pSrcB[iSrcBOff+j]) + 1) >> 1)
		}
		iDstOff += int(iDstStride)
		iSrcAOff += int(iSrcAStride)
		iSrcBOff += int(iSrcBStride)
	}
}

func McCopy_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	if (iWidth == 16 || iWidth == 8 || iWidth == 4) &&
		copyBlockFast(pDst, iDstOff, iDstStride, pSrc, iSrcOff, iSrcStride, int(iWidth), int(iHeight)) {
		return
	}
	if iWidth == 16 {
		McCopyWidthEq16_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iHeight)
	} else if iWidth == 8 {
		McCopyWidthEq8_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iHeight)
	} else if iWidth == 4 {
		McCopyWidthEq4_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iHeight)
	} else { //here iWidth == 2
		McCopyWidthEq2_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iHeight)
	}
}

// McHorVer20_c: horizontal filter to gain half sample, that is (2, 0)
// location in quarter sample.
func McHorVer20_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	for i := int32(0); i < iHeight; i++ {
		for j := 0; j < int(iWidth); j++ {
			pDst[iDstOff+j] = WelsClip1((FilterInput8bitWithStride_c(pSrc, iSrcOff+j, 1) + 16) >> 5)
		}
		iDstOff += int(iDstStride)
		iSrcOff += int(iSrcStride)
	}
}

// McHorVer02_c: vertical filter to gain half sample, that is (0, 2)
// location in quarter sample.
func McHorVer02_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	for i := int32(0); i < iHeight; i++ {
		for j := 0; j < int(iWidth); j++ {
			pDst[iDstOff+j] = WelsClip1((FilterInput8bitWithStride_c(pSrc, iSrcOff+j, iSrcStride) + 16) >> 5)
		}
		iDstOff += int(iDstStride)
		iSrcOff += int(iSrcStride)
	}
}

// McHorVer22_c: horizontal and vertical filter to gain half sample, that is
// (2, 2) location in quarter sample.
func McHorVer22_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var iTmp [17 + 5]int16

	for i := int32(0); i < iHeight; i++ {
		for j := 0; j < int(iWidth)+5; j++ {
			iTmp[j] = int16(FilterInput8bitWithStride_c(pSrc, iSrcOff-2+j, iSrcStride))
		}
		for k := 0; k < int(iWidth); k++ {
			pDst[iDstOff+k] = WelsClip1((HorFilterInput16bit_c(iTmp[:], k) + 512) >> 10)
		}
		iSrcOff += int(iSrcStride)
		iDstOff += int(iDstStride)
	}
}

/////////////////////luma MC//////////////////////////

func McHorVer01_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiTmp [256]uint8
	McHorVer02_c(pSrc, iSrcOff, iSrcStride, uiTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, pSrc, iSrcOff, iSrcStride, uiTmp[:], 0, 16, iWidth, iHeight)
}

func McHorVer03_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiTmp [256]uint8
	McHorVer02_c(pSrc, iSrcOff, iSrcStride, uiTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, pSrc, iSrcOff+int(iSrcStride), iSrcStride, uiTmp[:], 0, 16, iWidth, iHeight)
}

func McHorVer10_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiTmp [256]uint8
	McHorVer20_c(pSrc, iSrcOff, iSrcStride, uiTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, pSrc, iSrcOff, iSrcStride, uiTmp[:], 0, 16, iWidth, iHeight)
}

func McHorVer11_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiHorTmp [256]uint8
	var uiVerTmp [256]uint8
	McHorVer20_c(pSrc, iSrcOff, iSrcStride, uiHorTmp[:], 0, 16, iWidth, iHeight)
	McHorVer02_c(pSrc, iSrcOff, iSrcStride, uiVerTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, uiHorTmp[:], 0, 16, uiVerTmp[:], 0, 16, iWidth, iHeight)
}

func McHorVer12_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiVerTmp [256]uint8
	var uiCtrTmp [256]uint8
	McHorVer02_c(pSrc, iSrcOff, iSrcStride, uiVerTmp[:], 0, 16, iWidth, iHeight)
	McHorVer22_c(pSrc, iSrcOff, iSrcStride, uiCtrTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, uiVerTmp[:], 0, 16, uiCtrTmp[:], 0, 16, iWidth, iHeight)
}

func McHorVer13_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiHorTmp [256]uint8
	var uiVerTmp [256]uint8
	McHorVer20_c(pSrc, iSrcOff+int(iSrcStride), iSrcStride, uiHorTmp[:], 0, 16, iWidth, iHeight)
	McHorVer02_c(pSrc, iSrcOff, iSrcStride, uiVerTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, uiHorTmp[:], 0, 16, uiVerTmp[:], 0, 16, iWidth, iHeight)
}

func McHorVer21_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiHorTmp [256]uint8
	var uiCtrTmp [256]uint8
	McHorVer20_c(pSrc, iSrcOff, iSrcStride, uiHorTmp[:], 0, 16, iWidth, iHeight)
	McHorVer22_c(pSrc, iSrcOff, iSrcStride, uiCtrTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, uiHorTmp[:], 0, 16, uiCtrTmp[:], 0, 16, iWidth, iHeight)
}

func McHorVer23_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiHorTmp [256]uint8
	var uiCtrTmp [256]uint8
	McHorVer20_c(pSrc, iSrcOff+int(iSrcStride), iSrcStride, uiHorTmp[:], 0, 16, iWidth, iHeight)
	McHorVer22_c(pSrc, iSrcOff, iSrcStride, uiCtrTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, uiHorTmp[:], 0, 16, uiCtrTmp[:], 0, 16, iWidth, iHeight)
}

func McHorVer30_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiHorTmp [256]uint8
	McHorVer20_c(pSrc, iSrcOff, iSrcStride, uiHorTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, pSrc, iSrcOff+1, iSrcStride, uiHorTmp[:], 0, 16, iWidth, iHeight)
}

func McHorVer31_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiHorTmp [256]uint8
	var uiVerTmp [256]uint8
	McHorVer20_c(pSrc, iSrcOff, iSrcStride, uiHorTmp[:], 0, 16, iWidth, iHeight)
	McHorVer02_c(pSrc, iSrcOff+1, iSrcStride, uiVerTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, uiHorTmp[:], 0, 16, uiVerTmp[:], 0, 16, iWidth, iHeight)
}

func McHorVer32_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiVerTmp [256]uint8
	var uiCtrTmp [256]uint8
	McHorVer02_c(pSrc, iSrcOff+1, iSrcStride, uiVerTmp[:], 0, 16, iWidth, iHeight)
	McHorVer22_c(pSrc, iSrcOff, iSrcStride, uiCtrTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, uiVerTmp[:], 0, 16, uiCtrTmp[:], 0, 16, iWidth, iHeight)
}

func McHorVer33_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32) {
	var uiHorTmp [256]uint8
	var uiVerTmp [256]uint8
	McHorVer20_c(pSrc, iSrcOff+int(iSrcStride), iSrcStride, uiHorTmp[:], 0, 16, iWidth, iHeight)
	McHorVer02_c(pSrc, iSrcOff+1, iSrcStride, uiVerTmp[:], 0, 16, iWidth, iHeight)
	PixelAvg_c(pDst, iDstOff, iDstStride, uiHorTmp[:], 0, 16, uiVerTmp[:], 0, 16, iWidth, iHeight)
}

var pWelsMcFunc = [4][4]PWelsMcWidthHeightFunc{ //[x][y]
	{McCopy_c, McHorVer01_c, McHorVer02_c, McHorVer03_c},
	{McHorVer10_c, McHorVer11_c, McHorVer12_c, McHorVer13_c},
	{McHorVer20_c, McHorVer21_c, McHorVer22_c, McHorVer23_c},
	{McHorVer30_c, McHorVer31_c, McHorVer32_c, McHorVer33_c},
}

// McLuma_c performs luma MC; pSrc has been added the offset of mv.
func McLuma_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iMvX, iMvY int16, iWidth, iHeight int32) {
	pWelsMcFunc[iMvX&0x03][iMvY&0x03](pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
}

func McChromaWithFragMv_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iMvX, iMvY int16, iWidth, iHeight int32) {
	var iA, iB, iC, iD int32
	iSrcNextOff := iSrcOff + int(iSrcStride)
	pABCD := &G_kuiABCD[iMvY&0x07][iMvX&0x07]
	iA = int32(pABCD[0])
	iB = int32(pABCD[1])
	iC = int32(pABCD[2])
	iD = int32(pABCD[3])
	for i := int32(0); i < iHeight; i++ {
		for j := 0; j < int(iWidth); j++ {
			pDst[iDstOff+j] = uint8((iA*int32(pSrc[iSrcOff+j]) + iB*int32(pSrc[iSrcOff+j+1]) +
				iC*int32(pSrc[iSrcNextOff+j]) + iD*int32(pSrc[iSrcNextOff+j+1]) + 32) >> 6)
		}
		iDstOff += int(iDstStride)
		iSrcOff = iSrcNextOff
		iSrcNextOff += int(iSrcStride)
	}
}

// McChroma_c performs chroma MC; pSrc has been added the offset of mv.
func McChroma_c(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iMvX, iMvY int16, iWidth, iHeight int32) {
	kiD8x := int32(iMvX) & 0x07
	kiD8y := int32(iMvY) & 0x07
	if 0 == kiD8x && 0 == kiD8y {
		McCopy_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
	} else {
		McChromaWithFragMv_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iMvX, iMvY, iWidth, iHeight)
	}
}

// InitMcFunc fills pMcFuncs with scalar implementations and replaces the hot
// motion kernels when the current CPU supports their instruction set.
func InitMcFunc(pMcFuncs *SMcFunc, uiCpuFlag uint32) {
	pMcFuncs.PfLumaHalfpelHor = McHorVer20_c
	pMcFuncs.PfLumaHalfpelVer = McHorVer02_c
	pMcFuncs.PfLumaHalfpelCen = McHorVer22_c
	pMcFuncs.PfSampleAveraging = PixelAvg_c
	pMcFuncs.PMcChromaFunc = McChroma_c
	pMcFuncs.PMcLumaFunc = McLuma_c
	pMcFuncs.BRefineAVX2 = false

	if uiCpuFlag&WELS_CPU_SSE2 != 0 {
		pMcFuncs.PfLumaHalfpelHor = McHorVer20_sse2
		pMcFuncs.PfLumaHalfpelVer = McHorVer02_sse2
		pMcFuncs.PfLumaHalfpelCen = McHorVer22_sse2
		pMcFuncs.PfSampleAveraging = PixelAvg_sse2
		pMcFuncs.PMcLumaFunc = McLuma_sse2
		pMcFuncs.PMcChromaFunc = McChroma_sse2
	}
	initMcAVX2(pMcFuncs, uiCpuFlag)
}
