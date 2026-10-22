// Port of codec/encoder/core/inc/wels_func_ptr_def.h.

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/processing"
)

// PSetMemoryZero is void (*) (void* pDst, int32_t iSize). It is only used on
// int16_t coefficient buffers; iSize is in BYTES as in C (iSize/2 elements
// are cleared).
type PSetMemoryZero func(pDst []int16, iSize int32)

type PDctFunc func(pDct []int16, pSample1 []uint8, iSample1Off int, iStride1 int32, pSample2 []uint8, iSample2Off int, iStride2 int32)

// PCopyFunc has the signature of the common.WelsCopy*_c block copies.
type PCopyFunc func(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32)

type PIDctFunc func(pRec []uint8, iRecOff int, iStride int32, pPred []uint8, iPredOff int, iPredStride int32, pRes []int16)

type PDeQuantizationFunc func(pRes []int16, kpQpTable []uint16)
type PDeQuantizationHadamardFunc func(pRes []int16, kuiMF uint16)
type PGetNoneZeroCountFunc func(pLevel []int16) int32

type PScanFunc func(pLevel []int16, pDct []int16)
type PCalculateSingleCtrFunc func(pDct []int16) int32

type PTransformHadamard4x4Func func(pLumaDc []int16, pDct []int16)
type PQuantizationFunc func(pDct []int16, pFF []int16, pMF []int16)
type PQuantizationMaxFunc func(pDct []int16, pFF []int16, pMF []int16, pMax []int16)
type PQuantizationDcFunc func(pDct []int16, iFF int16, iMF int16)
type PQuantizationSkipFunc func(pDct []int16, iFF int16, iMF int16) int32
type PQuantizationHadamardFunc func(pRes []int16, kiFF int16, iMF int16, pDct []int16, pBlock []int16) int32

// Deblocking filters: same signatures as the common.DeblockLuma*/DeblockChroma* _c filters.
type PLumaDeblockingLT4Func func(iSampleY []uint8, iSampleYOff int, iStride int32, iAlpha int32, iBeta int32, iTc []int8)
type PLumaDeblockingEQ4Func func(iSampleY []uint8, iSampleYOff int, iStride int32, iAlpha int32, iBeta int32)
type PChromaDeblockingLT4Func func(iSampleCb []uint8, iSampleCbOff int, iSampleCr []uint8, iSampleCrOff int, iStride int32, iAlpha int32,
	iBeta int32, iTc []int8)
type PChromaDeblockingEQ4Func func(iSampleCb []uint8, iSampleCbOff int, iSampleCr []uint8, iSampleCrOff int, iStride int32, iAlpha int32,
	iBeta int32)

// PDeblockingBSCalc: uint8_t uiBS[2][4][4] -> *[2][4][4]uint8.
type PDeblockingBSCalc func(pFunc *SWelsFuncPtrList, pCurMb *SMB, uiBS *[2][4][4]uint8, uiCurMbType Mb_Type,
	iMbStride int32, iLeftFlag int32, iTopFlag int32)
type PDeblockingFilterSlice func(pCurDq *SDqLayer, pFunc *SWelsFuncPtrList, pSlice *SSlice)

// DeblockingFunc is the deblocking function table (struct tagDeblockingFunc).
type DeblockingFunc struct {
	pfLumaDeblockingLT4Ver   PLumaDeblockingLT4Func
	pfLumaDeblockingEQ4Ver   PLumaDeblockingEQ4Func
	pfLumaDeblockingLT4Hor   PLumaDeblockingLT4Func
	pfLumaDeblockingEQ4Hor   PLumaDeblockingEQ4Func
	pfChromaDeblockingLT4Ver PChromaDeblockingLT4Func
	pfChromaDeblockingEQ4Ver PChromaDeblockingEQ4Func
	pfChromaDeblockingLT4Hor PChromaDeblockingLT4Func
	pfChromaDeblockingEQ4Hor PChromaDeblockingEQ4Func
	pfDeblockingBSCalc       PDeblockingBSCalc
	pfDeblockingFilterSlice  PDeblockingFilterSlice
}

type PSetNoneZeroCountZeroFunc func(pNonZeroCount []int8)

type PIntraFineMdFunc func(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) int32
type PInterFineMdFunc func(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, slice *SSlice, pCurMb *SMB, bestCost int32)
type PInterMdFirstIntraModeFunc func(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) bool

// PFillInterNeighborCacheFunc: int8_t* pVaaBgMbFlag points at the current MB's
// entry of SVAAFrameInfo.pVaaBackgroundMbFlag and is indexed negatively ->
// (slice, offset).
type PFillInterNeighborCacheFunc func(pMbCache *SMbCache, pCurMb *SMB, iMbWidth int32, pVaaBgMbFlag []int8, iVaaBgMbFlagOff int)
type PAccumulateSadFunc func(pSumDiff *uint32, pGomForegroundBlockNum []int32, iSad8x8 []int32,
	pVaaBgMbFlag []int8) // for RC
type PDynamicSlicingStepBackFunc func(pEncCtx *sWelsEncCtx, pSlice *SSlice, pSliceCtx *SSliceCtx, pCurMb *SMB,
	pDynamicSlicingStack *SDynamicSlicingStack) bool
type PInterMdBackgroundDecisionFunc func(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, slice *SSlice, pCurMb *SMB,
	pMbCache *SMbCache, pKeepPskip *bool) bool
type PMdBackgroundInfoUpdateFunc func(pCurLayer *SDqLayer, pCurMb *SMB, bFlag bool,
	kiRefPictureType int32)

type PInterMdScrollingPSkipDecisionFunc func(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, slice *SSlice, pCurMb *SMB,
	pMbCache *SMbCache) bool
type PSetScrollingMv func(pVaa *SVAAFrameInfo, pMd *SWelsMD)

type PInterMdFunc func(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, slice *SSlice, pCurMb *SMB, pMbCache *SMbCache)

// PSampleSadSatdCostFunc: int32_t (*) (uint8_t*, int32_t, uint8_t*, int32_t);
// same signature as the common.WelsSampleSad*_c functions.
type PSampleSadSatdCostFunc func(pSample1 []uint8, iSample1Off int, iStride1 int32, pSample2 []uint8, iSample2Off int, iStride2 int32) int32

// PSample4SadCostFunc: void (*) (uint8_t*, int32_t, uint8_t*, int32_t, int32_t*);
// same signature as the common.WelsSampleSadFour*_c functions (pSad receives 4 SADs).
type PSample4SadCostFunc func(pSample1 []uint8, iSample1Off int, iStride1 int32, pSample2 []uint8, iSample2Off int, iStride2 int32, pSad []int32)

// PIntraPred4x4Combined3Func: (pDec, iDecStride, pEnc, iEncStride, pDst, pBestMode, iLambda2, iLambda1, iLambda0).
type PIntraPred4x4Combined3Func func(pDec []uint8, iDecOff int, iDecStride int32, pEnc []uint8, iEncOff int, iEncStride int32,
	pDst []uint8, iDstOff int, pBestMode *int32, iLambda2 int32, iLambda1 int32, iLambda0 int32) int32

// PIntraPred16x16Combined3Func: (pDec, iDecStride, pEnc, iEncStride, pBestMode, iLambda, pDst).
type PIntraPred16x16Combined3Func func(pDec []uint8, iDecOff int, iDecStride int32, pEnc []uint8, iEncOff int, iEncStride int32,
	pBestMode *int32, iLambda int32, pDst []uint8, iDstOff int) int32

// PIntraPred8x8Combined3Func: (pDecCb, iDecStride, pEncCb, iEncStride, pBestMode, iLambda, pDstChroma, pDecCr, pEncCr).
type PIntraPred8x8Combined3Func func(pDecCb []uint8, iDecCbOff int, iDecStride int32, pEncCb []uint8, iEncCbOff int, iEncStride int32,
	pBestMode *int32, iLambda int32, pDstChroma []uint8, iDstChromaOff int, pDecCr []uint8, iDecCrOff int, pEncCr []uint8, iEncCrOff int) int32

// PSampleSadHor8Func: uint32_t (*) (uint8_t*, int32_t, uint8_t*, int32_t, uint16_t*, int32_t*)
// (only SIMD implementations exist; kept for the table shape).
type PSampleSadHor8Func func(pSample1 []uint8, iSample1Off int, iStride1 int32, pSample2 []uint8, iSample2Off int, iStride2 int32,
	pBaseCost []uint16, pIndexMinPos *int32) uint32

type PMotionSearchFunc func(pFuncList *SWelsFuncPtrList, pCurDqLayer *SDqLayer, pMe *SWelsME,
	pSlice *SSlice)
type PSearchMethodFunc func(pFuncList *SWelsFuncPtrList, pMe *SWelsME, pSlice *SSlice, kiEncStride int32,
	kiRefStride int32)
type PCalculateSatdFunc func(pSatd PSampleSadSatdCostFunc, pMe *SWelsME, kiEncStride int32,
	kiRefStride int32)

// PCheckDirectionalMv: int32_t& iBestSadCost -> *int32.
type PCheckDirectionalMv func(pSad PSampleSadSatdCostFunc, pMe *SWelsME,
	ksMinMv SMVUnitXY, ksMaxMv SMVUnitXY, kiEncStride int32, kiRefStride int32,
	iBestSadCost *int32) bool

// PLineFullSearchFunc: uint16_t* pMvdTable is negatively indexed -> (slice, offset).
type PLineFullSearchFunc func(pFuncList *SWelsFuncPtrList, pMe *SWelsME,
	pMvdTable []uint16, iMvdTableOff int,
	kiEncStride int32, kiRefStride int32,
	kiMinMv int16, kiMaxMv int16,
	bVerticalSearch bool)

// PInitializeHashforFeatureFunc: uint16_t** -> [][]uint16 (entries become sub-slices of pBuf).
type PInitializeHashforFeatureFunc func(pTimesOfFeatureValue []uint32, pBuf []uint16, kiListSize int32,
	pLocationOfFeature [][]uint16, pFeatureValuePointerList [][]uint16)
type PFillQpelLocationByFeatureValueFunc func(pFeatureOfBlock []uint16, kiWidth int32,
	kiHeight int32,
	pFeatureValuePointerList [][]uint16)

type PCalculateBlockFeatureOfFrame func(pRef []uint8, iRefOff int, kiWidth int32, kiHeight int32,
	kiRefStride int32,
	pFeatureOfBlock []uint16, pTimesOfFeatureValue []uint32)
type PCalculateSingleBlockFeature func(pRef []uint8, iRefOff int, kiRefStride int32) int32
type PUpdateFMESwitch func(pCurLayer *SDqLayer)

const MAX_BLOCK_TYPE = BLOCK_SIZE_ALL

// SSampleDealingFunc is the SAD / SATD function table.
type SSampleDealingFunc struct {
	pfSampleSad              [MAX_BLOCK_TYPE]PSampleSadSatdCostFunc
	pfSampleSatd             [MAX_BLOCK_TYPE]PSampleSadSatdCostFunc
	pfSample4Sad             [MAX_BLOCK_TYPE]PSample4SadCostFunc
	pfSampleAvgSatd          sampleAvgSatdFunc
	pfSampleAvgSatdValidated sampleAvgSatdValidatedFunc
	// Disable this capability to instrument the original per-candidate
	// checked/validated callbacks. A direct helper keeps local params on stack.
	bSampleAvgSatdFour bool
	// Disable when replacing half-refinement SATD callbacks for instrumentation.
	bSampleHalfSatdPair bool
	// Disable when replacing SAD callbacks to retain per-candidate instrumentation.
	bSampleSadTwo bool

	pfIntra4x4Combined3Satd   PIntraPred4x4Combined3Func
	pfIntra16x16Combined3Satd PIntraPred16x16Combined3Func
	pfIntra16x16Combined3Sad  PIntraPred16x16Combined3Func
	pfIntra8x8Combined3Satd   PIntraPred8x8Combined3Func
	pfIntra8x8Combined3Sad    PIntraPred8x8Combined3Func

	// C PSampleSadSatdCostFunc*: points at pfSampleSad or pfSampleSatd.
	pfMdCost *[MAX_BLOCK_TYPE]PSampleSadSatdCostFunc
	pfMeCost *[MAX_BLOCK_TYPE]PSampleSadSatdCostFunc

	pfIntra16x16Combined3 PIntraPred16x16Combined3Func
	pfIntra8x8Combined3   PIntraPred8x8Combined3Func
	pfIntra4x4Combined3   PIntraPred4x4Combined3Func
}

// PGetIntraPredFunc: (pPrediction, pRef, kiStride); pRef points into the
// reconstructed picture and is indexed negatively.
type PGetIntraPredFunc func(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32)

type PGetVarianceFromIntraVaaFunc func(pSampelY []uint8, iSampelYOff int, kiStride int32) int32

// PGetMbSignFromInterVaaFunc: int32_t* pSad8x8 (4 entries) -> []int32.
type PGetMbSignFromInterVaaFunc func(pSad8x8 []int32) uint8

// PUpdateMbMvFunc: SMVUnitXY* pMvUnit (16 entries) -> []SMVUnitXY.
type PUpdateMbMvFunc func(pMvUnit []SMVUnitXY, ksMv SMVUnitXY)

type PBuildRefListFunc func(pCtx *sWelsEncCtx, iPOC int32, iBestLtrRefIdx int32) bool
type PMarkPicFunc func(pCtx *sWelsEncCtx)
type PUpdateRefListFunc func(pCtx *sWelsEncCtx) bool
type PEndofUpdateRefListFunc func(pCtx *sWelsEncCtx)
type PAfterBuildRefListFunc func(pCtx *sWelsEncCtx)

type PCavlcParamCalFunc func(pCoff []int16, pRun []uint8, pLevel []int16, pTotalCoeffs *int32,
	iEndIdx int32) int32
type PWelsSpatialWriteMbSyn func(pCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB) int32
type PStashMBStatus func(pDss *SDynamicSlicingStack, pSlice *SSlice, iMbSkipRun int32)
type PStashPopMBStatus func(pDss *SDynamicSlicingStack, pSlice *SSlice) int32
type PGetBsPosition func(pSlice *SSlice) int32

// SWelsFuncPtrList is the encoder function table (struct TagWelsFuncPointerList).
type SWelsFuncPtrList struct {
	sExpandPicFunc            common.SExpandPicFunc
	pfFillInterNeighborCache  PFillInterNeighborCacheFunc
	pfGetVarianceFromIntraVaa PGetVarianceFromIntraVaaFunc
	pfGetMbSignFromInterVaa   PGetMbSignFromInterVaaFunc
	pfUpdateMbMv              PUpdateMbMvFunc

	pfFirstIntraMode            PInterMdFirstIntraModeFunc // svc_encode_slice.c svc_mode_decision.c svc_base_layer_md.c
	pfIntraFineMd               PIntraFineMdFunc           // svc_encode_slice.c svc_mode_decision.c svc_base_layer_md.c
	pfInterFineMd               PInterFineMdFunc           // svc_encode_slice.c svc_base_layer_md.c
	pfInterMd                   PInterMdFunc
	pfInterMdBackgroundDecision PInterMdBackgroundDecisionFunc
	pfMdBackgroundInfoUpdate    PMdBackgroundInfoUpdateFunc

	pfSCDPSkipDecision PInterMdScrollingPSkipDecisionFunc
	pfSetScrollingMv   PSetScrollingMv

	sMcFuncs            common.SMcFunc
	sSampleDealingFuncs SSampleDealingFunc
	pfGetLumaI16x16Pred [common.I16_PRED_DC_A]PGetIntraPredFunc
	pfGetLumaI4x4Pred   [common.I4_PRED_A]PGetIntraPredFunc
	pfGetChromaPred     [common.C_PRED_A]PGetIntraPredFunc

	pfSampleSadHor8                  [2]PSampleSadHor8Func                              // 1: for 16x16 square; 0: for 8x8 square
	pfMotionSearch                   [processing.BLOCK_STATIC_IDC_ALL]PMotionSearchFunc // svc_encode_slice.c svc_mode_decision.c svc_enhance_layer_md.c svc_base_layer_md.c
	pfSearchMethod                   [BLOCK_SIZE_ALL]PSearchMethodFunc
	pfCalculateSatd                  PCalculateSatdFunc
	pfCheckDirectionalMv             PCheckDirectionalMv
	pfInitializeHashforFeature       PInitializeHashforFeatureFunc
	pfFillQpelLocationByFeatureValue PFillQpelLocationByFeatureValueFunc
	pfCalculateBlockFeatureOfFrame   [2]PCalculateBlockFeatureOfFrame // 0 - for 8x8, 1 for 16x16
	pfCalculateSingleBlockFeature    [2]PCalculateSingleBlockFeature  // 0 - for 8x8, 1 for 16x16
	pfVerticalFullSearch             PLineFullSearchFunc
	pfHorizontalFullSearch           PLineFullSearchFunc
	pfUpdateFMESwitch                PUpdateFMESwitch

	pfCopy16x16Aligned    PCopyFunc // svc_encode_slice.c svc_mode_decision.c svc_base_layer_md.c
	pfCopy16x16NotAligned PCopyFunc // md.c
	pfCopy8x8Aligned      PCopyFunc // svc_encode_slice.c svc_mode_decision.c svc_base_layer_md.c md.c
	pfCopy16x8NotAligned  PCopyFunc // for MeRefineFracPixel 16x8 based
	pfCopy8x16Aligned     PCopyFunc // for MeRefineFracPixel 8x16 based
	pfCopy4x4             PCopyFunc // not sure if aligned or not, need further tune
	pfCopy8x4             PCopyFunc // not sure if aligned or not, need further tune
	pfCopy4x8             PCopyFunc // not sure if aligned or not, need further tune
	pfDctT4               PDctFunc
	pfDctFourT4           PDctFunc

	pfCalculateSingleCtr4x4 PCalculateSingleCtrFunc
	pfScan4x4               PScanFunc // DC/AC
	pfScan4x4Ac             PScanFunc

	pfQuantization4x4             PQuantizationFunc
	pfQuantizationFour4x4         PQuantizationFunc
	pfQuantizationDc4x4           PQuantizationDcFunc
	pfQuantizationFour4x4Max      PQuantizationMaxFunc
	pfQuantizationHadamard2x2     PQuantizationHadamardFunc
	pfQuantizationHadamard2x2Skip PQuantizationSkipFunc

	pfTransformHadamard4x4Dc PTransformHadamard4x4Func

	pfGetNoneZeroCount PGetNoneZeroCountFunc

	pfDequantization4x4          PDeQuantizationFunc
	pfDequantizationFour4x4      PDeQuantizationFunc
	pfDequantizationIHadamard4x4 PDeQuantizationHadamardFunc
	pfIDctFourT4                 PIDctFunc
	pfIDctT4                     PIDctFunc
	pfIDctI16x16Dc               PIDctFunc

	/* For Deblocking */
	pfDeblocking DeblockingFunc
	pfSetNZCZero PSetNoneZeroCountZeroFunc

	pfRc                 SWelsRcFunc
	pfAccumulateSadForRc PAccumulateSadFunc

	pfSetMemZeroSize8           PSetMemoryZero // for size is times to 8
	pfSetMemZeroSize64Aligned16 PSetMemoryZero // for size is times of 64, and address is align to 16
	pfSetMemZeroSize64          PSetMemoryZero // for size is times of 64, and don't know address is align to 16 or not

	pfCavlcParamCal         PCavlcParamCalFunc
	pfWelsSpatialWriteMbSyn PWelsSpatialWriteMbSyn
	pfGetBsPosition         PGetBsPosition
	pfStashMBStatus         PStashMBStatus
	pfStashPopMBStatus      PStashPopMBStatus

	pParametersetStrategy IWelsParametersetStrategy
}
