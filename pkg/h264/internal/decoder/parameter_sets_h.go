// Port of codec/decoder/core/inc/parameter_sets.h.

package decoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

/* VUI syntax in Sequence Parameter Set, refer to E.1 in Rec */
type SVui struct {
	bAspectRatioInfoPresentFlag         bool
	uiAspectRatioIdc                    uint32
	uiSarWidth                          uint32
	uiSarHeight                         uint32
	bOverscanInfoPresentFlag            bool
	bOverscanAppropriateFlag            bool
	bVideoSignalTypePresentFlag         bool
	uiVideoFormat                       uint8
	bVideoFullRangeFlag                 bool
	bColourDescripPresentFlag           bool
	uiColourPrimaries                   uint8
	uiTransferCharacteristics           uint8
	uiMatrixCoeffs                      uint8
	bChromaLocInfoPresentFlag           bool
	uiChromaSampleLocTypeTopField       uint32
	uiChromaSampleLocTypeBottomField    uint32
	bTimingInfoPresentFlag              bool
	uiNumUnitsInTick                    uint32
	uiTimeScale                         uint32
	bFixedFrameRateFlag                 bool
	bNalHrdParamPresentFlag             bool
	bVclHrdParamPresentFlag             bool
	bPicStructPresentFlag               bool
	bBitstreamRestrictionFlag           bool
	bMotionVectorsOverPicBoundariesFlag bool
	uiMaxBytesPerPicDenom               uint32
	uiMaxBitsPerMbDenom                 uint32
	uiLog2MaxMvLengthHorizontal         uint32
	uiLog2MaxMvLengthVertical           uint32
	uiMaxNumReorderFrames               uint32
	uiMaxDecFrameBuffering              uint32
}

type PVui = *SVui

/* Sequence Parameter Set, refer to Page 57 in JVT X201wcm */
type SSps struct {
	iSpsId         int32
	iMbWidth       uint32
	iMbHeight      uint32
	uiTotalMbCount uint32 //used in decode_slice_data()

	uiLog2MaxFrameNum uint32
	uiPocType         uint32
	/* POC type 0 */
	iLog2MaxPocLsb int32
	/* POC type 1 */
	iOffsetForNonRefPic int32

	iOffsetForTopToBottomField int32
	iNumRefFramesInPocCycle    int32
	iOffsetForRefFrame         [256]int8
	iNumRefFrames              int32

	sFrameCrop SPosOffset

	uiProfileIdc      ProfileIdc
	uiLevelIdc        uint8
	uiChromaFormatIdc uint8
	uiChromaArrayType uint8

	uiBitDepthLuma   uint8
	uiBitDepthChroma uint8
	/* TO BE CONTINUE: POC type 1 */
	bDeltaPicOrderAlwaysZeroFlag    bool
	bGapsInFrameNumValueAllowedFlag bool

	bFrameMbsOnlyFlag       bool
	bMbaffFlag              bool // MB Adapative Frame Field
	bDirect8x8InferenceFlag bool
	bFrameCroppingFlag      bool

	bVuiParamPresentFlag bool
	//  bool          bTimingInfoPresentFlag;
	//  bool          bFixedFrameRateFlag;
	bConstraintSet0Flag           bool
	bConstraintSet1Flag           bool
	bConstraintSet2Flag           bool
	bConstraintSet3Flag           bool
	bSeparateColorPlaneFlag       bool
	bQpPrimeYZeroTransfBypassFlag bool
	bSeqScalingMatrixPresentFlag  bool
	bSeqScalingListPresentFlag    [12]bool
	//Add scaling list supporting
	iScalingList4x4 [6][16]uint8
	iScalingList8x8 [6][64]uint8
	sVui            SVui
	pSLevelLimits   *common.SLevelLimits // points into common.G_ksLevelLimits
}

type PSps = *SSps

/* Sequence Parameter Set extension syntax, refer to Page 391 in JVT X201wcm */
type SSpsSvcExt struct {
	sSeqScaledRefLayer SPosOffset

	uiExtendedSpatialScalability               uint8 // ESS
	uiChromaPhaseXPlus1Flag                    uint8
	uiChromaPhaseYPlus1                        uint8
	uiSeqRefLayerChromaPhaseXPlus1Flag         uint8
	uiSeqRefLayerChromaPhaseYPlus1             uint8
	bInterLayerDeblockingFilterCtrlPresentFlag bool
	bSeqTCoeffLevelPredFlag                    bool
	bAdaptiveTCoeffLevelPredFlag               bool
	bSliceHeaderRestrictionFlag                bool
}

type PSpsSvcExt = *SSpsSvcExt

/* Subset sequence parameter set syntax, refer to Page 391 in JVT X201wcm */
type SSubsetSps struct {
	sSps                          SSps
	sSpsSvcExt                    SSpsSvcExt
	bSvcVuiParamPresentFlag       bool
	bAdditionalExtension2Flag     bool
	bAdditionalExtension2DataFlag bool
}

type PSubsetSps = *SSubsetSps

/* Picture parameter set syntax, refer to Page 59 in JVT X201wcm */
type SPps struct {
	iSpsId int32
	iPpsId int32

	uiNumSliceGroups    uint32
	uiSliceGroupMapType uint32
	/* slice_group_map_type = 0 */
	uiRunLength [MAX_SLICEGROUP_IDS]uint32
	/* slice_group_map_type = 2 */
	uiTopLeft     [MAX_SLICEGROUP_IDS]uint32
	uiBottomRight [MAX_SLICEGROUP_IDS]uint32
	/* slice_group_map_type = 3, 4 or 5 */
	uiSliceGroupChangeRate uint32
	/* slice_group_map_type = 6 */
	uiPicSizeInMapUnits uint32
	uiSliceGroupId      [MAX_SLICEGROUP_IDS]uint32

	uiNumRefIdxL0Active uint32
	uiNumRefIdxL1Active uint32

	iPicInitQp           int32
	iPicInitQs           int32
	iChromaQpIndexOffset [2]int32 //cb,cr

	bEntropyCodingModeFlag bool
	bPicOrderPresentFlag   bool
	/* slice_group_map_type = 3, 4 or 5 */
	bSliceGroupChangeDirectionFlag      bool
	bDeblockingFilterControlPresentFlag bool

	bConstainedIntraPredFlag    bool
	bRedundantPicCntPresentFlag bool
	bWeightedPredFlag           bool
	uiWeightedBipredIdc         uint8

	bTransform8x8ModeFlag bool
	//Add for scalinglist support
	bPicScalingMatrixPresentFlag bool
	bPicScalingListPresentFlag   [12]bool
	iScalingList4x4              [6][16]uint8
	iScalingList8x8              [6][64]uint8

	iSecondChromaQPIndexOffset int32 //second_chroma_qp_index_offset
}

type PPps = *SPps
