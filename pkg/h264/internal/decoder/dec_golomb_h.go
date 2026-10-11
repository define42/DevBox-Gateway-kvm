// Port of codec/decoder/core/inc/dec_golomb.h (exponential golomb bit-stream
// reading routines).
//
// common.SBitStringAux: PBuf is the whole buffer, PStartBuf / PEndBuf /
// PCurBuf are int offsets into it (C pointers pStartBuf / pEndBuf / pCurBuf).

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// WELS_READ_VERIFY(uiRet) contains a `return` and must be expanded inline:
//
//	if uiRetTmp := uint32(uiRet); uiRetTmp != ERR_NONE {
//		return int32(uiRetTmp) // or uiRetTmp, depending on the function's return type
//	}

// GET_WORD: reads the next (up to) 16 bits of the buffer into iCurBits.
// Returns ERR_INFO_READ_OVERFLOW where the C macro returns it from the
// enclosing function, ERR_NONE otherwise.
func GET_WORD(pBs *common.SBitStringAux, iAllowedBytes, iReadBytes int) int32 {
	if iReadBytes > iAllowedBytes+1 {
		return ERR_INFO_READ_OVERFLOW
	}
	var uiWord uint32
	if iReadBytes < iAllowedBytes {
		uiWord = uint32(pBs.PBuf[pBs.PCurBuf]) << 8
	}
	if (iReadBytes + 1) < iAllowedBytes {
		uiWord |= uint32(pBs.PBuf[pBs.PCurBuf+1])
	}
	pBs.UiCurBits |= uiWord << uint32(pBs.ILeftBits)
	pBs.ILeftBits -= 16
	pBs.PCurBuf += 2
	return ERR_NONE
}

// NEED_BITS: refills the cache when needed (see GET_WORD for the return value).
func NEED_BITS(pBs *common.SBitStringAux, iAllowedBytes, iReadBytes int) int32 {
	if pBs.ILeftBits > 0 {
		return GET_WORD(pBs, iAllowedBytes, iReadBytes)
	}
	return ERR_NONE
}

// UBITS is (iCurBits>>(32-(iNumBits))). Note: for iNumBits == 0 the C shift
// by 32 is undefined; Go yields 0.
func UBITS(iCurBits uint32, iNumBits int32) uint32 {
	return iCurBits >> uint32(32-iNumBits)
}

// DUMP_BITS: drops iNumBits bits from the cache (see GET_WORD for the return
// value).
func DUMP_BITS(pBs *common.SBitStringAux, iNumBits int32, iAllowedBytes, iReadBytes int) int32 {
	pBs.UiCurBits <<= uint32(iNumBits)
	pBs.ILeftBits += iNumBits
	return NEED_BITS(pBs, iAllowedBytes, iReadBytes)
}

func BsGetBits(pBs *common.SBitStringAux, iNumBits int32, pCode *uint32) int32 {
	iRc := UBITS(pBs.UiCurBits, iNumBits)
	iAllowedBytes := pBs.PEndBuf - pBs.PStartBuf //actual stream bytes
	iReadBytes := pBs.PCurBuf - pBs.PStartBuf
	if iRet := DUMP_BITS(pBs, iNumBits, iAllowedBytes, iReadBytes); iRet != ERR_NONE {
		return iRet
	}
	*pCode = iRc
	return ERR_NONE
}

/*
 *  Exponential Golomb codes decoding routines
 */

// g_kuiIntra4x4CbpTable, g_kuiIntra4x4CbpTable400, g_kuiInterCbpTable,
// g_kuiInterCbpTable400 and g_kuiLeadingZeroTable are defined in
// decoder_data_tables.go.

var g_kuiPrefix8BitsTable = [16]uint32{
	0, 0, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 3, 3, 3, 3,
}

func GetPrefixBits(uiValue uint32) uint32 {
	var iNumBit uint32

	if uiValue&0xffff0000 != 0 {
		uiValue >>= 16
		iNumBit += 16
	}
	if uiValue&0xff00 != 0 {
		uiValue >>= 8
		iNumBit += 8
	}

	if uiValue&0xf0 != 0 {
		uiValue >>= 4
		iNumBit += 4
	}
	iNumBit += g_kuiPrefix8BitsTable[uiValue]

	return 32 - iNumBit
}

/*
 *  Read one bit from bit stream followed
 */
func BsGetOneBit(pBs *common.SBitStringAux, pCode *uint32) uint32 {
	return uint32(BsGetBits(pBs, 1, pCode))
}

func GetLeadingZeroBits(iCurBits uint32) int32 { //<=32 bits
	var uiValue uint32

	uiValue = UBITS(iCurBits, 8) //ShowBits( bs, 8 );
	if uiValue != 0 {
		return int32(g_kuiLeadingZeroTable[uiValue])
	}

	uiValue = UBITS(iCurBits, 16) //ShowBits( bs, 16 );
	if uiValue != 0 {
		return int32(g_kuiLeadingZeroTable[uiValue]) + 8
	}

	uiValue = UBITS(iCurBits, 24) //ShowBits( bs, 24 );
	if uiValue != 0 {
		return int32(g_kuiLeadingZeroTable[uiValue]) + 16
	}

	uiValue = iCurBits //ShowBits( bs, 32 );
	if uiValue != 0 {
		return int32(g_kuiLeadingZeroTable[uiValue]) + 24
	}
	//ASSERT(false);  // should not go here
	return -1
}

func BsGetUe(pBs *common.SBitStringAux, pCode *uint32) uint32 {
	var iValue uint32
	iLeadingZeroBits := GetLeadingZeroBits(pBs.UiCurBits)
	var iAllowedBytes, iReadBytes int
	iAllowedBytes = pBs.PEndBuf - pBs.PStartBuf //actual stream bytes

	if iLeadingZeroBits == -1 { //bistream error
		return ERR_INFO_READ_LEADING_ZERO //-1
	} else if iLeadingZeroBits > 16 { //rarely into this condition (even may be bitstream error), prevent from 16-bit reading overflow
		//using two-step reading instead of one time reading of >16 bits.
		iReadBytes = pBs.PCurBuf - pBs.PStartBuf
		if iRet := DUMP_BITS(pBs, 16, iAllowedBytes, iReadBytes); iRet != ERR_NONE {
			return uint32(iRet)
		}
		iReadBytes = pBs.PCurBuf - pBs.PStartBuf
		if iRet := DUMP_BITS(pBs, iLeadingZeroBits+1-16, iAllowedBytes, iReadBytes); iRet != ERR_NONE {
			return uint32(iRet)
		}
	} else {
		iReadBytes = pBs.PCurBuf - pBs.PStartBuf
		if iRet := DUMP_BITS(pBs, iLeadingZeroBits+1, iAllowedBytes, iReadBytes); iRet != ERR_NONE {
			return uint32(iRet)
		}
	}
	if iLeadingZeroBits != 0 {
		iValue = UBITS(pBs.UiCurBits, iLeadingZeroBits)
		iReadBytes = pBs.PCurBuf - pBs.PStartBuf
		if iRet := DUMP_BITS(pBs, iLeadingZeroBits, iAllowedBytes, iReadBytes); iRet != ERR_NONE {
			return uint32(iRet)
		}
	}

	*pCode = (uint32(1) << uint32(iLeadingZeroBits)) - 1 + iValue
	return ERR_NONE
}

/*
 *  Read signed exp golomb codes
 */
func BsGetSe(pBs *common.SBitStringAux, pCode *int32) int32 {
	var uiCodeNum uint32

	if uiRetTmp := BsGetUe(pBs, &uiCodeNum); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}

	if uiCodeNum&0x01 != 0 {
		*pCode = int32((uiCodeNum + 1) >> 1)
	} else {
		*pCode = common.NEG_NUM(int32(uiCodeNum >> 1))
	}
	return ERR_NONE
}

/*
 * Get unsigned truncated exp golomb code.
 */
func BsGetTe0(pBs *common.SBitStringAux, iRange int32, pCode *uint32) int32 {
	if iRange == 1 {
		*pCode = 0
	} else if iRange == 2 {
		if uiRetTmp := BsGetOneBit(pBs, pCode); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		*pCode ^= 1
	} else {
		if uiRetTmp := BsGetUe(pBs, pCode); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
	}
	return ERR_NONE
}

/*
 *  Get number of trailing bits
 *  pBuf: sub-slice starting at the byte to inspect (C: *pBuf).
 */
func BsGetTrailingBits(pBuf []uint8) int32 {
	// TODO
	uiValue := uint32(pBuf[0])
	var iRetNum int32

	for {
		if uiValue&1 != 0 {
			return iRetNum
		}
		uiValue >>= 1
		iRetNum++
		if !(iRetNum < 9) {
			break
		}
	}

	return 0
}

/*
 *      Check whether there is more rbsp data for processing
 */
func CheckMoreRBSPData(pBsAux *common.SBitStringAux) bool {
	if (int(pBsAux.IBits) - ((pBsAux.PCurBuf - pBsAux.PStartBuf - 2) << 3) - int(pBsAux.ILeftBits)) > 1 {
		return true
	}
	return false
}

// The WELS_CHECK_SE_* macros contain a `return`; the Go versions report
// whether the check failed (after logging, for the non-NOLOG variants) and
// the caller returns ret_code itself:
//
//	if WELS_CHECK_SE_BOTH_ERROR(pCtx, iCode, -12, 12, "slice_alpha_c0_offset_div2") {
//		return ret_code
//	}
//
// The WARNING variants only log. The C macros log through pCtx->sLogCtx of
// the enclosing function, hence the pCtx parameter.

func WELS_CHECK_SE_BOTH_ERROR[T common.Integer](pCtx *SWelsDecoderContext, val, lower_bound, upper_bound T, syntax_name string) bool {
	if (val < lower_bound) || (val > upper_bound) {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "invalid syntax "+syntax_name+" %d", val)
		return true
	}
	return false
}

func WELS_CHECK_SE_LOWER_ERROR[T common.Integer](pCtx *SWelsDecoderContext, val, lower_bound T, syntax_name string) bool {
	if val < lower_bound {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "invalid syntax "+syntax_name+" %d", val)
		return true
	}
	return false
}

func WELS_CHECK_SE_UPPER_ERROR[T common.Integer](pCtx *SWelsDecoderContext, val, upper_bound T, syntax_name string) bool {
	if val > upper_bound {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "invalid syntax "+syntax_name+" %d", val)
		return true
	}
	return false
}

func WELS_CHECK_SE_BOTH_ERROR_NOLOG[T common.Integer](val, lower_bound, upper_bound T, syntax_name string) bool {
	return (val < lower_bound) || (val > upper_bound)
}

func WELS_CHECK_SE_LOWER_ERROR_NOLOG[T common.Integer](val, lower_bound T, syntax_name string) bool {
	return val < lower_bound
}

func WELS_CHECK_SE_UPPER_ERROR_NOLOG[T common.Integer](val, upper_bound T, syntax_name string) bool {
	return val > upper_bound
}

func WELS_CHECK_SE_BOTH_WARNING[T common.Integer](pCtx *SWelsDecoderContext, val, lower_bound, upper_bound T, syntax_name string) {
	if (val < lower_bound) || (val > upper_bound) {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "invalid syntax "+syntax_name+" %d", val)
	}
}

func WELS_CHECK_SE_LOWER_WARNING[T common.Integer](pCtx *SWelsDecoderContext, val, lower_bound T, syntax_name string) {
	if val < lower_bound {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "invalid syntax "+syntax_name+" %d", val)
	}
}

func WELS_CHECK_SE_UPPER_WARNING[T common.Integer](pCtx *SWelsDecoderContext, val, upper_bound T, syntax_name string) {
	if val > upper_bound {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "invalid syntax "+syntax_name+" %d", val)
	}
}

// below define syntax element offset
const (
	// for bit_depth_luma_minus8 and bit_depth_chroma_minus8
	BIT_DEPTH_LUMA_OFFSET   = 8
	BIT_DEPTH_CHROMA_OFFSET = 8
	// for log2_max_frame_num_minus4
	LOG2_MAX_FRAME_NUM_OFFSET = 4
	// for log2_max_pic_order_cnt_lsb_minus4
	LOG2_MAX_PIC_ORDER_CNT_LSB_OFFSET = 4
	// for pic_width_in_mbs_minus1
	PIC_WIDTH_IN_MBS_OFFSET = 1
	// for pic_height_in_map_units_minus1
	PIC_HEIGHT_IN_MAP_UNITS_OFFSET = 1
	// for bit_depth_aux_minus8
	BIT_DEPTH_AUX_OFFSET = 8
	// for num_slice_groups_minus1
	NUM_SLICE_GROUPS_OFFSET = 1
	// for run_length_minus1
	RUN_LENGTH_OFFSET = 1
	// for slice_group_change_rate_minus1
	SLICE_GROUP_CHANGE_RATE_OFFSET = 1
	// for pic_size_in_map_units_minus1
	PIC_SIZE_IN_MAP_UNITS_OFFSET = 1
	// for num_ref_idx_l0_default_active_minus1 and num_ref_idx_l1_default_active_minus1
	NUM_REF_IDX_L0_DEFAULT_ACTIVE_OFFSET = 1
	NUM_REF_IDX_L1_DEFAULT_ACTIVE_OFFSET = 1
	// for pic_init_qp_minus26 and pic_init_qs_minus26
	PIC_INIT_QP_OFFSET = 26
	PIC_INIT_QS_OFFSET = 26
	// for num_ref_idx_l0_active_minus1 and num_ref_idx_l1_active_minus1
	NUM_REF_IDX_L0_ACTIVE_OFFSET = 1
	NUM_REF_IDX_L1_ACTIVE_OFFSET = 1

	// From Level 5.2
	MAX_MB_SIZE = 36864
	// for aspect_ratio_idc
	EXTENDED_SAR = 255
)
