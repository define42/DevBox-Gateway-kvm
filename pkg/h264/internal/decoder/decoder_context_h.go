// Port of codec/decoder/core/inc/decoder_context.h.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const (
	MAX_PRED_MODE_ID_I16x16 = 3
	MAX_PRED_MODE_ID_CHROMA = 3
	MAX_PRED_MODE_ID_I4x4   = 8
	WELS_QP_MAX             = 51
)

// LONG_TERM_REF is defined in the C build: the #ifdef LONG_TERM_REF branches
// are the ones to port.
const LONG_TERM_REF = true

const IMinInt32 = -0x7FFFFFFF

// SWelsCabacCtx (SWels_Cabac_Element).
type SWelsCabacCtx struct {
	uiState uint8
	uiMPS   uint8
}

type PWelsCabacCtx = *SWelsCabacCtx

// SWelsCabacDecEngine. The three C buffer pointers point into the slice's
// bit-stream buffer: pBuff is that whole buffer (common.SBitStringAux.PBuf)
// and pBuffStart / pBuffCurr / pBuffEnd are int offsets into it.
type SWelsCabacDecEngine struct {
	uiRange    uint64
	uiOffset   uint64
	iBitsLeft  int32
	pBuff      []uint8
	pBuffStart int // offset into pBuff
	pBuffCurr  int // offset into pBuff
	pBuffEnd   int // offset into pBuff
}

type PWelsCabacDecEngine = *SWelsCabacDecEngine

const (
	NEW_CTX_OFFSET_MB_TYPE_I    = 3
	NEW_CTX_OFFSET_SKIP         = 11
	NEW_CTX_OFFSET_SUBMB_TYPE   = 21
	NEW_CTX_OFFSET_B_SUBMB_TYPE = 36
	NEW_CTX_OFFSET_MVD          = 40
	NEW_CTX_OFFSET_REF_NO       = 54
	NEW_CTX_OFFSET_DELTA_QP     = 60
	NEW_CTX_OFFSET_IPR          = 68
	NEW_CTX_OFFSET_CIPR         = 64
	NEW_CTX_OFFSET_CBP          = 73
	NEW_CTX_OFFSET_CBF          = 85
	NEW_CTX_OFFSET_MAP          = 105
	NEW_CTX_OFFSET_LAST         = 166
	NEW_CTX_OFFSET_ONE          = 227
	NEW_CTX_OFFSET_ABS          = 232
	NEW_CTX_OFFSET_TS_8x8_FLAG  = 399
	CTX_NUM_MVD                 = 7
	CTX_NUM_CBP                 = 4
	// Table 9-34 in Page 270
	NEW_CTX_OFFSET_TRANSFORM_SIZE_8X8_FLAG = 399
	NEW_CTX_OFFSET_MAP_8x8                 = 402
	NEW_CTX_OFFSET_LAST_8x8                = 417
	NEW_CTX_OFFSET_ONE_8x8                 = 426
	NEW_CTX_OFFSET_ABS_8x8                 = 431 // Puzzle, where is the definition?
)

// SDataBuffer (TagDataBuffer). pHead is the whole allocation (nil == NULL);
// pEnd, pStartPos and pCurPos are int offsets into pHead (so C's
// `pEnd - pCurPos` stays `pEnd - pCurPos` and `pCurPos[i]` becomes
// `pHead[pCurPos+i]`).
type SDataBuffer struct {
	pHead []uint8
	pEnd  int

	pStartPos int
	pCurPos   int
}

// limit size for SPS PPS total permitted size for parse_only
const SPS_PPS_BS_SIZE = 128

type SSpsBsInfo struct {
	pSpsBsBuf  [SPS_PPS_BS_SIZE]uint8
	iSpsId     int32
	uiSpsBsLen uint16
}

type SPpsBsInfo struct {
	pPpsBsBuf  [SPS_PPS_BS_SIZE]uint8
	iPpsId     int32
	uiPpsBsLen uint16
}

/*typedef for get intra predictor func pointer*/

// PGetIntraPredFunc: pPred is a (slice, offset) pair.
type PGetIntraPredFunc func(pPred []uint8, iPredOff int, kiLumaStride int32)

// PIdctResAddPredFunc: pPred is a (slice, offset) pair; pRs a coefficient sub-slice.
type PIdctResAddPredFunc func(pPred []uint8, iPredOff int, kiStride int32, pRs []int16)

// PIdctFourResAddPredFunc: pPred is a (slice, offset) pair; pRs / pNzc sub-slices.
type PIdctFourResAddPredFunc func(pPred []uint8, iPredOff int, iStride int32, pRs []int16, pNzc []int8)

// PExpandPictureFunc: pDst is a (slice, offset) pair.
type PExpandPictureFunc func(pDst []uint8, iDstOff int, kiStride int32, kiPicWidth int32, kiPicHeight int32)

// PGetIntraPred8x8Func: pPred is a (slice, offset) pair.
type PGetIntraPred8x8Func func(pPred []uint8, iPredOff int, kiLumaStride int32, bTLAvail bool, bTRAvail bool)

/**/
type SRefPic struct {
	pRefList             [common.LIST_A][MAX_DPB_COUNT]*SPicture // reference picture marking plus FIFO scheme
	pShortRefList        [common.LIST_A][MAX_DPB_COUNT]*SPicture
	pLongRefList         [common.LIST_A][MAX_DPB_COUNT]*SPicture
	uiRefCount           [common.LIST_A]uint8
	uiShortRefCount      [common.LIST_A]uint8
	uiLongRefCount       [common.LIST_A]uint8 // dependend on ref pic module
	iMaxLongTermFrameIdx int32
}

type PRefPic = *SRefPic

// PCopyFunc: pDst and pSrc are (slice, offset) pairs.
type PCopyFunc func(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32)

type SCopyFunc struct {
	pCopyLumaFunc   PCopyFunc
	pCopyChromaFunc PCopyFunc
}

//deblock module defination

// SDeblockingFilter (tagDeblockingFilter).
type SDeblockingFilter struct {
	// pCsData[i]: (slice, offset) pair - pointer to reconstructed picture
	// data: pCsData[i] is pDec.pData[i] (whole allocation) and iCsDataOff[i]
	// is pDec.iDataOff[i].
	pCsData             [3][]uint8
	iCsDataOff          [3]int
	iCsStride           [2]int32 // Cs stride
	eSliceType          common.EWelsSliceType
	iSliceAlphaC0Offset int8
	iSliceBetaOffset    int8
	iChromaQP           [2]int8
	iLumaQP             int8
	pLoopf              *SDeblockingFunc
	pRefPics            [common.LIST_A][]*SPicture // C: PPicture*, = pCtx.sRefPic.pRefList[i][:]
}

type PDeblockingFilter = *SDeblockingFilter

type PDeblockingFilterMbFunc func(pCurDqLayer *SDqLayer, filter *SDeblockingFilter, boundry_flag int32)

// Deblocking function pointers: every sample pointer is a (slice, offset) pair.
type PLumaDeblockingLT4Func func(iSampleY []uint8, iSampleYOff int, iStride int32, iAlpha int32, iBeta int32, iTc []int8)
type PLumaDeblockingEQ4Func func(iSampleY []uint8, iSampleYOff int, iStride int32, iAlpha int32, iBeta int32)
type PChromaDeblockingLT4Func func(iSampleCb []uint8, iSampleCbOff int, iSampleCr []uint8, iSampleCrOff int, iStride int32,
	iAlpha int32, iBeta int32, iTc []int8)
type PChromaDeblockingEQ4Func func(iSampleCb []uint8, iSampleCbOff int, iSampleCr []uint8, iSampleCrOff int, iStride int32,
	iAlpha int32, iBeta int32)
type PChromaDeblockingLT4Func2 func(iSampleCbr []uint8, iSampleCbrOff int, iStride int32, iAlpha int32, iBeta int32, iTc []int8)
type PChromaDeblockingEQ4Func2 func(iSampleCbr []uint8, iSampleCbrOff int, iStride int32, iAlpha int32, iBeta int32)

type SDeblockingFunc struct {
	pfLumaDeblockingLT4Ver PLumaDeblockingLT4Func
	pfLumaDeblockingEQ4Ver PLumaDeblockingEQ4Func
	pfLumaDeblockingLT4Hor PLumaDeblockingLT4Func
	pfLumaDeblockingEQ4Hor PLumaDeblockingEQ4Func

	pfChromaDeblockingLT4Ver PChromaDeblockingLT4Func
	pfChromaDeblockingEQ4Ver PChromaDeblockingEQ4Func
	pfChromaDeblockingLT4Hor PChromaDeblockingLT4Func
	pfChromaDeblockingEQ4Hor PChromaDeblockingEQ4Func

	pfChromaDeblockingLT4Ver2 PChromaDeblockingLT4Func2
	pfChromaDeblockingEQ4Ver2 PChromaDeblockingEQ4Func2
	pfChromaDeblockingLT4Hor2 PChromaDeblockingLT4Func2
	pfChromaDeblockingEQ4Hor2 PChromaDeblockingEQ4Func2
}

type PDeblockingFunc = *SDeblockingFunc

// PWelsNonZeroCountFunc: pNonZeroCount is a sub-slice (24 entries).
type PWelsNonZeroCountFunc func(pNonZeroCount []int8)

// PWelsBlockZeroFunc: block is a coefficient sub-slice.
type PWelsBlockZeroFunc func(block []int16, stride int32)

type SBlockFunc struct {
	pWelsSetNonZeroCountFunc PWelsNonZeroCountFunc
	pWelsBlockZero16x16Func  PWelsBlockZeroFunc
	pWelsBlockZero8x8Func    PWelsBlockZeroFunc
}

// pNonZeroCount / pIntraPredMode are the MB caches (sub-slices).
type PWelsFillNeighborMbInfoIntra4x4Func func(pNeighAvail *SWelsNeighAvail, pNonZeroCount []uint8, pIntraPredMode []int8,
	pCurDqLayer *SDqLayer)
type PWelsMapNeighToSample func(pNeighAvail *SWelsNeighAvail, pSampleAvail []int32)
type PWelsMap16NeighToSample func(pNeighAvail *SWelsNeighAvail, pSampleAvail *uint8)
type PWelsParseIntra4x4ModeFunc func(pNeighAvail *SWelsNeighAvail, pIntraPredMode []int8, pBs *common.SBitStringAux,
	pCurDqLayer *SDqLayer) int32
type PWelsParseIntra16x16ModeFunc func(pNeighAvail *SWelsNeighAvail, pBs *common.SBitStringAux, pCurDqLayer *SDqLayer) int32

const (
	OVERWRITE_NONE      = 0
	OVERWRITE_PPS       = 1
	OVERWRITE_SPS       = 1 << 1
	OVERWRITE_SUBSETSPS = 1 << 2
)

// Decoder SPS and PPS global CTX
type SWelsDecoderSpsPpsCTX struct {
	sFrameCrop SPosOffset

	sSpsBuffer [common.MAX_SPS_COUNT + 1]SSps
	sPpsBuffer [MAX_PPS_COUNT + 1]SPps

	sSubsetSpsBuffer [common.MAX_SPS_COUNT + 1]SSubsetSps
	sPrefixNal       SNalUnit

	pActiveLayerSps [MAX_LAYER_NUM]*SSps
	bAvcBasedFlag   bool // For decoding bitstream:

	// for EC parameter sets
	bSpsExistAheadFlag    bool // whether does SPS NAL exist ahead of sequence?
	bSubspsExistAheadFlag bool // whether does Subset SPS NAL exist ahead of sequence?
	bPpsExistAheadFlag    bool // whether does PPS NAL exist ahead of sequence?

	iSpsErrorIgnored    int32
	iSubSpsErrorIgnored int32
	iPpsErrorIgnored    int32

	bSpsAvailFlags       [common.MAX_SPS_COUNT]bool
	bSubspsAvailFlags    [common.MAX_SPS_COUNT]bool
	bPpsAvailFlags       [MAX_PPS_COUNT]bool
	iPPSLastInvalidId    int32
	iPPSInvalidNum       int32
	iSPSLastInvalidId    int32
	iSPSInvalidNum       int32
	iSubSPSLastInvalidId int32
	iSubSPSInvalidNum    int32
	iSeqId               int32 //sequence id
	iOverwriteFlags      int32
}

type PWelsDecoderSpsPpsCTX = *SWelsDecoderSpsPpsCTX

// Last Decoded Picture Info
type SWelsLastDecPicInfo struct {
	// Save the last nal header info
	sLastNalHdrExt               common.SNalUnitHeaderExt
	sLastSliceHeader             SSliceHeader
	iPrevPicOrderCntMsb          int32
	iPrevPicOrderCntLsb          int32
	pPreviousDecodedPictureInDpb *SPicture //pointer to previously decoded picture in DPB for error concealment
	iPrevFrameNum                int32     // frame number of previous frame well decoded for non-truncated mode yet
	bLastHasMmco5                bool
	uiDecodingTimeStamp          uint32 //represent relative decoding time stamps
}

type PWelsLastDecPicInfo = *SWelsLastDecPicInfo

type SPictInfo struct {
	sBufferInfo         api.SBufferInfo
	iPOC                int32
	iPicBuffIdx         int32
	uiDecodingTimeStamp uint32
	iSeqNum             int32
}

type PPictInfo = *SPictInfo

type SPictReoderingStatus struct {
	iPictInfoIndex           int32
	iMinSeqNum               int32
	iMinPOC                  int32
	iNumOfPicts              int32
	iLastWrittenSeqNum       int32
	iLastWrittenPOC          int32
	iLargestBufferedPicIndex int32
	bHasBSlice               bool
}

type PPictReoderingStatus = *SPictReoderingStatus

// SDecMbCtx is the anonymous `sMb` member struct of TagWelsDecoderContext:
// per-MB arrays (slices indexed by MB address) for each exchangeable layer.
type SDecMbCtx struct {
	pMbType                         [LAYER_NUM_EXCHANGEABLE][]uint32 /* mb type */
	pMv                             [LAYER_NUM_EXCHANGEABLE][common.LIST_A][][common.MB_BLOCK4x4_NUM][common.MV_A]int16
	pRefIndex                       [LAYER_NUM_EXCHANGEABLE][common.LIST_A][][common.MB_BLOCK4x4_NUM]int8
	pDirect                         [LAYER_NUM_EXCHANGEABLE][][common.MB_BLOCK4x4_NUM]int8
	pNoSubMbPartSizeLessThan8x8Flag [LAYER_NUM_EXCHANGEABLE][]bool
	pTransformSize8x8Flag           [LAYER_NUM_EXCHANGEABLE][]bool
	pLumaQp                         [LAYER_NUM_EXCHANGEABLE][]int8    /*mb luma_qp*/
	pChromaQp                       [LAYER_NUM_EXCHANGEABLE][][2]int8 /*mb chroma_qp*/
	pMvd                            [LAYER_NUM_EXCHANGEABLE][common.LIST_A][][common.MB_BLOCK4x4_NUM][common.MV_A]int16
	pCbfDc                          [LAYER_NUM_EXCHANGEABLE][]uint16
	pNzc                            [LAYER_NUM_EXCHANGEABLE][][24]int8
	pNzcRs                          [LAYER_NUM_EXCHANGEABLE][][24]int8
	pScaledTCoeff                   [LAYER_NUM_EXCHANGEABLE][][common.MB_COEFF_LIST_SIZE]int16 /*need be aligned*/
	pIntraPredMode                  [LAYER_NUM_EXCHANGEABLE][][8]int8                          //0~3 top4x4 ; 4~6 left 4x4; 7 intra16x16
	pIntra4x4FinalMode              [LAYER_NUM_EXCHANGEABLE][][common.MB_BLOCK4x4_NUM]int8
	pIntraNxNAvailFlag              [LAYER_NUM_EXCHANGEABLE][]uint8
	pChromaPredMode                 [LAYER_NUM_EXCHANGEABLE][]int8
	pCbp                            [LAYER_NUM_EXCHANGEABLE][]int8
	pMotionPredFlag                 [LAYER_NUM_EXCHANGEABLE][common.LIST_A][][common.MB_PARTITION_SIZE]uint8 // 8x8
	pSubMbType                      [LAYER_NUM_EXCHANGEABLE][][MB_SUB_PARTITION_SIZE]uint32
	pSliceIdc                       [LAYER_NUM_EXCHANGEABLE][]int32 // using int32_t for slice_idc
	pResidualPredFlag               [LAYER_NUM_EXCHANGEABLE][]int8
	pInterPredictionDoneFlag        [LAYER_NUM_EXCHANGEABLE][]int8
	pMbCorrectlyDecodedFlag         [LAYER_NUM_EXCHANGEABLE][]bool
	pMbRefConcealedFlag             [LAYER_NUM_EXCHANGEABLE][]bool
	iMbWidth                        uint32
	iMbHeight                       uint32
}

/*
 *  decoder context
 */
type SWelsDecoderContext struct {
	sLogCtx common.SLogContext
	// Input
	pArgDec any // structured arguments for decoder, reserved here for extension in the future

	sRawData   SDataBuffer
	sSavedData SDataBuffer //for parse only purpose

	// Configuration
	pParam    *api.SDecodingParam
	uiCpuFlag uint32 // CPU compatibility detected (always 0 in Go)

	eVideoType     api.VIDEO_BITSTREAM_TYPE //indicate the type of video to decide whether or not to do qp_delta error detection.
	bHaveGotMemory bool                     // global memory for decoder context related ever requested?

	iImgWidthInPixel      int32 // width of image in pixel reconstruction picture to be output
	iImgHeightInPixel     int32 // height of image in pixel reconstruction picture to be output
	iLastImgWidthInPixel  int32 // width of image in last successful pixel reconstruction picture to be output
	iLastImgHeightInPixel int32 // height of image in last successful pixel reconstruction picture to be output
	bFreezeOutput         bool  // indicating current frame freezing. Default: true

	// Derived common elements
	sCurNalHead   common.SNalUnitHeader
	eSliceType    common.EWelsSliceType // Slice type
	bUsedAsRef    bool                  //flag as ref
	iFrameNum     int32
	iErrorCode    int32               // error code return while decoding in case packets lost
	sFmoList      [MAX_PPS_COUNT]SFmo // list for FMO storage
	pFmo          *SFmo               // current fmo context after parsed slice_header
	iActiveFmoNum int32               // active count number of fmo context in list

	/*needed info by decode slice level and mb level*/
	iDecBlockOffsetArray [24]int32 // address talbe for sub 4x4 block in intra4x4_mb, so no need to caculta the address every time.

	sMb SDecMbCtx

	// reconstruction picture
	pDec *SPicture //pointer to current picture being reconstructed

	pTempDec *SPicture //pointer to temp decoder picture to be used only for Bi Prediction.

	// reference pictures
	sRefPic    SRefPic
	sTmpRefPic SRefPic    //used to temporarily save RefPic for next active thread
	pVlcTable  *SVlcTable // vlc table

	sBs                    common.SBitStringAux
	iMaxBsBufferSizeInByte int32 //actual memory size for BS buffer

	/* Global memory external */
	sSpsPpsCtx SWelsDecoderSpsPpsCTX
	bHasNewSps bool

	sFrameCrop SPosOffset

	pSliceHeader *SSliceHeader

	pPicBuff        *SPicBuff // Initially allocated memory for pictures which are used in decoding.
	iPicQueueNumber int32

	pAccessUnitList *SAccessUnit // current access unit list to be performed
	//PSps                          pActiveLayerSps[MAX_LAYER_NUM];
	pSps *SSps // used by current AU
	pPps *SPps // used by current AU
	// Memory for pAccessUnitList is dynamically held till decoder destruction.
	pCurDqLayer   *SDqLayer                         // current DQ layer representation, also carry reference base layer if applicable
	pDqLayersList [LAYER_NUM_EXCHANGEABLE]*SDqLayer // DQ layers list with memory allocated
	pNalCur       *SNalUnit                         // point to current NAL Nnit
	uiNalRefIdc   uint8                             // NalRefIdc for easy access;
	iPicWidthReq  int32                             // picture width have requested the memory
	iPicHeightReq int32                             // picture height have requested the memory

	uiTargetDqId uint8 // maximal DQ ID in current access unit, meaning target layer ID
	//bool                          bAvcBasedFlag;          // For decoding bitstream:
	bEndOfStreamFlag    bool // Flag on end of stream requested by external application layer
	bInstantDecFlag     bool // Flag for no-delay decoding
	bInitialDqLayersMem bool // dq layers related memory is available?

	bOnlyOneLayerInCurAuFlag bool //only one layer in current AU: 1

	bReferenceLostAtT0Flag bool
	iTotalNumMbRec         int32 //record current number of decoded MB
	// #ifdef LONG_TERM_REF
	bParamSetsLostFlag bool //sps or pps do not exist or not correct

	bCurAuContainLtrMarkSeFlag bool  //current AU has the LTR marking syntax element, mark the previous frame or self
	iFrameNumOfAuMarkedLtr     int32 //if bCurAuContainLtrMarkSeFlag==true, SHOULD set this variable

	uiCurIdrPicId uint16
	// #endif
	bNewSeqBegin     bool
	bNextNewSeqBegin bool
	pStreamSeqNum    *int32 // points to CWelsDecoder.m_iStreamSeqNum
	iSeqNum          int32

	//for Parse only
	bFramePending    bool
	bFrameFinish     bool
	iNalNum          int32
	iMaxNalNum       int32 //permitted max NAL num stored in parser
	sSpsBsInfo       [common.MAX_SPS_COUNT]SSpsBsInfo
	sSubsetSpsBsInfo [MAX_PPS_COUNT]SSpsBsInfo
	sPpsBsInfo       [MAX_PPS_COUNT]SPpsBsInfo
	pParserBsInfo    *api.SParserBsInfo

	//PPicture pPreviousDecodedPictureInDpb; //pointer to previously decoded picture in DPB for error concealment
	pGetI16x16LumaPredFunc  [7]PGetIntraPredFunc  //h264_predict_copy_16x16;
	pGetI4x4LumaPredFunc    [14]PGetIntraPredFunc // h264_predict_4x4_t
	pGetIChromaPredFunc     [7]PGetIntraPredFunc  // h264_predict_8x8_t
	pIdctResAddPredFunc     PIdctResAddPredFunc
	pIdctFourResAddPredFunc PIdctFourResAddPredFunc
	sMcFunc                 common.SMcFunc
	//Transform8x8
	pGetI8x8LumaPredFunc   [14]PGetIntraPred8x8Func
	pIdctResAddPredFunc8x8 PIdctResAddPredFunc

	//For error concealment
	sCopyFunc SCopyFunc
	/* For Deblocking */
	sDeblockingFunc SDeblockingFunc
	sExpandPicFunc  common.SExpandPicFunc

	/* For Block */
	sBlockFunc SBlockFunc

	iCurSeqIntervalTargetDependId int32
	iCurSeqIntervalMaxPicWidth    int32
	iCurSeqIntervalMaxPicHeight   int32

	pFillInfoCacheIntraNxNFunc PWelsFillNeighborMbInfoIntra4x4Func
	pMapNxNNeighToSampleFunc   PWelsMapNeighToSample
	pMap16x16NeighToSampleFunc PWelsMap16NeighToSample

	//feedback whether or not have VCL in current AU, and the temporal ID
	iFeedbackVclNalInAu int32
	iFeedbackTidInAu    int32
	iFeedbackNalRefIdc  int32

	bAuReadyFlag bool // true: one au is ready for decoding; false: default value

	bPrintFrameErrorTraceFlag    bool  //true: can print info for upper layer
	iIgnoredErrorInfoPacketCount int32 //store the packet number with error decoding info
	//trace handle
	pTraceHandle any

	pLastDecPicInfo *SWelsLastDecPicInfo // points to CWelsDecoder.m_sLastDecPicInfo

	sWelsCabacContexts  [4][WELS_QP_MAX + 1][common.WELS_CONTEXT_COUNT]SWelsCabacCtx
	bCabacInited        bool
	pCabacCtx           [common.WELS_CONTEXT_COUNT]SWelsCabacCtx
	pCabacDecEngine     *SWelsCabacDecEngine
	dDecTime            float64
	pDecoderStatistics  *api.SDecoderStatistics // For real time debugging (points to CWelsDecoder.m_sDecoderStatistics)
	iMbEcedNum          int32
	iMbEcedPropNum      int32
	iMbNum              int32
	bMbRefConcealed     bool
	bRPLRError          bool
	iECMVs              [16][2]int32
	pECRefPic           [16]*SPicture
	uiTimeStamp         uint64
	uiDecodingTimeStamp uint32 //represent relative decoding time stamps
	// To support scaling list HP
	pDequant_coeff_buffer4x4 [6][52][16]uint16
	pDequant_coeff_buffer8x8 [6][52][64]uint16
	pDequant_coeff4x4        [6][][16]uint16 // 4x4 sclaing list value pointer (= pDequant_coeff_buffer4x4[i][:])
	pDequant_coeff8x8        [6][][64]uint16 //64 residual coeff ,with 6 kinds of residual type, 52 qp level (= pDequant_coeff_buffer8x8[i][:])
	iDequantCoeffPpsid       int32           //When a new pps actived, reinitialised the scaling list value
	bDequantCoeff4x4Init     bool
	bUseScalingList          bool
	// CMemoryAlign* pMemAlign is dropped (Go allocates with make/new).
	pThreadCtx     *SWelsDecoderThreadCTX // always nil: the Go port is single-threaded
	pLastThreadCtx *SWelsDecoderThreadCTX // always nil: the Go port is single-threaded
	// WELS_MUTEX* pCsDecoder is dropped (no threads).
	lastReadyHeightOffset [common.LIST_A][MAX_REF_PIC_COUNT]int16 //last ready reference MB offset
	pPictInfoList         []SPictInfo                             // = CWelsDecoder.m_sPictInfoList[:]
	pPictReoderingStatus  *SPictReoderingStatus                   // points to CWelsDecoder.m_sReoderingStatus
}

type PWelsDecoderContext = *SWelsDecoderContext

// SWelsDecThreadInfo (tagSWelsDecThread). Threading placeholder.
type SWelsDecThreadInfo struct {
	sIsBusy        *SWelsDecSemphore
	sIsActivated   SWelsDecSemphore
	sIsIdle        SWelsDecSemphore
	sThrHandle     SWelsDecThread
	uiCommand      uint32
	uiThrNum       uint32
	uiThrMaxNum    uint32
	uiThrStackSize uint32
	pThrProcMain   LPWELS_THREAD_ROUTINE
}

type PWelsDecThreadInfo = *SWelsDecThreadInfo

// SWelsDecoderThreadCTX (tagSWelsDecThreadCtx). Threading placeholder: the
// Go port never creates one for a decoder context (pCtx.pThreadCtx is nil).
type SWelsDecoderThreadCTX struct {
	sThreadInfo        SWelsDecThreadInfo
	pCtx               *SWelsDecoderContext
	threadCtxOwner     *CWelsDecoder
	kpSrc              []uint8
	kiSrcLen           int32
	ppDst              *[3][]uint8
	sDstInfo           api.SBufferInfo
	pDec               *SPicture
	sImageReady        SWelsDecEvent
	sSliceDecodeStart  SWelsDecEvent
	sSliceDecodeFinish SWelsDecEvent
	iPicBuffIdx        int32 //picBuff Index
	// Per-thread snapshot of pPreviousDecodedPictureInDpb captured before sSliceDecodeFinish
	// is signaled. Prevents concurrent workers from overwriting the shared pLastDecPicInfo
	// field before BufferingReadyPicture() reads it.
	pPreviousDecodedPictureInDpb *SPicture
}

type PWelsDecoderThreadCTX = *SWelsDecoderThreadCTX

func ResetActiveSPSForEachLayer(pCtx *SWelsDecoderContext) {
	if pCtx.iTotalNumMbRec == 0 {
		for i := 0; i < MAX_LAYER_NUM; i++ {
			pCtx.sSpsPpsCtx.pActiveLayerSps[i] = nil
		}
	}
}

func GetThreadCount(pCtx *SWelsDecoderContext) int32 {
	var iThreadCount int32
	if pCtx.pThreadCtx != nil {
		pThreadCtx := pCtx.pThreadCtx
		iThreadCount = int32(pThreadCtx.sThreadInfo.uiThrMaxNum)
	}
	return iThreadCount
}

// GetPrevFrameNum only applies when thread count >= 2. The C version scans
// the sibling thread contexts; the Go port is single-threaded (pThreadCtx is
// always nil), so only the fallback remains.
func GetPrevFrameNum(pCtx *SWelsDecoderContext) int32 {
	return pCtx.pLastDecPicInfo.iPrevFrameNum
}
