// Port of codec/encoder/core/src/wels_preprocess.cpp.
//
// SPixMap planes handed to the processing library (IWelsVP) are filled with
// the whole picture allocation (SPicture.pData[i]) plus the plane origin
// offset (SPicture.iDataOff[i]) in SPixMap.IPixelOff[i], so that the
// processing modules can reach into the picture padding exactly as in C.

package encoder

import (
	"math"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/processing"
)

// pData: picture plane -> (slice, offset).
func ClearEndOfLinePadding(pData []uint8, iDataOff int, iStride int32, iWidth int32, iHeight int32) {
	if iWidth < iStride {
		for i := int32(0); i < iHeight; i++ {
			iOff := iDataOff + int(i*iStride+iWidth)
			clear(pData[iOff : iOff+int(iStride-iWidth)])
		}
	}
}

// ******* table definition ***********************************************************************//
var g_kuiRefTemporalIdx = [MAX_TEMPORAL_LEVEL][MAX_GOP_SIZE]uint8{
	{0},                      // 0
	{0, 0},                   // 1
	{0, 0, 0, 1},             // 2
	{0, 0, 0, 2, 0, 1, 1, 2}, // 3
}

const g_kiPixMapSizeInBits = 1 * 8 // sizeof (uint8_t) * 8

// (inline in C)
func WelsUpdateSpatialIdxMap(pEncCtx *sWelsEncCtx, iPos int32, pPic *SPicture, iDidx int32) {
	pEncCtx.sSpatialIndexMap[iPos].pSrc = pPic
	pEncCtx.sSpatialIndexMap[iPos].iDid = iDidx
}

// vppSetPixMapPlane fills plane i of an SPixMap from plane j of a picture
// (C: pPixMap->pPixel[i] = pPic->pData[j]).
func vppSetPixMapPlane(pPixMap *processing.SPixMap, i int, pPic *SPicture, j int) {
	pPixMap.PPixel[i] = pPic.pData[j]
	pPixMap.IPixelOff[i] = pPic.iDataOff[j]
}

// vppSamePlaneOrigin is the C pointer comparison pPic1->pData[i] == pPic2->pData[i].
func vppSamePlaneOrigin(pPic1 *SPicture, pPic2 *SPicture, i int) bool {
	a, b := pPic1.pData[i], pPic2.pData[i]
	if a == nil || b == nil {
		return a == nil && b == nil && pPic1.iDataOff[i] == pPic2.iDataOff[i]
	}
	if pPic1.iDataOff[i] != pPic2.iDataOff[i] || len(a) == 0 || len(b) == 0 {
		return len(a) == 0 && len(b) == 0 && pPic1.iDataOff[i] == pPic2.iDataOff[i]
	}
	return &a[0] == &b[0]
}

/***************************************************************************
*
*   implement of the interface
*
***************************************************************************/

// static CWelsPreProcess::CreatePreProcess: returns the base part of a
// NewCWelsPreProcessVideo / NewCWelsPreProcessScreen object.
func CreatePreProcess(pEncCtx *sWelsEncCtx) *CWelsPreProcess {
	var pPreProcess *CWelsPreProcess
	switch pEncCtx.pSvcParam.IUsageType {
	case api.SCREEN_CONTENT_REAL_TIME:
		pPreProcess = &NewCWelsPreProcessScreen(pEncCtx).CWelsPreProcess
	default:
		pPreProcess = &NewCWelsPreProcessVideo(pEncCtx).CWelsPreProcess
	}
	return pPreProcess
}

// constructor body CWelsPreProcess::CWelsPreProcess
func (p *CWelsPreProcess) ctorCWelsPreProcess(pEncCtx *sWelsEncCtx) {
	p.m_pInterfaceVp = nil
	p.m_bInitDone = false
	p.m_pEncCtx = pEncCtx
	p.m_sScaledPicture = Scaled_Picture{}
	p.m_pSpatialPic = [MAX_DEPENDENCY_LAYER][MAX_REF_PIC_COUNT + 1]*SPicture{}
	p.m_uiSpatialLayersInTemporal = [MAX_DEPENDENCY_LAYER]uint8{}
	p.m_uiSpatialPicNum = [MAX_DEPENDENCY_LAYER]uint8{}
}

// destructor CWelsPreProcess::~CWelsPreProcess
func (p *CWelsPreProcess) Destruct() {
	FreeScaledPic(&p.m_sScaledPicture)
	p.WelsPreprocessDestroy()
}

func (p *CWelsPreProcess) WelsPreprocessCreate() int32 {
	if p.m_pInterfaceVp == nil {
		processing.WelsCreateVpInterface(&p.m_pInterfaceVp, processing.WELSVP_INTERFACE_VERION)
		if p.m_pInterfaceVp == nil {
			p.WelsPreprocessDestroy()
			return 1
		}
	} else {
		p.WelsPreprocessDestroy()
		return 1
	}

	return 0
}

func (p *CWelsPreProcess) WelsPreprocessDestroy() int32 {
	p.sourceCopy.invalidate()
	processing.WelsDestroyVpInterface(p.m_pInterfaceVp, processing.WELSVP_INTERFACE_VERION)
	p.m_pInterfaceVp = nil

	return 0
}

func (p *CWelsPreProcess) WelsPreprocessReset(pCtx *sWelsEncCtx, iWidth int32, iHeight int32) int32 {
	p.sourceCopy.invalidate()
	iRet := int32(-1)
	pSvcParam := pCtx.pSvcParam
	//init source width and height
	pSvcParam.SUsedPicRect.iLeft = 0
	pSvcParam.SUsedPicRect.iTop = 0
	pSvcParam.SUsedPicRect.iWidth = iWidth
	pSvcParam.SUsedPicRect.iHeight = iHeight
	if (iWidth < 16) || (iHeight < 16) {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR, "Don't support width(%d) or height(%d) which is less than 16 ", iWidth,
			iHeight)
		return iRet
	}
	if pCtx != nil {
		FreeScaledPic(&p.m_sScaledPicture)
		iRet = p.InitLastSpatialPictures(pCtx)
		iRet = WelsInitScaledPic(pCtx.pSvcParam, &p.m_sScaledPicture)
	}

	return iRet
}

func (p *CWelsPreProcess) AllocSpatialPictures(pCtx *sWelsEncCtx, pParam *SWelsSvcCodingParam) int32 {
	kiDlayerCount := pParam.ISpatialLayerNum
	var iDlayerIndex int32

	// spatial pictures
	iDlayerIndex = 0
	for {
		kiPicWidth := pParam.SSpatialLayers[iDlayerIndex].IVideoWidth
		kiPicHeight := pParam.SSpatialLayers[iDlayerIndex].IVideoHeight
		kuiLayerInTemporal := uint8(2 + common.WELS_MAX(int32(pParam.sDependencyLayers[iDlayerIndex].iHighestTemporalId), 1))
		kuiRefNumInTemporal := uint8(int32(kuiLayerInTemporal) + pParam.ILTRRefNum)
		i := uint8(0)

		p.m_uiSpatialPicNum[iDlayerIndex] = kuiRefNumInTemporal
		for {
			pPic := AllocPicture(kiPicWidth, kiPicHeight, false, 0)
			if pPic == nil {
				return 1
			}
			p.m_pSpatialPic[iDlayerIndex][i] = pPic
			i++
			if !(i < kuiRefNumInTemporal) {
				break
			}
		}

		if pParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
			p.m_uiSpatialLayersInTemporal[iDlayerIndex] = 1
		} else {
			p.m_uiSpatialLayersInTemporal[iDlayerIndex] = kuiLayerInTemporal
		}

		iDlayerIndex++
		if !(iDlayerIndex < kiDlayerCount) {
			break
		}
	}

	return 0
}

func (p *CWelsPreProcess) FreeSpatialPictures(pCtx *sWelsEncCtx) {
	j := int32(0)
	for j < pCtx.pSvcParam.ISpatialLayerNum {
		i := uint8(0)
		uiRefNumInTemporal := p.m_uiSpatialPicNum[j]

		for i < uiRefNumInTemporal {
			if nil != p.m_pSpatialPic[j][i] {
				FreePicture(&p.m_pSpatialPic[j][i])
			}
			i++
		}
		p.m_uiSpatialLayersInTemporal[j] = 0
		j++
	}
}

func (p *CWelsPreProcess) BuildSpatialPicList(pCtx *sWelsEncCtx, kpSrcPic *api.SSourcePicture, pSpatialNum *int32) int32 {
	pSvcParam := pCtx.pSvcParam
	iWidth := ((kpSrcPic.IPicWidth >> 1) << 1)
	iHeight := ((kpSrcPic.IPicHeight >> 1) << 1)
	*pSpatialNum = 0

	if !p.m_bInitDone {
		if p.WelsPreprocessCreate() != 0 {
			return ENC_RETURN_MEMALLOCERR
		}

		if p.WelsPreprocessReset(pCtx, iWidth, iHeight) != 0 {
			return ENC_RETURN_MEMALLOCERR
		}

		p.m_iAvaliableRefInSpatialPicList = pSvcParam.INumRefFrame

		p.m_bInitDone = true
	} else {
		if (iWidth != pSvcParam.SUsedPicRect.iWidth) || (iHeight != pSvcParam.SUsedPicRect.iHeight) {
			if p.WelsPreprocessReset(pCtx, iWidth, iHeight) != 0 {
				return ENC_RETURN_MEMALLOCERR
			}
		}
	}

	if p.m_pInterfaceVp == nil {
		return ENC_RETURN_MEMALLOCERR
	}

	pCtx.pVaa.bIdrPeriodFlag = false
	pCtx.pVaa.bSceneChangeFlag = false

	iRet := p.SingleLayerPreprocess(pCtx, kpSrcPic, &p.m_sScaledPicture, pSpatialNum)
	if iRet != ENC_RETURN_SUCCESS {
		return iRet
	}

	return ENC_RETURN_SUCCESS
}

func (p *CWelsPreProcess) GetBestRefPic(iUsageType api.EUsageType, bSceneLtr bool, eSliceType common.EWelsSliceType, kiDidx int32, iRefTemporalIdx int32) *SPicture {
	// assert (iUsageType == SCREEN_CONTENT_REAL_TIME);
	pVaaExt := p.m_pEncCtx.pVaa.pExt
	var BestRefCandidateParam *SRefInfoParam
	if bSceneLtr {
		BestRefCandidateParam = &pVaaExt.sVaaLtrBestRefCandidate[0]
	} else {
		BestRefCandidateParam = &pVaaExt.sVaaStrBestRefCandidate[0]
	}
	return p.m_pSpatialPic[0][BestRefCandidateParam.iSrcListIdx]
}

// C++ overload CWelsPreProcess::GetBestRefPic (const int32_t kiDidx, const int32_t iRefTemporalIdx).
func (p *CWelsPreProcess) GetBestRefPic2(kiDidx int32, iRefTemporalIdx int32) *SPicture {
	return p.m_pSpatialPic[kiDidx][iRefTemporalIdx]
}

func (p *CWelsPreProcess) AnalyzeSpatialPic(pCtx *sWelsEncCtx, kiDidx int32) int32 {
	pSvcParam := pCtx.pSvcParam
	bNeededMbAq := (pSvcParam.BEnableAdaptiveQuant && (pCtx.eSliceType == common.P_SLICE))
	bCalculateBGD := (pCtx.eSliceType == common.P_SLICE && pSvcParam.BEnableBackgroundDetection)
	pParamInternal := &pSvcParam.sDependencyLayers[kiDidx]
	iCurTemporalIdx := int32(p.m_uiSpatialLayersInTemporal[kiDidx]) - 1

	iRefTemporalIdx := int32(g_kuiRefTemporalIdx[pSvcParam.iDecompStages][uint32(pParamInternal.iCodingIndex)&
		(pSvcParam.uiGopSize-1)])
	if pCtx.uiTemporalId == 0 && pCtx.pLtr[pCtx.uiDependencyId].bReceivedT0LostFlag {
		iRefTemporalIdx = int32(p.m_uiSpatialLayersInTemporal[kiDidx]) + int32(pCtx.pVaa.uiValidLongTermPicIdx)
	}

	pCurPic := p.m_pSpatialPic[kiDidx][iCurTemporalIdx]
	if kiDidx == 0 && p.vaaUnchanged.enabled {
		p.vaaUnchanged.current = pCurPic
	}
	bCalculateVar := (pSvcParam.IRCMode >= api.RC_BITRATE_MODE && pCtx.eSliceType == common.I_SLICE)

	if pSvcParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		pRefPic := p.GetBestRefPic(pSvcParam.IUsageType, pCtx.bCurFrameMarkedAsSceneLtr, pCtx.eSliceType, kiDidx,
			iRefTemporalIdx)

		p.VaaCalculation(pCtx.pVaa, pCurPic, pRefPic, false, bCalculateVar, bCalculateBGD)

		if pSvcParam.BEnableBackgroundDetection {
			p.BackgroundDetection(pCtx.pVaa, pCurPic, pRefPic, bCalculateBGD && pRefPic.iPictureType != int32(common.I_SLICE))
		}
		if bNeededMbAq {
			p.AdaptiveQuantCalculation(pCtx.pVaa, pCurPic, pRefPic)
		}
	} else {
		pRefPic := p.GetBestRefPic2(kiDidx, iRefTemporalIdx)
		pLastPic := p.m_pLastSpatialPicture[kiDidx][0]
		bCalculateSQDiff := (vppSamePlaneOrigin(pLastPic, pRefPic, 0) && bNeededMbAq)

		p.VaaCalculation(pCtx.pVaa, pCurPic, pRefPic, bCalculateSQDiff, bCalculateVar, bCalculateBGD)

		if pSvcParam.BEnableBackgroundDetection {
			p.BackgroundDetection(pCtx.pVaa, pCurPic, pRefPic, bCalculateBGD && pRefPic.iPictureType != int32(common.I_SLICE))
		}

		if bNeededMbAq {
			p.AdaptiveQuantCalculation(pCtx.pVaa, p.m_pLastSpatialPicture[kiDidx][1], p.m_pLastSpatialPicture[kiDidx][0])
		}
	}
	return 0
}

func (p *CWelsPreProcess) GetCurPicPosition(kiDidx int32) int32 {
	return (int32(p.m_uiSpatialLayersInTemporal[kiDidx]) - 1)
}

func (p *CWelsPreProcess) UpdateSpatialPictures(pCtx *sWelsEncCtx, pParam *SWelsSvcCodingParam, iCurTid int8, kiDidx int32) int32 {
	if pCtx.pSvcParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		return 0
	}

	p.WelsExchangeSpatialPictures(&p.m_pLastSpatialPicture[kiDidx][1], &p.m_pLastSpatialPicture[kiDidx][0])

	kiCurPos := p.GetCurPicPosition(kiDidx)
	if int32(iCurTid) < kiCurPos || pParam.iDecompStages == 0 {
		if (int32(iCurTid) >= MAX_TEMPORAL_LEVEL) || (kiCurPos > MAX_TEMPORAL_LEVEL) {
			p.InitLastSpatialPictures(pCtx)
			return 1
		}
		if pCtx.bRefOfCurTidIsLtr[kiDidx][iCurTid] {
			kiAvailableLtrPos := int32(p.m_uiSpatialLayersInTemporal[kiDidx]) + int32(pCtx.pVaa.uiMarkLongTermPicIdx)
			p.WelsExchangeSpatialPictures(&p.m_pSpatialPic[kiDidx][kiAvailableLtrPos],
				&p.m_pSpatialPic[kiDidx][iCurTid])
			pCtx.bRefOfCurTidIsLtr[kiDidx][iCurTid] = false
		}
		p.WelsExchangeSpatialPictures(&p.m_pSpatialPic[kiDidx][kiCurPos],
			&p.m_pSpatialPic[kiDidx][iCurTid])

	}
	return 0
}

/*
 *   SingleLayerPreprocess: down sampling if applicable
 *  @return: exact number of spatial layers need to encoder indeed
 */
func (p *CWelsPreProcess) SingleLayerPreprocess(pCtx *sWelsEncCtx, kpSrc *api.SSourcePicture, pScaledPicture *Scaled_Picture, pSpatialNum *int32) int32 {
	pSvcParam := pCtx.pSvcParam
	iDependencyId := int8(pSvcParam.ISpatialLayerNum - 1)

	var pSrcPic *SPicture // large
	var pDstPic *SPicture // small
	var pDlayerParam *api.SSpatialLayerConfig
	var pDlayerParamInternal *SSpatialLayerInternal
	var iSpatialNum int32
	var iSrcWidth int32
	var iSrcHeight int32
	var iTargetWidth int32
	var iTargetHeight int32
	var iTemporalId int32
	iClosestDid := int32(iDependencyId)
	pDlayerParamInternal = &pSvcParam.sDependencyLayers[iDependencyId]
	pDlayerParam = &pSvcParam.SSpatialLayers[iDependencyId]
	iTargetWidth = pDlayerParam.IVideoWidth
	iTargetHeight = pDlayerParam.IVideoHeight

	iSrcWidth = pSvcParam.SUsedPicRect.iWidth
	iSrcHeight = pSvcParam.SUsedPicRect.iHeight
	if pSvcParam.UiIntraPeriod != 0 {
		pCtx.pVaa.bIdrPeriodFlag = 1+pDlayerParamInternal.iFrameIndex >= int32(pSvcParam.UiIntraPeriod)
		if pCtx.pVaa.bIdrPeriodFlag {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG,
				"pSvcParam->uiIntraPeriod=%d, pCtx->pVaa->bIdrPeriodFlag=%d",
				pSvcParam.UiIntraPeriod,
				rcBool2Int(pCtx.pVaa.bIdrPeriodFlag))
		}
	}

	*pSpatialNum = 0
	if pScaledPicture.pScaledInputPicture != nil {
		pSrcPic = pScaledPicture.pScaledInputPicture
	} else {
		pSrcPic = p.GetCurrentOrigFrame(int32(iDependencyId))
	}
	iRet := p.WelsMoveMemoryWrapper(pSvcParam, pSrcPic, kpSrc, iSrcWidth, iSrcHeight)
	if iRet != ENC_RETURN_SUCCESS {
		return iRet
	}

	if pSvcParam.BEnableDenoise {
		p.BilateralDenoising(pSrcPic, iSrcWidth, iSrcHeight)
	}

	// different scaling in between input picture and dst highest spatial picture.
	iShrinkWidth := iSrcWidth
	iShrinkHeight := iSrcHeight
	pDstPic = pSrcPic
	if pScaledPicture.pScaledInputPicture != nil {
		// for highest downsampling
		pDstPic = p.GetCurrentOrigFrame(int32(iDependencyId))
		iShrinkWidth = pScaledPicture.iScaledWidth[iDependencyId]
		iShrinkHeight = pScaledPicture.iScaledHeight[iDependencyId]
	}
	p.DownsamplePadding(pSrcPic, pDstPic, iSrcWidth, iSrcHeight, iShrinkWidth, iShrinkHeight, iTargetWidth, iTargetHeight,
		false)

	if pSvcParam.BEnableSceneChangeDetect && !pCtx.pVaa.bIdrPeriodFlag {
		if pSvcParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
			if pDlayerParamInternal.bEncCurFrmAsIdrFlag {
				pCtx.pVaa.eSceneChangeIdc = processing.LARGE_CHANGED_SCENE
			} else {
				pCtx.pVaa.eSceneChangeIdc = p.DetectSceneChange(pDstPic, nil)
			}
			pCtx.pVaa.bSceneChangeFlag = (processing.LARGE_CHANGED_SCENE == pCtx.pVaa.eSceneChangeIdc)
		} else {
			if (!pDlayerParamInternal.bEncCurFrmAsIdrFlag) &&
				!(uint32(pDlayerParamInternal.iCodingIndex)&(pSvcParam.uiGopSize-1) != 0) {
				var pRefPic *SPicture
				if pCtx.pLtr[iDependencyId].bReceivedT0LostFlag {
					pRefPic = p.m_pSpatialPic[iDependencyId][int32(p.m_uiSpatialLayersInTemporal[iDependencyId])+
						int32(pCtx.pVaa.uiValidLongTermPicIdx)]
				} else {
					pRefPic = p.m_pLastSpatialPicture[iDependencyId][0]
				}
				//pCtx->pVaa->eSceneChangeIdc = DetectSceneChange (pDstPic, pRefPic);
				pCtx.pVaa.bSceneChangeFlag = p.GetSceneChangeFlag(p.DetectSceneChange(pDstPic, pRefPic))
			}
		}
	}

	for i := int32(0); i < pSvcParam.ISpatialLayerNum; i++ {
		pDlayerParamInternal = &pSvcParam.sDependencyLayers[i]
		iTemporalId = int32(pDlayerParamInternal.uiCodingIdx2TemporalId[uint32(pDlayerParamInternal.iCodingIndex)&
			(pSvcParam.uiGopSize-1)])
		if iTemporalId != int32(INVALID_TEMPORAL_ID) {
			iSpatialNum++
		}
	}
	pDlayerParamInternal = &pSvcParam.sDependencyLayers[iDependencyId]
	iTemporalId = int32(pDlayerParamInternal.uiCodingIdx2TemporalId[uint32(pDlayerParamInternal.iCodingIndex)&
		(pSvcParam.uiGopSize-1)])
	iActualSpatialNum := iSpatialNum - 1
	if iTemporalId != int32(INVALID_TEMPORAL_ID) {
		WelsUpdateSpatialIdxMap(pCtx, iActualSpatialNum, pDstPic, int32(iDependencyId))
		iActualSpatialNum--
	}

	p.m_pLastSpatialPicture[iDependencyId][1] = p.GetCurrentOrigFrame(int32(iDependencyId))
	iDependencyId--

	// generate other spacial layer
	// pSrc is
	//    -- padded input pic, if downsample should be applied to generate highest layer, [if] block above
	//    -- highest layer, if no downsampling, [else] block above
	if pSvcParam.ISpatialLayerNum > 1 {
		for iDependencyId >= 0 {
			pDlayerParamInternal = &pSvcParam.sDependencyLayers[iDependencyId]
			pDlayerParam = &pSvcParam.SSpatialLayers[iDependencyId]
			pSrcPic := p.m_pLastSpatialPicture[iClosestDid][1] // large
			iTargetWidth = pDlayerParam.IVideoWidth
			iTargetHeight = pDlayerParam.IVideoHeight
			iTemporalId = int32(pDlayerParamInternal.uiCodingIdx2TemporalId[uint32(pDlayerParamInternal.iCodingIndex)&
				(pSvcParam.uiGopSize-1)])

			// down sampling performed
			iSrcWidth := pScaledPicture.iScaledWidth[iClosestDid]
			iSrcHeight := pScaledPicture.iScaledHeight[iClosestDid]
			pDstPic = p.GetCurrentOrigFrame(int32(iDependencyId)) // small
			iShrinkWidth = pScaledPicture.iScaledWidth[iDependencyId]
			iShrinkHeight = pScaledPicture.iScaledHeight[iDependencyId]
			p.DownsamplePadding(pSrcPic, pDstPic, iSrcWidth, iSrcHeight, iShrinkWidth, iShrinkHeight, iTargetWidth, iTargetHeight,
				true)

			if iTemporalId != int32(INVALID_TEMPORAL_ID) {
				WelsUpdateSpatialIdxMap(pCtx, iActualSpatialNum, pDstPic, int32(iDependencyId))
				iActualSpatialNum--
			}

			p.m_pLastSpatialPicture[iDependencyId][1] = pDstPic

			iClosestDid = int32(iDependencyId)
			iDependencyId--
		}
	}
	*pSpatialNum = iSpatialNum
	return ENC_RETURN_SUCCESS
}

/*!
 * \brief   Whether input picture need be scaled?
 */
func JudgeNeedOfScaling(pParam *SWelsSvcCodingParam, pScaledPicture *Scaled_Picture) bool {
	kiInputPicWidth := pParam.SUsedPicRect.iWidth
	kiInputPicHeight := pParam.SUsedPicRect.iHeight
	kiDstPicWidth := pParam.sDependencyLayers[pParam.ISpatialLayerNum-1].iActualWidth
	kiDstPicHeight := pParam.sDependencyLayers[pParam.ISpatialLayerNum-1].iActualHeight
	bNeedDownsampling := true

	iSpatialIdx := pParam.ISpatialLayerNum - 1

	if kiDstPicWidth >= kiInputPicWidth && kiDstPicHeight >= kiInputPicHeight {
		bNeedDownsampling = false
	}

	for ; iSpatialIdx >= 0; iSpatialIdx-- {
		pCurLayer := &pParam.sDependencyLayers[iSpatialIdx]
		iCurDstWidth := pCurLayer.iActualWidth
		iCurDstHeight := pCurLayer.iActualHeight
		iInputWidthXDstHeight := kiInputPicWidth * iCurDstHeight
		iInputHeightXDstWidth := kiInputPicHeight * iCurDstWidth

		if iInputWidthXDstHeight > iInputHeightXDstWidth {
			pScaledPicture.iScaledWidth[iSpatialIdx] = common.WELS_MAX(iCurDstWidth, 4)
			pScaledPicture.iScaledHeight[iSpatialIdx] = common.WELS_MAX(iInputHeightXDstWidth/kiInputPicWidth, 4)
		} else {
			pScaledPicture.iScaledWidth[iSpatialIdx] = common.WELS_MAX(iInputWidthXDstHeight/kiInputPicHeight, 4)
			pScaledPicture.iScaledHeight[iSpatialIdx] = common.WELS_MAX(iCurDstHeight, 4)
		}
	}

	return bNeedDownsampling
}

// CMemoryAlign* pMemoryAlign dropped.
func WelsInitScaledPic(pParam *SWelsSvcCodingParam, pScaledPicture *Scaled_Picture) int32 {
	bInputPicNeedScaling := JudgeNeedOfScaling(pParam, pScaledPicture)
	if bInputPicNeedScaling {
		pScaledPicture.pScaledInputPicture = AllocPicture(pParam.SUsedPicRect.iWidth,
			pParam.SUsedPicRect.iHeight, false, 0)
		if pScaledPicture.pScaledInputPicture == nil {
			return -1
		}

		// Avoid valgrind false positives (zero-initialize the padding area
		// beyond each line of the source buffer used for downsampling).
		pPic := pScaledPicture.pScaledInputPicture
		ClearEndOfLinePadding(pPic.pData[0], pPic.iDataOff[0], pPic.iLineSize[0], pPic.iWidthInPixel, pPic.iHeightInPixel)
		ClearEndOfLinePadding(pPic.pData[1], pPic.iDataOff[1], pPic.iLineSize[1], pPic.iWidthInPixel>>1, pPic.iHeightInPixel>>1)
		ClearEndOfLinePadding(pPic.pData[2], pPic.iDataOff[2], pPic.iLineSize[2], pPic.iWidthInPixel>>1, pPic.iHeightInPixel>>1)
	}
	return 0
}

func FreeScaledPic(pScaledPicture *Scaled_Picture) {
	if pScaledPicture.pScaledInputPicture != nil {
		FreePicture(&pScaledPicture.pScaledInputPicture)
		pScaledPicture.pScaledInputPicture = nil
	}
}

func (p *CWelsPreProcess) InitLastSpatialPictures(pCtx *sWelsEncCtx) int32 {
	pParam := pCtx.pSvcParam
	kiDlayerCount := pParam.ISpatialLayerNum
	iDlayerIndex := int32(0)
	if pParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		for ; iDlayerIndex < MAX_DEPENDENCY_LAYER; iDlayerIndex++ {
			p.m_pLastSpatialPicture[iDlayerIndex][0] = nil
			p.m_pLastSpatialPicture[iDlayerIndex][1] = nil
		}
	} else {
		for ; iDlayerIndex < kiDlayerCount; iDlayerIndex++ {
			kiLayerInTemporal := int32(p.m_uiSpatialLayersInTemporal[iDlayerIndex])
			p.m_pLastSpatialPicture[iDlayerIndex][0] = p.m_pSpatialPic[iDlayerIndex][kiLayerInTemporal-2]
			p.m_pLastSpatialPicture[iDlayerIndex][1] = nil
		}
		for ; iDlayerIndex < MAX_DEPENDENCY_LAYER; iDlayerIndex++ {
			p.m_pLastSpatialPicture[iDlayerIndex][0] = nil
			p.m_pLastSpatialPicture[iDlayerIndex][1] = nil
		}
	}
	return 0
}

//*********************************************************************************************************/

func (p *CWelsPreProcess) ColorspaceConvert(pSvcParam *SWelsSvcCodingParam, pDstPic *SPicture, kpSrc *api.SSourcePicture, kiWidth int32, kiHeight int32) int32 {
	return 1
	//not support yet
}

func (p *CWelsPreProcess) BilateralDenoising(pSrc *SPicture, kiWidth int32, kiHeight int32) {
	iMethodIdx := processing.METHOD_DENOISE
	var sSrcPixMap processing.SPixMap
	vppSetPixMapPlane(&sSrcPixMap, 0, pSrc, 0)
	vppSetPixMapPlane(&sSrcPixMap, 1, pSrc, 1)
	vppSetPixMapPlane(&sSrcPixMap, 2, pSrc, 2)
	sSrcPixMap.ISizeInBits = g_kiPixMapSizeInBits
	sSrcPixMap.SRect.IRectWidth = kiWidth
	sSrcPixMap.SRect.IRectHeight = kiHeight
	sSrcPixMap.IStride[0] = pSrc.iLineSize[0]
	sSrcPixMap.IStride[1] = pSrc.iLineSize[1]
	sSrcPixMap.IStride[2] = pSrc.iLineSize[2]
	sSrcPixMap.EFormat = processing.VIDEO_FORMAT_I420

	p.m_pInterfaceVp.Process(iMethodIdx, &sSrcPixMap, nil)
}

// C default argument pRefPicture = NULL.
func (p *CWelsPreProcessVideo) DetectSceneChange(pCurPicture *SPicture, pRefPicture *SPicture) processing.ESceneChangeIdc {
	iMethodIdx := processing.METHOD_SCENE_CHANGE_DETECTION_VIDEO
	sSceneChangeDetectResult := processing.SSceneChangeResult{ESceneChangeIdc: processing.SIMILAR_SCENE}
	var sSrcPixMap processing.SPixMap
	var sRefPixMap processing.SPixMap
	vppSetPixMapPlane(&sSrcPixMap, 0, pCurPicture, 0)
	sSrcPixMap.ISizeInBits = g_kiPixMapSizeInBits
	sSrcPixMap.IStride[0] = pCurPicture.iLineSize[0]
	sSrcPixMap.SRect.IRectWidth = pCurPicture.iWidthInPixel
	sSrcPixMap.SRect.IRectHeight = pCurPicture.iHeightInPixel
	sSrcPixMap.EFormat = processing.VIDEO_FORMAT_I420

	vppSetPixMapPlane(&sRefPixMap, 0, pRefPicture, 0)
	sRefPixMap.ISizeInBits = g_kiPixMapSizeInBits
	sRefPixMap.IStride[0] = pRefPicture.iLineSize[0]
	sRefPixMap.SRect.IRectWidth = pRefPicture.iWidthInPixel
	sRefPixMap.SRect.IRectHeight = pRefPicture.iHeightInPixel
	sRefPixMap.EFormat = processing.VIDEO_FORMAT_I420

	iRet := p.m_pInterfaceVp.Process(iMethodIdx, &sSrcPixMap, &sRefPixMap)
	if iRet == 0 {
		p.m_pInterfaceVp.Get(iMethodIdx, &sSceneChangeDetectResult)
		//bSceneChangeFlag = (sSceneChangeDetectResult.eSceneChangeIdc == LARGE_CHANGED_SCENE) ? true : false;
	}
	return sSceneChangeDetectResult.ESceneChangeIdc
}

func (p *CWelsPreProcessVideo) GetCurrentOrigFrame(iDIdx int32) *SPicture {
	return p.m_pSpatialPic[iDIdx][p.GetCurPicPosition(iDIdx)]
}

func (p *CWelsPreProcess) DownsamplePadding(pSrc *SPicture, pDstPic *SPicture, iSrcWidth int32, iSrcHeight int32, iShrinkWidth int32, iShrinkHeight int32, iTargetWidth int32, iTargetHeight int32, bForceCopy bool) int32 {
	iRet := int32(0)
	var sSrcPixMap processing.SPixMap
	var sDstPicMap processing.SPixMap
	vppSetPixMapPlane(&sSrcPixMap, 0, pSrc, 0)
	vppSetPixMapPlane(&sSrcPixMap, 1, pSrc, 1)
	vppSetPixMapPlane(&sSrcPixMap, 2, pSrc, 2)
	sSrcPixMap.ISizeInBits = g_kiPixMapSizeInBits
	sSrcPixMap.SRect.IRectWidth = iSrcWidth
	sSrcPixMap.SRect.IRectHeight = iSrcHeight
	sSrcPixMap.IStride[0] = pSrc.iLineSize[0]
	sSrcPixMap.IStride[1] = pSrc.iLineSize[1]
	sSrcPixMap.IStride[2] = pSrc.iLineSize[2]
	sSrcPixMap.EFormat = processing.VIDEO_FORMAT_I420

	if iSrcWidth != iShrinkWidth || iSrcHeight != iShrinkHeight || bForceCopy {
		iMethodIdx := processing.METHOD_DOWNSAMPLE
		vppSetPixMapPlane(&sDstPicMap, 0, pDstPic, 0)
		vppSetPixMapPlane(&sDstPicMap, 1, pDstPic, 1)
		vppSetPixMapPlane(&sDstPicMap, 2, pDstPic, 2)
		sDstPicMap.ISizeInBits = g_kiPixMapSizeInBits
		sDstPicMap.SRect.IRectWidth = iShrinkWidth
		sDstPicMap.SRect.IRectHeight = iShrinkHeight
		sDstPicMap.IStride[0] = pDstPic.iLineSize[0]
		sDstPicMap.IStride[1] = pDstPic.iLineSize[1]
		sDstPicMap.IStride[2] = pDstPic.iLineSize[2]
		sDstPicMap.EFormat = processing.VIDEO_FORMAT_I420

		if iSrcWidth != iShrinkWidth || iSrcHeight != iShrinkHeight {
			iRet = p.m_pInterfaceVp.Process(iMethodIdx, &sSrcPixMap, &sDstPicMap)
		} else {
			WelsMoveMemory_c(pDstPic.pData[0], pDstPic.iDataOff[0], pDstPic.pData[1], pDstPic.iDataOff[1],
				pDstPic.pData[2], pDstPic.iDataOff[2],
				pDstPic.iLineSize[0], pDstPic.iLineSize[1], pDstPic.iLineSize[2],
				pSrc.pData[0], pSrc.iDataOff[0], pSrc.pData[1], pSrc.iDataOff[1], pSrc.pData[2], pSrc.iDataOff[2],
				pSrc.iLineSize[0], pSrc.iLineSize[1], pSrc.iLineSize[2], iSrcWidth, iSrcHeight)
		}
	} else {
		sDstPicMap = sSrcPixMap
	}

	// get rid of odd line
	iShrinkWidth -= (iShrinkWidth & 1)
	iShrinkHeight -= (iShrinkHeight & 1)
	p.Padding(sDstPicMap.PPixel[0], sDstPicMap.IPixelOff[0], sDstPicMap.PPixel[1], sDstPicMap.IPixelOff[1],
		sDstPicMap.PPixel[2], sDstPicMap.IPixelOff[2],
		sDstPicMap.IStride[0], sDstPicMap.IStride[1], iShrinkWidth, iTargetWidth, iShrinkHeight, iTargetHeight)

	return iRet
}

//*********************************************************************************************************/

func (p *CWelsPreProcess) VaaCalculation(pVaaInfo *SVAAFrameInfo, pCurPicture *SPicture, pRefPicture *SPicture, bCalculateSQDiff bool, bCalculateVar bool, bCalculateBGD bool) {
	pVaaInfo.sVaaCalcInfo.PCurY = pCurPicture.pData[0][pCurPicture.iDataOff[0]:]
	pVaaInfo.sVaaCalcInfo.PRefY = pRefPicture.pData[0][pRefPicture.iDataOff[0]:]
	{
		iMethodIdx := processing.METHOD_VAA_STATISTICS
		var sCurPixMap processing.SPixMap
		var sRefPixMap processing.SPixMap
		var calc_param processing.SVAACalcParam

		vppSetPixMapPlane(&sCurPixMap, 0, pCurPicture, 0)
		sCurPixMap.ISizeInBits = g_kiPixMapSizeInBits
		sCurPixMap.SRect.IRectWidth = pCurPicture.iWidthInPixel
		sCurPixMap.SRect.IRectHeight = pCurPicture.iHeightInPixel
		sCurPixMap.IStride[0] = pCurPicture.iLineSize[0]
		sCurPixMap.EFormat = processing.VIDEO_FORMAT_I420

		vppSetPixMapPlane(&sRefPixMap, 0, pRefPicture, 0)
		sRefPixMap.ISizeInBits = g_kiPixMapSizeInBits
		sRefPixMap.SRect.IRectWidth = pRefPicture.iWidthInPixel
		sRefPixMap.SRect.IRectHeight = pRefPicture.iHeightInPixel
		sRefPixMap.IStride[0] = pRefPicture.iLineSize[0]
		sRefPixMap.EFormat = processing.VIDEO_FORMAT_I420

		calc_param.ICalcVar = rcBool2Int(bCalculateVar)
		calc_param.ICalcBgd = rcBool2Int(bCalculateBGD)
		calc_param.ICalcSsd = rcBool2Int(bCalculateSQDiff)
		calc_param.PCalcResult = &pVaaInfo.sVaaCalcInfo
		if !bCalculateVar && !bCalculateBGD && !bCalculateSQDiff {
			calc_param.UnchangedMacroblocks = p.vaaUnchanged.maskFor(p.m_pEncCtx, pCurPicture, pRefPicture)
		}

		p.m_pInterfaceVp.Set(iMethodIdx, &calc_param)
		p.m_pInterfaceVp.Process(iMethodIdx, &sCurPixMap, &sRefPixMap)
		if calc_param.UnchangedMacroblocks != nil {
			// Framework validation may reject the maps before the strategy can
			// consume its borrowed hint. Release it on that path as well.
			calc_param.UnchangedMacroblocks = nil
			p.m_pInterfaceVp.Set(iMethodIdx, &calc_param)
		}
	}
}

func (p *CWelsPreProcess) BackgroundDetection(pVaaInfo *SVAAFrameInfo, pCurPicture *SPicture, pRefPicture *SPicture, bDetectFlag bool) {
	if bDetectFlag {
		pVaaInfo.iPicWidth = pCurPicture.iWidthInPixel
		pVaaInfo.iPicHeight = pCurPicture.iHeightInPixel

		pVaaInfo.iPicStride = pCurPicture.iLineSize[0]
		pVaaInfo.iPicStrideUV = pCurPicture.iLineSize[1]
		pVaaInfo.pCurY, pVaaInfo.iCurYOff = pCurPicture.pData[0], pCurPicture.iDataOff[0]
		pVaaInfo.pRefY, pVaaInfo.iRefYOff = pRefPicture.pData[0], pRefPicture.iDataOff[0]
		pVaaInfo.pCurU, pVaaInfo.iCurUOff = pCurPicture.pData[1], pCurPicture.iDataOff[1]
		pVaaInfo.pRefU, pVaaInfo.iRefUOff = pRefPicture.pData[1], pRefPicture.iDataOff[1]
		pVaaInfo.pCurV, pVaaInfo.iCurVOff = pCurPicture.pData[2], pCurPicture.iDataOff[2]
		pVaaInfo.pRefV, pVaaInfo.iRefVOff = pRefPicture.pData[2], pRefPicture.iDataOff[2]

		iMethodIdx := processing.METHOD_BACKGROUND_DETECTION
		var sSrcPixMap processing.SPixMap
		var sRefPixMap processing.SPixMap
		var BGDParam processing.SBGDInterface

		vppSetPixMapPlane(&sSrcPixMap, 0, pCurPicture, 0)
		vppSetPixMapPlane(&sSrcPixMap, 1, pCurPicture, 1)
		vppSetPixMapPlane(&sSrcPixMap, 2, pCurPicture, 2)
		sSrcPixMap.ISizeInBits = g_kiPixMapSizeInBits
		sSrcPixMap.IStride[0] = pCurPicture.iLineSize[0]
		sSrcPixMap.IStride[1] = pCurPicture.iLineSize[1]
		sSrcPixMap.IStride[2] = pCurPicture.iLineSize[2]
		sSrcPixMap.SRect.IRectWidth = pCurPicture.iWidthInPixel
		sSrcPixMap.SRect.IRectHeight = pCurPicture.iHeightInPixel
		sSrcPixMap.EFormat = processing.VIDEO_FORMAT_I420

		vppSetPixMapPlane(&sRefPixMap, 0, pRefPicture, 0)
		vppSetPixMapPlane(&sRefPixMap, 1, pRefPicture, 1)
		vppSetPixMapPlane(&sRefPixMap, 2, pRefPicture, 2)
		sRefPixMap.ISizeInBits = g_kiPixMapSizeInBits
		sRefPixMap.IStride[0] = pRefPicture.iLineSize[0]
		sRefPixMap.IStride[1] = pRefPicture.iLineSize[1]
		sRefPixMap.IStride[2] = pRefPicture.iLineSize[2]
		sRefPixMap.SRect.IRectWidth = pRefPicture.iWidthInPixel
		sRefPixMap.SRect.IRectHeight = pRefPicture.iHeightInPixel
		sRefPixMap.EFormat = processing.VIDEO_FORMAT_I420

		BGDParam.PBackgroundMbFlag = pVaaInfo.pVaaBackgroundMbFlag
		BGDParam.PCalcRes = &pVaaInfo.sVaaCalcInfo
		p.m_pInterfaceVp.Set(iMethodIdx, &BGDParam)
		p.m_pInterfaceVp.Process(iMethodIdx, &sSrcPixMap, &sRefPixMap)
	} else {
		iPicWidthInMb := (pCurPicture.iWidthInPixel + 15) >> 4
		iPicHeightInMb := (pCurPicture.iHeightInPixel + 15) >> 4
		clear(pVaaInfo.pVaaBackgroundMbFlag[:iPicWidthInMb*iPicHeightInMb])
	}
}

func (p *CWelsPreProcess) AdaptiveQuantCalculation(pVaaInfo *SVAAFrameInfo, pCurPicture *SPicture, pRefPicture *SPicture) {
	pVaaInfo.sAdaptiveQuantParam.PCalcResult = &pVaaInfo.sVaaCalcInfo
	pVaaInfo.sAdaptiveQuantParam.IAverMotionTextureIndexToDeltaQp = 0

	{
		iMethodIdx := processing.METHOD_ADAPTIVE_QUANT
		var pSrc processing.SPixMap
		var pRef processing.SPixMap
		var iRet int32

		vppSetPixMapPlane(&pSrc, 0, pCurPicture, 0)
		pSrc.ISizeInBits = g_kiPixMapSizeInBits
		pSrc.IStride[0] = pCurPicture.iLineSize[0]
		pSrc.SRect.IRectWidth = pCurPicture.iWidthInPixel
		pSrc.SRect.IRectHeight = pCurPicture.iHeightInPixel
		pSrc.EFormat = processing.VIDEO_FORMAT_I420

		vppSetPixMapPlane(&pRef, 0, pRefPicture, 0)
		pRef.ISizeInBits = g_kiPixMapSizeInBits
		pRef.IStride[0] = pRefPicture.iLineSize[0]
		pRef.SRect.IRectWidth = pRefPicture.iWidthInPixel
		pRef.SRect.IRectHeight = pRefPicture.iHeightInPixel
		pRef.EFormat = processing.VIDEO_FORMAT_I420

		iRet = p.m_pInterfaceVp.Set(iMethodIdx, &pVaaInfo.sAdaptiveQuantParam)
		iRet = p.m_pInterfaceVp.Process(iMethodIdx, &pSrc, &pRef)
		if iRet == 0 {
			p.m_pInterfaceVp.Get(iMethodIdx, &pVaaInfo.sAdaptiveQuantParam)
		}
	}
}

// uint32_t** pRefMbTypeArray (&pPic->uiRefMbType) -> *[]uint32.
func (p *CWelsPreProcess) SetRefMbType(pCtx *sWelsEncCtx, pRefMbTypeArray *[]uint32, iRefPicType int32) {
	uiTid := pCtx.uiTemporalId
	uiDid := pCtx.uiDependencyId
	pRefPicLlist := pCtx.ppRefPicListExt[uiDid]
	pLtr := &pCtx.pLtr[uiDid]
	var i uint8

	if pCtx.pSvcParam.BEnableLongTermReference && pLtr.bReceivedT0LostFlag && uiTid == 0 {
		for i = 0; i < pRefPicLlist.uiLongRefCount; i++ {
			pRef := pRefPicLlist.pLongRefList[i]
			if pRef != nil && pRef.uiRecieveConfirmed == 1 /*RECIEVE_SUCCESS*/ {
				*pRefMbTypeArray = pRef.uiRefMbType
				break
			}
		}
	} else {
		for i = 0; i < pRefPicLlist.uiShortRefCount; i++ {
			pRef := pRefPicLlist.pShortRefList[i]
			if pRef != nil && pRef.bUsedAsRef && pRef.iFramePoc >= 0 && pRef.uiTemporalId <= uiTid {
				*pRefMbTypeArray = pRef.uiRefMbType
				break
			}
		}
	}
}

func (p *CWelsPreProcess) AnalyzePictureComplexity(pCtx *sWelsEncCtx, pCurPicture *SPicture, pRefPicture *SPicture, kiDependencyId int32, bCalculateBGD bool) {
	pSvcParam := pCtx.pSvcParam
	var iComplexityAnalysisMode int32

	if pSvcParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		pVaaExt := pCtx.pVaa.pExt
		sComplexityAnalysisParam := &pVaaExt.sComplexityScreenParam
		pWelsSvcRc := &pCtx.pWelsSvcRc[kiDependencyId]

		if pCtx.eSliceType == common.P_SLICE {
			iComplexityAnalysisMode = processing.GOM_SAD
		} else if pCtx.eSliceType == common.I_SLICE {
			iComplexityAnalysisMode = processing.GOM_VAR
		} else {
			return
		}
		_ = iComplexityAnalysisMode

		clear(pWelsSvcRc.pGomForegroundBlockNum[:pWelsSvcRc.iGomSize])
		clear(pWelsSvcRc.pCurrentFrameGomSad[:pWelsSvcRc.iGomSize])

		sComplexityAnalysisParam.IFrameComplexity = 0
		sComplexityAnalysisParam.PGomComplexity = pWelsSvcRc.pCurrentFrameGomSad
		sComplexityAnalysisParam.IGomNumInFrame = pWelsSvcRc.iGomSize
		sComplexityAnalysisParam.IIdrFlag = rcBool2Int(pCtx.eSliceType == common.I_SLICE)
		sComplexityAnalysisParam.IMbRowInGom = GOM_H_SCC
		sComplexityAnalysisParam.SScrollResult.BScrollDetectFlag = false
		sComplexityAnalysisParam.SScrollResult.IScrollMvX = 0
		sComplexityAnalysisParam.SScrollResult.IScrollMvY = 0

		iMethodIdx := processing.METHOD_COMPLEXITY_ANALYSIS_SCREEN
		var sSrcPixMap processing.SPixMap
		var sRefPixMap processing.SPixMap
		var iRet int32

		vppSetPixMapPlane(&sSrcPixMap, 0, pCurPicture, 0)
		sSrcPixMap.ISizeInBits = g_kiPixMapSizeInBits
		sSrcPixMap.IStride[0] = pCurPicture.iLineSize[0]
		sSrcPixMap.SRect.IRectWidth = pCurPicture.iWidthInPixel
		sSrcPixMap.SRect.IRectHeight = pCurPicture.iHeightInPixel
		sSrcPixMap.EFormat = processing.VIDEO_FORMAT_I420

		if pRefPicture != nil {
			vppSetPixMapPlane(&sRefPixMap, 0, pRefPicture, 0)
			sRefPixMap.ISizeInBits = g_kiPixMapSizeInBits
			sRefPixMap.IStride[0] = pRefPicture.iLineSize[0]
			sRefPixMap.SRect.IRectWidth = pRefPicture.iWidthInPixel
			sRefPixMap.SRect.IRectHeight = pRefPicture.iHeightInPixel
			sRefPixMap.EFormat = processing.VIDEO_FORMAT_I420
		}

		iRet = p.m_pInterfaceVp.Set(iMethodIdx, sComplexityAnalysisParam)
		iRet = p.m_pInterfaceVp.Process(iMethodIdx, &sSrcPixMap, &sRefPixMap)
		if iRet == 0 {
			p.m_pInterfaceVp.Get(iMethodIdx, sComplexityAnalysisParam)
		}

	} else {
		pVaaInfo := pCtx.pVaa
		sComplexityAnalysisParam := &pVaaInfo.sComplexityAnalysisParam
		SWelsSvcRc := &pCtx.pWelsSvcRc[kiDependencyId]

		if pSvcParam.IRCMode == api.RC_QUALITY_MODE && pCtx.eSliceType == common.P_SLICE {
			iComplexityAnalysisMode = processing.FRAME_SAD
		} else if ((pSvcParam.IRCMode == api.RC_BITRATE_MODE) || (pSvcParam.IRCMode == api.RC_TIMESTAMP_MODE)) &&
			pCtx.eSliceType == common.P_SLICE {
			iComplexityAnalysisMode = processing.GOM_SAD
		} else if ((pSvcParam.IRCMode == api.RC_BITRATE_MODE) || (pSvcParam.IRCMode == api.RC_TIMESTAMP_MODE)) &&
			pCtx.eSliceType == common.I_SLICE {
			iComplexityAnalysisMode = processing.GOM_VAR
		} else {
			return
		}

		sComplexityAnalysisParam.IComplexityAnalysisMode = iComplexityAnalysisMode
		sComplexityAnalysisParam.PCalcResult = &pVaaInfo.sVaaCalcInfo
		sComplexityAnalysisParam.PBackgroundMbFlag = pVaaInfo.pVaaBackgroundMbFlag
		if pRefPicture != nil {
			p.SetRefMbType(pCtx, &sComplexityAnalysisParam.UiRefMbType, pRefPicture.iPictureType)
		}
		sComplexityAnalysisParam.ICalcBgd = rcBool2Int(bCalculateBGD)
		sComplexityAnalysisParam.IFrameComplexity = 0

		clear(SWelsSvcRc.pGomForegroundBlockNum[:SWelsSvcRc.iGomSize])
		if iComplexityAnalysisMode != processing.FRAME_SAD {
			clear(SWelsSvcRc.pCurrentFrameGomSad[:SWelsSvcRc.iGomSize])
		}

		sComplexityAnalysisParam.PGomComplexity = SWelsSvcRc.pCurrentFrameGomSad
		sComplexityAnalysisParam.PGomForegroundBlockNum = SWelsSvcRc.pGomForegroundBlockNum
		sComplexityAnalysisParam.IMbNumInGom = SWelsSvcRc.iNumberMbGom

		{
			iMethodIdx := processing.METHOD_COMPLEXITY_ANALYSIS
			var sSrcPixMap processing.SPixMap
			var sRefPixMap processing.SPixMap
			var iRet int32

			vppSetPixMapPlane(&sSrcPixMap, 0, pCurPicture, 0)
			sSrcPixMap.ISizeInBits = g_kiPixMapSizeInBits
			sSrcPixMap.IStride[0] = pCurPicture.iLineSize[0]
			sSrcPixMap.SRect.IRectWidth = pCurPicture.iWidthInPixel
			sSrcPixMap.SRect.IRectHeight = pCurPicture.iHeightInPixel
			sSrcPixMap.EFormat = processing.VIDEO_FORMAT_I420

			if pRefPicture != nil {
				vppSetPixMapPlane(&sRefPixMap, 0, pRefPicture, 0)
				sRefPixMap.ISizeInBits = g_kiPixMapSizeInBits
				sRefPixMap.IStride[0] = pRefPicture.iLineSize[0]
				sRefPixMap.SRect.IRectWidth = pRefPicture.iWidthInPixel
				sRefPixMap.SRect.IRectHeight = pRefPicture.iHeightInPixel
			}
			sRefPixMap.EFormat = processing.VIDEO_FORMAT_I420

			iRet = p.m_pInterfaceVp.Set(iMethodIdx, sComplexityAnalysisParam)
			iRet = p.m_pInterfaceVp.Process(iMethodIdx, &sSrcPixMap, &sRefPixMap)
			if iRet == 0 {
				p.m_pInterfaceVp.Get(iMethodIdx, sComplexityAnalysisParam)
			}
		}
	}
}

func (p *CWelsPreProcess) InitPixMap(pPicture *SPicture, pPixMap *processing.SPixMap) {
	vppSetPixMapPlane(pPixMap, 0, pPicture, 0)
	vppSetPixMapPlane(pPixMap, 1, pPicture, 1)
	vppSetPixMapPlane(pPixMap, 2, pPicture, 2)
	pPixMap.ISizeInBits = 1 // sizeof (uint8_t)
	pPixMap.IStride[0] = pPicture.iLineSize[0]
	pPixMap.IStride[1] = pPicture.iLineSize[1]
	pPixMap.SRect.IRectWidth = pPicture.iWidthInPixel
	pPixMap.SRect.IRectHeight = pPicture.iHeightInPixel

	pPixMap.EFormat = processing.VIDEO_FORMAT_I420
}

// returns &m_pSpatialPic[iTargetDid][1] -> m_pSpatialPic[iTargetDid][1:].
func (p *CWelsPreProcessScreen) GetReferenceSrcPicList(iTargetDid int32) []*SPicture {
	return p.m_pSpatialPic[iTargetDid][1:]
}

// SPicture** pSrcPicList -> []*SPicture; SRefInfoParam* pAvailableRefParam -> []SRefInfoParam; int32_t& -> *int32.
func (p *CWelsPreProcessScreen) GetAvailableRefListLosslessScreenRefSelection(pSrcPicList []*SPicture, iCurTid uint8, iClosestLtrFrameNum int32, pAvailableRefParam []SRefInfoParam, iAvailableRefNum *int32, iAvailableSceneRefNum *int32) {
	pRefPicList := pSrcPicList
	iSourcePicNum := p.m_iAvaliableRefInSpatialPicList
	if 0 >= iSourcePicNum {
		*iAvailableRefNum = 0
		*iAvailableSceneRefNum = 0
		return
	}
	bCurFrameMarkedAsSceneLtr := p.m_pEncCtx.bCurFrameMarkedAsSceneLtr
	var pRefPic *SPicture
	var uiRefTid uint8
	bRefRealLtr := false

	*iAvailableRefNum = 1 //zero is left for the closest frame
	*iAvailableSceneRefNum = 0

	//the saving order will be depend on pSrcPicList
	//TODO: use a frame_idx to find the closer ref in time distance, and correctly sort the ref list
	for i := iSourcePicNum - 1; i >= 0; i-- {
		pRefPic = pRefPicList[i]
		if nil == pRefPic || !pRefPic.bUsedAsRef || !pRefPic.bIsLongRef || (bCurFrameMarkedAsSceneLtr &&
			(!pRefPic.bIsSceneLTR)) {
			continue
		}
		uiRefTid = pRefPic.uiTemporalId
		bRefRealLtr = pRefPic.bIsSceneLTR

		if bRefRealLtr || (0 == iCurTid && 0 == uiRefTid) || (uiRefTid < iCurTid) {
			var idx int32
			if pRefPic.iLongTermPicNum == iClosestLtrFrameNum {
				idx = 0
			} else {
				idx = *iAvailableRefNum
				*iAvailableRefNum++
			}
			pAvailableRefParam[idx].pRefPicture = pRefPic
			pAvailableRefParam[idx].iSrcListIdx = i + 1 //in SrcList, the idx 0 is reserved for CurPic
			*iAvailableSceneRefNum += rcBool2Int(bRefRealLtr)
		}
	}

	if pAvailableRefParam[0].pRefPicture == nil {
		for i := int32(1); i < *iAvailableRefNum; i++ {
			pAvailableRefParam[i-1].pRefPicture = pAvailableRefParam[i].pRefPicture
			pAvailableRefParam[i-1].iSrcListIdx = pAvailableRefParam[i].iSrcListIdx
		}

		pAvailableRefParam[*iAvailableRefNum-1].pRefPicture = nil
		pAvailableRefParam[*iAvailableRefNum-1].iSrcListIdx = 0
		*iAvailableRefNum--
	}
}

func (p *CWelsPreProcessScreen) GetAvailableRefList(pSrcPicList []*SPicture, iCurTid uint8, iClosestLtrFrameNum int32, pAvailableRefList []SRefInfoParam, iAvailableRefNum *int32, iAvailableSceneRefNum *int32) {
	iSourcePicNum := p.m_iAvaliableRefInSpatialPicList
	if 0 >= iSourcePicNum {
		*iAvailableRefNum = 0
		*iAvailableSceneRefNum = 0
		return
	}
	var pRefPic *SPicture
	var uiRefTid uint8
	*iAvailableRefNum = 0
	*iAvailableSceneRefNum = 0

	//the saving order will be depend on pSrcPicList
	//TODO: use a frame_idx to find the closer ref in time distance, and correctly sort the ref list
	for i := iSourcePicNum - 1; i >= 0; i-- {
		pRefPic = pSrcPicList[i]
		if nil == pRefPic || !pRefPic.bUsedAsRef {
			continue
		}
		uiRefTid = pRefPic.uiTemporalId

		if uiRefTid <= iCurTid {
			pAvailableRefList[*iAvailableRefNum].pRefPicture = pRefPic
			pAvailableRefList[*iAvailableRefNum].iSrcListIdx = i + 1 //in SrcList, the idx 0 is reserved for CurPic
			*iAvailableRefNum++
		}
	}
}

func (p *CWelsPreProcessScreen) InitRefJudgement(pRefJudgement *SRefJudgement) {
	pRefJudgement.iMinFrameComplexity = math.MaxInt32
	pRefJudgement.iMinFrameComplexity08 = math.MaxInt32
	pRefJudgement.iMinFrameComplexity11 = math.MaxInt32

	pRefJudgement.iMinFrameNumGap = math.MaxInt32
	pRefJudgement.iMinFrameQp = math.MaxInt32
}

// const SRefJudgement& -> value.
func (p *CWelsPreProcessScreen) JudgeBestRef(pRefPic *SPicture, sRefJudgement SRefJudgement, iFrameComplexity int64, bIsClosestLtrFrame bool) bool {
	if bIsClosestLtrFrame {
		return iFrameComplexity < sRefJudgement.iMinFrameComplexity11
	}
	return (iFrameComplexity < sRefJudgement.iMinFrameComplexity08) || ((iFrameComplexity <= sRefJudgement.iMinFrameComplexity11) &&
		(pRefPic.iFrameAverageQp < sRefJudgement.iMinFrameQp))
}

func (p *CWelsPreProcessScreen) SaveBestRefToJudgement(iRefPictureAvQP int32, iComplexity int64, pRefJudgement *SRefJudgement) {
	pRefJudgement.iMinFrameQp = iRefPictureAvQP
	pRefJudgement.iMinFrameComplexity = iComplexity
	pRefJudgement.iMinFrameComplexity08 = int64(int32(float64(iComplexity) * 0.8))
	pRefJudgement.iMinFrameComplexity11 = int64(int32(float64(iComplexity) * 1.1))
}

// const SSceneChangeResult& -> value.
func (p *CWelsPreProcessScreen) SaveBestRefToLocal(pRefPicInfo *SRefInfoParam, sSceneChangeResult processing.SSceneChangeResult, pRefSaved *SRefInfoParam) {
	*pRefSaved = *pRefPicInfo
	pRefSaved.pBestBlockStaticIdc = sSceneChangeResult.PStaticBlockIdc
}

// SRefInfoParam& sRefSaved -> *SRefInfoParam.
func (p *CWelsPreProcessScreen) SaveBestRefToVaa(sRefSaved *SRefInfoParam, pVaaBestRef *SRefInfoParam) {
	*pVaaBestRef = *sRefSaved
}

func (p *CWelsPreProcessScreen) GetCurrentOrigFrame(iDIdx int32) *SPicture {
	return p.m_pSpatialPic[iDIdx][0]
}

const STATIC_SCENE_MOTION_RATIO float32 = 0.01

// C default argument pRef = NULL.
func (p *CWelsPreProcessScreen) DetectSceneChange(pCurPicture *SPicture, pRef *SPicture) processing.ESceneChangeIdc {
	pCtx := p.m_pEncCtx
	if nil == pCtx || nil == pCtx.pVaa || nil == pCtx.pVaa.pExt || nil == pCurPicture {
		return processing.LARGE_CHANGED_SCENE
	}
	pSvcParam := pCtx.pSvcParam
	pVaaExt := pCtx.pVaa.pExt
	pParamInternal := &pSvcParam.sDependencyLayers[0]

	iTargetDid := pSvcParam.ISpatialLayerNum - 1
	if 0 != iTargetDid {
		return processing.LARGE_CHANGED_SCENE
	}

	var iVaaFrameSceneChangeIdc processing.ESceneChangeIdc = processing.LARGE_CHANGED_SCENE
	pRefPicList := p.GetReferenceSrcPicList(iTargetDid)
	if nil == pRefPicList {
		return processing.LARGE_CHANGED_SCENE
	}

	var sAvailableRefParam [MAX_REF_PIC_COUNT]SRefInfoParam
	var iAvailableRefNum int32
	var iAvailableSceneRefNum int32

	iSceneChangeMethodIdx := processing.METHOD_SCENE_CHANGE_DETECTION_SCREEN
	sSceneChangeResult := processing.SSceneChangeResult{ESceneChangeIdc: processing.SIMILAR_SCENE}

	var sSrcMap processing.SPixMap
	var sRefMap processing.SPixMap
	var sLtrJudgement SRefJudgement
	var sSceneLtrJudgement SRefJudgement
	var sLtrSaved SRefInfoParam
	var sSceneLtrSaved SRefInfoParam

	var iNumOfLargeChange, iNumOfMediumChangeToLtr int32

	bIsClosestLtrFrame := false
	var ret int32
	var iScdIdx int32

	var pRefPic *SPicture
	var pRefPicInfo *SRefInfoParam
	var pCurBlockStaticPointer []uint8
	pLogCtx := &pCtx.sLogCtx
	iNegligibleMotionBlocks := int32(float32((pCurPicture.iWidthInPixel>>3)*(pCurPicture.iHeightInPixel>>3)) *
		STATIC_SCENE_MOTION_RATIO)
	iCurTid := uint8(GetTemporalLevel(&pSvcParam.sDependencyLayers[p.m_pEncCtx.sSpatialIndexMap[0].iDid],
		pParamInternal.iCodingIndex, int32(pSvcParam.uiGopSize)))
	if iCurTid == INVALID_TEMPORAL_ID {
		return processing.LARGE_CHANGED_SCENE
	}
	iClosestLtrFrameNum := pCtx.pLtr[iTargetDid].iLastLtrIdx[iCurTid]
	if pSvcParam.BEnableLongTermReference {
		p.GetAvailableRefListLosslessScreenRefSelection(pRefPicList, iCurTid, iClosestLtrFrameNum, sAvailableRefParam[:],
			&iAvailableRefNum,
			&iAvailableSceneRefNum)
	} else {
		p.GetAvailableRefList(pRefPicList, iCurTid, iClosestLtrFrameNum, sAvailableRefParam[:], &iAvailableRefNum,
			&iAvailableSceneRefNum)
	}
	//after this build, pAvailableRefList[idx].iSrcListIdx is the idx of the ref in h->spatial_pic
	if 0 == iAvailableRefNum {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "SceneChangeDetect() iAvailableRefNum=0 but not I.")
		return processing.LARGE_CHANGED_SCENE
	}

	p.InitPixMap(pCurPicture, &sSrcMap)
	p.InitRefJudgement(&sLtrJudgement)
	p.InitRefJudgement(&sSceneLtrJudgement)

	for iScdIdx = 0; iScdIdx < iAvailableRefNum; iScdIdx++ {
		pCurBlockStaticPointer = pVaaExt.pVaaBlockStaticIdc[iScdIdx]
		sSceneChangeResult.ESceneChangeIdc = processing.SIMILAR_SCENE
		sSceneChangeResult.PStaticBlockIdc = pCurBlockStaticPointer
		sSceneChangeResult.SScrollResult.BScrollDetectFlag = false

		pRefPicInfo = &sAvailableRefParam[iScdIdx]
		pRefPic = pRefPicInfo.pRefPicture
		p.InitPixMap(pRefPic, &sRefMap)

		bIsClosestLtrFrame = (pRefPic.iLongTermPicNum == iClosestLtrFrameNum)
		if 0 == iScdIdx {
			var ret int32
			pScrollDetectInfo := &pVaaExt.sScrollDetectInfo
			*pScrollDetectInfo = processing.SScrollDetectionParam{}

			iMethodIdx := processing.METHOD_SCROLL_DETECTION

			p.m_pInterfaceVp.Set(iMethodIdx, pScrollDetectInfo)
			ret = p.m_pInterfaceVp.Process(iMethodIdx, &sSrcMap, &sRefMap)

			if ret == 0 {
				p.m_pInterfaceVp.Get(iMethodIdx, pScrollDetectInfo)
				// Ensure detected scroll motion vectors stay within the configured
				// motion vector range.
				if pScrollDetectInfo.BScrollDetectFlag {
					pScrollDetectInfo.IScrollMvX = common.WELS_CLIP3(pScrollDetectInfo.IScrollMvX, -p.m_pEncCtx.iMvRange,
						p.m_pEncCtx.iMvRange)
					pScrollDetectInfo.IScrollMvY = common.WELS_CLIP3(pScrollDetectInfo.IScrollMvY, -p.m_pEncCtx.iMvRange,
						p.m_pEncCtx.iMvRange)
				}
			}
			sSceneChangeResult.SScrollResult = pVaaExt.sScrollDetectInfo
		}

		p.m_pInterfaceVp.Set(iSceneChangeMethodIdx, &sSceneChangeResult)
		ret = p.m_pInterfaceVp.Process(iSceneChangeMethodIdx, &sSrcMap, &sRefMap)

		if ret == 0 {
			p.m_pInterfaceVp.Get(iSceneChangeMethodIdx, &sSceneChangeResult)

			iFrameComplexity := sSceneChangeResult.IFrameComplexity
			iSceneDetectIdc := sSceneChangeResult.ESceneChangeIdc
			iMotionBlockNum := sSceneChangeResult.IMotionBlockNum

			bCurRefIsSceneLtr := pRefPic.bIsSceneLTR
			iRefPicAvQP := pRefPic.iFrameAverageQp

			//for scene change detection
			iNumOfLargeChange += rcBool2Int(processing.LARGE_CHANGED_SCENE == iSceneDetectIdc)
			iNumOfMediumChangeToLtr += rcBool2Int((bCurRefIsSceneLtr) && (iSceneDetectIdc != processing.SIMILAR_SCENE))

			//for reference selection
			//this judge can only be saved when iAvailableRefNum==1, which is very limit
			//when LTR is OFF, it can still judge from all available STR
			if p.JudgeBestRef(pRefPic, sLtrJudgement, iFrameComplexity, bIsClosestLtrFrame) {
				p.SaveBestRefToJudgement(iRefPicAvQP, iFrameComplexity, &sLtrJudgement)
				p.SaveBestRefToLocal(pRefPicInfo, sSceneChangeResult, &sLtrSaved)
			}
			if bCurRefIsSceneLtr && p.JudgeBestRef(pRefPic, sSceneLtrJudgement, iFrameComplexity, bIsClosestLtrFrame) {
				p.SaveBestRefToJudgement(iRefPicAvQP, iFrameComplexity, &sSceneLtrJudgement)
				p.SaveBestRefToLocal(pRefPicInfo, sSceneChangeResult, &sSceneLtrSaved)
			}

			if iMotionBlockNum <= iNegligibleMotionBlocks {
				break
			}
		}
	}

	if iNumOfLargeChange == iAvailableRefNum {
		iVaaFrameSceneChangeIdc = processing.LARGE_CHANGED_SCENE
	} else if (iNumOfMediumChangeToLtr == iAvailableSceneRefNum) && (0 != iAvailableSceneRefNum) {
		iVaaFrameSceneChangeIdc = processing.MEDIUM_CHANGED_SCENE
	} else {
		iVaaFrameSceneChangeIdc = processing.SIMILAR_SCENE
	}

	common.WelsLog(pLogCtx, api.WELS_LOG_DEBUG, "iVaaFrameSceneChangeIdc = %d,codingIdx = %d", iVaaFrameSceneChangeIdc,
		pParamInternal.iCodingIndex)

	p.SaveBestRefToVaa(&sLtrSaved, &pVaaExt.sVaaStrBestRefCandidate[0])
	pVaaExt.iVaaBestRefFrameNum = sLtrSaved.pRefPicture.iFrameNum
	pVaaExt.pVaaBestBlockStaticIdc = sLtrSaved.pBestBlockStaticIdc

	if 0 < iAvailableSceneRefNum {
		p.SaveBestRefToVaa(&sSceneLtrSaved, &pVaaExt.sVaaLtrBestRefCandidate[0])
	}

	pVaaExt.iNumOfAvailableRef = 1
	return iVaaFrameSceneChangeIdc
}

// SPicture*& pRefOri -> **SPicture.
func (p *CWelsPreProcess) GetRefFrameInfo(iRefIdx int32, bCurrentFrameIsSceneLtr bool, pRefOri **SPicture) int32 {
	iTargetDid := p.m_pEncCtx.pSvcParam.ISpatialLayerNum - 1
	pVaaExt := p.m_pEncCtx.pVaa.pExt
	var pBestRefCandidateParam *SRefInfoParam
	if bCurrentFrameIsSceneLtr {
		pBestRefCandidateParam = &pVaaExt.sVaaLtrBestRefCandidate[iRefIdx]
	} else {
		pBestRefCandidateParam = &pVaaExt.sVaaStrBestRefCandidate[iRefIdx]
	}
	*pRefOri = p.m_pSpatialPic[iTargetDid][pBestRefCandidateParam.iSrcListIdx]
	return p.m_pSpatialPic[iTargetDid][pBestRefCandidateParam.iSrcListIdx].iLongTermPicNum
}

// planes -> (slice, offset).
func (p *CWelsPreProcess) Padding(pSrcY []uint8, iSrcYOff int, pSrcU []uint8, iSrcUOff int, pSrcV []uint8, iSrcVOff int, iStrideY int32, iStrideUV int32, iActualWidth int32, iPaddingWidth int32, iActualHeight int32, iPaddingHeight int32) {
	var i int32

	if iPaddingHeight > iActualHeight {
		for i = iActualHeight; i < iPaddingHeight; i++ {
			WelsMemset(pSrcY[iSrcYOff+int(i*iStrideY):], 0, uint32(iActualWidth))

			if (i & 1) == 0 {
				WelsMemset(pSrcU[iSrcUOff+int(i/2*iStrideUV):], 0x80, uint32(iActualWidth/2))
				WelsMemset(pSrcV[iSrcVOff+int(i/2*iStrideUV):], 0x80, uint32(iActualWidth/2))
			}
		}
	}

	if iPaddingWidth > iActualWidth {
		for i = 0; i < iPaddingHeight; i++ {
			WelsMemset(pSrcY[iSrcYOff+int(i*iStrideY+iActualWidth):], 0, uint32(iPaddingWidth-iActualWidth))
			if (i & 1) == 0 {
				WelsMemset(pSrcU[iSrcUOff+int(i/2*iStrideUV+iActualWidth/2):], 0x80, uint32((iPaddingWidth-iActualWidth)/2))
				WelsMemset(pSrcV[iSrcVOff+int(i/2*iStrideUV+iActualWidth/2):], 0x80, uint32((iPaddingWidth-iActualWidth)/2))
			}
		}
	}
}

// pCurBlockStaticPointer: one of SVAAFrameInfoExt.pVaaBlockStaticIdc[].
func (p *CWelsPreProcess) UpdateBlockIdcForScreen(pCurBlockStaticPointer []uint8, kpRefPic *SPicture, kpSrcPic *SPicture) int32 {
	iSceneChangeMethodIdx := processing.METHOD_SCENE_CHANGE_DETECTION_SCREEN
	sSceneChangeResult := processing.SSceneChangeResult{ESceneChangeIdc: processing.SIMILAR_SCENE}
	sSceneChangeResult.PStaticBlockIdc = pCurBlockStaticPointer
	sSceneChangeResult.SScrollResult.BScrollDetectFlag = false

	var sSrcMap processing.SPixMap
	var sRefMap processing.SPixMap
	p.InitPixMap(kpSrcPic, &sSrcMap)
	p.InitPixMap(kpRefPic, &sRefMap)

	p.m_pInterfaceVp.Set(iSceneChangeMethodIdx, &sSceneChangeResult)
	iRet := p.m_pInterfaceVp.Process(iSceneChangeMethodIdx, &sSrcMap, &sRefMap)
	if iRet == 0 {
		p.m_pInterfaceVp.Get(iSceneChangeMethodIdx, &sSceneChangeResult)
		return 0
	}
	return iRet
}

/*!
* \brief    exchange two picture pData planes
* \param    ppPic1      picture pointer to picture 1
* \param    ppPic2      picture pointer to picture 2
* \return   none
 */
func (p *CWelsPreProcess) WelsExchangeSpatialPictures(ppPic1 **SPicture, ppPic2 **SPicture) {
	tmp := *ppPic1

	// assert (*ppPic1 != *ppPic2);

	*ppPic1 = *ppPic2
	*ppPic2 = tmp
}

// SPicture** pLongRefList -> []*SPicture.
func (p *CWelsPreProcess) UpdateSrcListLosslessScreenRefSelectionWithLtr(pCurPicture *SPicture, kiCurDid int32, kuiMarkLongTermPicIdx int32, pLongRefList []*SPicture) {
	pLongRefSrcList := &p.m_pSpatialPic[kiCurDid]
	for i := 0; i < MAX_REF_PIC_COUNT; i++ {
		if nil == pLongRefSrcList[i+1] || (nil != pLongRefList[i] && pLongRefList[i].bUsedAsRef &&
			pLongRefList[i].bIsLongRef) {
			continue
		} else {
			pLongRefSrcList[i+1].SetUnref()
		}
	}
	p.WelsExchangeSpatialPictures(&p.m_pSpatialPic[kiCurDid][0],
		&p.m_pSpatialPic[kiCurDid][1+kuiMarkLongTermPicIdx])
	p.m_iAvaliableRefInSpatialPicList = MAX_REF_PIC_COUNT
	(p.GetCurrentOrigFrame(kiCurDid)).SetUnref()
}

func (p *CWelsPreProcess) UpdateSrcList(pCurPicture *SPicture, kiCurDid int32, pShortRefList []*SPicture, kuiShortRefCount uint32) {
	pRefSrcList := &p.m_pSpatialPic[kiCurDid]

	//pRefSrcList[0] is for current frame
	if pCurPicture.bUsedAsRef || pCurPicture.bIsLongRef {
		if pCurPicture.iPictureType == int32(common.P_SLICE) && pCurPicture.uiTemporalId != 0 {
			for iRefIdx := int32(kuiShortRefCount - 1); iRefIdx >= 0; iRefIdx-- {
				p.WelsExchangeSpatialPictures(&pRefSrcList[iRefIdx+1],
					&pRefSrcList[iRefIdx])
			}
			p.m_iAvaliableRefInSpatialPicList = int32(kuiShortRefCount)
		} else {
			p.WelsExchangeSpatialPictures(&pRefSrcList[0], &pRefSrcList[1])
			for i := MAX_SHORT_REF_COUNT - 1; i > 0; i-- {
				if pRefSrcList[i+1] != nil {
					pRefSrcList[i+1].SetUnref()
				}
			}
			p.m_iAvaliableRefInSpatialPicList = 1
		}
	}
	(p.GetCurrentOrigFrame(kiCurDid)).SetUnref()
}

//TODO: may opti later
//TODO: not use this func?

// void* memcpy / memset helpers on byte buffers (return dst / p like C).
func WelsMemcpy(dst []uint8, kpSrc []uint8, uiSize uint32) []uint8 {
	copy(dst[:uiSize], kpSrc[:uiSize])
	return dst
}

func WelsMemset(p []uint8, val int32, uiSize uint32) []uint8 {
	b := uint8(val)
	s := p[:uiSize]
	for i := range s {
		s[i] = b
	}
	return p
}

// i420_to_i420_c
// planes -> (slice, offset).
func WelsMoveMemory_c(pDstY []uint8, iDstYOff int, pDstU []uint8, iDstUOff int, pDstV []uint8, iDstVOff int, iDstStrideY int32, iDstStrideU int32, iDstStrideV int32, pSrcY []uint8, iSrcYOff int, pSrcU []uint8, iSrcUOff int, pSrcV []uint8, iSrcVOff int, iSrcStrideY int32, iSrcStrideU int32, iSrcStrideV int32, iWidth int32, iHeight int32) {
	iWidth2 := iWidth >> 1
	iHeight2 := iHeight >> 1
	var j int32

	for j = iHeight; j != 0; j-- {
		WelsMemcpy(pDstY[iDstYOff:], pSrcY[iSrcYOff:], uint32(iWidth))
		iDstYOff += int(iDstStrideY)
		iSrcYOff += int(iSrcStrideY)
	}

	for j = iHeight2; j != 0; j-- {
		WelsMemcpy(pDstU[iDstUOff:], pSrcU[iSrcUOff:], uint32(iWidth2))
		WelsMemcpy(pDstV[iDstVOff:], pSrcV[iSrcVOff:], uint32(iWidth2))
		iDstUOff += int(iDstStrideU)
		iDstVOff += int(iDstStrideV)
		iSrcUOff += int(iSrcStrideU)
		iSrcVOff += int(iSrcStrideV)
	}
}

func (p *CWelsPreProcess) WelsMoveMemoryWrapper(pSvcParam *SWelsSvcCodingParam, pDstPic *SPicture, kpSrc *api.SSourcePicture, kiTargetWidth int32, kiTargetHeight int32) int32 {
	if processing.VIDEO_FORMAT_I420 != (kpSrc.IColorFormat &^ processing.VIDEO_FORMAT_VFlip) {
		return ENC_RETURN_INVALIDINPUT
	}

	iSrcWidth := kpSrc.IPicWidth
	iSrcHeight := kpSrc.IPicHeight

	if iSrcHeight > kiTargetHeight {
		iSrcHeight = kiTargetHeight
	}
	if iSrcWidth > kiTargetWidth {
		iSrcWidth = kiTargetWidth
	}

	// copy from fr26 to fix the odd uiSize failed issue
	if iSrcWidth&0x1 != 0 {
		iSrcWidth--
	}
	if iSrcHeight&0x1 != 0 {
		iSrcHeight--
	}

	kiSrcTopOffsetY := pSvcParam.SUsedPicRect.iTop
	kiSrcTopOffsetUV := (kiSrcTopOffsetY >> 1)
	kiSrcLeftOffsetY := pSvcParam.SUsedPicRect.iLeft
	kiSrcLeftOffsetUV := (kiSrcLeftOffsetY >> 1)
	var iSrcOffset [3]int32
	iSrcOffset[0] = kpSrc.IStride[0]*kiSrcTopOffsetY + kiSrcLeftOffsetY
	iSrcOffset[1] = kpSrc.IStride[1]*kiSrcTopOffsetUV + kiSrcLeftOffsetUV
	iSrcOffset[2] = kpSrc.IStride[2]*kiSrcTopOffsetUV + kiSrcLeftOffsetUV

	// kpSrc->pData[i] + iSrcOffset[i]: (slice, offset)
	pSrcY, iSrcYOff := kpSrc.PData[0], int(iSrcOffset[0])
	pSrcU, iSrcUOff := kpSrc.PData[1], int(iSrcOffset[1])
	pSrcV, iSrcVOff := kpSrc.PData[2], int(iSrcOffset[2])
	kiSrcStrideY := kpSrc.IStride[0]
	kiSrcStrideU := kpSrc.IStride[1]
	kiSrcStrideV := kpSrc.IStride[2]

	pDstY, iDstYOff := pDstPic.pData[0], pDstPic.iDataOff[0]
	pDstU, iDstUOff := pDstPic.pData[1], pDstPic.iDataOff[1]
	pDstV, iDstVOff := pDstPic.pData[2], pDstPic.iDataOff[2]
	kiDstStrideY := pDstPic.iLineSize[0]
	kiDstStrideU := pDstPic.iLineSize[1]
	kiDstStrideV := pDstPic.iLineSize[2]

	if pSrcY != nil {
		if iSrcWidth <= 0 || iSrcHeight <= 0 {
			return ENC_RETURN_INVALIDINPUT
		}
		if iSrcWidth > pDstPic.iWidthInPixel || iSrcHeight > pDstPic.iHeightInPixel {
			return ENC_RETURN_INVALIDINPUT
		}
		if kiSrcTopOffsetY >= iSrcHeight || kiSrcLeftOffsetY >= iSrcWidth || iSrcWidth > kiSrcStrideY ||
			(iSrcWidth>>1) > kiSrcStrideU || (iSrcWidth>>1) > kiSrcStrideV {
			return ENC_RETURN_INVALIDINPUT
		}
	}
	if pDstY != nil {
		if kiTargetWidth <= 0 || kiTargetHeight <= 0 {
			return ENC_RETURN_INVALIDINPUT
		}
		if kiTargetWidth > pDstPic.iWidthInPixel || kiTargetHeight > pDstPic.iHeightInPixel {
			return ENC_RETURN_INVALIDINPUT
		}
		if kiTargetWidth > kiDstStrideY || (kiTargetWidth>>1) > kiDstStrideU || (kiTargetWidth>>1) > kiDstStrideV {
			return ENC_RETURN_INVALIDINPUT
		}
	}

	if pSrcY == nil || pSrcU == nil || pSrcV == nil || pDstY == nil || pDstU == nil || pDstV == nil ||
		(iSrcWidth&1) != 0 || (iSrcHeight&1) != 0 {
		return ENC_RETURN_INVALIDINPUT
	} else {
		//i420_to_i420_c
		if !p.tryCopySource(pSvcParam, pDstPic, kpSrc, kiTargetWidth, kiTargetHeight) {
			WelsMoveMemory_c(pDstY, iDstYOff, pDstU, iDstUOff, pDstV, iDstVOff, kiDstStrideY, kiDstStrideU,
				kiDstStrideV, pSrcY, iSrcYOff, pSrcU, iSrcUOff, pSrcV, iSrcVOff, kiSrcStrideY,
				kiSrcStrideU, kiSrcStrideV, iSrcWidth, iSrcHeight)
		}

		//in VP Process
		if kiTargetWidth > iSrcWidth || kiTargetHeight > iSrcHeight {
			p.Padding(pDstY, iDstYOff, pDstU, iDstUOff, pDstV, iDstVOff, kiDstStrideY, kiDstStrideU, iSrcWidth,
				kiTargetWidth, iSrcHeight, kiTargetHeight)
		}
	}

	return ENC_RETURN_SUCCESS
}

func (p *CWelsPreProcess) GetSceneChangeFlag(eSceneChangeIdc processing.ESceneChangeIdc) bool {
	return eSceneChangeIdc == processing.LARGE_CHANGED_SCENE
}
