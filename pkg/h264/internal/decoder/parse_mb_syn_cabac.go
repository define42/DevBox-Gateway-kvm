// Port of codec/decoder/core/src/parse_mb_syn_cabac.cpp: cabac parse for
// syntax elements.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const IDX_UNUSED = -1

var g_kMaxPos = [...]int16{IDX_UNUSED, 15, 14, 15, 3, 14, 63, 3, 3, 14, 14}
var g_kMaxC2 = [...]int16{IDX_UNUSED, 4, 4, 4, 3, 4, 4, 3, 3, 4, 4}
var g_kBlockCat2CtxOffsetCBF = [...]int16{IDX_UNUSED, 0, 4, 8, 12, 16, 0, 12, 12, 16, 16}
var g_kBlockCat2CtxOffsetMap = [...]int16{IDX_UNUSED, 0, 15, 29, 44, 47, 0, 44, 44, 47, 47}
var g_kBlockCat2CtxOffsetLast = [...]int16{IDX_UNUSED, 0, 15, 29, 44, 47, 0, 44, 44, 47, 47}
var g_kBlockCat2CtxOffsetOne = [...]int16{IDX_UNUSED, 0, 10, 20, 30, 39, 0, 30, 30, 39, 39}
var g_kBlockCat2CtxOffsetAbs = [...]int16{IDX_UNUSED, 0, 10, 20, 30, 39, 0, 30, 30, 39, 39}

var g_kTopBlkInsideMb = [24]uint8{ //for index with z-order 0~23
	//  0   1 | 4  5      luma 8*8 block           pNonZeroCount[16+8]
	0, 0, 1, 1, //  2   3 | 6  7        0  |  1                  0   1   2   3
	0, 0, 1, 1, //---------------      ---------                 4   5   6   7
	1, 1, 1, 1, //  8   9 | 12 13       2  |  3                  8   9  10  11
	1, 1, 1, 1, // 10  11 | 14 15-----------------------------> 12  13  14  15
	0, 0, 1, 1, //----------------    chroma 8*8 block          16  17  18  19
	0, 0, 1, 1, // 16  17 | 20 21        0    1                 20  21  22  23
	// 18  19 | 22 23
}

var g_kLeftBlkInsideMb = [24]uint8{ //for index with z-order 0~23
	//  0   1 | 4  5      luma 8*8 block           pNonZeroCount[16+8]
	0, 1, 0, 1, //  2   3 | 6  7        0  |  1                  0   1   2   3
	1, 1, 1, 1, //---------------      ---------                 4   5   6   7
	0, 1, 0, 1, //  8   9 | 12 13       2  |  3                  8   9  10  11
	1, 1, 1, 1, // 10  11 | 14 15-----------------------------> 12  13  14  15
	0, 1, 0, 1, //----------------    chroma 8*8 block          16  17  18  19
	0, 1, 0, 1, // 16  17 | 20 21        0    1                 20  21  22  23
	// 18  19 | 22 23
}

// synSt32Mv is ST32 (dst, LD32 (src)) for one int16[2] motion vector.
func synSt32Mv(dst *[common.MV_A]int16, src []int16) {
	dst[0] = src[0]
	dst[1] = src[1]
}

// synSt64Mv is ST64 (dst[i], LD64 (src)): it writes src[0..3] into the two
// consecutive motion vectors dst[i] and dst[i+1].
func synSt64Mv(dst [][common.MV_A]int16, i int, src *[4]int16) {
	dst[i][0] = src[0]
	dst[i][1] = src[1]
	dst[i+1][0] = src[2]
	dst[i+1][1] = src[3]
}

func b2i32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

func DecodeCabacIntraMbType(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, ctx_base int) uint32 {
	var uiCode uint32
	var uiMbType uint32

	pCabacDecEngine := pCtx.pCabacDecEngine
	pBinCtx := pCtx.pCabacCtx[ctx_base:]

	if uiRet := uint32(DecodeBinCabac(pCabacDecEngine, &pBinCtx[0], &uiCode)); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode == 0 {
		return 0 /* I4x4 */
	}

	if uiRet := uint32(DecodeTerminateCabac(pCabacDecEngine, &uiCode)); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode != 0 {
		return 25 /* PCM */
	}
	uiMbType = 1 /* I16x16 */
	/* cbp_luma != 0 */
	if uiRet := uint32(DecodeBinCabac(pCabacDecEngine, &pBinCtx[1], &uiCode)); uiRet != ERR_NONE {
		return uiRet
	}
	uiMbType += 12 * uiCode

	if uiRet := uint32(DecodeBinCabac(pCabacDecEngine, &pBinCtx[2], &uiCode)); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode != 0 {
		if uiRet := uint32(DecodeBinCabac(pCabacDecEngine, &pBinCtx[2], &uiCode)); uiRet != ERR_NONE {
			return uiRet
		}
		uiMbType += 4 + 4*uiCode
	}
	if uiRet := uint32(DecodeBinCabac(pCabacDecEngine, &pBinCtx[3], &uiCode)); uiRet != ERR_NONE {
		return uiRet
	}
	uiMbType += 2 * uiCode
	if uiRet := uint32(DecodeBinCabac(pCabacDecEngine, &pBinCtx[3], &uiCode)); uiRet != ERR_NONE {
		return uiRet
	}
	uiMbType += 1 * uiCode
	return uiMbType
}

// void UpdateP16x8RefIdxCabac (PDqLayer pCurDqLayer, int8_t pRefIndex[LIST_A][30], int32_t iPartIdx,
// const int8_t iRef, const int8_t iListIdx)
func UpdateP16x8RefIdxCabac(pCurDqLayer *SDqLayer, pRefIndex *[common.LIST_A][30]int8, iPartIdx int32, iRef int8, iListIdx int8) {
	iMbXy := pCurDqLayer.iMbXyIndex
	iScan4Idx := int(g_kuiScan4[iPartIdx])
	iScan4Idx4 := 4 + iScan4Idx
	iCacheIdx := int(common.G_kuiCache30ScanIdx[iPartIdx])
	iCacheIdx6 := 6 + iCacheIdx
	//mb
	pMbRef := &pCurDqLayer.pDec.pRefIndex[iListIdx][iMbXy]
	for k := 0; k < 4; k++ {
		pMbRef[iScan4Idx+k] = iRef
		pMbRef[iScan4Idx4+k] = iRef
	}
	//cache
	for k := 0; k < 4; k++ {
		pRefIndex[iListIdx][iCacheIdx+k] = iRef
		pRefIndex[iListIdx][iCacheIdx6+k] = iRef
	}
}

// void UpdateP8x16RefIdxCabac (PDqLayer pCurDqLayer, int8_t pRefIndex[LIST_A][30], int32_t iPartIdx,
// const int8_t iRef, const int8_t iListIdx)
func UpdateP8x16RefIdxCabac(pCurDqLayer *SDqLayer, pRefIndex *[common.LIST_A][30]int8, iPartIdx int32, iRef int8, iListIdx int8) {
	iMbXy := pCurDqLayer.iMbXyIndex
	pMbRef := &pCurDqLayer.pDec.pRefIndex[iListIdx][iMbXy]
	for i := 0; i < 2; i, iPartIdx = i+1, iPartIdx+8 {
		iScan4Idx := int(g_kuiScan4[iPartIdx])
		iCacheIdx := int(common.G_kuiCache30ScanIdx[iPartIdx])
		iScan4Idx4 := 4 + iScan4Idx
		iCacheIdx6 := 6 + iCacheIdx
		//mb
		pMbRef[iScan4Idx], pMbRef[iScan4Idx+1] = iRef, iRef
		pMbRef[iScan4Idx4], pMbRef[iScan4Idx4+1] = iRef, iRef
		//cache
		pRefIndex[iListIdx][iCacheIdx], pRefIndex[iListIdx][iCacheIdx+1] = iRef, iRef
		pRefIndex[iListIdx][iCacheIdx6], pRefIndex[iListIdx][iCacheIdx6+1] = iRef, iRef
	}
}

// void UpdateP8x8RefIdxCabac (PDqLayer pCurDqLayer, int8_t pRefIndex[LIST_A][30], int32_t iPartIdx,
// const int8_t iRef, const int8_t iListIdx)
func UpdateP8x8RefIdxCabac(pCurDqLayer *SDqLayer, pRefIndex *[common.LIST_A][30]int8, iPartIdx int32, iRef int8, iListIdx int8) {
	iMbXy := pCurDqLayer.iMbXyIndex
	iScan4Idx := int(g_kuiScan4[iPartIdx])
	pMbRef := &pCurDqLayer.pDec.pRefIndex[iListIdx][iMbXy]
	pMbRef[iScan4Idx] = iRef
	pMbRef[iScan4Idx+1] = iRef
	pMbRef[iScan4Idx+4] = iRef
	pMbRef[iScan4Idx+5] = iRef
}

// void UpdateP8x8DirectCabac (PDqLayer pCurDqLayer, int32_t iPartIdx)
func UpdateP8x8DirectCabac(pCurDqLayer *SDqLayer, iPartIdx int32) {
	iMbXy := pCurDqLayer.iMbXyIndex
	iScan4Idx := int(g_kuiScan4[iPartIdx])
	pDirect := &pCurDqLayer.pDirect[iMbXy]
	pDirect[iScan4Idx] = 1
	pDirect[iScan4Idx+1] = 1
	pDirect[iScan4Idx+4] = 1
	pDirect[iScan4Idx+5] = 1
}

// void UpdateP16x16DirectCabac (PDqLayer pCurDqLayer)
func UpdateP16x16DirectCabac(pCurDqLayer *SDqLayer) {
	iMbXy := pCurDqLayer.iMbXyIndex
	pDirect := &pCurDqLayer.pDirect[iMbXy]
	for i := 0; i < 16; i += 4 {
		kuiScan4Idx := int(g_kuiScan4[i])
		kuiScan4IdxPlus4 := 4 + kuiScan4Idx
		pDirect[kuiScan4Idx], pDirect[kuiScan4Idx+1] = 1, 1
		pDirect[kuiScan4IdxPlus4], pDirect[kuiScan4IdxPlus4+1] = 1, 1
	}
}

// void UpdateP16x16MvdCabac (SDqLayer* pCurDqLayer, int16_t pMvd[2], const int8_t iListIdx)
func UpdateP16x16MvdCabac(pCurDqLayer *SDqLayer, pMvd *[2]int16, iListIdx int8) {
	pMvd32 := [4]int16{pMvd[0], pMvd[1], pMvd[0], pMvd[1]}
	iMbXy := pCurDqLayer.iMbXyIndex
	dst := pCurDqLayer.pMvd[iListIdx][iMbXy][:]
	for i := 0; i < 16; i += 2 {
		synSt64Mv(dst, i, &pMvd32)
	}
}

// void UpdateP16x8MvdCabac (SDqLayer* pCurDqLayer, int16_t pMvdCache[LIST_A][30][MV_A], int32_t
// iPartIdx, int16_t pMvd[2], const int8_t iListIdx)
func UpdateP16x8MvdCabac(pCurDqLayer *SDqLayer, pMvdCache *[common.LIST_A][30][common.MV_A]int16, iPartIdx int32, pMvd *[2]int16, iListIdx int8) {
	pMvd32 := [4]int16{pMvd[0], pMvd[1], pMvd[0], pMvd[1]}
	iMbXy := pCurDqLayer.iMbXyIndex
	dst := pCurDqLayer.pMvd[iListIdx][iMbXy][:]
	cache := pMvdCache[iListIdx][:]
	for i := 0; i < 2; i, iPartIdx = i+1, iPartIdx+4 {
		iScan4Idx := int(g_kuiScan4[iPartIdx])
		iScan4Idx4 := 4 + iScan4Idx
		iCacheIdx := int(common.G_kuiCache30ScanIdx[iPartIdx])
		iCacheIdx6 := 6 + iCacheIdx
		//mb
		synSt64Mv(dst, iScan4Idx, &pMvd32)
		synSt64Mv(dst, iScan4Idx4, &pMvd32)
		//cache
		synSt64Mv(cache, iCacheIdx, &pMvd32)
		synSt64Mv(cache, iCacheIdx6, &pMvd32)
	}
}

// void UpdateP8x16MvdCabac (SDqLayer* pCurDqLayer, int16_t pMvdCache[LIST_A][30][MV_A], int32_t
// iPartIdx, int16_t pMvd[2], const int8_t iListIdx)
func UpdateP8x16MvdCabac(pCurDqLayer *SDqLayer, pMvdCache *[common.LIST_A][30][common.MV_A]int16, iPartIdx int32, pMvd *[2]int16, iListIdx int8) {
	pMvd32 := [4]int16{pMvd[0], pMvd[1], pMvd[0], pMvd[1]}
	iMbXy := pCurDqLayer.iMbXyIndex
	dst := pCurDqLayer.pMvd[iListIdx][iMbXy][:]
	cache := pMvdCache[iListIdx][:]

	for i := 0; i < 2; i, iPartIdx = i+1, iPartIdx+8 {
		iScan4Idx := int(g_kuiScan4[iPartIdx])
		iCacheIdx := int(common.G_kuiCache30ScanIdx[iPartIdx])
		iScan4Idx4 := 4 + iScan4Idx
		iCacheIdx6 := 6 + iCacheIdx
		//mb
		synSt64Mv(dst, iScan4Idx, &pMvd32)
		synSt64Mv(dst, iScan4Idx4, &pMvd32)
		//cache
		synSt64Mv(cache, iCacheIdx, &pMvd32)
		synSt64Mv(cache, iCacheIdx6, &pMvd32)
	}
}

// int32_t ParseEndOfSliceCabac (PWelsDecoderContext pCtx, uint32_t& uiBinVal)
func ParseEndOfSliceCabac(pCtx *SWelsDecoderContext, uiBinVal *uint32) int32 {
	*uiBinVal = 0
	if uiRet := DecodeTerminateCabac(pCtx.pCabacDecEngine, uiBinVal); uiRet != ERR_NONE {
		return uiRet
	}
	return ERR_NONE
}

// int32_t ParseSkipFlagCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, uint32_t& uiSkip)
func ParseSkipFlagCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, uiSkip *uint32) int32 {
	*uiSkip = 0
	var iCtxInc int32 = NEW_CTX_OFFSET_SKIP
	iCtxInc += b2i32(pNeighAvail.iLeftAvail != 0 && !common.IS_SKIP(pNeighAvail.iLeftType)) +
		b2i32(pNeighAvail.iTopAvail != 0 && !common.IS_SKIP(pNeighAvail.iTopType))
	if common.B_SLICE == pCtx.eSliceType {
		iCtxInc += 13
	}
	pBinCtx := &pCtx.pCabacCtx[iCtxInc]
	if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, pBinCtx, uiSkip); uiRet != ERR_NONE {
		return uiRet
	}
	return ERR_NONE
}

// int32_t ParseMBTypeISliceCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, uint32_t&
// uiBinVal)
func ParseMBTypeISliceCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, uiBinVal *uint32) int32 {
	var uiCode uint32
	var iIdxA, iIdxB int32
	var iCtxInc int32
	*uiBinVal = 0
	pCabacDecEngine := pCtx.pCabacDecEngine
	pBinCtx := pCtx.pCabacCtx[NEW_CTX_OFFSET_MB_TYPE_I:] //I mode in I slice
	iIdxA = b2i32((pNeighAvail.iLeftAvail != 0) && (pNeighAvail.iLeftType != common.MB_TYPE_INTRA4x4 &&
		pNeighAvail.iLeftType != common.MB_TYPE_INTRA8x8))
	iIdxB = b2i32((pNeighAvail.iTopAvail != 0) && (pNeighAvail.iTopType != common.MB_TYPE_INTRA4x4 &&
		pNeighAvail.iTopType != common.MB_TYPE_INTRA8x8))
	iCtxInc = iIdxA + iIdxB
	if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[iCtxInc], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	*uiBinVal = uiCode
	if *uiBinVal != 0 { //I16x16
		if uiRet := DecodeTerminateCabac(pCabacDecEngine, &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		if uiCode == 1 {
			*uiBinVal = 25 //I_PCM
		} else {
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[3], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiBinVal = 1 + uiCode*12
			//decoding of uiCbp:0,1,2
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[4], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			if uiCode != 0 {
				if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[5], &uiCode); uiRet != ERR_NONE {
					return uiRet
				}
				*uiBinVal += 4
				if uiCode != 0 {
					*uiBinVal += 4
				}
			}
			//decoding of I pred-mode: 0,1,2,3
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[6], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiBinVal += uiCode << 1
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[7], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiBinVal += uiCode
		}
	}
	//I4x4
	return ERR_NONE
}

// int32_t ParseMBTypePSliceCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, uint32_t&
// uiMbType)
func ParseMBTypePSliceCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, uiMbType *uint32) int32 {
	var uiCode uint32
	*uiMbType = 0
	pCabacDecEngine := pCtx.pCabacDecEngine

	pBinCtx := pCtx.pCabacCtx[NEW_CTX_OFFSET_SKIP:]
	if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[3], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode != 0 {
		// Intra MB
		if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[6], &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		if uiCode != 0 { // Intra 16x16
			if uiRet := DecodeTerminateCabac(pCabacDecEngine, &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			if uiCode != 0 {
				*uiMbType = 30
				return ERR_NONE //MB_TYPE_INTRA_PCM;
			}

			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[7], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiMbType = 6 + uiCode*12

			//uiCbp: 0,1,2
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[8], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			if uiCode != 0 {
				*uiMbType += 4
				if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[8], &uiCode); uiRet != ERR_NONE {
					return uiRet
				}
				if uiCode != 0 {
					*uiMbType += 4
				}
			}

			//IPredMode: 0,1,2,3
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[9], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiMbType += uiCode << 1
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[9], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiMbType += uiCode
		} else {
			// Intra 4x4
			*uiMbType = 5
		}
	} else { // P MB
		if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[4], &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		if uiCode != 0 { //second bit
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[6], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			if uiCode != 0 {
				*uiMbType = 1
			} else {
				*uiMbType = 2
			}
		} else {
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[5], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			if uiCode != 0 {
				*uiMbType = 3
			} else {
				*uiMbType = 0
			}
		}
	}
	return ERR_NONE
}

// int32_t ParseMBTypeBSliceCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, uint32_t&
// uiMbType)
func ParseMBTypeBSliceCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, uiMbType *uint32) int32 {
	var uiCode uint32
	*uiMbType = 0
	var iIdxA, iIdxB int32
	var iCtxInc int32

	pCabacDecEngine := pCtx.pCabacDecEngine
	pBinCtx := pCtx.pCabacCtx[27:] //B slice

	iIdxA = b2i32((pNeighAvail.iLeftAvail != 0) && !common.IS_DIRECT(pNeighAvail.iLeftType))
	iIdxB = b2i32((pNeighAvail.iTopAvail != 0) && !common.IS_DIRECT(pNeighAvail.iTopType))

	iCtxInc = iIdxA + iIdxB
	if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[iCtxInc], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode == 0 {
		*uiMbType = 0 // Bi_Direct
	} else {
		if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[3], &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		if uiCode == 0 {
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[5], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiMbType = 1 + uiCode // 16x16 L0L1
		} else {
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[4], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiMbType = uiCode << 3
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[5], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiMbType |= uiCode << 2
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[5], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiMbType |= uiCode << 1
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[5], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiMbType |= uiCode
			if *uiMbType < 8 {
				*uiMbType += 3
				return ERR_NONE
			} else if *uiMbType == 13 {
				*uiMbType = DecodeCabacIntraMbType(pCtx, pNeighAvail, 32) + 23
				return ERR_NONE
			} else if *uiMbType == 14 {
				*uiMbType = 11 // Bi8x16
				return ERR_NONE
			} else if *uiMbType == 15 {
				*uiMbType = 22 // 8x8
				return ERR_NONE
			}
			*uiMbType <<= 1
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[5], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiMbType |= uiCode
			*uiMbType -= 4
		}
	}
	return ERR_NONE
}

// int32_t ParseTransformSize8x8FlagCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, bool&
// bTransformSize8x8Flag)
func ParseTransformSize8x8FlagCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, bTransformSize8x8Flag *bool) int32 {
	var uiCode uint32
	var iIdxA, iIdxB int32
	var iCtxInc int32
	pCabacDecEngine := pCtx.pCabacDecEngine
	pBinCtx := pCtx.pCabacCtx[NEW_CTX_OFFSET_TS_8x8_FLAG:]
	iIdxA = b2i32((pNeighAvail.iLeftAvail != 0) && (pCtx.pCurDqLayer.pTransformSize8x8Flag[pCtx.pCurDqLayer.iMbXyIndex-1]))
	iIdxB = b2i32((pNeighAvail.iTopAvail != 0) &&
		(pCtx.pCurDqLayer.pTransformSize8x8Flag[pCtx.pCurDqLayer.iMbXyIndex-pCtx.pCurDqLayer.iMbWidth]))
	iCtxInc = iIdxA + iIdxB
	if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[iCtxInc], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	*bTransformSize8x8Flag = uiCode != 0

	return ERR_NONE
}

// int32_t ParseSubMBTypeCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, uint32_t&
// uiSubMbType)
func ParseSubMBTypeCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, uiSubMbType *uint32) int32 {
	var uiCode uint32
	pCabacDecEngine := pCtx.pCabacDecEngine
	pBinCtx := pCtx.pCabacCtx[NEW_CTX_OFFSET_SUBMB_TYPE:]
	if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[0], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode != 0 {
		*uiSubMbType = 0
	} else {
		if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[1], &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		if uiCode != 0 {
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[2], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiSubMbType = 3 - uiCode
		} else {
			*uiSubMbType = 1
		}
	}
	return ERR_NONE
}

// int32_t ParseBSubMBTypeCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, uint32_t&
// uiSubMbType)
func ParseBSubMBTypeCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, uiSubMbType *uint32) int32 {
	var uiCode uint32
	pCabacDecEngine := pCtx.pCabacDecEngine
	pBinCtx := pCtx.pCabacCtx[NEW_CTX_OFFSET_B_SUBMB_TYPE:]
	if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[0], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode == 0 {
		*uiSubMbType = 0 /* B_Direct_8x8 */
		return ERR_NONE
	}
	if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[1], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode == 0 {
		if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[3], &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		*uiSubMbType = 1 + uiCode /* B_L0_8x8, B_L1_8x8 */
		return ERR_NONE
	}
	*uiSubMbType = 3
	if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[2], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode != 0 {
		if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[3], &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		if uiCode != 0 {
			if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[3], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiSubMbType = 11 + uiCode /* B_L1_4x4, B_Bi_4x4 */
			return ERR_NONE
		}
		*uiSubMbType += 4
	}
	if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[3], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	*uiSubMbType += 2 * uiCode
	if uiRet := DecodeBinCabac(pCabacDecEngine, &pBinCtx[3], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	*uiSubMbType += uiCode

	return ERR_NONE
}

// int32_t ParseIntraPredModeLumaCabac (PWelsDecoderContext pCtx, int32_t& iBinVal)
func ParseIntraPredModeLumaCabac(pCtx *SWelsDecoderContext, iBinVal *int32) int32 {
	var uiCode uint32
	*iBinVal = 0
	if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_IPR], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode == 1 {
		*iBinVal = -1
	} else {
		if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_IPR+1], &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		*iBinVal |= int32(uiCode)
		if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_IPR+1], &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		*iBinVal |= int32(uiCode << 1)
		if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_IPR+1], &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		*iBinVal |= int32(uiCode << 2)
	}
	return ERR_NONE
}

// int32_t ParseIntraPredModeChromaCabac (PWelsDecoderContext pCtx, uint8_t uiNeighAvail, int32_t&
// iBinVal)
func ParseIntraPredModeChromaCabac(pCtx *SWelsDecoderContext, uiNeighAvail uint8, iBinVal *int32) int32 {
	var uiCode uint32
	var iIdxA, iIdxB, iCtxInc int32
	pChromaPredMode := pCtx.pCurDqLayer.pChromaPredMode
	pMbType := pCtx.pCurDqLayer.pDec.pMbType
	iLeftAvail := int32(uiNeighAvail & 0x04)
	iTopAvail := int32(uiNeighAvail & 0x01)

	iMbXy := pCtx.pCurDqLayer.iMbXyIndex
	iMbXyTop := iMbXy - pCtx.pCurDqLayer.iMbWidth
	iMbXyLeft := iMbXy - 1

	*iBinVal = 0

	iIdxB = b2i32(iTopAvail != 0 && (pChromaPredMode[iMbXyTop] > 0 && pChromaPredMode[iMbXyTop] <= 3) &&
		pMbType[iMbXyTop] != common.MB_TYPE_INTRA_PCM)
	iIdxA = b2i32(iLeftAvail != 0 && (pChromaPredMode[iMbXyLeft] > 0 && pChromaPredMode[iMbXyLeft] <= 3) &&
		pMbType[iMbXyLeft] != common.MB_TYPE_INTRA_PCM)
	iCtxInc = iIdxA + iIdxB
	if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_CIPR+iCtxInc], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	*iBinVal = int32(uiCode)
	if *iBinVal != 0 {
		var iSym uint32
		if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_CIPR+3], &iSym); uiRet != ERR_NONE {
			return uiRet
		}
		if iSym == 0 {
			*iBinVal = int32(iSym + 1)
			return ERR_NONE
		}
		iSym = 0
		for {
			if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_CIPR+3], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			iSym++
			if !((uiCode != 0) && (iSym < 1)) {
				break
			}
		}

		if (uiCode != 0) && (iSym == 1) {
			iSym++
		}
		*iBinVal = int32(iSym + 1)
		return ERR_NONE
	}
	return ERR_NONE
}

// cabacCheckRefIdx implements the repeated "error ref_idx" block. It
// returns (iRef, iRet): when iRet != ERR_NONE the caller must return it.
// bCheckNull adds the RETURN_ERR_IF_NULL check of the B-slice variants.
func cabacCheckRefIdx(pCtx *SWelsDecoderContext, ppRefPic []*SPicture, iRef int8, iRefCount int32, bIsPending bool, bCheckNull bool) (int8, int32) {
	if (iRef < 0) || (int32(iRef) >= iRefCount) || (ppRefPic[iRef] == nil) { //error ref_idx
		pCtx.bMbRefConcealed = true
		if pCtx.pParam.EEcActiveIdc != api.ERROR_CON_DISABLE {
			iRef = 0
			pCtx.iErrorCode |= int32(api.DsBitstreamError)
			if bCheckNull && ppRefPic[iRef] == nil {
				return iRef, GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_REF_INDEX)
			}
		} else {
			return iRef, GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_REF_INDEX)
		}
	}
	pCtx.bMbRefConcealed = pCtx.bRPLRError || pCtx.bMbRefConcealed || !(ppRefPic[iRef] != nil &&
		(ppRefPic[iRef].bIsComplete || bIsPending))
	return iRef, ERR_NONE
}

// int32_t ParseInterPMotionInfoCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, uint8_t*
// pNonZeroCount, int16_t pMotionVector[LIST_A][30][MV_A], int16_t pMvdCache[LIST_A][30][MV_A], int8_t
// pRefIndex[LIST_A][30])
//
// pNonZeroCount: the MB non-zero-count cache (sub-slice).
func ParseInterPMotionInfoCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, pNonZeroCount []uint8, pMotionVector *[common.LIST_A][30][common.MV_A]int16, pMvdCache *[common.LIST_A][30][common.MV_A]int16, pRefIndex *[common.LIST_A][30]int8) int32 {
	pSlice := &pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	pCurDqLayer := pCtx.pCurDqLayer
	ppRefPic := pCtx.sRefPic.pRefList[common.LIST_0][:]
	var pRefCount [2]int32
	var i, j int32
	iMbXy := pCurDqLayer.iMbXyIndex
	var pMv [4]int16
	var pMvd [4]int16
	pMv2 := (*[2]int16)(pMv[:2])
	pMvd2 := (*[2]int16)(pMvd[:2])
	var iRef [2]int8
	var iPartIdx int32
	iMinVmv := pSliceHeader.pSps.pSLevelLimits.IMinVmv
	iMaxVmv := pSliceHeader.pSps.pSLevelLimits.IMaxVmv
	pRefCount[0] = pSliceHeader.uiRefCount[0]
	pRefCount[1] = pSliceHeader.uiRefCount[1]

	bIsPending := GetThreadCount(pCtx) > 1

	switch pCurDqLayer.pDec.pMbType[iMbXy] {
	case common.MB_TYPE_16x16:
		iPartIdx = 0
		if uiRet := ParseRefIdxCabac(pCtx, pNeighAvail, pNonZeroCount, pRefIndex, nil, common.LIST_0, iPartIdx, pRefCount[0], 0,
			&iRef[0]); uiRet != ERR_NONE {
			return uiRet
		}
		var iRet int32
		if iRef[0], iRet = cabacCheckRefIdx(pCtx, ppRefPic, iRef[0], pRefCount[0], bIsPending, false); iRet != ERR_NONE {
			return iRet
		}
		PredMv(pMotionVector, pRefIndex, common.LIST_0, 0, 4, iRef[0], pMv2)
		if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, common.LIST_0, 0, &pMvd[0]); uiRet != ERR_NONE {
			return uiRet
		}
		if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, common.LIST_0, 1, &pMvd[1]); uiRet != ERR_NONE {
			return uiRet
		}
		pMv[0] += pMvd[0]
		pMv[1] += pMvd[1]
		WELS_CHECK_SE_BOTH_WARNING(pCtx, pMv[1], iMinVmv, iMaxVmv, "vertical mv")
		UpdateP16x16MotionInfo(pCurDqLayer, common.LIST_0, iRef[0], pMv2)
		UpdateP16x16MvdCabac(pCurDqLayer, pMvd2, common.LIST_0)
	case common.MB_TYPE_16x8:
		for i = 0; i < 2; i++ {
			iPartIdx = i << 3
			if uiRet := ParseRefIdxCabac(pCtx, pNeighAvail, pNonZeroCount, pRefIndex, nil, common.LIST_0, iPartIdx, pRefCount[0], 0,
				&iRef[i]); uiRet != ERR_NONE {
				return uiRet
			}
			var iRet int32
			if iRef[i], iRet = cabacCheckRefIdx(pCtx, ppRefPic, iRef[i], pRefCount[0], bIsPending, false); iRet != ERR_NONE {
				return iRet
			}
			UpdateP16x8RefIdxCabac(pCurDqLayer, pRefIndex, iPartIdx, iRef[i], common.LIST_0)
		}
		for i = 0; i < 2; i++ {
			iPartIdx = i << 3
			PredInter16x8Mv(pMotionVector, pRefIndex, common.LIST_0, iPartIdx, iRef[i], pMv2)
			if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, common.LIST_0, 0, &pMvd[0]); uiRet != ERR_NONE {
				return uiRet
			}
			if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, common.LIST_0, 1, &pMvd[1]); uiRet != ERR_NONE {
				return uiRet
			}
			pMv[0] += pMvd[0]
			pMv[1] += pMvd[1]
			WELS_CHECK_SE_BOTH_WARNING(pCtx, pMv[1], iMinVmv, iMaxVmv, "vertical mv")
			UpdateP16x8MotionInfo(pCurDqLayer, pMotionVector, pRefIndex, common.LIST_0, iPartIdx, iRef[i], pMv2)
			UpdateP16x8MvdCabac(pCurDqLayer, pMvdCache, iPartIdx, pMvd2, common.LIST_0)
		}
	case common.MB_TYPE_8x16:
		for i = 0; i < 2; i++ {
			iPartIdx = i << 2
			if uiRet := ParseRefIdxCabac(pCtx, pNeighAvail, pNonZeroCount, pRefIndex, nil, common.LIST_0, iPartIdx, pRefCount[0], 0,
				&iRef[i]); uiRet != ERR_NONE {
				return uiRet
			}
			var iRet int32
			if iRef[i], iRet = cabacCheckRefIdx(pCtx, ppRefPic, iRef[i], pRefCount[0], bIsPending, false); iRet != ERR_NONE {
				return iRet
			}
			UpdateP8x16RefIdxCabac(pCurDqLayer, pRefIndex, iPartIdx, iRef[i], common.LIST_0)
		}
		for i = 0; i < 2; i++ {
			iPartIdx = i << 2
			PredInter8x16Mv(pMotionVector, pRefIndex, common.LIST_0, i<<2, iRef[i], pMv2 /*&mv[0], &mv[1]*/)

			if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, common.LIST_0, 0, &pMvd[0]); uiRet != ERR_NONE {
				return uiRet
			}
			if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, common.LIST_0, 1, &pMvd[1]); uiRet != ERR_NONE {
				return uiRet
			}
			pMv[0] += pMvd[0]
			pMv[1] += pMvd[1]
			WELS_CHECK_SE_BOTH_WARNING(pCtx, pMv[1], iMinVmv, iMaxVmv, "vertical mv")
			UpdateP8x16MotionInfo(pCurDqLayer, pMotionVector, pRefIndex, common.LIST_0, iPartIdx, iRef[i], pMv2)
			UpdateP8x16MvdCabac(pCurDqLayer, pMvdCache, iPartIdx, pMvd2, common.LIST_0)
		}
	case common.MB_TYPE_8x8, common.MB_TYPE_8x8_REF0:
		var pRefIdx [4]int8
		var pSubPartCount, pPartW [4]int8
		var uiSubMbType uint32
		//sub_mb_type, partition
		for i = 0; i < 4; i++ {
			if uiRet := ParseSubMBTypeCabac(pCtx, pNeighAvail, &uiSubMbType); uiRet != ERR_NONE {
				return uiRet
			}
			if uiSubMbType >= 4 { //invalid sub_mb_type
				return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_SUB_MB_TYPE)
			}
			pCurDqLayer.pSubMbType[iMbXy][i] = g_ksInterPSubMbTypeInfo[uiSubMbType].iType
			pSubPartCount[i] = g_ksInterPSubMbTypeInfo[uiSubMbType].iPartCount
			pPartW[i] = g_ksInterPSubMbTypeInfo[uiSubMbType].iPartWidth

			// Need modification when B picture add in, reference to 7.3.5
			pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] && (uiSubMbType == 0)
		}

		for i = 0; i < 4; i++ {
			iIdx8 := int16(i << 2)
			if uiRet := ParseRefIdxCabac(pCtx, pNeighAvail, pNonZeroCount, pRefIndex, nil, common.LIST_0, int32(iIdx8), pRefCount[0], 1,
				&pRefIdx[i]); uiRet != ERR_NONE {
				return uiRet
			}
			var iRet int32
			if pRefIdx[i], iRet = cabacCheckRefIdx(pCtx, ppRefPic, pRefIdx[i], pRefCount[0], bIsPending, false); iRet != ERR_NONE {
				return iRet
			}
			UpdateP8x8RefIdxCabac(pCurDqLayer, pRefIndex, int32(iIdx8), pRefIdx[i], common.LIST_0)
		}
		//mv
		for i = 0; i < 4; i++ {
			iPartCount := pSubPartCount[i]
			uiSubMbType = pCurDqLayer.pSubMbType[iMbXy][i]
			var iPartIdx int16
			iBlockW := int16(pPartW[i])
			var iScan4Idx, iCacheIdx int
			iCacheIdx = int(common.G_kuiCache30ScanIdx[i<<2])
			pRefIndex[0][iCacheIdx] = pRefIdx[i]
			pRefIndex[0][iCacheIdx+1] = pRefIdx[i]
			pRefIndex[0][iCacheIdx+6] = pRefIdx[i]
			pRefIndex[0][iCacheIdx+7] = pRefIdx[i]

			for j = 0; j < int32(iPartCount); j++ {
				iPartIdx = int16(i<<2) + int16(j)*iBlockW
				iScan4Idx = int(g_kuiScan4[iPartIdx])
				iCacheIdx = int(common.G_kuiCache30ScanIdx[iPartIdx])
				PredMv(pMotionVector, pRefIndex, common.LIST_0, int32(iPartIdx), int32(iBlockW), pRefIdx[i], pMv2)
				if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, int32(iPartIdx), common.LIST_0, 0, &pMvd[0]); uiRet != ERR_NONE {
					return uiRet
				}
				if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, int32(iPartIdx), common.LIST_0, 1, &pMvd[1]); uiRet != ERR_NONE {
					return uiRet
				}
				pMv[0] += pMvd[0]
				pMv[1] += pMvd[1]
				WELS_CHECK_SE_BOTH_WARNING(pCtx, pMv[1], iMinVmv, iMaxVmv, "vertical mv")
				pDecMv := pCurDqLayer.pDec.pMv[0][iMbXy][:]
				pMbMvd := pCurDqLayer.pMvd[0][iMbXy][:]
				if common.SUB_MB_TYPE_8x8 == uiSubMbType {
					pMv[2], pMv[3] = pMv[0], pMv[1]
					pMvd[2], pMvd[3] = pMvd[0], pMvd[1]
					synSt64Mv(pDecMv, iScan4Idx, &pMv)
					synSt64Mv(pDecMv, iScan4Idx+4, &pMv)
					synSt64Mv(pMbMvd, iScan4Idx, &pMvd)
					synSt64Mv(pMbMvd, iScan4Idx+4, &pMvd)
					synSt64Mv(pMotionVector[0][:], iCacheIdx, &pMv)
					synSt64Mv(pMotionVector[0][:], iCacheIdx+6, &pMv)
					synSt64Mv(pMvdCache[0][:], iCacheIdx, &pMvd)
					synSt64Mv(pMvdCache[0][:], iCacheIdx+6, &pMvd)
				} else if common.SUB_MB_TYPE_8x4 == uiSubMbType {
					pMv[2], pMv[3] = pMv[0], pMv[1]
					pMvd[2], pMvd[3] = pMvd[0], pMvd[1]
					synSt64Mv(pDecMv, iScan4Idx, &pMv)
					synSt64Mv(pMbMvd, iScan4Idx, &pMvd)
					synSt64Mv(pMotionVector[0][:], iCacheIdx, &pMv)
					synSt64Mv(pMvdCache[0][:], iCacheIdx, &pMvd)
				} else if common.SUB_MB_TYPE_4x8 == uiSubMbType {
					synSt32Mv(&pDecMv[iScan4Idx], pMv[:])
					synSt32Mv(&pDecMv[iScan4Idx+4], pMv[:])
					synSt32Mv(&pMbMvd[iScan4Idx], pMvd[:])
					synSt32Mv(&pMbMvd[iScan4Idx+4], pMvd[:])
					synSt32Mv(&pMotionVector[0][iCacheIdx], pMv[:])
					synSt32Mv(&pMotionVector[0][iCacheIdx+6], pMv[:])
					synSt32Mv(&pMvdCache[0][iCacheIdx], pMvd[:])
					synSt32Mv(&pMvdCache[0][iCacheIdx+6], pMvd[:])
				} else { //SUB_MB_TYPE_4x4
					synSt32Mv(&pDecMv[iScan4Idx], pMv[:])
					synSt32Mv(&pMbMvd[iScan4Idx], pMvd[:])
					synSt32Mv(&pMotionVector[0][iCacheIdx], pMv[:])
					synSt32Mv(&pMvdCache[0][iCacheIdx], pMvd[:])
				}
			}
		}
	default:
	}
	return ERR_NONE
}

// int32_t ParseInterBMotionInfoCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, uint8_t*
// pNonZeroCount, int16_t pMotionVector[LIST_A][30][MV_A], int16_t pMvdCache[LIST_A][30][MV_A], int8_t
// pRefIndex[LIST_A][30], int8_t pDirect[30])
//
// pNonZeroCount: the MB non-zero-count cache (sub-slice).
func ParseInterBMotionInfoCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, pNonZeroCount []uint8, pMotionVector *[common.LIST_A][30][common.MV_A]int16, pMvdCache *[common.LIST_A][30][common.MV_A]int16, pRefIndex *[common.LIST_A][30]int8, pDirect *[30]int8) int32 {
	pSlice := &pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	pCurDqLayer := pCtx.pCurDqLayer
	var pRefCount [common.LIST_A]int32
	iMbXy := pCurDqLayer.iMbXyIndex
	var pMv [4]int16
	var pMvd [4]int16
	pMv2 := (*[2]int16)(pMv[:2])
	pMvd2 := (*[2]int16)(pMvd[:2])
	var iRef [common.LIST_A]int8
	var iPartIdx int32
	iMinVmv := pSliceHeader.pSps.pSLevelLimits.IMinVmv
	iMaxVmv := pSliceHeader.pSps.pSLevelLimits.IMaxVmv
	pRefCount[0] = pSliceHeader.uiRefCount[0]
	pRefCount[1] = pSliceHeader.uiRefCount[1]

	mbType := MbType(pCurDqLayer.pDec.pMbType[iMbXy])

	bIsPending := GetThreadCount(pCtx) > 1

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
		iPartIdx = 0
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			iRef[listIdx] = REF_NOT_IN_LIST
			if common.IS_DIR(mbType, 0, listIdx) {
				if uiRet := ParseRefIdxCabac(pCtx, pNeighAvail, pNonZeroCount, pRefIndex, pDirect, listIdx, iPartIdx,
					pRefCount[listIdx], 0, &iRef[listIdx]); uiRet != ERR_NONE {
					return uiRet
				}
				var iRet int32
				if iRef[listIdx], iRet = cabacCheckRefIdx(pCtx, pCtx.sRefPic.pRefList[listIdx][:], iRef[listIdx], pRefCount[listIdx],
					bIsPending, true); iRet != ERR_NONE {
					return iRet
				}
			}
		}
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			if common.IS_DIR(mbType, 0, listIdx) {
				PredMv(pMotionVector, pRefIndex, listIdx, 0, 4, iRef[listIdx], pMv2)
				if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, int8(listIdx), 0, &pMvd[0]); uiRet != ERR_NONE {
					return uiRet
				}
				if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, int8(listIdx), 1, &pMvd[1]); uiRet != ERR_NONE {
					return uiRet
				}
				pMv[0] += pMvd[0]
				pMv[1] += pMvd[1]
				WELS_CHECK_SE_BOTH_WARNING(pCtx, pMv[1], iMinVmv, iMaxVmv, "vertical mv")
			} else {
				pMv[0], pMv[1] = 0, 0
				pMvd[0], pMvd[1] = 0, 0
			}
			UpdateP16x16MotionInfo(pCurDqLayer, listIdx, iRef[listIdx], pMv2)
			UpdateP16x16MvdCabac(pCurDqLayer, pMvd2, int8(listIdx))
		}
	} else if common.IS_INTER_16x8(mbType) {
		ref_idx_list := [common.LIST_A][2]int8{{REF_NOT_IN_LIST, REF_NOT_IN_LIST}, {REF_NOT_IN_LIST, REF_NOT_IN_LIST}}
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			for i := int32(0); i < 2; i++ {
				iPartIdx = i << 3
				var ref_idx int8 = REF_NOT_IN_LIST
				if common.IS_DIR(mbType, i, listIdx) {
					if uiRet := ParseRefIdxCabac(pCtx, pNeighAvail, pNonZeroCount, pRefIndex, pDirect, listIdx, iPartIdx,
						pRefCount[listIdx], 0, &ref_idx); uiRet != ERR_NONE {
						return uiRet
					}
					var iRet int32
					if ref_idx, iRet = cabacCheckRefIdx(pCtx, pCtx.sRefPic.pRefList[listIdx][:], ref_idx, pRefCount[listIdx],
						bIsPending, true); iRet != ERR_NONE {
						return iRet
					}
				}
				UpdateP16x8RefIdxCabac(pCurDqLayer, pRefIndex, iPartIdx, ref_idx, int8(listIdx))
				ref_idx_list[listIdx][i] = ref_idx
			}
		}
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			for i := int32(0); i < 2; i++ {
				iPartIdx = i << 3
				ref_idx := ref_idx_list[listIdx][i]
				if common.IS_DIR(mbType, i, listIdx) {
					PredInter16x8Mv(pMotionVector, pRefIndex, listIdx, iPartIdx, ref_idx, pMv2)
					if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, int8(listIdx), 0, &pMvd[0]); uiRet != ERR_NONE {
						return uiRet
					}
					if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, int8(listIdx), 1, &pMvd[1]); uiRet != ERR_NONE {
						return uiRet
					}
					pMv[0] += pMvd[0]
					pMv[1] += pMvd[1]
					WELS_CHECK_SE_BOTH_WARNING(pCtx, pMv[1], iMinVmv, iMaxVmv, "vertical mv")
				} else {
					pMv[0], pMv[1] = 0, 0
					pMvd[0], pMvd[1] = 0, 0
				}
				UpdateP16x8MotionInfo(pCurDqLayer, pMotionVector, pRefIndex, listIdx, iPartIdx, ref_idx, pMv2)
				UpdateP16x8MvdCabac(pCurDqLayer, pMvdCache, iPartIdx, pMvd2, int8(listIdx))
			}
		}
	} else if common.IS_INTER_8x16(mbType) {
		ref_idx_list := [common.LIST_A][2]int8{{REF_NOT_IN_LIST, REF_NOT_IN_LIST}, {REF_NOT_IN_LIST, REF_NOT_IN_LIST}}
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			for i := int32(0); i < 2; i++ {
				iPartIdx = i << 2
				var ref_idx int8 = REF_NOT_IN_LIST
				if common.IS_DIR(mbType, i, listIdx) {
					if uiRet := ParseRefIdxCabac(pCtx, pNeighAvail, pNonZeroCount, pRefIndex, pDirect, listIdx, iPartIdx,
						pRefCount[listIdx], 0, &ref_idx); uiRet != ERR_NONE {
						return uiRet
					}
					var iRet int32
					if ref_idx, iRet = cabacCheckRefIdx(pCtx, pCtx.sRefPic.pRefList[listIdx][:], ref_idx, pRefCount[listIdx],
						bIsPending, true); iRet != ERR_NONE {
						return iRet
					}
				}
				UpdateP8x16RefIdxCabac(pCurDqLayer, pRefIndex, iPartIdx, ref_idx, int8(listIdx))
				ref_idx_list[listIdx][i] = ref_idx
			}
		}
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			for i := int32(0); i < 2; i++ {
				iPartIdx = i << 2
				ref_idx := ref_idx_list[listIdx][i]
				if common.IS_DIR(mbType, i, listIdx) {
					PredInter8x16Mv(pMotionVector, pRefIndex, listIdx, iPartIdx, ref_idx, pMv2)
					if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, int8(listIdx), 0, &pMvd[0]); uiRet != ERR_NONE {
						return uiRet
					}
					if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, int8(listIdx), 1, &pMvd[1]); uiRet != ERR_NONE {
						return uiRet
					}
					pMv[0] += pMvd[0]
					pMv[1] += pMvd[1]
					WELS_CHECK_SE_BOTH_WARNING(pCtx, pMv[1], iMinVmv, iMaxVmv, "vertical mv")
				} else {
					pMv[0], pMv[1] = 0, 0
					pMvd[0], pMvd[1] = 0, 0
				}
				UpdateP8x16MotionInfo(pCurDqLayer, pMotionVector, pRefIndex, listIdx, iPartIdx, ref_idx, pMv2)
				UpdateP8x16MvdCabac(pCurDqLayer, pMvdCache, iPartIdx, pMvd2, int8(listIdx))
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
		for i := 0; i < 4; i++ {
			if uiRet := ParseBSubMBTypeCabac(pCtx, pNeighAvail, &uiSubMbType); uiRet != ERR_NONE {
				return uiRet
			}
			if uiSubMbType >= 13 { //invalid sub_mb_type
				return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_SUB_MB_TYPE)
			}
			//      pCurDqLayer->pSubMbType[iMbXy][i] = g_ksInterBSubMbTypeInfo[uiSubMbType].iType;
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
		for i := 0; i < 4; i++ { //Direct 8x8 Ref and mv
			iIdx8 := int16(i << 2)
			if common.IS_DIRECT(pCurDqLayer.pSubMbType[iMbXy][i]) {
				if pSliceHeader.iDirectSpatialMvPredFlag != 0 {
					FillSpatialDirect8x8Mv(pCurDqLayer, iIdx8, pSubPartCount[i], pPartW[i], directSubMbType, bIsLongRef, &pMvDirect, &iRef,
						pMotionVector, pMvdCache)
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
					UpdateP8x8RefCacheIdxCabac(pRefIndex, iIdx8, common.LIST_0, iRef[common.LIST_0])
					UpdateP8x8RefCacheIdxCabac(pRefIndex, iIdx8, common.LIST_1, iRef[common.LIST_1])
					FillTemporalDirect8x8Mv(pCurDqLayer, iIdx8, pSubPartCount[i], pPartW[i], directSubMbType, &iRef, mvColoc, pMotionVector,
						pMvdCache)
				}
			}
		}
		//ref no-direct
		// C: int8_t ref_idx_list[LIST_A][4] = { {REF_NOT_IN_LIST, REF_NOT_IN_LIST}, {...} }; the
		// remaining elements are zero-initialized.
		ref_idx_list := [common.LIST_A][4]int8{{REF_NOT_IN_LIST, REF_NOT_IN_LIST}, {REF_NOT_IN_LIST, REF_NOT_IN_LIST}}
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
					UpdateP8x8DirectCabac(pCurDqLayer, int32(iIdx8))
				} else {
					if common.IS_DIR(subMbType, 0, listIdx) {
						if uiRet := ParseRefIdxCabac(pCtx, pNeighAvail, pNonZeroCount, pRefIndex, pDirect, listIdx, int32(iIdx8),
							pRefCount[listIdx], 1, &iref); uiRet != ERR_NONE {
							return uiRet
						}
						var iRet int32
						if iref, iRet = cabacCheckRefIdx(pCtx, pCtx.sRefPic.pRefList[listIdx][:], iref, pRefCount[listIdx],
							bIsPending, true); iRet != ERR_NONE {
							return iRet
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
				iIdx8 := int16(i << 2)

				subMbType := pCurDqLayer.pSubMbType[iMbXy][i]
				if common.IS_DIRECT(subMbType) && pSliceHeader.iDirectSpatialMvPredFlag == 0 {
					continue
				}

				iref := ref_idx_list[listIdx][i]
				UpdateP8x8RefCacheIdxCabac(pRefIndex, iIdx8, listIdx, iref)

				if common.IS_DIRECT(subMbType) {
					continue
				}

				is_dir := common.IS_DIR(subMbType, 0, listIdx)
				iPartCount := pSubPartCount[i]
				iBlockW := int16(pPartW[i])
				var iScan4Idx, iCacheIdx int
				pDecMv := pCurDqLayer.pDec.pMv[listIdx][iMbXy][:]
				pMbMvd := pCurDqLayer.pMvd[listIdx][iMbXy][:]
				for j := int32(0); j < int32(iPartCount); j++ {
					iPartIdx = int32(i<<2) + int32(int16(j)*iBlockW)
					iScan4Idx = int(g_kuiScan4[iPartIdx])
					iCacheIdx = int(common.G_kuiCache30ScanIdx[iPartIdx])
					if is_dir {
						PredMv(pMotionVector, pRefIndex, listIdx, iPartIdx, int32(iBlockW), iref, pMv2)
						if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, int8(listIdx), 0, &pMvd[0]); uiRet != ERR_NONE {
							return uiRet
						}
						if uiRet := ParseMvdInfoCabac(pCtx, pNeighAvail, pRefIndex, pMvdCache, iPartIdx, int8(listIdx), 1, &pMvd[1]); uiRet != ERR_NONE {
							return uiRet
						}
						pMv[0] += pMvd[0]
						pMv[1] += pMvd[1]
						WELS_CHECK_SE_BOTH_WARNING(pCtx, pMv[1], iMinVmv, iMaxVmv, "vertical mv")
					} else {
						pMv[0], pMv[1] = 0, 0
						pMvd[0], pMvd[1] = 0, 0
					}
					if common.IS_SUB_8x8(subMbType) { //MB_TYPE_8x8
						pMv[2], pMv[3] = pMv[0], pMv[1]
						pMvd[2], pMvd[3] = pMvd[0], pMvd[1]
						synSt64Mv(pDecMv, iScan4Idx, &pMv)
						synSt64Mv(pDecMv, iScan4Idx+4, &pMv)
						synSt64Mv(pMbMvd, iScan4Idx, &pMvd)
						synSt64Mv(pMbMvd, iScan4Idx+4, &pMvd)
						synSt64Mv(pMotionVector[listIdx][:], iCacheIdx, &pMv)
						synSt64Mv(pMotionVector[listIdx][:], iCacheIdx+6, &pMv)
						synSt64Mv(pMvdCache[listIdx][:], iCacheIdx, &pMvd)
						synSt64Mv(pMvdCache[listIdx][:], iCacheIdx+6, &pMvd)
					} else if common.IS_SUB_4x4(subMbType) { //MB_TYPE_4x4
						synSt32Mv(&pDecMv[iScan4Idx], pMv[:])
						synSt32Mv(&pMbMvd[iScan4Idx], pMvd[:])
						synSt32Mv(&pMotionVector[listIdx][iCacheIdx], pMv[:])
						synSt32Mv(&pMvdCache[listIdx][iCacheIdx], pMvd[:])
					} else if common.IS_SUB_4x8(subMbType) { //MB_TYPE_4x8 5, 7, 9
						synSt32Mv(&pDecMv[iScan4Idx], pMv[:])
						synSt32Mv(&pDecMv[iScan4Idx+4], pMv[:])
						synSt32Mv(&pMbMvd[iScan4Idx], pMvd[:])
						synSt32Mv(&pMbMvd[iScan4Idx+4], pMvd[:])
						synSt32Mv(&pMotionVector[listIdx][iCacheIdx], pMv[:])
						synSt32Mv(&pMotionVector[listIdx][iCacheIdx+6], pMv[:])
						synSt32Mv(&pMvdCache[listIdx][iCacheIdx], pMvd[:])
						synSt32Mv(&pMvdCache[listIdx][iCacheIdx+6], pMvd[:])
					} else { //MB_TYPE_8x4 4, 6, 8
						pMv[2], pMv[3] = pMv[0], pMv[1]
						pMvd[2], pMvd[3] = pMvd[0], pMvd[1]
						synSt64Mv(pDecMv, iScan4Idx, &pMv)
						synSt64Mv(pMbMvd, iScan4Idx, &pMvd)
						synSt64Mv(pMotionVector[listIdx][:], iCacheIdx, &pMv)
						synSt64Mv(pMvdCache[listIdx][:], iCacheIdx, &pMvd)
					}
				}
			}
		}
	}
	return ERR_NONE
}

// int32_t ParseRefIdxCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, uint8_t* nzc,
// int8_t ref_idx[LIST_A][30], int8_t direct[30], int32_t iListIdx, int32_t iZOrderIdx, int32_t
// iActiveRefNum, int32_t b8mode, int8_t& iRefIdxVal)
//
// direct may be nil.
func ParseRefIdxCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, nzc []uint8, ref_idx *[common.LIST_A][30]int8, direct *[30]int8, iListIdx int32, iZOrderIdx int32, iActiveRefNum int32, b8mode int32, iRefIdxVal *int8) int32 {
	if iActiveRefNum == 1 {
		*iRefIdxVal = 0
		return ERR_NONE
	}
	var uiCode uint32
	var iIdxA, iIdxB int32
	var iCtxInc int32
	pRefIdxInMB := &pCtx.pCurDqLayer.pDec.pRefIndex[iListIdx][pCtx.pCurDqLayer.iMbXyIndex]
	// C computes pDirect unconditionally; it is only dereferenced for B slices.
	var pDirect *[common.MB_BLOCK4x4_NUM]int8
	if pCtx.eSliceType == common.B_SLICE {
		pDirect = &pCtx.pCurDqLayer.pDirect[pCtx.pCurDqLayer.iMbXyIndex]
	}
	kCache := int(common.G_kuiCache30ScanIdx[iZOrderIdx])
	kScan4 := int(g_kuiScan4[iZOrderIdx])
	if iZOrderIdx == 0 {
		iIdxB = b2i32(pNeighAvail.iTopAvail != 0 && pNeighAvail.iTopType != common.MB_TYPE_INTRA_PCM &&
			ref_idx[iListIdx][kCache-6] > 0)
		iIdxA = b2i32(pNeighAvail.iLeftAvail != 0 && pNeighAvail.iLeftType != common.MB_TYPE_INTRA_PCM &&
			ref_idx[iListIdx][kCache-1] > 0)
		if pCtx.eSliceType == common.B_SLICE {
			if iIdxB > 0 && direct[kCache-6] == 0 {
				iCtxInc += 2
			}
			if iIdxA > 0 && direct[kCache-1] == 0 {
				iCtxInc++
			}
		}
	} else if iZOrderIdx == 4 {
		iIdxB = b2i32(pNeighAvail.iTopAvail != 0 && pNeighAvail.iTopType != common.MB_TYPE_INTRA_PCM &&
			ref_idx[iListIdx][kCache-6] > 0)
		iIdxA = b2i32(pRefIdxInMB[kScan4-1] > 0)
		if pCtx.eSliceType == common.B_SLICE {
			if iIdxB > 0 && direct[kCache-6] == 0 {
				iCtxInc += 2
			}
			if iIdxA > 0 && pDirect[kScan4-1] == 0 {
				iCtxInc++
			}
		}
	} else if iZOrderIdx == 8 {

		iIdxB = b2i32(pRefIdxInMB[kScan4-4] > 0)
		iIdxA = b2i32(pNeighAvail.iLeftAvail != 0 && pNeighAvail.iLeftType != common.MB_TYPE_INTRA_PCM &&
			ref_idx[iListIdx][kCache-1] > 0)
		if pCtx.eSliceType == common.B_SLICE {
			if iIdxB > 0 && pDirect[kScan4-4] == 0 {
				iCtxInc += 2
			}
			if iIdxA > 0 && direct[kCache-1] == 0 {
				iCtxInc++
			}
		}
	} else {
		iIdxB = b2i32(pRefIdxInMB[kScan4-4] > 0)
		iIdxA = b2i32(pRefIdxInMB[kScan4-1] > 0)
		if pCtx.eSliceType == common.B_SLICE {
			if iIdxB > 0 && pDirect[kScan4-4] == 0 {
				iCtxInc += 2
			}
			if iIdxA > 0 && pDirect[kScan4-1] == 0 {
				iCtxInc++
			}
		}
	}
	if pCtx.eSliceType != common.B_SLICE {
		iCtxInc = iIdxA + (iIdxB << 1)
	}

	if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_REF_NO+iCtxInc], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode != 0 {
		if uiRet := DecodeUnaryBinCabac(pCtx.pCabacDecEngine, pCtx.pCabacCtx[NEW_CTX_OFFSET_REF_NO+4:], 1, &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		uiCode++
	}
	*iRefIdxVal = int8(uiCode)
	return ERR_NONE
}

// int32_t ParseMvdInfoCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, int8_t
// pRefIndex[LIST_A][30], int16_t pMvdCache[LIST_A][30][2], int32_t index, int8_t iListIdx, int8_t
// iMvComp, int16_t& iMvdVal)
func ParseMvdInfoCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, pRefIndex *[common.LIST_A][30]int8, pMvdCache *[common.LIST_A][30][2]int16, index int32, iListIdx int8, iMvComp int8, iMvdVal *int16) int32 {
	var uiCode uint32
	var iIdxA int32
	//int32_t sym;
	pBinCtx := pCtx.pCabacCtx[NEW_CTX_OFFSET_MVD+int32(iMvComp)*CTX_NUM_MVD:]
	*iMvdVal = 0

	kCache := int(common.G_kuiCache30ScanIdx[index])
	if pRefIndex[iListIdx][kCache-6] >= 0 {
		iIdxA = common.WELS_ABS(int32(pMvdCache[iListIdx][kCache-6][iMvComp]))
	}
	if pRefIndex[iListIdx][kCache-1] >= 0 {
		iIdxA += common.WELS_ABS(int32(pMvdCache[iListIdx][kCache-1][iMvComp]))
	}

	var iCtxInc int32
	if iIdxA >= 3 {
		iCtxInc = 1 + b2i32(iIdxA > 32)
	}

	if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pBinCtx[iCtxInc], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode != 0 {
		if uiRet := DecodeUEGMvCabac(pCtx.pCabacDecEngine, pBinCtx[3:], 3, &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		*iMvdVal = int16(uiCode + 1)
		if uiRet := DecodeBypassCabac(pCtx.pCabacDecEngine, &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		if uiCode != 0 {
			*iMvdVal = -*iMvdVal
		}
	} else {
		*iMvdVal = 0
	}
	return ERR_NONE
}

// int32_t ParseCbpInfoCabac (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, uint32_t& uiCbp)
func ParseCbpInfoCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, uiCbp *uint32) int32 {
	var iIdxA, iIdxB int32
	var pALeftMb, pBTopMb [2]int32
	*uiCbp = 0
	var pCbpBit [6]uint32
	var iCtxInc int32

	iTopCbp := int32(pNeighAvail.iTopCbp)
	iLeftCbp := int32(pNeighAvail.iLeftCbp)

	//Luma: bit by bit for 4 8x8 blocks in z-order
	pBTopMb[0] = b2i32(pNeighAvail.iTopAvail != 0 && pNeighAvail.iTopType != common.MB_TYPE_INTRA_PCM &&
		((iTopCbp & (1 << 2)) == 0))
	pBTopMb[1] = b2i32(pNeighAvail.iTopAvail != 0 && pNeighAvail.iTopType != common.MB_TYPE_INTRA_PCM &&
		((iTopCbp & (1 << 3)) == 0))
	pALeftMb[0] = b2i32(pNeighAvail.iLeftAvail != 0 && pNeighAvail.iLeftType != common.MB_TYPE_INTRA_PCM &&
		((iLeftCbp & (1 << 1)) == 0))
	pALeftMb[1] = b2i32(pNeighAvail.iLeftAvail != 0 && pNeighAvail.iLeftType != common.MB_TYPE_INTRA_PCM &&
		((iLeftCbp & (1 << 3)) == 0))

	//left_top 8x8 block
	iCtxInc = pALeftMb[0] + (pBTopMb[0] << 1)
	if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_CBP+iCtxInc], &pCbpBit[0]); uiRet != ERR_NONE {
		return uiRet
	}
	if pCbpBit[0] != 0 {
		*uiCbp += 0x01
	}

	//right_top 8x8 block
	iIdxA = b2i32(pCbpBit[0] == 0)
	iCtxInc = iIdxA + (pBTopMb[1] << 1)
	if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_CBP+iCtxInc], &pCbpBit[1]); uiRet != ERR_NONE {
		return uiRet
	}
	if pCbpBit[1] != 0 {
		*uiCbp += 0x02
	}

	//left_bottom 8x8 block
	iIdxB = b2i32(pCbpBit[0] == 0)
	iCtxInc = pALeftMb[1] + (iIdxB << 1)
	if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_CBP+iCtxInc], &pCbpBit[2]); uiRet != ERR_NONE {
		return uiRet
	}
	if pCbpBit[2] != 0 {
		*uiCbp += 0x04
	}

	//right_bottom 8x8 block
	iIdxB = b2i32(pCbpBit[1] == 0)
	iIdxA = b2i32(pCbpBit[2] == 0)
	iCtxInc = iIdxA + (iIdxB << 1)
	if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_CBP+iCtxInc], &pCbpBit[3]); uiRet != ERR_NONE {
		return uiRet
	}
	if pCbpBit[3] != 0 {
		*uiCbp += 0x08
	}

	if pCtx.pSps.uiChromaFormatIdc == 0 { //monochroma
		return ERR_NONE
	}

	//Chroma: bit by bit
	iIdxB = b2i32(pNeighAvail.iTopAvail != 0 && (pNeighAvail.iTopType == common.MB_TYPE_INTRA_PCM || (iTopCbp>>4) != 0))
	iIdxA = b2i32(pNeighAvail.iLeftAvail != 0 && (pNeighAvail.iLeftType == common.MB_TYPE_INTRA_PCM || (iLeftCbp>>4) != 0))

	//BitIdx = 0
	iCtxInc = iIdxA + (iIdxB << 1)
	if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pCtx.pCabacCtx[NEW_CTX_OFFSET_CBP+CTX_NUM_CBP+iCtxInc],
		&pCbpBit[4]); uiRet != ERR_NONE {
		return uiRet
	}

	//BitIdx = 1
	if pCbpBit[4] != 0 {
		iIdxB = b2i32(pNeighAvail.iTopAvail != 0 && (pNeighAvail.iTopType == common.MB_TYPE_INTRA_PCM || (iTopCbp>>4) == 2))
		iIdxA = b2i32(pNeighAvail.iLeftAvail != 0 && (pNeighAvail.iLeftType == common.MB_TYPE_INTRA_PCM || (iLeftCbp>>4) == 2))
		iCtxInc = iIdxA + (iIdxB << 1)
		if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine,
			&pCtx.pCabacCtx[NEW_CTX_OFFSET_CBP+2*CTX_NUM_CBP+iCtxInc],
			&pCbpBit[5]); uiRet != ERR_NONE {
			return uiRet
		}
		*uiCbp += 1 << (4 + pCbpBit[5])
	}

	return ERR_NONE
}

// int32_t ParseDeltaQpCabac (PWelsDecoderContext pCtx, int32_t& iQpDelta)
func ParseDeltaQpCabac(pCtx *SWelsDecoderContext, iQpDelta *int32) int32 {
	var uiCode uint32
	pCurrSlice := &(pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer)
	*iQpDelta = 0
	pBinCtx := pCtx.pCabacCtx[NEW_CTX_OFFSET_DELTA_QP:]
	iCtxInc := b2i32(pCurrSlice.iLastDeltaQp != 0)
	if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pBinCtx[iCtxInc], &uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCode != 0 {
		if uiRet := DecodeUnaryBinCabac(pCtx.pCabacDecEngine, pBinCtx[2:], 1, &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		uiCode++
		*iQpDelta = int32((uiCode + 1) >> 1)
		if (uiCode & 1) == 0 {
			*iQpDelta = -*iQpDelta
		}
	}
	pCurrSlice.iLastDeltaQp = *iQpDelta
	return ERR_NONE
}

// int32_t ParseCbfInfoCabac (PWelsNeighAvail pNeighAvail, uint8_t* pNzcCache, int32_t iZIndex, int32_t
// iResProperty, PWelsDecoderContext pCtx, uint32_t& uiCbfBit)
func ParseCbfInfoCabac(pNeighAvail *SWelsNeighAvail, pNzcCache []uint8, iZIndex int32, iResProperty int32, pCtx *SWelsDecoderContext, uiCbfBit *uint32) int32 {
	var nA, nB int8 /*, zigzag_idx = 0*/
	iCurrBlkXy := pCtx.pCurDqLayer.iMbXyIndex
	iTopBlkXy := iCurrBlkXy - pCtx.pCurDqLayer.iMbWidth //default value: MB neighboring
	iLeftBlkXy := iCurrBlkXy - 1                        //default value: MB neighboring
	pCbfDc := pCtx.pCurDqLayer.pCbfDc
	pMbType := pCtx.pCurDqLayer.pDec.pMbType
	var iCtxInc int32
	*uiCbfBit = 0
	nA = int8(b2i32(common.IS_INTRA(pMbType[iCurrBlkXy])))
	nB = nA

	if iResProperty == I16_LUMA_DC || iResProperty == CHROMA_DC_U || iResProperty == CHROMA_DC_V { //DC
		if pNeighAvail.iTopAvail != 0 {
			nB = int8(b2i32((pMbType[iTopBlkXy] == common.MB_TYPE_INTRA_PCM) || ((int32(pCbfDc[iTopBlkXy])>>iResProperty)&1) != 0))
		}
		if pNeighAvail.iLeftAvail != 0 {
			nA = int8(b2i32((pMbType[iLeftBlkXy] == common.MB_TYPE_INTRA_PCM) || ((int32(pCbfDc[iLeftBlkXy])>>iResProperty)&1) != 0))
		}
		iCtxInc = int32(nA) + (int32(nB) << 1)
		if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine,
			&pCtx.pCabacCtx[NEW_CTX_OFFSET_CBF+int32(g_kBlockCat2CtxOffsetCBF[iResProperty])+iCtxInc], uiCbfBit); uiRet != ERR_NONE {
			return uiRet
		}
		if *uiCbfBit != 0 {
			pCbfDc[iCurrBlkXy] |= uint16(1 << iResProperty)
		}
	} else { //AC
		kNzcIdx := int(g_kCacheNzcScanIdx[iZIndex])
		//for 4x4 blk, make sure blk-idx is correct
		if pNzcCache[kNzcIdx-8] != 0xff { //top blk available
			if g_kTopBlkInsideMb[iZIndex] != 0 {
				iTopBlkXy = iCurrBlkXy
			}
			nB = int8(b2i32(pNzcCache[kNzcIdx-8] != 0 || pMbType[iTopBlkXy] == common.MB_TYPE_INTRA_PCM))
		}
		if pNzcCache[kNzcIdx-1] != 0xff { //left blk available
			if g_kLeftBlkInsideMb[iZIndex] != 0 {
				iLeftBlkXy = iCurrBlkXy
			}
			nA = int8(b2i32(pNzcCache[kNzcIdx-1] != 0 || pMbType[iLeftBlkXy] == common.MB_TYPE_INTRA_PCM))
		}

		iCtxInc = int32(nA) + (int32(nB) << 1)
		if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine,
			&pCtx.pCabacCtx[NEW_CTX_OFFSET_CBF+int32(g_kBlockCat2CtxOffsetCBF[iResProperty])+iCtxInc], uiCbfBit); uiRet != ERR_NONE {
			return uiRet
		}
	}
	return ERR_NONE
}

// int32_t ParseSignificantMapCabac (int32_t* pSignificantMap, int32_t iResProperty,
// PWelsDecoderContext pCtx, uint32_t& uiCoeffNum)
func ParseSignificantMapCabac(pSignificantMap []int32, iResProperty int32, pCtx *SWelsDecoderContext, uiCoeffNum *uint32) int32 {
	var uiCode uint32

	var iMapBase, iLastBase int32
	if iResProperty == LUMA_DC_AC_8 {
		iMapBase = NEW_CTX_OFFSET_MAP_8x8
		iLastBase = NEW_CTX_OFFSET_LAST_8x8
	} else {
		iMapBase = NEW_CTX_OFFSET_MAP
		iLastBase = NEW_CTX_OFFSET_LAST
	}
	pMapCtx := pCtx.pCabacCtx[iMapBase+int32(g_kBlockCat2CtxOffsetMap[iResProperty]):]
	pLastCtx := pCtx.pCabacCtx[iLastBase+int32(g_kBlockCat2CtxOffsetLast[iResProperty]):]

	var i int32
	*uiCoeffNum = 0
	var i0 int32 = 0
	i1 := int32(g_kMaxPos[iResProperty])

	var iCtx int32
	k := 0 // pSignificantMap position

	for i = i0; i < i1; i++ {
		if iResProperty == LUMA_DC_AC_8 {
			iCtx = int32(g_kuiIdx2CtxSignificantCoeffFlag8x8[i])
		} else {
			iCtx = i
		}
		//read significant
		if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pMapCtx[iCtx], &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		if uiCode != 0 {
			pSignificantMap[k] = 1
			k++
			*uiCoeffNum++
			//read last significant
			if iResProperty == LUMA_DC_AC_8 {
				iCtx = int32(g_kuiIdx2CtxLastSignificantCoeffFlag8x8[i])
			} else {
				iCtx = i
			}
			if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pLastCtx[iCtx], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			if uiCode != 0 {
				clear(pSignificantMap[k : k+int(i1-i)])
				return ERR_NONE
			}
		} else {
			pSignificantMap[k] = 0
			k++
		}
	}

	//deal with last pSignificantMap if no data
	//if(i < i1+1)
	{
		pSignificantMap[k] = 1
		*uiCoeffNum++
	}

	return ERR_NONE
}

// int32_t ParseSignificantCoeffCabac (int32_t* pSignificant, int32_t iResProperty, PWelsDecoderContext
// pCtx)
func ParseSignificantCoeffCabac(pSignificant []int32, iResProperty int32, pCtx *SWelsDecoderContext) int32 {
	var uiCode uint32
	var iOneBase, iAbsBase int32
	if iResProperty == LUMA_DC_AC_8 {
		iOneBase = NEW_CTX_OFFSET_ONE_8x8
		iAbsBase = NEW_CTX_OFFSET_ABS_8x8
	} else {
		iOneBase = NEW_CTX_OFFSET_ONE
		iAbsBase = NEW_CTX_OFFSET_ABS
	}
	pOneCtx := pCtx.pCabacCtx[iOneBase+int32(g_kBlockCat2CtxOffsetOne[iResProperty]):]
	pAbsCtx := pCtx.pCabacCtx[iAbsBase+int32(g_kBlockCat2CtxOffsetAbs[iResProperty]):]

	iMaxType := int32(g_kMaxC2[iResProperty])
	i := int32(g_kMaxPos[iResProperty])
	pCoff := i // index into pSignificant
	var c1 int32 = 1
	var c2 int32 = 0
	for ; i >= 0; i-- {
		if pSignificant[pCoff] != 0 {
			if uiRet := DecodeBinCabac(pCtx.pCabacDecEngine, &pOneCtx[c1], &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			pSignificant[pCoff] += int32(uiCode)
			if pSignificant[pCoff] == 2 {
				if uiRet := DecodeUEGLevelCabac(pCtx.pCabacDecEngine, &pAbsCtx[c2], &uiCode); uiRet != ERR_NONE {
					return int32(uiRet)
				}
				pSignificant[pCoff] += int32(uiCode)
				c2++
				c2 = common.WELS_MIN(c2, iMaxType)
				c1 = 0
			} else if c1 != 0 {
				c1++
				c1 = common.WELS_MIN(c1, 4)
			}
			if uiRet := DecodeBypassCabac(pCtx.pCabacDecEngine, &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			if uiCode != 0 {
				pSignificant[pCoff] = -pSignificant[pCoff]
			}
		}
		pCoff--
	}
	return ERR_NONE
}

// int32_t ParseResidualBlockCabac8x8 (PWelsNeighAvail pNeighAvail, uint8_t* pNonZeroCountCache,
// SBitStringAux* pBsAux, int32_t iIndex, int32_t iMaxNumCoeff, const uint8_t* pScanTable, int32_t
// iResProperty, short* sTCoeff, /*int mb_mode*/ uint8_t uiQp, PWelsDecoderContext pCtx)
//
// sTCoeff: coefficient sub-slice starting at the C pointer.
func ParseResidualBlockCabac8x8(pNeighAvail *SWelsNeighAvail, pNonZeroCountCache []uint8, pBsAux *common.SBitStringAux, iIndex int32, iMaxNumCoeff int32, pScanTable []uint8, iResProperty int32, sTCoeff []int16, uiQp uint8, pCtx *SWelsDecoderContext) int32 {
	var uiTotalCoeffNum uint32
	var uiCbpBit uint32
	var pSignificantMap [64]int32

	var iMbResProperty int32
	GetMbResProperty(&iMbResProperty, &iResProperty, false)
	var pDeQuantMul []uint16
	if pCtx.bUseScalingList {
		pDeQuantMul = pCtx.pDequant_coeff8x8[iMbResProperty-6][uiQp][:]
	} else {
		pDeQuantMul = common.G_kuiDequantCoeff8x8[uiQp][:]
	}

	uiCbpBit = 1       // for 8x8, MaxNumCoeff == 64 && uiCbpBit == 1
	if uiCbpBit != 0 { //has coeff
		if uiRet := ParseSignificantMapCabac(pSignificantMap[:], iResProperty, pCtx, &uiTotalCoeffNum); uiRet != ERR_NONE {
			return uiRet
		}
		if uiRet := ParseSignificantCoeffCabac(pSignificantMap[:], iResProperty, pCtx); uiRet != ERR_NONE {
			return uiRet
		}
	}

	pNonZeroCountCache[g_kCacheNzcScanIdx[iIndex]] = uint8(uiTotalCoeffNum)
	pNonZeroCountCache[g_kCacheNzcScanIdx[iIndex+1]] = uint8(uiTotalCoeffNum)
	pNonZeroCountCache[g_kCacheNzcScanIdx[iIndex+2]] = uint8(uiTotalCoeffNum)
	pNonZeroCountCache[g_kCacheNzcScanIdx[iIndex+3]] = uint8(uiTotalCoeffNum)
	if uiTotalCoeffNum == 0 {
		return ERR_NONE
	}
	var j int32
	var i int32
	if iResProperty == LUMA_DC_AC_8 {
		iQp := int32(uiQp)
		for {
			if pSignificantMap[j] != 0 {
				i = int32(pScanTable[j])
				if iQp >= 36 {
					sTCoeff[i] = int16((pSignificantMap[j] * int32(pDeQuantMul[i])) * (int32(1) << uint32(iQp/6-6)))
				} else {
					sTCoeff[i] = int16((pSignificantMap[j]*int32(pDeQuantMul[i]) + (int32(1) << uint32(5-iQp/6))) >> uint32(6-iQp/6))
				}
			}
			j++
			if !(j < 64) {
				break
			}
		}
	}

	return ERR_NONE
}

// int32_t ParseResidualBlockCabac (PWelsNeighAvail pNeighAvail, uint8_t* pNonZeroCountCache,
// SBitStringAux* pBsAux, int32_t iIndex, int32_t iMaxNumCoeff, const uint8_t* pScanTable, int32_t
// iResProperty, short* sTCoeff, /*int mb_mode*/ uint8_t uiQp, PWelsDecoderContext pCtx)
//
// sTCoeff: coefficient sub-slice starting at the C pointer.
func ParseResidualBlockCabac(pNeighAvail *SWelsNeighAvail, pNonZeroCountCache []uint8, pBsAux *common.SBitStringAux, iIndex int32, iMaxNumCoeff int32, pScanTable []uint8, iResProperty int32, sTCoeff []int16, uiQp uint8, pCtx *SWelsDecoderContext) int32 {
	var iCurNzCacheIdx int32
	var uiTotalCoeffNum uint32
	var uiCbpBit uint32
	var pSignificantMap [16]int32

	var iMbResProperty int32
	GetMbResProperty(&iMbResProperty, &iResProperty, false)
	var pDeQuantMul []uint16
	if pCtx.bUseScalingList {
		pDeQuantMul = pCtx.pDequant_coeff4x4[iMbResProperty][uiQp][:]
	} else {
		pDeQuantMul = common.G_kuiDequantCoeff[uiQp][:]
	}

	if uiRet := ParseCbfInfoCabac(pNeighAvail, pNonZeroCountCache, iIndex, iResProperty, pCtx, &uiCbpBit); uiRet != ERR_NONE {
		return uiRet
	}
	if uiCbpBit != 0 { //has coeff
		if uiRet := ParseSignificantMapCabac(pSignificantMap[:], iResProperty, pCtx, &uiTotalCoeffNum); uiRet != ERR_NONE {
			return uiRet
		}
		if uiRet := ParseSignificantCoeffCabac(pSignificantMap[:], iResProperty, pCtx); uiRet != ERR_NONE {
			return uiRet
		}
	}

	iCurNzCacheIdx = int32(g_kCacheNzcScanIdx[iIndex])
	pNonZeroCountCache[iCurNzCacheIdx] = uint8(uiTotalCoeffNum)
	if uiTotalCoeffNum == 0 {
		return ERR_NONE
	}
	var j int
	if iResProperty == I16_LUMA_DC {
		for {
			sTCoeff[pScanTable[j]] = int16(pSignificantMap[j])
			j++
			if !(j < 16) {
				break
			}
		}
		WelsLumaDcDequantIdct(sTCoeff, int32(uiQp), pCtx)
	} else if iResProperty == CHROMA_DC_U || iResProperty == CHROMA_DC_V {
		for {
			sTCoeff[pScanTable[j]] = int16(pSignificantMap[j])
			j++
			if !(j < 4) {
				break
			}
		}
		//iHadamard2x2
		WelsChromaDcIdct(sTCoeff)
		//scaling
		if !pCtx.bUseScalingList {
			for j = 0; j < 4; j++ {
				sTCoeff[pScanTable[j]] = int16((int64(sTCoeff[pScanTable[j]]) * int64(pDeQuantMul[0])) >> 1)
			}
		} else { //with scaling list
			for j = 0; j < 4; j++ {
				sTCoeff[pScanTable[j]] = int16((int64(sTCoeff[pScanTable[j]]) * int64(pDeQuantMul[0])) >> 5)
			}
		}
	} else { //luma ac, chroma ac
		for {
			if pSignificantMap[j] != 0 {
				if !pCtx.bUseScalingList {
					sTCoeff[pScanTable[j]] = int16(pSignificantMap[j] * int32(pDeQuantMul[pScanTable[j]&0x07]))
				} else {
					sTCoeff[pScanTable[j]] = int16((int64(pSignificantMap[j])*int64(pDeQuantMul[pScanTable[j]]) + 8) >> 4)
				}
			}
			j++
			if !(j < 16) {
				break
			}
		}
	}
	return ERR_NONE
}

// int32_t ParseIPCMInfoCabac (PWelsDecoderContext pCtx)
func ParseIPCMInfoCabac(pCtx *SWelsDecoderContext) int32 {
	var i int32
	pCabacDecEngine := pCtx.pCabacDecEngine
	pBsAux := pCtx.pCurDqLayer.pBitStringAux
	pCurDqLayer := pCtx.pCurDqLayer
	iDstStrideLuma := pCurDqLayer.pDec.iLinesize[0]
	iDstStrideChroma := pCurDqLayer.pDec.iLinesize[1]
	iMbX := pCurDqLayer.iMbX
	iMbY := pCurDqLayer.iMbY
	iMbXy := pCurDqLayer.iMbXyIndex

	iMbOffsetLuma := (iMbX + iMbY*iDstStrideLuma) << 4
	iMbOffsetChroma := (iMbX + iMbY*iDstStrideChroma) << 3

	pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA_PCM
	RestoreCabacDecEngineToBS(pCabacDecEngine, pBsAux)
	iBytesLeft := pBsAux.PEndBuf - pBsAux.PCurBuf
	if iBytesLeft < I_PCM_MB_SIZE_IN_BYTE {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_CABAC_NO_BS_TO_READ)
	}
	pSrc := pBsAux.PBuf
	pPtrSrc := pBsAux.PCurBuf
	if !pCtx.pParam.BParseOnly {
		pDec := pCtx.pDec
		pMbDstY := pDec.iDataOff[0] + int(iMbOffsetLuma)
		pMbDstU := pDec.iDataOff[1] + int(iMbOffsetChroma)
		pMbDstV := pDec.iDataOff[2] + int(iMbOffsetChroma)
		for i = 0; i < 16; i++ { //luma
			copy(pDec.pData[0][pMbDstY:pMbDstY+16], pSrc[pPtrSrc:pPtrSrc+16])
			pMbDstY += int(iDstStrideLuma)
			pPtrSrc += 16
		}
		for i = 0; i < 8; i++ { //cb
			copy(pDec.pData[1][pMbDstU:pMbDstU+8], pSrc[pPtrSrc:pPtrSrc+8])
			pMbDstU += int(iDstStrideChroma)
			pPtrSrc += 8
		}
		for i = 0; i < 8; i++ { //cr
			copy(pDec.pData[2][pMbDstV:pMbDstV+8], pSrc[pPtrSrc:pPtrSrc+8])
			pMbDstV += int(iDstStrideChroma)
			pPtrSrc += 8
		}
	}

	pBsAux.PCurBuf += I_PCM_MB_SIZE_IN_BYTE

	pCurDqLayer.pLumaQp[iMbXy] = 0
	pCurDqLayer.pChromaQp[iMbXy][0] = 0
	pCurDqLayer.pChromaQp[iMbXy][1] = 0
	for k := range pCurDqLayer.pNzc[iMbXy] {
		pCurDqLayer.pNzc[iMbXy][k] = 16
	}

	//step 4: cabac engine init
	if uiRet := InitReadBits(pBsAux, 1); uiRet != ERR_NONE {
		return uiRet
	}
	if uiRet := InitCabacDecEngineFromBS(pCabacDecEngine, pBsAux); uiRet != ERR_NONE {
		return uiRet
	}
	return ERR_NONE
}

// void UpdateP8x8RefCacheIdxCabac (int8_t pRefIndex[LIST_A][30], const int16_t& iPartIdx, const
// int32_t& listIdx, const int8_t& iRef)
//
// const C++ references -> values.
func UpdateP8x8RefCacheIdxCabac(pRefIndex *[common.LIST_A][30]int8, iPartIdx int16, listIdx int32, iRef int8) {
	uiCacheIdx := int(common.G_kuiCache30ScanIdx[iPartIdx])
	pRefIndex[listIdx][uiCacheIdx] = iRef
	pRefIndex[listIdx][uiCacheIdx+1] = iRef
	pRefIndex[listIdx][uiCacheIdx+6] = iRef
	pRefIndex[listIdx][uiCacheIdx+7] = iRef
}
