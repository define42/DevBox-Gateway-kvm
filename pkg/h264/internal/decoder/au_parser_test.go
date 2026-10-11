package decoder

import (
	"os"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestDetectStartCodePrefix(t *testing.T) {
	buf := []uint8{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x00, 0x01, 0x68}
	var iOffset int32
	p := DetectStartCodePrefix(buf, &iOffset, int32(len(buf)))
	if p == nil || iOffset != 4 || p[0] != 0x67 {
		t.Fatalf("first start code: off %d", iOffset)
	}
	p = DetectStartCodePrefix(buf[5:], &iOffset, int32(len(buf)-5))
	if p == nil || iOffset != 4 || p[0] != 0x68 {
		t.Fatalf("second start code: off %d", iOffset)
	}
	if DetectStartCodePrefix([]uint8{0, 0, 2, 0, 0}, &iOffset, 5) != nil {
		t.Fatal("found a start code where there is none")
	}
}

func TestGetLevelLimits(t *testing.T) {
	if GetLevelLimits(11, true) != &common.G_ksLevelLimits[1] || GetLevelLimits(11, false) != &common.G_ksLevelLimits[2] ||
		GetLevelLimits(52, false) != &common.G_ksLevelLimits[16] || GetLevelLimits(7, false) != nil {
		t.Fatal("GetLevelLimits")
	}
}

// newParamSetTestCtx builds the minimal decoder context needed by
// ParseNalHeader / ParseNonVclNal for parameter-set NALs.
func newParamSetTestCtx(t *testing.T) *SWelsDecoderContext {
	pCtx := &SWelsDecoderContext{}
	pCtx.pParam = &api.SDecodingParam{}
	pCtx.pParam.EEcActiveIdc = api.ERROR_CON_DISABLE
	pCtx.pDecoderStatistics = &api.SDecoderStatistics{}
	if MemInitNalList(&pCtx.pAccessUnitList, MAX_NAL_UNIT_NUM_IN_AU) != ERR_NONE {
		t.Fatal("MemInitNalList")
	}
	return pCtx
}

// parseParamSets feeds every SPS/PPS NAL of an Annex B file through ParseNalHeader and
// ParseNonVclNal (start code detection and emulation prevention removal are done here,
// as WelsDecodeBs would).
func parseParamSets(t *testing.T, pCtx *SWelsDecoderContext, sFileName string) {
	data, err := os.ReadFile(sFileName)
	if err != nil {
		t.Skipf("bitstream not available: %v", err)
	}
	// split into NALs
	type nal struct{ start, end int } // payload (after the start code)
	var nals []nal
	pos := 0
	for pos < len(data) {
		var iOff int32
		if DetectStartCodePrefix(data[pos:], &iOff, int32(len(data)-pos)) == nil {
			break
		}
		start := pos + int(iOff)
		end := len(data)
		var iNext int32
		if DetectStartCodePrefix(data[start:], &iNext, int32(len(data)-start)) != nil {
			end = start + int(iNext)
			// back off the start code itself (00 00 01 / 00 00 00 01)
			end -= 3
			for end > start && data[end-1] == 0 {
				end--
			}
		}
		nals = append(nals, nal{start, end})
		pos = end
	}
	for _, n := range nals {
		eType := data[n.start] & 0x1f
		if eType != common.NAL_UNIT_SPS && eType != common.NAL_UNIT_PPS && eType != common.NAL_UNIT_SUBSET_SPS {
			continue
		}
		// remove emulation prevention bytes
		raw := make([]uint8, 0, n.end-n.start+8)
		zeros := 0
		for _, b := range data[n.start:n.end] {
			if zeros >= 2 && b == 3 {
				zeros = 0
				continue
			}
			if b == 0 {
				zeros++
			} else {
				zeros = 0
			}
			raw = append(raw, b)
		}
		raw = append(raw, 0, 0, 0, 0) // padding as in the raw buffer
		var iConsumed int32
		pRbsp, iRbspOff := ParseNalHeader(pCtx, &pCtx.sCurNalHead, raw, 0, int32(len(raw)-4), data[n.start-3:], int32(n.end-n.start+3), &iConsumed)
		if pRbsp == nil {
			t.Fatalf("ParseNalHeader failed, error code %#x", pCtx.iErrorCode)
		}
		if r := ParseNonVclNal(pCtx, pRbsp, iRbspOff, int32(len(raw)-4)-iConsumed, data[n.start-3:], int32(n.end-n.start+3)); r != ERR_NONE {
			t.Fatalf("ParseNonVclNal(type %d) = %#x", eType, r)
		}
	}
}

// Parameter-set part of DecoderParseSyntaxTest::TestScalingList (test/decoder/DecUT_ParseSyntax.cpp).
func TestParseScalingListFromBitstream(t *testing.T) {
	iScalingList := [6][16]uint8{
		{17, 17, 16, 16, 17, 16, 15, 15, 16, 15, 15, 15, 16, 15, 15, 15},
		{6, 12, 19, 26, 12, 19, 26, 31, 19, 26, 31, 35, 26, 31, 35, 39},
		{6, 12, 19, 26, 12, 19, 26, 31, 19, 26, 31, 35, 26, 31, 35, 40},
		{17, 17, 16, 16, 17, 16, 15, 15, 16, 15, 15, 15, 16, 15, 15, 14},
		{10, 14, 20, 24, 14, 20, 24, 27, 20, 24, 27, 30, 24, 27, 30, 34},
		{9, 13, 18, 21, 13, 18, 21, 24, 18, 21, 24, 27, 21, 24, 27, 27},
	}
	iScalingListPPS := iScalingList
	var iScalingListZero [6][16]uint8

	// Scalinglist matrix not written into sps or pps
	pCtx := newParamSetTestCtx(t)
	parseParamSets(t, pCtx, "../../../../res/BA_MW_D.264")
	if !pCtx.sSpsPpsCtx.bSpsAvailFlags[0] || !pCtx.sSpsPpsCtx.bPpsAvailFlags[0] {
		t.Fatal("SPS/PPS 0 not parsed")
	}
	if pCtx.sSpsPpsCtx.sSpsBuffer[0].bSeqScalingMatrixPresentFlag {
		t.Fatal("unexpected SPS scaling matrix")
	}
	if pCtx.sSpsPpsCtx.sSpsBuffer[0].iScalingList4x4 != iScalingListZero {
		t.Fatal("SPS scaling list not zero")
	}
	if pCtx.sSpsPpsCtx.sPpsBuffer[0].bPicScalingMatrixPresentFlag {
		t.Fatal("unexpected PPS scaling matrix")
	}
	if pCtx.sSpsPpsCtx.sPpsBuffer[0].iScalingList4x4 != iScalingListZero {
		t.Fatal("PPS scaling list not zero")
	}
	if w, h := pCtx.sSpsPpsCtx.sSpsBuffer[0].iMbWidth, pCtx.sSpsPpsCtx.sSpsBuffer[0].iMbHeight; w != 11 || h != 9 {
		t.Fatalf("BA_MW_D size %dx%d MBs", w, h)
	}

	// Scalinglist value just written into sps and pps
	pCtx = newParamSetTestCtx(t)
	parseParamSets(t, pCtx, "../../../../res/test_scalinglist_jm.264")
	if !pCtx.sSpsPpsCtx.sSpsBuffer[0].bSeqScalingMatrixPresentFlag {
		t.Fatal("SPS scaling matrix missing")
	}
	for i := 0; i < 6; i++ {
		if pCtx.sSpsPpsCtx.sSpsBuffer[0].iScalingList4x4[i] != iScalingList[i] {
			t.Errorf("SPS list %d = %v", i, pCtx.sSpsPpsCtx.sSpsBuffer[0].iScalingList4x4[i])
		}
	}
	if !pCtx.sSpsPpsCtx.sPpsBuffer[0].bPicScalingMatrixPresentFlag {
		t.Fatal("PPS scaling matrix missing")
	}
	for i := 0; i < 6; i++ {
		if pCtx.sSpsPpsCtx.sPpsBuffer[0].iScalingList4x4[i] != iScalingListPPS[i] {
			t.Errorf("PPS list %d = %v", i, pCtx.sSpsPpsCtx.sPpsBuffer[0].iScalingList4x4[i])
		}
	}
}

func TestParseParamSetsParseOnly(t *testing.T) {
	pCtx := newParamSetTestCtx(t)
	pCtx.pParam.BParseOnly = true
	parseParamSets(t, pCtx, "../../../../res/BA_MW_D.264")
	sps := &pCtx.sSpsBsInfo[0]
	if sps.uiSpsBsLen < 5 || sps.pSpsBsBuf[0] != 0 || sps.pSpsBsBuf[1] != 0 || sps.pSpsBsBuf[2] != 0 || sps.pSpsBsBuf[3] != 1 ||
		sps.pSpsBsBuf[4]&0x1f != common.NAL_UNIT_SPS {
		t.Fatalf("SPS bs info % x (len %d)", sps.pSpsBsBuf[:8], sps.uiSpsBsLen)
	}
	pps := &pCtx.sPpsBsInfo[0]
	if pps.uiPpsBsLen < 5 || pps.pPpsBsBuf[3] != 1 || pps.pPpsBsBuf[4]&0x1f != common.NAL_UNIT_PPS {
		t.Fatalf("PPS bs info % x (len %d)", pps.pPpsBsBuf[:8], pps.uiPpsBsLen)
	}
}
