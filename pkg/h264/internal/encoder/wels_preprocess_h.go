// Port of codec/encoder/core/inc/wels_preprocess.h.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/processing"

// Scaled_Picture holds the down-scaled input picture.
type Scaled_Picture struct {
	pScaledInputPicture *SPicture
	iScaledWidth        [MAX_DEPENDENCY_LAYER]int32
	iScaledHeight       [MAX_DEPENDENCY_LAYER]int32
}

type SRefJudgement struct {
	iMinFrameComplexity   int64
	iMinFrameComplexity08 int64
	iMinFrameComplexity11 int64

	iMinFrameNumGap int32
	iMinFrameQp     int32
}

type SRefInfoParam struct {
	pRefPicture   *SPicture
	iSrcListIdx   int32 // idx in  h->spatial_pic[base_did];
	bSceneLtrFlag bool
	// C unsigned char*: one of SVAAFrameInfoExt.pVaaBlockStaticIdc[] (per 8x8 block, non-negative indexing)
	pBestBlockStaticIdc []uint8
}

// SVAAFrameInfo holds the video analysis results of the current frame.
type SVAAFrameInfo struct {
	sVaaCalcInfo             processing.SVAACalcResult
	sAdaptiveQuantParam      processing.SAdaptiveQuantizationParam
	sComplexityAnalysisParam processing.SComplexityAnalysisParam

	iPicWidth    int32 // maximal iWidth of picture in samples for svc coding
	iPicHeight   int32 // maximal iHeight of picture in samples for svc coding
	iPicStride   int32 // luma
	iPicStrideUV int32

	// C uint8_t* pRefY/pCurY/pRefU/pCurU/pRefV/pCurV: picture planes ->
	// (slice, offset) as SPicture.pData/iDataOff.
	pRefY    []uint8 // pRef
	iRefYOff int
	pCurY    []uint8 // cur
	iCurYOff int
	pRefU    []uint8 // pRef
	iRefUOff int
	pCurU    []uint8 // cur
	iCurUOff int
	pRefV    []uint8 // pRef
	iRefVOff int
	pCurV    []uint8 // cur
	iCurVOff int

	// C int8_t*: per MB background flag (whole array; functions taking a
	// pointer to the current MB's entry use (slice, offset)).
	pVaaBackgroundMbFlag  []int8
	uiValidLongTermPicIdx uint8
	uiMarkLongTermPicIdx  uint8

	eSceneChangeIdc  processing.ESceneChangeIdc
	bSceneChangeFlag bool
	bIdrPeriodFlag   bool

	// Go-only: when this SVAAFrameInfo is the base of an SVAAFrameInfoExt
	// (screen content), pExt points to it. C static_cast<SVAAFrameInfoExt*>(pVaa)
	// becomes pVaa.pExt.
	pExt *SVAAFrameInfoExt
}

// SVAAFrameInfoExt is SVAAFrameInfoExt_t: public SVAAFrameInfo (screen content).
// Create it with NewSVAAFrameInfoExt so that the embedded base points back to it.
type SVAAFrameInfoExt struct {
	SVAAFrameInfo

	sComplexityScreenParam  processing.SComplexityAnalysisScreenParam
	sScrollDetectInfo       processing.SScrollDetectionParam
	sVaaStrBestRefCandidate [MAX_REF_PIC_COUNT]SRefInfoParam
	sVaaLtrBestRefCandidate [MAX_REF_PIC_COUNT]SRefInfoParam
	iNumOfAvailableRef      int32

	iVaaBestRefFrameNum    int32
	pVaaBestBlockStaticIdc []uint8     // C uint8_t*: pointer to one of pVaaBlockStaticIdc
	pVaaBlockStaticIdc     [16][]uint8 // real memory,
}

// SVAAFrameInfoExt_t is the C struct tag name.
type SVAAFrameInfoExt_t = SVAAFrameInfoExt

// NewSVAAFrameInfoExt allocates a zeroed SVAAFrameInfoExt whose base
// SVAAFrameInfo.pExt points back to it.
func NewSVAAFrameInfoExt() *SVAAFrameInfoExt {
	p := &SVAAFrameInfoExt{}
	p.SVAAFrameInfo.pExt = p
	return p
}

// iCWelsPreProcessVirtual lists the virtual methods of CWelsPreProcess that
// the derived classes override.
type iCWelsPreProcessVirtual interface {
	GetCurrentOrigFrame(iDIdx int32) *SPicture
	DetectSceneChange(pCurPicture *SPicture, pRefPicture *SPicture) processing.ESceneChangeIdc
}

// CWelsPreProcess is the (abstract) pre-processing class. sWelsEncCtx.pVpp
// holds a *CWelsPreProcess whose pVirt is the derived object
// (*CWelsPreProcessVideo or *CWelsPreProcessScreen).
type CWelsPreProcess struct {
	vaaUnchanged vaaUnchangedFrameState
	sourceCopy   sourceCopyState

	// Go-only: the most-derived object, for virtual dispatch.
	pVirt iCWelsPreProcessVirtual

	// protected:
	m_pInterfaceVp              processing.IWelsVP
	m_pEncCtx                   *sWelsEncCtx
	m_uiSpatialLayersInTemporal [MAX_DEPENDENCY_LAYER]uint8

	// private:
	m_sScaledPicture      Scaled_Picture
	m_pLastSpatialPicture [MAX_DEPENDENCY_LAYER][2]*SPicture
	m_bInitDone           bool
	m_uiSpatialPicNum     [MAX_DEPENDENCY_LAYER]uint8

	// protected:
	/* For Downsampling & VAA I420 based source pictures */
	m_pSpatialPic [MAX_DEPENDENCY_LAYER][MAX_REF_PIC_COUNT + 1]*SPicture
	// need memory requirement with total number of num_of_ref + 1, "+1" is for current frame
	m_iAvaliableRefInSpatialPicList int32
}

// GetCurrentOrigFrame is the pure virtual CWelsPreProcess::GetCurrentOrigFrame
// (dispatches to the derived class).
func (p *CWelsPreProcess) GetCurrentOrigFrame(iDIdx int32) *SPicture {
	return p.pVirt.GetCurrentOrigFrame(iDIdx)
}

// DetectSceneChange is the pure virtual CWelsPreProcess::DetectSceneChange
// (dispatches to the derived class). C default argument pRefPicture = NULL.
func (p *CWelsPreProcess) DetectSceneChange(pCurPicture *SPicture, pRefPicture *SPicture) processing.ESceneChangeIdc {
	return p.pVirt.DetectSceneChange(pCurPicture, pRefPicture)
}

// CWelsPreProcessVideo is the camera video pre-processing.
type CWelsPreProcessVideo struct {
	CWelsPreProcess
}

// NewCWelsPreProcessVideo is the C++ constructor CWelsPreProcessVideo (pEncCtx).
func NewCWelsPreProcessVideo(pEncCtx *sWelsEncCtx) *CWelsPreProcessVideo {
	p := &CWelsPreProcessVideo{}
	p.pVirt = p
	p.ctorCWelsPreProcess(pEncCtx)
	return p
}

// CWelsPreProcessScreen is the screen content pre-processing.
type CWelsPreProcessScreen struct {
	CWelsPreProcess
}

// NewCWelsPreProcessScreen is the C++ constructor CWelsPreProcessScreen (pEncCtx).
func NewCWelsPreProcessScreen(pEncCtx *sWelsEncCtx) *CWelsPreProcessScreen {
	p := &CWelsPreProcessScreen{}
	p.pVirt = p
	p.ctorCWelsPreProcess(pEncCtx)
	return p
}
