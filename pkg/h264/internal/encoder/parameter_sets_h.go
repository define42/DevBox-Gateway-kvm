// Port of codec/encoder/core/inc/parameter_sets.h.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"

// SWelsSPS is the Sequence Parameter Set, refer to Page 57 in JVT X201wcm.
type SWelsSPS struct {
	uiSpsId           uint32
	iMbWidth          int16
	iMbHeight         int16
	uiLog2MaxFrameNum uint32
	uiPocType         uint32
	/* POC type 0 */
	iLog2MaxPocLsb int32
	/* POC type 1 (not supported) */

	sFrameCrop    SCropOffset
	iNumRefFrames int16

	uiProfileIdc uint8
	iLevelIdc    uint8

	bGapsInFrameNumValueAllowedFlag bool
	bFrameCroppingFlag              bool
	bVuiParamPresentFlag            bool

	// Note: members bVideoSignalTypePresent through uiColorMatrix below are also defined in SSpatialLayerConfig in codec_app_def.h,
	// along with definitions for enumerators EVideoFormatSPS, EColorPrimaries, ETransferCharacteristics, and EColorMatrix.
	bVideoSignalTypePresent   bool  // false => do not write any of the following information to the header
	uiVideoFormat             uint8 // EVideoFormatSPS; 3 bits in header; 0-5 => component, kpal, ntsc, secam, mac, undef
	bFullRange                bool  // false => analog video data range [16, 235]; true => full data range [0,255]
	bColorDescriptionPresent  bool  // false => do not write any of the following three items to the header
	uiColorPrimaries          uint8 // EColorPrimaries; 8 bits in header
	uiTransferCharacteristics uint8 // ETransferCharacteristics; 8 bits in header
	uiColorMatrix             uint8 // EColorMatrix; 8 bits in header

	bConstraintSet0Flag bool
	bConstraintSet1Flag bool
	bConstraintSet2Flag bool
	bConstraintSet3Flag bool

	// aspect ratio in VUI
	bAspectRatioPresent   bool
	eAspectRatio          api.ESampleAspectRatio
	sAspectRatioExtWidth  uint16
	sAspectRatioExtHeight uint16
}

// PWelsSPS is a pointer alias.
type PWelsSPS = *SWelsSPS

// SSpsSvcExt is the Sequence Parameter Set SVC extension syntax, refer to Page 391 in JVT X201wcm.
type SSpsSvcExt struct {
	iExtendedSpatialScalability  uint8 // ESS
	bSeqTcoeffLevelPredFlag      bool
	bAdaptiveTcoeffLevelPredFlag bool
	bSliceHeaderRestrictionFlag  bool
}

// PSpsSvcExt is a pointer alias.
type PSpsSvcExt = *SSpsSvcExt

// SSubsetSps is the subset sequence parameter set syntax, refer to Page 391 in JVT X201wcm.
type SSubsetSps struct {
	pSps       SWelsSPS // (a value, despite the C name)
	sSpsSvcExt SSpsSvcExt
}

// PSubsetSps is a pointer alias.
type PSubsetSps = *SSubsetSps

// SWelsPPS is the picture parameter set syntax, refer to Page 59 in JVT X201wcm.
// (The FMO fields only exist without DISABLE_FMO_FEATURE and are not ported.)
type SWelsPPS struct {
	iSpsId uint32
	iPpsId uint32

	iPicInitQp            int8
	iPicInitQs            int8
	uiChromaQpIndexOffset uint8

	bEntropyCodingModeFlag              bool
	bDeblockingFilterControlPresentFlag bool
}

// PWelsPPPS is a pointer alias.
type PWelsPPPS = *SWelsPPS
