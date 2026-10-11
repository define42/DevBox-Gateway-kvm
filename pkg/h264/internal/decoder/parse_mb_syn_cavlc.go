// Port of codec/decoder/core/src/parse_mb_syn_cavlc.cpp: interfaces
// implementation for parsing the syntax of MB.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const MAX_LEVEL_PREFIX = 15

// SReadBitsCache (TagReadBitsCache). pBuf is a (slice, offset) pair: the
// whole bit-stream buffer plus the offset of the C pointer inside it.
type SReadBitsCache struct {
	uiCache32Bit uint32
	uiRemainBits uint8
	pBuf         []uint8
	iBufOff      int
}

// cavlcByteAt reads buf[i], returning 0 past the end of the buffer (the C
// code relies on the padding behind the bit-stream buffer).
func cavlcByteAt(buf []uint8, i int) uint32 {
	if i >= 0 && i < len(buf) {
		return uint32(buf[i])
	}
	return 0
}

// SHIFT_BUFFER(pBitsCache) (wels_common_basis.h):
//
//	{ pBitsCache->pBuf+=2; pBitsCache->uiRemainBits += 16;
//	  pBitsCache->uiCache32Bit |= (((pBitsCache->pBuf[2] << 8) | pBitsCache->pBuf[3]) << (32 - pBitsCache->uiRemainBits)); }
func SHIFT_BUFFER(pBitsCache *SReadBitsCache) {
	pBitsCache.iBufOff += 2
	pBitsCache.uiRemainBits += 16
	v := (cavlcByteAt(pBitsCache.pBuf, pBitsCache.iBufOff+2) << 8) | cavlcByteAt(pBitsCache.pBuf, pBitsCache.iBufOff+3)
	// the shift count is masked like x86 does (it is only out of range in
	// the undefined-behaviour cases of the C code).
	pBitsCache.uiCache32Bit |= v << (uint32(32-int32(pBitsCache.uiRemainBits)) & 31)
}

// POP_BUFFER(pBitsCache, iCount) (wels_common_basis.h):
//
//	{ pBitsCache->uiCache32Bit <<= iCount;  pBitsCache->uiRemainBits -= iCount; }
func POP_BUFFER[T common.Integer](pBitsCache *SReadBitsCache, iCount T) {
	pBitsCache.uiCache32Bit <<= uint32(iCount)
	pBitsCache.uiRemainBits -= uint8(iCount)
}

// void GetNeighborAvailMbType (PWelsNeighAvail pNeighAvail, PDqLayer pCurDqLayer)
func GetNeighborAvailMbType(pNeighAvail *SWelsNeighAvail, pCurDqLayer *SDqLayer) {
	var iCurSliceIdc, iTopSliceIdc, iLeftTopSliceIdc, iRightTopSliceIdc, iLeftSliceIdc int32
	var iCurXy, iTopXy, iLeftXy, iLeftTopXy, iRightTopXy int32
	var iCurX, iCurY int32

	iCurXy = pCurDqLayer.iMbXyIndex
	iCurX = pCurDqLayer.iMbX
	iCurY = pCurDqLayer.iMbY
	iCurSliceIdc = pCurDqLayer.pSliceIdc[iCurXy]
	if iCurX != 0 {
		iLeftXy = iCurXy - 1
		iLeftSliceIdc = pCurDqLayer.pSliceIdc[iLeftXy]
		pNeighAvail.iLeftAvail = b2i32(iLeftSliceIdc == iCurSliceIdc)
		if pNeighAvail.iLeftAvail != 0 {
			pNeighAvail.iLeftCbp = pCurDqLayer.pCbp[iLeftXy]
		} else {
			pNeighAvail.iLeftCbp = 0
		}
	} else {
		pNeighAvail.iLeftAvail = 0
		pNeighAvail.iLeftTopAvail = 0
		pNeighAvail.iLeftCbp = 0
	}

	if iCurY != 0 {
		iTopXy = iCurXy - pCurDqLayer.iMbWidth
		iTopSliceIdc = pCurDqLayer.pSliceIdc[iTopXy]
		pNeighAvail.iTopAvail = b2i32(iTopSliceIdc == iCurSliceIdc)
		if pNeighAvail.iTopAvail != 0 {
			pNeighAvail.iTopCbp = pCurDqLayer.pCbp[iTopXy]
		} else {
			pNeighAvail.iTopCbp = 0
		}
		if iCurX != 0 {
			iLeftTopXy = iTopXy - 1
			iLeftTopSliceIdc = pCurDqLayer.pSliceIdc[iLeftTopXy]
			pNeighAvail.iLeftTopAvail = b2i32(iLeftTopSliceIdc == iCurSliceIdc)
		} else {
			pNeighAvail.iLeftTopAvail = 0
		}
		if iCurX != (pCurDqLayer.iMbWidth - 1) {
			iRightTopXy = iTopXy + 1
			iRightTopSliceIdc = pCurDqLayer.pSliceIdc[iRightTopXy]
			pNeighAvail.iRightTopAvail = b2i32(iRightTopSliceIdc == iCurSliceIdc)
		} else {
			pNeighAvail.iRightTopAvail = 0
		}
	} else {
		pNeighAvail.iTopAvail = 0
		pNeighAvail.iLeftTopAvail = 0
		pNeighAvail.iRightTopAvail = 0
		pNeighAvail.iTopCbp = 0
	}

	pMbType := pCurDqLayer.pDec.pMbType
	pNeighAvail.iLeftType = 0
	if pNeighAvail.iLeftAvail != 0 {
		pNeighAvail.iLeftType = int32(pMbType[iLeftXy])
	}
	pNeighAvail.iTopType = 0
	if pNeighAvail.iTopAvail != 0 {
		pNeighAvail.iTopType = int32(pMbType[iTopXy])
	}
	pNeighAvail.iLeftTopType = 0
	if pNeighAvail.iLeftTopAvail != 0 {
		pNeighAvail.iLeftTopType = int32(pMbType[iLeftTopXy])
	}
	pNeighAvail.iRightTopType = 0
	if pNeighAvail.iRightTopAvail != 0 {
		pNeighAvail.iRightTopType = int32(pMbType[iRightTopXy])
	}
}

// void WelsFillCacheNonZeroCount (PWelsNeighAvail pNeighAvail, uint8_t* pNonZeroCount, PDqLayer
// pCurDqLayer)
//
// pNonZeroCount: the MB non-zero-count cache (sub-slice).
func WelsFillCacheNonZeroCount(pNeighAvail *SWelsNeighAvail, pNonZeroCount []uint8, pCurDqLayer *SDqLayer) { //no matter slice type, intra_pred_constrained_flag
	iCurXy := pCurDqLayer.iMbXyIndex
	var iTopXy int32
	var iLeftXy int32
	if pNeighAvail.iTopAvail != 0 {
		iTopXy = iCurXy - pCurDqLayer.iMbWidth
	}
	if pNeighAvail.iLeftAvail != 0 {
		iLeftXy = iCurXy - 1
	}

	//stuff non_zero_coeff_count from pNeighAvail(left and top)
	if pNeighAvail.iTopAvail != 0 {
		pTop := &pCurDqLayer.pNzc[iTopXy]
		pNonZeroCount[1] = uint8(pTop[12])
		pNonZeroCount[2] = uint8(pTop[13])
		pNonZeroCount[3] = uint8(pTop[14])
		pNonZeroCount[4] = uint8(pTop[15])
		pNonZeroCount[0], pNonZeroCount[5], pNonZeroCount[29] = 0, 0, 0
		pNonZeroCount[6] = uint8(pTop[20])
		pNonZeroCount[7] = uint8(pTop[21])
		pNonZeroCount[30] = uint8(pTop[22])
		pNonZeroCount[31] = uint8(pTop[23])
	} else {
		pNonZeroCount[1], pNonZeroCount[2], pNonZeroCount[3], pNonZeroCount[4] = 0xFF, 0xFF, 0xFF, 0xFF
		pNonZeroCount[0], pNonZeroCount[5], pNonZeroCount[29] = 0xFF, 0xFF, 0xFF
		pNonZeroCount[6], pNonZeroCount[7] = 0xFF, 0xFF
		pNonZeroCount[30], pNonZeroCount[31] = 0xFF, 0xFF
	}

	if pNeighAvail.iLeftAvail != 0 {
		pLeft := &pCurDqLayer.pNzc[iLeftXy]
		pNonZeroCount[8*1] = uint8(pLeft[3])
		pNonZeroCount[8*2] = uint8(pLeft[7])
		pNonZeroCount[8*3] = uint8(pLeft[11])
		pNonZeroCount[8*4] = uint8(pLeft[15])

		pNonZeroCount[5+8*1] = uint8(pLeft[17])
		pNonZeroCount[5+8*2] = uint8(pLeft[21])
		pNonZeroCount[5+8*4] = uint8(pLeft[19])
		pNonZeroCount[5+8*5] = uint8(pLeft[23])
	} else {
		pNonZeroCount[8*1] = 0xFF //unavailable
		pNonZeroCount[8*2] = 0xFF
		pNonZeroCount[8*3] = 0xFF
		pNonZeroCount[8*4] = 0xFF

		pNonZeroCount[5+8*1] = 0xFF //unavailable
		pNonZeroCount[5+8*2] = 0xFF

		pNonZeroCount[5+8*4] = 0xFF //unavailable
		pNonZeroCount[5+8*5] = 0xFF
	}
}

// void WelsFillCacheConstrain1IntraNxN (PWelsNeighAvail pNeighAvail, uint8_t* pNonZeroCount, int8_t*
// pIntraPredMode, PDqLayer pCurDqLayer)
func WelsFillCacheConstrain1IntraNxN(pNeighAvail *SWelsNeighAvail, pNonZeroCount []uint8, pIntraPredMode []int8, pCurDqLayer *SDqLayer) { //no matter slice type
	iCurXy := pCurDqLayer.iMbXyIndex
	var iTopXy int32
	var iLeftXy int32

	//stuff non_zero_coeff_count from pNeighAvail(left and top)
	WelsFillCacheNonZeroCount(pNeighAvail, pNonZeroCount, pCurDqLayer)

	if pNeighAvail.iTopAvail != 0 {
		iTopXy = iCurXy - pCurDqLayer.iMbWidth
	}
	if pNeighAvail.iLeftAvail != 0 {
		iLeftXy = iCurXy - 1
	}

	//intraNxN_pred_mode
	if pNeighAvail.iTopAvail != 0 && common.IS_INTRANxN(pNeighAvail.iTopType) { //top
		copy(pIntraPredMode[1:5], pCurDqLayer.pIntraPredMode[iTopXy][0:4])
	} else {
		var iPred int8
		if common.IS_INTRA16x16(pNeighAvail.iTopType) || (common.MB_TYPE_INTRA_PCM == pNeighAvail.iTopType) {
			iPred = 0x02 // 0x02020202
		} else {
			iPred = -1 // 0xffffffff
		}
		pIntraPredMode[1], pIntraPredMode[2], pIntraPredMode[3], pIntraPredMode[4] = iPred, iPred, iPred, iPred
	}

	if pNeighAvail.iLeftAvail != 0 && common.IS_INTRANxN(pNeighAvail.iLeftType) { //left
		pIntraPredMode[0+8] = pCurDqLayer.pIntraPredMode[iLeftXy][4]
		pIntraPredMode[0+8*2] = pCurDqLayer.pIntraPredMode[iLeftXy][5]
		pIntraPredMode[0+8*3] = pCurDqLayer.pIntraPredMode[iLeftXy][6]
		pIntraPredMode[0+8*4] = pCurDqLayer.pIntraPredMode[iLeftXy][3]
	} else {
		var iPred int8
		if common.IS_INTRA16x16(pNeighAvail.iLeftType) || (common.MB_TYPE_INTRA_PCM == pNeighAvail.iLeftType) {
			iPred = 2
		} else {
			iPred = -1
		}
		pIntraPredMode[0+8] = iPred
		pIntraPredMode[0+8*2] = iPred
		pIntraPredMode[0+8*3] = iPred
		pIntraPredMode[0+8*4] = iPred
	}
}

// void WelsFillCacheConstrain0IntraNxN (PWelsNeighAvail pNeighAvail, uint8_t* pNonZeroCount, int8_t*
// pIntraPredMode, PDqLayer pCurDqLayer)
func WelsFillCacheConstrain0IntraNxN(pNeighAvail *SWelsNeighAvail, pNonZeroCount []uint8, pIntraPredMode []int8, pCurDqLayer *SDqLayer) { //no matter slice type
	iCurXy := pCurDqLayer.iMbXyIndex
	var iTopXy int32
	var iLeftXy int32

	//stuff non_zero_coeff_count from pNeighAvail(left and top)
	WelsFillCacheNonZeroCount(pNeighAvail, pNonZeroCount, pCurDqLayer)

	if pNeighAvail.iTopAvail != 0 {
		iTopXy = iCurXy - pCurDqLayer.iMbWidth
	}
	if pNeighAvail.iLeftAvail != 0 {
		iLeftXy = iCurXy - 1
	}

	//intra4x4_pred_mode
	if pNeighAvail.iTopAvail != 0 && common.IS_INTRANxN(pNeighAvail.iTopType) { //top
		copy(pIntraPredMode[1:5], pCurDqLayer.pIntraPredMode[iTopXy][0:4])
	} else {
		var iPred int8
		if pNeighAvail.iTopAvail != 0 {
			iPred = 0x02 // 0x02020202
		} else {
			iPred = -1 // 0xffffffff
		}
		pIntraPredMode[1], pIntraPredMode[2], pIntraPredMode[3], pIntraPredMode[4] = iPred, iPred, iPred, iPred
	}

	if pNeighAvail.iLeftAvail != 0 && common.IS_INTRANxN(pNeighAvail.iLeftType) { //left
		pIntraPredMode[0+8*1] = pCurDqLayer.pIntraPredMode[iLeftXy][4]
		pIntraPredMode[0+8*2] = pCurDqLayer.pIntraPredMode[iLeftXy][5]
		pIntraPredMode[0+8*3] = pCurDqLayer.pIntraPredMode[iLeftXy][6]
		pIntraPredMode[0+8*4] = pCurDqLayer.pIntraPredMode[iLeftXy][3]
	} else {
		var iPred int8
		if pNeighAvail.iLeftAvail != 0 {
			iPred = 2
		} else {
			iPred = -1
		}
		pIntraPredMode[0+8*1] = iPred
		pIntraPredMode[0+8*2] = iPred
		pIntraPredMode[0+8*3] = iPred
		pIntraPredMode[0+8*4] = iPred
	}
}

// void WelsFillCacheInterCabac (PWelsNeighAvail pNeighAvail, uint8_t* pNonZeroCount, int16_t
// iMvArray[LIST_A][30][MV_A], int16_t iMvdCache[LIST_A][30][MV_A], int8_t iRefIdxArray[LIST_A][30],
// PDqLayer pCurDqLayer)
func WelsFillCacheInterCabac(pNeighAvail *SWelsNeighAvail, pNonZeroCount []uint8, iMvArray *[common.LIST_A][30][common.MV_A]int16, iMvdCache *[common.LIST_A][30][common.MV_A]int16, iRefIdxArray *[common.LIST_A][30]int8, pCurDqLayer *SDqLayer) {
	iCurXy := pCurDqLayer.iMbXyIndex
	var iTopXy int32
	var iLeftXy int32
	var iLeftTopXy int32
	var iRightTopXy int32

	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	listCount := 1
	if pSliceHeader.eSliceType == common.B_SLICE {
		listCount = 2
	}
	//stuff non_zero_coeff_count from pNeighAvail(left and top)
	WelsFillCacheNonZeroCount(pNeighAvail, pNonZeroCount, pCurDqLayer)

	if pNeighAvail.iTopAvail != 0 {
		iTopXy = iCurXy - pCurDqLayer.iMbWidth
	}
	if pNeighAvail.iLeftAvail != 0 {
		iLeftXy = iCurXy - 1
	}
	if pNeighAvail.iLeftTopAvail != 0 {
		iLeftTopXy = iCurXy - 1 - pCurDqLayer.iMbWidth
	}
	if pNeighAvail.iRightTopAvail != 0 {
		iRightTopXy = iCurXy + 1 - pCurDqLayer.iMbWidth
	}

	pDec := pCurDqLayer.pDec
	for listIdx := 0; listIdx < listCount; listIdx++ {
		mv := &iMvArray[listIdx]
		mvd := &iMvdCache[listIdx]
		ref := &iRefIdxArray[listIdx]
		//stuff mv_cache and iRefIdxArray from left and top (inter)
		if pNeighAvail.iLeftAvail != 0 && common.IS_INTER(pNeighAvail.iLeftType) {
			mv[6] = pDec.pMv[listIdx][iLeftXy][3]
			mv[12] = pDec.pMv[listIdx][iLeftXy][7]
			mv[18] = pDec.pMv[listIdx][iLeftXy][11]
			mv[24] = pDec.pMv[listIdx][iLeftXy][15]

			mvd[6] = pCurDqLayer.pMvd[listIdx][iLeftXy][3]
			mvd[12] = pCurDqLayer.pMvd[listIdx][iLeftXy][7]
			mvd[18] = pCurDqLayer.pMvd[listIdx][iLeftXy][11]
			mvd[24] = pCurDqLayer.pMvd[listIdx][iLeftXy][15]

			ref[6] = pDec.pRefIndex[listIdx][iLeftXy][3]
			ref[12] = pDec.pRefIndex[listIdx][iLeftXy][7]
			ref[18] = pDec.pRefIndex[listIdx][iLeftXy][11]
			ref[24] = pDec.pRefIndex[listIdx][iLeftXy][15]
		} else {
			mv[6] = [2]int16{}
			mv[12] = [2]int16{}
			mv[18] = [2]int16{}
			mv[24] = [2]int16{}

			mvd[6] = [2]int16{}
			mvd[12] = [2]int16{}
			mvd[18] = [2]int16{}
			mvd[24] = [2]int16{}

			if 0 == pNeighAvail.iLeftAvail { //not available
				ref[6], ref[12], ref[18], ref[24] = REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL
			} else { //available but is intra mb type
				ref[6], ref[12], ref[18], ref[24] = REF_NOT_IN_LIST, REF_NOT_IN_LIST, REF_NOT_IN_LIST, REF_NOT_IN_LIST
			}
		}
		if pNeighAvail.iLeftTopAvail != 0 && common.IS_INTER(pNeighAvail.iLeftTopType) {
			mv[0] = pDec.pMv[listIdx][iLeftTopXy][15]
			mvd[0] = pCurDqLayer.pMvd[listIdx][iLeftTopXy][15]
			ref[0] = pDec.pRefIndex[listIdx][iLeftTopXy][15]
		} else {
			mv[0] = [2]int16{}
			mvd[0] = [2]int16{}
			if 0 == pNeighAvail.iLeftTopAvail { //not available
				ref[0] = REF_NOT_AVAIL
			} else { //available but is intra mb type
				ref[0] = REF_NOT_IN_LIST
			}
		}

		if pNeighAvail.iTopAvail != 0 && common.IS_INTER(pNeighAvail.iTopType) {
			copy(mv[1:5], pDec.pMv[listIdx][iTopXy][12:16])
			copy(mvd[1:5], pCurDqLayer.pMvd[listIdx][iTopXy][12:16])
			copy(ref[1:5], pDec.pRefIndex[listIdx][iTopXy][12:16])
		} else {
			mv[1], mv[2], mv[3], mv[4] = [2]int16{}, [2]int16{}, [2]int16{}, [2]int16{}
			mvd[1], mvd[2], mvd[3], mvd[4] = [2]int16{}, [2]int16{}, [2]int16{}, [2]int16{}
			if 0 == pNeighAvail.iTopAvail { //not available
				ref[1], ref[2], ref[3], ref[4] = REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL
			} else { //available but is intra mb type
				ref[1], ref[2], ref[3], ref[4] = REF_NOT_IN_LIST, REF_NOT_IN_LIST, REF_NOT_IN_LIST, REF_NOT_IN_LIST
			}
		}

		if pNeighAvail.iRightTopAvail != 0 && common.IS_INTER(pNeighAvail.iRightTopType) {
			mv[5] = pDec.pMv[listIdx][iRightTopXy][12]
			mvd[5] = pCurDqLayer.pMvd[listIdx][iRightTopXy][12]
			ref[5] = pDec.pRefIndex[listIdx][iRightTopXy][12]
		} else {
			mv[5] = [2]int16{}
			if 0 == pNeighAvail.iRightTopAvail { //not available
				ref[5] = REF_NOT_AVAIL
			} else { //available but is intra mb type
				ref[5] = REF_NOT_IN_LIST
			}
		}

		//right-top 4*4 block unavailable
		mv[9] = [2]int16{}
		mv[21] = [2]int16{}
		mv[11] = [2]int16{}
		mv[17] = [2]int16{}
		mv[23] = [2]int16{}
		mvd[9] = [2]int16{}
		mvd[21] = [2]int16{}
		mvd[11] = [2]int16{}
		mvd[17] = [2]int16{}
		mvd[23] = [2]int16{}
		ref[9], ref[21], ref[11], ref[17], ref[23] = REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL
	}
}

// void WelsFillDirectCacheCabac (PWelsNeighAvail pNeighAvail, int8_t iDirect[30], PDqLayer
// pCurDqLayer)
func WelsFillDirectCacheCabac(pNeighAvail *SWelsNeighAvail, iDirect *[30]int8, pCurDqLayer *SDqLayer) {

	iCurXy := pCurDqLayer.iMbXyIndex
	var iTopXy int32
	var iLeftXy int32
	var iLeftTopXy int32
	var iRightTopXy int32

	if pNeighAvail.iTopAvail != 0 {
		iTopXy = iCurXy - pCurDqLayer.iMbWidth
	}
	if pNeighAvail.iLeftAvail != 0 {
		iLeftXy = iCurXy - 1
	}
	if pNeighAvail.iLeftTopAvail != 0 {
		iLeftTopXy = iCurXy - 1 - pCurDqLayer.iMbWidth
	}
	if pNeighAvail.iRightTopAvail != 0 {
		iRightTopXy = iCurXy + 1 - pCurDqLayer.iMbWidth
	}
	*iDirect = [30]int8{}
	if pNeighAvail.iLeftAvail != 0 && common.IS_INTER(pNeighAvail.iLeftType) {
		iDirect[6] = pCurDqLayer.pDirect[iLeftXy][3]
		iDirect[12] = pCurDqLayer.pDirect[iLeftXy][7]
		iDirect[18] = pCurDqLayer.pDirect[iLeftXy][11]
		iDirect[24] = pCurDqLayer.pDirect[iLeftXy][15]
	}
	if pNeighAvail.iLeftTopAvail != 0 && common.IS_INTER(pNeighAvail.iLeftTopType) {
		iDirect[0] = pCurDqLayer.pDirect[iLeftTopXy][15]
	}

	if pNeighAvail.iTopAvail != 0 && common.IS_INTER(pNeighAvail.iTopType) {
		copy(iDirect[1:5], pCurDqLayer.pDirect[iTopXy][12:16])
	}

	if pNeighAvail.iRightTopAvail != 0 && common.IS_INTER(pNeighAvail.iRightTopType) {
		iDirect[5] = pCurDqLayer.pDirect[iRightTopXy][12]
	}
	//right-top 4*4 block unavailable
}

// void WelsFillCacheInter (PWelsNeighAvail pNeighAvail, uint8_t* pNonZeroCount, int16_t
// iMvArray[LIST_A][30][MV_A], int8_t iRefIdxArray[LIST_A][30], PDqLayer pCurDqLayer)
func WelsFillCacheInter(pNeighAvail *SWelsNeighAvail, pNonZeroCount []uint8, iMvArray *[common.LIST_A][30][common.MV_A]int16, iRefIdxArray *[common.LIST_A][30]int8, pCurDqLayer *SDqLayer) {
	iCurXy := pCurDqLayer.iMbXyIndex
	var iTopXy int32
	var iLeftXy int32
	var iLeftTopXy int32
	var iRightTopXy int32

	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	listCount := 1
	if pSliceHeader.eSliceType == common.B_SLICE {
		listCount = 2
	}

	//stuff non_zero_coeff_count from pNeighAvail(left and top)
	WelsFillCacheNonZeroCount(pNeighAvail, pNonZeroCount, pCurDqLayer)

	if pNeighAvail.iTopAvail != 0 {
		iTopXy = iCurXy - pCurDqLayer.iMbWidth
	}
	if pNeighAvail.iLeftAvail != 0 {
		iLeftXy = iCurXy - 1
	}
	if pNeighAvail.iLeftTopAvail != 0 {
		iLeftTopXy = iCurXy - 1 - pCurDqLayer.iMbWidth
	}
	if pNeighAvail.iRightTopAvail != 0 {
		iRightTopXy = iCurXy + 1 - pCurDqLayer.iMbWidth
	}

	pDec := pCurDqLayer.pDec
	for listIdx := 0; listIdx < listCount; listIdx++ {
		mv := &iMvArray[listIdx]
		ref := &iRefIdxArray[listIdx]
		//stuff mv_cache and iRefIdxArray from left and top (inter)
		if pNeighAvail.iLeftAvail != 0 && common.IS_INTER(pNeighAvail.iLeftType) {
			mv[6] = pDec.pMv[listIdx][iLeftXy][3]
			mv[12] = pDec.pMv[listIdx][iLeftXy][7]
			mv[18] = pDec.pMv[listIdx][iLeftXy][11]
			mv[24] = pDec.pMv[listIdx][iLeftXy][15]
			ref[6] = pDec.pRefIndex[listIdx][iLeftXy][3]
			ref[12] = pDec.pRefIndex[listIdx][iLeftXy][7]
			ref[18] = pDec.pRefIndex[listIdx][iLeftXy][11]
			ref[24] = pDec.pRefIndex[listIdx][iLeftXy][15]
		} else {
			mv[6] = [2]int16{}
			mv[12] = [2]int16{}
			mv[18] = [2]int16{}
			mv[24] = [2]int16{}

			if 0 == pNeighAvail.iLeftAvail { //not available
				ref[6], ref[12], ref[18], ref[24] = REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL
			} else { //available but is intra mb type
				ref[6], ref[12], ref[18], ref[24] = REF_NOT_IN_LIST, REF_NOT_IN_LIST, REF_NOT_IN_LIST, REF_NOT_IN_LIST
			}
		}
		if pNeighAvail.iLeftTopAvail != 0 && common.IS_INTER(pNeighAvail.iLeftTopType) {
			mv[0] = pDec.pMv[listIdx][iLeftTopXy][15]
			ref[0] = pDec.pRefIndex[listIdx][iLeftTopXy][15]
		} else {
			mv[0] = [2]int16{}
			if 0 == pNeighAvail.iLeftTopAvail { //not available
				ref[0] = REF_NOT_AVAIL
			} else { //available but is intra mb type
				ref[0] = REF_NOT_IN_LIST
			}
		}
		if pNeighAvail.iTopAvail != 0 && common.IS_INTER(pNeighAvail.iTopType) {
			copy(mv[1:5], pDec.pMv[listIdx][iTopXy][12:16])
			copy(ref[1:5], pDec.pRefIndex[listIdx][iTopXy][12:16])
		} else {
			mv[1], mv[2], mv[3], mv[4] = [2]int16{}, [2]int16{}, [2]int16{}, [2]int16{}
			if 0 == pNeighAvail.iTopAvail { //not available
				ref[1], ref[2], ref[3], ref[4] = REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL
			} else { //available but is intra mb type
				ref[1], ref[2], ref[3], ref[4] = REF_NOT_IN_LIST, REF_NOT_IN_LIST, REF_NOT_IN_LIST, REF_NOT_IN_LIST
			}
		}
		if pNeighAvail.iRightTopAvail != 0 && common.IS_INTER(pNeighAvail.iRightTopType) {
			mv[5] = pDec.pMv[listIdx][iRightTopXy][12]
			ref[5] = pDec.pRefIndex[listIdx][iRightTopXy][12]
		} else {
			mv[5] = [2]int16{}
			if 0 == pNeighAvail.iRightTopAvail { //not available
				ref[5] = REF_NOT_AVAIL
			} else { //available but is intra mb type
				ref[5] = REF_NOT_IN_LIST
			}
		}
		//right-top 4*4 block unavailable
		mv[9] = [2]int16{}
		mv[21] = [2]int16{}
		mv[11] = [2]int16{}
		mv[17] = [2]int16{}
		mv[23] = [2]int16{}
		ref[9], ref[21], ref[11], ref[17], ref[23] = REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL, REF_NOT_AVAIL
	}
}

// int32_t PredIntra4x4Mode (int8_t* pIntraPredMode, int32_t iIdx4)
func PredIntra4x4Mode(pIntraPredMode []int8, iIdx4 int32) int32 {
	iTopMode := pIntraPredMode[int(g_kuiScan8[iIdx4])-8]
	iLeftMode := pIntraPredMode[int(g_kuiScan8[iIdx4])-1]
	var iBestMode int8

	if -1 == iLeftMode || -1 == iTopMode {
		iBestMode = 2
	} else {
		iBestMode = common.WELS_MIN(iLeftMode, iTopMode)
	}
	return int32(iBestMode)
}

// CHECK_I16_MODE / CHECK_CHROMA_MODE / CHECK_I4_MODE (file-local macros).
func checkPredMode(info *SI16PredInfo, a int8, b, c, d int32) bool {
	return (a == info.iPredMode) &&
		(b >= int32(info.iLeftAvail)) &&
		(c >= int32(info.iTopAvail)) &&
		(d >= int32(info.iLeftTopAvail))
}

func CHECK_I16_MODE(a int8, b, c, d int32) bool {
	return checkPredMode(&g_ksI16PredInfo[a], a, b, c, d)
}

func CHECK_CHROMA_MODE(a int8, b, c, d int32) bool {
	return checkPredMode(&g_ksChromaPredInfo[a], a, b, c, d)
}

func CHECK_I4_MODE(a int8, b, c, d int32) bool {
	info := &g_ksI4PredInfo[a]
	return (a == info.iPredMode) &&
		(b >= int32(info.iLeftAvail)) &&
		(c >= int32(info.iTopAvail)) &&
		(d >= int32(info.iLeftTopAvail))
}

// int32_t CheckIntra16x16PredMode (uint8_t uiSampleAvail, int8_t* pMode)
func CheckIntra16x16PredMode(uiSampleAvail uint8, pMode *int8) int32 {
	iLeftAvail := int32(uiSampleAvail & 0x04)
	bLeftTopAvail := int32(uiSampleAvail & 0x02)
	iTopAvail := int32(uiSampleAvail & 0x01)

	if (*pMode < 0) || (*pMode > MAX_PRED_MODE_ID_I16x16) {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_I16x16_PRED_MODE)
	}

	if common.I16_PRED_DC == *pMode {
		if iLeftAvail != 0 && iTopAvail != 0 {
			return ERR_NONE
		} else if iLeftAvail != 0 {
			*pMode = common.I16_PRED_DC_L
		} else if iTopAvail != 0 {
			*pMode = common.I16_PRED_DC_T
		} else {
			*pMode = common.I16_PRED_DC_128
		}
	} else {
		bModeAvail := CHECK_I16_MODE(*pMode, iLeftAvail, iTopAvail, bLeftTopAvail)
		if !bModeAvail {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_I16x16_PRED_MODE)
		}
	}
	return ERR_NONE
}

// int32_t CheckIntraChromaPredMode (uint8_t uiSampleAvail, int8_t* pMode)
func CheckIntraChromaPredMode(uiSampleAvail uint8, pMode *int8) int32 {
	iLeftAvail := int32(uiSampleAvail & 0x04)
	bLeftTopAvail := int32(uiSampleAvail & 0x02)
	iTopAvail := int32(uiSampleAvail & 0x01)

	if common.C_PRED_DC == *pMode {
		if iLeftAvail != 0 && iTopAvail != 0 {
			return ERR_NONE
		} else if iLeftAvail != 0 {
			*pMode = common.C_PRED_DC_L
		} else if iTopAvail != 0 {
			*pMode = common.C_PRED_DC_T
		} else {
			*pMode = common.C_PRED_DC_128
		}
	} else {
		// The C code indexes g_ksChromaPredInfo[*pMode] without a range check
		// (callers validate the mode first); an out-of-range mode would be
		// an out-of-bounds read there, here it is reported as invalid.
		if *pMode < 0 || int(*pMode) >= len(g_ksChromaPredInfo) {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_I_CHROMA_PRED_MODE)
		}
		bModeAvail := CHECK_CHROMA_MODE(*pMode, iLeftAvail, iTopAvail, bLeftTopAvail)
		if !bModeAvail {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_I_CHROMA_PRED_MODE)
		}
	}
	return ERR_NONE
}

// int32_t CheckIntraNxNPredMode (int32_t* pSampleAvail, int8_t* pMode, int32_t iIndex, bool b8x8)
func CheckIntraNxNPredMode(pSampleAvail []int32, pMode *int8, iIndex int32, b8x8 bool) int32 {
	iIdx := int(int8(common.G_kuiCache30ScanIdx[iIndex]))

	iLeftAvail := pSampleAvail[iIdx-1]
	iTopAvail := pSampleAvail[iIdx-6]
	bLeftTopAvail := pSampleAvail[iIdx-7]
	var bRightTopAvail int32
	if b8x8 {
		bRightTopAvail = pSampleAvail[iIdx-4] // Diff with 4x4 Pred
	} else {
		bRightTopAvail = pSampleAvail[iIdx-5]
	}

	var iFinalMode int8

	if (*pMode < 0) || (*pMode > MAX_PRED_MODE_ID_I4x4) {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INVALID_INTRA4X4_MODE)
	}

	if common.I4_PRED_DC == *pMode {
		if iLeftAvail != 0 && iTopAvail != 0 {
			return int32(*pMode)
		} else if iLeftAvail != 0 {
			iFinalMode = common.I4_PRED_DC_L
		} else if iTopAvail != 0 {
			iFinalMode = common.I4_PRED_DC_T
		} else {
			iFinalMode = common.I4_PRED_DC_128
		}
	} else {
		bModeAvail := CHECK_I4_MODE(*pMode, iLeftAvail, iTopAvail, bLeftTopAvail)
		if !bModeAvail {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INVALID_INTRA4X4_MODE)
		}

		iFinalMode = *pMode

		//if right-top unavailable, modify mode DDL and VL (padding rightmost pixel of top)
		if common.I4_PRED_DDL == iFinalMode && 0 == bRightTopAvail {
			iFinalMode = common.I4_PRED_DDL_TOP
		} else if common.I4_PRED_VL == iFinalMode && 0 == bRightTopAvail {
			iFinalMode = common.I4_PRED_VL_TOP
		}
	}
	return int32(iFinalMode)
}

// void BsStartCavlc (PBitStringAux pBs)
func BsStartCavlc(pBs *common.SBitStringAux) {
	pBs.IIndex = ((pBs.PCurBuf - pBs.PStartBuf) << 3) - (16 - int(pBs.ILeftBits))
}

// void BsEndCavlc (PBitStringAux pBs)
func BsEndCavlc(pBs *common.SBitStringAux) {
	pBs.PCurBuf = pBs.PStartBuf + (pBs.IIndex >> 3)
	p := pBs.PCurBuf
	uiCache32Bit := (((cavlcByteAt(pBs.PBuf, p) << 8) | cavlcByteAt(pBs.PBuf, p+1)) << 16) |
		(cavlcByteAt(pBs.PBuf, p+2) << 8) | cavlcByteAt(pBs.PBuf, p+3)
	pBs.UiCurBits = uiCache32Bit << uint32(pBs.IIndex&0x07)
	pBs.PCurBuf += 4
	pBs.ILeftBits = -16 + int32(pBs.IIndex&0x07)
}

var kpVlcTableMoreBitsCountList = [3][]uint8{g_kuiVlcTableMoreBitsCount0[:], g_kuiVlcTableMoreBitsCount1[:], g_kuiVlcTableMoreBitsCount2[:]}

// return: used bits
func CavlcGetTrailingOnesAndTotalCoeff(uiTotalCoeff *uint8, uiTrailingOnes *uint8,
	pBitsCache *SReadBitsCache, pVlcTable *SVlcTable, bChromaDc bool, nC int8) int32 {
	var iUsedBits int32
	var iIndexVlc, iIndexValue, iNcMapIdx int32
	var uiCount uint32
	var uiValue uint32

	if bChromaDc {
		uiValue = pBitsCache.uiCache32Bit >> 24
		iIndexVlc = int32(pVlcTable.kpChromaCoeffTokenVlcTable[uiValue][0])
		uiCount = uint32(pVlcTable.kpChromaCoeffTokenVlcTable[uiValue][1])
		POP_BUFFER(pBitsCache, uiCount)
		iUsedBits += int32(uiCount)
		*uiTrailingOnes = g_kuiVlcTrailingOneTotalCoeffTable[iIndexVlc][0]
		*uiTotalCoeff = g_kuiVlcTrailingOneTotalCoeffTable[iIndexVlc][1]
	} else { //luma
		iNcMapIdx = int32(g_kuiNcMapTable[nC])
		if iNcMapIdx <= 2 {
			uiValue = pBitsCache.uiCache32Bit >> 24
			if uiValue < uint32(g_kuiVlcTableNeedMoreBitsThread[iNcMapIdx]) {
				POP_BUFFER(pBitsCache, 8)
				iUsedBits += 8
				iIndexValue = int32(pBitsCache.uiCache32Bit >> (32 - uint32(kpVlcTableMoreBitsCountList[iNcMapIdx][uiValue])))
				iIndexVlc = int32(pVlcTable.kpCoeffTokenVlcTable[iNcMapIdx+1][uiValue][iIndexValue][0])
				uiCount = uint32(pVlcTable.kpCoeffTokenVlcTable[iNcMapIdx+1][uiValue][iIndexValue][1])
				POP_BUFFER(pBitsCache, uiCount)
				iUsedBits += int32(uiCount)
			} else {
				iIndexVlc = int32(pVlcTable.kpCoeffTokenVlcTable[0][iNcMapIdx][uiValue][0])
				uiCount = uint32(pVlcTable.kpCoeffTokenVlcTable[0][iNcMapIdx][uiValue][1])
				uiValue = pBitsCache.uiCache32Bit >> (32 - uiCount)
				POP_BUFFER(pBitsCache, uiCount)
				iUsedBits += int32(uiCount)
			}
		} else {
			uiValue = pBitsCache.uiCache32Bit >> (32 - 6)
			POP_BUFFER(pBitsCache, 6)
			iUsedBits += 6
			iIndexVlc = int32(pVlcTable.kpCoeffTokenVlcTable[0][3][uiValue][0]) //differ
		}
		*uiTrailingOnes = g_kuiVlcTrailingOneTotalCoeffTable[iIndexVlc][0]
		*uiTotalCoeff = g_kuiVlcTrailingOneTotalCoeffTable[iIndexVlc][1]
	}

	return iUsedBits
}

func CavlcGetLevelVal(iLevel *[16]int32, pBitsCache *SReadBitsCache, uiTotalCoeff uint8,
	uiTrailingOnes uint8) int32 {
	var i, iUsedBits int32
	var iSuffixLength, iSuffixLengthSize, iLevelPrefix, iPrefixBits, iLevelCode, iThreshold int32
	for i = 0; i < int32(uiTrailingOnes); i++ {
		iLevel[i] = int32(1 - ((pBitsCache.uiCache32Bit >> uint32(30-i)) & 0x02))
	}
	POP_BUFFER(pBitsCache, uiTrailingOnes)
	iUsedBits += int32(uiTrailingOnes)

	iSuffixLength = b2i32(uiTotalCoeff > 10 && uiTrailingOnes < 3)

	for ; i < int32(uiTotalCoeff); i++ {
		if pBitsCache.uiRemainBits <= 16 {
			SHIFT_BUFFER(pBitsCache)
		}
		iPrefixBits = int32(WELS_GET_PREFIX_BITS(pBitsCache.uiCache32Bit))
		if iPrefixBits > MAX_LEVEL_PREFIX+1 { //iPrefixBits includes leading "0"s and first "1", should +1
			return -1
		}
		POP_BUFFER(pBitsCache, iPrefixBits)
		iUsedBits += iPrefixBits
		iLevelPrefix = iPrefixBits - 1

		iLevelCode = iLevelPrefix << uint32(iSuffixLength) //differ
		iSuffixLengthSize = iSuffixLength

		if iLevelPrefix >= 14 {
			if 14 == iLevelPrefix && 0 == iSuffixLength {
				iSuffixLengthSize = 4
			} else if 15 == iLevelPrefix {
				iSuffixLengthSize = 12
				if iSuffixLength == 0 {
					iLevelCode += 15
				}
			}
		}

		if iSuffixLengthSize > 0 {
			if int32(pBitsCache.uiRemainBits) <= iSuffixLengthSize {
				SHIFT_BUFFER(pBitsCache)
			}
			iLevelCode += int32(pBitsCache.uiCache32Bit >> uint32(32-iSuffixLengthSize))
			POP_BUFFER(pBitsCache, iSuffixLengthSize)
			iUsedBits += iSuffixLengthSize
		}

		iLevelCode += b2i32((i == int32(uiTrailingOnes)) && (uiTrailingOnes < 3)) << 1
		iLevel[i] = (iLevelCode + 2) >> 1
		iLevel[i] -= (iLevel[i] << 1) & (-(iLevelCode & 0x01))

		iSuffixLength += b2i32(iSuffixLength == 0)
		iThreshold = 3 << uint32(iSuffixLength-1)
		iSuffixLength += b2i32(((iLevel[i] > iThreshold) || (iLevel[i] < -iThreshold)) && (iSuffixLength < 6))
	}

	return iUsedBits
}

func CavlcGetTotalZeros(iZerosLeft *int32, pBitsCache *SReadBitsCache, uiTotalCoeff uint8,
	pVlcTable *SVlcTable, bChromaDc bool) int32 {
	var iCount, iUsedBits int32
	var kpBitNumMap []uint8
	var uiValue uint32

	var iTotalZeroVlcIdx int32
	var uiTableType uint8
	//chroma_dc (0 < uiTotalCoeff < 4); others (chroma_ac or luma: 0 < uiTotalCoeff < 16)

	if bChromaDc {
		iTotalZeroVlcIdx = int32(uiTotalCoeff)
		kpBitNumMap = g_kuiTotalZerosBitNumChromaMap[:]
		uiTableType = 1
	} else {
		iTotalZeroVlcIdx = int32(uiTotalCoeff)
		kpBitNumMap = g_kuiTotalZerosBitNumMap[:]
		uiTableType = 0
	}

	iCount = int32(kpBitNumMap[iTotalZeroVlcIdx-1])
	if int32(pBitsCache.uiRemainBits) < iCount {
		SHIFT_BUFFER(pBitsCache) // if uiRemainBits+16 still smaller than iCount?? potential bug
	}
	uiValue = pBitsCache.uiCache32Bit >> uint32(32-iCount)
	iCount = int32(pVlcTable.kpTotalZerosTable[uiTableType][iTotalZeroVlcIdx-1][uiValue][1])
	POP_BUFFER(pBitsCache, iCount)
	iUsedBits += iCount
	*iZerosLeft = int32(pVlcTable.kpTotalZerosTable[uiTableType][iTotalZeroVlcIdx-1][uiValue][0])

	return iUsedBits
}

func CavlcGetRunBefore(iRun *[16]int32, pBitsCache *SReadBitsCache, uiTotalCoeff uint8,
	pVlcTable *SVlcTable, iZerosLeft int32) int32 {
	var i, iUsedBits int32
	var uiCount, uiValue, iPrefixBits uint32

	for i = 0; i < int32(uiTotalCoeff)-1; i++ {
		if iZerosLeft > 0 {
			uiCount = uint32(g_kuiZeroLeftBitNumMap[iZerosLeft])
			if uint32(pBitsCache.uiRemainBits) < uiCount {
				SHIFT_BUFFER(pBitsCache)
			}
			uiValue = pBitsCache.uiCache32Bit >> (32 - uiCount)
			if iZerosLeft < 7 {
				uiCount = uint32(pVlcTable.kpZeroTable[iZerosLeft-1][uiValue][1])
				POP_BUFFER(pBitsCache, uiCount)
				iUsedBits += int32(uiCount)
				iRun[i] = int32(pVlcTable.kpZeroTable[iZerosLeft-1][uiValue][0])
			} else {
				POP_BUFFER(pBitsCache, uiCount)
				iUsedBits += int32(uiCount)
				if pVlcTable.kpZeroTable[6][uiValue][0] < 7 {
					iRun[i] = int32(pVlcTable.kpZeroTable[6][uiValue][0])
				} else {
					if pBitsCache.uiRemainBits < 16 {
						SHIFT_BUFFER(pBitsCache)
					}
					iPrefixBits = WELS_GET_PREFIX_BITS(pBitsCache.uiCache32Bit)
					iRun[i] = int32(iPrefixBits + 6)
					if iRun[i] > iZerosLeft {
						return -1
					}
					POP_BUFFER(pBitsCache, iPrefixBits)
					iUsedBits += int32(iPrefixBits)
				}
			}
		} else {
			for j := i; j < int32(uiTotalCoeff); j++ {
				iRun[j] = 0
			}
			return iUsedBits
		}

		iZerosLeft -= iRun[i]
	}

	iRun[int32(uiTotalCoeff)-1] = iZerosLeft

	return iUsedBits
}

// cavlcInitReadBitsCache fills an SReadBitsCache at bit position iCurIdx of
// pBs (shared prologue of WelsResidualBlockCavlc and WelsResidualBlockCavlc8x8).
func cavlcInitReadBitsCache(sReadBitsCache *SReadBitsCache, pBs *common.SBitStringAux, iCurIdx int) {
	pBufOff := pBs.PStartBuf + (iCurIdx >> 3)
	pBuf := pBs.PBuf
	uiCache32Bit := (((cavlcByteAt(pBuf, pBufOff) << 8) | cavlcByteAt(pBuf, pBufOff+1)) << 16) |
		(cavlcByteAt(pBuf, pBufOff+2) << 8) | cavlcByteAt(pBuf, pBufOff+3)
	sReadBitsCache.uiCache32Bit = uiCache32Bit << uint32(iCurIdx&0x07)
	sReadBitsCache.uiRemainBits = uint8(32 - (iCurIdx & 0x07))
	sReadBitsCache.pBuf = pBuf
	sReadBitsCache.iBufOff = pBufOff
}

// int32_t WelsResidualBlockCavlc (SVlcTable* pVlcTable, uint8_t* pNonZeroCountCache, PBitStringAux
// pBs, int32_t iIndex, int32_t iMaxNumCoeff, const uint8_t* kpZigzagTable, int32_t iResidualProperty,
// int16_t* pTCoeff, uint8_t uiQp, PWelsDecoderContext pCtx)
//
// pTCoeff: coefficient sub-slice starting at the C pointer.
func WelsResidualBlockCavlc(pVlcTable *SVlcTable, pNonZeroCountCache []uint8, pBs *common.SBitStringAux, iIndex int32, iMaxNumCoeff int32, kpZigzagTable []uint8, iResidualProperty int32, pTCoeff []int16, uiQp uint8, pCtx *SWelsDecoderContext) int32 {
	var iLevel [16]int32
	var iZerosLeft, iCoeffNum int32
	var iRun [16]int32
	var iCurNonZeroCacheIdx, i int32

	var iMbResProperty int32
	GetMbResProperty(&iMbResProperty, &iResidualProperty, true)
	var kpDequantCoeff []uint16
	if pCtx.bUseScalingList {
		kpDequantCoeff = pCtx.pDequant_coeff4x4[iMbResProperty][uiQp][:]
	} else {
		kpDequantCoeff = common.G_kuiDequantCoeff[uiQp][:]
	}

	var nA, nB, nC int8
	var uiTotalCoeff, uiTrailingOnes uint8
	var iUsedBits int32
	iCurIdx := pBs.IIndex
	bChromaDc := (CHROMA_DC == iResidualProperty)
	bChroma := (bChromaDc || CHROMA_AC == iResidualProperty)
	var sReadBitsCache SReadBitsCache

	cavlcInitReadBitsCache(&sReadBitsCache, pBs, iCurIdx)
	//////////////////////////////////////////////////////////////////////////

	if bChroma {
		iCurNonZeroCacheIdx = int32(common.G_kuiCache48CountScan4Idx[iIndex])
		nA = int8(pNonZeroCountCache[iCurNonZeroCacheIdx-1])
		nB = int8(pNonZeroCountCache[iCurNonZeroCacheIdx-8])
	} else { //luma
		iCurNonZeroCacheIdx = int32(common.G_kuiCache48CountScan4Idx[iIndex])
		nA = int8(pNonZeroCountCache[iCurNonZeroCacheIdx-1])
		nB = int8(pNonZeroCountCache[iCurNonZeroCacheIdx-8])
	}

	nC = common.WELS_NON_ZERO_COUNT_AVERAGE(nA, nB)

	iUsedBits += CavlcGetTrailingOnesAndTotalCoeff(&uiTotalCoeff, &uiTrailingOnes, &sReadBitsCache, pVlcTable, bChromaDc,
		nC)

	if iResidualProperty != CHROMA_DC && iResidualProperty != I16_LUMA_DC {
		pNonZeroCountCache[iCurNonZeroCacheIdx] = uiTotalCoeff
		//////////////////////////////////////////////////////////////////////////
	}
	if 0 == uiTotalCoeff {
		pBs.IIndex += int(iUsedBits)
		return ERR_NONE
	}
	if (uiTrailingOnes > 3) || (uiTotalCoeff > 16) { /////////////////check uiTrailingOnes and uiTotalCoeff
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_CAVLC_INVALID_TOTAL_COEFF_OR_TRAILING_ONES)
	}
	if i = CavlcGetLevelVal(&iLevel, &sReadBitsCache, uiTotalCoeff, uiTrailingOnes); i == -1 {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_CAVLC_INVALID_LEVEL)
	}
	iUsedBits += i
	if int32(uiTotalCoeff) < iMaxNumCoeff {
		iUsedBits += CavlcGetTotalZeros(&iZerosLeft, &sReadBitsCache, uiTotalCoeff, pVlcTable, bChromaDc)
	} else {
		iZerosLeft = 0
	}

	if (iZerosLeft < 0) || ((iZerosLeft + int32(uiTotalCoeff)) > iMaxNumCoeff) {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_CAVLC_INVALID_ZERO_LEFT)
	}
	if i = CavlcGetRunBefore(&iRun, &sReadBitsCache, uiTotalCoeff, pVlcTable, iZerosLeft); i == -1 {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_CAVLC_INVALID_RUN_BEFORE)
	}
	iUsedBits += i
	pBs.IIndex += int(iUsedBits)
	iCoeffNum = -1

	if iResidualProperty == CHROMA_DC {
		//chroma dc scaling process, is kpDequantCoeff[0]? LevelScale(qPdc%6,0,0))<<(qPdc/6-6), the transform is done at construction.
		for i = int32(uiTotalCoeff) - 1; i >= 0; i-- {
			//FIXME merge into rundecode?
			iCoeffNum += iRun[i] + 1 //FIXME add 1 earlier ?
			j := kpZigzagTable[iCoeffNum]
			pTCoeff[j] = int16(iLevel[i])
		}
		WelsChromaDcIdct(pTCoeff)
		//scaling
		if !pCtx.bUseScalingList {
			for j := 0; j < 4; j++ {
				pTCoeff[kpZigzagTable[j]] = int16((int32(pTCoeff[kpZigzagTable[j]]) * int32(kpDequantCoeff[0])) >> 1)
			}
		} else {
			for j := 0; j < 4; j++ {
				pTCoeff[kpZigzagTable[j]] = int16((int64(pTCoeff[kpZigzagTable[j]]) * int64(kpDequantCoeff[0])) >> 5)
			}
		}
	} else if iResidualProperty == I16_LUMA_DC { //DC coefficent, only call in Intra_16x16, base_mode_flag = 0
		for i = int32(uiTotalCoeff) - 1; i >= 0; i-- { //FIXME merge into rundecode?
			iCoeffNum += iRun[i] + 1 //FIXME add 1 earlier ?
			j := kpZigzagTable[iCoeffNum]
			pTCoeff[j] = int16(iLevel[i])
		}
		WelsLumaDcDequantIdct(pTCoeff, int32(uiQp), pCtx)
	} else {
		for i = int32(uiTotalCoeff) - 1; i >= 0; i-- { //FIXME merge into  rundecode?
			iCoeffNum += iRun[i] + 1 //FIXME add 1 earlier ?
			j := kpZigzagTable[iCoeffNum]
			if !pCtx.bUseScalingList {
				pTCoeff[j] = int16(iLevel[i] * int32(kpDequantCoeff[j&0x07]))
			} else {
				pTCoeff[j] = int16((iLevel[i]*int32(kpDequantCoeff[j]) + 8) >> 4)
			}
		}
	}

	return ERR_NONE
}

// int32_t WelsResidualBlockCavlc8x8 (SVlcTable* pVlcTable, uint8_t* pNonZeroCountCache, PBitStringAux
// pBs, int32_t iIndex, int32_t iMaxNumCoeff, const uint8_t* kpZigzagTable, int32_t iResidualProperty,
// int16_t* pTCoeff, int32_t iIdx4x4, uint8_t uiQp, PWelsDecoderContext pCtx)
//
// pTCoeff: coefficient sub-slice starting at the C pointer.
func WelsResidualBlockCavlc8x8(pVlcTable *SVlcTable, pNonZeroCountCache []uint8, pBs *common.SBitStringAux, iIndex int32, iMaxNumCoeff int32, kpZigzagTable []uint8, iResidualProperty int32, pTCoeff []int16, iIdx4x4 int32, uiQp uint8, pCtx *SWelsDecoderContext) int32 {
	var iLevel [16]int32
	var iZerosLeft, iCoeffNum int32
	var iRun [16]int32
	var iCurNonZeroCacheIdx, i int32

	var iMbResProperty int32
	GetMbResProperty(&iMbResProperty, &iResidualProperty, true)

	var kpDequantCoeff []uint16
	if pCtx.bUseScalingList {
		kpDequantCoeff = pCtx.pDequant_coeff8x8[iMbResProperty-6][uiQp][:]
	} else {
		kpDequantCoeff = common.G_kuiDequantCoeff8x8[uiQp][:]
	}

	var nA, nB, nC int8
	var uiTotalCoeff, uiTrailingOnes uint8
	var iUsedBits int32
	iCurIdx := pBs.IIndex
	bChromaDc := (CHROMA_DC == iResidualProperty)
	bChroma := (bChromaDc || CHROMA_AC == iResidualProperty)
	var sReadBitsCache SReadBitsCache

	cavlcInitReadBitsCache(&sReadBitsCache, pBs, iCurIdx)
	//////////////////////////////////////////////////////////////////////////

	if bChroma {
		iCurNonZeroCacheIdx = int32(common.G_kuiCache48CountScan4Idx[iIndex])
		nA = int8(pNonZeroCountCache[iCurNonZeroCacheIdx-1])
		nB = int8(pNonZeroCountCache[iCurNonZeroCacheIdx-8])
	} else { //luma
		iCurNonZeroCacheIdx = int32(common.G_kuiCache48CountScan4Idx[iIndex])
		nA = int8(pNonZeroCountCache[iCurNonZeroCacheIdx-1])
		nB = int8(pNonZeroCountCache[iCurNonZeroCacheIdx-8])
	}

	nC = common.WELS_NON_ZERO_COUNT_AVERAGE(nA, nB)

	iUsedBits += CavlcGetTrailingOnesAndTotalCoeff(&uiTotalCoeff, &uiTrailingOnes, &sReadBitsCache, pVlcTable, bChromaDc,
		nC)

	if iResidualProperty != CHROMA_DC && iResidualProperty != I16_LUMA_DC {
		pNonZeroCountCache[iCurNonZeroCacheIdx] = uiTotalCoeff
		//////////////////////////////////////////////////////////////////////////
	}
	if 0 == uiTotalCoeff {
		pBs.IIndex += int(iUsedBits)
		return ERR_NONE
	}
	if (uiTrailingOnes > 3) || (uiTotalCoeff > 16) { /////////////////check uiTrailingOnes and uiTotalCoeff
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_CAVLC_INVALID_TOTAL_COEFF_OR_TRAILING_ONES)
	}
	if i = CavlcGetLevelVal(&iLevel, &sReadBitsCache, uiTotalCoeff, uiTrailingOnes); i == -1 {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_CAVLC_INVALID_LEVEL)
	}
	iUsedBits += i
	if int32(uiTotalCoeff) < iMaxNumCoeff {
		iUsedBits += CavlcGetTotalZeros(&iZerosLeft, &sReadBitsCache, uiTotalCoeff, pVlcTable, bChromaDc)
	} else {
		iZerosLeft = 0
	}

	if (iZerosLeft < 0) || ((iZerosLeft + int32(uiTotalCoeff)) > iMaxNumCoeff) {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_CAVLC_INVALID_ZERO_LEFT)
	}
	if i = CavlcGetRunBefore(&iRun, &sReadBitsCache, uiTotalCoeff, pVlcTable, iZerosLeft); i == -1 {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_CAVLC_INVALID_RUN_BEFORE)
	}
	iUsedBits += i
	pBs.IIndex += int(iUsedBits)
	iCoeffNum = -1

	iQp := int32(uiQp)
	for i = int32(uiTotalCoeff) - 1; i >= 0; i-- { //FIXME merge into  rundecode?
		var j int32
		iCoeffNum += iRun[i] + 1 //FIXME add 1 earlier ?
		j = (iCoeffNum << 2) + iIdx4x4
		j = int32(kpZigzagTable[j])
		if iQp >= 36 {
			pTCoeff[j] = int16((iLevel[i] * int32(kpDequantCoeff[j])) * (int32(1) << uint32(iQp/6-6)))
		} else {
			pTCoeff[j] = int16((iLevel[i]*int32(kpDequantCoeff[j]) + (int32(1) << uint32(5-iQp/6))) >> uint32(6-iQp/6))
		}
	}

	return ERR_NONE
}

// cavlcCheckRefIdx implements the repeated "error ref_idx" block of the
// CAVLC inter parsers. It returns (iRefIdx, iRet): when iRet != ERR_NONE
// the caller must return it. bCheckNull adds the RETURN_ERR_IF_NULL check
// of the B-slice variants.
func cavlcCheckRefIdx(pCtx *SWelsDecoderContext, ppRefPic []*SPicture, iRefIdx int32, iRefCount int32, bIsPending bool, bCheckNull bool) (int32, int32) {
	if (iRefIdx < 0) || (iRefIdx >= iRefCount) || (ppRefPic[iRefIdx] == nil) { //error ref_idx
		pCtx.bMbRefConcealed = true
		if pCtx.pParam.EEcActiveIdc != api.ERROR_CON_DISABLE {
			iRefIdx = 0
			pCtx.iErrorCode |= int32(api.DsBitstreamError)
			if bCheckNull && ppRefPic[iRefIdx] == nil {
				return iRefIdx, GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_REF_INDEX)
			}
		} else {
			return iRefIdx, GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_REF_INDEX)
		}
	}
	pCtx.bMbRefConcealed = pCtx.bRPLRError || pCtx.bMbRefConcealed || !(ppRefPic[iRefIdx] != nil &&
		(ppRefPic[iRefIdx].bIsComplete || bIsPending))
	return iRefIdx, ERR_NONE
}

// cavlcAddMvd is `iMv[k] += iCode;` with the int16 truncation of C.
func cavlcAddMvd(iMv *[2]int16, k int, iCode int32) {
	iMv[k] = int16(int32(iMv[k]) + iCode)
}

// int32_t ParseInterInfo (PWelsDecoderContext pCtx, int16_t iMvArray[LIST_A][30][MV_A], int8_t
// iRefIdxArray[LIST_A][30], PBitStringAux pBs)
func ParseInterInfo(pCtx *SWelsDecoderContext, iMvArray *[common.LIST_A][30][common.MV_A]int16, iRefIdxArray *[common.LIST_A][30]int8, pBs *common.SBitStringAux) int32 {
	pSlice := &pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	ppRefPic := pCtx.sRefPic.pRefList[common.LIST_0][:]
	var iRefCount [2]int32
	pCurDqLayer := pCtx.pCurDqLayer
	var i, j int32
	iMbXy := pCurDqLayer.iMbXyIndex
	var iMotionPredFlag [4]int32
	var iMv [2]int16
	var uiCode uint32
	var iCode int32
	iMinVmv := pSliceHeader.pSps.pSLevelLimits.IMinVmv
	iMaxVmv := pSliceHeader.pSps.pSLevelLimits.IMaxVmv
	iDefault := b2i32(pSlice.sSliceHeaderExt.bDefaultMotionPredFlag)
	iMotionPredFlag[0], iMotionPredFlag[1], iMotionPredFlag[2], iMotionPredFlag[3] = iDefault, iDefault, iDefault, iDefault
	iRefCount[0] = pSliceHeader.uiRefCount[0]
	iRefCount[1] = pSliceHeader.uiRefCount[1]

	bIsPending := GetThreadCount(pCtx) > 1

	switch pCurDqLayer.pDec.pMbType[iMbXy] {
	case common.MB_TYPE_16x16:
		var iRefIdx int32
		if pSlice.sSliceHeaderExt.bAdaptiveMotionPredFlag {
			if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l0[ mbPartIdx ]
				return int32(uiRet)
			}
			iMotionPredFlag[0] = int32(uiCode)
		}
		if iMotionPredFlag[0] == 0 {
			if uiRet := BsGetTe0(pBs, iRefCount[0], &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l1[ mbPartIdx ]
				return uiRet
			}
			iRefIdx = int32(uiCode)
			// Security check: iRefIdx should be in range 0 to num_ref_idx_l0_active_minus1, includsive
			// ref to standard section 7.4.5.1. iRefCount[0] is 1 + num_ref_idx_l0_active_minus1.
			var iRet int32
			if iRefIdx, iRet = cavlcCheckRefIdx(pCtx, ppRefPic, iRefIdx, iRefCount[0], bIsPending, false); iRet != ERR_NONE {
				return iRet
			}
		} else {
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "inter parse: iMotionPredFlag = 1 not supported. ")
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_UNSUPPORTED_ILP)
		}
		PredMv(iMvArray, iRefIdxArray, common.LIST_0, 0, 4, int8(iRefIdx), &iMv)

		if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l0[ mbPartIdx ][ 0 ][ compIdx ]
			return uiRet
		}
		cavlcAddMvd(&iMv, 0, iCode)
		if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l1[ mbPartIdx ][ 0 ][ compIdx ]
			return uiRet
		}
		cavlcAddMvd(&iMv, 1, iCode)
		WELS_CHECK_SE_BOTH_WARNING(pCtx, iMv[1], iMinVmv, iMaxVmv, "vertical mv")
		UpdateP16x16MotionInfo(pCurDqLayer, common.LIST_0, int8(iRefIdx), &iMv)
	case common.MB_TYPE_16x8:
		var iRefIdx [2]int32
		for i = 0; i < 2; i++ {
			if pSlice.sSliceHeaderExt.bAdaptiveMotionPredFlag {
				if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l0[ mbPartIdx ]
					return int32(uiRet)
				}
				iMotionPredFlag[i] = int32(uiCode)
			}
		}

		for i = 0; i < 2; i++ {
			if iMotionPredFlag[i] != 0 {
				common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "inter parse: iMotionPredFlag = 1 not supported. ")
				return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_UNSUPPORTED_ILP)
			}
			if uiRet := BsGetTe0(pBs, iRefCount[0], &uiCode); uiRet != ERR_NONE { //ref_idx_l0[ mbPartIdx ]
				return uiRet
			}
			iRefIdx[i] = int32(uiCode)
			var iRet int32
			if iRefIdx[i], iRet = cavlcCheckRefIdx(pCtx, ppRefPic, iRefIdx[i], iRefCount[0], bIsPending, false); iRet != ERR_NONE {
				return iRet
			}
		}
		for i = 0; i < 2; i++ {
			PredInter16x8Mv(iMvArray, iRefIdxArray, common.LIST_0, i<<3, int8(iRefIdx[i]), &iMv)

			if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l0[ mbPartIdx ][ 0 ][ compIdx ]
				return uiRet
			}
			cavlcAddMvd(&iMv, 0, iCode)
			if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l1[ mbPartIdx ][ 0 ][ compIdx ]
				return uiRet
			}
			cavlcAddMvd(&iMv, 1, iCode)
			WELS_CHECK_SE_BOTH_WARNING(pCtx, iMv[1], iMinVmv, iMaxVmv, "vertical mv")
			UpdateP16x8MotionInfo(pCurDqLayer, iMvArray, iRefIdxArray, common.LIST_0, i<<3, int8(iRefIdx[i]), &iMv)
		}
	case common.MB_TYPE_8x16:
		var iRefIdx [2]int32
		for i = 0; i < 2; i++ {
			if pSlice.sSliceHeaderExt.bAdaptiveMotionPredFlag {
				if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l0[ mbPartIdx ]
					return int32(uiRet)
				}
				iMotionPredFlag[i] = int32(uiCode)
			}
		}

		for i = 0; i < 2; i++ {
			if iMotionPredFlag[i] == 0 {
				if uiRet := BsGetTe0(pBs, iRefCount[0], &uiCode); uiRet != ERR_NONE { //ref_idx_l0[ mbPartIdx ]
					return uiRet
				}
				iRefIdx[i] = int32(uiCode)
				var iRet int32
				if iRefIdx[i], iRet = cavlcCheckRefIdx(pCtx, ppRefPic, iRefIdx[i], iRefCount[0], bIsPending, false); iRet != ERR_NONE {
					return iRet
				}
			} else {
				common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "inter parse: iMotionPredFlag = 1 not supported. ")
				return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_UNSUPPORTED_ILP)
			}

		}
		for i = 0; i < 2; i++ {
			PredInter8x16Mv(iMvArray, iRefIdxArray, common.LIST_0, i<<2, int8(iRefIdx[i]), &iMv)

			if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l0[ mbPartIdx ][ 0 ][ compIdx ]
				return uiRet
			}
			cavlcAddMvd(&iMv, 0, iCode)
			if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l1[ mbPartIdx ][ 0 ][ compIdx ]
				return uiRet
			}
			cavlcAddMvd(&iMv, 1, iCode)
			WELS_CHECK_SE_BOTH_WARNING(pCtx, iMv[1], iMinVmv, iMaxVmv, "vertical mv")
			UpdateP8x16MotionInfo(pCurDqLayer, iMvArray, iRefIdxArray, common.LIST_0, i<<2, int8(iRefIdx[i]), &iMv)
		}
	case common.MB_TYPE_8x8, common.MB_TYPE_8x8_REF0:
		var iRefIdx [4]int32
		var iSubPartCount, iPartWidth [4]int32
		var uiSubMbType uint32

		if common.MB_TYPE_8x8_REF0 == pCurDqLayer.pDec.pMbType[iMbXy] {
			iRefCount[0] = 1
			iRefCount[1] = 1
		}

		//uiSubMbType, partition
		for i = 0; i < 4; i++ {
			if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //sub_mb_type[ mbPartIdx ]
				return int32(uiRet)
			}
			uiSubMbType = uiCode
			if uiSubMbType >= 4 { //invalid uiSubMbType
				return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_SUB_MB_TYPE)
			}
			pCurDqLayer.pSubMbType[iMbXy][i] = g_ksInterPSubMbTypeInfo[uiSubMbType].iType
			iSubPartCount[i] = int32(g_ksInterPSubMbTypeInfo[uiSubMbType].iPartCount)
			iPartWidth[i] = int32(g_ksInterPSubMbTypeInfo[uiSubMbType].iPartWidth)

			// Need modification when B picture add in, reference to 7.3.5
			pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] && (uiSubMbType == 0)
		}

		if pSlice.sSliceHeaderExt.bAdaptiveMotionPredFlag {
			for i = 0; i < 4; i++ {
				if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l0[ mbPartIdx ]
					return int32(uiRet)
				}
				iMotionPredFlag[i] = int32(uiCode)
			}
		}

		//iRefIdxArray
		if common.MB_TYPE_8x8_REF0 == pCurDqLayer.pDec.pMbType[iMbXy] {
			pCurDqLayer.pDec.pRefIndex[0][iMbXy] = [common.MB_BLOCK4x4_NUM]int8{}
		} else {
			for i = 0; i < 4; i++ {
				iIndex8 := int16(i << 2)
				uiScan4Idx := int(g_kuiScan4[iIndex8])

				if iMotionPredFlag[i] == 0 {
					if uiRet := BsGetTe0(pBs, iRefCount[0], &uiCode); uiRet != ERR_NONE { //ref_idx_l0[ mbPartIdx ]
						return uiRet
					}
					iRefIdx[i] = int32(uiCode)
					var iRet int32
					if iRefIdx[i], iRet = cavlcCheckRefIdx(pCtx, ppRefPic, iRefIdx[i], iRefCount[0], bIsPending, false); iRet != ERR_NONE {
						return iRet
					}

					pMbRef := &pCurDqLayer.pDec.pRefIndex[0][iMbXy]
					pMbRef[uiScan4Idx] = int8(iRefIdx[i])
					pMbRef[uiScan4Idx+1] = int8(iRefIdx[i])
					pMbRef[uiScan4Idx+4] = int8(iRefIdx[i])
					pMbRef[uiScan4Idx+5] = int8(iRefIdx[i])
				} else {
					common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "inter parse: iMotionPredFlag = 1 not supported. ")
					return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_UNSUPPORTED_ILP)
				}
			}
		}

		//gain mv and update mv cache
		for i = 0; i < 4; i++ {
			iPartCount := int8(iSubPartCount[i])
			uiSubMbType := pCurDqLayer.pSubMbType[iMbXy][i]
			var iMv [2]int16
			var iPartIdx int16
			iBlockWidth := int16(iPartWidth[i])
			iIdx := int16(i << 2)
			var uiScan4Idx, uiCacheIdx int

			uiIdx4Cache := int(common.G_kuiCache30ScanIdx[iIdx])

			iRefIdxArray[0][uiIdx4Cache] = int8(iRefIdx[i])
			iRefIdxArray[0][uiIdx4Cache+1] = int8(iRefIdx[i])
			iRefIdxArray[0][uiIdx4Cache+6] = int8(iRefIdx[i])
			iRefIdxArray[0][uiIdx4Cache+7] = int8(iRefIdx[i])

			pDecMv := &pCurDqLayer.pDec.pMv[0][iMbXy]
			for j = 0; j < int32(iPartCount); j++ {
				iPartIdx = iIdx + int16(j)*iBlockWidth
				uiScan4Idx = int(g_kuiScan4[iPartIdx])
				uiCacheIdx = int(common.G_kuiCache30ScanIdx[iPartIdx])
				PredMv(iMvArray, iRefIdxArray, common.LIST_0, int32(iPartIdx), int32(iBlockWidth), int8(iRefIdx[i]), &iMv)

				if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l0[ mbPartIdx ][ subMbPartIdx ][ compIdx ]
					return uiRet
				}
				cavlcAddMvd(&iMv, 0, iCode)
				if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l1[ mbPartIdx ][ subMbPartIdx ][ compIdx ]
					return uiRet
				}
				cavlcAddMvd(&iMv, 1, iCode)
				WELS_CHECK_SE_BOTH_WARNING(pCtx, iMv[1], iMinVmv, iMaxVmv, "vertical mv")
				if common.SUB_MB_TYPE_8x8 == uiSubMbType {
					pDecMv[uiScan4Idx] = iMv
					pDecMv[uiScan4Idx+1] = iMv
					pDecMv[uiScan4Idx+4] = iMv
					pDecMv[uiScan4Idx+5] = iMv
					iMvArray[0][uiCacheIdx] = iMv
					iMvArray[0][uiCacheIdx+1] = iMv
					iMvArray[0][uiCacheIdx+6] = iMv
					iMvArray[0][uiCacheIdx+7] = iMv
				} else if common.SUB_MB_TYPE_8x4 == uiSubMbType {
					pDecMv[uiScan4Idx] = iMv
					pDecMv[uiScan4Idx+1] = iMv
					iMvArray[0][uiCacheIdx] = iMv
					iMvArray[0][uiCacheIdx+1] = iMv
				} else if common.SUB_MB_TYPE_4x8 == uiSubMbType {
					pDecMv[uiScan4Idx] = iMv
					pDecMv[uiScan4Idx+4] = iMv
					iMvArray[0][uiCacheIdx] = iMv
					iMvArray[0][uiCacheIdx+6] = iMv
				} else { //SUB_MB_TYPE_4x4 == uiSubMbType
					pDecMv[uiScan4Idx] = iMv
					iMvArray[0][uiCacheIdx] = iMv
				}
			}
		}
	default:
	}

	return ERR_NONE
}

// int32_t ParseInterBInfo (PWelsDecoderContext pCtx, int16_t iMvArray[LIST_A][30][MV_A], int8_t
// iRefIdxArray[LIST_A][30], PBitStringAux pBs)
func ParseInterBInfo(pCtx *SWelsDecoderContext, iMvArray *[common.LIST_A][30][common.MV_A]int16, iRefIdxArray *[common.LIST_A][30]int8, pBs *common.SBitStringAux) int32 {
	pSlice := &pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	var ppRefPic [2][]*SPicture
	ppRefPic[common.LIST_0] = pCtx.sRefPic.pRefList[common.LIST_0][:]
	ppRefPic[common.LIST_1] = pCtx.sRefPic.pRefList[common.LIST_1][:]
	var ref_idx_list [common.LIST_A][4]int8
	iRef := [2]int8{0, 0}
	var iRefCount [2]int32
	pCurDqLayer := pCtx.pCurDqLayer
	iMbXy := pCurDqLayer.iMbXyIndex
	var iMotionPredFlag [common.LIST_A][4]uint8
	var iMv [2]int16
	var uiCode uint32
	var iCode int32
	iMinVmv := pSliceHeader.pSps.pSLevelLimits.IMinVmv
	iMaxVmv := pSliceHeader.pSps.pSLevelLimits.IMaxVmv
	for l := range ref_idx_list {
		for k := range ref_idx_list[l] {
			ref_idx_list[l][k] = -1
		}
	}
	var uiDefaultFlag uint8
	if pSlice.sSliceHeaderExt.bDefaultMotionPredFlag {
		uiDefaultFlag = 1
	}
	for l := range iMotionPredFlag {
		for k := range iMotionPredFlag[l] {
			iMotionPredFlag[l][k] = uiDefaultFlag
		}
	}
	iRefCount[0] = pSliceHeader.uiRefCount[0]
	iRefCount[1] = pSliceHeader.uiRefCount[1]

	bIsPending := GetThreadCount(pCtx) > 1

	mbType := MbType(pCurDqLayer.pDec.pMbType[iMbXy])
	if common.IS_DIRECT(mbType) {

		var pMvDirect [common.LIST_A][2]int16
		var subMbType SubMbType
		if pSliceHeader.iDirectSpatialMvPredFlag != 0 {
			//predict direct spatial mv
			ret := PredMvBDirectSpatial(pCtx, &pMvDirect, &iRef, &subMbType)
			if ret != ERR_NONE {
				return ret
			}
		} else {
			//temporal direct 16x16 mode
			ret := PredBDirectTemporal(pCtx, &pMvDirect, &iRef, &subMbType)
			if ret != ERR_NONE {
				return ret
			}
		}
	} else if common.IS_INTER_16x16(mbType) {
		if pSlice.sSliceHeaderExt.bAdaptiveMotionPredFlag {
			for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
				if common.IS_DIR(mbType, 0, listIdx) {
					if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l0/l1[ mbPartIdx ]
						return int32(uiRet)
					}
					iMotionPredFlag[listIdx][0] = uint8(uiCode)
				}
			}
		}
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			if common.IS_DIR(mbType, 0, listIdx) {
				if iMotionPredFlag[listIdx][0] == 0 {
					if uiRet := BsGetTe0(pBs, iRefCount[listIdx], &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l1[ mbPartIdx ]
						return uiRet
					}
					ref_idx_list[listIdx][0] = int8(uiCode)
					// Security check: iRefIdx should be in range 0 to num_ref_idx_l0_active_minus1, includsive
					// ref to standard section 7.4.5.1. iRefCount[0] is 1 + num_ref_idx_l0_active_minus1.
					iRefIdx, iRet := cavlcCheckRefIdx(pCtx, ppRefPic[listIdx], int32(ref_idx_list[listIdx][0]), iRefCount[listIdx],
						bIsPending, true)
					ref_idx_list[listIdx][0] = int8(iRefIdx)
					if iRet != ERR_NONE {
						return iRet
					}
				} else {
					common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "inter parse: iMotionPredFlag = 1 not supported. ")
					return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_UNSUPPORTED_ILP)
				}
			}
		}
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			if common.IS_DIR(mbType, 0, listIdx) {
				PredMv(iMvArray, iRefIdxArray, listIdx, 0, 4, ref_idx_list[listIdx][0], &iMv)
				if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l0[ mbPartIdx ][ 0 ][ compIdx ]
					return uiRet
				}
				cavlcAddMvd(&iMv, 0, iCode)
				if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l1[ mbPartIdx ][ 0 ][ compIdx ]
					return uiRet
				}
				cavlcAddMvd(&iMv, 1, iCode)
				WELS_CHECK_SE_BOTH_WARNING(pCtx, iMv[1], iMinVmv, iMaxVmv, "vertical mv")
			} else {
				iMv = [2]int16{}
			}
			UpdateP16x16MotionInfo(pCurDqLayer, listIdx, ref_idx_list[listIdx][0], &iMv)
		}
	} else if common.IS_INTER_16x8(mbType) {
		if pSlice.sSliceHeaderExt.bAdaptiveMotionPredFlag {
			for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
				for i := int32(0); i < 2; i++ {
					if common.IS_DIR(mbType, i, listIdx) {
						if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l0/l1[ mbPartIdx ]
							return int32(uiRet)
						}
						iMotionPredFlag[listIdx][i] = uint8(uiCode)
					}
				}
			}
		}
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			for i := int32(0); i < 2; i++ {
				if common.IS_DIR(mbType, i, listIdx) {
					if iMotionPredFlag[listIdx][i] == 0 {
						if uiRet := BsGetTe0(pBs, iRefCount[listIdx], &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l1[ mbPartIdx ]
							return uiRet
						}
						iRefIdx := int32(uiCode)
						// Security check: iRefIdx should be in range 0 to num_ref_idx_l0_active_minus1, includsive
						// ref to standard section 7.4.5.1. iRefCount[0] is 1 + num_ref_idx_l0_active_minus1.
						var iRet int32
						if iRefIdx, iRet = cavlcCheckRefIdx(pCtx, ppRefPic[listIdx], iRefIdx, iRefCount[listIdx], bIsPending, true); iRet != ERR_NONE {
							return iRet
						}
						ref_idx_list[listIdx][i] = int8(iRefIdx)
					} else {
						common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "inter parse: iMotionPredFlag = 1 not supported. ")
						return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_UNSUPPORTED_ILP)
					}
				}
			}
		}
		// Read mvd_L0 then mvd_L1
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			// Partitions
			for i := int32(0); i < 2; i++ {
				iPartIdx := i << 3
				iRefIdx := int32(ref_idx_list[listIdx][i])
				if common.IS_DIR(mbType, i, listIdx) {
					PredInter16x8Mv(iMvArray, iRefIdxArray, listIdx, iPartIdx, int8(iRefIdx), &iMv)

					if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l{0,1}[ mbPartIdx ][ listIdx ][x]
						return uiRet
					}
					cavlcAddMvd(&iMv, 0, iCode)
					if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l{0,1}[ mbPartIdx ][ listIdx ][y]
						return uiRet
					}
					cavlcAddMvd(&iMv, 1, iCode)

					WELS_CHECK_SE_BOTH_WARNING(pCtx, iMv[1], iMinVmv, iMaxVmv, "vertical mv")
				} else {
					iMv = [2]int16{}
				}
				UpdateP16x8MotionInfo(pCurDqLayer, iMvArray, iRefIdxArray, listIdx, iPartIdx, int8(iRefIdx), &iMv)
			}
		}
	} else if common.IS_INTER_8x16(mbType) {
		if pSlice.sSliceHeaderExt.bAdaptiveMotionPredFlag {
			for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
				for i := int32(0); i < 2; i++ {
					if common.IS_DIR(mbType, i, listIdx) {
						if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l0/l1[ mbPartIdx ]
							return int32(uiRet)
						}
						iMotionPredFlag[listIdx][i] = uint8(uiCode)
					}
				}
			}
		}
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			for i := int32(0); i < 2; i++ {
				if common.IS_DIR(mbType, i, listIdx) {
					if iMotionPredFlag[listIdx][i] == 0 {
						if uiRet := BsGetTe0(pBs, iRefCount[listIdx], &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l1[ mbPartIdx ]
							return uiRet
						}
						iRefIdx := int32(uiCode)
						// Security check: iRefIdx should be in range 0 to num_ref_idx_l0_active_minus1, includsive
						// ref to standard section 7.4.5.1. iRefCount[0] is 1 + num_ref_idx_l0_active_minus1.
						var iRet int32
						if iRefIdx, iRet = cavlcCheckRefIdx(pCtx, ppRefPic[listIdx], iRefIdx, iRefCount[listIdx], bIsPending, true); iRet != ERR_NONE {
							return iRet
						}
						ref_idx_list[listIdx][i] = int8(iRefIdx)
					} else {
						common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "inter parse: iMotionPredFlag = 1 not supported. ")
						return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_UNSUPPORTED_ILP)
					}
				}
			}
		}
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			for i := int32(0); i < 2; i++ {
				iPartIdx := i << 2
				iRefIdx := int32(ref_idx_list[listIdx][i])
				if common.IS_DIR(mbType, i, listIdx) {
					PredInter8x16Mv(iMvArray, iRefIdxArray, listIdx, iPartIdx, int8(iRefIdx), &iMv)

					if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l0[ mbPartIdx ][ 0 ][ compIdx ]
						return uiRet
					}
					cavlcAddMvd(&iMv, 0, iCode)
					if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l1[ mbPartIdx ][ 0 ][ compIdx ]
						return uiRet
					}
					cavlcAddMvd(&iMv, 1, iCode)
					WELS_CHECK_SE_BOTH_WARNING(pCtx, iMv[1], iMinVmv, iMaxVmv, "vertical mv")
				} else {
					iMv = [2]int16{}
				}
				UpdateP8x16MotionInfo(pCurDqLayer, iMvArray, iRefIdxArray, listIdx, iPartIdx, int8(iRefIdx), &iMv)
			}
		}
	} else if common.IS_Inter_8x8(mbType) {
		var pSubPartCount, pPartW [4]int8
		var uiSubMbType uint32
		//sub_mb_type, partition
		var pMvDirect [common.LIST_A][2]int16
		if pCtx.sRefPic.pRefList[common.LIST_1][0] == nil {
			pLogCtx := &(pCtx.sLogCtx)
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "Colocated Ref Picture for B-Slice is lost, B-Slice decoding cannot be continued!")
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_DATA, ERR_INFO_REFERENCE_PIC_LOST)
		}
		bIsLongRef := pCtx.sRefPic.pRefList[common.LIST_1][0].bIsLongRef
		ref0Count := common.WELS_MIN(pSliceHeader.uiRefCount[common.LIST_0], int32(pCtx.sRefPic.uiRefCount[common.LIST_0]))
		has_direct_called := false
		var directSubMbType SubMbType = 0

		//uiSubMbType, partition
		for i := 0; i < 4; i++ {
			if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //sub_mb_type[ mbPartIdx ]
				return int32(uiRet)
			}
			uiSubMbType = uiCode
			if uiSubMbType >= 13 { //invalid uiSubMbType
				return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_SUB_MB_TYPE)
			}
			pSubPartCount[i] = g_ksInterBSubMbTypeInfo[uiSubMbType].iPartCount
			pPartW[i] = g_ksInterBSubMbTypeInfo[uiSubMbType].iPartWidth

			// Need modification when B picture add in, reference to 7.3.5
			if pSubPartCount[i] > 1 {
				pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = false
			}

			if common.IS_DIRECT(g_ksInterBSubMbTypeInfo[uiSubMbType].iType) {
				if !has_direct_called {
					if pSliceHeader.iDirectSpatialMvPredFlag != 0 {
						ret := PredMvBDirectSpatial(pCtx, &pMvDirect, &iRef, &directSubMbType)
						if ret != ERR_NONE {
							return ret
						}

					} else {
						//temporal direct mode
						ret := PredBDirectTemporal(pCtx, &pMvDirect, &iRef, &directSubMbType)
						if ret != ERR_NONE {
							return ret
						}
					}
					has_direct_called = true
				}
				pCurDqLayer.pSubMbType[iMbXy][i] = directSubMbType
				if common.IS_SUB_4x4(pCurDqLayer.pSubMbType[iMbXy][i]) {
					pSubPartCount[i] = 4
					pPartW[i] = 1
				}
			} else {
				pCurDqLayer.pSubMbType[iMbXy][i] = g_ksInterBSubMbTypeInfo[uiSubMbType].iType
			}
		}
		if pSlice.sSliceHeaderExt.bAdaptiveMotionPredFlag {
			for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
				for i := 0; i < 4; i++ {
					is_dir := common.IS_DIR(pCurDqLayer.pSubMbType[iMbXy][i], 0, listIdx)
					if is_dir {
						if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //motion_prediction_flag_l0[ mbPartIdx ]
							return int32(uiRet)
						}
						iMotionPredFlag[listIdx][i] = uint8(uiCode)
					}
				}
			}
		}
		for i := 0; i < 4; i++ { //Direct 8x8 Ref and mv
			iIdx8 := int16(i << 2)
			if common.IS_DIRECT(pCurDqLayer.pSubMbType[iMbXy][i]) {
				if pSliceHeader.iDirectSpatialMvPredFlag != 0 {
					FillSpatialDirect8x8Mv(pCurDqLayer, iIdx8, pSubPartCount[i], pPartW[i], directSubMbType, bIsLongRef, &pMvDirect, &iRef,
						iMvArray, nil)
				} else {
					mvColoc := &pCurDqLayer.iColocMv[common.LIST_0]
					iRef[common.LIST_1] = 0
					iRef[common.LIST_0] = 0
					uiColoc4Idx := g_kuiScan4[iIdx8]
					if pCurDqLayer.iColocIntra[uiColoc4Idx] == 0 {
						iRef[common.LIST_0] = 0
						colocRefIndexL0 := pCurDqLayer.iColocRefIndex[common.LIST_0][uiColoc4Idx]
						if colocRefIndexL0 >= 0 {
							iRef[common.LIST_0] = MapColToList0(pCtx, colocRefIndexL0, ref0Count)
						} else {
							mvColoc = &pCurDqLayer.iColocMv[common.LIST_1]
						}
					}
					Update8x8RefIdx(pCurDqLayer, iIdx8, common.LIST_0, iRef[common.LIST_0])
					Update8x8RefIdx(pCurDqLayer, iIdx8, common.LIST_1, iRef[common.LIST_1])
					FillTemporalDirect8x8Mv(pCurDqLayer, iIdx8, pSubPartCount[i], pPartW[i], directSubMbType, &iRef, mvColoc, iMvArray,
						nil)
				}
			}
		}
		//ref no-direct
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			for i := 0; i < 4; i++ {
				iIdx8 := int16(i << 2)
				subMbType := int32(pCurDqLayer.pSubMbType[iMbXy][i])
				var iref int8 = REF_NOT_IN_LIST
				if common.IS_DIRECT(subMbType) {
					if pSliceHeader.iDirectSpatialMvPredFlag != 0 {
						Update8x8RefIdx(pCurDqLayer, iIdx8, listIdx, iRef[listIdx])
						ref_idx_list[listIdx][i] = iRef[listIdx]
					}
				} else {
					if common.IS_DIR(subMbType, 0, listIdx) {
						if iMotionPredFlag[listIdx][i] == 0 {
							if uiRet := BsGetTe0(pBs, iRefCount[listIdx], &uiCode); uiRet != ERR_NONE { //ref_idx_l0[ mbPartIdx ]
								return uiRet
							}
							iref = int8(uiCode)
							iRefIdx, iRet := cavlcCheckRefIdx(pCtx, ppRefPic[listIdx], int32(iref), iRefCount[listIdx], bIsPending, true)
							iref = int8(iRefIdx)
							if iRet != ERR_NONE {
								return iRet
							}
						} else {
							common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "inter parse: iMotionPredFlag = 1 not supported. ")
							return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_UNSUPPORTED_ILP)
						}
					}
					Update8x8RefIdx(pCurDqLayer, iIdx8, listIdx, iref)
					ref_idx_list[listIdx][i] = iref
				}
			}
		}
		//mv
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			for i := 0; i < 4; i++ {
				iPartCount := pSubPartCount[i]
				var iPartIdx int16
				iBlockW := int16(pPartW[i])
				var uiScan4Idx, uiCacheIdx int

				uiCacheIdx = int(common.G_kuiCache30ScanIdx[i<<2])

				iref := ref_idx_list[listIdx][i]
				iRefIdxArray[listIdx][uiCacheIdx] = iref
				iRefIdxArray[listIdx][uiCacheIdx+1] = iref
				iRefIdxArray[listIdx][uiCacheIdx+6] = iref
				iRefIdxArray[listIdx][uiCacheIdx+7] = iref

				subMbType := pCurDqLayer.pSubMbType[iMbXy][i]
				if common.IS_DIRECT(subMbType) {
					continue
				}
				is_dir := common.IS_DIR(subMbType, 0, listIdx)
				pDecMv := &pCurDqLayer.pDec.pMv[listIdx][iMbXy]
				for j := int32(0); j < int32(iPartCount); j++ {
					iPartIdx = int16(i<<2) + int16(j)*iBlockW
					uiScan4Idx = int(g_kuiScan4[iPartIdx])
					uiCacheIdx = int(common.G_kuiCache30ScanIdx[iPartIdx])
					if is_dir {
						PredMv(iMvArray, iRefIdxArray, listIdx, int32(iPartIdx), int32(iBlockW), iref, &iMv)

						if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l0[ mbPartIdx ][ subMbPartIdx ][ compIdx ]
							return uiRet
						}
						cavlcAddMvd(&iMv, 0, iCode)
						if uiRet := BsGetSe(pBs, &iCode); uiRet != ERR_NONE { //mvd_l1[ mbPartIdx ][ subMbPartIdx ][ compIdx ]
							return uiRet
						}
						cavlcAddMvd(&iMv, 1, iCode)
						WELS_CHECK_SE_BOTH_WARNING(pCtx, iMv[1], iMinVmv, iMaxVmv, "vertical mv")
					} else {
						iMv = [2]int16{}
					}
					if common.IS_SUB_8x8(subMbType) { //MB_TYPE_8x8
						pDecMv[uiScan4Idx] = iMv
						pDecMv[uiScan4Idx+1] = iMv
						pDecMv[uiScan4Idx+4] = iMv
						pDecMv[uiScan4Idx+5] = iMv
						iMvArray[listIdx][uiCacheIdx] = iMv
						iMvArray[listIdx][uiCacheIdx+1] = iMv
						iMvArray[listIdx][uiCacheIdx+6] = iMv
						iMvArray[listIdx][uiCacheIdx+7] = iMv
					} else if common.IS_SUB_8x4(subMbType) {
						pDecMv[uiScan4Idx] = iMv
						pDecMv[uiScan4Idx+1] = iMv
						iMvArray[listIdx][uiCacheIdx] = iMv
						iMvArray[listIdx][uiCacheIdx+1] = iMv
					} else if common.IS_SUB_4x8(subMbType) {
						pDecMv[uiScan4Idx] = iMv
						pDecMv[uiScan4Idx+4] = iMv
						iMvArray[listIdx][uiCacheIdx] = iMv
						iMvArray[listIdx][uiCacheIdx+6] = iMv
					} else { //SUB_MB_TYPE_4x4 == uiSubMbType
						pDecMv[uiScan4Idx] = iMv
						iMvArray[listIdx][uiCacheIdx] = iMv
					}
				}
			}
		}
	}
	return ERR_NONE
}
