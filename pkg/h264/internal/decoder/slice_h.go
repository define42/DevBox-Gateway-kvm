// Port of codec/decoder/core/inc/slice.h.

package decoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

// SReorderingSyn is the anonymous element struct of
// SRefPicListReorderSyn.sReorderingSyn.
type SReorderingSyn struct {
	uiAbsDiffPicNumMinus1    uint32
	uiLongTermPicNum         uint16
	uiReorderingOfPicNumsIdc uint16
}

/*
 *  Reference picture list reordering syntax, refer to page 64 in JVT X201wcm
 */
type SRefPicListReorderSyn struct {
	sReorderingSyn            [common.LIST_A][MAX_REF_PIC_COUNT + 1]SReorderingSyn
	bRefPicListReorderingFlag [common.LIST_A]bool
}

type PRefPicListReorderSyn = *SRefPicListReorderSyn

// SPredWeightList is the anonymous element struct of SPredWeightTabSyn.sPredList.
type SPredWeightList struct {
	iLumaWeight       [MAX_REF_PIC_COUNT]int32
	iLumaOffset       [MAX_REF_PIC_COUNT]int32
	iChromaWeight     [MAX_REF_PIC_COUNT][2]int32
	iChromaOffset     [MAX_REF_PIC_COUNT][2]int32
	bLumaWeightFlag   bool
	bChromaWeightFlag bool
}

/*
 *  Prediction weight table syntax, refer to page 65 in JVT X201wcm
 */
type SPredWeightTabSyn struct {
	uiLumaLog2WeightDenom   uint32
	uiChromaLog2WeightDenom uint32
	sPredList               [common.LIST_A]SPredWeightList
	iImplicitWeight         [MAX_REF_PIC_COUNT][MAX_REF_PIC_COUNT]int32
}

type PPredWeightTabSyn = *SPredWeightTabSyn

// SMmcoRef is the anonymous element struct of SRefPicMarking.sMmcoRef.
type SMmcoRef struct {
	uiMmcoType           uint32
	iShortFrameNum       int32
	iDiffOfPicNum        int32
	uiLongTermPicNum     uint32
	iLongTermFrameIdx    int32
	iMaxLongTermFrameIdx int32
}

/* Decoded reference picture marking syntax, refer to Page 66 in JVT X201wcm */
type SRefPicMarking struct {
	sMmcoRef [MAX_MMCO_COUNT]SMmcoRef

	bNoOutputOfPriorPicsFlag       bool
	bLongTermRefFlag               bool
	bAdaptiveRefPicMarkingModeFlag bool
}

type PRefPicMarking = *SRefPicMarking

// SMmcoBase is the anonymous element struct of SRefBasePicMarking.mmco_base.
type SMmcoBase struct {
	uiMmcoType       uint32
	iShortFrameNum   int32
	uiDiffOfPicNums  uint32
	uiLongTermPicNum uint32 //should uint32_t, cover larger range of iFrameNum.
}

/* Decode reference base picture marking syntax in Page 396 of JVT X201wcm */
type SRefBasePicMarking struct {
	mmco_base [MAX_MMCO_COUNT]SMmcoBase // MAX_REF_PIC for reference picture based on frame

	bAdaptiveRefBasePicMarkingModeFlag bool
}

type PRefBasePicMarking = *SRefBasePicMarking

/* Header of slice syntax elements, refer to Page 63 in JVT X201wcm */
type SSliceHeader struct {
	/*****************************slice header syntax and generated****************************/
	iFirstMbInSlice              int32
	iFrameNum                    int32
	iPicOrderCntLsb              int32
	iDeltaPicOrderCntBottom      int32
	iDeltaPicOrderCnt            [2]int32
	iRedundantPicCnt             int32
	iDirectSpatialMvPredFlag     int32 //!< Direct Mode type to be used (0: Temporal, 1: Spatial)
	uiRefCount                   [common.LIST_A]int32
	iSliceQpDelta                int32 //no use for iSliceQp is used directly
	iSliceQp                     int32
	iSliceQsDelta                int32 // For SP/SI slices
	uiDisableDeblockingFilterIdc uint32
	iSliceAlphaC0Offset          int32
	iSliceBetaOffset             int32
	iSliceGroupChangeCycle       int32

	pSps     *SSps // points into SWelsDecoderSpsPpsCTX.sSpsBuffer / sSubsetSpsBuffer[].sSps
	pPps     *SPps // points into SWelsDecoderSpsPpsCTX.sPpsBuffer
	iSpsId   int32
	iPpsId   int32
	bIdrFlag bool

	/*********************got from other layer for efficency if possible*********************/
	pRefPicListReordering SRefPicListReorderSyn // Reference picture list reordering syntaxs
	sPredWeightTable      SPredWeightTabSyn
	iCabacInitIdc         int32
	iMbWidth              int32          //from?
	iMbHeight             int32          //from?
	sRefMarking           SRefPicMarking // Decoded reference picture marking syntaxs

	uiIdrPicId                   uint16
	eSliceType                   common.EWelsSliceType
	bNumRefIdxActiveOverrideFlag bool
	bFieldPicFlag                bool //not supported in base profile
	bBottomFiledFlag             bool //not supported in base profile
	uiPadding1Byte               uint8
	bSpForSwitchFlag             bool // For SP/SI slices
	iPadding2Bytes               int16
}

type PSliceHeader = *SSliceHeader

/* Slice header in scalable extension syntax, refer to Page 394 in JVT X201wcm */
type SSliceHeaderExt struct {
	sSliceHeader SSliceHeader
	pSubsetSps   *SSubsetSps // points into SWelsDecoderSpsPpsCTX.sSubsetSpsBuffer

	uiDisableInterLayerDeblockingFilterIdc uint32
	iInterLayerSliceAlphaC0Offset          int32
	iInterLayerSliceBetaOffset             int32

	//SPosOffset sScaledRefLayer;
	iScaledRefLayerPicWidthInSampleLuma  int32
	iScaledRefLayerPicHeightInSampleLuma int32

	sRefBasePicMarking              SRefBasePicMarking
	bBasePredWeightTableFlag        bool
	bStoreRefBasePicFlag            bool
	bConstrainedIntraResamplingFlag bool
	bSliceSkipFlag                  bool

	bAdaptiveBaseModeFlag           bool
	bDefaultBaseModeFlag            bool
	bAdaptiveMotionPredFlag         bool
	bDefaultMotionPredFlag          bool
	bAdaptiveResidualPredFlag       bool
	bDefaultResidualPredFlag        bool
	bTCoeffLevelPredFlag            bool
	uiRefLayerChromaPhaseXPlus1Flag uint8

	uiRefLayerChromaPhaseYPlus1 uint8
	uiRefLayerDqId              uint8
	uiScanIdxStart              uint8
	uiScanIdxEnd                uint8
}

type PSliceHeaderExt = *SSliceHeaderExt

type SSlice struct {
	/*******************************slice_header****************************/
	sSliceHeaderExt SSliceHeaderExt

	/*******************************use for future****************************/
	// for Macroblock coding within slice
	iLastMbQp int32 // stored qp for last mb coded, maybe more efficient for mb skip detection etc.

	/*******************************slice_data****************************/
	/*slice_data_ext()*/
	iMbSkipRun         int32
	iTotalMbInCurSlice int32 //record the total number of MB in current slice.

	/*slice_data_ext() generate*/

	/*******************************misc use****************************/
	bSliceHeaderExtFlag bool // Indicate which slice header is used, avc or ext?
	/*************got from other layer for effiency if possible***************/
	/*from lower layer: slice header*/
	eSliceType   uint8
	uiPadding    [2]uint8
	iLastDeltaQp int32
	iMvScale     [common.LIST_A][MAX_DPB_COUNT]int16 //Moton vector scale For Temporal Direct Mode Type
}

type PSlice = *SSlice
