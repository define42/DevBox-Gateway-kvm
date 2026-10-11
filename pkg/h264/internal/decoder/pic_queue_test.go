// Tests for pic_queue.go (no C counterpart in test/decoder).

package decoder

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestPicQueue_AllocPictureLayout(t *testing.T) {
	ctx := &SWelsDecoderContext{pParam: &api.SDecodingParam{}}
	pPic := AllocPicture(ctx, 176, 144)
	iPicWidth := common.WELS_ALIGN(int32(176+2*common.PADDING_LENGTH), PICTURE_RESOLUTION_ALIGNMENT)
	iPicHeight := common.WELS_ALIGN(int32(144+2*common.PADDING_LENGTH), PICTURE_RESOLUTION_ALIGNMENT)
	iLumaSize := int(iPicWidth * iPicHeight)
	iChromaSize := iLumaSize / 4
	if pPic.iLinesize[0] != iPicWidth || pPic.iLinesize[1] != iPicWidth/2 || pPic.iLinesize[2] != iPicWidth/2 {
		t.Fatalf("linesizes %v", pPic.iLinesize)
	}
	if len(pPic.pBuffer[0]) != iLumaSize+2*iChromaSize {
		t.Fatalf("buffer size %d", len(pPic.pBuffer[0]))
	}
	if pPic.iDataOff[0] != int((1+iPicWidth)*common.PADDING_LENGTH) ||
		pPic.iDataOff[1] != iLumaSize+int((1+iPicWidth/2)*common.PADDING_LENGTH/2) ||
		pPic.iDataOff[2] != iLumaSize+iChromaSize+int((1+iPicWidth/2)*common.PADDING_LENGTH/2) {
		t.Fatalf("data offsets %v", pPic.iDataOff)
	}
	if pPic.pBuffer[0][0] != 128 || pPic.iFrameNum != -1 || pPic.iPlanes != 3 {
		t.Fatalf("bad init")
	}
	if len(pPic.pMbType) != 11*9 || len(pPic.pMv[1]) != 11*9 || len(pPic.pRefIndex[0]) != 11*9 || pPic.pNzc != nil {
		t.Fatalf("bad per-MB arrays")
	}
	FreePicture(pPic)
	if pPic.pBuffer[0] != nil || pPic.pMbType != nil {
		t.Fatalf("FreePicture did not release")
	}
}

func TestPicQueue_PrefetchPic(t *testing.T) {
	pics := make([]*SPicture, 3)
	for i := range pics {
		pics[i] = &SPicture{}
	}
	buf := &SPicBuff{ppPic: pics, iCapacity: 3, iCurrentIdx: 0}
	if p := PrefetchPic(buf); p != pics[1] || buf.iCurrentIdx != 1 || p.iPicBuffIdx != 1 {
		t.Fatalf("first prefetch")
	}
	pics[2].bUsedAsRef = true
	if p := PrefetchPic(buf); p != pics[0] || buf.iCurrentIdx != 0 {
		t.Fatalf("wrap prefetch")
	}
	pics[0].iRefCount = 1
	pics[1].bUsedAsRef = true
	if p := PrefetchPic(buf); p != nil || buf.iCurrentIdx != 1 {
		t.Fatalf("full prefetch: idx %d", buf.iCurrentIdx)
	}
	if PrefetchLastPicForThread(buf, 2) != pics[2] || PrefetchLastPicForThread(buf, 3) != nil {
		t.Fatalf("PrefetchLastPicForThread")
	}
	buf.iCurrentIdx = 2
	if p := PrefetchPicForThread(buf); p != pics[2] || buf.iCurrentIdx != 0 {
		t.Fatalf("PrefetchPicForThread")
	}
}
