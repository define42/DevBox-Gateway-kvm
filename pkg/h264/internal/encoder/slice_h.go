// Port of codec/encoder/core/inc/slice.h.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

/*******************************sub struct of slice header****************************/

// SReorderingSyntax is one entry of SRefPicListReorderSyntax (anonymous struct in C).
type SReorderingSyntax struct {
	uiAbsDiffPicNumMinus1    uint32 // uiAbsDiffPicNumMinus1 SHOULD be in the range of [4, (1<<pSps->uiLog2MaxFrameNum)-1], {p104, JVT-X201wcm1}
	iLongTermPicNum          uint16
	uiReorderingOfPicNumsIdc uint16 // in order to pack 2-uint16_t into 1-(u)int32_t, so modify the type into uint16_t.
}

// SRefPicListReorderSyntax is the reference picture list reordering syntax, refer to page 64 in JVT X201wcm.
type SRefPicListReorderSyntax struct {
	SReorderingSyntax [MAX_REFERENCE_REORDER_COUNT_NUM]SReorderingSyntax // MAX_REF_PIC_COUNT
}

// SMmcoRef is one entry of SRefPicMarking (anonymous struct in C).
type SMmcoRef struct {
	iMmcoType            int32
	iShortFrameNum       int32
	iDiffOfPicNum        int32
	iLongTermPicNum      int32
	iLongTermFrameIdx    int32
	iMaxLongTermFrameIdx int32
}

// SRefPicMarking is the decoded reference picture marking syntax, refer to Page 66 in JVT X201wcm.
type SRefPicMarking struct {
	SMmcoRef [MAX_REFERENCE_MMCO_COUNT_NUM]SMmcoRef // MAX_MMCO_COUNT

	uiMmcoCount                    uint8
	bNoOutputOfPriorPicsFlag       bool
	bLongTermRefFlag               bool
	bAdaptiveRefPicMarkingModeFlag bool
}

// SRCSlicing is the slice level rc statistic info.
type SRCSlicing struct {
	iComplexityIndexSlice int32
	iCalculatedQpSlice    int32
	iStartMbSlice         int32
	iEndMbSlice           int32
	iTotalQpSlice         int32
	iTotalMbSlice         int32
	iTargetBitsSlice      int32
	iBsPosSlice           int32
	iFrameBitsSlice       int32
	iGomBitsSlice         int32
	iGomTargetBits        int32
}

// SSliceHeader is the header of slice syntax elements, refer to Page 63 in JVT X201wcm.
type SSliceHeader struct {
	/*****************************slice header syntax and generated****************************/
	iFirstMbInSlice int32
	iFrameNum       int32
	iPicOrderCntLsb int32

	eSliceType          common.EWelsSliceType
	uiNumRefIdxL0Active uint8
	uiRefCount          uint8
	uiRefIndex          uint8 // exact reference picture index for slice
	iSliceQpDelta       int8

	uiDisableDeblockingFilterIdc uint8
	iSliceAlphaC0Offset          int8
	iSliceBetaOffset             int8

	pSps *SWelsSPS // points into sWelsEncCtx.pSpsArray (or the subset SPS' pSps)
	pPps *SWelsPPS // points into sWelsEncCtx.pPPSArray

	iSpsId     int32
	iPpsId     int32
	uiIdrPicId uint16

	bNumRefIdxActiveOverrideFlag bool

	uiPadding1Bytes uint8

	sRefMarking    SRefPicMarking           // Decoded reference picture marking syntaxs
	sRefReordering SRefPicListReorderSyntax // Reference picture list reordering syntaxs
}

// PSliceHeader is a pointer alias.
type PSliceHeader = *SSliceHeader

// SSliceHeaderExt is the SSlice header in scalable extension syntax, refer to Page 394 in JVT X201wcm.
type SSliceHeaderExt struct {
	sSliceHeader SSliceHeader
	pSubsetSps   *SSubsetSps // points into sWelsEncCtx.pSubsetArray

	uiNumMbsInSlice uint32

	bStoreRefBasePicFlag            bool
	bConstrainedIntraResamplingFlag bool
	bSliceSkipFlag                  bool

	bAdaptiveBaseModeFlag     bool
	bDefaultBaseModeFlag      bool
	bAdaptiveMotionPredFlag   bool
	bDefaultMotionPredFlag    bool
	bAdaptiveResidualPredFlag bool
	bDefaultResidualPredFlag  bool
	bTcoeffLevelPredFlag      bool

	uiDisableInterLayerDeblockingFilterIdc uint8
}

// PSliceHeaderExt is a pointer alias.
type PSliceHeaderExt = *SSliceHeaderExt

// SSlice holds one slice (mainly for multiple threads imp.).
type SSlice struct {
	// Each active slice owns its scratch. Clear it at slice boundaries so
	// mode decisions and rollback state never retain frame-specific references.
	sMdScratch   SWelsMD
	sDssScratch  SDynamicSlicingStack
	sMbCacheInfo SMbCache // MBCache is introduced within slice dependency
	// C SBitStringAux*: the writer this slice writes to: &sSliceBs.sBsWrite
	// (independent slice buffers) or &sWelsEncCtx.pOut.sBsWrite.
	pSliceBsa *common.SBitStringAux
	sSliceBs  SWelsSliceBs

	/*******************************sSliceHeader****************************/
	sSliceHeaderExt SSliceHeaderExt

	sMvStartMin SMVUnitXY
	sMvStartMax SMVUnitXY
	sMvc        [5]SMVUnitXY
	uiMvcNum    uint8
	sScaleShift uint8

	iSliceIdx                        int32
	uiBufferIdx                      uint32
	bSliceHeaderExtFlag              bool  // Indicate which slice header is used, avc or ext?
	uiLastMbQp                       uint8 // stored qp for last mb coded, maybe more efficient for mb skip detection etc.
	bDynamicSlicingSliceSizeCtrlFlag bool
	uiAssumeLog2BytePerMb            uint8

	uiSliceFMECostDown uint32 // TODO: for FME switch under MT, to opt after ME final?

	uiReservedFillByte uint8 // reserved to meet 4 bytes alignment

	sCabacCtx          SCabacCtx
	iCabacInitIdc      int32
	iMbSkipRun         int32
	iCountMbNumInSlice int32
	uiSliceConsumeTime uint32
	iSliceComplexRatio int32

	sSlicingOverRc SRCSlicing // slice level rc statistic info
}

// PSlice is a pointer alias.
type PSlice = *SSlice
