// Port of codec/encoder/core/src/svc_base_layer_md.cpp (mode decision of
// the base layer).

package encoder

import (
	"math"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var g_kiIntra16AvaliMode = [8][5]int8{
	{common.I16_PRED_DC_128, common.I16_PRED_INVALID, common.I16_PRED_INVALID, common.I16_PRED_INVALID, 1},
	{common.I16_PRED_DC_L, common.I16_PRED_H, common.I16_PRED_INVALID, common.I16_PRED_INVALID, 2},
	{common.I16_PRED_DC_T, common.I16_PRED_V, common.I16_PRED_INVALID, common.I16_PRED_INVALID, 2},
	{common.I16_PRED_V, common.I16_PRED_H, common.I16_PRED_DC, common.I16_PRED_INVALID, 3},
	{common.I16_PRED_DC_128, common.I16_PRED_INVALID, common.I16_PRED_INVALID, common.I16_PRED_INVALID, 1},
	{common.I16_PRED_DC_L, common.I16_PRED_H, common.I16_PRED_INVALID, common.I16_PRED_INVALID, 2},
	{common.I16_PRED_DC_T, common.I16_PRED_V, common.I16_PRED_INVALID, common.I16_PRED_INVALID, 2},
	{common.I16_PRED_V, common.I16_PRED_H, common.I16_PRED_DC, common.I16_PRED_P, 4},
}

// I4_PRED_MODE_EXTEND is not defined.
var g_kiIntra4AvailCount = [16]uint8{
	1, 3, 2, 4, 1, 3, 2, 7, 1, 3, 4, 6, 1, 3, 4, 9,
}

// left_avail | (top_avail<<1) | (left_top_avail<<2) | (right_top_avail<<3);
var g_kiIntra4AvailMode = [16][16]uint8{
	{
		common.I4_PRED_DC_128, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  0000

	{
		common.I4_PRED_DC_L, common.I4_PRED_H, common.I4_PRED_HU, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  0001

	{
		common.I4_PRED_DC_T, common.I4_PRED_V, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  0010

	{
		common.I4_PRED_DC, common.I4_PRED_H, common.I4_PRED_V, common.I4_PRED_HU,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  0011

	{
		common.I4_PRED_DC_128, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  0100

	{
		common.I4_PRED_DC_L, common.I4_PRED_H, common.I4_PRED_HU, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  0101

	{
		common.I4_PRED_DC_T, common.I4_PRED_V, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  0110

	{
		common.I4_PRED_DC, common.I4_PRED_H, common.I4_PRED_V, common.I4_PRED_HU,
		common.I4_PRED_DDR, common.I4_PRED_VR, common.I4_PRED_HD, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  0111

	{
		common.I4_PRED_DC_128, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  1000

	{
		common.I4_PRED_DC_L, common.I4_PRED_H, common.I4_PRED_HU, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  1001

	{
		common.I4_PRED_DC_T, common.I4_PRED_V, common.I4_PRED_DDL, common.I4_PRED_VL,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  1010

	{
		common.I4_PRED_DC, common.I4_PRED_H, common.I4_PRED_V, common.I4_PRED_HU,
		common.I4_PRED_DDL, common.I4_PRED_VL, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  1011

	{
		common.I4_PRED_DC_128, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  1100

	{
		common.I4_PRED_DC_L, common.I4_PRED_H, common.I4_PRED_HU, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  1101

	{
		common.I4_PRED_DC_T, common.I4_PRED_V, common.I4_PRED_DDL, common.I4_PRED_VL,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  1110

	{
		common.I4_PRED_DC, common.I4_PRED_H, common.I4_PRED_V, common.I4_PRED_HU,
		common.I4_PRED_DDL, common.I4_PRED_VL, common.I4_PRED_DDR, common.I4_PRED_VR,
		common.I4_PRED_HD, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
		common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID, common.I4_PRED_INVALID,
	}, //  1111
}

var g_kiIntraChromaAvailMode = [8][5]int8{
	{common.C_PRED_DC_128, common.C_PRED_INVALID, common.C_PRED_INVALID, common.C_PRED_INVALID, 1},
	{common.C_PRED_DC_L, common.C_PRED_H, common.C_PRED_INVALID, common.C_PRED_INVALID, 2},
	{common.C_PRED_DC_T, common.C_PRED_V, common.C_PRED_INVALID, common.C_PRED_INVALID, 2},
	{common.C_PRED_V, common.C_PRED_H, common.C_PRED_DC, common.C_PRED_INVALID, 3},
	{common.C_PRED_DC_128, common.C_PRED_INVALID, common.C_PRED_INVALID, common.C_PRED_INVALID, 1},
	{common.C_PRED_DC_L, common.C_PRED_H, common.C_PRED_INVALID, common.C_PRED_INVALID, 2},
	{common.C_PRED_DC_T, common.C_PRED_V, common.C_PRED_INVALID, common.C_PRED_INVALID, 2},
	{common.C_PRED_V, common.C_PRED_H, common.C_PRED_DC, common.C_PRED_P, 4},
}

// for cache hit, two table are total sizeof 64 Bytes
var g_kiCoordinateIdx4x4X = [16]int8{0, 4, 0, 4,
	8, 12, 8, 12,
	0, 4, 0, 4,
	8, 12, 8, 12,
}

var g_kiCoordinateIdx4x4Y = [16]int8{0, 0, 4, 4,
	0, 0, 4, 4,
	8, 8, 12, 12,
	8, 8, 12, 12,
}

var g_kiNeighborIntraToI4x4 = [16][16]int8{
	{0, 1, 10, 7, 1, 1, 15, 7, 10, 15, 10, 7, 15, 7, 15, 7},
	{1, 1, 15, 7, 1, 1, 15, 7, 15, 15, 15, 7, 15, 7, 15, 7},
	{10, 15, 10, 7, 15, 7, 15, 7, 10, 15, 10, 7, 15, 7, 15, 7},
	{11, 15, 15, 7, 15, 7, 15, 7, 15, 15, 15, 7, 15, 7, 15, 7},
	{4, 1, 10, 7, 1, 1, 15, 7, 10, 15, 10, 7, 15, 7, 15, 7},
	{5, 1, 15, 7, 1, 1, 15, 7, 15, 15, 15, 7, 15, 7, 15, 7},
	{14, 15, 10, 7, 15, 7, 15, 7, 10, 15, 10, 7, 15, 7, 15, 7},
	{15, 15, 15, 7, 15, 7, 15, 7, 15, 15, 15, 7, 15, 7, 15, 7},
	{0, 1, 10, 7, 1, 9, 15, 7, 10, 15, 10, 7, 15, 7, 15, 7},
	{1, 1, 15, 7, 1, 9, 15, 7, 15, 15, 15, 7, 15, 7, 15, 7},
	{10, 15, 10, 7, 15, 15, 15, 7, 10, 15, 10, 7, 15, 7, 15, 7},
	{11, 15, 15, 7, 15, 15, 15, 7, 15, 15, 15, 7, 15, 7, 15, 7},
	{4, 1, 10, 7, 1, 9, 15, 7, 10, 15, 10, 7, 15, 7, 15, 7},
	{5, 1, 15, 7, 1, 9, 15, 7, 15, 15, 15, 7, 15, 7, 15, 7},
	{14, 15, 10, 7, 15, 15, 15, 7, 10, 15, 10, 7, 15, 7, 15, 7},
	{15, 15, 15, 7, 15, 15, 15, 7, 15, 15, 15, 7, 15, 7, 15, 7},
}

var g_kiMapModeI4x4 = [14]int8{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 2, 2, 2, 3, 7,
}

// pIntraPredMode: SMbCache.iIntraPredMode (whole cache), iIdx4 >= 8.
func PredIntra4x4Mode(pIntraPredMode []int8, iIdx4 int32) int32 {
	iTopMode := pIntraPredMode[iIdx4-8]
	iLeftMode := pIntraPredMode[iIdx4-1]
	var iBestMode int8

	if -1 == iLeftMode || -1 == iTopMode {
		iBestMode = 2
	} else {
		iBestMode = common.WELS_MIN(iLeftMode, iTopMode)
	}
	return int32(iBestMode)
}

func WelsMdIntraInit(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache, iSliceFirstMbXY int32) {
	pCurLayer := pEncCtx.pCurDqLayer

	kiMbX := int32(pCurMb.iMbX)
	kiMbY := int32(pCurMb.iMbY)
	kiMbXY := pCurMb.iMbXY
	pPicData := &pMbCache.SPicData

	// step 3. locating current pEnc and pDec
	// unroll loops here
	if 0 == kiMbX || iSliceFirstMbXY == kiMbXY {
		var iStrideY, iStrideUV int32
		var iOffsetY, iOffsetUV int32

		iStrideY = pCurLayer.iEncStride[0]
		iStrideUV = pCurLayer.iEncStride[1]
		iOffsetY = (kiMbX + kiMbY*iStrideY) << 4
		iOffsetUV = (kiMbX + kiMbY*iStrideUV) << 3
		pPicData.pEncMb[0], pPicData.iEncMbOff[0] = pCurLayer.pEncData[0], pCurLayer.iEncDataOff[0]+int(iOffsetY)
		pPicData.pEncMb[1], pPicData.iEncMbOff[1] = pCurLayer.pEncData[1], pCurLayer.iEncDataOff[1]+int(iOffsetUV)
		pPicData.pEncMb[2], pPicData.iEncMbOff[2] = pCurLayer.pEncData[2], pCurLayer.iEncDataOff[2]+int(iOffsetUV)

		iStrideY = pCurLayer.iCsStride[0]
		iStrideUV = pCurLayer.iCsStride[1]
		iOffsetY = (kiMbX + kiMbY*iStrideY) << 4
		iOffsetUV = (kiMbX + kiMbY*iStrideUV) << 3
		pPicData.pCsMb[0], pPicData.iCsMbOff[0] = pCurLayer.pCsData[0], pCurLayer.iCsDataOff[0]+int(iOffsetY)
		pPicData.pCsMb[1], pPicData.iCsMbOff[1] = pCurLayer.pCsData[1], pCurLayer.iCsDataOff[1]+int(iOffsetUV)
		pPicData.pCsMb[2], pPicData.iCsMbOff[2] = pCurLayer.pCsData[2], pCurLayer.iCsDataOff[2]+int(iOffsetUV)

		pDecPic := pCurLayer.pDecPic
		iStrideY = pDecPic.iLineSize[0]
		iStrideUV = pDecPic.iLineSize[1]
		iOffsetY = (kiMbX + kiMbY*iStrideY) << 4
		iOffsetUV = (kiMbX + kiMbY*iStrideUV) << 3
		pPicData.pDecMb[0], pPicData.iDecMbOff[0] = pDecPic.pData[0], pDecPic.iDataOff[0]+int(iOffsetY)
		pPicData.pDecMb[1], pPicData.iDecMbOff[1] = pDecPic.pData[1], pDecPic.iDataOff[1]+int(iOffsetUV)
		pPicData.pDecMb[2], pPicData.iDecMbOff[2] = pDecPic.pData[2], pDecPic.iDataOff[2]+int(iOffsetUV)
	} else {
		pPicData.iEncMbOff[0] += common.MB_WIDTH_LUMA
		pPicData.iEncMbOff[1] += common.MB_WIDTH_CHROMA
		pPicData.iEncMbOff[2] += common.MB_WIDTH_CHROMA

		pPicData.iDecMbOff[0] += common.MB_WIDTH_LUMA
		pPicData.iDecMbOff[1] += common.MB_WIDTH_CHROMA
		pPicData.iDecMbOff[2] += common.MB_WIDTH_CHROMA

		pPicData.iCsMbOff[0] += common.MB_WIDTH_LUMA
		pPicData.iCsMbOff[1] += common.MB_WIDTH_CHROMA
		pPicData.iCsMbOff[2] += common.MB_WIDTH_CHROMA
	}

	//step 2. initial pWelsMd
	pCurMb.uiCbp = 0

	//step 4: locating scaled_tcoeff

	//step 1. load neighbor cache
	FillNeighborCacheIntra(pMbCache, pCurMb, int32(pCurLayer.iMbWidth))
	pMbCache.pMemPredLuma = pMbCache.pMemPredMb         // in WelsMdI16x16() will be changed, so re-init here!
	pMbCache.pMemPredChroma = pMbCache.pMemPredMb[256:] // Init with default, maybe change in WelsMdI16x16 and svc_md_i16x16_sad
}

func WelsMdInterInit(pEncCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB, iSliceFirstMbXY int32) {
	pCurLayer := pEncCtx.pCurDqLayer
	pMbCache := &pSlice.sMbCacheInfo
	kiMbX := int32(pCurMb.iMbX)
	kiMbY := int32(pCurMb.iMbY)
	kiMbXY := pCurMb.iMbXY
	kiMbWidth := int32(pCurLayer.iMbWidth)
	kiMbHeight := int32(pCurLayer.iMbHeight)
	pPicData := &pMbCache.SPicData

	pMbCache.pEncSad = pCurLayer.pDecPic.pMbSkipSad
	pMbCache.iEncSadOff = int(kiMbXY)

	//step 1. load neighbor cache
	pEncCtx.pFuncList.pfFillInterNeighborCache(pMbCache, pCurMb, kiMbWidth,
		pEncCtx.pVaa.pVaaBackgroundMbFlag, int(kiMbXY)) //BGD spatial pFunc

	//step 3: initial cost

	//step 4. locating current p_ref
	// merge loops
	if 0 == kiMbX || iSliceFirstMbXY == kiMbXY {
		pRefPic := pCurLayer.pRefPic
		kiRefStrideY := pRefPic.iLineSize[0]
		kiRefStrideUV := pRefPic.iLineSize[1]
		kiCurStrideY := (kiMbX + kiMbY*kiRefStrideY) << 4
		kiCurStrideUV := (kiMbX + kiMbY*kiRefStrideUV) << 3
		pPicData.pRefMb[0], pPicData.iRefMbOff[0] = pRefPic.pData[0], pRefPic.iDataOff[0]+int(kiCurStrideY)
		pPicData.pRefMb[1], pPicData.iRefMbOff[1] = pRefPic.pData[1], pRefPic.iDataOff[1]+int(kiCurStrideUV)
		pPicData.pRefMb[2], pPicData.iRefMbOff[2] = pRefPic.pData[2], pRefPic.iDataOff[2]+int(kiCurStrideUV)
	} else {
		pPicData.iRefMbOff[0] += common.MB_WIDTH_LUMA
		pPicData.iRefMbOff[1] += common.MB_WIDTH_CHROMA
		pPicData.iRefMbOff[2] += common.MB_WIDTH_CHROMA
	}

	pMbCache.uiRefMbType = pCurLayer.pRefPic.uiRefMbType[kiMbXY]
	pMbCache.bCollocatedPredFlag = false

	//comment: sometimes, mode decision process may skip the md_p16x16 and md_pskip function,
	pCurMb.sP16x16Mv = SMVUnitXY{}
	pCurLayer.pDecPic.sMvList[kiMbXY] = SMVUnitXY{}

	SetMvWithinIntegerMvRange(kiMbWidth, kiMbHeight, kiMbX, kiMbY, pEncCtx.iMvRange, &pSlice.sMvStartMin,
		&pSlice.sMvStartMax)
}

func WelsMdI16x16(pFunc *SWelsFuncPtrList, pCurDqLayer *SDqLayer, pMbCache *SMbCache, iLambda int32) int32 {
	var kpAvailMode []int8
	var iAvailCount int32
	var iIdx int32 = 0
	pPredI16x16 := [2][]uint8{pMbCache.pMemPredMb, pMbCache.pMemPredMb[256:]}
	pDst := pPredI16x16[0]
	pDec := pMbCache.SPicData.pCsMb[0]
	iDecOff := pMbCache.SPicData.iCsMbOff[0]
	pEnc := pMbCache.SPicData.pEncMb[0]
	iEncOff := pMbCache.SPicData.iEncMbOff[0]
	iLineSizeDec := pCurDqLayer.iCsStride[0]
	iLineSizeEnc := pCurDqLayer.iEncStride[0]
	var iCurCost, iCurMode, iBestMode int32
	var iBestCost int32 = math.MaxInt32

	iOffset := int32(pMbCache.uiNeighborIntra & 0x07)
	iAvailCount = int32(g_kiIntra16AvaliMode[iOffset][4])
	kpAvailMode = g_kiIntra16AvaliMode[iOffset][:]
	if iAvailCount > 3 && pFunc.sSampleDealingFuncs.pfIntra16x16Combined3 != nil {
		pMbCache.iBestModeScratch = iBestMode
		iBestCost = pFunc.sSampleDealingFuncs.pfIntra16x16Combined3(pDec, iDecOff, iLineSizeDec, pEnc, iEncOff, iLineSizeEnc, &pMbCache.iBestModeScratch,
			iLambda, pDst /*temp*/, 0)
		iBestMode = pMbCache.iBestModeScratch
		iCurMode = int32(kpAvailMode[3])
		pFunc.pfGetLumaI16x16Pred[iCurMode](pDst, 0, pDec, iDecOff, iLineSizeDec)
		iCurCost = pFunc.sSampleDealingFuncs.pfMdCost[BLOCK_16x16](pDst, 0, 16, pEnc, iEncOff, iLineSizeEnc) + iLambda*4
		if iCurCost < iBestCost {
			iBestMode = iCurMode
			iBestCost = iCurCost
		} else {
			pFunc.pfGetLumaI16x16Pred[iBestMode](pDst, 0, pDec, iDecOff, iLineSizeDec)
		}
		iIdx = 1
		iBestCost += iLambda
	} else {
		iBestMode = int32(kpAvailMode[0])
		for i := int32(0); i < iAvailCount; i++ {
			iCurMode = int32(kpAvailMode[i])

			pFunc.pfGetLumaI16x16Pred[iCurMode](pDst, 0, pDec, iDecOff, iLineSizeDec)
			iCurCost = pFunc.sSampleDealingFuncs.pfMdCost[BLOCK_16x16](pDst, 0, 16, pEnc, iEncOff, iLineSizeEnc)
			iCurCost += iLambda * int32(BsSizeUE(uint32(g_kiMapModeI16x16[iCurMode])))
			if iCurCost < iBestCost {
				iBestMode = iCurMode
				iBestCost = iCurCost
				iIdx = iIdx ^ 0x01
				pDst = pPredI16x16[iIdx]
			}
		}
	}
	pMbCache.pMemPredChroma = pPredI16x16[iIdx]

	pMbCache.pMemPredLuma = pPredI16x16[iIdx^0x01]
	pMbCache.uiLumaI16x16Mode = uint8(iBestMode)
	return iBestCost
}

func WelsMdI4x4(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) int32 {
	pFunc := pEncCtx.pFuncList
	pCurDqLayer := pEncCtx.pCurDqLayer
	iLambda := pWelsMd.iLambda
	iBestCostLuma := pWelsMd.iCostLuma
	pEncMb := pMbCache.SPicData.pEncMb[0]
	iEncMbOff := pMbCache.SPicData.iEncMbOff[0]
	pDecMb := pMbCache.SPicData.pCsMb[0]
	iDecMbOff := pMbCache.SPicData.iCsMbOff[0]
	kiLineSizeEnc := pCurDqLayer.iEncStride[0]
	kiLineSizeDec := pCurDqLayer.iCsStride[0]

	var iPredMode, iCurMode, iBestMode, iFinalMode int32
	var iCurCost, iBestCost int32
	var iAvailCount int32
	lambda := [2]int32{iLambda << 2, iLambda}
	pPrevIntra4x4PredModeFlag := pMbCache.pPrevIntra4x4PredModeFlag
	iPrevIdx := 0
	pRemIntra4x4PredModeFlag := pMbCache.pRemIntra4x4PredModeFlag
	iRemIdx := 0
	kpIntra4x4AvailCount := &g_kiIntra4AvailCount
	kpCache48CountScan4 := &common.G_kuiCache48CountScan4Idx
	kpNeighborIntraToI4x4 := &g_kiNeighborIntraToI4x4[pMbCache.uiNeighborIntra]
	kpCoordinateIdxX := &g_kiCoordinateIdx4x4X
	kpCoordinateIdxY := &g_kiCoordinateIdx4x4Y
	var iBestPredBufferNum int32 = 0
	var iCosti4x4 int32 = 0

	lambdaIdx := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}

	for i := 0; i < 16; i++ {
		kiOffset := int32(kpNeighborIntraToI4x4[i])

		//step 1: locating current 4x4 block position in pEnc and pDecMb
		iCoordinateX := int32(kpCoordinateIdxX[i])
		iCoordinateY := int32(kpCoordinateIdxY[i])

		iIdxStrideEnc := (iCoordinateY * kiLineSizeEnc) + iCoordinateX
		iCurEncOff := iEncMbOff + int(iIdxStrideEnc)
		iIdxStrideDec := (iCoordinateY * kiLineSizeDec) + iCoordinateX
		iCurDecOff := iDecMbOff + int(iIdxStrideDec)

		//step 2: get predicted mode from neighbor
		iPredMode = PredIntra4x4Mode(pMbCache.iIntraPredMode[:], int32(kpCache48CountScan4[i]))

		//step 3: collect candidates of iPredMode
		iAvailCount = int32(kpIntra4x4AvailCount[kiOffset])
		kpAvailMode := &g_kiIntra4AvailMode[kiOffset]

		//step 4: gain the best pred mode
		iBestCost = math.MaxInt32
		iBestMode = int32(kpAvailMode[0])

		if pFunc.sSampleDealingFuncs.pfIntra4x4Combined3 != nil && (iAvailCount >= 6) {
			pMbCache.iBestModeScratch = iBestMode
			iDstOff := int(iBestPredBufferNum << 4)

			iBestCost = pFunc.sSampleDealingFuncs.pfIntra4x4Combined3(pDecMb, iCurDecOff, kiLineSizeDec, pEncMb, iCurEncOff, kiLineSizeEnc,
				pMbCache.pMemPredBlk4, iDstOff,
				&pMbCache.iBestModeScratch,
				lambda[lambdaIdx(iPredMode == 2)], lambda[lambdaIdx(iPredMode == 1)], lambda[lambdaIdx(iPredMode == 0)])
			iBestMode = pMbCache.iBestModeScratch
			//     ST64(&pMbCache->pMemPredBlk4[iBestMode<<4], LD64(mem_pred_blk4_temp));
			//     ST64(&pMbCache->pMemPredBlk4[8+(iBestMode<<4)], LD64(mem_pred_blk4_temp+8));

			for j := int32(3); j < iAvailCount; j++ {
				iCurMode = int32(kpAvailMode[j])

				iDstOff = int((1 - iBestPredBufferNum) << 4)

				pFunc.pfGetLumaI4x4Pred[iCurMode](pMbCache.pMemPredBlk4, iDstOff, pDecMb, iCurDecOff, kiLineSizeDec)
				iCurCost = pFunc.sSampleDealingFuncs.pfSampleSatd[BLOCK_4x4](pMbCache.pMemPredBlk4, iDstOff, 4, pEncMb, iCurEncOff, kiLineSizeEnc) +
					lambda[lambdaIdx(iPredMode == int32(g_kiMapModeI4x4[iCurMode]))]

				if iCurCost < iBestCost {
					iBestMode = iCurMode
					iBestCost = iCurCost
					iBestPredBufferNum = 1 - iBestPredBufferNum
				}
			}
		} else {
			for j := int32(0); j < iAvailCount; j++ {
				iCurMode = int32(kpAvailMode[j])

				iDstOff := int((1 - iBestPredBufferNum) << 4)

				pFunc.pfGetLumaI4x4Pred[iCurMode](pMbCache.pMemPredBlk4, iDstOff, pDecMb, iCurDecOff, kiLineSizeDec)
				iCurCost = pFunc.sSampleDealingFuncs.pfSampleSatd[BLOCK_4x4](pMbCache.pMemPredBlk4, iDstOff, 4, pEncMb, iCurEncOff, kiLineSizeEnc) +
					lambda[lambdaIdx(iPredMode == int32(g_kiMapModeI4x4[iCurMode]))]

				if iCurCost < iBestCost {
					iBestMode = iCurMode
					iBestCost = iCurCost
					iBestPredBufferNum = 1 - iBestPredBufferNum
				}
			}
		}
		pMbCache.pBestPredI4x4Blk4 = pMbCache.pMemPredBlk4[iBestPredBufferNum<<4:]
		iCosti4x4 += iBestCost
		if iCosti4x4 >= iBestCostLuma {
			break
		}

		//step 5: update pred mode and sample avail cache
		iFinalMode = int32(g_kiMapModeI4x4[iBestMode])
		if iPredMode == iFinalMode {
			pPrevIntra4x4PredModeFlag[iPrevIdx] = true
			iPrevIdx++
		} else {
			pPrevIntra4x4PredModeFlag[iPrevIdx] = false
			iPrevIdx++
			if iFinalMode < iPredMode {
				pRemIntra4x4PredModeFlag[iRemIdx] = int8(iFinalMode)
			} else {
				pRemIntra4x4PredModeFlag[iRemIdx] = int8(iFinalMode - 1)
			}
		}
		iRemIdx++
		// pCurMb->pIntra4x4PredMode[g_kuiMbCountScan4Idx[i]] = iFinalMode;
		pMbCache.iIntraPredMode[kpCache48CountScan4[i]] = int8(iFinalMode)

		//step 6: encoding I_4x4
		WelsEncRecI4x4Y(pEncCtx, pCurMb, pMbCache, uint8(i))
	}
	copy(pCurMb.pIntra4x4PredMode[0:4], pMbCache.iIntraPredMode[33:37])
	pCurMb.pIntra4x4PredMode[4] = pMbCache.iIntraPredMode[12]
	pCurMb.pIntra4x4PredMode[5] = pMbCache.iIntraPredMode[20]
	pCurMb.pIntra4x4PredMode[6] = pMbCache.iIntraPredMode[28]
	iCosti4x4 += (iLambda << 4) + (iLambda << 3) //4*6*lambda from JVT SATD0
	return iCosti4x4
}

func WelsMdI4x4Fast(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) int32 {
	pFunc := pEncCtx.pFuncList
	pCurDqLayer := pEncCtx.pCurDqLayer
	iLambda := pWelsMd.iLambda
	iBestCostLuma := pWelsMd.iCostLuma
	pEncMb := pMbCache.SPicData.pEncMb[0]
	iEncMbOff := pMbCache.SPicData.iEncMbOff[0]
	pDecMb := pMbCache.SPicData.pCsMb[0]
	iDecMbOff := pMbCache.SPicData.iCsMbOff[0]
	kiLineSizeEnc := pCurDqLayer.iEncStride[0]
	kiLineSizeDec := pCurDqLayer.iCsStride[0]

	var iPredMode, iCurMode, iBestMode, iFinalMode int8
	var iCurCost, iBestCost int32
	var iAvailCount int32
	var iCostH, iCostV, iCostVR, iCostHD, iCostVL, iCostHU int32
	var iBestModeFake bool
	lambda := [2]int32{iLambda << 2, iLambda}
	pPrevIntra4x4PredModeFlag := pMbCache.pPrevIntra4x4PredModeFlag
	iPrevIdx := 0
	pRemIntra4x4PredModeFlag := pMbCache.pRemIntra4x4PredModeFlag
	iRemIdx := 0
	kpIntra4x4AvailCount := &g_kiIntra4AvailCount
	kpCache48CountScan4 := &common.G_kuiCache48CountScan4Idx
	kpNeighborIntraToI4x4 := &g_kiNeighborIntraToI4x4[pMbCache.uiNeighborIntra]
	kpCoordinateIdxX := &g_kiCoordinateIdx4x4X
	kpCoordinateIdxY := &g_kiCoordinateIdx4x4Y
	var iBestPredBufferNum int32 = 0
	var iCosti4x4 int32 = 0
	pMdCost4x4 := pFunc.sSampleDealingFuncs.pfMdCost[BLOCK_4x4]

	for i := 0; i < 16; i++ {
		kiOffset := int32(kpNeighborIntraToI4x4[i])

		//step 1: locating current 4x4 block position in pEnc and pDecMb
		iCoordinateX := int32(kpCoordinateIdxX[i])
		iCoordinateY := int32(kpCoordinateIdxY[i])

		iIdxStrideEnc := (iCoordinateY * kiLineSizeEnc) + iCoordinateX
		iCurEncOff := iEncMbOff + int(iIdxStrideEnc)
		iIdxStrideDec := (iCoordinateY * kiLineSizeDec) + iCoordinateX
		iCurDecOff := iDecMbOff + int(iIdxStrideDec)

		//step 2: get predicted mode from neighbor
		iPredMode = int8(PredIntra4x4Mode(pMbCache.iIntraPredMode[:], int32(kpCache48CountScan4[i])))
		//step 3: collect candidates of iPredMode
		iAvailCount = int32(kpIntra4x4AvailCount[kiOffset])
		kpAvailMode := &g_kiIntra4AvailMode[kiOffset]

		// cost of mode iMode predicted into the buffer selected by iBufNum
		modeCost := func(iMode int8, iBufNum int32) int32 {
			iDstOff := int(iBufNum << 4)
			pFunc.pfGetLumaI4x4Pred[iMode](pMbCache.pMemPredBlk4, iDstOff, pDecMb, iCurDecOff, kiLineSizeDec)
			l := lambda[0]
			if iPredMode == g_kiMapModeI4x4[iMode] {
				l = lambda[1]
			}
			return pMdCost4x4(pMbCache.pMemPredBlk4, iDstOff, 4, pEncMb, iCurEncOff, kiLineSizeEnc) + l
		}
		// try iCurMode in the spare buffer and keep it when it is the best
		tryMode := func() {
			iCurCost = modeCost(iCurMode, 1-iBestPredBufferNum)
			if iCurCost < iBestCost {
				iBestMode = iCurMode
				iBestCost = iCurCost
				iBestPredBufferNum = 1 - iBestPredBufferNum
			}
		}

		if iAvailCount == 9 || iAvailCount == 7 {
			//I4_PRED_DC(2)

			iBestMode = common.I4_PRED_DC

			iBestCost = modeCost(iBestMode, iBestPredBufferNum)

			//I4_PRED_H(1)
			iCurMode = common.I4_PRED_H
			tryMode()
			iCostH = iCurCost

			//I4_PRED_V(0)
			iCurMode = common.I4_PRED_V
			tryMode()
			iCostV = iCurCost

			if iCostV < iCostH {
				if iAvailCount == 9 {
					iBestModeFake = true //indicating whether V is the best fake mode

					//I4_PRED_VR(5) and I4_PRED_VL(7)
					iCurMode = common.I4_PRED_VR
					tryMode()
					iCostVR = iCurCost

					if iCurCost < iCostV {
						iBestModeFake = false
					}

					iCurMode = common.I4_PRED_VL
					tryMode()
					iCostVL = iCurCost

					if iCurCost < iCostV {
						iBestModeFake = false
					}

					//Vertical Early Determination
					if !iBestModeFake { //Vertical is not the best, go on checking...
						//select the best one from VL and VR
						if iCostVR < iCostVL {
							//I4_PRED_DDR(4)
							iCurMode = common.I4_PRED_DDR
							tryMode()
						} else {
							//I4_PRED_DDL(3)
							iCurMode = common.I4_PRED_DDL
							tryMode()
						}
					}
				} else if iAvailCount == 7 {
					iCurMode = common.I4_PRED_DDR
					tryMode()

					iCurMode = common.I4_PRED_VR
					tryMode()
				}
			} else {
				iBestModeFake = true //indicating whether H is the best fake mode
				//I4_PRED_HD(6) and I4_PRED_HU(8)
				iCurMode = common.I4_PRED_HD
				tryMode()
				iCostHD = iCurCost

				if iCurCost < iCostH {
					iBestModeFake = false
				}

				iCurMode = common.I4_PRED_HU
				tryMode()
				iCostHU = iCurCost

				if iCurCost < iCostH {
					iBestModeFake = false
				}

				if !iBestModeFake { //Horizontal is not the best, go on checking...
					//select the best one from VL and VR
					if iCostHD < iCostHU {
						//I4_PRED_DDR(4)
						iCurMode = common.I4_PRED_DDR
						tryMode()
					} else if iAvailCount == 9 {
						//I4_PRED_DDL(3)
						iCurMode = common.I4_PRED_DDL
						tryMode()
					}
				}
			}
		} else {
			iBestCost = math.MaxInt32
			iBestMode = common.I4_PRED_INVALID
			for j := int32(0); j < iAvailCount; j++ {
				// I4x4_MODE_CHECK(pAvailMode[j], iCurCost);
				iCurMode = int8(kpAvailMode[j])
				tryMode()
			}
		}
		pMbCache.pBestPredI4x4Blk4 = pMbCache.pMemPredBlk4[iBestPredBufferNum<<4:]
		iCosti4x4 += iBestCost
		if iCosti4x4 >= iBestCostLuma {
			break
		}

		//step 5: update pred mode and sample avail cache
		iFinalMode = g_kiMapModeI4x4[iBestMode]
		if iPredMode == iFinalMode {
			pPrevIntra4x4PredModeFlag[iPrevIdx] = true
			iPrevIdx++
		} else {
			pPrevIntra4x4PredModeFlag[iPrevIdx] = false
			iPrevIdx++
			if iFinalMode < iPredMode {
				pRemIntra4x4PredModeFlag[iRemIdx] = iFinalMode
			} else {
				pRemIntra4x4PredModeFlag[iRemIdx] = iFinalMode - 1
			}
		}
		iRemIdx++
		// pCurMb->pIntra4x4PredMode[scan4[i]] = iFinalMode;
		pMbCache.iIntraPredMode[kpCache48CountScan4[i]] = iFinalMode
		//step 6: encoding I_4x4
		WelsEncRecI4x4Y(pEncCtx, pCurMb, pMbCache, uint8(i))
	}
	copy(pCurMb.pIntra4x4PredMode[0:4], pMbCache.iIntraPredMode[33:37])
	pCurMb.pIntra4x4PredMode[4] = pMbCache.iIntraPredMode[12]
	pCurMb.pIntra4x4PredMode[5] = pMbCache.iIntraPredMode[20]
	pCurMb.pIntra4x4PredMode[6] = pMbCache.iIntraPredMode[28]
	iCosti4x4 += (iLambda << 4) + (iLambda << 3) //4*6*lambda from JVT SATD0
	return iCosti4x4
}

func WelsMdIntraChroma(pFunc *SWelsFuncPtrList, pCurDqLayer *SDqLayer, pMbCache *SMbCache, iLambda int32) int32 {
	var kpAvailMode []int8
	var iAvailCount int32 = 0
	var iChmaIdx int32 = 0
	pPredIntraChma := [2][]uint8{pMbCache.pMemPredChroma, pMbCache.pMemPredChroma[128:]}
	pDstChma := pPredIntraChma[0]
	pEncCb := pMbCache.SPicData.pEncMb[1]
	iEncCbOff := pMbCache.SPicData.iEncMbOff[1]
	pEncCr := pMbCache.SPicData.pEncMb[2]
	iEncCrOff := pMbCache.SPicData.iEncMbOff[2]
	pDecCb := pMbCache.SPicData.pCsMb[1] //pMbCache->SPicData.pDecMb[1];
	iDecCbOff := pMbCache.SPicData.iCsMbOff[1]
	pDecCr := pMbCache.SPicData.pCsMb[2] //pMbCache->SPicData.pDecMb[2];
	iDecCrOff := pMbCache.SPicData.iCsMbOff[2]
	kiLineSizeEnc := pCurDqLayer.iEncStride[1]
	kiLineSizeDec := pCurDqLayer.iCsStride[1] //pMbCache->SPicData.i_stride_dec[1];

	var iCurMode, iCurCost, iBestMode int32
	var iBestCost int32 = math.MaxInt32

	iOffset := int32(pMbCache.uiNeighborIntra & 0x07)
	iAvailCount = int32(g_kiIntraChromaAvailMode[iOffset][4])
	kpAvailMode = g_kiIntraChromaAvailMode[iOffset][:]
	if iAvailCount > 3 && pFunc.sSampleDealingFuncs.pfIntra8x8Combined3 != nil {
		pMbCache.iBestModeScratch = iBestMode
		iBestCost = pFunc.sSampleDealingFuncs.pfIntra8x8Combined3(pDecCb, iDecCbOff, kiLineSizeDec, pEncCb, iEncCbOff, kiLineSizeEnc, &pMbCache.iBestModeScratch,
			iLambda, pDstChma, 0, pDecCr, iDecCrOff, pEncCr, iEncCrOff)
		iBestMode = pMbCache.iBestModeScratch
		iCurMode = int32(kpAvailMode[3])
		pFunc.pfGetChromaPred[iCurMode](pDstChma, 0, pDecCb, iDecCbOff, kiLineSizeDec)  //Cb
		pFunc.pfGetChromaPred[iCurMode](pDstChma, 64, pDecCr, iDecCrOff, kiLineSizeDec) //Cr

		iCurCost = pFunc.sSampleDealingFuncs.pfMdCost[BLOCK_8x8](pDstChma, 0, 8, pEncCb, iEncCbOff, kiLineSizeEnc) +
			pFunc.sSampleDealingFuncs.pfMdCost[BLOCK_8x8](pDstChma, 64, 8, pEncCr, iEncCrOff, kiLineSizeEnc) +
			iLambda*4
		if iCurCost < iBestCost {
			iBestMode = iCurMode
			iBestCost = iCurCost
		} else {
			pFunc.pfGetChromaPred[iBestMode](pDstChma, 0, pDecCb, iDecCbOff, kiLineSizeDec)  //Cb
			pFunc.pfGetChromaPred[iBestMode](pDstChma, 64, pDecCr, iDecCrOff, kiLineSizeDec) //Cr
		}
		iBestCost += iLambda
		iChmaIdx = 1
	} else {
		iBestMode = int32(kpAvailMode[0])
		for i := int32(0); i < iAvailCount; i++ {
			iCurMode = int32(kpAvailMode[i])

			// pDstCb = &pMbCache->mem_pred_intra_cb[iCurMode<<6];
			// pDstCr = &pMbCache->mem_pred_intra_cr[iCurMode<<6];
			pFunc.pfGetChromaPred[iCurMode](pDstChma, 0, pDecCb, iDecCbOff, kiLineSizeDec) //Cb
			iCurCost = pFunc.sSampleDealingFuncs.pfMdCost[BLOCK_8x8](pDstChma, 0, 8, pEncCb, iEncCbOff, kiLineSizeEnc)

			pFunc.pfGetChromaPred[iCurMode](pDstChma, 64, pDecCr, iDecCrOff, kiLineSizeDec) //Cr
			iCurCost += pFunc.sSampleDealingFuncs.pfMdCost[BLOCK_8x8](pDstChma, 64, 8, pEncCr, iEncCrOff, kiLineSizeEnc) +
				iLambda*int32(BsSizeUE(uint32(g_kiMapModeIntraChroma[iCurMode])))
			if iCurCost < iBestCost {
				iBestMode = iCurMode
				iBestCost = iCurCost
				iChmaIdx = iChmaIdx ^ 0x01
				pDstChma = pPredIntraChma[iChmaIdx]
			}
		}
	}

	pMbCache.pBestPredIntraChroma = pPredIntraChma[iChmaIdx^0x01]
	pMbCache.uiChmaI8x8Mode = uint8(iBestMode)
	return iBestCost
}

func WelsMdIntraFinePartition(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) int32 {
	iCosti4x4 := WelsMdI4x4(pEncCtx, pWelsMd, pCurMb, pMbCache)

	if iCosti4x4 < pWelsMd.iCostLuma {
		pCurMb.uiMbType = common.MB_TYPE_INTRA4x4
		pWelsMd.iCostLuma = iCosti4x4
	}
	return pWelsMd.iCostLuma
}

func WelsMdIntraFinePartitionVaa(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) int32 {

	if MdIntraAnalysisVaaInfo(pEncCtx, pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0]) {
		iCosti4x4 := WelsMdI4x4Fast(pEncCtx, pWelsMd, pCurMb, pMbCache)

		if iCosti4x4 < pWelsMd.iCostLuma {
			pCurMb.uiMbType = common.MB_TYPE_INTRA4x4
			pWelsMd.iCostLuma = iCosti4x4
		}
	}

	return pWelsMd.iCostLuma
}

func WelsMdIntraMb(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) {
	//initial prediction memory for I_16x16
	pWelsMd.iCostLuma = WelsMdI16x16(pEncCtx.pFuncList, pEncCtx.pCurDqLayer, pMbCache, pWelsMd.iLambda)
	pCurMb.uiMbType = common.MB_TYPE_INTRA16x16

	WelsMdIntraSecondaryModesEnc(pEncCtx, pWelsMd, pCurMb, pMbCache)
}

// InitMeMd is the file-local static InitMe of svc_base_layer_md.cpp (renamed: the
// ME unit test defines its own InitMe).
func InitMeMd(sWelsMd *SWelsMD, iBlockSize int32, pEnc []uint8, iEncOff int, pRef []uint8, iRefOff int,
	pRefFeatureStorage *SScreenBlockFeatureStorage,
	sWelsMe *SWelsME) {
	sWelsMe.iCurMeBlockPixX = sWelsMd.iMbPixX
	sWelsMe.iCurMeBlockPixY = sWelsMd.iMbPixY
	sWelsMe.uiBlockSize = uint8(iBlockSize)
	sWelsMe.pMvdCost = sWelsMd.pMvdCost
	sWelsMe.iMvdCostOff = sWelsMd.iMvdCostOff

	sWelsMe.pEncMb, sWelsMe.iEncMbOff = pEnc, iEncOff
	sWelsMe.pRefMb, sWelsMe.iRefMbOff = pRef, iRefOff
	sWelsMe.pColoRefMb, sWelsMe.iColoRefMbOff = pRef, iRefOff

	sWelsMe.pRefFeatureStorage = pRefFeatureStorage
}

func WelsMdP16x16(pFunc *SWelsFuncPtrList, pCurLayer *SDqLayer, pWelsMd *SWelsMD, pSlice *SSlice, pCurMb *SMB) int32 {
	pMbCache := &pSlice.sMbCacheInfo
	pMe16x16 := &pWelsMd.sMe.sMe16x16
	uiNeighborAvail := uint32(pCurMb.uiNeighborAvail)
	kiMbWidth := int32(pCurLayer.iMbWidth) // for assign once
	kiMbHeight := int32(pCurLayer.iMbHeight)
	InitMeMd(pWelsMd, BLOCK_16x16, pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
		pMbCache.SPicData.pRefMb[0], pMbCache.SPicData.iRefMbOff[0],
		pCurLayer.pRefPic.pScreenBlockFeatureStorage,
		pMe16x16)
	//not putting the line below into InitMe to avoid judging mode in InitMe
	pMe16x16.uSadPredISatd = uint32(pWelsMd.iSadPredMb)

	pSlice.uiMvcNum = 0
	pSlice.sMvc[pSlice.uiMvcNum] = pMe16x16.sMvBase
	pSlice.uiMvcNum++
	//spatial motion vector predictors
	if uiNeighborAvail&LEFT_MB_POS != 0 { //left available
		pSlice.sMvc[pSlice.uiMvcNum] = pCurMb.Add(-1).sP16x16Mv
		pSlice.uiMvcNum++
	}
	if uiNeighborAvail&TOP_MB_POS != 0 { //top available
		pSlice.sMvc[pSlice.uiMvcNum] = pCurMb.Add(-kiMbWidth).sP16x16Mv
		pSlice.uiMvcNum++
	}
	//temporal motion vector predictors
	if pCurLayer.pRefPic.iPictureType == common.P_SLICE {
		if int32(pCurMb.iMbX) < kiMbWidth-1 {
			sTempMv := pCurLayer.pRefPic.sMvList[pCurMb.iMbXY+1]
			pSlice.sMvc[pSlice.uiMvcNum].iMvX = sTempMv.iMvX >> pSlice.sScaleShift
			pSlice.sMvc[pSlice.uiMvcNum].iMvY = sTempMv.iMvY >> pSlice.sScaleShift
			pSlice.uiMvcNum++
		}
		if int32(pCurMb.iMbY) < kiMbHeight-1 {
			sTempMv := pCurLayer.pRefPic.sMvList[pCurMb.iMbXY+kiMbWidth]
			pSlice.sMvc[pSlice.uiMvcNum].iMvX = sTempMv.iMvX >> pSlice.sScaleShift
			pSlice.sMvc[pSlice.uiMvcNum].iMvY = sTempMv.iMvY >> pSlice.sScaleShift
			pSlice.uiMvcNum++
		}
	}

	PredMv(&pMbCache.sMvComponents, 0, 4, 0, &pMe16x16.sMvp)
	pFunc.pfMotionSearch[0](pFunc, pCurLayer, pMe16x16, pSlice)

	pCurMb.sP16x16Mv = pMe16x16.sMv
	pCurLayer.pDecPic.sMvList[pCurMb.iMbXY] = pMe16x16.sMv

	return int32(pMe16x16.uiSatdCost)
}

func WelsMdP16x8(pFunc *SWelsFuncPtrList, pCurDqLayer *SDqLayer, pWelsMd *SWelsMD, pSlice *SSlice) int32 {
	pMbCache := &pSlice.sMbCacheInfo
	iStrideEnc := pCurDqLayer.iEncStride[0]
	iStrideRef := pCurDqLayer.pRefPic.iLineSize[0]
	var i int32 = 0
	var iCostP16x8 int32 = 0
	for {
		sMe16x8 := &pWelsMd.sMe.sMe16x8[i]
		iPixelY := i << 3
		InitMeMd(pWelsMd, BLOCK_16x8,
			pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0]+int(iPixelY*iStrideEnc),
			pMbCache.SPicData.pRefMb[0], pMbCache.SPicData.iRefMbOff[0]+int(iPixelY*iStrideRef),
			pCurDqLayer.pRefPic.pScreenBlockFeatureStorage,
			sMe16x8)
		//not putting the lines below into InitMe to avoid judging mode in InitMe
		sMe16x8.iCurMeBlockPixY = pWelsMd.iMbPixY + iPixelY
		sMe16x8.uSadPredISatd = uint32(pWelsMd.iSadPredMb >> 1)

		pSlice.sMvc[0] = sMe16x8.sMvBase
		pSlice.uiMvcNum = 1

		PredInter16x8Mv(pMbCache, i<<3, 0, &sMe16x8.sMvp)
		pFunc.pfMotionSearch[0](pFunc, pCurDqLayer, sMe16x8, pSlice)
		UpdateP16x8Motion2Cache(pMbCache, i<<3, int8(pWelsMd.uiRef), &sMe16x8.sMv)
		iCostP16x8 += int32(sMe16x8.uiSatdCost)
		i++
		if i >= 2 {
			break
		}
	}
	return iCostP16x8
}

func WelsMdP8x16(pFunc *SWelsFuncPtrList, pCurLayer *SDqLayer, pWelsMd *SWelsMD, pSlice *SSlice) int32 {
	pMbCache := &pSlice.sMbCacheInfo
	var i int32 = 0
	var iCostP8x16 int32 = 0
	for {
		iPixelX := i << 3
		sMe8x16 := &pWelsMd.sMe.sMe8x16[i]
		InitMeMd(pWelsMd, BLOCK_8x16,
			pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0]+int(iPixelX),
			pMbCache.SPicData.pRefMb[0], pMbCache.SPicData.iRefMbOff[0]+int(iPixelX),
			pCurLayer.pRefPic.pScreenBlockFeatureStorage,
			sMe8x16)
		//not putting the lines below into InitMe to avoid judging mode in InitMe
		sMe8x16.iCurMeBlockPixX = pWelsMd.iMbPixX + iPixelX
		sMe8x16.uSadPredISatd = uint32(pWelsMd.iSadPredMb >> 1)

		pSlice.sMvc[0] = sMe8x16.sMvBase
		pSlice.uiMvcNum = 1

		PredInter8x16Mv(pMbCache, i<<2, 0, &sMe8x16.sMvp)
		pFunc.pfMotionSearch[0](pFunc, pCurLayer, sMe8x16, pSlice)
		UpdateP8x16Motion2Cache(pMbCache, i<<2, int8(pWelsMd.uiRef), &sMe8x16.sMv)
		iCostP8x16 += int32(sMe8x16.uiSatdCost)
		i++
		if i >= 2 {
			break
		}
	}
	return iCostP8x16
}

func WelsMdP8x8(pFunc *SWelsFuncPtrList, pCurDqLayer *SDqLayer, pWelsMd *SWelsMD, pSlice *SSlice) int32 {
	pMbCache := &pSlice.sMbCacheInfo
	iLineSizeEnc := pCurDqLayer.iEncStride[0]
	iLineSizeRef := pCurDqLayer.pRefPic.iLineSize[0]
	var iCostP8x8 int32 = 0
	for i := int32(0); i < 4; i++ {
		iIdxX := i & 1
		iIdxY := i >> 1
		iPixelX := iIdxX << 3
		iPixelY := iIdxY << 3
		iStrideEnc := iPixelX + (iPixelY * iLineSizeEnc)
		iStrideRef := iPixelX + (iPixelY * iLineSizeRef)

		sMe8x8 := &pWelsMd.sMe.sMe8x8[i]
		InitMeMd(pWelsMd, BLOCK_8x8,
			pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0]+int(iStrideEnc),
			pMbCache.SPicData.pRefMb[0], pMbCache.SPicData.iRefMbOff[0]+int(iStrideRef),
			pCurDqLayer.pRefPic.pScreenBlockFeatureStorage,
			sMe8x8)
		//not putting these three lines below into InitMe to avoid judging mode in InitMe
		sMe8x8.iCurMeBlockPixX = pWelsMd.iMbPixX + iPixelX
		sMe8x8.iCurMeBlockPixY = pWelsMd.iMbPixY + iPixelY
		sMe8x8.uSadPredISatd = uint32(pWelsMd.iSadPredMb >> 2)

		pSlice.sMvc[0] = sMe8x8.sMvBase
		pSlice.uiMvcNum = 1

		PredMv(&pMbCache.sMvComponents, int8(i<<2), 2, int32(pWelsMd.uiRef), &sMe8x8.sMvp)
		pFunc.pfMotionSearch[pWelsMd.iBlock8x8StaticIdc[i]](pFunc, pCurDqLayer, sMe8x8, pSlice)
		UpdateP8x8Motion2Cache(pMbCache, i<<2, int8(pWelsMd.uiRef), &sMe8x8.sMv)
		iCostP8x8 += int32(sMe8x8.uiSatdCost)
		//    sMe8x8++;
	}
	return iCostP8x8
}

func WelsMdP4x4(pFunc *SWelsFuncPtrList, pCurDqLayer *SDqLayer, pWelsMd *SWelsMD, pSlice *SSlice, ki8x8Idx int32) int32 {
	pMbCache := &pSlice.sMbCacheInfo
	iLineSizeEnc := pCurDqLayer.iEncStride[0]
	iLineSizeRef := pCurDqLayer.pRefPic.iLineSize[0]
	var iCostP4x4 int32 = 0
	for i4x4Idx := int32(0); i4x4Idx < 4; i4x4Idx++ {
		iPartIdx := (ki8x8Idx << 2) + i4x4Idx
		iIdxX := ((ki8x8Idx & 1) << 1) + (i4x4Idx & 1)
		iIdxY := ((ki8x8Idx >> 1) << 1) + (i4x4Idx >> 1)
		iPixelX := iIdxX << 2
		iPixelY := iIdxY << 2
		iStrideEnc := iPixelX + (iPixelY * iLineSizeEnc)
		iStrideRef := iPixelX + (iPixelY * iLineSizeRef)

		sMe4x4 := &pWelsMd.sMe.sMe4x4[ki8x8Idx][i4x4Idx]
		InitMeMd(pWelsMd, BLOCK_4x4,
			pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0]+int(iStrideEnc),
			pMbCache.SPicData.pRefMb[0], pMbCache.SPicData.iRefMbOff[0]+int(iStrideRef),
			pCurDqLayer.pRefPic.pScreenBlockFeatureStorage,
			sMe4x4)
		//not putting these three lines below into InitMe to avoid judging mode in InitMe
		sMe4x4.iCurMeBlockPixX = pWelsMd.iMbPixX + iPixelX
		sMe4x4.iCurMeBlockPixY = pWelsMd.iMbPixY + iPixelY
		sMe4x4.uSadPredISatd = uint32(pWelsMd.iSadPredMb >> 2)

		pSlice.sMvc[0] = sMe4x4.sMvBase
		pSlice.uiMvcNum = 1

		PredMv(&pMbCache.sMvComponents, int8(iPartIdx), 1, int32(pWelsMd.uiRef), &sMe4x4.sMvp)
		pFunc.pfMotionSearch[0](pFunc, pCurDqLayer, sMe4x4, pSlice)
		UpdateP4x4Motion2Cache(pMbCache, iPartIdx, int8(pWelsMd.uiRef), &sMe4x4.sMv)
		iCostP4x4 += int32(sMe4x4.uiSatdCost)
	}
	return iCostP4x4
}

func WelsMdP8x4(pFunc *SWelsFuncPtrList, pCurDqLayer *SDqLayer, pWelsMd *SWelsMD, pSlice *SSlice, ki8x8Idx int32) int32 {
	pMbCache := &pSlice.sMbCacheInfo
	iLineSizeEnc := pCurDqLayer.iEncStride[0]
	iLineSizeRef := pCurDqLayer.pRefPic.iLineSize[0]
	var iCostP8x4 int32 = 0
	for i8x4Idx := int32(0); i8x4Idx < 2; i8x4Idx++ {
		iPartIdx := (ki8x8Idx << 2) + (i8x4Idx << 1)
		iIdxX := (ki8x8Idx & 1) << 1
		iIdxY := ((ki8x8Idx >> 1) << 1) + i8x4Idx
		iPixelX := iIdxX << 2
		iPixelY := iIdxY << 2
		iStrideEnc := iPixelX + (iPixelY * iLineSizeEnc)
		iStrideRef := iPixelX + (iPixelY * iLineSizeRef)

		sMe8x4 := &pWelsMd.sMe.sMe8x4[ki8x8Idx][i8x4Idx]
		InitMeMd(pWelsMd, BLOCK_8x4,
			pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0]+int(iStrideEnc),
			pMbCache.SPicData.pRefMb[0], pMbCache.SPicData.iRefMbOff[0]+int(iStrideRef),
			pCurDqLayer.pRefPic.pScreenBlockFeatureStorage,
			sMe8x4)
		//not putting these three lines below into InitMe to avoid judging mode in InitMe
		sMe8x4.iCurMeBlockPixX = pWelsMd.iMbPixX + iPixelX
		sMe8x4.iCurMeBlockPixY = pWelsMd.iMbPixY + iPixelY
		sMe8x4.uSadPredISatd = uint32(pWelsMd.iSadPredMb >> 2)

		pSlice.sMvc[0] = sMe8x4.sMvBase
		pSlice.uiMvcNum = 1

		PredMv(&pMbCache.sMvComponents, int8(iPartIdx), 2, int32(pWelsMd.uiRef), &sMe8x4.sMvp)
		pFunc.pfMotionSearch[0](pFunc, pCurDqLayer, sMe8x4, pSlice)
		UpdateP8x4Motion2Cache(pMbCache, iPartIdx, int8(pWelsMd.uiRef), &sMe8x4.sMv)
		iCostP8x4 += int32(sMe8x4.uiSatdCost)
	}
	return iCostP8x4
}

func WelsMdP4x8(pFunc *SWelsFuncPtrList, pCurDqLayer *SDqLayer, pWelsMd *SWelsMD, pSlice *SSlice, ki8x8Idx int32) int32 {
	//Wayne, to be modified
	pMbCache := &pSlice.sMbCacheInfo
	iLineSizeEnc := pCurDqLayer.iEncStride[0]
	iLineSizeRef := pCurDqLayer.pRefPic.iLineSize[0]
	var iCostP4x8 int32 = 0
	for i4x8Idx := int32(0); i4x8Idx < 2; i4x8Idx++ {
		iPartIdx := (ki8x8Idx << 2) + i4x8Idx
		iIdxX := ((ki8x8Idx & 1) << 1) + i4x8Idx
		iIdxY := (ki8x8Idx >> 1) << 1
		iPixelX := iIdxX << 2
		iPixelY := iIdxY << 2
		iStrideEnc := iPixelX + (iPixelY * iLineSizeEnc)
		iStrideRef := iPixelX + (iPixelY * iLineSizeRef)

		sMe4x8 := &pWelsMd.sMe.sMe4x8[ki8x8Idx][i4x8Idx]
		InitMeMd(pWelsMd, BLOCK_4x8,
			pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0]+int(iStrideEnc),
			pMbCache.SPicData.pRefMb[0], pMbCache.SPicData.iRefMbOff[0]+int(iStrideRef),
			pCurDqLayer.pRefPic.pScreenBlockFeatureStorage,
			sMe4x8)
		//not putting these three lines below into InitMe to avoid judging mode in InitMe
		sMe4x8.iCurMeBlockPixX = pWelsMd.iMbPixX + iPixelX
		sMe4x8.iCurMeBlockPixY = pWelsMd.iMbPixY + iPixelY
		sMe4x8.uSadPredISatd = uint32(pWelsMd.iSadPredMb >> 2)

		pSlice.sMvc[0] = sMe4x8.sMvBase
		pSlice.uiMvcNum = 1

		PredMv(&pMbCache.sMvComponents, int8(iPartIdx), 1, int32(pWelsMd.uiRef), &sMe4x8.sMvp)
		pFunc.pfMotionSearch[0](pFunc, pCurDqLayer, sMe4x8, pSlice)
		UpdateP4x8Motion2Cache(pMbCache, iPartIdx, int8(pWelsMd.uiRef), &sMe4x8.sMv)
		iCostP4x8 += int32(sMe4x8.uiSatdCost)
	}
	return iCostP4x8
}

// setSubMbType8x8 is memset (pCurMb->uiSubMbType, SUB_MB_TYPE_8x8, 4).
func setSubMbType8x8(pCurMb *SMB) {
	for k := range pCurMb.uiSubMbType {
		pCurMb.uiSubMbType[k] = common.SUB_MB_TYPE_8x8
	}
}

func WelsMdInterFinePartition(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pSlice *SSlice, pCurMb *SMB, iBestCost int32) {
	pCurDqLayer := pEncCtx.pCurDqLayer
	//  SMbCache *pMbCache = &pSlice->sMbCacheInfo;
	var iCost int32 = 0

	iCost = WelsMdP8x8(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice)

	if iCost < iBestCost {
		var iCostPart int32
		pCurMb.uiMbType = common.MB_TYPE_8x8
		setSubMbType8x8(pCurMb)

		iCostPart = WelsMdP16x8(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice)
		if iCostPart <= iCost {
			iCost = iCostPart
			pCurMb.uiMbType = common.MB_TYPE_16x8
			//pCurMb->mb_partition = 2;
		}

		iCostPart = WelsMdP8x16(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice)
		if iCostPart <= iCost {
			iCost = iCostPart
			pCurMb.uiMbType = common.MB_TYPE_8x16
			//pCurMb->mb_partition = 2;
		}
	}
}

func WelsMdInterFinePartitionVaa(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pSlice *SSlice, pCurMb *SMB, iBestCost int32) {
	pCurDqLayer := pEncCtx.pCurDqLayer
	//  SMbCache *pMbCache = &pSlice->sMbCacheInfo;
	var iCostP8x16, iCostP16x8, iCostP8x8 int32
	uiMbSign := pEncCtx.pFuncList.pfGetMbSignFromInterVaa(pEncCtx.pVaa.sVaaCalcInfo.PSad8x8[pCurMb.iMbXY][:])

	if uiMbSign == 15 {
		return
	}

	//  iCost = pWelsMd->sMe16x16.uiSatdCost;

	switch uiMbSign {
	case 3, 12:
		iCostP16x8 = WelsMdP16x8(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice)
		if iCostP16x8 < iBestCost {
			iBestCost = iCostP16x8
			pCurMb.uiMbType = common.MB_TYPE_16x8
			//pCurMb->mb_partition = 2;
		}

	case 5, 10:
		iCostP8x16 = WelsMdP8x16(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice)
		if iCostP8x16 < iBestCost {
			iBestCost = iCostP8x16
			pCurMb.uiMbType = common.MB_TYPE_8x16
			//pCurMb->mb_partition = 2;
		}

	case 6, 9:
		iCostP8x8 = WelsMdP8x8(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice)
		if iCostP8x8 < iBestCost {
			iBestCost = iCostP8x8
			pCurMb.uiMbType = common.MB_TYPE_8x8
			setSubMbType8x8(pCurMb)
		}

	default:
		iCostP8x8 = WelsMdP8x8(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice)
		if iCostP8x8 < iBestCost {
			iBestCost = iCostP8x8
			pCurMb.uiMbType = common.MB_TYPE_8x8
			setSubMbType8x8(pCurMb)

			iCostP16x8 = WelsMdP16x8(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice)
			if iCostP16x8 <= iBestCost {
				iBestCost = iCostP16x8
				pCurMb.uiMbType = common.MB_TYPE_16x8
			}

			iCostP8x16 = WelsMdP8x16(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice)
			if iCostP8x16 <= iBestCost {
				iBestCost = iCostP8x16
				pCurMb.uiMbType = common.MB_TYPE_8x16
			}
		}
	}
	pWelsMd.iCostLuma = iBestCost
}

// (inline in C)
func VaaBackgroundMbDataUpdate(pFunc *SWelsFuncPtrList, pVaaInfo *SVAAFrameInfo, pCurMb *SMB) {
	kiPicStride := pVaaInfo.iPicStride
	kiPicStrideUV := pVaaInfo.iPicStrideUV
	kiOffsetY := int((int32(pCurMb.iMbY)*kiPicStride + int32(pCurMb.iMbX)) << 4)
	kiOffsetUV := int((int32(pCurMb.iMbY)*kiPicStrideUV + int32(pCurMb.iMbX)) << 3)

	pFunc.pfCopy16x16Aligned(pVaaInfo.pCurY, pVaaInfo.iCurYOff+kiOffsetY, kiPicStride, pVaaInfo.pRefY, pVaaInfo.iRefYOff+kiOffsetY, kiPicStride)
	pFunc.pfCopy8x8Aligned(pVaaInfo.pCurU, pVaaInfo.iCurUOff+kiOffsetUV, kiPicStrideUV, pVaaInfo.pRefU, pVaaInfo.iRefUOff+kiOffsetUV, kiPicStrideUV)
	pFunc.pfCopy8x8Aligned(pVaaInfo.pCurV, pVaaInfo.iCurVOff+kiOffsetUV, kiPicStrideUV, pVaaInfo.pRefV, pVaaInfo.iRefVOff+kiOffsetUV, kiPicStrideUV)
}

// setRefIndexZero is ST32 (pCurMb->pRefIndex, 0).
func setRefIndexZero(pCurMb *SMB) {
	pCurMb.pRefIndex[0] = 0
	pCurMb.pRefIndex[1] = 0
	pCurMb.pRefIndex[2] = 0
	pCurMb.pRefIndex[3] = 0
}

// chromaQpOf returns g_kuiChromaQpTable[CLIP3_QP_0_51 (uiLumaQp + uiChromaQpIndexOffset)].
func chromaQpOf(pCurDqLayer *SDqLayer, uiLumaQp uint8) uint8 {
	return common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(int32(uiLumaQp)+
		int32(pCurDqLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset))]
}

func WelsMdBackgroundMbEnc(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache, pSlice *SSlice, bSkipMbFlag bool) {
	pCurDqLayer := pEncCtx.pCurDqLayer
	pFunc := pEncCtx.pFuncList
	sMvp := SMVUnitXY{}
	pRefLuma, iRefLumaOff := pMbCache.SPicData.pRefMb[0], pMbCache.SPicData.iRefMbOff[0]
	pRefCb, iRefCbOff := pMbCache.SPicData.pRefMb[1], pMbCache.SPicData.iRefMbOff[1]
	pRefCr, iRefCrOff := pMbCache.SPicData.pRefMb[2], pMbCache.SPicData.iRefMbOff[2]
	iLineSizeY := pCurDqLayer.pRefPic.iLineSize[0]
	iLineSizeUV := pCurDqLayer.pRefPic.iLineSize[1]
	pDstLuma, iDstLumaOff := pMbCache.pSkipMb, 0
	pDstCb, iDstCbOff := pMbCache.pSkipMb, 256
	pDstCr, iDstCrOff := pMbCache.pSkipMb, 256+64

	if !bSkipMbFlag {
		pDstLuma, iDstLumaOff = pMbCache.pMemPredLuma, 0
		pDstCb, iDstCbOff = pMbCache.pMemPredChroma, 0
		pDstCr, iDstCrOff = pMbCache.pMemPredChroma, 64
	}
	//MC
	pFunc.sMcFuncs.PMcLumaFunc(pRefLuma, iRefLumaOff, iLineSizeY, pDstLuma, iDstLumaOff, 16, 0, 0, 16, 16)
	pFunc.sMcFuncs.PMcChromaFunc(pRefCb, iRefCbOff, iLineSizeUV, pDstCb, iDstCbOff, 8, sMvp.iMvX, sMvp.iMvY, 8, 8) //Cb
	pFunc.sMcFuncs.PMcChromaFunc(pRefCr, iRefCrOff, iLineSizeUV, pDstCr, iDstCrOff, 8, sMvp.iMvX, sMvp.iMvY, 8, 8) //Cr

	pCurMb.uiCbp = 0
	pMbCache.bCollocatedPredFlag = true
	pWelsMd.iCostLuma = 0 //BGD&RC integration
	*pCurMb.pSadCost = pFunc.sSampleDealingFuncs.pfSampleSad[BLOCK_16x16](pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
		pCurDqLayer.iEncStride[0], pRefLuma, iRefLumaOff, iLineSizeY)
	pCurMb.sP16x16Mv = SMVUnitXY{}
	pCurDqLayer.pDecPic.sMvList[pCurMb.iMbXY] = SMVUnitXY{}

	if bSkipMbFlag {
		pCurMb.uiMbType = MB_TYPE_BACKGROUND

		//update motion info to current MB
		setRefIndexZero(pCurMb)
		pFunc.pfUpdateMbMv(pCurMb.sMv, sMvp)

		pCurMb.uiLumaQp = pSlice.uiLastMbQp
		pCurMb.uiChromaQp = chromaQpOf(pCurDqLayer, pCurMb.uiLumaQp)

		WelsRecPskip(pCurDqLayer, pEncCtx.pFuncList, pCurMb, pMbCache)
		VaaBackgroundMbDataUpdate(pEncCtx.pFuncList, pEncCtx.pVaa, pCurMb)
		return
	}

	pCurMb.uiMbType = common.MB_TYPE_16x16

	pWelsMd.sMe.sMe16x16.sMv.iMvX = 0
	pWelsMd.sMe.sMe16x16.sMv.iMvY = 0
	PredMv(&pMbCache.sMvComponents, 0, 4, int32(pWelsMd.uiRef), &pWelsMd.sMe.sMe16x16.sMvp)
	pMbCache.sMbMvp[0] = pWelsMd.sMe.sMe16x16.sMvp

	UpdateP16x16MotionInfo(pMbCache, pCurMb, int8(pWelsMd.uiRef), &pWelsMd.sMe.sMe16x16.sMv)

	if pWelsMd.bMdUsingSad {
		pWelsMd.iCostLuma = *pCurMb.pSadCost
	} else {
		pWelsMd.iCostLuma = pFunc.sSampleDealingFuncs.pfSampleSatd[BLOCK_16x16](pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
			pCurDqLayer.iEncStride[0], pRefLuma, iRefLumaOff, iLineSizeY)
	}

	WelsInterMbEncode(pEncCtx, pSlice, pCurMb)
	WelsPMbChromaEncode(pEncCtx, pSlice, pCurMb)

	pFunc.pfCopy16x16Aligned(pMbCache.SPicData.pCsMb[0], pMbCache.SPicData.iCsMbOff[0], pCurDqLayer.iCsStride[0], pMbCache.pMemPredLuma, 0, 16)
	pFunc.pfCopy8x8Aligned(pMbCache.SPicData.pCsMb[1], pMbCache.SPicData.iCsMbOff[1], pCurDqLayer.iCsStride[1], pMbCache.pMemPredChroma, 0, 8)
	pFunc.pfCopy8x8Aligned(pMbCache.SPicData.pCsMb[2], pMbCache.SPicData.iCsMbOff[2], pCurDqLayer.iCsStride[1], pMbCache.pMemPredChroma, 64, 8)
}

func WelsMdPSkipEnc(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) bool {
	pCurLayer := pEncCtx.pCurDqLayer
	pFunc := pEncCtx.pFuncList

	pRefLuma, iRefLumaOff := pMbCache.SPicData.pRefMb[0], pMbCache.SPicData.iRefMbOff[0]
	pRefCb, iRefCbOff := pMbCache.SPicData.pRefMb[1], pMbCache.SPicData.iRefMbOff[1]
	pRefCr, iRefCrOff := pMbCache.SPicData.pRefMb[2], pMbCache.SPicData.iRefMbOff[2]
	iLineSizeY := pCurLayer.pRefPic.iLineSize[0]
	iLineSizeUV := pCurLayer.pRefPic.iLineSize[1]

	pDstLuma, iDstLumaOff := pMbCache.pSkipMb, 0
	pDstCb, iDstCbOff := pMbCache.pSkipMb, 256
	pDstCr, iDstCrOff := pMbCache.pSkipMb, 256+64

	sMvp := SMVUnitXY{}
	var n int32

	iEncStride := pCurLayer.iEncStride[0]
	pEncMb, iEncMbOff := pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0]
	pStrideEncBlockOffset := pEncCtx.pStrideTab.pStrideEncBlockOffset[pEncCtx.uiDependencyId]
	var pEncBlockOffset []int32

	var iSadCostLuma int32 = 0
	var iSadCostChroma int32 = 0
	var iSadCostMb int32 = 0

	PredSkipMv(pMbCache, &sMvp)

	// Special case, need to clip the vector //
	sQpelMvp := SMVUnitXY{sMvp.iMvX >> 2, sMvp.iMvY >> 2}
	n = (int32(pCurMb.iMbX) << 4) + int32(sQpelMvp.iMvX)
	if n < -29 {
		return false
	} else if n > ((int32(pCurLayer.iMbWidth) << 4) + 12) {
		return false
	}

	n = (int32(pCurMb.iMbY) << 4) + int32(sQpelMvp.iMvY)
	if n < -29 {
		return false
	} else if n > ((int32(pCurLayer.iMbHeight) << 4) + 12) {
		return false
	}

	//luma
	iRefLumaOff += int(int32(sQpelMvp.iMvY)*iLineSizeY + int32(sQpelMvp.iMvX))
	pFunc.sMcFuncs.PMcLumaFunc(pRefLuma, iRefLumaOff, iLineSizeY, pDstLuma, iDstLumaOff, 16, sMvp.iMvX, sMvp.iMvY, 16, 16)
	iSadCostLuma = pFunc.sSampleDealingFuncs.pfSampleSad[BLOCK_16x16](pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
		pCurLayer.iEncStride[0], pDstLuma, iDstLumaOff, 16)

	iStrideUV := int((int32(sQpelMvp.iMvY)>>1)*iLineSizeUV + (int32(sQpelMvp.iMvX) >> 1))
	iRefCbOff += iStrideUV
	pFunc.sMcFuncs.PMcChromaFunc(pRefCb, iRefCbOff, iLineSizeUV, pDstCb, iDstCbOff, 8, sMvp.iMvX, sMvp.iMvY, 8, 8) //Cb
	iSadCostChroma = pFunc.sSampleDealingFuncs.pfSampleSad[BLOCK_8x8](pMbCache.SPicData.pEncMb[1], pMbCache.SPicData.iEncMbOff[1],
		pCurLayer.iEncStride[1], pDstCb, iDstCbOff, 8)

	iRefCrOff += iStrideUV
	pFunc.sMcFuncs.PMcChromaFunc(pRefCr, iRefCrOff, iLineSizeUV, pDstCr, iDstCrOff, 8, sMvp.iMvX, sMvp.iMvY, 8, 8) //Cr
	iSadCostChroma += pFunc.sSampleDealingFuncs.pfSampleSad[BLOCK_8x8](pMbCache.SPicData.pEncMb[2], pMbCache.SPicData.iEncMbOff[2],
		pCurLayer.iEncStride[2], pDstCr, iDstCrOff, 8)

	iSadCostMb = iSadCostLuma + iSadCostChroma

	// update motion info to current MB and finish as P_Skip
	decideSkip := func() bool {
		//update motion info to current MB
		setRefIndexZero(pCurMb)
		pFunc.pfUpdateMbMv(pCurMb.sMv, sMvp)

		if pWelsMd.bMdUsingSad {
			*pCurMb.pSadCost = iSadCostLuma
			pWelsMd.iCostLuma = *pCurMb.pSadCost
		} else {
			pWelsMd.iCostLuma = pFunc.sSampleDealingFuncs.pfSampleSatd[BLOCK_16x16](pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
				pCurLayer.iEncStride[0], pDstLuma, iDstLumaOff, 16)
		}

		pWelsMd.iCostSkipMb = iSadCostMb

		pCurMb.sP16x16Mv = sMvp
		pCurLayer.pDecPic.sMvList[pCurMb.iMbXY] = sMvp

		return true
	}

	if iSadCostMb == 0 ||
		iSadCostMb < pWelsMd.iSadPredSkip ||
		(pCurLayer.pRefPic.iPictureType == common.P_SLICE &&
			pMbCache.uiRefMbType == common.MB_TYPE_SKIP &&
			iSadCostMb < pCurLayer.pRefPic.pMbSkipSad[pCurMb.iMbXY]) {
		return decideSkip()
	}

	WelsDctMb(pMbCache.pCoeffLevel, pEncMb, iEncMbOff, iEncStride, pDstLuma, iDstLumaOff, pEncCtx.pFuncList.pfDctFourT4)

	if WelsTryPYskip(pEncCtx, pCurMb, pMbCache) {
		iEncStride = pEncCtx.pCurDqLayer.iEncStride[1]
		pEncMb, iEncMbOff = pMbCache.SPicData.pEncMb[1], pMbCache.SPicData.iEncMbOff[1]
		pEncBlockOffset = pStrideEncBlockOffset[16:]
		pFunc.pfDctFourT4(pMbCache.pCoeffLevel[256:], pEncMb, iEncMbOff+int(pEncBlockOffset[0]), iEncStride, pMbCache.pSkipMb, 256, 8)
		if WelsTryPUVskip(pEncCtx, pCurMb, pMbCache, 1) {
			pEncMb, iEncMbOff = pMbCache.SPicData.pEncMb[2], pMbCache.SPicData.iEncMbOff[2]
			pEncBlockOffset = pStrideEncBlockOffset[20:]
			pFunc.pfDctFourT4(pMbCache.pCoeffLevel[320:], pEncMb, iEncMbOff+int(pEncBlockOffset[0]), iEncStride, pMbCache.pSkipMb, 320, 8)
			if WelsTryPUVskip(pEncCtx, pCurMb, pMbCache, 2) {
				return decideSkip()
			}
		}
	}
	return false
}

var g_kiPixStrideIdx8x8 = [4]int32{0, ME_REFINE_BUF_WIDTH_BLK8,
	ME_REFINE_BUF_STRIDE_BLK8, ME_REFINE_BUF_STRIDE_BLK8 + ME_REFINE_BUF_WIDTH_BLK8,
}

var g_kiPixStrideIdx4x4 = [4][4]int32{
	{
		0,
		0 + ME_REFINE_BUF_WIDTH_BLK4,
		0 + ME_REFINE_BUF_STRIDE_BLK4,
		0 + ME_REFINE_BUF_WIDTH_BLK4 + ME_REFINE_BUF_STRIDE_BLK4,
	}, //[0][]
	{
		ME_REFINE_BUF_WIDTH_BLK8,
		ME_REFINE_BUF_WIDTH_BLK8 + ME_REFINE_BUF_WIDTH_BLK4,
		ME_REFINE_BUF_WIDTH_BLK8 + ME_REFINE_BUF_STRIDE_BLK4,
		ME_REFINE_BUF_WIDTH_BLK8 + ME_REFINE_BUF_WIDTH_BLK4 + ME_REFINE_BUF_STRIDE_BLK4,
	}, //[1][]
	{
		ME_REFINE_BUF_STRIDE_BLK8,
		ME_REFINE_BUF_STRIDE_BLK8 + ME_REFINE_BUF_WIDTH_BLK4,
		ME_REFINE_BUF_STRIDE_BLK8 + ME_REFINE_BUF_STRIDE_BLK4,
		ME_REFINE_BUF_STRIDE_BLK8 + ME_REFINE_BUF_WIDTH_BLK4 + ME_REFINE_BUF_STRIDE_BLK4,
	}, //[2][]
	{
		ME_REFINE_BUF_STRIDE_BLK8 + ME_REFINE_BUF_WIDTH_BLK8,
		ME_REFINE_BUF_STRIDE_BLK8 + ME_REFINE_BUF_WIDTH_BLK8 + ME_REFINE_BUF_WIDTH_BLK4,
		ME_REFINE_BUF_STRIDE_BLK8 + ME_REFINE_BUF_WIDTH_BLK8 + ME_REFINE_BUF_STRIDE_BLK4,
		ME_REFINE_BUF_STRIDE_BLK8 + ME_REFINE_BUF_WIDTH_BLK8 + ME_REFINE_BUF_WIDTH_BLK4 + ME_REFINE_BUF_STRIDE_BLK4,
	}, //[3][]
}

func WelsMdInterMbRefinement(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) {
	pCurDqLayer := pEncCtx.pCurDqLayer
	pFunc := pEncCtx.pFuncList
	var iMvStride, iRefBlk4Stride, iDstBlk4Stride int32
	var pMv *SMVUnitXY
	var iBestSadCost, iBestSatdCost int32 = 0, 0
	var sMeRefine SMeRefinePointer

	var iIdx, iPixStride int32

	pRefCb, iRefCbOff := pMbCache.SPicData.pRefMb[1], pMbCache.SPicData.iRefMbOff[1]
	pRefCr, iRefCrOff := pMbCache.SPicData.pRefMb[2], pMbCache.SPicData.iRefMbOff[2]
	pDstCb, iDstCbOff := pMbCache.pMemPredChroma, 0
	pDstCr, iDstCrOff := pMbCache.pMemPredChroma, 64
	pDstLuma := pMbCache.pMemPredLuma

	iLineSizeRefUV := pCurDqLayer.pRefPic.iLineSize[1]
	pMcChroma := pEncCtx.pFuncList.sMcFuncs.PMcChromaFunc

	// chroma MC of one partition: the reference block at iRefBlk4Stride + iMvStride,
	// the destination at iDstBlk4Stride
	mcChroma := func(iRefStride int32, iDstStride int32, iW int32, iH int32) {
		pMcChroma(pRefCb, iRefCbOff+int(iRefStride), iLineSizeRefUV, pDstCb, iDstCbOff+int(iDstStride), 8, pMv.iMvX, pMv.iMvY, iW, iH) //Cb
		pMcChroma(pRefCr, iRefCrOff+int(iRefStride), iLineSizeRefUV, pDstCr, iDstCrOff+int(iDstStride), 8, pMv.iMvX, pMv.iMvY, iW, iH) //Cr
	}

	switch pCurMb.uiMbType {
	case common.MB_TYPE_16x16:
		//luma
		InitMeRefinePointer(&sMeRefine, pMbCache, 0)
		sMeRefine.pfCopyBlockByMode = pFunc.pfCopy16x16NotAligned // dst can be align with 16 bytes, but not sure at pSrc, 12/29/2011
		MeRefineFracPixel(pEncCtx, pDstLuma, 0, &pWelsMd.sMe.sMe16x16, &sMeRefine, 16, 16)
		UpdateP16x16MotionInfo(pMbCache, pCurMb, int8(pWelsMd.uiRef), &pWelsMd.sMe.sMe16x16.sMv)

		pMbCache.sMbMvp[0] = pWelsMd.sMe.sMe16x16.sMvp
		//save the best cost of final mode
		iBestSadCost = int32(pWelsMd.sMe.sMe16x16.uiSadCost)
		iBestSatdCost = int32(pWelsMd.sMe.sMe16x16.uiSatdCost)

		//chroma
		pMv = &pWelsMd.sMe.sMe16x16.sMv
		iMvStride = (int32(pMv.iMvY)>>3)*iLineSizeRefUV + (int32(pMv.iMvX) >> 3)
		mcChroma(iMvStride, 0, 8, 8)

		pSad := &pEncCtx.pFuncList.sSampleDealingFuncs.pfSampleSad
		pWelsMd.iCostSkipMb = pSad[BLOCK_16x16](pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
			pCurDqLayer.iEncStride[0], pDstLuma, 0, 16)
		pWelsMd.iCostSkipMb += pSad[BLOCK_8x8](pMbCache.SPicData.pEncMb[1], pMbCache.SPicData.iEncMbOff[1],
			pCurDqLayer.iEncStride[1], pDstCb, iDstCbOff, 8)
		pWelsMd.iCostSkipMb += pSad[BLOCK_8x8](pMbCache.SPicData.pEncMb[2], pMbCache.SPicData.iEncMbOff[2],
			pCurDqLayer.iEncStride[2], pDstCr, iDstCrOff, 8)

	case common.MB_TYPE_16x8:
		iPixStride = 0
		sMeRefine.pfCopyBlockByMode = pFunc.pfCopy16x8NotAligned // dst can be align with 16 bytes, but not sure at pSrc, 12/29/2011
		for i := int32(0); i < 2; i++ {
			//luma
			iIdx = i << 3
			InitMeRefinePointer(&sMeRefine, pMbCache, iPixStride)
			iPixStride += ME_REFINE_BUF_STRIDE_BLK8
			PredInter16x8Mv(pMbCache, iIdx, int8(pWelsMd.uiRef), &pWelsMd.sMe.sMe16x8[i].sMvp)
			MeRefineFracPixel(pEncCtx, pDstLuma, int(g_kuiSmb4AddrIn256[iIdx]), &pWelsMd.sMe.sMe16x8[i], &sMeRefine, 16, 8)
			UpdateP16x8MotionInfo(pMbCache, pCurMb, iIdx, int8(pWelsMd.uiRef), &pWelsMd.sMe.sMe16x8[i].sMv)
			pMbCache.sMbMvp[i] = pWelsMd.sMe.sMe16x8[i].sMvp
			//save the best cost of final mode
			iBestSadCost += int32(pWelsMd.sMe.sMe16x8[i].uiSadCost)
			iBestSatdCost += int32(pWelsMd.sMe.sMe16x8[i].uiSatdCost)

			//chroma
			iRefBlk4Stride = (i << 2) * iLineSizeRefUV
			iDstBlk4Stride = i << 5 // 4*8
			pMv = &pWelsMd.sMe.sMe16x8[i].sMv
			iMvStride = (int32(pMv.iMvY)>>3)*iLineSizeRefUV + (int32(pMv.iMvX) >> 3)
			mcChroma(iRefBlk4Stride+iMvStride, iDstBlk4Stride, 8, 4)
		}

	case common.MB_TYPE_8x16:
		iPixStride = 0
		sMeRefine.pfCopyBlockByMode = pFunc.pfCopy8x16Aligned
		for i := int32(0); i < 2; i++ {
			//luma
			iIdx = i << 2
			InitMeRefinePointer(&sMeRefine, pMbCache, iPixStride)
			iPixStride += ME_REFINE_BUF_WIDTH_BLK8
			PredInter8x16Mv(pMbCache, iIdx, int8(pWelsMd.uiRef), &pWelsMd.sMe.sMe8x16[i].sMvp)
			MeRefineFracPixel(pEncCtx, pDstLuma, int(g_kuiSmb4AddrIn256[iIdx]), &pWelsMd.sMe.sMe8x16[i], &sMeRefine, 8, 16)
			update_P8x16_motion_info(pMbCache, pCurMb, iIdx, int8(pWelsMd.uiRef), &pWelsMd.sMe.sMe8x16[i].sMv)
			pMbCache.sMbMvp[i] = pWelsMd.sMe.sMe8x16[i].sMvp
			//save the best cost of final mode
			iBestSadCost += int32(pWelsMd.sMe.sMe8x16[i].uiSadCost)
			iBestSatdCost += int32(pWelsMd.sMe.sMe8x16[i].uiSatdCost)

			//chroma
			iRefBlk4Stride = iIdx //4
			pMv = &pWelsMd.sMe.sMe8x16[i].sMv
			iMvStride = (int32(pMv.iMvY)>>3)*iLineSizeRefUV + (int32(pMv.iMvX) >> 3)
			mcChroma(iRefBlk4Stride+iMvStride, iRefBlk4Stride, 4, 8)
		}

	case common.MB_TYPE_8x8:
		pMbCache.sMvComponents.iRefIndexCache[9] = common.REF_NOT_AVAIL
		pMbCache.sMvComponents.iRefIndexCache[21] = common.REF_NOT_AVAIL
		for i := int32(0); i < 4; i++ {
			iBlk8Idx := i << 2 //0, 4, 8, 12
			var iBlk4X, iBlk4Y, iBlk4x4Idx int32

			pCurMb.pRefIndex[i] = int8(pWelsMd.uiRef)
			switch pCurMb.uiSubMbType[i] {
			case common.SUB_MB_TYPE_8x8:
				sMeRefine.pfCopyBlockByMode = pFunc.pfCopy8x8Aligned
				//luma
				InitMeRefinePointer(&sMeRefine, pMbCache, g_kiPixStrideIdx8x8[i])
				PredMv(&pMbCache.sMvComponents, int8(iBlk8Idx), 2, int32(pWelsMd.uiRef), &pWelsMd.sMe.sMe8x8[i].sMvp)
				MeRefineFracPixel(pEncCtx, pDstLuma, int(g_kuiSmb4AddrIn256[iBlk8Idx]), &pWelsMd.sMe.sMe8x8[i], &sMeRefine, 8, 8)
				UpdateP8x8MotionInfo(pMbCache, pCurMb, iBlk8Idx, int8(pWelsMd.uiRef), &pWelsMd.sMe.sMe8x8[i].sMv)
				pMbCache.sMbMvp[common.G_kuiMbCountScan4Idx[iBlk8Idx]] = pWelsMd.sMe.sMe8x8[i].sMvp
				iBestSadCost += int32(pWelsMd.sMe.sMe8x8[i].uiSadCost)
				iBestSatdCost += int32(pWelsMd.sMe.sMe8x8[i].uiSatdCost)

				//chroma
				pMv = &pWelsMd.sMe.sMe8x8[i].sMv
				iMvStride = (int32(pMv.iMvY)>>3)*iLineSizeRefUV + (int32(pMv.iMvX) >> 3)

				iBlk4X = (i & 1) << 2
				iBlk4Y = (i >> 1) << 2
				iRefBlk4Stride = iBlk4Y*iLineSizeRefUV + iBlk4X
				iDstBlk4Stride = (iBlk4Y << 3) + iBlk4X

				mcChroma(iRefBlk4Stride+iMvStride, iDstBlk4Stride, 4, 4)

			case common.SUB_MB_TYPE_4x4:
				sMeRefine.pfCopyBlockByMode = pFunc.pfCopy4x4
				//luma
				for j := int32(0); j < 4; j++ {
					iBlk4x4Idx = iBlk8Idx + j
					InitMeRefinePointer(&sMeRefine, pMbCache, g_kiPixStrideIdx4x4[i][j])
					PredMv(&pMbCache.sMvComponents, int8(iBlk4x4Idx), 1, int32(pWelsMd.uiRef), &pWelsMd.sMe.sMe4x4[i][j].sMvp)
					MeRefineFracPixel(pEncCtx, pDstLuma, int(g_kuiSmb4AddrIn256[iBlk4x4Idx]), &pWelsMd.sMe.sMe4x4[i][j], &sMeRefine, 4, 4)
					UpdateP4x4MotionInfo(pMbCache, pCurMb, iBlk4x4Idx, int8(pWelsMd.uiRef), &pWelsMd.sMe.sMe4x4[i][j].sMv)
					pMbCache.sMbMvp[common.G_kuiMbCountScan4Idx[iBlk4x4Idx]] = pWelsMd.sMe.sMe4x4[i][j].sMvp
					iBestSadCost += int32(pWelsMd.sMe.sMe4x4[i][j].uiSadCost)
					iBestSatdCost += int32(pWelsMd.sMe.sMe4x4[i][j].uiSatdCost)

					//chroma
					pMv = &pWelsMd.sMe.sMe4x4[i][j].sMv
					iMvStride = (int32(pMv.iMvY)>>3)*iLineSizeRefUV + (int32(pMv.iMvX) >> 3)

					iBlk4X = (((i & 1) << 1) + (j & 1)) << 1
					iBlk4Y = (((i >> 1) << 1) + (j >> 1)) << 1
					iRefBlk4Stride = iBlk4Y*iLineSizeRefUV + iBlk4X
					iDstBlk4Stride = (iBlk4Y << 3) + iBlk4X

					mcChroma(iRefBlk4Stride+iMvStride, iDstBlk4Stride, 2, 2)
				}

			case common.SUB_MB_TYPE_8x4:
				sMeRefine.pfCopyBlockByMode = pFunc.pfCopy8x4
				//luma
				for j := int32(0); j < 2; j++ {
					iBlk4x4Idx = iBlk8Idx + (j << 1)
					InitMeRefinePointer(&sMeRefine, pMbCache, g_kiPixStrideIdx4x4[i][j<<1])
					PredMv(&pMbCache.sMvComponents, int8(iBlk4x4Idx), 2, int32(pWelsMd.uiRef), &pWelsMd.sMe.sMe8x4[i][j].sMvp)
					MeRefineFracPixel(pEncCtx, pDstLuma, int(g_kuiSmb4AddrIn256[iBlk4x4Idx]), &pWelsMd.sMe.sMe8x4[i][j], &sMeRefine, 8, 4)
					UpdateP8x4MotionInfo(pMbCache, pCurMb, iBlk4x4Idx, int8(pWelsMd.uiRef), &pWelsMd.sMe.sMe8x4[i][j].sMv)
					pMbCache.sMbMvp[common.G_kuiMbCountScan4Idx[iBlk4x4Idx]] = pWelsMd.sMe.sMe8x4[i][j].sMvp
					//pMbCache->sMbMvp[g_kuiMbCountScan4Idx[1 + iBlk4x4Idx]] = pWelsMd->sMe.sMe8x4[i][j].sMvp;
					iBestSadCost += int32(pWelsMd.sMe.sMe8x4[i][j].uiSadCost)
					iBestSatdCost += int32(pWelsMd.sMe.sMe8x4[i][j].uiSatdCost)

					//chroma
					pMv = &pWelsMd.sMe.sMe8x4[i][j].sMv
					iMvStride = (int32(pMv.iMvY)>>3)*iLineSizeRefUV + (int32(pMv.iMvX) >> 3)

					iBlk4X = ((i & 1) << 1) << 1
					iBlk4Y = (((i >> 1) << 1) + j) << 1
					iRefBlk4Stride = iBlk4Y*iLineSizeRefUV + iBlk4X
					iDstBlk4Stride = (iBlk4Y << 3) + iBlk4X

					mcChroma(iRefBlk4Stride+iMvStride, iDstBlk4Stride, 4, 2)
				}

			case common.SUB_MB_TYPE_4x8:
				sMeRefine.pfCopyBlockByMode = pFunc.pfCopy4x8
				//luma
				for j := int32(0); j < 2; j++ {
					iBlk4x4Idx = iBlk8Idx + j
					InitMeRefinePointer(&sMeRefine, pMbCache, g_kiPixStrideIdx4x4[i][j])
					PredMv(&pMbCache.sMvComponents, int8(iBlk4x4Idx), 1, int32(pWelsMd.uiRef), &pWelsMd.sMe.sMe4x8[i][j].sMvp)
					MeRefineFracPixel(pEncCtx, pDstLuma, int(g_kuiSmb4AddrIn256[iBlk4x4Idx]), &pWelsMd.sMe.sMe4x8[i][j], &sMeRefine, 4, 8)
					UpdateP4x8MotionInfo(pMbCache, pCurMb, iBlk4x4Idx, int8(pWelsMd.uiRef), &pWelsMd.sMe.sMe4x8[i][j].sMv)
					pMbCache.sMbMvp[common.G_kuiMbCountScan4Idx[iBlk4x4Idx]] = pWelsMd.sMe.sMe4x8[i][j].sMvp
					//pMbCache->sMbMvp[g_kuiMbCountScan4Idx[4 + iBlk4x4Idx]] = pWelsMd->sMe.sMe8x4[i][j].sMvp;
					iBestSadCost += int32(pWelsMd.sMe.sMe4x8[i][j].uiSadCost)
					iBestSatdCost += int32(pWelsMd.sMe.sMe4x8[i][j].uiSatdCost)

					//chroma
					pMv = &pWelsMd.sMe.sMe4x8[i][j].sMv
					iMvStride = (int32(pMv.iMvY)>>3)*iLineSizeRefUV + (int32(pMv.iMvX) >> 3)

					iBlk4X = (((i & 1) << 1) + j) << 1
					iBlk4Y = ((i >> 1) << 1) << 1
					iRefBlk4Stride = iBlk4Y*iLineSizeRefUV + iBlk4X
					iDstBlk4Stride = (iBlk4Y << 3) + iBlk4X

					mcChroma(iRefBlk4Stride+iMvStride, iDstBlk4Stride, 2, 4)
				}
			}
		}
	default:
	}
	*pCurMb.pSadCost = iBestSadCost
	if pWelsMd.bMdUsingSad {
		pWelsMd.iCostLuma = iBestSadCost
	} else {
		pWelsMd.iCostLuma = iBestSatdCost
	}
}

func WelsMdFirstIntraMode(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) bool {
	pFunc := pEncCtx.pFuncList

	iCostI16x16 := WelsMdI16x16(pFunc, pEncCtx.pCurDqLayer, pMbCache, pWelsMd.iLambda)

	//compare cost_p16x16 with cost_i16x16
	if iCostI16x16 < pWelsMd.iCostLuma {
		pCurMb.uiMbType = common.MB_TYPE_INTRA16x16
		pWelsMd.iCostLuma = iCostI16x16

		pFunc.pfIntraFineMd(pEncCtx, pWelsMd, pCurMb, pMbCache)

		//add pEnc&rec to MD--2010.3.15
		if common.IS_INTRA16x16(pCurMb.uiMbType) {
			pCurMb.uiCbp = 0
			WelsEncRecI16x16Y(pEncCtx, pCurMb, pMbCache)
		}

		//chroma
		pWelsMd.iCostChroma = WelsMdIntraChroma(pFunc, pEncCtx.pCurDqLayer, pMbCache, pWelsMd.iLambda)
		WelsIMbChromaEncode(pEncCtx, pCurMb, pMbCache) //add pEnc&rec to MD--2010.3.15
		pCurMb.uiChromPredMode = uint32(pMbCache.uiChmaI8x8Mode)
		*pCurMb.pSadCost = 0
		return true //intra_mb_type is best
	}

	return false
}

func WelsMdInterMb(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pSlice *SSlice, pCurMb *SMB, pUnused *SMbCache) {
	if tryUnchangedMacroblock(pEncCtx, pWelsMd, pSlice, pCurMb) {
		return
	}
	pCurDqLayer := pEncCtx.pCurDqLayer
	pMbCache := &pSlice.sMbCacheInfo
	kuiNeighborAvail := uint32(pCurMb.uiNeighborAvail)
	kiMbWidth := int32(pCurDqLayer.iMbWidth)
	// top_mb = pCurMb - kiMbWidth (only dereferenced when available)
	bMbLeftAvailPskip := kuiNeighborAvail&LEFT_MB_POS != 0 && common.IS_SKIP(pCurMb.Add(-1).uiMbType)
	bMbTopAvailPskip := kuiNeighborAvail&TOP_MB_POS != 0 && common.IS_SKIP(pCurMb.Add(-kiMbWidth).uiMbType)
	bMbTopLeftAvailPskip := kuiNeighborAvail&TOPLEFT_MB_POS != 0 && common.IS_SKIP(pCurMb.Add(-kiMbWidth-1).uiMbType)
	bMbTopRightAvailPskip := kuiNeighborAvail&TOPRIGHT_MB_POS != 0 && common.IS_SKIP(pCurMb.Add(-kiMbWidth+1).uiMbType)
	bTrySkip := bMbLeftAvailPskip || bMbTopAvailPskip || bMbTopLeftAvailPskip || bMbTopRightAvailPskip
	pMbCache.bKeepSkipScratch = bMbLeftAvailPskip && bMbTopAvailPskip && bMbTopRightAvailPskip
	bSkip := false

	//try BGD skip
	if pEncCtx.pFuncList.pfInterMdBackgroundDecision(pEncCtx, pWelsMd, pSlice, pCurMb, pMbCache, &pMbCache.bKeepSkipScratch) {
		return
	}

	//try static or scrolled Pskip
	if pEncCtx.pFuncList.pfSCDPSkipDecision(pEncCtx, pWelsMd, pSlice, pCurMb, pMbCache) {
		return
	}

	//step 1: try SKIP
	bSkip = WelsMdInterJudgePskip(pEncCtx, pWelsMd, pSlice, pCurMb, pMbCache, bTrySkip)

	if bSkip {
		if pMbCache.bKeepSkipScratch {
			WelsMdInterDecidedPskip(pEncCtx, pSlice, pCurMb, pMbCache)
			return
		}
	} else {
		PredictSad(pMbCache.sMvComponents.iRefIndexCache[:], pMbCache.iSadCost[:], 0, &pWelsMd.iSadPredMb)

		//step 2: P_16x16
		pWelsMd.iCostLuma = WelsMdP16x16(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice, pCurMb)
		pCurMb.uiMbType = common.MB_TYPE_16x16
	}

	WelsMdInterSecondaryModesEnc(pEncCtx, pWelsMd, pSlice, pCurMb, pMbCache, bSkip)
}

// try the ordinary Pskip
func WelsMdInterJudgePskip(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pSlice *SSlice, pCurMb *SMB, pMbCache *SMbCache, bTrySkip bool) bool {
	bRet := true
	if ((pEncCtx.pRefPic.iPictureType == common.P_SLICE) && (pMbCache.uiRefMbType == common.MB_TYPE_SKIP ||
		pMbCache.uiRefMbType == MB_TYPE_BACKGROUND)) ||
		bTrySkip {
		PredictSadSkip(pMbCache.sMvComponents.iRefIndexCache[:], pMbCache.bMbTypeSkip[:], pMbCache.iSadCostSkip[:], 0,
			&pWelsMd.iSadPredSkip)
		bRet = WelsMdPSkipEnc(pEncCtx, pWelsMd, pCurMb, pMbCache)
		return bRet
	}

	return false
}

// try the ordinary Pskip
func WelsMdInterUpdatePskip(pCurDqLayer *SDqLayer, pSlice *SSlice, pCurMb *SMB, pMbCache *SMbCache) {
	//add pEnc&rec to MD--2010.3.15
	pCurMb.uiCbp = 0
	pCurMb.uiLumaQp = pSlice.uiLastMbQp
	pCurMb.uiChromaQp = chromaQpOf(pCurDqLayer, pCurMb.uiLumaQp)
	pMbCache.bCollocatedPredFlag = pCurMb.sMv[0] == SMVUnitXY{}
}

// doublecheck if current MBTYPE is Pskip
func WelsMdInterDoubleCheckPskip(pCurMb *SMB, pMbCache *SMbCache) {
	if common.MB_TYPE_16x16 == pCurMb.uiMbType && 0 == pCurMb.uiCbp {
		if 0 == pCurMb.pRefIndex[0] {
			sMvp := SMVUnitXY{}

			PredSkipMv(pMbCache, &sMvp)
			if sMvp == pCurMb.sMv[0] {
				pCurMb.uiMbType = common.MB_TYPE_SKIP
			}
		}
		pMbCache.bCollocatedPredFlag = pCurMb.sMv[0] == SMVUnitXY{}
	}
}

// Pskip mb encode
func WelsMdInterDecidedPskip(pEncCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB, pMbCache *SMbCache) {
	pCurDqLayer := pEncCtx.pCurDqLayer
	pCurMb.uiMbType = common.MB_TYPE_SKIP
	WelsRecPskip(pCurDqLayer, pEncCtx.pFuncList, pCurMb, pMbCache)
	WelsMdInterUpdatePskip(pCurDqLayer, pSlice, pCurMb, pMbCache)
}

// inter mb encode
func WelsMdInterEncode(pEncCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB, pMbCache *SMbCache) {
	pFunc := pEncCtx.pFuncList
	pCurDqLayer := pEncCtx.pCurDqLayer

	//add pEnc&rec to MD--2010.3.15
	kiCsStrideY := pCurDqLayer.iCsStride[0]
	kiCsStrideUV := pCurDqLayer.iCsStride[1]

	//add pEnc&rec to MD--2010.3.15
	pCurMb.uiCbp = 0
	WelsInterMbEncode(pEncCtx, pSlice, pCurMb)
	WelsPMbChromaEncode(pEncCtx, pSlice, pCurMb)

	pFunc.pfCopy16x16Aligned(pMbCache.SPicData.pCsMb[0], pMbCache.SPicData.iCsMbOff[0], kiCsStrideY, pMbCache.pMemPredLuma, 0, 16)
	pFunc.pfCopy8x8Aligned(pMbCache.SPicData.pCsMb[1], pMbCache.SPicData.iCsMbOff[1], kiCsStrideUV, pMbCache.pMemPredChroma, 0, 8)
	pFunc.pfCopy8x8Aligned(pMbCache.SPicData.pCsMb[2], pMbCache.SPicData.iCsMbOff[2], kiCsStrideUV, pMbCache.pMemPredChroma, 64, 8)
}

// pRefMbtypeList: SPicture.uiRefMbType (whole per-MB array).
func WelsMdInterSaveSadAndRefMbType(pRefMbtypeList []Mb_Type, pMbCache *SMbCache, pCurMb *SMB, pMd *SWelsMD) {
	kmtCurMbtype := pCurMb.uiMbType

	//sad
	if kmtCurMbtype == common.MB_TYPE_SKIP {
		pMbCache.pEncSad[pMbCache.iEncSadOff] = pMd.iCostSkipMb
	} else {
		pMbCache.pEncSad[pMbCache.iEncSadOff] = 0
	}
	//uiMbType
	pRefMbtypeList[pCurMb.iMbXY] = kmtCurMbtype
}

func WelsMdInterSecondaryModesEnc(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pSlice *SSlice, pCurMb *SMB, pMbCache *SMbCache, bSkip bool) {
	//step 2: Intra
	kbTrySkip := pEncCtx.pFuncList.pfFirstIntraMode(pEncCtx, pWelsMd, pCurMb, pMbCache)
	if kbTrySkip {
		return
	}

	if bSkip {
		WelsMdInterDecidedPskip(pEncCtx, pSlice, pCurMb, pMbCache)
	} else {
		//Step 3: SubP16 MD
		pEncCtx.pFuncList.pfSetScrollingMv(pEncCtx.pVaa, pWelsMd) //SCC
		pEncCtx.pFuncList.pfInterFineMd(pEncCtx, pWelsMd, pSlice, pCurMb, pWelsMd.iCostLuma)

		//refinement for inter type
		WelsMdInterMbRefinement(pEncCtx, pWelsMd, pCurMb, pMbCache)

		//step 7: invoke encoding
		WelsMdInterEncode(pEncCtx, pSlice, pCurMb, pMbCache)

		//step 8: double check Pskip
		WelsMdInterDoubleCheckPskip(pCurMb, pMbCache)
	}
}

func WelsMdIntraSecondaryModesEnc(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) {
	pFunc := pEncCtx.pFuncList
	//initial prediction memory for I_4x4
	pFunc.pfIntraFineMd(pEncCtx, pWelsMd, pCurMb, pMbCache) //WelsMdIntraFinePartitionVaa

	//add pEnc&rec to MD--2010.3.15
	if common.IS_INTRA16x16(pCurMb.uiMbType) {
		pCurMb.uiCbp = 0
		WelsEncRecI16x16Y(pEncCtx, pCurMb, pMbCache)
	}

	//chroma
	pWelsMd.iCostChroma = WelsMdIntraChroma(pFunc, pEncCtx.pCurDqLayer, pMbCache, pWelsMd.iLambda)
	WelsIMbChromaEncode(pEncCtx, pCurMb, pMbCache) //add pEnc&rec to MD--2010.3.15
	pCurMb.uiChromPredMode = uint32(pMbCache.uiChmaI8x8Mode)
	*pCurMb.pSadCost = 0
}
