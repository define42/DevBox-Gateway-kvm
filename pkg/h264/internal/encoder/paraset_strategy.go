// Port of codec/encoder/core/src/paraset_strategy.cpp.
//
// Parameter set (SPS/PPS) id strategies.

package encoder

import (
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// C memory layout emulation.
//
// Several strategies index SParaSetOffsetVariable.iParaSetIdDelta
// (MAX_DQ_LAYER_NUM entries) and SParaSetOffset.bPpsIdMappingIntoSubsetsps
// (MAX_DQ_LAYER_NUM entries) with SPS/PPS ids that can reach MAX_SPS_COUNT /
// MAX_PPS_COUNT (SPS listing strategies). In C this silently reads/writes the
// following members of SParaSetOffset. The Go structs have exactly the C
// layout (no field reordering, same sizes and alignments), so these accesses
// are reproduced through views that extend to the end of the enclosing
// SParaSetOffset (and never beyond it).

// psoDeltaView returns int32_t* &pso->sParaSetOffsetVariable[iParasetType].iParaSetIdDelta[0]
// as a slice reaching to the end of *pso.
func psoDeltaView(pso *SParaSetOffset, iParasetType int32) []int32 {
	p := &pso.sParaSetOffsetVariable[iParasetType].iParaSetIdDelta[0]
	iOff := uintptr(unsafe.Pointer(p)) - uintptr(unsafe.Pointer(pso))
	iLen := (unsafe.Sizeof(*pso) - iOff) / unsafe.Sizeof(int32(0))
	return unsafe.Slice(p, iLen)
}

// psoPpsMappingView returns bool* pso->bPpsIdMappingIntoSubsetsps as a slice
// reaching to the end of *pso.
func psoPpsMappingView(pso *SParaSetOffset) []bool {
	p := &pso.bPpsIdMappingIntoSubsetsps[0]
	iOff := uintptr(unsafe.Pointer(p)) - uintptr(unsafe.Pointer(pso))
	return unsafe.Slice(p, unsafe.Sizeof(*pso)-iOff)
}

// CreateParametersetStrategy is static IWelsParametersetStrategy::CreateParametersetStrategy.
func CreateParametersetStrategy(eSpsPpsIdStrategy api.EParameterSetStrategy, bSimulcastAVC bool, kiSpatialLayerNum int32) IWelsParametersetStrategy {
	var pParametersetStrategy IWelsParametersetStrategy
	switch eSpsPpsIdStrategy {
	case api.INCREASING_ID:
		pParametersetStrategy = NewCWelsParametersetIdIncreasing(bSimulcastAVC, kiSpatialLayerNum)
	case api.SPS_LISTING:
		pParametersetStrategy = NewCWelsParametersetSpsListing(bSimulcastAVC, kiSpatialLayerNum)
	case api.SPS_LISTING_AND_PPS_INCREASING:
		pParametersetStrategy = NewCWelsParametersetSpsListingPpsIncreasing(bSimulcastAVC, kiSpatialLayerNum)
	case api.SPS_PPS_LISTING:
		pParametersetStrategy = NewCWelsParametersetSpsPpsListing(bSimulcastAVC, kiSpatialLayerNum)
	case api.CONSTANT_ID:
		fallthrough
	default:
		pParametersetStrategy = NewCWelsParametersetIdConstant(bSimulcastAVC, kiSpatialLayerNum)
	}

	return pParametersetStrategy
}

func WelsGenerateNewSps(pCtx *sWelsEncCtx, kbUseSubsetSps bool, iDlayerIndex int32,
	iDlayerCount int32, kiSpsId int32,
	pSps **SWelsSPS, pSubsetSps **SSubsetSps, bSVCBaselayer bool) int32 {
	var iRet int32

	if !kbUseSubsetSps {
		*pSps = &pCtx.pSpsArray[kiSpsId]
	} else {
		*pSubsetSps = &pCtx.pSubsetArray[kiSpsId]
		*pSps = &(*pSubsetSps).pSps
	}

	pParam := pCtx.pSvcParam
	pDlayerParam := &pParam.SSpatialLayers[iDlayerIndex]
	// Need port pSps/pPps initialization due to spatial scalability changed
	if !kbUseSubsetSps {
		iRet = WelsInitSps(*pSps, pDlayerParam, &pParam.sDependencyLayers[iDlayerIndex], pParam.UiIntraPeriod,
			pParam.iMaxNumRefFrame,
			uint32(kiSpsId), pParam.BEnableFrameCroppingFlag, pParam.IRCMode != api.RC_OFF_MODE, iDlayerCount,
			bSVCBaselayer)
	} else {
		iRet = WelsInitSubsetSps(*pSubsetSps, pDlayerParam, &pParam.sDependencyLayers[iDlayerIndex], pParam.UiIntraPeriod,
			pParam.iMaxNumRefFrame,
			uint32(kiSpsId), pParam.BEnableFrameCroppingFlag, pParam.IRCMode != api.RC_OFF_MODE, iDlayerCount)
	}
	return iRet
}

func CheckMatchedSps(pSps1 *SWelsSPS, pSps2 *SWelsSPS) bool {

	if (pSps1.iMbWidth != pSps2.iMbWidth) ||
		(pSps1.iMbHeight != pSps2.iMbHeight) {
		return false
	}

	if (pSps1.uiLog2MaxFrameNum != pSps2.uiLog2MaxFrameNum) ||
		(pSps1.iLog2MaxPocLsb != pSps2.iLog2MaxPocLsb) {
		return false
	}

	if pSps1.iNumRefFrames != pSps2.iNumRefFrames {
		return false
	}

	if (pSps1.bFrameCroppingFlag != pSps2.bFrameCroppingFlag) ||
		(pSps1.sFrameCrop.iCropLeft != pSps2.sFrameCrop.iCropLeft) ||
		(pSps1.sFrameCrop.iCropRight != pSps2.sFrameCrop.iCropRight) ||
		(pSps1.sFrameCrop.iCropTop != pSps2.sFrameCrop.iCropTop) ||
		(pSps1.sFrameCrop.iCropBottom != pSps2.sFrameCrop.iCropBottom) {
		return false
	}

	if (pSps1.uiProfileIdc != pSps2.uiProfileIdc) ||
		(pSps1.bConstraintSet0Flag != pSps2.bConstraintSet0Flag) ||
		(pSps1.bConstraintSet1Flag != pSps2.bConstraintSet1Flag) ||
		(pSps1.bConstraintSet2Flag != pSps2.bConstraintSet2Flag) ||
		(pSps1.bConstraintSet3Flag != pSps2.bConstraintSet3Flag) ||
		(pSps1.iLevelIdc != pSps2.iLevelIdc) {
		return false
	}

	return true
}

func CheckMatchedSubsetSps(pSubsetSps1 *SSubsetSps, pSubsetSps2 *SSubsetSps) bool {
	if !CheckMatchedSps(&pSubsetSps1.pSps, &pSubsetSps2.pSps) {
		return false
	}

	if (pSubsetSps1.sSpsSvcExt.iExtendedSpatialScalability != pSubsetSps2.sSpsSvcExt.iExtendedSpatialScalability) ||
		(pSubsetSps1.sSpsSvcExt.bAdaptiveTcoeffLevelPredFlag != pSubsetSps2.sSpsSvcExt.bAdaptiveTcoeffLevelPredFlag) ||
		(pSubsetSps1.sSpsSvcExt.bSeqTcoeffLevelPredFlag != pSubsetSps2.sSpsSvcExt.bSeqTcoeffLevelPredFlag) ||
		(pSubsetSps1.sSpsSvcExt.bSliceHeaderRestrictionFlag != pSubsetSps2.sSpsSvcExt.bSliceHeaderRestrictionFlag) {
		return false
	}

	return true
}

// FindExistingSps checks if the current parameter can found a presenting sps.
// Returns the found id, or INVALID_ID if no existing SPS matches.
func FindExistingSps(pParam *SWelsSvcCodingParam, kbUseSubsetSps bool, iDlayerIndex int32, iDlayerCount int32, iSpsNumInUse int32, pSpsArray []SWelsSPS, pSubsetArray []SSubsetSps, bSVCBaseLayer bool) int32 {
	pDlayerParam := &pParam.SSpatialLayers[iDlayerIndex]

	if !kbUseSubsetSps {
		var sTmpSps SWelsSPS
		WelsInitSps(&sTmpSps, pDlayerParam, &pParam.sDependencyLayers[iDlayerIndex], pParam.UiIntraPeriod,
			pParam.iMaxNumRefFrame,
			0, pParam.BEnableFrameCroppingFlag, pParam.IRCMode != api.RC_OFF_MODE, iDlayerCount,
			bSVCBaseLayer)
		for iId := int32(0); iId < iSpsNumInUse; iId++ {
			if CheckMatchedSps(&sTmpSps, &pSpsArray[iId]) {
				return iId
			}
		}
	} else {
		var sTmpSubsetSps SSubsetSps
		WelsInitSubsetSps(&sTmpSubsetSps, pDlayerParam, &pParam.sDependencyLayers[iDlayerIndex], pParam.UiIntraPeriod,
			pParam.iMaxNumRefFrame,
			0, pParam.BEnableFrameCroppingFlag, pParam.IRCMode != api.RC_OFF_MODE, iDlayerCount)

		for iId := int32(0); iId < iSpsNumInUse; iId++ {
			if CheckMatchedSubsetSps(&sTmpSubsetSps, &pSubsetArray[iId]) {
				return iId
			}
		}
	}

	return INVALID_ID
}

// constructor body CWelsParametersetIdConstant::CWelsParametersetIdConstant
func (p *CWelsParametersetIdConstant) ctorCWelsParametersetIdConstant(bSimulcastAVC bool, kiSpatialLayerNum int32) {
	p.m_sParaSetOffset = SParaSetOffset{}

	p.m_bSimulcastAVC = bSimulcastAVC
	p.m_iSpatialLayerNum = kiSpatialLayerNum

	p.m_iBasicNeededSpsNum = 1
	p.m_iBasicNeededPpsNum = uint32(1 + p.m_iSpatialLayerNum)
}

// destructor CWelsParametersetIdConstant::~CWelsParametersetIdConstant
func (p *CWelsParametersetIdConstant) Destruct() {
}

func (p *CWelsParametersetIdConstant) GetPpsIdOffset(iPpsId int32) int32 {
	return 0
}

func (p *CWelsParametersetIdConstant) GetSpsIdOffset(iPpsId int32, iSpsId int32) int32 {
	return 0
}

func (p *CWelsParametersetIdConstant) GetSpsIdOffsetList(iParasetType int32) []int32 {
	return psoDeltaView(&p.m_sParaSetOffset, iParasetType)
}

func (p *CWelsParametersetIdConstant) GetAllNeededParasetNum() uint32 {
	return (p.pVirt.GetNeededSpsNum() +
		p.pVirt.GetNeededSubsetSpsNum() +
		p.pVirt.GetNeededPpsNum())
}

func (p *CWelsParametersetIdConstant) GetNeededSpsNum() uint32 {
	if 0 >= p.m_sParaSetOffset.uiNeededSpsNum {
		var iMul uint32 = 1
		if p.m_bSimulcastAVC {
			iMul = uint32(p.m_iSpatialLayerNum)
		}
		p.m_sParaSetOffset.uiNeededSpsNum = p.m_iBasicNeededSpsNum * iMul
	}
	return p.m_sParaSetOffset.uiNeededSpsNum
}

func (p *CWelsParametersetIdConstant) GetNeededSubsetSpsNum() uint32 {
	if 0 >= p.m_sParaSetOffset.uiNeededSubsetSpsNum {
		if p.m_bSimulcastAVC {
			p.m_sParaSetOffset.uiNeededSubsetSpsNum = 0
		} else {
			p.m_sParaSetOffset.uiNeededSubsetSpsNum = uint32(p.m_iSpatialLayerNum - 1)
		}
	}
	return p.m_sParaSetOffset.uiNeededSubsetSpsNum
}

func (p *CWelsParametersetIdConstant) GetNeededPpsNum() uint32 {
	if 0 == p.m_sParaSetOffset.uiNeededPpsNum {
		var iMul uint32 = 1
		if p.m_bSimulcastAVC {
			iMul = uint32(p.m_iSpatialLayerNum)
		}
		p.m_sParaSetOffset.uiNeededPpsNum = p.m_iBasicNeededPpsNum * iMul
	}
	return p.m_sParaSetOffset.uiNeededPpsNum
}

func (p *CWelsParametersetIdConstant) LoadPrevious(pExistingParasetList *SExistingParasetList, pSpsArray []SWelsSPS, pSubsetArray []SSubsetSps, pPpsArray []SWelsPPS) {
}

func (p *CWelsParametersetIdConstant) Update(kuiId uint32, iParasetType int32) {
	p.m_sParaSetOffset = SParaSetOffset{}
}

func (p *CWelsParametersetIdConstant) GenerateNewSps(pCtx *sWelsEncCtx, kbUseSubsetSps bool, iDlayerIndex int32, iDlayerCount int32, kuiSpsId uint32, pSps **SWelsSPS, pSubsetSps **SSubsetSps, bSVCBaselayer bool) uint32 {
	WelsGenerateNewSps(pCtx, kbUseSubsetSps, iDlayerIndex,
		iDlayerCount, int32(kuiSpsId),
		pSps, pSubsetSps, bSVCBaselayer)
	return kuiSpsId
}

func (p *CWelsParametersetIdConstant) InitPps(pCtx *sWelsEncCtx, kiSpsId uint32, pSps *SWelsSPS, pSubsetSps *SSubsetSps, kuiPpsId uint32, kbDeblockingFilterPresentFlag bool, kbUsingSubsetSps bool, kbEntropyCodingModeFlag bool) uint32 {
	WelsInitPps(&pCtx.pPPSArray[kuiPpsId], pSps, pSubsetSps, kuiPpsId, true, kbUsingSubsetSps, kbEntropyCodingModeFlag)
	p.pVirt.SetUseSubsetFlag(kuiPpsId, kbUsingSubsetSps)
	return kuiPpsId
}

func (p *CWelsParametersetIdConstant) SetUseSubsetFlag(iPpsId uint32, bUseSubsetSps bool) {
	psoPpsMappingView(&p.m_sParaSetOffset)[iPpsId] = bUseSubsetSps
}

func (p *CWelsParametersetIdNonConstant) OutputCurrentStructure(pParaSetOffsetVariable *[PARA_SET_TYPE]SParaSetOffsetVariable, pPpsIdList *[MAX_DQ_LAYER_NUM][MAX_PPS_COUNT]int32, pCtx *sWelsEncCtx, pExistingParasetList *SExistingParasetList) {
	for k := 0; k < PARA_SET_TYPE; k++ {
		clear(p.m_sParaSetOffset.sParaSetOffsetVariable[k].bUsedParaSetIdInBs[:])
	}
	*pParaSetOffsetVariable = p.m_sParaSetOffset.sParaSetOffsetVariable // confirmed_safe_unsafe_usage
}

func (p *CWelsParametersetIdNonConstant) LoadPreviousStructure(pParaSetOffsetVariable *[PARA_SET_TYPE]SParaSetOffsetVariable, pPpsIdList *[MAX_DQ_LAYER_NUM][MAX_PPS_COUNT]int32) {
	p.m_sParaSetOffset.sParaSetOffsetVariable = *pParaSetOffsetVariable // confirmed_safe_unsafe_usage
}

//
//CWelsParametersetIdIncreasing
//

// DebugSpsPps only checks assertions in _DEBUG builds of the C code.
func (p *CWelsParametersetIdIncreasing) DebugSpsPps(kiPpsId int32, kiSpsId int32) {
}

// DebugPps only checks assertions in _DEBUG builds of the C code.
func (p *CWelsParametersetIdIncreasing) DebugPps(kiPpsId int32) {
}

func ParasetIdAdditionIdAdjust(sParaSetOffsetVariable *SParaSetOffsetVariable, kiCurEncoderParaSetId int32, kuiMaxIdInBs uint32) {
	parasetIdAdditionIdAdjust(sParaSetOffsetVariable.iParaSetIdDelta[:], sParaSetOffsetVariable,
		kiCurEncoderParaSetId, kuiMaxIdInBs)
}

// parasetIdAdditionIdAdjust is ParasetIdAdditionIdAdjust with the delta array
// given as pDelta (possibly an extended view, see psoDeltaView).
func parasetIdAdditionIdAdjust(pDelta []int32, sParaSetOffsetVariable *SParaSetOffsetVariable,
	kiCurEncoderParaSetId int32, kuiMaxIdInBs uint32) { //paraset_type = 0: SPS; =1: PPS
	//SPS_ID in avc_sps and pSubsetSps will be different using this
	//SPS_ID case example:
	//1st enter:  next_spsid_in_bs == 0; spsid == 0; delta==0;            //actual spsid_in_bs == 0
	//1st finish: next_spsid_in_bs == 1;
	//2nd enter:  next_spsid_in_bs == 1; spsid == 0; delta==1;            //actual spsid_in_bs == 1
	//2nd finish: next_spsid_in_bs == 2;
	//31st enter: next_spsid_in_bs == 31; spsid == 0~2; delta==31~29;     //actual spsid_in_bs == 31
	//31st finish:next_spsid_in_bs == 0;
	//31st enter: next_spsid_in_bs == 0; spsid == 0~2; delta==-2~0;       //actual spsid_in_bs == 0
	//31st finish:next_spsid_in_bs == 1;

	kiEncId := kiCurEncoderParaSetId
	uiNextIdInBs := sParaSetOffsetVariable.uiNextParaSetIdToUseInBs

	//update current layer's pCodingParam
	pDelta[kiEncId] = int32(uiNextIdInBs - uint32(kiEncId)) //for current parameter set, change its id_delta
	//write pso pData for next update:
	sParaSetOffsetVariable.bUsedParaSetIdInBs[uiNextIdInBs] = true //   update current used_id

	//prepare for next update:
	//   find the next avaibable iId
	uiNextIdInBs++
	if uiNextIdInBs >= kuiMaxIdInBs {
		uiNextIdInBs = 0 //ensure the SPS_ID wound not exceed MAX_SPS_COUNT
	}
	//   update next_id
	sParaSetOffsetVariable.uiNextParaSetIdToUseInBs = uiNextIdInBs
}

func psoMaxIdInBs(iParasetType int32) uint32 {
	if iParasetType != PARA_SET_TYPE_PPS {
		return common.MAX_SPS_COUNT
	}
	return MAX_PPS_COUNT
}

func (p *CWelsParametersetIdIncreasing) Update(kuiId uint32, iParasetType int32) {
	parasetIdAdditionIdAdjust(psoDeltaView(&p.m_sParaSetOffset, iParasetType),
		&p.m_sParaSetOffset.sParaSetOffsetVariable[iParasetType],
		int32(kuiId),
		psoMaxIdInBs(iParasetType))
}

//((SPS_PPS_LISTING != pEncCtx->pSvcParam->eSpsPpsIdStrategy) ? (&
//  (pEncCtx->sPSOVector.sParaSetOffsetVariable[PARA_SET_TYPE_PPS].iParaSetIdDelta[0])) : NULL)

func (p *CWelsParametersetIdIncreasing) GetPpsIdOffset(kiPpsId int32) int32 {
	return psoDeltaView(&p.m_sParaSetOffset, PARA_SET_TYPE_PPS)[kiPpsId]
}

func (p *CWelsParametersetIdIncreasing) GetSpsIdOffset(kiPpsId int32, kiSpsId int32) int32 {
	var kiParameterSetType int32 = PARA_SET_TYPE_AVCSPS
	if psoPpsMappingView(&p.m_sParaSetOffset)[kiPpsId] {
		kiParameterSetType = PARA_SET_TYPE_SUBSETSPS
	}
	return psoDeltaView(&p.m_sParaSetOffset, kiParameterSetType)[kiSpsId]
}

//
//CWelsParametersetSpsListing
//

// constructor body CWelsParametersetSpsListing::CWelsParametersetSpsListing (runs ctorCWelsParametersetIdNonConstant first)
func (p *CWelsParametersetSpsListing) ctorCWelsParametersetSpsListing(bSimulcastAVC bool, kiSpatialLayerNum int32) {
	p.ctorCWelsParametersetIdNonConstant(bSimulcastAVC, kiSpatialLayerNum)

	p.m_sParaSetOffset = SParaSetOffset{}

	p.m_bSimulcastAVC = bSimulcastAVC
	p.m_iSpatialLayerNum = kiSpatialLayerNum

	p.m_iBasicNeededSpsNum = common.MAX_SPS_COUNT
	p.m_iBasicNeededPpsNum = 1
}

func (p *CWelsParametersetSpsListing) GetNeededSubsetSpsNum() uint32 {
	if 0 >= p.m_sParaSetOffset.uiNeededSubsetSpsNum {
		//      sPSOVector.uiNeededSubsetSpsNum = ((pSvcParam->bSimulcastAVC) ? (0) :((SPS_LISTING & pSvcParam->eSpsPpsIdStrategy) ? (MAX_SPS_COUNT) : (pSvcParam->iSpatialLayerNum - 1)));
		if p.m_bSimulcastAVC {
			p.m_sParaSetOffset.uiNeededSubsetSpsNum = 0
		} else {
			p.m_sParaSetOffset.uiNeededSubsetSpsNum = common.MAX_SPS_COUNT
		}
	}
	return p.m_sParaSetOffset.uiNeededSubsetSpsNum
}

func (p *CWelsParametersetSpsListing) LoadPreviousSps(pExistingParasetList *SExistingParasetList, pSpsArray []SWelsSPS, pSubsetArray []SSubsetSps) {
	//if ((SPS_LISTING & pParam->eSpsPpsIdStrategy) && (NULL != pExistingParasetList)) {
	p.m_sParaSetOffset.uiInUseSpsNum = pExistingParasetList.uiInUseSpsNum
	copy(pSpsArray[:common.MAX_SPS_COUNT], pExistingParasetList.sSps[:])

	if p.pVirt.GetNeededSubsetSpsNum() > 0 {
		p.m_sParaSetOffset.uiInUseSubsetSpsNum = pExistingParasetList.uiInUseSubsetSpsNum
		copy(pSubsetArray[:common.MAX_SPS_COUNT], pExistingParasetList.sSubsetSps[:])
	} else {
		p.m_sParaSetOffset.uiInUseSubsetSpsNum = 0
	}
	//}
}

func (p *CWelsParametersetSpsListing) LoadPrevious(pExistingParasetList *SExistingParasetList, pSpsArray []SWelsSPS, pSubsetArray []SSubsetSps, pPpsArray []SWelsPPS) {
	if nil == pExistingParasetList {
		return
	}
	p.pVirt.LoadPreviousSps(pExistingParasetList, pSpsArray, pSubsetArray)
	p.pVirt.LoadPreviousPps(pExistingParasetList, pPpsArray)
}

func (p *CWelsParametersetSpsListing) CheckParamCompatibility(pCodingParam *SWelsSvcCodingParam, pLogCtx *common.SLogContext) bool {
	if pCodingParam.ISpatialLayerNum > 1 && (!pCodingParam.BSimulcastAVC) {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
			"ParamValidationExt(), eSpsPpsIdStrategy setting (%d) with multiple svc SpatialLayers (%d) not supported! eSpsPpsIdStrategy adjusted to CONSTANT_ID",
			int32(pCodingParam.ESpsPpsIdStrategy), pCodingParam.ISpatialLayerNum)
		pCodingParam.ESpsPpsIdStrategy = api.CONSTANT_ID
		return false
	}
	return true
}

func (p *CWelsParametersetSpsListing) CheckPpsGenerating() bool {
	return true
}

func (p *CWelsParametersetSpsListing) SpsReset(pCtx *sWelsEncCtx, kbUseSubsetSps bool) int32 {

	// reset current list
	if !kbUseSubsetSps {
		p.m_sParaSetOffset.uiInUseSpsNum = 1
		clear(pCtx.pSpsArray[:common.MAX_SPS_COUNT])
	} else {
		p.m_sParaSetOffset.uiInUseSubsetSpsNum = 1
		clear(pCtx.pSubsetArray[:common.MAX_SPS_COUNT])
	}

	//iSpsId = 0;
	return 0
}

func (p *CWelsParametersetSpsListing) GenerateNewSps(pCtx *sWelsEncCtx, kbUseSubsetSps bool, iDlayerIndex int32, iDlayerCount int32, kuiSpsId uint32, pSps **SWelsSPS, pSubsetSps **SSubsetSps, bSvcBaselayer bool) uint32 {
	//check if the current param can fit in an existing SPS
	iSpsNumInUse := p.m_sParaSetOffset.uiInUseSpsNum
	if kbUseSubsetSps {
		iSpsNumInUse = p.m_sParaSetOffset.uiInUseSubsetSpsNum
	}
	kiFoundSpsId := FindExistingSps(pCtx.pSvcParam, kbUseSubsetSps, iDlayerIndex, iDlayerCount,
		int32(iSpsNumInUse),
		pCtx.pSpsArray,
		pCtx.pSubsetArray, bSvcBaselayer)

	if INVALID_ID != kiFoundSpsId {
		//if yes, set pSps or pSubsetSps to it
		kuiSpsId = uint32(kiFoundSpsId)
		if !kbUseSubsetSps {
			*pSps = &pCtx.pSpsArray[kiFoundSpsId]
		} else {
			*pSubsetSps = &pCtx.pSubsetArray[kiFoundSpsId]
		}
	} else {
		//if no, generate a new SPS as usual
		if !p.pSpsListingVirt.CheckPpsGenerating() {
			return ^uint32(0) // -1
		}

		if !kbUseSubsetSps {
			kuiSpsId = p.m_sParaSetOffset.uiInUseSpsNum
			p.m_sParaSetOffset.uiInUseSpsNum++
		} else {
			kuiSpsId = p.m_sParaSetOffset.uiInUseSubsetSpsNum
			p.m_sParaSetOffset.uiInUseSubsetSpsNum++
		}
		if kuiSpsId >= common.MAX_SPS_COUNT {
			if p.pSpsListingVirt.SpsReset(pCtx, kbUseSubsetSps) < 0 {
				return ^uint32(0) // -1
			}
			kuiSpsId = 0
		}

		WelsGenerateNewSps(pCtx, kbUseSubsetSps, iDlayerIndex,
			iDlayerCount, int32(kuiSpsId), pSps, pSubsetSps, bSvcBaselayer)
	}
	return kuiSpsId
}

func (p *CWelsParametersetSpsListing) UpdateParaSetNum(pCtx *sWelsEncCtx) {
	pCtx.iSpsNum = int32(p.m_sParaSetOffset.uiInUseSpsNum)
	pCtx.iSubsetSpsNum = int32(p.m_sParaSetOffset.uiInUseSubsetSpsNum)
}

func (p *CWelsParametersetSpsListing) OutputCurrentStructure(pParaSetOffsetVariable *[PARA_SET_TYPE]SParaSetOffsetVariable, pPpsIdList *[MAX_DQ_LAYER_NUM][MAX_PPS_COUNT]int32, pCtx *sWelsEncCtx, pExistingParasetList *SExistingParasetList) {
	p.CWelsParametersetIdNonConstant.OutputCurrentStructure(pParaSetOffsetVariable, pPpsIdList, pCtx, pExistingParasetList)
	pExistingParasetList.uiInUseSpsNum = p.m_sParaSetOffset.uiInUseSpsNum

	copy(pExistingParasetList.sSps[:], pCtx.pSpsArray[:common.MAX_SPS_COUNT])
	if nil != pCtx.pSubsetArray {
		pExistingParasetList.uiInUseSubsetSpsNum = p.m_sParaSetOffset.uiInUseSubsetSpsNum
		copy(pExistingParasetList.sSubsetSps[:], pCtx.pSubsetArray[:common.MAX_SPS_COUNT])
	} else {
		pExistingParasetList.uiInUseSubsetSpsNum = 0
	}
}

//
//CWelsParametersetSpsPpsListing
//

// constructor body CWelsParametersetSpsPpsListing::CWelsParametersetSpsPpsListing (runs ctorCWelsParametersetSpsListing first)
func (p *CWelsParametersetSpsPpsListing) ctorCWelsParametersetSpsPpsListing(bSimulcastAVC bool, kiSpatialLayerNum int32) {
	p.ctorCWelsParametersetSpsListing(bSimulcastAVC, kiSpatialLayerNum)

	p.m_sParaSetOffset = SParaSetOffset{}

	p.m_bSimulcastAVC = bSimulcastAVC
	p.m_iSpatialLayerNum = kiSpatialLayerNum

	p.m_iBasicNeededSpsNum = common.MAX_SPS_COUNT
	p.m_iBasicNeededPpsNum = MAX_PPS_COUNT
}

func (p *CWelsParametersetSpsPpsListing) LoadPreviousPps(pExistingParasetList *SExistingParasetList, pPpsArray []SWelsPPS) {
	// copy from existing if the pointer exists
	//if ((SPS_PPS_LISTING == pParam->eSpsPpsIdStrategy) && (NULL != pExistingParasetList)) {
	p.m_sParaSetOffset.uiInUsePpsNum = pExistingParasetList.uiInUsePpsNum
	copy(pPpsArray[:MAX_PPS_COUNT], pExistingParasetList.sPps[:])
	//}
}

//	if ((SPS_PPS_LISTING == pCtx->pSvcParam->eSpsPpsIdStrategy) && (pCtx->iPpsNum < MAX_PPS_COUNT)) {
//	  UpdatePpsList (pCtx);
//	}
func (p *CWelsParametersetSpsPpsListing) UpdatePpsList(pCtx *sWelsEncCtx) {
	if pCtx.iPpsNum >= MAX_PPS_COUNT {
		return
	}

	//Generate PPS LIST
	var iPpsId int32
	iUsePpsNum := pCtx.iPpsNum

	for iIdrRound := int32(0); iIdrRound < MAX_PPS_COUNT; iIdrRound++ {
		for iPpsId = 0; iPpsId < pCtx.iPpsNum; iPpsId++ {
			p.m_sParaSetOffset.iPpsIdList[iPpsId][iIdrRound] = ((iIdrRound*iUsePpsNum + iPpsId) % MAX_PPS_COUNT)
		}
	}

	for iPpsId = iUsePpsNum; iPpsId < MAX_PPS_COUNT; iPpsId++ {
		pCtx.pPPSArray[iPpsId] = pCtx.pPPSArray[iPpsId%iUsePpsNum]
		pCtx.pPPSArray[iPpsId].iPpsId = uint32(iPpsId)
		pCtx.iPpsNum++
	}

	p.m_sParaSetOffset.uiInUsePpsNum = uint32(pCtx.iPpsNum)
}

func (p *CWelsParametersetSpsPpsListing) CheckPpsGenerating() bool {
	/*if ((SPS_PPS_LISTING == pCtx->pSvcParam->eSpsPpsIdStrategy) && (MAX_PPS_COUNT <= pCtx->sPSOVector.uiInUsePpsNum)) {
	  //check if we can generate new SPS or not
	  WelsLog (& pCtx->sLogCtx, WELS_LOG_ERROR,
	           "InitDqLayers(), cannot generate new SPS under the SPS_PPS_LISTING mode!");
	  return ENC_RETURN_UNSUPPORTED_PARA;
	}*/
	if MAX_PPS_COUNT <= p.m_sParaSetOffset.uiInUsePpsNum {
		return false
	}

	return true
}

func (p *CWelsParametersetSpsPpsListing) SpsReset(pCtx *sWelsEncCtx, kbUseSubsetSps bool) int32 {
	/*        if (SPS_PPS_LISTING == pParam->eSpsPpsIdStrategy) {
	WelsLog (& (*ppCtx)->sLogCtx, WELS_LOG_ERROR,
	"InitDqLayers(), cannot generate new SPS under the SPS_PPS_LISTING mode!");
	return ENC_RETURN_UNSUPPORTED_PARA;
	}*/
	return -1
}

// FindExistingPps (DISABLE_FMO_FEATURE is defined, so the search runs).
func FindExistingPps(pSps *SWelsSPS, pSubsetSps *SSubsetSps, kbUseSubsetSps bool, iSpsId int32, kbEntropyCodingFlag bool, iPpsNumInUse int32, pPpsArray []SWelsPPS) int32 {
	var sTmpPps SWelsPPS
	WelsInitPps(&sTmpPps,
		pSps,
		pSubsetSps,
		0,
		true,
		kbUseSubsetSps,
		kbEntropyCodingFlag)

	for iId := int32(0); iId < iPpsNumInUse; iId++ {
		if (sTmpPps.iSpsId == pPpsArray[iId].iSpsId) &&
			(sTmpPps.bEntropyCodingModeFlag == pPpsArray[iId].bEntropyCodingModeFlag) &&
			(sTmpPps.iPicInitQp == pPpsArray[iId].iPicInitQp) &&
			(sTmpPps.iPicInitQs == pPpsArray[iId].iPicInitQs) &&
			(sTmpPps.uiChromaQpIndexOffset == pPpsArray[iId].uiChromaQpIndexOffset) &&
			(sTmpPps.bDeblockingFilterControlPresentFlag == pPpsArray[iId].bDeblockingFilterControlPresentFlag) {
			return iId
		}
	}

	return INVALID_ID
}

func (p *CWelsParametersetSpsPpsListing) InitPps(pCtx *sWelsEncCtx, kiSpsId uint32, pSps *SWelsSPS, pSubsetSps *SSubsetSps, kuiPpsId uint32, kbDeblockingFilterPresentFlag bool, kbUsingSubsetSps bool, kbEntropyCodingModeFlag bool) uint32 {
	kiFoundPpsId := FindExistingPps(pSps, pSubsetSps, kbUsingSubsetSps, int32(kiSpsId),
		kbEntropyCodingModeFlag,
		int32(p.m_sParaSetOffset.uiInUsePpsNum),
		pCtx.pPPSArray)

	if INVALID_ID != kiFoundPpsId {
		//if yes, set pPps to it
		kuiPpsId = uint32(kiFoundPpsId)
	} else {
		kuiPpsId = p.m_sParaSetOffset.uiInUsePpsNum
		p.m_sParaSetOffset.uiInUsePpsNum++
		WelsInitPps(&pCtx.pPPSArray[kuiPpsId], pSps, pSubsetSps, kuiPpsId, true, kbUsingSubsetSps, kbEntropyCodingModeFlag)
	}
	p.pVirt.SetUseSubsetFlag(kuiPpsId, kbUsingSubsetSps)
	return kuiPpsId
}

func (p *CWelsParametersetSpsPpsListing) UpdateParaSetNum(pCtx *sWelsEncCtx) {
	p.CWelsParametersetSpsListing.UpdateParaSetNum(pCtx)

	//UpdatePpsList (pCtx);
	pCtx.iPpsNum = int32(p.m_sParaSetOffset.uiInUsePpsNum)
}

func (p *CWelsParametersetSpsPpsListing) GetCurrentPpsId(iPpsId int32, iIdrLoop int32) int32 {
	return p.m_sParaSetOffset.iPpsIdList[iPpsId][iIdrLoop]
}

func (p *CWelsParametersetSpsPpsListing) LoadPreviousStructure(pParaSetOffsetVariable *[PARA_SET_TYPE]SParaSetOffsetVariable, pPpsIdList *[MAX_DQ_LAYER_NUM][MAX_PPS_COUNT]int32) {
	p.m_sParaSetOffset.sParaSetOffsetVariable = *pParaSetOffsetVariable // confirmed_safe_unsafe_usage

	p.m_sParaSetOffset.iPpsIdList = *pPpsIdList
}

func (p *CWelsParametersetSpsPpsListing) OutputCurrentStructure(pParaSetOffsetVariable *[PARA_SET_TYPE]SParaSetOffsetVariable, pPpsIdList *[MAX_DQ_LAYER_NUM][MAX_PPS_COUNT]int32, pCtx *sWelsEncCtx, pExistingParasetList *SExistingParasetList) {
	p.CWelsParametersetSpsListing.OutputCurrentStructure(pParaSetOffsetVariable, pPpsIdList, pCtx, pExistingParasetList)

	pExistingParasetList.uiInUsePpsNum = p.m_sParaSetOffset.uiInUsePpsNum
	// C copies from pCtx->pPps, which always points at pCtx->pPPSArray[0].
	copy(pExistingParasetList.sPps[:], pCtx.pPPSArray[:MAX_PPS_COUNT])
	*pPpsIdList = p.m_sParaSetOffset.iPpsIdList
}

//
//CWelsParametersetSpsListingPpsIncreasing
//

func (p *CWelsParametersetSpsListingPpsIncreasing) GetPpsIdOffset(kiPpsId int32) int32 {
	//same as CWelsParametersetIdIncreasing::GetPpsIdOffset
	return psoDeltaView(&p.m_sParaSetOffset, PARA_SET_TYPE_PPS)[kiPpsId]
}

func (p *CWelsParametersetSpsListingPpsIncreasing) Update(kuiId uint32, iParasetType int32) {
	//same as CWelsParametersetIdIncreasing::Update
	parasetIdAdditionIdAdjust(psoDeltaView(&p.m_sParaSetOffset, iParasetType),
		&p.m_sParaSetOffset.sParaSetOffsetVariable[iParasetType],
		int32(kuiId),
		psoMaxIdInBs(iParasetType))
}
