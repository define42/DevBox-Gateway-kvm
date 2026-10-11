// Port of codec/decoder/core/inc/wels_common_basis.h.
// (wels_common_defs.h - MB types, prediction modes, NAL types, ... - lives in
// package common.)

package decoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

// Common use tables g_kuiScan8, g_kuiLumaDcZigzagScan, g_kuiChromaDcScan,
// g_kMbNonZeroCountIdx, g_kCacheNzcScanIdx, g_kCache26ScanIdx,
// g_kCache30ScanIdx and g_kNonZeroScanIdxC are defined in
// decoder_data_tables.go.

/* Profile IDC */
type ProfileIdc = uint8

/* Position Offset structure */
type SPosOffset struct {
	iLeftOffset   int32
	iTopOffset    int32
	iRightOffset  int32
	iBottomOffset int32
}

/* MB Type & Sub-MB Type */
type MbType = uint32
type SubMbType = uint32

const (
	I16_LUMA_DC        = 1
	I16_LUMA_AC        = 2
	LUMA_DC_AC         = 3
	CHROMA_DC          = 4
	CHROMA_AC          = 5
	LUMA_DC_AC_8       = 6
	CHROMA_DC_U        = 7
	CHROMA_DC_V        = 8
	CHROMA_AC_U        = 9
	CHROMA_AC_V        = 10
	LUMA_DC_AC_INTRA   = 11
	LUMA_DC_AC_INTER   = 12
	CHROMA_DC_U_INTER  = 13
	CHROMA_DC_V_INTER  = 14
	CHROMA_AC_U_INTER  = 15
	CHROMA_AC_V_INTER  = 16
	LUMA_DC_AC_INTRA_8 = 17
	LUMA_DC_AC_INTER_8 = 18
)

// SHIFT_BUFFER / POP_BUFFER operate on SReadBitsCache, which is file-local
// to parse_mb_syn_cavlc.cpp; they are ported together with it in
// parse_mb_syn_cavlc.go.

var g_kuiZigzagScan = [16]uint8{ //4*4block residual zig-zag scan order
	0, 1, 4, 8,
	5, 2, 3, 6,
	9, 12, 13, 10,
	7, 11, 14, 15,
}

var g_kuiZigzagScan8x8 = [64]uint8{ //8x8 block residual zig-zag scan order
	0, 1, 8, 16, 9, 2, 3, 10,
	17, 24, 32, 25, 18, 11, 4, 5,
	12, 19, 26, 33, 40, 48, 41, 34,
	27, 20, 13, 6, 7, 14, 21, 28,
	35, 42, 49, 56, 57, 50, 43, 36,
	29, 22, 15, 23, 30, 37, 44, 51,
	58, 59, 52, 45, 38, 31, 39, 46,
	53, 60, 61, 54, 47, 55, 62, 63,
}

var g_kuiIdx2CtxSignificantCoeffFlag8x8 = [64]uint8{ // Table 9-43, Page 289
	0, 1, 2, 3, 4, 5, 5, 4,
	4, 3, 3, 4, 4, 4, 5, 5,
	4, 4, 4, 4, 3, 3, 6, 7,
	7, 7, 8, 9, 10, 9, 8, 7,
	7, 6, 11, 12, 13, 11, 6, 7,
	8, 9, 14, 10, 9, 8, 6, 11,
	12, 13, 11, 6, 9, 14, 10, 9,
	11, 12, 13, 11, 14, 10, 12, 14,
}

var g_kuiIdx2CtxLastSignificantCoeffFlag8x8 = [64]uint8{ // Table 9-43, Page 289
	0, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2,
	3, 3, 3, 3, 3, 3, 3, 3,
	4, 4, 4, 4, 4, 4, 4, 4,
	5, 5, 5, 5, 6, 6, 6, 6,
	7, 7, 7, 7, 8, 8, 8, 8,
}

func GetMbResProperty(pMBproperty *int32, pResidualProperty *int32, bCavlc bool) {
	switch *pResidualProperty {
	case CHROMA_AC_U:
		*pMBproperty = 1
		if bCavlc {
			*pResidualProperty = CHROMA_AC
		} else {
			*pResidualProperty = CHROMA_AC_U
		}
	case CHROMA_AC_V:
		*pMBproperty = 2
		if bCavlc {
			*pResidualProperty = CHROMA_AC
		} else {
			*pResidualProperty = CHROMA_AC_V
		}
	case LUMA_DC_AC_INTRA:
		*pMBproperty = 0
		*pResidualProperty = LUMA_DC_AC
	case CHROMA_DC_U:
		*pMBproperty = 1
		if bCavlc {
			*pResidualProperty = CHROMA_DC
		} else {
			*pResidualProperty = CHROMA_DC_U
		}
	case CHROMA_DC_V:
		*pMBproperty = 2
		if bCavlc {
			*pResidualProperty = CHROMA_DC
		} else {
			*pResidualProperty = CHROMA_DC_V
		}
	case I16_LUMA_AC:
		*pMBproperty = 0
	case I16_LUMA_DC:
		*pMBproperty = 0
	case LUMA_DC_AC_INTER:
		*pMBproperty = 3
		*pResidualProperty = LUMA_DC_AC
	case CHROMA_DC_U_INTER:
		*pMBproperty = 4
		if bCavlc {
			*pResidualProperty = CHROMA_DC
		} else {
			*pResidualProperty = CHROMA_DC_U
		}
	case CHROMA_DC_V_INTER:
		*pMBproperty = 5
		if bCavlc {
			*pResidualProperty = CHROMA_DC
		} else {
			*pResidualProperty = CHROMA_DC_V
		}
	case CHROMA_AC_U_INTER:
		*pMBproperty = 4
		if bCavlc {
			*pResidualProperty = CHROMA_AC
		} else {
			*pResidualProperty = CHROMA_AC_U
		}
	case CHROMA_AC_V_INTER:
		*pMBproperty = 5
		if bCavlc {
			*pResidualProperty = CHROMA_AC
		} else {
			*pResidualProperty = CHROMA_AC_V
		}
	// Reference to Table 7-2
	case LUMA_DC_AC_INTRA_8:
		*pMBproperty = 6
		*pResidualProperty = LUMA_DC_AC_8
	case LUMA_DC_AC_INTER_8:
		*pMBproperty = 7
		*pResidualProperty = LUMA_DC_AC_8
	}
}

type SI16PredInfo struct {
	iPredMode     int8
	iLeftAvail    int8
	iTopAvail     int8
	iLeftTopAvail int8
}

var g_ksI16PredInfo = [4]SI16PredInfo{
	{common.I16_PRED_V, 0, 1, 0},
	{common.I16_PRED_H, 1, 0, 0},
	{0, 0, 0, 0},
	{common.I16_PRED_P, 1, 1, 1},
}

var g_ksChromaPredInfo = [4]SI16PredInfo{
	{0, 0, 0, 0},
	{common.C_PRED_H, 1, 0, 0},
	{common.C_PRED_V, 0, 1, 0},
	{common.C_PRED_P, 1, 1, 1},
}

type SI4PredInfo struct {
	iPredMode     int8
	iLeftAvail    int8
	iTopAvail     int8
	iLeftTopAvail int8
	// int8_t right_top_avail; //when right_top unavailable but top avail, we can pad the right-top with the rightmost pixel of top
}

var g_ksI4PredInfo = [9]SI4PredInfo{
	{common.I4_PRED_V, 0, 1, 0},
	{common.I4_PRED_H, 1, 0, 0},
	{0, 0, 0, 0},
	{common.I4_PRED_DDL, 0, 1, 0},
	{common.I4_PRED_DDR, 1, 1, 1},
	{common.I4_PRED_VR, 1, 1, 1},
	{common.I4_PRED_HD, 1, 1, 1},
	{common.I4_PRED_VL, 0, 1, 0},
	{common.I4_PRED_HU, 1, 0, 0},
}

var g_kuiI16CbpTable = [6]uint8{0, 16, 32, 15, 31, 47}

type SPartMbInfo struct {
	iType      MbType
	iPartCount int8 //P_16*16, P_16*8, P_8*16, P_8*8 based on 8*8 block; P_8*4, P_4*8, P_4*4 based on 4*4 block
	iPartWidth int8 //based on 4*4 block
}

// Table 7.13. Macroblock type values 0 to 4 for P slices.
var g_ksInterPMbTypeInfo = [5]SPartMbInfo{
	{common.MB_TYPE_16x16, 1, 4},
	{common.MB_TYPE_16x8, 2, 4},
	{common.MB_TYPE_8x16, 2, 2},
	{common.MB_TYPE_8x8, 4, 4},
	{common.MB_TYPE_8x8_REF0, 4, 4}, //ref0--ref_idx not present in bit-stream and default as 0
}

// Table 7.14. Macroblock type values 0 to 22 for B slices.
var g_ksInterBMbTypeInfo = [...]SPartMbInfo{
	//            Part 0        Part 1
	{common.MB_TYPE_DIRECT, 1, 4},                                                                                       //B_Direct_16x16
	{common.MB_TYPE_16x16 | common.MB_TYPE_P0L0, 1, 4},                                                                  //B_L0_16x16
	{common.MB_TYPE_16x16 | common.MB_TYPE_P0L1, 1, 4},                                                                  //B_L1_16x16
	{common.MB_TYPE_16x16 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1, 1, 4},                                            //B_Bi_16x16
	{common.MB_TYPE_16x8 | common.MB_TYPE_P0L0 | common.MB_TYPE_P1L0, 2, 4},                                             //B_L0_L0_16x8
	{common.MB_TYPE_8x16 | common.MB_TYPE_P0L0 | common.MB_TYPE_P1L0, 2, 2},                                             //B_L0_L0_8x16
	{common.MB_TYPE_16x8 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L1, 2, 4},                                             //B_L1_L1_16x8
	{common.MB_TYPE_8x16 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L1, 2, 2},                                             //B_L1_L1_8x16
	{common.MB_TYPE_16x8 | common.MB_TYPE_P0L0 | common.MB_TYPE_P1L1, 2, 4},                                             //B_L0_L1_16x8
	{common.MB_TYPE_8x16 | common.MB_TYPE_P0L0 | common.MB_TYPE_P1L1, 2, 2},                                             //B_L0_L1_8x16
	{common.MB_TYPE_16x8 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L0, 2, 4},                                             //B_L1_L0_16x8
	{common.MB_TYPE_8x16 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L0, 2, 2},                                             //B_L1_L0_8x16
	{common.MB_TYPE_16x8 | common.MB_TYPE_P0L0 | common.MB_TYPE_P1L0 | common.MB_TYPE_P1L1, 2, 4},                       //B_L0_Bi_16x8
	{common.MB_TYPE_8x16 | common.MB_TYPE_P0L0 | common.MB_TYPE_P1L0 | common.MB_TYPE_P1L1, 2, 2},                       //B_L0_Bi_8x16
	{common.MB_TYPE_16x8 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L0 | common.MB_TYPE_P1L1, 2, 4},                       //B_L1_Bi_16x8
	{common.MB_TYPE_8x16 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L0 | common.MB_TYPE_P1L1, 2, 2},                       //B_L1_Bi_8x16
	{common.MB_TYPE_16x8 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L0, 2, 4},                       //B_Bi_L0_16x8
	{common.MB_TYPE_8x16 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L0, 2, 2},                       //B_Bi_L0_8x16
	{common.MB_TYPE_16x8 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L1, 2, 4},                       //B_Bi_L1_16x8
	{common.MB_TYPE_8x16 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L1, 2, 2},                       //B_Bi_L1_8x16
	{common.MB_TYPE_16x8 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L0 | common.MB_TYPE_P1L1, 2, 4}, //B_Bi_Bi_16x8
	{common.MB_TYPE_8x16 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L0 | common.MB_TYPE_P1L1, 2, 2}, //B_Bi_Bi_8x16
	{common.MB_TYPE_8x8 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L0 | common.MB_TYPE_P1L1, 4, 4},  //B_8x8
}

// Table 7.17 Sub-macroblock types in B macroblocks.
var g_ksInterPSubMbTypeInfo = [4]SPartMbInfo{
	{common.SUB_MB_TYPE_8x8, 1, 2},
	{common.SUB_MB_TYPE_8x4, 2, 2},
	{common.SUB_MB_TYPE_4x8, 2, 1},
	{common.SUB_MB_TYPE_4x4, 4, 1},
}

// Table 7.18 Sub-macroblock types in B macroblocks.
var g_ksInterBSubMbTypeInfo = [...]SPartMbInfo{
	{common.MB_TYPE_DIRECT, 1, 2},                                              //B_Direct_8x8
	{common.SUB_MB_TYPE_8x8 | common.MB_TYPE_P0L0, 1, 2},                       //B_L0_8x8
	{common.SUB_MB_TYPE_8x8 | common.MB_TYPE_P0L1, 1, 2},                       //B_L1_8x8
	{common.SUB_MB_TYPE_8x8 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1, 1, 2}, //B_Bi_8x8
	{common.SUB_MB_TYPE_8x4 | common.MB_TYPE_P0L0, 2, 2},                       //B_L0_8x4
	{common.SUB_MB_TYPE_4x8 | common.MB_TYPE_P0L0, 2, 1},                       //B_L0_4x8
	{common.SUB_MB_TYPE_8x4 | common.MB_TYPE_P0L1, 2, 2},                       //B_L1_8x4
	{common.SUB_MB_TYPE_4x8 | common.MB_TYPE_P0L1, 2, 1},                       //B_L1_4x8
	{common.SUB_MB_TYPE_8x4 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1, 2, 2}, //B_Bi_8x4
	{common.SUB_MB_TYPE_4x8 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1, 2, 1}, //B_Bi_4x8
	{common.SUB_MB_TYPE_4x4 | common.MB_TYPE_P0L0, 4, 1},                       //B_L0_4x4
	{common.SUB_MB_TYPE_4x4 | common.MB_TYPE_P0L1, 4, 1},                       //B_L1_4x4
	{common.SUB_MB_TYPE_4x4 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1, 4, 1}, //B_Bi_4x4
}

type sSar struct {
	uiWidth  uint32
	uiHeight uint32
}

var g_ksVuiSampleAspectRatio = [17]sSar{ //Table E-1
	{0, 0}, {1, 1}, {12, 11}, {10, 11}, {16, 11}, //0~4
	{40, 33}, {24, 11}, {20, 11}, {32, 11}, {80, 33}, //5~9
	{18, 11}, {15, 11}, {64, 33}, {160, 99}, {4, 3}, //10~14
	{3, 2}, {2, 1}, //15~16
}
