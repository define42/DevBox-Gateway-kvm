// Port of codec/decoder/core/src/mv_pred.cpp.
//
// Motion vector prediction (P and B slices, including B-slice spatial and
// temporal direct prediction).

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// SetRectBlock (static inline in mv_pred.cpp) is a byte-level rectangle fill
// on a void pointer. Its only callers work on the 16-entry colocated caches of
// SDqLayer, so it is split into the two element types that occur:
//
//   - setRectBlockInt8: int8 caches (iColocIntra, iColocRefIndex[l]) with
//     size == 1; p[off..] is the C vp, w / h / stride are in bytes == entries.
//   - setRectBlockMv: MV caches (iColocMv[l]) with size == 4 (one [2]int16 MV
//     per 4-byte unit); w / stride are in MV units.
//
// Only the (w, h) combinations handled by the C function are filled; any
// other combination is a no-op exactly as in C.
func setRectBlockInt8(p *[16]int8, off int, w int32, h int32, stride int32, val int8) {
	switch {
	case (w == 1 && h == 4) || (w == 2 && h == 2) || (w == 2 && h == 4) || (w == 4 && h == 2) ||
		(w == 4 && h == 4) || (w == 8 && h == 1) || (w == 8 && h == 2) || (w == 8 && h == 4) ||
		(w == 16 && h == 2) || (w == 16 && h == 3) || (w == 16 && h == 4):
		for y := int32(0); y < h; y++ {
			for x := int32(0); x < w; x++ {
				p[off+int(y*stride+x)] = val
			}
		}
	}
}

// setRectBlockMv: see setRectBlockInt8. w is the C w (in MVs, size 4),
// stride is the C byte stride; the C combinations reachable with size == 4
// are (w*4, h) in {(8,2)} for the callers; all valid byte shapes are handled.
func setRectBlockMv(p *[16][2]int16, off int, w int32, h int32, stride int32, val [2]int16) {
	wb := w * 4
	switch {
	case (wb == 4 && h == 2) || (wb == 4 && h == 4) || (wb == 8 && h == 1) || (wb == 8 && h == 2) ||
		(wb == 8 && h == 4) || (wb == 16 && h == 2) || (wb == 16 && h == 3) || (wb == 16 && h == 4):
		strideMv := stride / 4
		for y := int32(0); y < h; y++ {
			for x := int32(0); x < w; x++ {
				p[off+int(y*strideMv+x)] = val
			}
		}
	}
}

// CopyRectBlock4Cols ports void CopyRectBlock4Cols (void* vdst, void* vsrc, const int32_t
// stride_dst, const int32_t stride_src, int32_t w, const int32_t size).
//
// C: void* vdst / vsrc (byte strides). Only used inside mv_pred.cpp with vdst =
// &pCurDqLayer.iColocMv[l] (*[16][2]int16) / &pCurDqLayer.iColocRefIndex[l] (*[16]int8) and vsrc =
// &pic.pMv[l][mb] / &pic.pRefIndex[l][mb]; implemented for those types (strides in bytes).
func CopyRectBlock4Cols(vdst any, vsrc any, stride_dst int32, stride_src int32, w int32, size int32) {
	w *= size
	if w != 1 && w != 2 && w != 4 && w != 16 {
		return
	}
	switch dst := vdst.(type) {
	case *[16][2]int16:
		src := vsrc.(*[16][2]int16)
		// work on int16 units (2 bytes each)
		n := int(w / 2)
		for r := int32(0); r < 4; r++ {
			d := int(stride_dst*r) / 2
			s := int(stride_src*r) / 2
			for k := 0; k < n; k++ {
				dst[(d+k)/2][(d+k)%2] = src[(s+k)/2][(s+k)%2]
			}
		}
	case *[16]int8:
		src := vsrc.(*[16]int8)
		for r := int32(0); r < 4; r++ {
			d := int(stride_dst * r)
			s := int(stride_src * r)
			copy(dst[d:d+int(w)], src[s:s+int(w)])
		}
	}
}

// mvPredLayerMv returns pCurDqLayer->pDec ? pCurDqLayer->pDec->pMv[l] : pCurDqLayer->pMv[l].
func mvPredLayerMv(pCurDqLayer *SDqLayer, listIdx int32) [][common.MB_BLOCK4x4_NUM][common.MV_A]int16 {
	if pCurDqLayer.pDec != nil {
		return pCurDqLayer.pDec.pMv[listIdx]
	}
	return pCurDqLayer.pMv[listIdx]
}

// mvPredLayerRefIndex returns pCurDqLayer->pDec ? pCurDqLayer->pDec->pRefIndex[l] : pCurDqLayer->pRefIndex[l].
func mvPredLayerRefIndex(pCurDqLayer *SDqLayer, listIdx int32) [][common.MB_BLOCK4x4_NUM]int8 {
	if pCurDqLayer.pDec != nil {
		return pCurDqLayer.pDec.pRefIndex[listIdx]
	}
	return pCurDqLayer.pRefIndex[listIdx]
}

func mvMedian(a, b, c int16) int16 {
	return int16(common.WelsMedian(int32(a), int32(b), int32(c)))
}

func b2i8(b bool) int8 {
	if b {
		return 1
	}
	return 0
}

// PredPSkipMvFromNeighbor ports void PredPSkipMvFromNeighbor (PDqLayer pCurDqLayer, int16_t iMvp[2]).
func PredPSkipMvFromNeighbor(pCurDqLayer *SDqLayer, iMvp *[2]int16) {
	var bTopAvail, bLeftTopAvail, bRightTopAvail, bLeftAvail bool

	var iCurSliceIdc, iTopSliceIdc, iLeftTopSliceIdc, iRightTopSliceIdc, iLeftSliceIdc int32
	var iLeftTopType, iRightTopType, iTopType, iLeftType int32
	var iCurX, iCurY, iCurXy, iLeftXy, iTopXy, iLeftTopXy, iRightTopXy int32

	var iLeftRef int8
	var iTopRef int8
	var iRightTopRef int8
	var iLeftTopRef int8
	var iDiagonalRef int8
	var iMatchRef int8
	var iMvA, iMvB, iMvC, iMvD [2]int16

	iCurXy = pCurDqLayer.iMbXyIndex
	iCurX = pCurDqLayer.iMbX
	iCurY = pCurDqLayer.iMbY
	iCurSliceIdc = pCurDqLayer.pSliceIdc[iCurXy]

	if iCurX != 0 {
		iLeftXy = iCurXy - 1
		iLeftSliceIdc = pCurDqLayer.pSliceIdc[iLeftXy]
		bLeftAvail = (iLeftSliceIdc == iCurSliceIdc)
	} else {
		bLeftAvail = false
		bLeftTopAvail = false
	}

	if iCurY != 0 {
		iTopXy = iCurXy - pCurDqLayer.iMbWidth
		iTopSliceIdc = pCurDqLayer.pSliceIdc[iTopXy]
		bTopAvail = (iTopSliceIdc == iCurSliceIdc)
		if iCurX != 0 {
			iLeftTopXy = iTopXy - 1
			iLeftTopSliceIdc = pCurDqLayer.pSliceIdc[iLeftTopXy]
			bLeftTopAvail = (iLeftTopSliceIdc == iCurSliceIdc)
		} else {
			bLeftTopAvail = false
		}
		if iCurX != (pCurDqLayer.iMbWidth - 1) {
			iRightTopXy = iTopXy + 1
			iRightTopSliceIdc = pCurDqLayer.pSliceIdc[iRightTopXy]
			bRightTopAvail = (iRightTopSliceIdc == iCurSliceIdc)
		} else {
			bRightTopAvail = false
		}
	} else {
		bTopAvail = false
		bLeftTopAvail = false
		bRightTopAvail = false
	}

	if iCurX != 0 && bLeftAvail {
		iLeftType = int32(GetMbType(pCurDqLayer)[iLeftXy])
	}
	if iCurY != 0 && bTopAvail {
		iTopType = int32(GetMbType(pCurDqLayer)[iTopXy])
	}
	if iCurX != 0 && iCurY != 0 && bLeftTopAvail {
		iLeftTopType = int32(GetMbType(pCurDqLayer)[iLeftTopXy])
	}
	if iCurX != pCurDqLayer.iMbWidth-1 && iCurY != 0 && bRightTopAvail {
		iRightTopType = int32(GetMbType(pCurDqLayer)[iRightTopXy])
	}

	pMv := mvPredLayerMv(pCurDqLayer, 0)
	pRefIndex := mvPredLayerRefIndex(pCurDqLayer, 0)

	/*get neb mv&iRefIdxArray*/
	/*left*/
	if bLeftAvail && common.IS_INTER(iLeftType) {
		iMvA = pMv[iLeftXy][3]
		iLeftRef = pRefIndex[iLeftXy][3]
	} else {
		iMvA = [2]int16{}
		if !bLeftAvail { //not available
			iLeftRef = REF_NOT_AVAIL
		} else { //available but is intra mb type
			iLeftRef = REF_NOT_IN_LIST
		}
	}
	if REF_NOT_AVAIL == iLeftRef ||
		(0 == iLeftRef && iMvA == [2]int16{}) {
		*iMvp = [2]int16{}
		return
	}

	/*top*/
	if bTopAvail && common.IS_INTER(iTopType) {
		iMvB = pMv[iTopXy][12]
		iTopRef = pRefIndex[iTopXy][12]
	} else {
		iMvB = [2]int16{}
		if !bTopAvail { //not available
			iTopRef = REF_NOT_AVAIL
		} else { //available but is intra mb type
			iTopRef = REF_NOT_IN_LIST
		}
	}
	if REF_NOT_AVAIL == iTopRef ||
		(0 == iTopRef && iMvB == [2]int16{}) {
		*iMvp = [2]int16{}
		return
	}

	/*right_top*/
	if bRightTopAvail && common.IS_INTER(iRightTopType) {
		iMvC = pMv[iRightTopXy][12]
		iRightTopRef = pRefIndex[iRightTopXy][12]
	} else {
		iMvC = [2]int16{}
		if !bRightTopAvail { //not available
			iRightTopRef = REF_NOT_AVAIL
		} else { //available but is intra mb type
			iRightTopRef = REF_NOT_IN_LIST
		}
	}

	/*left_top*/
	if bLeftTopAvail && common.IS_INTER(iLeftTopType) {
		iMvD = pMv[iLeftTopXy][15]
		iLeftTopRef = pRefIndex[iLeftTopXy][15]
	} else {
		iMvD = [2]int16{}
		if !bLeftTopAvail { //not available
			iLeftTopRef = REF_NOT_AVAIL
		} else { //available but is intra mb type
			iLeftTopRef = REF_NOT_IN_LIST
		}
	}

	iDiagonalRef = iRightTopRef
	if REF_NOT_AVAIL == iDiagonalRef {
		iDiagonalRef = iLeftTopRef
		iMvC = iMvD
	}

	if REF_NOT_AVAIL == iTopRef && REF_NOT_AVAIL == iDiagonalRef && iLeftRef >= REF_NOT_IN_LIST {
		*iMvp = iMvA
		return
	}

	iMatchRef = b2i8(0 == iLeftRef) + b2i8(0 == iTopRef) + b2i8(0 == iDiagonalRef)
	if 1 == iMatchRef {
		if 0 == iLeftRef {
			*iMvp = iMvA
		} else if 0 == iTopRef {
			*iMvp = iMvB
		} else {
			*iMvp = iMvC
		}
	} else {
		iMvp[0] = mvMedian(iMvA[0], iMvB[0], iMvC[0])
		iMvp[1] = mvMedian(iMvA[1], iMvB[1], iMvC[1])
	}
}

// GetColocatedMb ports int32_t GetColocatedMb (PWelsDecoderContext pCtx, MbType& mbType,
// SubMbType& subMbType).
//
// C++ references -> pointers.
func GetColocatedMb(pCtx *SWelsDecoderContext, mbType *MbType, subMbType *SubMbType) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	iMbXy := pCurDqLayer.iMbXyIndex

	is8x8 := common.IS_Inter_8x8(GetMbType(pCurDqLayer)[iMbXy])
	*mbType = GetMbType(pCurDqLayer)[iMbXy]

	colocPic := pCtx.sRefPic.pRefList[common.LIST_1][0]
	if colocPic == nil {
		pLogCtx := &pCtx.sLogCtx
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "Colocated Ref Picture for B-Slice is lost, B-Slice decoding cannot be continued!")
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_DATA, ERR_INFO_REFERENCE_PIC_LOST)
	}

	// GetThreadCount (pCtx) > 1 branch (wait for colocated MB row) dropped: single-threaded port.

	var coloc_mbType MbType = colocPic.pMbType[iMbXy]
	if coloc_mbType == common.MB_TYPE_SKIP {
		//This indicates the colocated MB is P SKIP MB
		coloc_mbType |= common.MB_TYPE_16x16 | common.MB_TYPE_P0L0 | common.MB_TYPE_P1L0
	}
	if common.IS_Inter_8x8(coloc_mbType) && !pCtx.pSps.bDirect8x8InferenceFlag {
		*subMbType = common.SUB_MB_TYPE_4x4 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1 | common.MB_TYPE_DIRECT
		*mbType |= common.MB_TYPE_8x8 | common.MB_TYPE_L0 | common.MB_TYPE_L1
	} else if !is8x8 && (common.IS_INTER_16x16(coloc_mbType) || common.IS_INTRA(coloc_mbType) /* || IS_SKIP(coloc_mbType)*/) {
		*subMbType = common.SUB_MB_TYPE_8x8 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1 | common.MB_TYPE_DIRECT
		*mbType |= common.MB_TYPE_16x16 | common.MB_TYPE_L0 | common.MB_TYPE_L1
	} else {
		*subMbType = common.SUB_MB_TYPE_8x8 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1 | common.MB_TYPE_DIRECT
		*mbType |= common.MB_TYPE_8x8 | common.MB_TYPE_L0 | common.MB_TYPE_L1
	}

	if common.IS_INTRA(coloc_mbType) {
		setRectBlockInt8(&pCurDqLayer.iColocIntra, 0, 4, 4, 4, 1)
		return ERR_NONE
	}
	setRectBlockInt8(&pCurDqLayer.iColocIntra, 0, 4, 4, 4, 0)

	if common.IS_INTER_16x16(*mbType) {
		var iMVZero [2]int16
		pMv := &iMVZero
		if common.IS_TYPE_L1(coloc_mbType) {
			pMv = &colocPic.pMv[common.LIST_1][iMbXy][0]
		}
		pCurDqLayer.iColocMv[common.LIST_0][0] = colocPic.pMv[common.LIST_0][iMbXy][0]
		pCurDqLayer.iColocMv[common.LIST_1][0] = *pMv
		pCurDqLayer.iColocRefIndex[common.LIST_0][0] = colocPic.pRefIndex[common.LIST_0][iMbXy][0]
		if common.IS_TYPE_L1(coloc_mbType) {
			pCurDqLayer.iColocRefIndex[common.LIST_1][0] = colocPic.pRefIndex[common.LIST_1][iMbXy][0]
		} else {
			pCurDqLayer.iColocRefIndex[common.LIST_1][0] = REF_NOT_IN_LIST
		}
	} else {
		if !pCtx.pSps.bDirect8x8InferenceFlag {
			CopyRectBlock4Cols(&pCurDqLayer.iColocMv[common.LIST_0], &colocPic.pMv[common.LIST_0][iMbXy], 16, 16, 4, 4)
			CopyRectBlock4Cols(&pCurDqLayer.iColocRefIndex[common.LIST_0], &colocPic.pRefIndex[common.LIST_0][iMbXy], 4, 4, 4, 1)
			if common.IS_TYPE_L1(coloc_mbType) {
				CopyRectBlock4Cols(&pCurDqLayer.iColocMv[common.LIST_1], &colocPic.pMv[common.LIST_1][iMbXy], 16, 16, 4, 4)
				CopyRectBlock4Cols(&pCurDqLayer.iColocRefIndex[common.LIST_1], &colocPic.pRefIndex[common.LIST_1][iMbXy], 4, 4, 4, 1)
			} else { // only forward prediction
				setRectBlockInt8(&pCurDqLayer.iColocRefIndex[common.LIST_1], 0, 4, 4, 4, REF_NOT_IN_LIST)
			}
		} else {
			iListCount := int32(1)
			if coloc_mbType&common.MB_TYPE_L1 != 0 {
				iListCount = 2
			}
			for listIdx := int32(0); listIdx < iListCount; listIdx++ {
				setRectBlockMv(&pCurDqLayer.iColocMv[listIdx], 0, 2, 2, 16, colocPic.pMv[listIdx][iMbXy][0])
				setRectBlockMv(&pCurDqLayer.iColocMv[listIdx], 2, 2, 2, 16, colocPic.pMv[listIdx][iMbXy][3])
				setRectBlockMv(&pCurDqLayer.iColocMv[listIdx], 8, 2, 2, 16, colocPic.pMv[listIdx][iMbXy][12])
				setRectBlockMv(&pCurDqLayer.iColocMv[listIdx], 10, 2, 2, 16, colocPic.pMv[listIdx][iMbXy][15])

				setRectBlockInt8(&pCurDqLayer.iColocRefIndex[listIdx], 0, 2, 2, 4, colocPic.pRefIndex[listIdx][iMbXy][0])
				setRectBlockInt8(&pCurDqLayer.iColocRefIndex[listIdx], 2, 2, 2, 4, colocPic.pRefIndex[listIdx][iMbXy][3])
				setRectBlockInt8(&pCurDqLayer.iColocRefIndex[listIdx], 8, 2, 2, 4, colocPic.pRefIndex[listIdx][iMbXy][12])
				setRectBlockInt8(&pCurDqLayer.iColocRefIndex[listIdx], 10, 2, 2, 4, colocPic.pRefIndex[listIdx][iMbXy][15])
			}
			if coloc_mbType&common.MB_TYPE_L1 == 0 { // only forward prediction
				setRectBlockInt8(&pCurDqLayer.iColocRefIndex[1], 0, 4, 4, 4, REF_NOT_IN_LIST)
			}
		}
	}
	return ERR_NONE
}

// mvIsSmall is (unsigned) (mv + 1) <= 2, i.e. mv in [-1, 1].
func mvIsSmall(mv int16) bool {
	return uint32(int32(mv)+1) <= 2
}

// PredMvBDirectSpatial ports int32_t PredMvBDirectSpatial (PWelsDecoderContext pCtx, int16_t
// iMvp[LIST_A][2], int8_t ref[LIST_A], SubMbType& subMbType).
func PredMvBDirectSpatial(pCtx *SWelsDecoderContext, iMvp *[common.LIST_A][2]int16, ref *[common.LIST_A]int8, subMbType *SubMbType) int32 {
	var ret int32 = ERR_NONE
	pCurDqLayer := pCtx.pCurDqLayer
	iMbXy := pCurDqLayer.iMbXyIndex
	bSkipOrDirect := common.IS_SKIP(GetMbType(pCurDqLayer)[iMbXy]) || common.IS_DIRECT(GetMbType(pCurDqLayer)[iMbXy])

	var mbType MbType
	ret = GetColocatedMb(pCtx, &mbType, subMbType)
	if ret != ERR_NONE {
		return ret
	}

	var bTopAvail, bLeftTopAvail, bRightTopAvail, bLeftAvail bool
	var iLeftTopType, iRightTopType, iTopType, iLeftType int32
	var iCurSliceIdc, iTopSliceIdc, iLeftTopSliceIdc, iRightTopSliceIdc, iLeftSliceIdc int32
	var iCurX, iCurY, iCurXy, iLeftXy, iTopXy, iLeftTopXy, iRightTopXy int32

	var iLeftRef [common.LIST_A]int8
	var iTopRef [common.LIST_A]int8
	var iRightTopRef [common.LIST_A]int8
	var iLeftTopRef [common.LIST_A]int8
	var iDiagonalRef [common.LIST_A]int8
	var iMvA, iMvB, iMvC, iMvD [common.LIST_A][2]int16

	iCurXy = pCurDqLayer.iMbXyIndex

	iCurX = pCurDqLayer.iMbX
	iCurY = pCurDqLayer.iMbY
	iCurSliceIdc = pCurDqLayer.pSliceIdc[iCurXy]

	if iCurX != 0 {
		iLeftXy = iCurXy - 1
		iLeftSliceIdc = pCurDqLayer.pSliceIdc[iLeftXy]
		bLeftAvail = (iLeftSliceIdc == iCurSliceIdc)
	} else {
		bLeftAvail = false
		bLeftTopAvail = false
	}

	if iCurY != 0 {
		iTopXy = iCurXy - pCurDqLayer.iMbWidth
		iTopSliceIdc = pCurDqLayer.pSliceIdc[iTopXy]
		bTopAvail = (iTopSliceIdc == iCurSliceIdc)
		if iCurX != 0 {
			iLeftTopXy = iTopXy - 1
			iLeftTopSliceIdc = pCurDqLayer.pSliceIdc[iLeftTopXy]
			bLeftTopAvail = (iLeftTopSliceIdc == iCurSliceIdc)
		} else {
			bLeftTopAvail = false
		}
		if iCurX != (pCurDqLayer.iMbWidth - 1) {
			iRightTopXy = iTopXy + 1
			iRightTopSliceIdc = pCurDqLayer.pSliceIdc[iRightTopXy]
			bRightTopAvail = (iRightTopSliceIdc == iCurSliceIdc)
		} else {
			bRightTopAvail = false
		}
	} else {
		bTopAvail = false
		bLeftTopAvail = false
		bRightTopAvail = false
	}

	if iCurX != 0 && bLeftAvail {
		iLeftType = int32(GetMbType(pCurDqLayer)[iLeftXy])
	}
	if iCurY != 0 && bTopAvail {
		iTopType = int32(GetMbType(pCurDqLayer)[iTopXy])
	}
	if iCurX != 0 && iCurY != 0 && bLeftTopAvail {
		iLeftTopType = int32(GetMbType(pCurDqLayer)[iLeftTopXy])
	}
	if iCurX != pCurDqLayer.iMbWidth-1 && iCurY != 0 && bRightTopAvail {
		iRightTopType = int32(GetMbType(pCurDqLayer)[iRightTopXy])
	}

	/*get neb mv&iRefIdxArray*/
	for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
		pMv := mvPredLayerMv(pCurDqLayer, listIdx)
		pRefIndex := mvPredLayerRefIndex(pCurDqLayer, listIdx)

		/*left*/
		if bLeftAvail && common.IS_INTER(iLeftType) {
			iMvA[listIdx] = pMv[iLeftXy][3]
			iLeftRef[listIdx] = pRefIndex[iLeftXy][3]
		} else {
			iMvA[listIdx] = [2]int16{}
			if !bLeftAvail { //not available
				iLeftRef[listIdx] = REF_NOT_AVAIL
			} else { //available but is intra mb type
				iLeftRef[listIdx] = REF_NOT_IN_LIST
			}
		}

		/*top*/
		if bTopAvail && common.IS_INTER(iTopType) {
			iMvB[listIdx] = pMv[iTopXy][12]
			iTopRef[listIdx] = pRefIndex[iTopXy][12]
		} else {
			iMvB[listIdx] = [2]int16{}
			if !bTopAvail { //not available
				iTopRef[listIdx] = REF_NOT_AVAIL
			} else { //available but is intra mb type
				iTopRef[listIdx] = REF_NOT_IN_LIST
			}
		}

		/*right_top*/
		if bRightTopAvail && common.IS_INTER(iRightTopType) {
			iMvC[listIdx] = pMv[iRightTopXy][12]
			iRightTopRef[listIdx] = pRefIndex[iRightTopXy][12]
		} else {
			iMvC[listIdx] = [2]int16{}
			if !bRightTopAvail { //not available
				iRightTopRef[listIdx] = REF_NOT_AVAIL
			} else { //available but is intra mb type
				iRightTopRef[listIdx] = REF_NOT_IN_LIST
			}
		}
		/*left_top*/
		if bLeftTopAvail && common.IS_INTER(iLeftTopType) {
			iMvD[listIdx] = pMv[iLeftTopXy][15]
			iLeftTopRef[listIdx] = pRefIndex[iLeftTopXy][15]
		} else {
			iMvD[listIdx] = [2]int16{}
			if !bLeftTopAvail { //not available
				iLeftTopRef[listIdx] = REF_NOT_AVAIL
			} else { //available but is intra mb type
				iLeftTopRef[listIdx] = REF_NOT_IN_LIST
			}
		}

		iDiagonalRef[listIdx] = iRightTopRef[listIdx]
		if REF_NOT_AVAIL == iDiagonalRef[listIdx] {
			iDiagonalRef[listIdx] = iLeftTopRef[listIdx]
			iMvC[listIdx] = iMvD[listIdx]
		}

		ref_temp := common.WELS_MIN_POSITIVE(iTopRef[listIdx], iDiagonalRef[listIdx])
		ref[listIdx] = common.WELS_MIN_POSITIVE(iLeftRef[listIdx], ref_temp)
		if ref[listIdx] >= 0 {

			match_count := uint32(b2i8(iLeftRef[listIdx] == ref[listIdx])) + uint32(b2i8(iTopRef[listIdx] == ref[listIdx])) +
				uint32(b2i8(iDiagonalRef[listIdx] == ref[listIdx]))
			if match_count == 1 {
				if iLeftRef[listIdx] == ref[listIdx] {
					iMvp[listIdx] = iMvA[listIdx]
				} else if iTopRef[listIdx] == ref[listIdx] {
					iMvp[listIdx] = iMvB[listIdx]
				} else {
					iMvp[listIdx] = iMvC[listIdx]
				}
			} else {
				iMvp[listIdx][0] = mvMedian(iMvA[listIdx][0], iMvB[listIdx][0], iMvC[listIdx][0])
				iMvp[listIdx][1] = mvMedian(iMvA[listIdx][1], iMvB[listIdx][1], iMvC[listIdx][1])
			}
		} else {
			iMvp[listIdx][0] = 0
			iMvp[listIdx][1] = 0
			ref[listIdx] = REF_NOT_IN_LIST
		}
	}
	if ref[common.LIST_0] <= REF_NOT_IN_LIST && ref[common.LIST_1] <= REF_NOT_IN_LIST {
		ref[common.LIST_0] = 0
		ref[common.LIST_1] = 0
	} else if ref[common.LIST_1] < 0 {
		mbType &^= common.MB_TYPE_L1
		*subMbType &^= common.MB_TYPE_L1
	} else if ref[common.LIST_0] < 0 {
		mbType &^= common.MB_TYPE_L0
		*subMbType &^= common.MB_TYPE_L0
	}
	GetMbType(pCurDqLayer)[iMbXy] = mbType

	var pMvd [2]int16 // C: int16_t pMvd[4] = { 0 }

	bIsLongRef := pCtx.sRefPic.pRefList[common.LIST_1][0].bIsLongRef

	if common.IS_INTER_16x16(mbType) {
		if iMvp[common.LIST_0] != [2]int16{} || iMvp[common.LIST_1] != [2]int16{} {
			if 0 == pCurDqLayer.iColocIntra[0] && !bIsLongRef &&
				((pCurDqLayer.iColocRefIndex[common.LIST_0][0] == 0 && mvIsSmall(pCurDqLayer.iColocMv[common.LIST_0][0][0]) &&
					mvIsSmall(pCurDqLayer.iColocMv[common.LIST_0][0][1])) ||
					(pCurDqLayer.iColocRefIndex[common.LIST_0][0] < 0 && pCurDqLayer.iColocRefIndex[common.LIST_1][0] == 0 &&
						mvIsSmall(pCurDqLayer.iColocMv[common.LIST_1][0][0]) &&
						mvIsSmall(pCurDqLayer.iColocMv[common.LIST_1][0][1]))) {
				if 0 >= ref[0] {
					iMvp[common.LIST_0] = [2]int16{}
				}
				if 0 >= ref[1] {
					iMvp[common.LIST_1] = [2]int16{}
				}
			}
		}
		UpdateP16x16DirectCabac(pCurDqLayer)
		for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
			UpdateP16x16MotionInfo(pCurDqLayer, listIdx, ref[listIdx], &iMvp[listIdx])
			UpdateP16x16MvdCabac(pCurDqLayer, &pMvd, int8(listIdx))
		}
	} else {
		if bSkipOrDirect {
			var pSubPartCount, pPartW [4]int8
			for i := int32(0); i < 4; i++ { //Direct 8x8 Ref and mv
				iIdx8 := int16(i << 2)
				pCurDqLayer.pSubMbType[iMbXy][i] = *subMbType
				var pRefIndex [common.LIST_A][30]int8
				UpdateP8x8RefIdxCabac(pCurDqLayer, &pRefIndex, int32(iIdx8), ref[common.LIST_0], common.LIST_0)
				UpdateP8x8RefIdxCabac(pCurDqLayer, &pRefIndex, int32(iIdx8), ref[common.LIST_1], common.LIST_1)
				UpdateP8x8DirectCabac(pCurDqLayer, int32(iIdx8))

				pSubPartCount[i] = g_ksInterBSubMbTypeInfo[0].iPartCount
				pPartW[i] = g_ksInterBSubMbTypeInfo[0].iPartWidth

				if common.IS_SUB_4x4(*subMbType) {
					pSubPartCount[i] = 4
					pPartW[i] = 1
				}
				FillSpatialDirect8x8Mv(pCurDqLayer, iIdx8, pSubPartCount[i], pPartW[i], *subMbType, bIsLongRef, iMvp, ref, nil, nil)
			}
		}
	}
	return ret
}

// PredBDirectTemporal ports int32_t PredBDirectTemporal (PWelsDecoderContext pCtx, int16_t
// iMvp[LIST_A][2], int8_t ref[LIST_A], SubMbType& subMbType).
func PredBDirectTemporal(pCtx *SWelsDecoderContext, iMvp *[common.LIST_A][2]int16, ref *[common.LIST_A]int8, subMbType *SubMbType) int32 {
	var ret int32 = ERR_NONE
	pCurDqLayer := pCtx.pCurDqLayer
	iMbXy := pCurDqLayer.iMbXyIndex
	bSkipOrDirect := common.IS_SKIP(GetMbType(pCurDqLayer)[iMbXy]) || common.IS_DIRECT(GetMbType(pCurDqLayer)[iMbXy])

	var mbType MbType
	ret = GetColocatedMb(pCtx, &mbType, subMbType)
	if ret != ERR_NONE {
		return ret
	}

	GetMbType(pCurDqLayer)[iMbXy] = mbType

	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	var pMvd [2]int16 // C: int16_t pMvd[4] = { 0 }
	ref0Count := common.WELS_MIN(pSliceHeader.uiRefCount[common.LIST_0], int32(pCtx.sRefPic.uiRefCount[common.LIST_0]))
	if common.IS_INTER_16x16(mbType) {
		ref[common.LIST_0] = 0
		ref[common.LIST_1] = 0
		UpdateP16x16DirectCabac(pCurDqLayer)
		UpdateP16x16RefIdx(pCurDqLayer, common.LIST_1, ref[common.LIST_1])
		*iMvp = [common.LIST_A][2]int16{}
		if pCurDqLayer.iColocIntra[0] != 0 {
			UpdateP16x16MotionOnly(pCurDqLayer, common.LIST_0, &iMvp[common.LIST_0])
			UpdateP16x16MotionOnly(pCurDqLayer, common.LIST_1, &iMvp[common.LIST_1])
			UpdateP16x16RefIdx(pCurDqLayer, common.LIST_0, ref[common.LIST_0])
		} else {
			ref[common.LIST_0] = 0
			mv := &pCurDqLayer.iColocMv[common.LIST_0][0]
			colocRefIndexL0 := pCurDqLayer.iColocRefIndex[common.LIST_0][0]
			if colocRefIndexL0 >= 0 {
				ref[common.LIST_0] = MapColToList0(pCtx, colocRefIndexL0, ref0Count)
			} else {
				mv = &pCurDqLayer.iColocMv[common.LIST_1][0]
			}
			UpdateP16x16RefIdx(pCurDqLayer, common.LIST_0, ref[common.LIST_0])

			iMvScale := int32(pSlice.iMvScale[common.LIST_0][ref[common.LIST_0]])
			iMvp[common.LIST_0][0] = int16((iMvScale*int32(mv[0]) + 128) >> 8)
			iMvp[common.LIST_0][1] = int16((iMvScale*int32(mv[1]) + 128) >> 8)
			UpdateP16x16MotionOnly(pCurDqLayer, common.LIST_0, &iMvp[common.LIST_0])
			iMvp[common.LIST_1][0] = int16(int32(iMvp[common.LIST_0][0]) - int32(mv[0]))
			iMvp[common.LIST_1][1] = int16(int32(iMvp[common.LIST_0][1]) - int32(mv[1]))
			UpdateP16x16MotionOnly(pCurDqLayer, common.LIST_1, &iMvp[common.LIST_1])
		}
		UpdateP16x16MvdCabac(pCurDqLayer, &pMvd, common.LIST_0)
		UpdateP16x16MvdCabac(pCurDqLayer, &pMvd, common.LIST_1)
	} else {
		if bSkipOrDirect {
			var pSubPartCount, pPartW [4]int8
			var pRefIndex [common.LIST_A][30]int8
			for i := int32(0); i < 4; i++ {
				iIdx8 := int16(i << 2)
				iScan4Idx := g_kuiScan4[iIdx8]
				pCurDqLayer.pSubMbType[iMbXy][i] = *subMbType

				mvColoc := &pCurDqLayer.iColocMv[common.LIST_0]

				ref[common.LIST_1] = 0
				UpdateP8x8RefIdxCabac(pCurDqLayer, &pRefIndex, int32(iIdx8), ref[common.LIST_1], common.LIST_1)
				if pCurDqLayer.iColocIntra[iScan4Idx] != 0 {
					ref[common.LIST_0] = 0
					UpdateP8x8RefIdxCabac(pCurDqLayer, &pRefIndex, int32(iIdx8), ref[common.LIST_0], common.LIST_0)
					*iMvp = [common.LIST_A][2]int16{}
				} else {
					ref[common.LIST_0] = 0
					colocRefIndexL0 := pCurDqLayer.iColocRefIndex[common.LIST_0][iScan4Idx]
					if colocRefIndexL0 >= 0 {
						ref[common.LIST_0] = MapColToList0(pCtx, colocRefIndexL0, ref0Count)
					} else {
						mvColoc = &pCurDqLayer.iColocMv[common.LIST_1]
					}
					UpdateP8x8RefIdxCabac(pCurDqLayer, &pRefIndex, int32(iIdx8), ref[common.LIST_0], common.LIST_0)
				}
				UpdateP8x8DirectCabac(pCurDqLayer, int32(iIdx8))

				pSubPartCount[i] = g_ksInterBSubMbTypeInfo[0].iPartCount
				pPartW[i] = g_ksInterBSubMbTypeInfo[0].iPartWidth

				if common.IS_SUB_4x4(*subMbType) {
					pSubPartCount[i] = 4
					pPartW[i] = 1
				}
				FillTemporalDirect8x8Mv(pCurDqLayer, iIdx8, pSubPartCount[i], pPartW[i], *subMbType, ref, mvColoc, nil, nil)
			}
		}
	}
	return ret
}

// PredMv ports void PredMv (int16_t iMotionVector[LIST_A][30][MV_A], int8_t iRefIndex[LIST_A][30],
// int32_t listIdx, int32_t iPartIdx, int32_t iPartWidth, int8_t iRef, int16_t iMVP[2]).
//
// basic iMVs prediction unit for iMVs partition width (4, 2, 1)
func PredMv(iMotionVector *[common.LIST_A][30][common.MV_A]int16, iRefIndex *[common.LIST_A][30]int8, listIdx int32, iPartIdx int32, iPartWidth int32, iRef int8, iMVP *[2]int16) {
	kuiLeftIdx := common.G_kuiCache30ScanIdx[iPartIdx] - 1
	kuiTopIdx := common.G_kuiCache30ScanIdx[iPartIdx] - 6
	kuiRightTopIdx := kuiTopIdx + uint8(iPartWidth)
	kuiLeftTopIdx := kuiTopIdx - 1

	kiLeftRef := iRefIndex[listIdx][kuiLeftIdx]
	kiTopRef := iRefIndex[listIdx][kuiTopIdx]
	kiRightTopRef := iRefIndex[listIdx][kuiRightTopIdx]
	kiLeftTopRef := iRefIndex[listIdx][kuiLeftTopIdx]
	iDiagonalRef := kiRightTopRef

	var iMatchRef int8

	var iAMV, iBMV, iCMV [2]int16

	iAMV = iMotionVector[listIdx][kuiLeftIdx]
	iBMV = iMotionVector[listIdx][kuiTopIdx]
	iCMV = iMotionVector[listIdx][kuiRightTopIdx]

	if REF_NOT_AVAIL == iDiagonalRef {
		iDiagonalRef = kiLeftTopRef
		iCMV = iMotionVector[listIdx][kuiLeftTopIdx]
	}

	iMatchRef = b2i8(iRef == kiLeftRef) + b2i8(iRef == kiTopRef) + b2i8(iRef == iDiagonalRef)

	if REF_NOT_AVAIL == kiTopRef && REF_NOT_AVAIL == iDiagonalRef && kiLeftRef >= REF_NOT_IN_LIST {
		*iMVP = iAMV
		return
	}

	if 1 == iMatchRef {
		if iRef == kiLeftRef {
			*iMVP = iAMV
		} else if iRef == kiTopRef {
			*iMVP = iBMV
		} else {
			*iMVP = iCMV
		}
	} else {
		iMVP[0] = mvMedian(iAMV[0], iBMV[0], iCMV[0])
		iMVP[1] = mvMedian(iAMV[1], iBMV[1], iCMV[1])
	}
}

// PredInter8x16Mv ports void PredInter8x16Mv (int16_t iMotionVector[LIST_A][30][MV_A], int8_t
// iRefIndex[LIST_A][30], int32_t listIdx, int32_t iPartIdx, int8_t iRef, int16_t iMVP[2]).
func PredInter8x16Mv(iMotionVector *[common.LIST_A][30][common.MV_A]int16, iRefIndex *[common.LIST_A][30]int8, listIdx int32, iPartIdx int32, iRef int8, iMVP *[2]int16) {
	if 0 == iPartIdx {
		kiLeftRef := iRefIndex[listIdx][6]
		if iRef == kiLeftRef {
			*iMVP = iMotionVector[listIdx][6]
			return
		}
	} else { // 1 == iPartIdx
		iDiagonalRef := iRefIndex[listIdx][5] //top-right
		index := 5
		if REF_NOT_AVAIL == iDiagonalRef {
			iDiagonalRef = iRefIndex[listIdx][2] //top-left for 8*8 block(index 1)
			index = 2
		}
		if iRef == iDiagonalRef {
			*iMVP = iMotionVector[listIdx][index]
			return
		}
	}

	PredMv(iMotionVector, iRefIndex, listIdx, iPartIdx, 2, iRef, iMVP)
}

// PredInter16x8Mv ports void PredInter16x8Mv (int16_t iMotionVector[LIST_A][30][MV_A], int8_t
// iRefIndex[LIST_A][30], int32_t listIdx, int32_t iPartIdx, int8_t iRef, int16_t iMVP[2]).
func PredInter16x8Mv(iMotionVector *[common.LIST_A][30][common.MV_A]int16, iRefIndex *[common.LIST_A][30]int8, listIdx int32, iPartIdx int32, iRef int8, iMVP *[2]int16) {
	if 0 == iPartIdx {
		kiTopRef := iRefIndex[listIdx][1]
		if iRef == kiTopRef {
			*iMVP = iMotionVector[listIdx][1]
			return
		}
	} else { // 8 == iPartIdx
		kiLeftRef := iRefIndex[listIdx][18]
		if iRef == kiLeftRef {
			*iMVP = iMotionVector[listIdx][18]
			return
		}
	}

	PredMv(iMotionVector, iRefIndex, listIdx, iPartIdx, 4, iRef, iMVP)
}

// UpdateP16x16MotionInfo ports void UpdateP16x16MotionInfo (PDqLayer pCurDqLayer, int32_t
// listIdx, int8_t iRef, int16_t iMVs[2]).
//
// update iMVs and iRefIndex cache for current MB, only for P_16*16 (SKIP inclusive)
func UpdateP16x16MotionInfo(pCurDqLayer *SDqLayer, listIdx int32, iRef int8, iMVs *[2]int16) {
	kiMV32 := *iMVs
	iMbXy := pCurDqLayer.iMbXyIndex
	pMv := mvPredLayerMv(pCurDqLayer, listIdx)
	pRefIndex := mvPredLayerRefIndex(pCurDqLayer, listIdx)

	for i := 0; i < 16; i += 4 {
		//mb
		kuiScan4Idx := g_kuiScan4[i]
		kuiScan4IdxPlus4 := 4 + kuiScan4Idx
		pRef := &pRefIndex[iMbXy]
		pRef[kuiScan4Idx] = iRef
		pRef[kuiScan4Idx+1] = iRef
		pRef[kuiScan4IdxPlus4] = iRef
		pRef[kuiScan4IdxPlus4+1] = iRef

		pMvMb := &pMv[iMbXy]
		pMvMb[kuiScan4Idx] = kiMV32
		pMvMb[1+kuiScan4Idx] = kiMV32
		pMvMb[kuiScan4IdxPlus4] = kiMV32
		pMvMb[1+kuiScan4IdxPlus4] = kiMV32
	}
}

// UpdateP16x16RefIdx ports void UpdateP16x16RefIdx (PDqLayer pCurDqLayer, int32_t listIdx,
// int8_t iRef).
//
// update iRefIndex cache for current MB, only for P_16*16 (SKIP inclusive)
func UpdateP16x16RefIdx(pCurDqLayer *SDqLayer, listIdx int32, iRef int8) {
	iMbXy := pCurDqLayer.iMbXyIndex

	for i := 0; i < 16; i += 4 {
		//mb
		kuiScan4Idx := g_kuiScan4[i]
		kuiScan4IdxPlus4 := 4 + kuiScan4Idx

		pRef := &pCurDqLayer.pDec.pRefIndex[listIdx][iMbXy]
		pRef[kuiScan4Idx] = iRef
		pRef[kuiScan4Idx+1] = iRef
		pRef[kuiScan4IdxPlus4] = iRef
		pRef[kuiScan4IdxPlus4+1] = iRef
	}
}

// UpdateP16x16MotionOnly ports void UpdateP16x16MotionOnly (PDqLayer pCurDqLayer, int32_t
// listIdx, int16_t iMVs[2]).
//
// update iMVs only cache for current MB, only for P_16*16 (SKIP inclusive)
func UpdateP16x16MotionOnly(pCurDqLayer *SDqLayer, listIdx int32, iMVs *[2]int16) {
	kiMV32 := *iMVs
	iMbXy := pCurDqLayer.iMbXyIndex
	pMv := mvPredLayerMv(pCurDqLayer, listIdx)

	for i := 0; i < 16; i += 4 {
		//mb
		kuiScan4Idx := g_kuiScan4[i]
		kuiScan4IdxPlus4 := 4 + kuiScan4Idx
		pMvMb := &pMv[iMbXy]
		pMvMb[kuiScan4Idx] = kiMV32
		pMvMb[1+kuiScan4Idx] = kiMV32
		pMvMb[kuiScan4IdxPlus4] = kiMV32
		pMvMb[1+kuiScan4IdxPlus4] = kiMV32
	}
}

// UpdateP16x8MotionInfo ports void UpdateP16x8MotionInfo (PDqLayer pCurDqLayer, int16_t
// iMotionVector[LIST_A][30][MV_A], int8_t iRefIndex[LIST_A][30], int32_t listIdx, int32_t iPartIdx,
// int8_t iRef, int16_t iMVs[2]).
//
// update iRefIndex and iMVs of Mb, only for P16x8
func UpdateP16x8MotionInfo(pCurDqLayer *SDqLayer, iMotionVector *[common.LIST_A][30][common.MV_A]int16, iRefIndex *[common.LIST_A][30]int8, listIdx int32, iPartIdx int32, iRef int8, iMVs *[2]int16) {
	kiMV32 := *iMVs
	iMbXy := pCurDqLayer.iMbXyIndex
	pMv := mvPredLayerMv(pCurDqLayer, listIdx)
	pRefIndex := mvPredLayerRefIndex(pCurDqLayer, listIdx)
	for i := 0; i < 2; i, iPartIdx = i+1, iPartIdx+4 {
		kuiScan4Idx := g_kuiScan4[iPartIdx]
		kuiScan4IdxPlus4 := 4 + kuiScan4Idx
		kuiCacheIdx := common.G_kuiCache30ScanIdx[iPartIdx]
		kuiCacheIdxPlus6 := 6 + kuiCacheIdx

		//mb
		pRef := &pRefIndex[iMbXy]
		pRef[kuiScan4Idx] = iRef
		pRef[kuiScan4Idx+1] = iRef
		pRef[kuiScan4IdxPlus4] = iRef
		pRef[kuiScan4IdxPlus4+1] = iRef
		pMvMb := &pMv[iMbXy]
		pMvMb[kuiScan4Idx] = kiMV32
		pMvMb[1+kuiScan4Idx] = kiMV32
		pMvMb[kuiScan4IdxPlus4] = kiMV32
		pMvMb[1+kuiScan4IdxPlus4] = kiMV32
		//cache
		iRefIndex[listIdx][kuiCacheIdx] = iRef
		iRefIndex[listIdx][kuiCacheIdx+1] = iRef
		iRefIndex[listIdx][kuiCacheIdxPlus6] = iRef
		iRefIndex[listIdx][kuiCacheIdxPlus6+1] = iRef
		iMotionVector[listIdx][kuiCacheIdx] = kiMV32
		iMotionVector[listIdx][1+kuiCacheIdx] = kiMV32
		iMotionVector[listIdx][kuiCacheIdxPlus6] = kiMV32
		iMotionVector[listIdx][1+kuiCacheIdxPlus6] = kiMV32
	}
}

// UpdateP8x16MotionInfo ports void UpdateP8x16MotionInfo (PDqLayer pCurDqLayer, int16_t
// iMotionVector[LIST_A][30][MV_A], int8_t iRefIndex[LIST_A][30], int32_t listIdx, int32_t iPartIdx,
// int8_t iRef, int16_t iMVs[2]).
//
// update iRefIndex and iMVs of both Mb and Mb_cache, only for P8x16
func UpdateP8x16MotionInfo(pCurDqLayer *SDqLayer, iMotionVector *[common.LIST_A][30][common.MV_A]int16, iRefIndex *[common.LIST_A][30]int8, listIdx int32, iPartIdx int32, iRef int8, iMVs *[2]int16) {
	kiMV32 := *iMVs
	iMbXy := pCurDqLayer.iMbXyIndex
	pMv := mvPredLayerMv(pCurDqLayer, listIdx)
	pRefIndex := mvPredLayerRefIndex(pCurDqLayer, listIdx)

	for i := 0; i < 2; i, iPartIdx = i+1, iPartIdx+8 {
		kuiScan4Idx := g_kuiScan4[iPartIdx]
		kuiCacheIdx := common.G_kuiCache30ScanIdx[iPartIdx]
		kuiScan4IdxPlus4 := 4 + kuiScan4Idx
		kuiCacheIdxPlus6 := 6 + kuiCacheIdx

		//mb
		pRef := &pRefIndex[iMbXy]
		pRef[kuiScan4Idx] = iRef
		pRef[kuiScan4Idx+1] = iRef
		pRef[kuiScan4IdxPlus4] = iRef
		pRef[kuiScan4IdxPlus4+1] = iRef
		pMvMb := &pMv[iMbXy]
		pMvMb[kuiScan4Idx] = kiMV32
		pMvMb[1+kuiScan4Idx] = kiMV32
		pMvMb[kuiScan4IdxPlus4] = kiMV32
		pMvMb[1+kuiScan4IdxPlus4] = kiMV32
		//cache
		iRefIndex[listIdx][kuiCacheIdx] = iRef
		iRefIndex[listIdx][kuiCacheIdx+1] = iRef
		iRefIndex[listIdx][kuiCacheIdxPlus6] = iRef
		iRefIndex[listIdx][kuiCacheIdxPlus6+1] = iRef
		iMotionVector[listIdx][kuiCacheIdx] = kiMV32
		iMotionVector[listIdx][1+kuiCacheIdx] = kiMV32
		iMotionVector[listIdx][kuiCacheIdxPlus6] = kiMV32
		iMotionVector[listIdx][1+kuiCacheIdxPlus6] = kiMV32
	}
}

// fillMv2 does ST64 (p[idx], LD64 (pMV)) for a 4-int16 MV pair (two MVs).
func fillMv2(p *[common.MB_BLOCK4x4_NUM][common.MV_A]int16, idx uint8, mv [2]int16) {
	p[idx] = mv
	p[idx+1] = mv
}

func fillMv2Cache(p *[30][common.MV_A]int16, idx uint8, mv [2]int16) {
	p[idx] = mv
	p[idx+1] = mv
}

// FillSpatialDirect8x8Mv ports void FillSpatialDirect8x8Mv (PDqLayer pCurDqLayer, const int16_t&
// iIdx8, const int8_t& iPartCount, const int8_t& iPartW, const SubMbType& subMbType, const bool&
// bIsLongRef, int16_t pMvDirect[LIST_A][2], int8_t iRef[LIST_A], int16_t
// pMotionVector[LIST_A][30][MV_A], int16_t pMvdCache[LIST_A][30][MV_A]).
//
// const C++ references -> values. pMotionVector / pMvdCache may be nil.
func FillSpatialDirect8x8Mv(pCurDqLayer *SDqLayer, iIdx8 int16, iPartCount int8, iPartW int8, subMbType SubMbType, bIsLongRef bool, pMvDirect *[common.LIST_A][2]int16, iRef *[common.LIST_A]int8, pMotionVector *[common.LIST_A][30][common.MV_A]int16, pMvdCache *[common.LIST_A][30][common.MV_A]int16) {
	iMbXy := pCurDqLayer.iMbXyIndex
	var zero [2]int16
	for j := int32(0); j < int32(iPartCount); j++ {
		iPartIdx := int8(int32(iIdx8) + j*int32(iPartW))
		iScan4Idx := g_kuiScan4[iPartIdx]
		iColocIdx := g_kuiScan4[iPartIdx]
		iCacheIdx := common.G_kuiCache30ScanIdx[iPartIdx]

		if common.IS_SUB_8x8(subMbType) {
			for l := 0; l < common.LIST_A; l++ {
				pMV := pMvDirect[l]
				fillMv2(&pCurDqLayer.pDec.pMv[l][iMbXy], iScan4Idx, pMV)
				fillMv2(&pCurDqLayer.pDec.pMv[l][iMbXy], iScan4Idx+4, pMV)
				fillMv2(&pCurDqLayer.pMvd[l][iMbXy], iScan4Idx, zero)
				fillMv2(&pCurDqLayer.pMvd[l][iMbXy], iScan4Idx+4, zero)
				if pMotionVector != nil {
					fillMv2Cache(&pMotionVector[l], iCacheIdx, pMV)
					fillMv2Cache(&pMotionVector[l], iCacheIdx+6, pMV)
				}
				if pMvdCache != nil {
					fillMv2Cache(&pMvdCache[l], iCacheIdx, zero)
					fillMv2Cache(&pMvdCache[l], iCacheIdx+6, zero)
				}
			}
		} else { //SUB_4x4
			for l := 0; l < common.LIST_A; l++ {
				pMV := pMvDirect[l]
				pCurDqLayer.pDec.pMv[l][iMbXy][iScan4Idx] = pMV
				pCurDqLayer.pMvd[l][iMbXy][iScan4Idx] = zero
				if pMotionVector != nil {
					pMotionVector[l][iCacheIdx] = pMV
				}
				if pMvdCache != nil {
					pMvdCache[l][iCacheIdx] = zero
				}
			}
		}
		if pMvDirect[common.LIST_0] != zero || pMvDirect[common.LIST_1] != zero {
			uiColZeroFlag := (0 == pCurDqLayer.iColocIntra[iColocIdx]) && !bIsLongRef &&
				(pCurDqLayer.iColocRefIndex[common.LIST_0][iColocIdx] == 0 || (pCurDqLayer.iColocRefIndex[common.LIST_0][iColocIdx] < 0 &&
					pCurDqLayer.iColocRefIndex[common.LIST_1][iColocIdx] == 0))
			mvColoc := &pCurDqLayer.iColocMv[common.LIST_1]
			if 0 == pCurDqLayer.iColocRefIndex[common.LIST_0][iColocIdx] {
				mvColoc = &pCurDqLayer.iColocMv[common.LIST_0]
			}
			mv := mvColoc[iColocIdx]
			if common.IS_SUB_8x8(subMbType) {
				if uiColZeroFlag && (mvIsSmall(mv[0]) && mvIsSmall(mv[1])) {
					for l := 0; l < common.LIST_A; l++ {
						if iRef[l] == 0 {
							fillMv2(&pCurDqLayer.pDec.pMv[l][iMbXy], iScan4Idx, zero)
							fillMv2(&pCurDqLayer.pDec.pMv[l][iMbXy], iScan4Idx+4, zero)
							fillMv2(&pCurDqLayer.pMvd[l][iMbXy], iScan4Idx, zero)
							fillMv2(&pCurDqLayer.pMvd[l][iMbXy], iScan4Idx+4, zero)
							if pMotionVector != nil {
								fillMv2Cache(&pMotionVector[l], iCacheIdx, zero)
								fillMv2Cache(&pMotionVector[l], iCacheIdx+6, zero)
							}
							if pMvdCache != nil {
								fillMv2Cache(&pMvdCache[l], iCacheIdx, zero)
								fillMv2Cache(&pMvdCache[l], iCacheIdx+6, zero)
							}
						}
					}
				}
			} else {
				if uiColZeroFlag && (mvIsSmall(mv[0]) && mvIsSmall(mv[1])) {
					for l := 0; l < common.LIST_A; l++ {
						if iRef[l] == 0 {
							pCurDqLayer.pDec.pMv[l][iMbXy][iScan4Idx] = zero
							pCurDqLayer.pMvd[l][iMbXy][iScan4Idx] = zero
							if pMotionVector != nil {
								pMotionVector[l][iCacheIdx] = zero
							}
							if pMvdCache != nil {
								pMvdCache[l][iCacheIdx] = zero
							}
						}
					}
				}
			}
		}
	}
}

// FillTemporalDirect8x8Mv ports void FillTemporalDirect8x8Mv (PDqLayer pCurDqLayer, const
// int16_t& iIdx8, const int8_t& iPartCount, const int8_t& iPartW, const SubMbType& subMbType,
// int8_t iRef[LIST_A], int16_t (*mvColoc)[2], int16_t pMotionVector[LIST_A][30][MV_A], int16_t
// pMvdCache[LIST_A][30][MV_A]).
//
// mvColoc: C int16_t (*)[2] pointing at pCurDqLayer.iColocMv[l] -> &pCurDqLayer.iColocMv[l].
// pMotionVector / pMvdCache may be nil.
func FillTemporalDirect8x8Mv(pCurDqLayer *SDqLayer, iIdx8 int16, iPartCount int8, iPartW int8, subMbType SubMbType, iRef *[common.LIST_A]int8, mvColoc *[16][2]int16, pMotionVector *[common.LIST_A][30][common.MV_A]int16, pMvdCache *[common.LIST_A][30][common.MV_A]int16) {
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	iMbXy := pCurDqLayer.iMbXyIndex
	var pMvDirect [common.LIST_A][2]int16
	var zero [2]int16
	for j := int32(0); j < int32(iPartCount); j++ {
		iPartIdx := int8(int32(iIdx8) + j*int32(iPartW))
		iScan4Idx := g_kuiScan4[iPartIdx]
		iColocIdx := g_kuiScan4[iPartIdx]
		iCacheIdx := common.G_kuiCache30ScanIdx[iPartIdx]

		mv := &mvColoc[iColocIdx]

		if common.IS_SUB_8x8(subMbType) {
			if pCurDqLayer.iColocIntra[iColocIdx] == 0 {
				iMvScale := int32(pSlice.iMvScale[common.LIST_0][iRef[common.LIST_0]])
				pMvDirect[common.LIST_0][0] = int16((iMvScale*int32(mv[0]) + 128) >> 8)
				pMvDirect[common.LIST_0][1] = int16((iMvScale*int32(mv[1]) + 128) >> 8)
			}
			pMV := pMvDirect[common.LIST_0]
			fillMv2(&pCurDqLayer.pDec.pMv[common.LIST_0][iMbXy], iScan4Idx, pMV)
			fillMv2(&pCurDqLayer.pDec.pMv[common.LIST_0][iMbXy], iScan4Idx+4, pMV)
			fillMv2(&pCurDqLayer.pMvd[common.LIST_0][iMbXy], iScan4Idx, zero)
			fillMv2(&pCurDqLayer.pMvd[common.LIST_0][iMbXy], iScan4Idx+4, zero)
			if pMotionVector != nil {
				fillMv2Cache(&pMotionVector[common.LIST_0], iCacheIdx, pMV)
				fillMv2Cache(&pMotionVector[common.LIST_0], iCacheIdx+6, pMV)
			}
			if pMvdCache != nil {
				fillMv2Cache(&pMvdCache[common.LIST_0], iCacheIdx, zero)
				fillMv2Cache(&pMvdCache[common.LIST_0], iCacheIdx+6, zero)
			}
			if pCurDqLayer.iColocIntra[g_kuiScan4[iIdx8]] == 0 {
				pMvDirect[common.LIST_1][0] = int16(int32(pMvDirect[common.LIST_0][0]) - int32(mv[0]))
				pMvDirect[common.LIST_1][1] = int16(int32(pMvDirect[common.LIST_0][1]) - int32(mv[1]))
			}
			pMV = pMvDirect[common.LIST_1]
			fillMv2(&pCurDqLayer.pDec.pMv[common.LIST_1][iMbXy], iScan4Idx, pMV)
			fillMv2(&pCurDqLayer.pDec.pMv[common.LIST_1][iMbXy], iScan4Idx+4, pMV)
			fillMv2(&pCurDqLayer.pMvd[common.LIST_1][iMbXy], iScan4Idx, zero)
			fillMv2(&pCurDqLayer.pMvd[common.LIST_1][iMbXy], iScan4Idx+4, zero)
			if pMotionVector != nil {
				fillMv2Cache(&pMotionVector[common.LIST_1], iCacheIdx, pMV)
				fillMv2Cache(&pMotionVector[common.LIST_1], iCacheIdx+6, pMV)
			}
			if pMvdCache != nil {
				fillMv2Cache(&pMvdCache[common.LIST_1], iCacheIdx, zero)
				fillMv2Cache(&pMvdCache[common.LIST_1], iCacheIdx+6, zero)
			}
		} else { //SUB_4x4
			if pCurDqLayer.iColocIntra[iColocIdx] == 0 {
				iMvScale := int32(pSlice.iMvScale[common.LIST_0][iRef[common.LIST_0]])
				pMvDirect[common.LIST_0][0] = int16((iMvScale*int32(mv[0]) + 128) >> 8)
				pMvDirect[common.LIST_0][1] = int16((iMvScale*int32(mv[1]) + 128) >> 8)
			}
			pCurDqLayer.pDec.pMv[common.LIST_0][iMbXy][iScan4Idx] = pMvDirect[common.LIST_0]
			pCurDqLayer.pMvd[common.LIST_0][iMbXy][iScan4Idx] = zero
			if pMotionVector != nil {
				pMotionVector[common.LIST_0][iCacheIdx] = pMvDirect[common.LIST_0]
			}
			if pMvdCache != nil {
				pMvdCache[common.LIST_0][iCacheIdx] = zero
			}
			if pCurDqLayer.iColocIntra[iColocIdx] == 0 {
				pMvDirect[common.LIST_1][0] = int16(int32(pMvDirect[common.LIST_0][0]) - int32(mv[0]))
				pMvDirect[common.LIST_1][1] = int16(int32(pMvDirect[common.LIST_0][1]) - int32(mv[1]))
			}
			pCurDqLayer.pDec.pMv[common.LIST_1][iMbXy][iScan4Idx] = pMvDirect[common.LIST_1]
			pCurDqLayer.pMvd[common.LIST_1][iMbXy][iScan4Idx] = zero
			if pMotionVector != nil {
				pMotionVector[common.LIST_1][iCacheIdx] = pMvDirect[common.LIST_1]
			}
			if pMvdCache != nil {
				pMvdCache[common.LIST_1][iCacheIdx] = zero
			}
		}
	}
}

// MapColToList0 ports int8_t MapColToList0 (PWelsDecoderContext& pCtx, const int8_t&
// colocRefIndexL0, const int32_t& ref0Count). ISO/IEC 14496-10:2009(E) (8-193)
//
// const C++ references -> values.
func MapColToList0(pCtx *SWelsDecoderContext, colocRefIndexL0 int8, ref0Count int32) int8 {
	//When reference is lost, this function must be skipped.
	if (pCtx.iErrorCode & int32(api.DsRefLost)) == int32(api.DsRefLost) {
		return 0
	}
	pic1 := pCtx.sRefPic.pRefList[common.LIST_1][0]
	if pic1 != nil && pic1.pRefPic[common.LIST_0][colocRefIndexL0] != nil {
		iFramePoc := pic1.pRefPic[common.LIST_0][colocRefIndexL0].iFramePoc
		for i := int32(0); i < ref0Count; i++ {
			if pCtx.sRefPic.pRefList[common.LIST_0][i].iFramePoc == iFramePoc {
				return int8(i)
			}
		}
	}
	return 0
}

// Update8x8RefIdx ports void Update8x8RefIdx (PDqLayer& pCurDqLayer, const int16_t& iPartIdx,
// const int32_t& listIdx, const int8_t& iRef).
//
// const C++ references -> values.
func Update8x8RefIdx(pCurDqLayer *SDqLayer, iPartIdx int16, listIdx int32, iRef int8) {
	iMbXy := pCurDqLayer.iMbXyIndex
	iScan4Idx := g_kuiScan4[iPartIdx]
	pRef := &pCurDqLayer.pDec.pRefIndex[listIdx][iMbXy]
	pRef[iScan4Idx] = iRef
	pRef[iScan4Idx+1] = iRef
	pRef[iScan4Idx+4] = iRef
	pRef[iScan4Idx+5] = iRef
}
