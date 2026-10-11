// Port of codec/encoder/core/src/picture_handle.cpp.
//
// Picture pData handling.

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// AllocPicture allocs picture pData with borders for each plane based width
// and height of picture. CMemoryAlign* pMa dropped.
func AllocPicture(kiWidth int32, kiHeight int32, bNeedMbInfo bool, iNeedFeatureStorage int32) *SPicture {
	var pPic *SPicture
	var iPicWidth int32
	var iPicHeight int32

	var iPicChromaWidth int32
	var iPicChromaHeight int32
	var iLumaSize int32
	var iChromaSize int32

	pPic = new(SPicture)

	iPicWidth = common.WELS_ALIGN(kiWidth, common.MB_WIDTH_LUMA) + (common.PADDING_LENGTH << 1)    // with width of horizon
	iPicHeight = common.WELS_ALIGN(kiHeight, common.MB_HEIGHT_LUMA) + (common.PADDING_LENGTH << 1) // with height of vertical
	iPicChromaWidth = iPicWidth >> 1
	iPicChromaHeight = iPicHeight >> 1
	iPicWidth = common.WELS_ALIGN(iPicWidth, 32) // 32(or 16 for chroma below) to match original imp. here instead of cache_line_size
	iPicChromaWidth = common.WELS_ALIGN(iPicChromaWidth, 16)
	iLumaSize = iPicWidth * iPicHeight
	iChromaSize = iPicChromaWidth * iPicChromaHeight

	pPic.pBuffer = make([]uint8, iLumaSize /* luma */ +(iChromaSize<<1) /* Cb,Cr */)
	pPic.iLineSize[0] = iPicWidth
	pPic.iLineSize[2] = iPicChromaWidth
	pPic.iLineSize[1] = iPicChromaWidth
	pPic.pData[0] = pPic.pBuffer
	pPic.pData[1] = pPic.pBuffer
	pPic.pData[2] = pPic.pBuffer
	pPic.iDataOff[0] = int((1 + pPic.iLineSize[0]) * common.PADDING_LENGTH)
	pPic.iDataOff[1] = int(iLumaSize + (((1 + pPic.iLineSize[1]) * common.PADDING_LENGTH) >> 1))
	pPic.iDataOff[2] = int(iLumaSize + iChromaSize + (((1 + pPic.iLineSize[2]) * common.PADDING_LENGTH) >> 1))

	pPic.iWidthInPixel = kiWidth
	pPic.iHeightInPixel = kiHeight
	pPic.iFrameNum = -1

	pPic.bIsLongRef = false
	pPic.iLongTermPicNum = -1
	pPic.uiRecieveConfirmed = 0
	pPic.iMarkFrameNum = -1

	if bNeedMbInfo {
		kuiCountMbNum := uint32(((15 + kiWidth) >> 4) * ((15 + kiHeight) >> 4))

		pPic.uiRefMbType = make([]uint32, kuiCountMbNum)
		pPic.pRefMbQp = make([]uint8, kuiCountMbNum)
		pPic.sMvList = make([]SMVUnitXY, kuiCountMbNum)
		pPic.pMbSkipSad = make([]int32, kuiCountMbNum)
	}

	if iNeedFeatureStorage != 0 {
		pPic.pScreenBlockFeatureStorage = new(SScreenBlockFeatureStorage)
		iReturn := RequestScreenBlockFeatureStorage(kiWidth, kiHeight, iNeedFeatureStorage,
			pPic.pScreenBlockFeatureStorage)
		if ENC_RETURN_SUCCESS != iReturn {
			FreePicture(&pPic)
			return nil
		}
	} else {
		pPic.pScreenBlockFeatureStorage = nil
	}
	return pPic
}

// FreePicture frees picture pData planes.
func FreePicture(ppPic **SPicture) {
	if nil != ppPic && nil != *ppPic {
		pPic := *ppPic

		pPic.pBuffer = nil
		pPic.pData[0] = nil
		pPic.pData[1] = nil
		pPic.pData[2] = nil
		pPic.iDataOff = [3]int{}
		pPic.iLineSize[0] = 0
		pPic.iLineSize[1] = 0
		pPic.iLineSize[2] = 0

		pPic.iWidthInPixel = 0
		pPic.iHeightInPixel = 0
		pPic.iFrameNum = -1

		pPic.bIsLongRef = false
		pPic.uiRecieveConfirmed = 0
		pPic.iLongTermPicNum = -1
		pPic.iMarkFrameNum = -1

		pPic.uiRefMbType = nil
		pPic.pRefMbQp = nil
		pPic.sMvList = nil
		pPic.pMbSkipSad = nil

		if pPic.pScreenBlockFeatureStorage != nil {
			ReleaseScreenBlockFeatureStorage(pPic.pScreenBlockFeatureStorage)
			pPic.pScreenBlockFeatureStorage = nil
		}

		*ppPic = nil
	}
}
