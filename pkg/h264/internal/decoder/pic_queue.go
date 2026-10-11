// Port of codec/decoder/core/src/pic_queue.cpp.
//
// Recycled queue management for pictures.

package decoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

///////////////////////////////////Recycled queue management for pictures///////////////////////////////////
/*   ______________________________________
 -->| P0 | P1 | P2 | P3 | P4 | .. | Pn-1 |-->
    --------------------------------------
*
*  How does it work?
*  node <- next; ++ next;
*
*/

// AllocPicture ports PPicture AllocPicture (PWelsDecoderContext pCtx, const int32_t kiPicWidth,
// const int32_t kiPicHeight).
//
// Sets pBuffer[0..2] and pData[0..2] to the single allocation and iDataOff[i] to the plane origins
// (see SPicture).
func AllocPicture(pCtx *SWelsDecoderContext, kiPicWidth int32, kiPicHeight int32) *SPicture {
	var pPic *SPicture
	var iPicWidth int32
	var iPicHeight int32

	var iPicChromaWidth int32
	var iPicChromaHeight int32
	var iLumaSize int32
	var iChromaSize int32

	pPic = new(SPicture)

	iPicWidth = common.WELS_ALIGN(kiPicWidth+(common.PADDING_LENGTH<<1), PICTURE_RESOLUTION_ALIGNMENT)
	iPicHeight = common.WELS_ALIGN(kiPicHeight+(common.PADDING_LENGTH<<1), PICTURE_RESOLUTION_ALIGNMENT)
	iPicChromaWidth = iPicWidth >> 1
	iPicChromaHeight = iPicHeight >> 1

	iLumaSize = iPicWidth * iPicHeight
	iChromaSize = iPicChromaWidth * iPicChromaHeight

	if pCtx.pParam.BParseOnly {
		pPic.pBuffer[0], pPic.pBuffer[1], pPic.pBuffer[2] = nil, nil, nil
		pPic.pData[0], pPic.pData[1], pPic.pData[2] = nil, nil, nil
		pPic.iLinesize[0] = iPicWidth
		pPic.iLinesize[1] = iPicChromaWidth
		pPic.iLinesize[2] = iPicChromaWidth
	} else {
		pBuf := make([]uint8, iLumaSize /* luma */ +(iChromaSize<<1) /* Cb,Cr */)
		for i := range pBuf {
			pBuf[i] = 128
		}
		pPic.iLinesize[0] = iPicWidth
		pPic.iLinesize[1] = iPicChromaWidth
		pPic.iLinesize[2] = iPicChromaWidth
		pPic.pBuffer[0] = pBuf
		pPic.pBuffer[1] = pBuf // C: pBuffer[0] + iLumaSize
		pPic.pBuffer[2] = pBuf // C: pBuffer[1] + iChromaSize
		pPic.pData[0] = pBuf
		pPic.pData[1] = pBuf
		pPic.pData[2] = pBuf
		pPic.iDataOff[0] = int((1 + pPic.iLinesize[0]) * common.PADDING_LENGTH)
		pPic.iDataOff[1] = int(iLumaSize) + int(((1+pPic.iLinesize[1])*common.PADDING_LENGTH)>>1)
		pPic.iDataOff[2] = int(iLumaSize+iChromaSize) + int(((1+pPic.iLinesize[2])*common.PADDING_LENGTH)>>1)
	}
	pPic.iPlanes = 3 // yv12 in default
	pPic.iWidthInPixel = kiPicWidth
	pPic.iHeightInPixel = kiPicHeight
	pPic.iFrameNum = -1
	pPic.iRefCount = 0
	pPic.pSetUnRef = nil

	uiMbWidth := uint32(kiPicWidth+15) >> 4
	uiMbHeight := uint32(kiPicHeight+15) >> 4
	uiMbCount := uiMbWidth * uiMbHeight

	pPic.pMbCorrectlyDecodedFlag = make([]bool, uiMbCount)
	if GetThreadCount(pCtx) > 1 {
		pPic.pNzc = make([][24]int8, uiMbCount)
	} else {
		pPic.pNzc = nil
	}
	pPic.pMbType = make([]uint32, uiMbCount)
	pPic.pMv[common.LIST_0] = make([][common.MB_BLOCK4x4_NUM][common.MV_A]int16, uiMbCount)
	pPic.pMv[common.LIST_1] = make([][common.MB_BLOCK4x4_NUM][common.MV_A]int16, uiMbCount)
	pPic.pRefIndex[common.LIST_0] = make([][common.MB_BLOCK4x4_NUM]int8, uiMbCount)
	pPic.pRefIndex[common.LIST_1] = make([][common.MB_BLOCK4x4_NUM]int8, uiMbCount)
	// Single-threaded port: pCtx.pThreadCtx is always nil, so no ready events.
	pPic.pReadyEvent = nil

	return pPic
}

// FreePicture ports void FreePicture (PPicture pPic, CMemoryAlign* pMa).
//
// The C CMemoryAlign* pMa parameter is dropped.
func FreePicture(pPic *SPicture) {
	if pPic != nil {
		if pPic.pBuffer[0] != nil {
			pPic.pBuffer[0] = nil
			pPic.pBuffer[1] = nil
			pPic.pBuffer[2] = nil
			pPic.pData[0] = nil
			pPic.pData[1] = nil
			pPic.pData[2] = nil
		}

		pPic.pMbCorrectlyDecodedFlag = nil
		pPic.pNzc = nil
		pPic.pMbType = nil

		for listIdx := common.LIST_0; listIdx < common.LIST_A; listIdx++ {
			pPic.pMv[listIdx] = nil
			pPic.pRefIndex[listIdx] = nil
		}
		pPic.pReadyEvent = nil
	}
}

// PrefetchPic ports PPicture PrefetchPic (PPicBuff pPicBuf).
func PrefetchPic(pPicBuf *SPicBuff) *SPicture {
	var iPicIdx int32
	var pPic *SPicture

	if pPicBuf.iCapacity == 0 {
		return nil
	}

	for iPicIdx = pPicBuf.iCurrentIdx + 1; iPicIdx < pPicBuf.iCapacity; iPicIdx++ {
		if pPicBuf.ppPic[iPicIdx] != nil && !pPicBuf.ppPic[iPicIdx].bUsedAsRef &&
			pPicBuf.ppPic[iPicIdx].iRefCount <= 0 {
			pPic = pPicBuf.ppPic[iPicIdx]
			break
		}
	}
	if pPic != nil {
		pPicBuf.iCurrentIdx = iPicIdx
		pPic.iPicBuffIdx = iPicIdx
		return pPic
	}
	for iPicIdx = 0; iPicIdx <= pPicBuf.iCurrentIdx; iPicIdx++ {
		if pPicBuf.ppPic[iPicIdx] != nil && !pPicBuf.ppPic[iPicIdx].bUsedAsRef &&
			pPicBuf.ppPic[iPicIdx].iRefCount <= 0 {
			pPic = pPicBuf.ppPic[iPicIdx]
			break
		}
	}

	pPicBuf.iCurrentIdx = iPicIdx
	if pPic != nil {
		pPic.iPicBuffIdx = iPicIdx
	}
	return pPic
}

// PrefetchPicForThread ports PPicture PrefetchPicForThread (PPicBuff pPicBuf).
func PrefetchPicForThread(pPicBuf *SPicBuff) *SPicture {
	var pPic *SPicture

	if pPicBuf.iCapacity == 0 {
		return nil
	}
	pPic = pPicBuf.ppPic[pPicBuf.iCurrentIdx]
	pPic.iPicBuffIdx = pPicBuf.iCurrentIdx
	pPicBuf.iCurrentIdx++
	if pPicBuf.iCurrentIdx >= pPicBuf.iCapacity {
		pPicBuf.iCurrentIdx = 0
	}
	return pPic
}

// PrefetchLastPicForThread ports PPicture PrefetchLastPicForThread (PPicBuff pPicBuf, const
// int32_t& iLastPicBuffIdx).
func PrefetchLastPicForThread(pPicBuf *SPicBuff, iLastPicBuffIdx int32) *SPicture {
	var pPic *SPicture

	if pPicBuf.iCapacity == 0 {
		return nil
	}
	if iLastPicBuffIdx >= 0 && iLastPicBuffIdx < pPicBuf.iCapacity {
		pPic = pPicBuf.ppPic[iLastPicBuffIdx]
	}
	return pPic
}
