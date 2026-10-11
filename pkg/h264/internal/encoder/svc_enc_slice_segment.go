// Port of codec/encoder/core/src/svc_enc_slice_segment.cpp.
//
// SSlice segment routine (Single/multiple slices) for SVC encoder.

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// AssignMbMapSingleSlice assigns the MB map for single slice segment.
// pMbMap: void* (SSliceCtx.pOverallMbMap) -> []uint16; kiMapUnitSize is sizeof(element).
func AssignMbMapSingleSlice(pMbMap []uint16, kiCountMbNum int32, kiMapUnitSize int32) int32 {
	if nil == pMbMap || kiCountMbNum <= 0 {
		return 1
	}

	clear(pMbMap[:kiCountMbNum])

	return 0
}

// AssignMbMapMultipleSlices assigns the MB map for multiple slice(s) segment.
func AssignMbMapMultipleSlices(pCurDq *SDqLayer, kpSliceArgument *api.SSliceArgument) int32 {
	pSliceSeg := &pCurDq.sSliceEncCtx
	iSliceIdx := int32(0)
	if api.SM_SINGLE_SLICE == pSliceSeg.uiSliceMode {
		return 1
	}

	if (api.SM_RASTER_SLICE == pSliceSeg.uiSliceMode) && (0 == kpSliceArgument.UiSliceMbNum[0]) {
		kiMbWidth := int32(pSliceSeg.iMbWidth)
		iSliceNum := pSliceSeg.iSliceNumInFrame

		iSliceIdx = 0
		for iSliceIdx < iSliceNum {
			kiFirstMb := iSliceIdx * kiMbWidth
			common.WelsSetMemMultiplebytes_c(pSliceSeg.pOverallMbMap[kiFirstMb:], uint32(iSliceIdx), kiMbWidth, 2)
			iSliceIdx++
		}

		return 0
	} else if api.SM_RASTER_SLICE == pSliceSeg.uiSliceMode ||
		api.SM_FIXEDSLCNUM_SLICE == pSliceSeg.uiSliceMode {
		kpSlicesAssignList := &kpSliceArgument.UiSliceMbNum
		kiCountNumMbInFrame := pSliceSeg.iMbNumInFrame
		kiCountSliceNumInFrame := pSliceSeg.iSliceNumInFrame
		iMbIdx := int32(0)

		iSliceIdx = 0
		for {
			kiCurRunLength := int32(kpSlicesAssignList[iSliceIdx])
			iRunIdx := int32(0)

			// due here need check validate mb_assign_map for input pData, can not use memset
			for {
				pSliceSeg.pOverallMbMap[iMbIdx+iRunIdx] = uint16(iSliceIdx)
				iRunIdx++
				if !(iRunIdx < kiCurRunLength && iMbIdx+iRunIdx < kiCountNumMbInFrame) {
					break
				}
			}

			iMbIdx += kiCurRunLength
			iSliceIdx++
			if !(iSliceIdx < kiCountSliceNumInFrame && iMbIdx < kiCountNumMbInFrame) {
				break
			}
		}
	} else if api.SM_SIZELIMITED_SLICE == pSliceSeg.uiSliceMode {
		// do nothing,pSliceSeg->pOverallMbMap will be initial later
	} else { // any else uiSliceMode?
		// assert (0)
	}

	// extention for other multiple slice type in the future
	return 1
}

// CheckFixedSliceNumMultiSliceSetting checks the slice parameters for SM_FIXEDSLCNUM_SLICE.
func CheckFixedSliceNumMultiSliceSetting(kiMbNumInFrame int32, pSliceArg *api.SSliceArgument) bool {
	pSlicesAssignList := &pSliceArg.UiSliceMbNum
	kuiSliceNum := pSliceArg.UiSliceNum
	uiSliceIdx := uint32(0)
	kiMbNumPerSlice := int32(uint32(kiMbNumInFrame) / kuiSliceNum)
	iNumMbLeft := kiMbNumInFrame

	for ; uiSliceIdx+1 < kuiSliceNum; uiSliceIdx++ {
		pSlicesAssignList[uiSliceIdx] = uint32(kiMbNumPerSlice)
		iNumMbLeft -= kiMbNumPerSlice
	}

	pSlicesAssignList[uiSliceIdx] = uint32(iNumMbLeft)

	if iNumMbLeft <= 0 || kiMbNumPerSlice <= 0 {
		return false
	}

	return true
}

// CheckRowMbMultiSliceSetting checks the slice parameters for SM_ROWMB_SLICE.
func CheckRowMbMultiSliceSetting(kiMbWidth int32, pSliceArg *api.SSliceArgument) bool {
	pSlicesAssignList := &pSliceArg.UiSliceMbNum
	kuiSliceNum := pSliceArg.UiSliceNum
	uiSliceIdx := uint32(0)

	for uiSliceIdx < kuiSliceNum {
		pSlicesAssignList[uiSliceIdx] = uint32(kiMbWidth)
		uiSliceIdx++
	}
	return true
}

// CheckRasterMultiSliceSetting checks the slice parameters for SM_RASTER_SLICE.
func CheckRasterMultiSliceSetting(kiMbNumInFrame int32, pSliceArg *api.SSliceArgument) bool {
	pSlicesAssignList := &pSliceArg.UiSliceMbNum // C: viewed as int32_t*
	iActualSliceCount := int32(0)

	//check mb_num setting
	uiSliceIdx := uint32(0)
	iCountMb := int32(0)

	for (uiSliceIdx < MAX_SLICES_NUM) && (0 < int32(pSlicesAssignList[uiSliceIdx])) {
		iCountMb += int32(pSlicesAssignList[uiSliceIdx])
		iActualSliceCount = int32(uiSliceIdx + 1)

		if iCountMb >= kiMbNumInFrame {
			break
		}

		uiSliceIdx++
	}
	//break condition above makes, after the while
	// here must have (iActualSliceCount <= MAX_SLICES_NUM)

	//correction if needed
	if iCountMb == kiMbNumInFrame {
	} else if iCountMb > kiMbNumInFrame {
		//need correction:
		//setting is more than iMbNumInFrame,
		//cut the last uiSliceMbNum; adjust iCountMb
		pSlicesAssignList[iActualSliceCount-1] = uint32(int32(pSlicesAssignList[iActualSliceCount-1]) - (iCountMb - kiMbNumInFrame))
		iCountMb = kiMbNumInFrame
	} else if iActualSliceCount < MAX_SLICES_NUM {
		//where ( iCountMb < iMbNumInFrame )
		//can do correction:
		//  make the last uiSliceMbNum the left num
		pSlicesAssignList[iActualSliceCount] = uint32(kiMbNumInFrame - iCountMb)
		iActualSliceCount += 1
	} else {
		//here ( iCountMb < iMbNumInFrame ) && ( iActualSliceCount == MAX_SLICES_NUM )
		//no more slice can be added
		return false
	}

	pSliceArg.UiSliceNum = uint32(iActualSliceCount)
	return true
}

func gomSizeOf(kiMbWidth int32) int32 {
	// The default RC is Bit-rate mode[Yi], but need consider as below:
	// Tuned to use max of mode0 and mode1 due can not refresh on this from rc mode changed outside, 8/16/2011
	// NOTE: GOM_ROW_MODE0_?P is integer multipler of GOM_ROW_MODE1_?P, which predefined at rc.h there, so GOM_ROM take MODE0 as the initial
	if kiMbWidth <= MB_WIDTH_THRESHOLD_90P {
		return kiMbWidth * GOM_ROW_MODE0_90P
	} else if kiMbWidth <= MB_WIDTH_THRESHOLD_180P {
		return kiMbWidth * GOM_ROW_MODE0_180P
	} else if kiMbWidth <= MB_WIDTH_THRESHOLD_360P {
		return kiMbWidth * GOM_ROW_MODE0_360P
	}
	return kiMbWidth * GOM_ROW_MODE0_720P
}

// GomValidCheckSliceNum is the GOM based RC related check for uiSliceNum
// decision, only used at SM_FIXEDSLCNUM_SLICE.
func GomValidCheckSliceNum(kiMbWidth int32, kiMbHeight int32, pSliceNum *uint32) bool {
	kiCountNumMb := kiMbWidth * kiMbHeight
	iSliceNum := *pSliceNum
	iGomSize := gomSizeOf(kiMbWidth)

	for {
		if kiCountNumMb < iGomSize*int32(iSliceNum) {
			iSliceNum--
			iSliceNum = iSliceNum - (iSliceNum & 0x01) // verfiy even num for multiple slices case
			if iSliceNum < 2 {                         // for safe
				break
			}
			continue
		}
		break
	}

	if *pSliceNum != iSliceNum {
		if 0 != iSliceNum {
			*pSliceNum = iSliceNum
		} else {
			*pSliceNum = 1
		}
		return false
	}
	return true
}

// GomValidCheckSliceMbNum is the GOM based RC related check for uiSliceMbNum
// decision, only used at SM_FIXEDSLCNUM_SLICE.
func GomValidCheckSliceMbNum(kiMbWidth int32, kiMbHeight int32, pSliceArg *api.SSliceArgument) bool {
	pSlicesAssignList := &pSliceArg.UiSliceMbNum
	kuiSliceNum := pSliceArg.UiSliceNum
	kiMbNumInFrame := kiMbWidth * kiMbHeight
	kiMbNumPerSlice := int32(uint32(kiMbNumInFrame) / kuiSliceNum)
	iNumMbLeft := kiMbNumInFrame

	var iMinimalMbNum int32 // in theory we need only 1 SMB, here let it as one SMB row required
	var iMaximalMbNum int32 // dynamically assign later
	var iGomSize int32

	uiSliceIdx := uint32(0) // for test

	iGomSize = gomSizeOf(kiMbWidth)
	// GOM boundary aligned
	iNumMbAssigning := common.WELS_DIV_ROUND(INT_MULTIPLY*kiMbNumPerSlice, iGomSize*INT_MULTIPLY) * iGomSize
	iCurNumMbAssigning := int32(0)

	iMinimalMbNum = iGomSize
	// Ensure that the minimum macroblock requirement across all slices does not
	// exceed total frame capacity, preventing negative calculations for remaining
	// macroblocks.
	if iMinimalMbNum*int32(kuiSliceNum) > kiMbNumInFrame {
		return false
	}
	for uiSliceIdx+1 < kuiSliceNum {
		// C: iNumMbLeft - (kuiSliceNum - uiSliceIdx - 1) * iMinimalMbNum evaluated in uint32
		iMaximalMbNum = int32(uint32(iNumMbLeft) - (kuiSliceNum-uiSliceIdx-1)*uint32(iMinimalMbNum)) // get maximal num_mb in left parts
		// make sure one GOM at least in each slice for safe
		if iNumMbAssigning < iMinimalMbNum {
			iCurNumMbAssigning = iMinimalMbNum
		} else if iNumMbAssigning > iMaximalMbNum {
			iCurNumMbAssigning = (iMaximalMbNum / iGomSize) * iGomSize
		} else {
			iCurNumMbAssigning = iNumMbAssigning
		}

		if iCurNumMbAssigning <= 0 {
			return false
		}

		iNumMbLeft -= iCurNumMbAssigning
		if iNumMbLeft <= 0 {
			return false
		}

		pSlicesAssignList[uiSliceIdx] = uint32(iCurNumMbAssigning)
		uiSliceIdx++
	}
	pSlicesAssignList[uiSliceIdx] = uint32(iNumMbLeft)
	if iNumMbLeft < iMinimalMbNum {
		return false
	}

	return true
}

// GetInitialSliceNum gets the slice count for multiple slice segment.
func GetInitialSliceNum(pSliceArgument *api.SSliceArgument) int32 {
	if nil == pSliceArgument {
		return -1
	}

	switch pSliceArgument.UiSliceMode {
	case api.SM_SINGLE_SLICE, api.SM_FIXEDSLCNUM_SLICE, api.SM_RASTER_SLICE:
		return int32(pSliceArgument.UiSliceNum)
	case api.SM_SIZELIMITED_SLICE:
		return AVERSLICENUM_CONSTRAINT //at the beginning of dynamic slicing, set the uiSliceNum to be 1
	default:
		return -1
	}
}

// InitSliceSegment initializes slice segment (Single/multiple slices).
// CMemoryAlign* pMa dropped.
func InitSliceSegment(pCurDq *SDqLayer, pSliceArgument *api.SSliceArgument, kiMbWidth int32, kiMbHeight int32) int32 {
	pSliceSeg := &pCurDq.sSliceEncCtx
	kiCountMbNum := kiMbWidth * kiMbHeight
	uiSliceMode := api.SM_SINGLE_SLICE

	if nil == pSliceArgument || kiMbWidth == 0 || kiMbHeight == 0 {
		return 1
	}

	uiSliceMode = pSliceArgument.UiSliceMode
	if pSliceSeg.iMbNumInFrame == kiCountMbNum && int32(pSliceSeg.iMbWidth) == kiMbWidth &&
		int32(pSliceSeg.iMbHeight) == kiMbHeight && pSliceSeg.uiSliceMode == uiSliceMode && pSliceSeg.pOverallMbMap != nil {
		return 0
	} else if pSliceSeg.iMbNumInFrame != kiCountMbNum {
		if nil != pSliceSeg.pOverallMbMap {
			pSliceSeg.pOverallMbMap = nil
		}

		// just for safe
		pSliceSeg.iSliceNumInFrame = 0
		pSliceSeg.iMbNumInFrame = 0
		pSliceSeg.iMbWidth = 0
		pSliceSeg.iMbHeight = 0
		pSliceSeg.uiSliceMode = api.SM_SINGLE_SLICE // sigle in default
	}

	if api.SM_SINGLE_SLICE == uiSliceMode {
		pSliceSeg.pOverallMbMap = make([]uint16, kiCountMbNum)

		pSliceSeg.iSliceNumInFrame = 1

		pSliceSeg.uiSliceMode = uiSliceMode
		pSliceSeg.iMbWidth = int16(kiMbWidth)
		pSliceSeg.iMbHeight = int16(kiMbHeight)
		pSliceSeg.iMbNumInFrame = kiCountMbNum

		return AssignMbMapSingleSlice(pSliceSeg.pOverallMbMap, kiCountMbNum, 2)
	}
	//if ( SM_MULTIPLE_SLICE == uiSliceMode )
	if uiSliceMode != api.SM_FIXEDSLCNUM_SLICE && uiSliceMode != api.SM_RASTER_SLICE &&
		uiSliceMode != api.SM_SIZELIMITED_SLICE {
		return 1
	}

	pSliceSeg.pOverallMbMap = make([]uint16, kiCountMbNum)

	common.WelsSetMemMultiplebytes_c(pSliceSeg.pOverallMbMap, 0, kiCountMbNum, 2)

	//SM_SIZELIMITED_SLICE: init, set pSliceSeg->iSliceNumInFrame = 1;
	pSliceSeg.iSliceNumInFrame = GetInitialSliceNum(pSliceArgument)
	if -1 == pSliceSeg.iSliceNumInFrame {
		return 1
	}

	pSliceSeg.uiSliceMode = pSliceArgument.UiSliceMode

	pSliceSeg.iMbWidth = int16(kiMbWidth)
	pSliceSeg.iMbHeight = int16(kiMbHeight)
	pSliceSeg.iMbNumInFrame = kiCountMbNum
	if api.SM_SIZELIMITED_SLICE == pSliceArgument.UiSliceMode {
		if 0 < pSliceArgument.UiSliceSizeConstraint {
			pSliceSeg.uiSliceSizeConstraint = pSliceArgument.UiSliceSizeConstraint
		} else {
			return 1
		}
	} else {
		pSliceSeg.uiSliceSizeConstraint = DEFAULT_MAXPACKETSIZE_CONSTRAINT
	}
	// about "iMaxSliceNumConstraint"
	//only used in SM_SIZELIMITED_SLICE mode so far,
	//now follows NAL_UNIT_CONSTRAINT, (see definition)
	//will be adjusted under MT if there is limitation on iLayerNum
	pSliceSeg.iMaxSliceNumConstraint = MAX_SLICES_NUM

	return AssignMbMapMultipleSlices(pCurDq, pSliceArgument)
}

// UninitSliceSegment uninitializes slice segment (Single/multiple slices).
func UninitSliceSegment(pCurDq *SDqLayer) {
	pSliceSeg := &pCurDq.sSliceEncCtx
	pSliceSeg.pOverallMbMap = nil

	pSliceSeg.uiSliceMode = api.SM_SINGLE_SLICE // single in default
	pSliceSeg.iMbWidth = 0
	pSliceSeg.iMbHeight = 0
	pSliceSeg.iSliceNumInFrame = 0
	pSliceSeg.iMbNumInFrame = 0
	pSliceSeg.uiSliceSizeConstraint = 0
	pSliceSeg.iMaxSliceNumConstraint = 0
}

// InitSlicePEncCtx initializes the Wels SSlice context (Single/multiple slices and FMO).
// CMemoryAlign* pMa dropped; pPpsArg: void* that is a SWelsPPS*.
func InitSlicePEncCtx(pCurDq *SDqLayer, bFmoUseFlag bool, iMbWidth int32, iMbHeight int32, pSliceArgument *api.SSliceArgument, pPpsArg *SWelsPPS) int32 {
	if nil == pCurDq {
		return 1
	}

	InitSliceSegment(pCurDq, pSliceArgument, iMbWidth, iMbHeight)
	return 0
}

// UninitSlicePEncCtx uninitializes the Wels SSlice context.
func UninitSlicePEncCtx(pCurDq *SDqLayer) {
	if nil != pCurDq {
		UninitSliceSegment(pCurDq)
	}
}

// WelsMbToSliceIdc gets the slice idc for given iMbXY.
func WelsMbToSliceIdc(pCurDq *SDqLayer, kiMbXY int32) uint16 {
	if nil == pCurDq {
		return 0xffff
	}

	pSliceCtx := &pCurDq.sSliceEncCtx
	if kiMbXY < pSliceCtx.iMbNumInFrame && kiMbXY >= 0 {
		return pSliceCtx.pOverallMbMap[kiMbXY]
	}
	return 0xffff
}

// WelsGetFirstMbOfSlice gets the first mb in slice/slice_group: uiSliceIdc.
func WelsGetFirstMbOfSlice(pCurLayer *SDqLayer, kuiSliceIdc int32) int32 {
	if nil == pCurLayer || nil == pCurLayer.pFirstMbIdxOfSlice {
		return -1
	}

	return pCurLayer.pFirstMbIdxOfSlice[kuiSliceIdc]
}

// WelsGetNextMbOfSlice gets the successive mb to be processed in slice/slice_group.
func WelsGetNextMbOfSlice(pCurDq *SDqLayer, kiMbXY int32) int32 {
	if nil != pCurDq {
		pSliceSeg := &pCurDq.sSliceEncCtx
		if kiMbXY < 0 || kiMbXY >= pSliceSeg.iMbNumInFrame {
			return -1
		}
		if api.SM_SINGLE_SLICE == pSliceSeg.uiSliceMode {
			iNextMbIdx := kiMbXY
			iNextMbIdx++
			if iNextMbIdx >= pSliceSeg.iMbNumInFrame {
				iNextMbIdx = -1
			}
			return iNextMbIdx
		}
		/*if ( SM_MULTIPLE_SLICE == pSliceSeg->uiSliceMode )*/
		if api.SM_RESERVED != pSliceSeg.uiSliceMode {
			iNextMbIdx := kiMbXY
			iNextMbIdx++
			if iNextMbIdx < pSliceSeg.iMbNumInFrame && pSliceSeg.pOverallMbMap != nil &&
				pSliceSeg.pOverallMbMap[iNextMbIdx] == pSliceSeg.pOverallMbMap[kiMbXY] {
				return iNextMbIdx
			}
			return -1
		}
		return -1 // reserved here for other multiple slice type
	}
	return -1
}

// WelsGetPrevMbOfSlice gets the previous mb to be processed in slice/slice_group.
func WelsGetPrevMbOfSlice(pCurDq *SDqLayer, kiMbXY int32) int32 {
	if nil != pCurDq {
		pSliceSeg := &pCurDq.sSliceEncCtx
		if kiMbXY < 0 || kiMbXY >= pSliceSeg.iMbNumInFrame {
			return -1
		}
		if pSliceSeg.uiSliceMode == api.SM_SINGLE_SLICE {
			return -1 + kiMbXY
		}
		/* if ( pSliceSeg->uiSliceMode == SM_MULTIPLE_SLICE )*/
		if api.SM_RESERVED == pSliceSeg.uiSliceMode {
			iPrevMbIdx := kiMbXY
			iPrevMbIdx--
			if iPrevMbIdx >= 0 && iPrevMbIdx < pSliceSeg.iMbNumInFrame && nil != pSliceSeg.pOverallMbMap &&
				pSliceSeg.pOverallMbMap[kiMbXY] == pSliceSeg.pOverallMbMap[iPrevMbIdx] {
				return iPrevMbIdx
			}
			return -1
		}
		return -1
	}
	return -1
}

// WelsGetNumMbInSlice gets the number of mb in slice/slice_group: uiSliceIdc.
func WelsGetNumMbInSlice(pCurDq *SDqLayer, pSlice *SSlice, kuiSliceIdc int32) int32 {
	pSliceCtx := &pCurDq.sSliceEncCtx
	bInValidFlag := false

	if nil == pSlice || kuiSliceIdc < 0 {
		return -1
	}

	bInValidFlag = ((api.SM_SINGLE_SLICE != pSliceCtx.uiSliceMode) && (kuiSliceIdc >= pSliceCtx.iSliceNumInFrame)) ||
		((api.SM_SINGLE_SLICE == pSliceCtx.uiSliceMode) && (kuiSliceIdc > 0))
	if bInValidFlag {
		return -1
	}

	return pSlice.iCountMbNumInSlice
}

// GetCurrentSliceNum returns the slice count in frame of the layer.
func GetCurrentSliceNum(pCurDq *SDqLayer) int32 {
	return pCurDq.sSliceEncCtx.iSliceNumInFrame
}

// DynamicAdjustSlicePEncCtxAll re-assigns the slice partitions from run lengths.
func DynamicAdjustSlicePEncCtxAll(pCurDq *SDqLayer, pRunLength []int32) int32 {
	pSliceCtx := &pCurDq.sSliceEncCtx
	iCountNumMbInFrame := pSliceCtx.iMbNumInFrame
	iCountSliceNumInFrame := pSliceCtx.iSliceNumInFrame
	iSameRunLenFlag := int32(1)
	iFirstMbIdx := int32(0)
	iSliceIdx := int32(0)

	// assert (iCountSliceNumInFrame <= MAX_THREADS_NUM)

	for iSliceIdx < iCountSliceNumInFrame {
		if pRunLength[iSliceIdx] != pCurDq.pFirstMbIdxOfSlice[iSliceIdx] {
			iSameRunLenFlag = 0
			break
		}
		iSliceIdx++
	}
	if iSameRunLenFlag != 0 {
		return 1 // do not need adjust it due to same running length as before to save complexity
	}

	iSliceIdx = 0
	for {
		kiSliceRun := pRunLength[iSliceIdx]
		pCurDq.pFirstMbIdxOfSlice[iSliceIdx] = iFirstMbIdx
		pCurDq.pCountMbNumInSlice[iSliceIdx] = kiSliceRun

		common.WelsSetMemMultiplebytes_c(pSliceCtx.pOverallMbMap[iFirstMbIdx:], uint32(iSliceIdx), kiSliceRun, 2)

		iFirstMbIdx += kiSliceRun

		iSliceIdx++
		if !(iSliceIdx < iCountSliceNumInFrame && iFirstMbIdx < iCountNumMbInFrame) {
			break
		}
	}

	return 0
}

// DynamicMaxSliceNumConstraint computes the maximal slice number constraint.
func DynamicMaxSliceNumConstraint(uiMaximumNum uint32, iConsumedNum int32, iDulplicateTimes uint32) int32 {
	return int32((uiMaximumNum - uint32(iConsumedNum) - 1) / iDulplicateTimes)
}
