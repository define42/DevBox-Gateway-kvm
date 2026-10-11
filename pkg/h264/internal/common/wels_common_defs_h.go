package common

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"

// Port of codec/common/inc/wels_common_defs.h.
//
// Enum types are Go named types; their constants are untyped so they can be
// used with both the enum type and plain int32 variables (as in C).

const (
	CTX_NA             = 0
	WELS_CONTEXT_COUNT = 460
	LEVEL_NUMBER       = 17
)

// SLevelLimits mirrors TagLevelLimits.
type SLevelLimits struct {
	UiLevelIdc    api.ELevelIdc // level idc
	UiMaxMBPS     uint32        // Max macroblock processing rate(MB/s)
	UiMaxFS       uint32        // Max frame sizea(MBs)
	UiMaxDPBMbs   uint32        // Max decoded picture buffer size(MBs)
	UiMaxBR       uint32        // Max video bit rate
	UiMaxCPB      uint32        // Max CPB size
	IMinVmv       int16         // Vertical MV component range upper bound
	IMaxVmv       int16         // Vertical MV component range lower bound
	UiMinCR       uint16        // Min compression ration
	IMaxMvsPer2Mb int16         // Max number of motion vectors per two consecutive MBs
}

const CpbBrNalFactor = 1200 //baseline,main,and extended profiles.

// EWelsNalUnitType : NAL Unit Type (5 Bits)
type EWelsNalUnitType int32

const (
	NAL_UNIT_UNSPEC_0        = 0
	NAL_UNIT_CODED_SLICE     = 1
	NAL_UNIT_CODED_SLICE_DPA = 2
	NAL_UNIT_CODED_SLICE_DPB = 3
	NAL_UNIT_CODED_SLICE_DPC = 4
	NAL_UNIT_CODED_SLICE_IDR = 5
	NAL_UNIT_SEI             = 6
	NAL_UNIT_SPS             = 7
	NAL_UNIT_PPS             = 8
	NAL_UNIT_AU_DELIMITER    = 9
	NAL_UNIT_END_OF_SEQ      = 10
	NAL_UNIT_END_OF_STR      = 11
	NAL_UNIT_FILLER_DATA     = 12
	NAL_UNIT_SPS_EXT         = 13
	NAL_UNIT_PREFIX          = 14
	NAL_UNIT_SUBSET_SPS      = 15
	NAL_UNIT_DEPTH_PARAM     = 16 // NAL_UNIT_RESV_16
	NAL_UNIT_RESV_17         = 17
	NAL_UNIT_RESV_18         = 18
	NAL_UNIT_AUX_CODED_SLICE = 19
	NAL_UNIT_CODED_SLICE_EXT = 20
	NAL_UNIT_MVC_SLICE_EXT   = 21 // NAL_UNIT_RESV_21
	NAL_UNIT_RESV_22         = 22
	NAL_UNIT_RESV_23         = 23
	NAL_UNIT_UNSPEC_24       = 24
	NAL_UNIT_UNSPEC_25       = 25
	NAL_UNIT_UNSPEC_26       = 26
	NAL_UNIT_UNSPEC_27       = 27
	NAL_UNIT_UNSPEC_28       = 28
	NAL_UNIT_UNSPEC_29       = 29
	NAL_UNIT_UNSPEC_30       = 30
	NAL_UNIT_UNSPEC_31       = 31
)

// EWelsNalRefIdc : NAL Reference IDC (2 Bits)
type EWelsNalRefIdc int32

const (
	NRI_PRI_LOWEST  = 0
	NRI_PRI_LOW     = 1
	NRI_PRI_HIGH    = 2
	NRI_PRI_HIGHEST = 3
)

// EVclType : VCL TYPE
type EVclType int32

const (
	NON_VCL = 0
	VCL     = 1
	NOT_APP = 2
)

// IS_VCL_NAL is (g_keTypeMap[t][ext_idx] == VCL).
func IS_VCL_NAL[T, U Integer](t T, ext_idx U) bool { return G_keTypeMap[t][ext_idx] == VCL }

func IS_PARAM_SETS_NALS[T Integer](t T) bool {
	return t == NAL_UNIT_SPS || t == NAL_UNIT_PPS || t == NAL_UNIT_SUBSET_SPS
}
func IS_SPS_NAL[T Integer](t T) bool          { return t == NAL_UNIT_SPS }
func IS_SUBSET_SPS_NAL[T Integer](t T) bool   { return t == NAL_UNIT_SUBSET_SPS }
func IS_PPS_NAL[T Integer](t T) bool          { return t == NAL_UNIT_PPS }
func IS_SEI_NAL[T Integer](t T) bool          { return t == NAL_UNIT_SEI }
func IS_AU_DELIMITER_NAL[T Integer](t T) bool { return t == NAL_UNIT_AU_DELIMITER }
func IS_PREFIX_NAL[T Integer](t T) bool       { return t == NAL_UNIT_PREFIX }
func IS_SUBSET_SPS_USED[T Integer](t T) bool {
	return t == NAL_UNIT_SUBSET_SPS || t == NAL_UNIT_CODED_SLICE_EXT
}
func IS_VCL_NAL_AVC_BASE[T Integer](t T) bool {
	return t == NAL_UNIT_CODED_SLICE || t == NAL_UNIT_CODED_SLICE_IDR
}
func IS_NEW_INTRODUCED_SVC_NAL[T Integer](t T) bool {
	return t == NAL_UNIT_PREFIX || t == NAL_UNIT_CODED_SLICE_EXT
}

// EWelsSliceType : Base SSlice Types
type EWelsSliceType int32

const (
	P_SLICE       = 0
	B_SLICE       = 1
	I_SLICE       = 2
	SP_SLICE      = 3
	SI_SLICE      = 4
	UNKNOWN_SLICE = 5
)

// ESliceTypeExt : SSlice Types in scalable extension
type ESliceTypeExt int32

const (
	EP_SLICE = 0 // EP_SLICE: 0, 5
	EB_SLICE = 1 // EB_SLICE: 1, 6
	EI_SLICE = 2 // EI_SLICE: 2, 7
)

// EListIndex : List Index
type EListIndex int32

const (
	LIST_0 = 0
	LIST_1 = 1
	LIST_A = 2
)

// EMvComp : Motion Vector components
type EMvComp int32

const (
	MV_X = 0
	MV_Y = 1
	MV_A = 2
)

// EChromaComp : Chroma Components
type EChromaComp int32

const (
	CHROMA_CB = 0
	CHROMA_CR = 1
	CHROMA_A  = 2
)

// EMmcoCode : Memory Management Control Operation (MMCO) code
type EMmcoCode int32

const (
	MMCO_END          = 0
	MMCO_SHORT2UNUSED = 1
	MMCO_LONG2UNUSED  = 2
	MMCO_SHORT2LONG   = 3
	MMCO_SET_MAX_LONG = 4
	MMCO_RESET        = 5
	MMCO_LONG         = 6
)

type EVuiVideoFormat int32

const (
	VUI_COMPONENT   = 0
	VUI_PAL         = 1
	VUI_NTSC        = 2
	VUI_SECAM       = 3
	VUI_MAC         = 4
	VUI_UNSPECIFIED = 5
	VUI_RESERVED1   = 6
	VUI_RESERVED2   = 7
)

// SBitStringAux mirrors TagBitStringAux (bit-stream auxiliary reading /
// writing).
//
// Pointer representation: the three C pointers pStartBuf, pEndBuf and
// pCurBuf all point into the same buffer. In Go that buffer is PBuf (the
// whole underlying slice) and PStartBuf, PEndBuf and PCurBuf are int offsets
// into it, so `pBs->pCurBuf - pBs->pStartBuf` stays
// `pBs.PCurBuf - pBs.PStartBuf` and `pBs->pCurBuf[i]` becomes
// `pBs.PBuf[pBs.PCurBuf+i]`.
type SBitStringAux struct {
	PBuf      []uint8 // underlying buffer that the offsets below index
	PStartBuf int     // buffer to start position (offset into PBuf)
	PEndBuf   int     // buffer + length (offset into PBuf)
	IBits     int32   // count bits of overall bitstreaming input

	IIndex    IntX_t // only for cavlc usage
	PCurBuf   int    // current reading position (offset into PBuf)
	UiCurBits uint32
	ILeftBits int32 // count number of available bits left ([1, 8]),
	// need pointer to next byte start position in case 0 bit left then 8 instead
}

type PBitStringAux = *SBitStringAux

// SNalUnitHeader : NAL Unix Header in AVC, refer to Page 56 in JVT X201wcm
type SNalUnitHeader struct {
	UiForbiddenZeroBit uint8
	UiNalRefIdc        uint8
	ENalUnitType       EWelsNalUnitType
	UiReservedOneByte  uint8 // only padding usage
}

type PNalUnitHeader = *SNalUnitHeader

// SNalUnitHeaderExt : NAL Unit Header in scalable extension syntax, refer to
// Page 390 in JVT X201wcm
type SNalUnitHeaderExt struct {
	SNalUnitHeader SNalUnitHeader

	// uint8_t   reserved_one_bit;
	BIdrFlag              bool
	UiPriorityId          uint8
	INoInterLayerPredFlag int8 // change as int8_t to support 3 values probably in encoder
	UiDependencyId        uint8

	UiQualityId        uint8
	UiTemporalId       uint8
	BUseRefBasePicFlag bool
	BDiscardableFlag   bool

	BOutputFlag          bool
	UiReservedThree2Bits uint8
	// Derived variable(s)
	UiLayerDqId uint8
	BNalExtFlag bool
}

type PNalUnitHeaderExt = *SNalUnitHeaderExt

// AVC MB types
const (
	MB_TYPE_INTRA4x4   = 0x00000001
	MB_TYPE_INTRA16x16 = 0x00000002
	MB_TYPE_INTRA8x8   = 0x00000004
	MB_TYPE_16x16      = 0x00000008
	MB_TYPE_16x8       = 0x00000010
	MB_TYPE_8x16       = 0x00000020
	MB_TYPE_8x8        = 0x00000040
	MB_TYPE_8x8_REF0   = 0x00000080
	MB_TYPE_SKIP       = 0x00000100
	MB_TYPE_INTRA_PCM  = 0x00000200
	MB_TYPE_INTRA_BL   = 0x00000400
	MB_TYPE_DIRECT     = 0x00000800
	MB_TYPE_P0L0       = 0x00001000
	MB_TYPE_P1L0       = 0x00002000
	MB_TYPE_P0L1       = 0x00004000
	MB_TYPE_P1L1       = 0x00008000
	MB_TYPE_L0         = MB_TYPE_P0L0 | MB_TYPE_P1L0
	MB_TYPE_L1         = MB_TYPE_P0L1 | MB_TYPE_P1L1

	SUB_MB_TYPE_8x8 = 0x00000001
	SUB_MB_TYPE_8x4 = 0x00000002
	SUB_MB_TYPE_4x8 = 0x00000004
	SUB_MB_TYPE_4x4 = 0x00000008

	MB_TYPE_INTRA = MB_TYPE_INTRA4x4 | MB_TYPE_INTRA16x16 | MB_TYPE_INTRA8x8 | MB_TYPE_INTRA_PCM
	MB_TYPE_INTER = MB_TYPE_16x16 | MB_TYPE_16x8 | MB_TYPE_8x16 | MB_TYPE_8x8 | MB_TYPE_8x8_REF0 | MB_TYPE_SKIP | MB_TYPE_DIRECT
)

// The IS_* macros return bool (they are only used in boolean context). The
// type argument is widened to uint32 like the C int promotion would.

func IS_INTRA4x4[T Integer](t T) bool { return MB_TYPE_INTRA4x4 == uint32(t) }
func IS_INTRA8x8[T Integer](t T) bool { return MB_TYPE_INTRA8x8 == uint32(t) }
func IS_INTRANxN[T Integer](t T) bool {
	return MB_TYPE_INTRA4x4 == uint32(t) || MB_TYPE_INTRA8x8 == uint32(t)
}
func IS_INTRA16x16[T Integer](t T) bool { return MB_TYPE_INTRA16x16 == uint32(t) }
func IS_INTRA[T Integer](t T) bool      { return uint32(t)&MB_TYPE_INTRA != 0 }
func IS_INTER[T Integer](t T) bool      { return uint32(t)&MB_TYPE_INTER != 0 }
func IS_INTER_16x16[T Integer](t T) bool {
	return uint32(t)&MB_TYPE_16x16 != 0
}
func IS_INTER_16x8[T Integer](t T) bool { return uint32(t)&MB_TYPE_16x8 != 0 }
func IS_INTER_8x16[T Integer](t T) bool { return uint32(t)&MB_TYPE_8x16 != 0 }
func IS_TYPE_L0[T Integer](t T) bool    { return uint32(t)&MB_TYPE_L0 != 0 }
func IS_TYPE_L1[T Integer](t T) bool    { return uint32(t)&MB_TYPE_L1 != 0 }

// IS_DIR is ((a) & (MB_TYPE_P0L0<<((part)+2*(list)))).
func IS_DIR[T, P, L Integer](a T, part P, list L) bool {
	return uint32(a)&(uint32(MB_TYPE_P0L0)<<uint32(int32(part)+2*int32(list))) != 0
}

func IS_SKIP[T Integer](t T) bool      { return uint32(t)&MB_TYPE_SKIP != 0 }
func IS_DIRECT[T Integer](t T) bool    { return uint32(t)&MB_TYPE_DIRECT != 0 }
func IS_SVC_INTER[T Integer](t T) bool { return IS_INTER(t) }
func IS_I_BL[T Integer](t T) bool      { return uint32(t) == MB_TYPE_INTRA_BL }
func IS_SVC_INTRA[T Integer](t T) bool { return IS_I_BL(t) || IS_INTRA(t) }
func IS_Inter_8x8[T Integer](t T) bool { return uint32(t)&MB_TYPE_8x8 != 0 }
func IS_SUB_8x8[T Integer](sub_type T) bool {
	return uint32(sub_type)&SUB_MB_TYPE_8x8 != 0
}
func IS_SUB_8x4[T Integer](sub_type T) bool {
	return uint32(sub_type)&SUB_MB_TYPE_8x4 != 0
}
func IS_SUB_4x8[T Integer](sub_type T) bool {
	return uint32(sub_type)&SUB_MB_TYPE_4x8 != 0
}
func IS_SUB_4x4[T Integer](sub_type T) bool {
	return uint32(sub_type)&SUB_MB_TYPE_4x4 != 0
}

const (
	REF_NOT_AVAIL   = -2
	REF_NOT_IN_LIST = -1 //intra
)

// intra16x16 Luma
const (
	I16_PRED_INVALID = -1
	I16_PRED_V       = 0
	I16_PRED_H       = 1
	I16_PRED_DC      = 2
	I16_PRED_P       = 3

	I16_PRED_DC_L   = 4
	I16_PRED_DC_T   = 5
	I16_PRED_DC_128 = 6
	I16_PRED_DC_A   = 7
)

// intra4x4 Luma (I8x8 also uses these definitions)
const (
	I4_PRED_INVALID = 0
	I4_PRED_V       = 0
	I4_PRED_H       = 1
	I4_PRED_DC      = 2
	I4_PRED_DDL     = 3 //diagonal_down_left
	I4_PRED_DDR     = 4 //diagonal_down_right
	I4_PRED_VR      = 5 //vertical_right
	I4_PRED_HD      = 6 //horizon_down
	I4_PRED_VL      = 7 //vertical_left
	I4_PRED_HU      = 8 //horizon_up

	I4_PRED_DC_L   = 9
	I4_PRED_DC_T   = 10
	I4_PRED_DC_128 = 11

	I4_PRED_DDL_TOP = 12 //right-top replacing by padding rightmost pixel of top
	I4_PRED_VL_TOP  = 13 //right-top replacing by padding rightmost pixel of top
	I4_PRED_A       = 14
)

// intra Chroma
const (
	C_PRED_INVALID = -1
	C_PRED_DC      = 0
	C_PRED_H       = 1
	C_PRED_V       = 2
	C_PRED_P       = 3

	C_PRED_DC_L   = 4
	C_PRED_DC_T   = 5
	C_PRED_DC_128 = 6
	C_PRED_A      = 7
)
