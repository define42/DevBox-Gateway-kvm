// Port of codec/encoder/core/src/encoder.cpp.
//
// Core encoder.

package encoder

import (
	"fmt"
	"io"
	"os"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/processing"
)

// InitPic initializes source picture body.
// kpSrc: const void* that is always an SSourcePicture.
// Returns successful - 0; otherwise none 0 for failed.
func InitPic(kpSrc *api.SSourcePicture, kiColorspace int32, kiWidth int32, kiHeight int32) int32 {
	pSrcPic := kpSrc

	if nil == pSrcPic || kiWidth == 0 || kiHeight == 0 {
		return 1
	}

	kiVFlip := int32(api.VideoFormatVFlip)

	pSrcPic.IColorFormat = kiColorspace
	pSrcPic.IPicWidth = kiWidth
	pSrcPic.IPicHeight = kiHeight

	//currently encoder only supports videoFormatI420.
	if (kiColorspace &^ kiVFlip) != int32(api.VideoFormatI420) {
		return 2
	}
	switch api.EVideoFormatType(kiColorspace &^ kiVFlip) {
	case api.VideoFormatI420, api.VideoFormatYV12:
		pSrcPic.PData[0] = nil
		pSrcPic.PData[1] = nil
		pSrcPic.PData[2] = nil
		pSrcPic.PData[3] = nil
		pSrcPic.IStride[0] = kiWidth
		pSrcPic.IStride[1] = kiWidth >> 1
		pSrcPic.IStride[2] = pSrcPic.IStride[1]
		pSrcPic.IStride[3] = 0
	case api.VideoFormatYUY2, api.VideoFormatYVYU, api.VideoFormatUYVY:
		pSrcPic.PData[0] = nil
		pSrcPic.PData[1] = nil
		pSrcPic.PData[2] = nil
		pSrcPic.PData[3] = nil
		pSrcPic.IStride[0] = common.CALC_BI_STRIDE(kiWidth, 16)
		pSrcPic.IStride[1] = 0
		pSrcPic.IStride[2] = 0
		pSrcPic.IStride[3] = 0
	case api.VideoFormatRGB, api.VideoFormatBGR:
		pSrcPic.PData[0] = nil
		pSrcPic.PData[1] = nil
		pSrcPic.PData[2] = nil
		pSrcPic.PData[3] = nil
		pSrcPic.IStride[0] = common.CALC_BI_STRIDE(kiWidth, 24)
		pSrcPic.IStride[1] = 0
		pSrcPic.IStride[2] = 0
		pSrcPic.IStride[3] = 0
		if kiColorspace&kiVFlip != 0 {
			pSrcPic.IColorFormat = kiColorspace &^ kiVFlip
		} else {
			pSrcPic.IColorFormat = kiColorspace | kiVFlip
		}
	case api.VideoFormatBGRA, api.VideoFormatRGBA, api.VideoFormatARGB, api.VideoFormatABGR:
		pSrcPic.PData[0] = nil
		pSrcPic.PData[1] = nil
		pSrcPic.PData[2] = nil
		pSrcPic.PData[3] = nil
		pSrcPic.IStride[0] = kiWidth << 2
		pSrcPic.IStride[1] = 0
		pSrcPic.IStride[2] = 0
		pSrcPic.IStride[3] = 0
		if kiColorspace&kiVFlip != 0 {
			pSrcPic.IColorFormat = kiColorspace &^ kiVFlip
		} else {
			pSrcPic.IColorFormat = kiColorspace | kiVFlip
		}
	default:
		return 2 // any else?
	}

	return 0
}

func WelsInitBGDFunc(pFuncList *SWelsFuncPtrList, kbEnableBackgroundDetection bool) {
	if kbEnableBackgroundDetection {
		pFuncList.pfInterMdBackgroundDecision = WelsMdInterJudgeBGDPskip
		pFuncList.pfMdBackgroundInfoUpdate = WelsMdUpdateBGDInfo
	} else {
		pFuncList.pfInterMdBackgroundDecision = WelsMdInterJudgeBGDPskipFalse
		pFuncList.pfMdBackgroundInfoUpdate = WelsMdUpdateBGDInfoNULL
	}
}

func encoderBoolToInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// InitFunctionPointers initializes function pointers that potentially used
// in Wels encoding. Returns successful - 0; otherwise none 0 for failed.
func InitFunctionPointers(pEncCtx *sWelsEncCtx, pParam *SWelsSvcCodingParam, uiCpuFlag uint32) int32 {
	var iReturn int32 = ENC_RETURN_SUCCESS
	pFuncList := pEncCtx.pFuncList
	bScreenContent := (api.SCREEN_CONTENT_REAL_TIME == pParam.IUsageType)

	/* Functionality utilization of CPU instructions dependency */
	pFuncList.pfSetMemZeroSize8 = WelsSetMemZero_c           // confirmed_safe_unsafe_usage
	pFuncList.pfSetMemZeroSize64Aligned16 = WelsSetMemZero_c // confirmed_safe_unsafe_usage
	pFuncList.pfSetMemZeroSize64 = WelsSetMemZero_c          // confirmed_safe_unsafe_usage

	common.InitExpandPictureFunc(&pFuncList.sExpandPicFunc, uiCpuFlag)

	/* Intra_Prediction_fn*/
	WelsInitIntraPredFuncs(pFuncList, uiCpuFlag)

	/* ME func */
	WelsInitMeFunc(pFuncList, uiCpuFlag, bScreenContent)

	/* sad, satd, average */
	WelsInitSampleSadFunc(pFuncList, uiCpuFlag)

	//
	WelsInitBGDFunc(pFuncList, pParam.BEnableBackgroundDetection)
	WelsInitSCDPskipFunc(pFuncList, bScreenContent &&
		(pParam.BEnableSceneChangeDetect) &&
		(pEncCtx.pSvcParam.IComplexityMode < api.HIGH_COMPLEXITY))

	// for pfGetVarianceFromIntraVaa function ptr adaptive by CPU features, 6/7/2010
	InitIntraAnalysisVaaInfo(pFuncList, uiCpuFlag)

	/* Motion compensation */
	/*init pixel average function*/
	/*get one column or row pixel when refinement*/
	common.InitMcFunc(&pFuncList.sMcFuncs, uiCpuFlag)
	InitCoeffFunc(pFuncList, uiCpuFlag, pParam.IEntropyCodingModeFlag)

	WelsInitEncodingFuncs(pFuncList, uiCpuFlag)
	WelsInitReconstructionFuncs(pFuncList, uiCpuFlag)

	DeblockingInit(&pFuncList.pfDeblocking, int32(uiCpuFlag))
	WelsBlockFuncInit(&pFuncList.pfSetNZCZero, int32(uiCpuFlag))

	InitFillNeighborCacheInterFunc(pFuncList, encoderBoolToInt32(pParam.BEnableBackgroundDetection))

	pFuncList.pParametersetStrategy = CreateParametersetStrategy(pParam.ESpsPpsIdStrategy,
		pParam.BSimulcastAVC, pParam.ISpatialLayerNum)
	if nil == pFuncList.pParametersetStrategy {
		return ENC_RETURN_MEMALLOCERR
	}

	return iReturn
}

func UpdateFrameNum(pEncCtx *sWelsEncCtx, kiDidx int32) {
	pParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[kiDidx]
	bNeedFrameNumIncreasing := false

	if common.NRI_PRI_LOWEST != pEncCtx.eLastNalPriority[kiDidx] {
		bNeedFrameNumIncreasing = true
	}

	if bNeedFrameNumIncreasing {
		if pParamInternal.iFrameNum < (int32(1)<<pEncCtx.pSps.uiLog2MaxFrameNum)-1 {
			pParamInternal.iFrameNum++
		} else {
			pParamInternal.iFrameNum = 0 // if iFrameNum overflow
		}
	}

	pEncCtx.eLastNalPriority[kiDidx] = common.NRI_PRI_LOWEST
}

func LoadBackFrameNum(pEncCtx *sWelsEncCtx, kiDidx int32) {
	pParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[kiDidx]
	bNeedFrameNumIncreasing := false

	if common.NRI_PRI_LOWEST != pEncCtx.eLastNalPriority[kiDidx] {
		bNeedFrameNumIncreasing = true
	}

	if bNeedFrameNumIncreasing {
		if pParamInternal.iFrameNum != 0 {
			pParamInternal.iFrameNum--
		} else {
			pParamInternal.iFrameNum = (int32(1) << pEncCtx.pSps.uiLog2MaxFrameNum) - 1
		}
	}
}

func InitBitStream(pEncCtx *sWelsEncCtx) {
	// for bitstream writing
	pEncCtx.iPosBsBuffer = 0       // reset bs pBuffer position
	pEncCtx.pOut.iNalIndex = 0     // reset NAL index
	pEncCtx.pOut.iLayerBsIndex = 0 // reset index of Layer Bs

	common.InitBits(&pEncCtx.pOut.sBsWrite, pEncCtx.pOut.pBsBuffer, 0, int32(pEncCtx.pOut.uiSize))
}

// InitFrameCoding initializes frame coding.
func InitFrameCoding(pEncCtx *sWelsEncCtx, keFrameType api.EVideoFrameType, kiDidx int32) {
	pParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[kiDidx]
	if keFrameType == api.VideoFrameTypeP {
		pParamInternal.iFrameIndex++

		if pParamInternal.iPOC < (int32(1)<<pEncCtx.pSps.iLog2MaxPocLsb)-2 { // if iPOC type is no 0, this need be modification
			pParamInternal.iPOC += 2 // for POC type 0
		} else {
			pParamInternal.iPOC = 0
		}

		UpdateFrameNum(pEncCtx, kiDidx)

		pEncCtx.eNalType = common.NAL_UNIT_CODED_SLICE
		pEncCtx.eSliceType = common.P_SLICE
		pEncCtx.eNalPriority = common.NRI_PRI_HIGH
	} else if keFrameType == api.VideoFrameTypeIDR {
		pParamInternal.iFrameNum = 0
		pParamInternal.iPOC = 0
		pParamInternal.bEncCurFrmAsIdrFlag = false
		pParamInternal.iFrameIndex = 0

		pEncCtx.eNalType = common.NAL_UNIT_CODED_SLICE_IDR
		pEncCtx.eSliceType = common.I_SLICE
		pEncCtx.eNalPriority = common.NRI_PRI_HIGHEST

		pParamInternal.iCodingIndex = 0

		// reset_ref_list

		// rc_init_gop
	} else if keFrameType == api.VideoFrameTypeI {
		if pParamInternal.iPOC < (int32(1)<<pEncCtx.pSps.iLog2MaxPocLsb)-2 { // if iPOC type is no 0, this need be modification
			pParamInternal.iPOC += 2 // for POC type 0
		} else {
			pParamInternal.iPOC = 0
		}

		UpdateFrameNum(pEncCtx, kiDidx)

		pEncCtx.eNalType = common.NAL_UNIT_CODED_SLICE
		pEncCtx.eSliceType = common.I_SLICE
		pEncCtx.eNalPriority = common.NRI_PRI_HIGHEST

		// rc_init_gop
	} else { // B pictures are not supported now, any else?
		// assert (0);
	}
}

func DecideFrameType(pEncCtx *sWelsEncCtx, kiSpatialNum int8, kiDidx int32, bSkipFrameFlag bool) api.EVideoFrameType {
	pSvcParam := pEncCtx.pSvcParam
	pParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[kiDidx]
	iFrameType := api.VideoFrameTypeInvalid
	bSceneChangeFlag := false
	if pSvcParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		if (!pSvcParam.BEnableSceneChangeDetect) || pEncCtx.pVaa.bIdrPeriodFlag ||
			(int32(kiSpatialNum) < pSvcParam.ISpatialLayerNum) {
			bSceneChangeFlag = false
		} else {
			bSceneChangeFlag = pEncCtx.pVaa.bSceneChangeFlag
		}
		if pEncCtx.pVaa.bIdrPeriodFlag || pParamInternal.bEncCurFrmAsIdrFlag || (!pSvcParam.BEnableLongTermReference &&
			bSceneChangeFlag && !bSkipFrameFlag) {
			iFrameType = api.VideoFrameTypeIDR
		} else if pSvcParam.BEnableLongTermReference && (bSceneChangeFlag ||
			pEncCtx.pVaa.eSceneChangeIdc == processing.LARGE_CHANGED_SCENE) {
			iActualLtrcount := int32(0)
			pLongTermRefList := &pEncCtx.ppRefPicListExt[0].pLongRefList
			for i := int32(0); i < pSvcParam.ILTRRefNum; i++ {
				if nil != pLongTermRefList[i] && pLongTermRefList[i].bUsedAsRef && pLongTermRefList[i].bIsLongRef &&
					pLongTermRefList[i].bIsSceneLTR {
					iActualLtrcount++
				}
			}
			if iActualLtrcount == pSvcParam.ILTRRefNum && bSceneChangeFlag {
				iFrameType = api.VideoFrameTypeIDR
			} else {
				iFrameType = api.VideoFrameTypeP
				pEncCtx.bCurFrameMarkedAsSceneLtr = true
			}
		} else {
			iFrameType = api.VideoFrameTypeP
		}
		if api.VideoFrameTypeP == iFrameType && bSkipFrameFlag {
			iFrameType = api.VideoFrameTypeSkip
		} else if api.VideoFrameTypeIDR == iFrameType {
			pParamInternal.iCodingIndex = 0
			pEncCtx.bCurFrameMarkedAsSceneLtr = true
		}

	} else {
		// perform scene change detection
		if (!pSvcParam.BEnableSceneChangeDetect) || pEncCtx.pVaa.bIdrPeriodFlag ||
			(int32(kiSpatialNum) < pSvcParam.ISpatialLayerNum) ||
			(pParamInternal.iFrameIndex < (VGOP_SIZE << 1)) { // avoid too frequent I frame coding, rc control
			bSceneChangeFlag = false
		} else {
			bSceneChangeFlag = pEncCtx.pVaa.bSceneChangeFlag
		}

		//scene_changed_flag: RC enable && iSpatialNum == pSvcParam->iSpatialLayerNum
		//bIdrPeriodFlag: RC disable || iSpatialNum != pSvcParam->iSpatialLayerNum
		//pEncCtx->bEncCurFrmAsIdrFlag: 1. first frame should be IDR; 2. idr pause; 3. idr request
		if pEncCtx.pVaa.bIdrPeriodFlag || bSceneChangeFlag || pParamInternal.bEncCurFrmAsIdrFlag {
			iFrameType = api.VideoFrameTypeIDR
		} else {
			iFrameType = api.VideoFrameTypeP
		}
		if api.VideoFrameTypeIDR == iFrameType {
			common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
				"encoding videoFrameTypeIDR due to ( bIdrPeriodFlag %d, bSceneChangeFlag %d, bEncCurFrmAsIdrFlag %d )",
				encoderBoolToInt32(pEncCtx.pVaa.bIdrPeriodFlag),
				encoderBoolToInt32(bSceneChangeFlag),
				encoderBoolToInt32(pParamInternal.bEncCurFrmAsIdrFlag))
		}

		if api.VideoFrameTypeP == iFrameType && bSkipFrameFlag { // for frame skip, 1/5/2010
			iFrameType = api.VideoFrameTypeSkip
		} else if api.VideoFrameTypeIDR == iFrameType {
			pParamInternal.iCodingIndex = 0
		}
	}
	return iFrameType
}

// dumpRecPlanes writes the (cropped) planes of pCurPicture to pDumpRecFile.
func dumpRecPlanes(pDumpRecFile *os.File, pCurPicture *SPicture, bFrameCroppingFlag bool, pFrameCrop *SCropOffset) {
	kiStrideY := pCurPicture.iLineSize[0]
	kiLumaWidth := pCurPicture.iWidthInPixel
	kiLumaHeight := pCurPicture.iHeightInPixel
	if bFrameCroppingFlag {
		kiLumaWidth = pCurPicture.iWidthInPixel - ((int32(pFrameCrop.iCropLeft) + int32(pFrameCrop.iCropRight)) << 1)
		kiLumaHeight = pCurPicture.iHeightInPixel - ((int32(pFrameCrop.iCropTop) + int32(pFrameCrop.iCropBottom)) << 1)
	}
	kiChromaWidth := kiLumaWidth >> 1
	kiChromaHeight := kiLumaHeight >> 1
	pSrc := pCurPicture.pData[0]
	iSrcOff := pCurPicture.iDataOff[0]
	if bFrameCroppingFlag {
		iSrcOff += int(kiStrideY*(int32(pFrameCrop.iCropTop)<<1) + (int32(pFrameCrop.iCropLeft) << 1))
	}
	for j := int32(0); j < kiLumaHeight; j++ {
		iOff := iSrcOff + int(j*kiStrideY)
		iWrittenSize, _ := pDumpRecFile.Write(pSrc[iOff : iOff+int(kiLumaWidth)])
		if int32(iWrittenSize) < kiLumaWidth {
			pDumpRecFile.Close()
			return
		}
	}
	for i := 1; i < I420_PLANES; i++ {
		kiStrideUV := pCurPicture.iLineSize[i]
		pSrc = pCurPicture.pData[i]
		iSrcOff = pCurPicture.iDataOff[i]
		if bFrameCroppingFlag {
			iSrcOff += int(kiStrideUV*int32(pFrameCrop.iCropTop) + int32(pFrameCrop.iCropLeft))
		}
		for j := int32(0); j < kiChromaHeight; j++ {
			iOff := iSrcOff + int(j*kiStrideUV)
			iWrittenSize, _ := pDumpRecFile.Write(pSrc[iOff : iOff+int(kiChromaWidth)])
			if int32(iWrittenSize) < kiChromaWidth {
				pDumpRecFile.Close()
				return
			}
		}
	}
	pDumpRecFile.Close()
}

func dumpRecOpen(kpFileName string, bAppend bool) *os.File {
	iFlag := os.O_WRONLY | os.O_CREATE
	if bAppend {
		iFlag |= os.O_APPEND
	} else {
		iFlag |= os.O_TRUNC
	}
	f, err := os.OpenFile(kpFileName, iFlag, 0o644)
	if err != nil {
		return nil
	}
	if bAppend {
		f.Seek(0, io.SeekEnd)
	}
	return f
}

// DumpDependencyRec dumps reconstruction for dependency layer.
// kpFileName: const char* -> string.
func DumpDependencyRec(pCurPicture *SPicture, kpFileName string, kiDid int8, bAppend bool, pDqLayer *SDqLayer, bSimulCastAVC bool) {
	var pSpsTmp *SWelsSPS
	if bSimulCastAVC || (kiDid == BASE_DEPENDENCY_ID) {
		pSpsTmp = pDqLayer.sLayerInfo.pSpsP
	} else {
		pSpsTmp = &pDqLayer.sLayerInfo.pSubsetSpsP.pSps
	}
	bFrameCroppingFlag := pSpsTmp.bFrameCroppingFlag
	pFrameCrop := &pSpsTmp.sFrameCrop

	if nil == pCurPicture || int32(kiDid) >= MAX_DEPENDENCY_LAYER {
		return
	}
	var pDumpRecFile *os.File
	if len(kpFileName) > 0 { // confirmed_safe_unsafe_usage
		pDumpRecFile = dumpRecOpen(kpFileName, bAppend)
	} else {
		sDependencyRecFileName := fmt.Sprintf("rec%d.yuv", kiDid) // confirmed_safe_unsafe_usage
		pDumpRecFile = dumpRecOpen(sDependencyRecFileName, bAppend)
	}

	if nil != pDumpRecFile {
		dumpRecPlanes(pDumpRecFile, pCurPicture, bFrameCroppingFlag, pFrameCrop)
	}
}

// DumpRecFrame dumps the reconstruction pictures.
func DumpRecFrame(pCurPicture *SPicture, kpFileName string, kiDid int8, bAppend bool, pDqLayer *SDqLayer) {
	var pSpsTmp *SWelsSPS
	if kiDid > BASE_DEPENDENCY_ID {
		pSpsTmp = &pDqLayer.sLayerInfo.pSubsetSpsP.pSps
	} else {
		pSpsTmp = pDqLayer.sLayerInfo.pSpsP
	}
	bFrameCroppingFlag := pSpsTmp.bFrameCroppingFlag
	pFrameCrop := &pSpsTmp.sFrameCrop

	if nil == pCurPicture {
		return
	}

	var pDumpRecFile *os.File
	if len(kpFileName) > 0 { // confirmed_safe_unsafe_usage
		pDumpRecFile = dumpRecOpen(kpFileName, bAppend)
	} else {
		pDumpRecFile = dumpRecOpen("rec.yuv", bAppend)
	}

	if nil != pDumpRecFile {
		dumpRecPlanes(pDumpRecFile, pCurPicture, bFrameCroppingFlag, pFrameCrop)
	}
}

/***********************************************************************************/

// WelsSetMemZero_c is a PSetMemoryZero: iSize is in bytes (iSize/2 int16 values are cleared).
func WelsSetMemZero_c(pDst []int16, iSize int32) { // confirmed_safe_unsafe_usage
	clear(pDst[:iSize/2])
}
