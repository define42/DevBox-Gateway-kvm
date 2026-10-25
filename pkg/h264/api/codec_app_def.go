// Port of codec/api/wels/codec_app_def.h.

package api

// Constants
const (
	MAX_TEMPORAL_LAYER_NUM = 4
	MAX_SPATIAL_LAYER_NUM  = 4
	MAX_QUALITY_LAYER_NUM  = 4

	MAX_LAYER_NUM_OF_FRAME = 128
	MAX_NAL_UNITS_IN_LAYER = 128 ///< predetermined here, adjust it later if need

	MAX_RTP_PAYLOAD_LEN     = 1000
	AVERAGE_RTP_PAYLOAD_LEN = 800

	SAVED_NALUNIT_NUM_TMP = ((MAX_SPATIAL_LAYER_NUM * MAX_QUALITY_LAYER_NUM) + 1 + MAX_SPATIAL_LAYER_NUM) ///< SPS/PPS + SEI/SSEI + PADDING_NAL
	MAX_SLICES_NUM_TMP    = ((MAX_NAL_UNITS_IN_LAYER - SAVED_NALUNIT_NUM_TMP) / 3)

	AUTO_REF_PIC_COUNT   = -1 ///< encoder selects the number of reference frame automatically
	UNSPECIFIED_BIT_RATE = 0  ///< to do: add detail comment
)

// OpenH264Version is the struct of OpenH264 version (struct _tagVersion).
//
// E.g. SDK version is 1.2.0.0, major version number is 1, minor version number
// is 2, and revision number is 0.
type OpenH264Version struct {
	UMajor    uint32 ///< The major version number
	UMinor    uint32 ///< The minor version number
	URevision uint32 ///< The revision number
	UReserved uint32 ///< The reserved number, it should be 0.
}

// DECODING_STATE is the decoding status.
type DECODING_STATE int32

const (
	// Errors derived from bitstream parsing
	DsErrorFree          DECODING_STATE = 0x00 ///< bit stream error-free
	DsFramePending       DECODING_STATE = 0x01 ///< need more throughput to generate a frame output,
	DsRefLost            DECODING_STATE = 0x02 ///< layer lost at reference frame with temporal id 0
	DsBitstreamError     DECODING_STATE = 0x04 ///< error bitstreams(maybe broken internal frame) the decoder cared
	DsDepLayerLost       DECODING_STATE = 0x08 ///< dependented layer is ever lost
	DsNoParamSets        DECODING_STATE = 0x10 ///< no parameter set NALs involved
	DsDataErrorConcealed DECODING_STATE = 0x20 ///< current data error concealed specified
	DsRefListNullPtrs    DECODING_STATE = 0x40 ///<ref picure list contains null ptrs within uiRefCount range

	// Errors derived from logic level
	DsInvalidArgument    DECODING_STATE = 0x1000 ///< invalid argument specified
	DsInitialOptExpected DECODING_STATE = 0x2000 ///< initializing operation is expected
	DsOutOfMemory        DECODING_STATE = 0x4000 ///< out of memory due to new request
	// ANY OTHERS?
	DsDstBufNeedExpan DECODING_STATE = 0x8000 ///< actual picture size exceeds size of dst pBuffer feed in decoder, so need expand its size
)

// ENCODER_OPTION enumerates option types introduced in SVC encoder application.
type ENCODER_OPTION int32

const (
	ENCODER_OPTION_DATAFORMAT            ENCODER_OPTION = iota // = 0
	ENCODER_OPTION_IDR_INTERVAL                                ///< IDR period,0/-1 means no Intra period (only the first frame); lager than 0 means the desired IDR period, must be multiple of (2^temporal_layer)
	ENCODER_OPTION_SVC_ENCODE_PARAM_BASE                       ///< structure of Base Param
	ENCODER_OPTION_SVC_ENCODE_PARAM_EXT                        ///< structure of Extension Param
	ENCODER_OPTION_FRAME_RATE                                  ///< maximal input frame rate, current supported range: MAX_FRAME_RATE = 30,MIN_FRAME_RATE = 1
	ENCODER_OPTION_BITRATE
	ENCODER_OPTION_MAX_BITRATE
	ENCODER_OPTION_INTER_SPATIAL_PRED
	ENCODER_OPTION_RC_MODE
	ENCODER_OPTION_RC_FRAME_SKIP
	ENCODER_PADDING_PADDING ///< 0:disable padding;1:padding

	ENCODER_OPTION_PROFILE         ///< assgin the profile for each layer
	ENCODER_OPTION_LEVEL           ///< assgin the level for each layer
	ENCODER_OPTION_NUMBER_REF      ///< the number of refererence frame
	ENCODER_OPTION_DELIVERY_STATUS ///< the delivery info which is a feedback from app level

	ENCODER_LTR_RECOVERY_REQUEST
	ENCODER_LTR_MARKING_FEEDBACK
	ENCODER_LTR_MARKING_PERIOD
	ENCODER_OPTION_LTR ///< 0:disable LTR;larger than 0 enable LTR; LTR number is fixed to be 2 in current encoder
	ENCODER_OPTION_COMPLEXITY

	ENCODER_OPTION_ENABLE_SSEI              ///< enable SSEI: true--enable ssei; false--disable ssei
	ENCODER_OPTION_ENABLE_PREFIX_NAL_ADDING ///< enable prefix: true--enable prefix; false--disable prefix
	ENCODER_OPTION_SPS_PPS_ID_STRATEGY      ///< different stategy in adjust ID in SPS/PPS: 0- constant ID, 1-additional ID, 6-mapping and additional

	ENCODER_OPTION_CURRENT_PATH
	ENCODER_OPTION_DUMP_FILE              ///< dump layer reconstruct frame to a specified file
	ENCODER_OPTION_TRACE_LEVEL            ///< trace info based on the trace level
	ENCODER_OPTION_TRACE_CALLBACK         ///< a WelsTraceCallback which receives log messages (pass *WelsTraceCallback)
	ENCODER_OPTION_TRACE_CALLBACK_CONTEXT ///< context info of trace callback

	ENCODER_OPTION_GET_STATISTICS          ///< read only
	ENCODER_OPTION_STATISTICS_LOG_INTERVAL ///< log interval in millisecond

	ENCODER_OPTION_IS_LOSSLESS_LINK ///< advanced algorithmetic settings

	ENCODER_OPTION_BITS_VARY_PERCENTAGE ///< bit vary percentage
)

// DECODER_OPTION enumerates option types introduced in decoder application.
type DECODER_OPTION int32

const (
	DECODER_OPTION_END_OF_STREAM        DECODER_OPTION = iota + 1 ///< end of stream flag
	DECODER_OPTION_VCL_NAL                                        ///< feedback whether or not have VCL NAL in current AU for application layer
	DECODER_OPTION_TEMPORAL_ID                                    ///< feedback temporal id for application layer
	DECODER_OPTION_FRAME_NUM                                      ///< feedback current decoded frame number
	DECODER_OPTION_IDR_PIC_ID                                     ///< feedback current frame belong to which IDR period
	DECODER_OPTION_LTR_MARKING_FLAG                               ///< feedback wether current frame mark a LTR
	DECODER_OPTION_LTR_MARKED_FRAME_NUM                           ///< feedback frame num marked by current Frame
	DECODER_OPTION_ERROR_CON_IDC                                  ///< indicate decoder error concealment method
	DECODER_OPTION_TRACE_LEVEL
	DECODER_OPTION_TRACE_CALLBACK         ///< a WelsTraceCallback which receives log messages (pass *WelsTraceCallback)
	DECODER_OPTION_TRACE_CALLBACK_CONTEXT ///< context info of trace callbac

	DECODER_OPTION_GET_STATISTICS                    ///< feedback decoder statistics
	DECODER_OPTION_GET_SAR_INFO                      ///< feedback decoder Sample Aspect Ratio info in Vui
	DECODER_OPTION_PROFILE                           ///< get current AU profile info, only is used in GetOption
	DECODER_OPTION_LEVEL                             ///< get current AU level info,only is used in GetOption
	DECODER_OPTION_STATISTICS_LOG_INTERVAL           ///< set log output interval
	DECODER_OPTION_IS_REF_PIC                        ///< feedback current frame is ref pic or not
	DECODER_OPTION_NUM_OF_FRAMES_REMAINING_IN_BUFFER ///< number of frames remaining in decoder buffer when pictures are required to re-ordered into display-order.
	DECODER_OPTION_NUM_OF_THREADS                    ///< number of decoding threads. The maximum thread count is equal or less than lesser of (cpu core counts and 16).
)

// ERROR_CON_IDC enumerates the type of error concealment methods.
type ERROR_CON_IDC int32

const (
	ERROR_CON_DISABLE ERROR_CON_IDC = iota
	ERROR_CON_FRAME_COPY
	ERROR_CON_SLICE_COPY
	ERROR_CON_FRAME_COPY_CROSS_IDR
	ERROR_CON_SLICE_COPY_CROSS_IDR
	ERROR_CON_SLICE_COPY_CROSS_IDR_FREEZE_RES_CHANGE
	ERROR_CON_SLICE_MV_COPY_CROSS_IDR
	ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE
)

// FEEDBACK_VCL_NAL_IN_AU is the feedback that whether or not have VCL NAL in current AU.
type FEEDBACK_VCL_NAL_IN_AU int32

const (
	FEEDBACK_NON_VCL_NAL FEEDBACK_VCL_NAL_IN_AU = iota
	FEEDBACK_VCL_NAL
	FEEDBACK_UNKNOWN_NAL
)

// LAYER_TYPE is the type of layer being encoded.
type LAYER_TYPE int32

const (
	NON_VIDEO_CODING_LAYER LAYER_TYPE = 0
	VIDEO_CODING_LAYER     LAYER_TYPE = 1
)

// LAYER_NUM is the spatial layer num.
type LAYER_NUM int32

const (
	SPATIAL_LAYER_0   LAYER_NUM = 0
	SPATIAL_LAYER_1   LAYER_NUM = 1
	SPATIAL_LAYER_2   LAYER_NUM = 2
	SPATIAL_LAYER_3   LAYER_NUM = 3
	SPATIAL_LAYER_ALL LAYER_NUM = 4
)

// VIDEO_BITSTREAM_TYPE enumerates the type of video bitstream which is provided to decoder.
type VIDEO_BITSTREAM_TYPE int32

const (
	VIDEO_BITSTREAM_AVC     VIDEO_BITSTREAM_TYPE = 0
	VIDEO_BITSTREAM_SVC     VIDEO_BITSTREAM_TYPE = 1
	VIDEO_BITSTREAM_DEFAULT VIDEO_BITSTREAM_TYPE = VIDEO_BITSTREAM_SVC
)

// KEY_FRAME_REQUEST_TYPE enumerates the type of key frame request.
type KEY_FRAME_REQUEST_TYPE int32

const (
	NO_RECOVERY_REQUSET     KEY_FRAME_REQUEST_TYPE = 0
	LTR_RECOVERY_REQUEST    KEY_FRAME_REQUEST_TYPE = 1
	IDR_RECOVERY_REQUEST    KEY_FRAME_REQUEST_TYPE = 2
	NO_LTR_MARKING_FEEDBACK KEY_FRAME_REQUEST_TYPE = 3
	LTR_MARKING_SUCCESS     KEY_FRAME_REQUEST_TYPE = 4
	LTR_MARKING_FAILED      KEY_FRAME_REQUEST_TYPE = 5
)

// SLTRRecoverRequest is the structure for LTR recover request.
type SLTRRecoverRequest struct {
	UiFeedbackType       uint32 ///< IDR request or LTR recovery request
	UiIDRPicId           uint32 ///< distinguish request from different IDR
	ILastCorrectFrameNum int32
	ICurrentFrameNum     int32 ///< specify current decoder frame_num.
	ILayerId             int32 //specify the layer for recovery request
}

// SLTRMarkingFeedback is the structure for LTR marking feedback.
type SLTRMarkingFeedback struct {
	UiFeedbackType uint32 ///< mark failed or successful
	UiIDRPicId     uint32 ///< distinguish request from different IDR
	ILTRFrameNum   int32  ///< specify current decoder frame_num
	ILayerId       int32  //specify the layer for LTR marking feedback
}

// SLTRConfig is the structure for LTR configuration.
type SLTRConfig struct {
	BEnableLongTermReference bool  ///< 1: on, 0: off
	ILTRRefNum               int32 ///< TODO: not supported to set it arbitrary yet
}

// RC_MODES enumerates the type of rate control mode.
type RC_MODES int32

const (
	RC_QUALITY_MODE           RC_MODES = 0  ///< quality mode
	RC_BITRATE_MODE           RC_MODES = 1  ///< bitrate mode
	RC_BUFFERBASED_MODE       RC_MODES = 2  ///< no bitrate control,only using buffer status,adjust the video quality
	RC_TIMESTAMP_MODE         RC_MODES = 3  //rate control based timestamp
	RC_BITRATE_MODE_POST_SKIP RC_MODES = 4  ///< this is in-building RC MODE, WILL BE DELETED after algorithm tuning!
	RC_OFF_MODE               RC_MODES = -1 ///< rate control off mode
)

// EProfileIdc enumerates the type of profile id.
type EProfileIdc int32

const (
	PRO_UNKNOWN  EProfileIdc = 0
	PRO_BASELINE EProfileIdc = 66
	PRO_MAIN     EProfileIdc = 77
	PRO_EXTENDED EProfileIdc = 88
	PRO_HIGH     EProfileIdc = 100
	PRO_HIGH10   EProfileIdc = 110
	PRO_HIGH422  EProfileIdc = 122
	PRO_HIGH444  EProfileIdc = 144
	PRO_CAVLC444 EProfileIdc = 244

	PRO_SCALABLE_BASELINE EProfileIdc = 83
	PRO_SCALABLE_HIGH     EProfileIdc = 86
)

// ELevelIdc enumerates the type of level id.
type ELevelIdc int32

const (
	LEVEL_UNKNOWN ELevelIdc = 0
	LEVEL_1_0     ELevelIdc = 10
	LEVEL_1_B     ELevelIdc = 9
	LEVEL_1_1     ELevelIdc = 11
	LEVEL_1_2     ELevelIdc = 12
	LEVEL_1_3     ELevelIdc = 13
	LEVEL_2_0     ELevelIdc = 20
	LEVEL_2_1     ELevelIdc = 21
	LEVEL_2_2     ELevelIdc = 22
	LEVEL_3_0     ELevelIdc = 30
	LEVEL_3_1     ELevelIdc = 31
	LEVEL_3_2     ELevelIdc = 32
	LEVEL_4_0     ELevelIdc = 40
	LEVEL_4_1     ELevelIdc = 41
	LEVEL_4_2     ELevelIdc = 42
	LEVEL_5_0     ELevelIdc = 50
	LEVEL_5_1     ELevelIdc = 51
	LEVEL_5_2     ELevelIdc = 52
)

// Wels log levels (anonymous enum).
const (
	WELS_LOG_QUIET       = 0x00   ///< quiet mode
	WELS_LOG_ERROR       = 1 << 0 ///< error log iLevel
	WELS_LOG_WARNING     = 1 << 1 ///< Warning log iLevel
	WELS_LOG_INFO        = 1 << 2 ///< information log iLevel
	WELS_LOG_DEBUG       = 1 << 3 ///< debug log, critical algo log
	WELS_LOG_DETAIL      = 1 << 4 ///< per packet/frame log
	WELS_LOG_RESV        = 1 << 5 ///< resversed log iLevel
	WELS_LOG_LEVEL_COUNT = 6
	WELS_LOG_DEFAULT     = WELS_LOG_WARNING ///< default log iLevel in Wels codec
)

// SliceModeEnum enumerates the type of slice mode.
type SliceModeEnum int32

const (
	SM_SINGLE_SLICE      SliceModeEnum = 0 ///< | SliceNum==1
	SM_FIXEDSLCNUM_SLICE SliceModeEnum = 1 ///< | according to SliceNum        | enabled dynamic slicing for multi-thread
	SM_RASTER_SLICE      SliceModeEnum = 2 ///< | according to SlicesAssign    | need input of MB numbers each slice. In addition, if other constraint in SSliceArgument is presented, need to follow the constraints. Typically if MB num and slice size are both constrained, re-encoding may be involved.
	SM_SIZELIMITED_SLICE SliceModeEnum = 3 ///< | according to SliceSize       | slicing according to size, the slicing will be dynamic(have no idea about slice_nums until encoding current frame)
	SM_RESERVED          SliceModeEnum = 4
)

// SSliceArgument is the structure for slice argument.
type SSliceArgument struct {
	UiSliceMode           SliceModeEnum              ///< by default, uiSliceMode will be SM_SINGLE_SLICE
	UiSliceNum            uint32                     ///< only used when uiSliceMode=1, when uiSliceNum=0 means auto design it with cpu core number
	UiSliceMbNum          [MAX_SLICES_NUM_TMP]uint32 ///< only used when uiSliceMode=2; when =0 means setting one MB row a slice
	UiSliceSizeConstraint uint32                     ///< now only used when uiSliceMode=4
}

// EVideoFormatSPS enumerates the type of video format (EVideoFormat is already defined/used elsewhere!).
type EVideoFormatSPS int32

const (
	VF_COMPONENT EVideoFormatSPS = iota
	VF_PAL
	VF_NTSC
	VF_SECAM
	VF_MAC
	VF_UNDEF
	VF_NUM_ENUM
)

// EColorPrimaries enumerates the type of color primaries.
type EColorPrimaries int32

const (
	CP_RESERVED0 EColorPrimaries = iota
	CP_BT709
	CP_UNDEF
	CP_RESERVED3
	CP_BT470M
	CP_BT470BG
	CP_SMPTE170M
	CP_SMPTE240M
	CP_FILM
	CP_BT2020
	CP_NUM_ENUM
)

// ETransferCharacteristics enumerates the type of transfer characteristics.
type ETransferCharacteristics int32

const (
	TRC_RESERVED0 ETransferCharacteristics = iota
	TRC_BT709
	TRC_UNDEF
	TRC_RESERVED3
	TRC_BT470M
	TRC_BT470BG
	TRC_SMPTE170M
	TRC_SMPTE240M
	TRC_LINEAR
	TRC_LOG100
	TRC_LOG316
	TRC_IEC61966_2_4
	TRC_BT1361E
	TRC_IEC61966_2_1
	TRC_BT2020_10
	TRC_BT2020_12
	TRC_NUM_ENUM
)

// EColorMatrix enumerates the type of color matrix.
type EColorMatrix int32

const (
	CM_GBR EColorMatrix = iota
	CM_BT709
	CM_UNDEF
	CM_RESERVED3
	CM_FCC
	CM_BT470BG
	CM_SMPTE170M
	CM_SMPTE240M
	CM_YCGCO
	CM_BT2020NC
	CM_BT2020C
	CM_NUM_ENUM
)

// ESampleAspectRatio enumerates the type of sample aspect ratio.
type ESampleAspectRatio int32

const (
	ASP_UNSPECIFIED ESampleAspectRatio = 0
	ASP_1x1         ESampleAspectRatio = 1
	ASP_12x11       ESampleAspectRatio = 2
	ASP_10x11       ESampleAspectRatio = 3
	ASP_16x11       ESampleAspectRatio = 4
	ASP_40x33       ESampleAspectRatio = 5
	ASP_24x11       ESampleAspectRatio = 6
	ASP_20x11       ESampleAspectRatio = 7
	ASP_32x11       ESampleAspectRatio = 8
	ASP_80x33       ESampleAspectRatio = 9
	ASP_18x11       ESampleAspectRatio = 10
	ASP_15x11       ESampleAspectRatio = 11
	ASP_64x33       ESampleAspectRatio = 12
	ASP_160x99      ESampleAspectRatio = 13

	ASP_EXT_SAR ESampleAspectRatio = 255
)

// SSpatialLayerConfig is the structure for spatial layer configuration.
type SSpatialLayerConfig struct {
	IVideoWidth        int32       ///< width of picture in luminance samples of a layer
	IVideoHeight       int32       ///< height of picture in luminance samples of a layer
	FFrameRate         float32     ///< frame rate specified for a layer
	ISpatialBitrate    int32       ///< target bitrate for a spatial layer, in unit of bps
	IMaxSpatialBitrate int32       ///< maximum  bitrate for a spatial layer, in unit of bps
	UiProfileIdc       EProfileIdc ///< value of profile IDC (PRO_UNKNOWN for auto-detection)
	UiLevelIdc         ELevelIdc   ///< value of profile IDC (0 for auto-detection)
	IDLayerQp          int32       ///< value of level IDC (0 for auto-detection)

	SSliceArgument SSliceArgument

	// Note: members bVideoSignalTypePresent through uiColorMatrix below are also defined in SWelsSPS in parameter_sets.h.
	BVideoSignalTypePresent   bool  // false => do not write any of the following information to the header
	UiVideoFormat             uint8 // EVideoFormatSPS; 3 bits in header; 0-5 => component, kpal, ntsc, secam, mac, undef
	BFullRange                bool  // false => analog video data range [16, 235]; true => full data range [0,255]
	BColorDescriptionPresent  bool  // false => do not write any of the following three items to the header
	UiColorPrimaries          uint8 // EColorPrimaries; 8 bits in header; 0 - 9 => ???, bt709, undef, ???, bt470m, bt470bg, smpte170m, smpte240m, film, bt2020
	UiTransferCharacteristics uint8 // ETransferCharacteristics; 8 bits in header; 0 - 15 => ???, bt709, undef, ???, bt470m, bt470bg, smpte170m, smpte240m, linear, log100, log316, iec61966-2-4, bt1361e, iec61966-2-1, bt2020-10, bt2020-12
	UiColorMatrix             uint8 // EColorMatrix; 8 bits in header (corresponds to FFmpeg "colorspace"); 0 - 10 => GBR, bt709, undef, ???, fcc, bt470bg, smpte170m, smpte240m, YCgCo, bt2020nc, bt2020c

	BAspectRatioPresent   bool               ///< aspect ratio present in VUI
	EAspectRatio          ESampleAspectRatio ///< aspect ratio idc
	SAspectRatioExtWidth  uint16             ///< use if aspect ratio idc == 255
	SAspectRatioExtHeight uint16             ///< use if aspect ratio idc == 255
}

// EUsageType is the encoder usage type.
type EUsageType int32

const (
	CAMERA_VIDEO_REAL_TIME   EUsageType = iota ///< camera video for real-time communication
	SCREEN_CONTENT_REAL_TIME                   ///< screen content signal
	CAMERA_VIDEO_NON_REAL_TIME
	SCREEN_CONTENT_NON_REAL_TIME
	INPUT_CONTENT_TYPE_ALL
)

// ECOMPLEXITY_MODE enumerates the complexity mode.
type ECOMPLEXITY_MODE int32

const (
	LOW_COMPLEXITY    ECOMPLEXITY_MODE = iota ///< the lowest compleixty,the fastest speed,
	MEDIUM_COMPLEXITY                         ///< medium complexity, medium speed,medium quality
	HIGH_COMPLEXITY                           ///< high complexity, lowest speed, high quality
)

// EParameterSetStrategy enumerates the strategy of SPS/PPS.
type EParameterSetStrategy int32

const (
	CONSTANT_ID                    EParameterSetStrategy = 0    ///< constant id in SPS/PPS
	INCREASING_ID                  EParameterSetStrategy = 0x01 ///< SPS/PPS id increases at each IDR
	SPS_LISTING                    EParameterSetStrategy = 0x02 ///< using SPS in the existing list if possible
	SPS_LISTING_AND_PPS_INCREASING EParameterSetStrategy = 0x03
	SPS_PPS_LISTING                EParameterSetStrategy = 0x06
)

// SEncParamBase holds the SVC encoding parameters (struct TagEncParamBase).
type SEncParamBase struct {
	IUsageType EUsageType ///< application type; please refer to the definition of EUsageType

	IPicWidth      int32    ///< width of picture in luminance samples (the maximum of all layers if multiple spatial layers presents)
	IPicHeight     int32    ///< height of picture in luminance samples((the maximum of all layers if multiple spatial layers presents)
	ITargetBitrate int32    ///< target bitrate desired, in unit of bps
	IRCMode        RC_MODES ///< rate control mode
	FMaxFrameRate  float32  ///< maximal input frame rate
}

// PEncParamBase is the pointer alias of SEncParamBase.
type PEncParamBase = *SEncParamBase

// SEncParamExt holds the SVC encoding parameters extension (struct TagEncParamExt).
type SEncParamExt struct {
	IUsageType EUsageType ///< same as in TagEncParamBase

	IPicWidth      int32    ///< same as in TagEncParamBase
	IPicHeight     int32    ///< same as in TagEncParamBase
	ITargetBitrate int32    ///< same as in TagEncParamBase
	IRCMode        RC_MODES ///< same as in TagEncParamBase
	FMaxFrameRate  float32  ///< same as in TagEncParamBase

	ITemporalLayerNum int32 ///< temporal layer number, max temporal layer = 4
	ISpatialLayerNum  int32 ///< spatial layer number,1<= iSpatialLayerNum <= MAX_SPATIAL_LAYER_NUM, MAX_SPATIAL_LAYER_NUM = 4
	SSpatialLayers    [MAX_SPATIAL_LAYER_NUM]SSpatialLayerConfig

	IComplexityMode        ECOMPLEXITY_MODE
	UiIntraPeriod          uint32                ///< period of Intra frame
	INumRefFrame           int32                 ///< number of reference frame used
	ESpsPpsIdStrategy      EParameterSetStrategy ///< different stategy in adjust ID in SPS/PPS: 0- constant ID, 1-additional ID, 6-mapping and additional
	BPrefixNalAddingCtrl   bool                  ///< false:not use Prefix NAL; true: use Prefix NAL
	BEnableSSEI            bool                  ///< false:not use SSEI; true: use SSEI -- TODO: planning to remove the interface of SSEI
	BSimulcastAVC          bool                  ///< (when encoding more than 1 spatial layer) false: use SVC syntax for higher layers; true: use Simulcast AVC
	IPaddingFlag           int32                 ///< 0:disable padding;1:padding
	IEntropyCodingModeFlag int32                 ///< 0:CAVLC  1:CABAC.

	/* rc control */
	BEnableFrameSkip bool   ///< False: don't skip frame even if VBV buffer overflow.True: allow skipping frames to keep the bitrate within limits
	IMaxBitrate      int32  ///< the maximum bitrate, in unit of bps, set it to UNSPECIFIED_BIT_RATE if not needed
	IMaxQp           int32  ///< the maximum QP encoder supports
	IMinQp           int32  ///< the minmum QP encoder supports
	UiMaxNalSize     uint32 ///< the maximum NAL size.  This value should be not 0 for dynamic slice mode

	/*LTR settings*/
	BEnableLongTermReference bool   ///< 1: on, 0: off
	ILTRRefNum               int32  ///< the number of LTR(long term reference),TODO: not supported to set it arbitrary yet
	ILtrMarkPeriod           uint32 ///< the LTR marked period that is used in feedback.
	/* multi-thread settings*/
	IMultipleThreadIdc uint16 ///< 1 # 0: auto(dynamic imp. internal encoder); 1: multiple threads imp. disabled; lager than 1: count number of threads;
	BUseLoadBalancing  bool   ///< only used when uiSliceMode=1 or 3, will change slicing of a picture during the run-time of multi-thread encoding, so the result of each run may be different

	/* Deblocking loop filter */
	ILoopFilterDisableIdc    int32 ///< 0: on, 1: off, 2: on except for slice boundaries
	ILoopFilterAlphaC0Offset int32 ///< AlphaOffset: valid range [-6, 6], default 0
	ILoopFilterBetaOffset    int32 ///< BetaOffset: valid range [-6, 6], default 0
	/*pre-processing feature*/
	BEnableDenoise             bool ///< denoise control
	BEnableBackgroundDetection bool ///< background detection control //VAA_BACKGROUND_DETECTION //BGD cmd
	BEnableAdaptiveQuant       bool ///< adaptive quantization control
	BEnableFrameCroppingFlag   bool ///< enable frame cropping flag: TRUE always in application
	BEnableSceneChangeDetect   bool

	BIsLosslessLink  bool  ///< LTR advanced setting
	BFixRCOverShoot  bool  ///< fix rate control overshooting
	IIdrBitrateRatio int32 ///< the target bits of IDR is (idr_bitrate_ratio/100) * average target bit per frame.
	BPsnrY           bool  ///< get Y PSNR stats for the whole video sequence
	BPsnrU           bool  ///< get U PSNR stats for the whole video sequence
	BPsnrV           bool  ///< get V PSNR stats for the whole video sequence
}

// SVideoProperty shows the property of video bitstream.
type SVideoProperty struct {
	Size         uint32               ///< size of the struct (C field "size")
	EVideoBsType VIDEO_BITSTREAM_TYPE ///< video stream type (AVC/SVC)
}

// SDecodingParam holds the SVC decoding parameters (struct TagSVCDecodingParam).
type SDecodingParam struct {
	PFileNameRestructed string ///< file name of reconstructed frame used for PSNR calculation based debug (char* -> string; "" == NULL)

	UiCpuLoad       uint32 ///< CPU load
	UiTargetDqLayer uint8  ///< setting target dq layer id

	EEcActiveIdc ERROR_CON_IDC ///< whether active error concealment feature in decoder
	BParseOnly   bool          ///< decoder for parse only, no reconstruction. When it is true, SPS/PPS size should not exceed SPS_PPS_BS_SIZE (128). Otherwise, it will return error info

	SVideoProperty SVideoProperty ///< video stream property
}

// PDecodingParam is the pointer alias of SDecodingParam.
type PDecodingParam = *SDecodingParam

// SLayerBSInfo is the bitstream information of a layer being encoded.
type SLayerBSInfo struct {
	UiTemporalId uint8
	UiSpatialId  uint8
	UiQualityId  uint8
	EFrameType   EVideoFrameType
	UiLayerType  uint8

	// The sub sequence layers are ordered hierarchically based on their
	// dependency on each other so that any picture in a layer shall not be
	// predicted from any picture on any higher layer.
	ISubSeqId int32 ///< refer to D.2.11 Sub-sequence information SEI message semantics
	INalCount int32 ///< count number of NAL coded already
	// PNalLengthInByte: length of NAL size in byte from 0 to iNalCount-1.
	// int* -> []int32 slice starting at the first NAL length of this layer
	// (it aliases the encoder's per-frame NAL length array).
	PNalLengthInByte []int32
	// PBsBuf: buffer of bitstream contained. unsigned char* -> []uint8 slice
	// starting at the first byte of this layer's bitstream (it aliases the
	// encoder's frame bitstream buffer); the layer's size is the sum of
	// PNalLengthInByte[0:INalCount].
	PBsBuf []uint8
	RPsnr  [3]float32 ///< PSNR values for Y/U/V
}

// PLayerBSInfo is the pointer alias of SLayerBSInfo.
type PLayerBSInfo = *SLayerBSInfo

// SFrameBSInfo is the frame bit stream info.
type SFrameBSInfo struct {
	ILayerNum  int32
	SLayerInfo [MAX_LAYER_NUM_OF_FRAME]SLayerBSInfo

	EFrameType        EVideoFrameType
	IFrameSizeInBytes int32
	UiTimeStamp       int64
}

// PFrameBSInfo is the pointer alias of SFrameBSInfo.
type PFrameBSInfo = *SFrameBSInfo

// MaxForcedShift bounds forced vertical motion in luma rows. It fits the
// ordinary motion search at every supported H.264 level, including level 1.
const MaxForcedShift = 63

// SSourcePicture is the structure for source picture (struct Source_Picture_s).
type SSourcePicture struct {
	IColorFormat int32    ///< color space type
	IStride      [4]int32 ///< stride for each plane pData
	// PData: plane pData. unsigned char* pData[4] -> one slice per plane,
	// each starting at that plane's origin (top-left sample); rows are
	// IStride[i] bytes apart.
	PData       [4][]uint8
	IPicWidth   int32 ///< luma picture width in x coordinate
	IPicHeight  int32 ///< luma picture height in y coordinate
	UiTimeStamp int64 ///< timestamp of the source picture, unit: millisecond
	BPsnrY      bool  ///< get Y PSNR for this frame
	BPsnrU      bool  ///< get U PSNR for this frame
	BPsnrV      bool  ///< get V PSNR for this frame
	// Go-only, borrowed read-only during EncodeFrame. A true entry certifies
	// that the entire source Y/U/V macroblock is unchanged since the immediately
	// preceding successful encoded picture. Entries are row-major, with
	// ceil(IPicWidth/16)*ceil(IPicHeight/16) elements. Nil/wrong length disables
	// this optional hint; incorrect hints can suppress actual picture changes.
	UnchangedMacroblocks []bool
	// Go-only, borrowed read-only during EncodeFrame, with the layout of
	// UnchangedMacroblocks and used only with a valid one. A true entry asks
	// for that macroblock to repeat a reference block, as a P_SKIP, whatever
	// its source: the colocated block, or with ForcedShift, the block that many
	// luma rows lower (negative: higher). A caller copying an area itself, for
	// example, keeps the decoded picture moving with it. When the motion
	// predictor rules that skip out, the macroblock is coded normally from its
	// source instead.
	ForcedMacroblocks []bool
	// ForcedShift, optional and with the same layout, gives each forced
	// macroblock's vertical displacement in luma rows. Values outside
	// [-MaxForcedShift, MaxForcedShift] are ignored: the macroblock is coded
	// normally from its source instead.
	ForcedShift []int16
}

// SBitrateInfo is the structure for bit rate info.
type SBitrateInfo struct {
	ILayer   LAYER_NUM
	IBitrate int32 ///< the maximum bitrate
}

// SDumpLayer is the structure for dump layer info.
type SDumpLayer struct {
	ILayer    int32
	PFileName string // char* -> string
}

// SProfileInfo is the structure for profile info in layer.
type SProfileInfo struct {
	ILayer       int32
	UiProfileIdc EProfileIdc ///< the profile info
}

// SLevelInfo is the structure for level info in layer.
type SLevelInfo struct {
	ILayer     int32
	UiLevelIdc ELevelIdc ///< the level info
}

// SDeliveryStatus is the structure for delivery status.
type SDeliveryStatus struct {
	BDeliveryFlag  bool  ///< 0: the previous frame isn't delivered,1: the previous frame is delivered
	IDropFrameType int32 ///< the frame type that is dropped; reserved
	IDropFrameSize int32 ///< the frame size that is dropped; reserved
}

// SDecoderCapability is the capability of decoder, for SDP negotiation.
type SDecoderCapability struct {
	IProfileIdc int32 ///< profile_idc
	IProfileIop int32 ///< profile-iop
	ILevelIdc   int32 ///< level_idc
	IMaxMbps    int32 ///< max-mbps
	IMaxFs      int32 ///< max-fs
	IMaxCpb     int32 ///< max-cpb
	IMaxDpb     int32 ///< max-dpb
	IMaxBr      int32 ///< max-br
	BRedPicCap  bool  ///< redundant-pic-cap
}

// SParserBsInfo is the structure for parse only output.
type SParserBsInfo struct {
	INalNum int32 ///< total NAL number in current AU
	// PNalLenInByte: each nal length. int* -> []int32 (caller- or
	// decoder-provided array, indexed 0..INalNum-1).
	PNalLenInByte []int32
	// PDstBuff: outputted dst buffer for parsed bitstream. unsigned char* ->
	// []uint8 starting at the first output byte.
	PDstBuff          []uint8
	ISpsWidthInPixel  int32  ///< required SPS width info
	ISpsHeightInPixel int32  ///< required SPS height info
	UiInBsTimeStamp   uint64 ///< input BS timestamp
	UiOutBsTimeStamp  uint64 ///< output BS timestamp
}

// PParserBsInfo is the pointer alias of SParserBsInfo.
type PParserBsInfo = *SParserBsInfo

// SEncoderStatistics is the structure for encoder statistics.
type SEncoderStatistics struct {
	UiWidth  uint32 ///< the width of encoded frame
	UiHeight uint32 ///< the height of encoded frame
	//following standard, will be 16x aligned, if there are multiple spatial, this is of the highest
	FAverageFrameSpeedInMs float32 ///< average_Encoding_Time

	// rate control related
	FAverageFrameRate float32 ///< the average frame rate in, calculate since encoding starts, supposed that the input timestamp is in unit of ms
	FLatestFrameRate  float32 ///< the frame rate in, in the last second, supposed that the input timestamp is in unit of ms (? useful for checking BR, but is it easy to calculate?
	UiBitRate         uint32  ///< sendrate in Bits per second, calculated within the set time-window
	UiAverageFrameQP  uint32  ///< the average QP of last encoded frame

	UiInputFrameCount   uint32 ///< number of frames
	UiSkippedFrameCount uint32 ///< number of frames

	UiResolutionChangeTimes uint32 ///< uiResolutionChangeTimes
	UiIDRReqNum             uint32 ///< number of IDR requests
	UiIDRSentNum            uint32 ///< number of actual IDRs sent
	UiLTRSentNum            uint32 ///< number of LTR sent/marked

	IStatisticsTs int64 ///< Timestamp of updating the statistics

	// unsigned long -> uint32 (PORTING.md: long -> 32 bit)
	ITotalEncodedBytes        uint32
	ILastStatisticsBytes      uint32
	ILastStatisticsFrameCount uint32
}

// SDecoderStatistics is the structure for decoder statistics.
type SDecoderStatistics struct {
	UiWidth                      uint32  ///< the width of encode/decode frame
	UiHeight                     uint32  ///< the height of encode/decode frame
	FAverageFrameSpeedInMs       float32 ///< average_Decoding_Time
	FActualAverageFrameSpeedInMs float32 ///< actual average_Decoding_Time, including freezing pictures
	UiDecodedFrameCount          uint32  ///< number of frames
	UiResolutionChangeTimes      uint32  ///< uiResolutionChangeTimes
	UiIDRCorrectNum              uint32  ///< number of correct IDR received
	//EC on related
	UiAvgEcRatio          uint32 ///< when EC is on, the average ratio of total EC areas, can be an indicator of reconstruction quality
	UiAvgEcPropRatio      uint32 ///< when EC is on, the rough average ratio of propogate EC areas, can be an indicator of reconstruction quality
	UiEcIDRNum            uint32 ///< number of actual unintegrity IDR or not received but eced
	UiEcFrameNum          uint32 ///<
	UiIDRLostNum          uint32 ///< number of whole lost IDR
	UiFreezingIDRNum      uint32 ///< number of freezing IDR with error (partly received), under resolution change
	UiFreezingNonIDRNum   uint32 ///< number of freezing non-IDR with error
	IAvgLumaQp            int32  ///< average luma QP. default: -1, no correct frame outputted
	ISpsReportErrorNum    int32  ///< number of Sps Invalid report
	ISubSpsReportErrorNum int32  ///< number of SubSps Invalid report
	IPpsReportErrorNum    int32  ///< number of Pps Invalid report
	ISpsNoExistNalNum     int32  ///< number of Sps NoExist Nal
	ISubSpsNoExistNalNum  int32  ///< number of SubSps NoExist Nal
	IPpsNoExistNalNum     int32  ///< number of Pps NoExist Nal

	UiProfile uint32 ///< Profile idc in syntax
	UiLevel   uint32 ///< level idc according to Annex A-1

	ICurrentActiveSpsId int32 ///< current active SPS id
	ICurrentActivePpsId int32 ///< current active PPS id

	IStatisticsLogInterval uint32 ///< frame interval of statistics log
}

// SVuiSarInfo is the structure for sample aspect ratio (SAR) info in VUI.
type SVuiSarInfo struct {
	UiSarWidth               uint32 ///< SAR width
	UiSarHeight              uint32 ///< SAR height
	BOverscanAppropriateFlag bool   ///< SAR overscan flag
}

// PVuiSarInfo is the pointer alias of SVuiSarInfo.
type PVuiSarInfo = *SVuiSarInfo
