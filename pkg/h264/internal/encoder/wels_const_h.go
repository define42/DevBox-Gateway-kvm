// Port of codec/encoder/core/inc/wels_const.h and as264_common.h.
// (wels_const_common.h lives in package common.)

package encoder

import (
	"math"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

// as264_common.h only holds feature switches: DISABLE_FMO_FEATURE and
// SINGLE_REF_FRAME are defined (the FMO paths are therefore not ported), all
// debug/trace output switches are off.

const (
	STATISTICS_LOG_INTERVAL_MS = 5000 // output statistics log every 5s

	INTRA_4x4_MODE_NUM          = 8
	MB_LUMA_CHROMA_BLOCK4x4_NUM = 24

	MAX_PPS_COUNT_LIMITED = 57                    // limit the max ID of PPS because of known limitation of receiver endpoints
	MAX_PPS_COUNT         = MAX_PPS_COUNT_LIMITED // in Standard is 256: Count number of PPS

	PARA_SET_TYPE                  = 3 // SPS+PPS
	PARA_SET_TYPE_AVCSPS           = 0
	PARA_SET_TYPE_SUBSETSPS        = 1
	PARA_SET_TYPE_PPS              = 2
	MAX_VERTICAL_MV_RANGE          = 1024 // TODO, for allocate enough memory for transpose
	MAX_FRAME_RATE                 = 60   // maximal frame rate to support
	MIN_FRAME_RATE                 = 1    // minimal frame rate need support
	MAX_BIT_RATE                   = math.MaxInt32
	MIN_BIT_RATE                   = 1 // minimal bit rate need support
	SVC_QUALITY_BASE_QP            = 26
	MAX_SLICEGROUP_IDS             = 8 // Count number of SSlice Groups
	MAX_THREADS_NUM                = 4 // assume to support up to 4 logical cores(threads)
	INTPEL_NEEDED_MARGIN           = 3 // for safe sub-pel MC
	I420_PLANES                    = 3
	COMPRESS_RATIO_THR             = float32(1.0) // set to size of the original data
	SSEI_BUFFER_SIZE               = 128
	SPS_BUFFER_SIZE                = 32
	PPS_BUFFER_SIZE                = 16
	MAX_MACROBLOCK_SIZE_IN_BYTE    = 400 // 3200/8, 3200 is from Annex A.3.1.(n)
	MAX_MACROBLOCK_SIZE_IN_BYTE_x2 = MAX_MACROBLOCK_SIZE_IN_BYTE << 1

	MAX_DEPENDENCY_LAYER = api.MAX_SPATIAL_LAYER_NUM  // Maximal dependency layer
	MAX_TEMPORAL_LEVEL   = api.MAX_TEMPORAL_LAYER_NUM // Maximal temporal level
	MAX_QUALITY_LEVEL    = api.MAX_QUALITY_LAYER_NUM  // Maximal quality level

	MAX_GOP_SIZE             = 1 << (MAX_TEMPORAL_LEVEL - 1)
	MAX_SHORT_REF_COUNT      = MAX_GOP_SIZE >> 1 // 16 in standard, maximal count number of short reference pictures
	LONG_TERM_REF_NUM        = 2
	LONG_TERM_REF_NUM_SCREEN = 4
	MAX_REF_PIC_COUNT        = 16 // 32 in standard, maximal Short + Long reference pictures
	MIN_REF_PIC_COUNT        = 1  // minimal count number of reference pictures, 1 short + 2 key reference based?
	MAX_MULTI_REF_PIC_COUNT  = 1  // maximum multi-reference number
	MAX_MMCO_COUNT           = 66

	// adjusted numbers reference picture functionality related definition
	MAX_REFERENCE_MMCO_COUNT_NUM           = 4 // adjusted MAX_MMCO_COUNT(66 in standard) definition per encoder design
	MAX_REFERENCE_REORDER_COUNT_NUM        = 2 // adjusted MAX_REF_PIC_COUNT(32 in standard) for reference reordering definition per encoder design
	MAX_REFERENCE_PICTURE_COUNT_NUM_CAMERA = MAX_SHORT_REF_COUNT + LONG_TERM_REF_NUM
	MAX_REFERENCE_PICTURE_COUNT_NUM_SCREEN = MAX_SHORT_REF_COUNT + LONG_TERM_REF_NUM_SCREEN

	BASE_DEPENDENCY_ID = 0
	MAX_DQ_LAYER_NUM   = MAX_DEPENDENCY_LAYER
	INVALID_ID         = -1

	NAL_HEADER_ADD_0X30BYTES = 20
	SLICE_NUM_EXPAND_COEF    = 2
)

const (
	BLOCK_16x16    = 0
	BLOCK_16x8     = 1
	BLOCK_8x16     = 2
	BLOCK_8x8      = 3
	BLOCK_4x4      = 4
	BLOCK_8x4      = 5
	BLOCK_4x8      = 6
	BLOCK_SIZE_ALL = 7
)

// LTR_MARKING_RECEIVE_STATE
type LTR_MARKING_RECEIVE_STATE int32

const (
	RECIEVE_UNKOWN  LTR_MARKING_RECEIVE_STATE = 0
	RECIEVE_SUCCESS LTR_MARKING_RECEIVE_STATE = 1
	RECIEVE_FAILED  LTR_MARKING_RECEIVE_STATE = 2
)

const (
	CUR_AU_IDX = 0 // index symbol for current access unit
	SUC_AU_IDX = 1 // index symbol for successive access unit
)

const (
	ENC_RETURN_SUCCESS          = 0
	ENC_RETURN_MEMALLOCERR      = 0x01 // will free memory and uninit
	ENC_RETURN_UNSUPPORTED_PARA = 0x02 // unsupported setting
	ENC_RETURN_UNEXPECTED       = 0x04 // unexpected value
	ENC_RETURN_CORRECTED        = 0x08 // unexpected value but corrected by encoder
	ENC_RETURN_INVALIDINPUT     = 0x10 // invalid input
	ENC_RETURN_MEMOVERFLOWFOUND = 0x20
	ENC_RETURN_VLCOVERFLOWFOUND = 0x40
	ENC_RETURN_KNOWN_ISSUE      = 0x80
)
