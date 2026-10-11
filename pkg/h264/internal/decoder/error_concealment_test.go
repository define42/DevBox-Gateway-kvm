// Port of test/decoder/DecUT_ErrorConcealment.cpp (C paths only).

package decoder

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

const (
	ecMaxMbWidth  = 260
	ecMaxMbHeight = 130
)

type SECInputCtx struct {
	iMbWidth                int32
	iMbHeight               int32
	iLinesize               [3]uint32
	pMbCorrectlyDecodedFlag []bool //actual memory
	pCtx                    *SWelsDecoderContext
	sDqLayer                SDqLayer
	sAncPic                 SPicture //Anc picture for comparison
	sSrcPic                 SPicture //Src picture as common input picture data
	sWelsPic                SPicture //Wels picture to be compared
	sLastDecPicInfo         SWelsLastDecPicInfo
}

// ecSetupPic lays out the three I420 planes contiguously in one buffer
// (pData[i] = whole buffer, iDataOff[i] = plane start), like the C test.
func ecSetupPic(pPic *SPicture, kiLumaSize int) {
	buf := make([]uint8, kiLumaSize*3/2)
	for i := 0; i < 3; i++ {
		pPic.pData[i] = buf
	}
	pPic.iDataOff[0] = 0
	pPic.iDataOff[1] = kiLumaSize
	pPic.iDataOff[2] = kiLumaSize + (kiLumaSize >> 2)
}

func InitAndAllocInputData(rnd *rand.Rand) *SECInputCtx {
	pECCtx := &SECInputCtx{}

	pECCtx.iMbWidth = rnd.Int31()%(ecMaxMbWidth-1) + 1   //give a constrained max width
	pECCtx.iMbHeight = rnd.Int31()%(ecMaxMbHeight-1) + 1 //give a constrained max height
	pECCtx.iLinesize[0] = uint32(pECCtx.iMbWidth << 4)
	pECCtx.iLinesize[1] = pECCtx.iLinesize[0] >> 1
	pECCtx.iLinesize[2] = pECCtx.iLinesize[1]

	kiLumaSize := int(pECCtx.iMbWidth * pECCtx.iMbHeight * 256)

	//allocate picture data
	ecSetupPic(&pECCtx.sWelsPic, kiLumaSize)
	ecSetupPic(&pECCtx.sAncPic, kiLumaSize)
	ecSetupPic(&pECCtx.sSrcPic, kiLumaSize)

	for _, p := range []*SPicture{&pECCtx.sWelsPic, &pECCtx.sAncPic, &pECCtx.sSrcPic} {
		for i := 0; i < 3; i++ {
			p.iLinesize[i] = int32(pECCtx.iLinesize[i])
		}
		p.iWidthInPixel = pECCtx.iMbWidth << 4
		p.iHeightInPixel = pECCtx.iMbHeight << 4
	}

	pECCtx.pMbCorrectlyDecodedFlag = make([]bool, pECCtx.iMbWidth*pECCtx.iMbHeight)

	pECCtx.pCtx = &SWelsDecoderContext{}
	pECCtx.pCtx.pDec = &pECCtx.sWelsPic
	pECCtx.pCtx.pCurDqLayer = &pECCtx.sDqLayer
	pECCtx.pCtx.pCurDqLayer.pMbCorrectlyDecodedFlag = pECCtx.pMbCorrectlyDecodedFlag
	pECCtx.pCtx.pLastDecPicInfo = &pECCtx.sLastDecPicInfo
	pECCtx.pCtx.pSps = &SSps{}
	pECCtx.pCtx.pSps.iMbWidth = uint32(pECCtx.iMbWidth)
	pECCtx.pCtx.pSps.iMbHeight = uint32(pECCtx.iMbHeight)
	pECCtx.pCtx.pParam = &api.SDecodingParam{}

	return pECCtx
}

func InitECCopyData(rnd *rand.Rand, pECCtx *SECInputCtx) {
	kiMbNum := int(pECCtx.iMbWidth * pECCtx.iMbHeight)
	//init pMbCorrectlyDecodedFlag
	for i := 0; i < kiMbNum; i++ {
		pECCtx.pMbCorrectlyDecodedFlag[i] = (rnd.Int31() & 1) != 0
	}
	//init Data
	iPixNum := kiMbNum * 256 * 3 / 2
	for i := 0; i < iPixNum; i++ {
		pECCtx.sSrcPic.pData[0][i] = uint8(rnd.Int31() & 0xff)
	}
	pECCtx.pCtx.uiCpuFlag = 0
	InitErrorCon(pECCtx.pCtx)
}

func DoAncErrorConSliceCopy(pECCtx *SECInputCtx) {
	iMbWidth := pECCtx.iMbWidth
	iMbHeight := pECCtx.iMbHeight
	pDstPic := &pECCtx.sAncPic
	pSrcPic := pECCtx.pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb
	if (pECCtx.pCtx.pParam.EEcActiveIdc == api.ERROR_CON_SLICE_COPY) &&
		(pECCtx.pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt.BIdrFlag) {
		pSrcPic = nil
	}

	pMbCorrectlyDecodedFlag := pECCtx.pMbCorrectlyDecodedFlag

	iSrcStride := int(pECCtx.iLinesize[0])
	iDstStride := int(pECCtx.iLinesize[0])
	for iMbY := 0; iMbY < int(iMbHeight); iMbY++ {
		for iMbX := 0; iMbX < int(iMbWidth); iMbX++ {
			iMbXyIndex := iMbY*int(iMbWidth) + iMbX
			if !pMbCorrectlyDecodedFlag[iMbXyIndex] {
				type plane struct{ idx, n, dstStride, srcStride, mbSize int }
				planes := []plane{{0, 16, iDstStride, iSrcStride, 16}, {1, 8, iDstStride / 2, iSrcStride / 2, 8}, {2, 8, iDstStride / 2, iSrcStride / 2, 8}}
				for _, pl := range planes {
					d := pDstPic.iDataOff[pl.idx] + iMbY*pl.mbSize*pl.dstStride + iMbX*pl.mbSize
					var s int
					if pSrcPic != nil {
						s = pSrcPic.iDataOff[pl.idx] + iMbY*pl.mbSize*pl.srcStride + iMbX*pl.mbSize
					}
					for i := 0; i < pl.n; i++ {
						if pSrcPic != nil {
							copy(pDstPic.pData[pl.idx][d:d+pl.n], pSrcPic.pData[pl.idx][s:s+pl.n])
							s += pl.srcStride
						} else {
							for k := 0; k < pl.n; k++ {
								pDstPic.pData[pl.idx][d+k] = 128
							}
						}
						d += pl.dstStride
					}
				}
			} //!pMbCorrectlyDecodedFlag[iMbXyIndex]
		} //iMbX
	} //iMbY
}

func ComparePictureDataI420(pSrcData []uint8, pDstData []uint8, kiStride uint32, kiHeight int32) bool {
	n := int(kiStride)*int(kiHeight) + 2*int(kiStride>>1)*int(kiHeight/2)
	return bytes.Equal(pSrcData[:n], pDstData[:n])
}

func IsPictureFilledWithValueI420(pData []uint8, kiStride uint32, kiHeight int32, kuiValue uint8) bool {
	iLumaSize := int(kiStride) * int(kiHeight)
	iChromaSize := int(kiStride>>1) * int(kiHeight>>1)

	for i := 0; i < iLumaSize; i++ {
		if pData[i] != kuiValue {
			return false
		}
	}
	for i := 0; i < iChromaSize; i++ {
		if pData[iLumaSize+i] != kuiValue || pData[iLumaSize+iChromaSize+i] != kuiValue {
			return false
		}
	}
	return true
}

func TestErrorConTest_DoErrorConFrameCopy(t *testing.T) {
	rnd := rand.New(rand.NewSource(11))
	pECCtx := InitAndAllocInputData(rnd)

	for iEC := 0; iEC < 2; iEC++ { //ERROR_CON_FRAME_COPY, ERROR_CON_FRAME_COPY_CROSS_IDR
		if iEC > 0 {
			pECCtx.pCtx.pParam.EEcActiveIdc = api.ERROR_CON_FRAME_COPY_CROSS_IDR
		} else {
			pECCtx.pCtx.pParam.EEcActiveIdc = api.ERROR_CON_FRAME_COPY
		}
		InitECCopyData(rnd, pECCtx)
		iLumaSize := int(pECCtx.iMbWidth * pECCtx.iMbHeight * 256)

		for iRef := 0; iRef < 2; iRef++ { //no ref, with ref
			if iRef != 0 {
				pECCtx.pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb = &pECCtx.sSrcPic
			} else {
				pECCtx.pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb = nil
			}
			for iIDR := 0; iIDR < 2; iIDR++ { //non IDR, IDR
				pECCtx.pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt.BIdrFlag = (iIDR > 0)
				//Do reference code method
				DoErrorConFrameCopy(pECCtx.pCtx)
				//Do anchor method
				if iRef != 0 && !((pECCtx.pCtx.pParam.EEcActiveIdc == api.ERROR_CON_FRAME_COPY) &&
					(pECCtx.pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt.BIdrFlag)) {
					copy(pECCtx.sAncPic.pData[0][:iLumaSize*3/2], pECCtx.sSrcPic.pData[0][:iLumaSize*3/2])
				} else {
					for i := 0; i < iLumaSize*3/2; i++ {
						pECCtx.sAncPic.pData[0][i] = 128 //should be the same as known EC method, here all 128
					}
				}
				//Compare results
				if !ComparePictureDataI420(pECCtx.sAncPic.pData[0], pECCtx.sWelsPic.pData[0], pECCtx.iLinesize[0],
					pECCtx.iMbHeight*16) {
					t.Errorf("mismatch iEC=%d iRef=%d iIDR=%d", iEC, iRef, iIDR)
				}
			} //non IDR, IDR
		} // no ref, with ref
	} //FRAME_COPY methods
}

func TestErrorConTest_DoErrorConSliceCopy(t *testing.T) {
	rnd := rand.New(rand.NewSource(12))
	pECCtx := InitAndAllocInputData(rnd)

	for iEC := 0; iEC < 2; iEC++ { //ERROR_CON_SLICE_COPY, ERROR_CON_SLICE_COPY_CROSS_IDR
		if iEC > 0 {
			pECCtx.pCtx.pParam.EEcActiveIdc = api.ERROR_CON_SLICE_COPY_CROSS_IDR
		} else {
			pECCtx.pCtx.pParam.EEcActiveIdc = api.ERROR_CON_SLICE_COPY
		}
		InitECCopyData(rnd, pECCtx)
		for iRef := 0; iRef < 2; iRef++ { //no ref, with ref
			if iRef != 0 {
				pECCtx.pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb = &pECCtx.sSrcPic
			} else {
				pECCtx.pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb = nil
			}
			for iIDR := 0; iIDR < 2; iIDR++ { //non IDR, IDR
				pECCtx.pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt.BIdrFlag = (iIDR > 0)
				//Do reference code method
				DoErrorConSliceCopy(pECCtx.pCtx)
				//Do anchor method
				DoAncErrorConSliceCopy(pECCtx)
				//Compare results
				if !ComparePictureDataI420(pECCtx.sAncPic.pData[0], pECCtx.sWelsPic.pData[0], pECCtx.iLinesize[0],
					pECCtx.iMbHeight*16) {
					t.Errorf("mismatch iEC=%d iRef=%d iIDR=%d", iEC, iRef, iIDR)
				}
			} //non IDR, IDR
		} // no ref, with ref
	} //SLICE_COPY methods
}

func TestErrorConTest_DoErrorConFrameCopyResolutionMismatchFallsBackToFill(t *testing.T) {
	rnd := rand.New(rand.NewSource(13))
	pECCtx := InitAndAllocInputData(rnd)

	pECCtx.pCtx.pParam.EEcActiveIdc = api.ERROR_CON_FRAME_COPY_CROSS_IDR
	InitECCopyData(rnd, pECCtx)
	pECCtx.pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt.BIdrFlag = false
	pECCtx.pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb = &pECCtx.sSrcPic

	pECCtx.sSrcPic.iWidthInPixel += 16 // use += so value stays > 0 even when iMbWidth==1
	n := int(pECCtx.iMbWidth * pECCtx.iMbHeight * 256 * 3 / 2)
	for i := 0; i < n; i++ {
		pECCtx.sWelsPic.pData[0][i] = 7
	}

	DoErrorConFrameCopy(pECCtx.pCtx)

	if !IsPictureFilledWithValueI420(pECCtx.sWelsPic.pData[0], uint32(pECCtx.sWelsPic.iLinesize[0]),
		pECCtx.sWelsPic.iHeightInPixel, 128) {
		t.Errorf("picture not filled with 128")
	}
}

func TestErrorConTest_DoErrorConSliceCopyResolutionMismatchFallsBackToFill(t *testing.T) {
	rnd := rand.New(rand.NewSource(14))
	pECCtx := InitAndAllocInputData(rnd)

	pECCtx.pCtx.pParam.EEcActiveIdc = api.ERROR_CON_SLICE_COPY_CROSS_IDR
	InitECCopyData(rnd, pECCtx)
	pECCtx.pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt.BIdrFlag = false
	pECCtx.pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb = &pECCtx.sSrcPic

	for i := range pECCtx.pMbCorrectlyDecodedFlag {
		pECCtx.pMbCorrectlyDecodedFlag[i] = false
	}
	pECCtx.sSrcPic.iHeightInPixel += 16 // use += so value stays > 0 even when iMbHeight==1
	n := int(pECCtx.iMbWidth * pECCtx.iMbHeight * 256 * 3 / 2)
	for i := 0; i < n; i++ {
		pECCtx.sWelsPic.pData[0][i] = 7
	}

	DoErrorConSliceCopy(pECCtx.pCtx)

	if !IsPictureFilledWithValueI420(pECCtx.sWelsPic.pData[0], uint32(pECCtx.sWelsPic.iLinesize[0]),
		pECCtx.sWelsPic.iHeightInPixel, 128) {
		t.Errorf("picture not filled with 128")
	}
}
