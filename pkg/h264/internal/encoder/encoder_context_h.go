// Port of codec/encoder/core/inc/encoder_context.h.

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// SRefList is the reference list for each quality layer in SVC.
type SRefList struct {
	pShortRefList   [1 + MAX_SHORT_REF_COUNT]*SPicture // reference list 0 - int16_t
	pLongRefList    [1 + MAX_REF_PIC_COUNT]*SPicture   // reference list 1 - int32_t
	pNextBuffer     *SPicture
	pRef            [1 + MAX_REF_PIC_COUNT]*SPicture // plus 1 for swap intend
	uiShortRefCount uint8
	uiLongRefCount  uint8 // dependend on pRef pic module
}

// SLTRState is the long term reference state of a dependency layer.
type SLTRState struct {
	// LTR mark feedback
	uiLtrMarkState     uint32 // LTR mark state, indicate whether there is a LTR mark feedback unsolved
	iLtrMarkFbFrameNum int32  // the unsolved LTR mark feedback, the marked iFrameNum feedback from decoder

	// LTR used as recovery reference
	iLastRecoverFrameNum int32 // reserve the last LTR or IDR recover iFrameNum
	iLastCorFrameNumDec  int32 // reserved the last correct position in decoder side, use to select valid LTR to recover or to decide the LTR mark validation
	iCurFrameNumInDec    int32 // current iFrameNum in decoder side, use to select valid LTR to recover or to decide the LTR mark validation

	// LTR mark
	iLTRMarkMode       int32 // direct mark or delay mark
	iLTRMarkSuccessNum int32 // successful marked num, for mark mode switch
	iCurLtrIdx         int32 // current int32_t term reference index to mark
	iLastLtrIdx        [api.MAX_TEMPORAL_LAYER_NUM]int32
	iSceneLtrIdx       int32 // related to Scene LTR, used by screen content

	uiLtrMarkInterval uint32 // the interval from the last int32_t term pRef mark

	bLTRMarkingFlag     bool // decide whether current frame marked as LTR
	bLTRMarkEnable      bool // when LTR is confirmed and the interval is no smaller than the marking period
	bReceivedT0LostFlag bool // indicate whether a t0 lost feedback is recieved, for LTR recovery
}

type SSpatialPicIndex struct {
	pSrc *SPicture // I420 based and after color space converted
	iDid int32     // dependency id
}

// SStrideTables holds stride tables for internal coding used.
type SStrideTables struct {
	pStrideDecBlockOffset [MAX_DEPENDENCY_LAYER][2][]int32 // C int32_t*: [iDid][tid==0][24 x 4]: luma+chroma= 24 x 4
	pStrideEncBlockOffset [MAX_DEPENDENCY_LAYER][]int32    // C int32_t*: [iDid][24 x 4]: luma+chroma= 24 x 4
	pMbIndexX             [MAX_DEPENDENCY_LAYER][]int16    // C int16_t*: [iDid][iMbX]: map for iMbX in each spatial layer coding
	pMbIndexY             [MAX_DEPENDENCY_LAYER][]int16    // C int16_t*: [iDid][iMbY]: map for iMbY in each spatial layer coding
}

// sWelsEncCtx is the encoder context (struct TagWelsEncCtx).
//
// Dropped members: pMemAlign (CMemoryAlign, see doc.go), mutexEncoderError
// (worker results are reduced after joining), the STAT_OUTPUT statistics
// (sStatData, sPerInfo) and the ENABLE_FRAME_DUMP flags (bDependencyRecFlag).
type sWelsEncCtx struct {
	sLogCtx common.SLogContext

	// Input
	pSvcParam *SWelsSvcCodingParam // SVC parameter, WelsSVCParamConfig in svc_param_settings.h

	pSadCostMb []int32 // C int32_t*: per MB SAD (SMB.pSadCost points into it)

	/* MVD cost tables for Inter MB */
	iMvRange            int32
	pMvdCostTable       []uint16 // C uint16_t*: [52] MVD cost tables (adaptive to spatial layers); centre of the table for QP q is at q*iMvdCostTableStride + iMvdCostTableSize
	iMvdCostTableSize   int32    // the size of above table
	iMvdCostTableStride int32    // the stride of above table

	pMvUnitBlock4x4         []SMVUnitXY // C SMVUnitXY*: (*pMvUnitBlock4x4[2])[MB_BLOCK4x4_NUM]; for store each 4x4 blocks' mv unit, the two swap after different d layer
	pRefIndexBlock4x4       []int8      // C int8_t*: (*pRefIndexBlock4x4[2])[MB_BLOCK8x8_NUM]; for store each 4x4 blocks' pRef index, the two swap after different d layer
	pNonZeroCountBlocks     []int8      // C int8_t*: (*pNonZeroCountBlocks)[MB_LUMA_CHROMA_BLOCK4x4_NUM];
	pIntra4x4PredModeBlocks []int8      // C int8_t*: (*pIntra4x4PredModeBlocks)[INTRA_4x4_MODE_NUM];

	// C SMB**: [MAX_DEPENDENCY_LAYER]; ppMbListD[0] is the allocation holding the
	// MBs of all layers, ppMbListD[i] the sub-slice of layer i (== ppDqLayerList[i].sMbDataP).
	ppMbListD [][]SMB

	pStrideTab         *SStrideTables // stride tables for internal coding used
	pFuncList          *SWelsFuncPtrList
	pSliceThreading    *SSliceThreading
	pTaskManage        IWelsTaskManage // was planning to put it under CWelsH264SVCEncoder but it may be updated (lock/no lock) when param is changed
	pReferenceStrategy IWelsReferenceStrategy

	// pointers
	pEncPic                   *SPicture   // pointer to current picture to be encoded
	pDecPic                   *SPicture   // pointer to current picture being reconstructed
	pRefPic                   *SPicture   // pointer to current reference picture
	pCurDqLayer               *SDqLayer   // DQ layer context used to being encoded currently, for reference base layer to refer: pCurDqLayer->pRefLayer if applicable
	ppDqLayerList             []*SDqLayer // C SDqLayer**: overall DQ layers encoded for storage
	ppRefPicListExt           []*SRefList // C SRefList**: reference picture list for SVC
	pRefList0                 [16]*SPicture
	pLtr                      []SLTRState // C SLTRState*: [MAX_DEPENDENCY_LAYER]
	bCurFrameMarkedAsSceneLtr bool

	// Derived
	eSliceType       common.EWelsSliceType                       // currently coding slice type
	eNalType         common.EWelsNalUnitType                     // NAL type
	eNalPriority     common.EWelsNalRefIdc                       // NAL_Reference_Idc currently
	eLastNalPriority [MAX_DEPENDENCY_LAYER]common.EWelsNalRefIdc // NAL_Reference_Idc in last frame
	iNumRef0         uint8

	uiDependencyId     uint8 // Idc of dependecy layer to be coded
	uiTemporalId       uint8 // Idc of temporal layer to be coded
	bNeedPrefixNalFlag bool  // whether add prefix nal

	// Rate control routine
	pWelsSvcRc                    []SWelsSvcRc // C SWelsSvcRc*: [MAX_DEPENDENCY_LAYER]
	bCheckWindowStatusRefreshFlag bool
	iCheckWindowStartTs           int64
	iCheckWindowCurrentTs         int64
	iCheckWindowInterval          int32
	iCheckWindowIntervalShift     int32
	bCheckWindowShiftResetFlag    bool
	iGlobalQp                     int32 // global qp

	// VAA
	// pVaa: VAA information of reference. For screen content it is the base of
	// an SVAAFrameInfoExt (allocate with NewSVAAFrameInfoExt and store
	// &ext.SVAAFrameInfo); static_cast<SVAAFrameInfoExt*>(pVaa) is pVaa.pExt.
	pVaa *SVAAFrameInfo
	pVpp *CWelsPreProcess // the base part of a CWelsPreProcessVideo / CWelsPreProcessScreen

	pSpsArray []SWelsSPS // C SWelsSPS*: MAX_SPS_COUNT by standard compatible
	pSps      *SWelsSPS  // points into pSpsArray
	pPPSArray []SWelsPPS // C SWelsPPS*: MAX_PPS_COUNT by standard compatible
	pPps      *SWelsPPS  // points into pPPSArray (always &pPPSArray[0])
	/* SVC only */
	pSubsetArray  []SSubsetSps // C SSubsetSps*: MAX_SPS_COUNT by standard compatible
	pSubsetSps    *SSubsetSps  // points into pSubsetArray
	iSpsNum       int32        // number of pSps used
	iSubsetSpsNum int32        // number of pSps used
	iPpsNum       int32        // number of pPps used

	// Output
	pOut         *SWelsEncoderOutput // for NAL raw pData (need allocating memory for sNalList internal)
	pFrameBs     []uint8             // C uint8_t*: restoring bitstream pBuffer of all NALs in a frame
	iFrameBsSize int32               // count size of frame bs in bytes allocated
	iPosBsBuffer int32               // current writing position of frame bs pBuffer

	sSpatialIndexMap [MAX_DEPENDENCY_LAYER]SSpatialPicIndex

	iSliceBufferSize [MAX_DEPENDENCY_LAYER]int32

	bRefOfCurTidIsLtr [MAX_DEPENDENCY_LAYER][MAX_TEMPORAL_LEVEL]bool

	iMaxSliceCount    int32 // maximal count number of slices for all layers observation
	iActiveThreadsNum int16 // number of threads active so far

	/*
	 * DQ layer idc map for svc encoding, might be a better scheme than that of design before,
	 * can aware idc of referencing layer and that idc of successive layer to be coded
	 */
	/* SVC only */
	pDqIdcMap []SDqIdc // C SDqIdc*: overall DQ map of full scalability in specific frame (All full D/T/Q layers involved)

	sPSOVector SParaSetOffset
	pPSOVector *SParaSetOffset

	// related to Statistics
	uiStartTimestamp       int64
	sEncoderStatistics     [MAX_DEPENDENCY_LAYER]api.SEncoderStatistics
	iStatisticsLogInterval int32
	iLastStatisticsLogTs   int64

	iEncoderError int32
	bDeliveryFlag bool

	sWelsCabacContexts [4][WELS_QP_MAX + 1][common.WELS_CONTEXT_COUNT]SStateCtx

	uiLastTimestamp  int64
	pDynamicBsBuffer [MAX_THREADS_NUM][]uint8
}
