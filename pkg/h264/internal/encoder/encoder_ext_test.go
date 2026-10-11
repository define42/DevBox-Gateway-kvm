package encoder

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

// The C unit tests of test/encoder/EncUT_EncoderExt.cpp exercise the whole
// encoder through the ISVCEncoder interface; the tests below check the
// self-contained helpers of encoder_ext.go against the C semantics.

func TestGetMvMvdRange(t *testing.T) {
	cases := []struct {
		usage     api.EUsageType
		layers    int32
		level     api.ELevelIdc
		mv, mvd   int32
		levelNote string
	}{
		// level unknown -> level 5.2 limits: min(512, 511) = 511 -> min(511, 64)
		{api.CAMERA_VIDEO_REAL_TIME, 1, api.LEVEL_UNKNOWN, CAMERA_STARTMV_RANGE, (CAMERA_STARTMV_RANGE + 1) << 1, "camera 1 layer"},
		{api.CAMERA_VIDEO_REAL_TIME, 2, api.LEVEL_5_2, CAMERA_STARTMV_RANGE, (CAMERA_STARTMV_RANGE + 1) << 1, "camera 2 layers"},
		// level 1.0: [-256, 255] >> 2 -> min(64, 63) = 63, mvd = 128
		{api.SCREEN_CONTENT_REAL_TIME, 1, api.LEVEL_1_0, 63, 128, "screen level 1.0"},
		// level 5.2: 511 -> min(511, 504) = 504, mvd = min(1010, 1010)
		{api.SCREEN_CONTENT_REAL_TIME, 1, api.LEVEL_5_2, EXPANDED_MV_RANGE, EXPANDED_MVD_RANGE, "screen level 5.2"},
	}
	for _, c := range cases {
		p := NewSWelsSvcCodingParam()
		p.IUsageType = c.usage
		p.ISpatialLayerNum = c.layers
		for i := int32(0); i < c.layers; i++ {
			p.SSpatialLayers[i].UiLevelIdc = c.level
		}
		var iMv, iMvd int32
		GetMvMvdRange(p, &iMv, &iMvd)
		if iMv != c.mv || iMvd != c.mvd {
			t.Errorf("%s: got (%d, %d), want (%d, %d)", c.levelNote, iMv, iMvd, c.mv, c.mvd)
		}
	}
}

func TestAllocStrideTables(t *testing.T) {
	p := NewSWelsSvcCodingParam()
	p.ISpatialLayerNum = 2
	p.ITemporalLayerNum = 1
	p.SSpatialLayers[0].IVideoWidth, p.SSpatialLayers[0].IVideoHeight = 160, 96
	p.SSpatialLayers[1].IVideoWidth, p.SSpatialLayers[1].IVideoHeight = 320, 192
	pCtx := &sWelsEncCtx{pSvcParam: p}
	if AllocStrideTables(&pCtx, 2) != 0 {
		t.Fatal("AllocStrideTables failed")
	}
	tab := pCtx.pStrideTab
	dims := [][2]int32{{10, 6}, {20, 12}}
	for d, wh := range dims {
		if len(tab.pMbIndexX[d]) != int(wh[0]*wh[1]) || len(tab.pMbIndexY[d]) != int(wh[0]*wh[1]) {
			t.Fatalf("layer %d: wrong MB index table size", d)
		}
		for y := int32(0); y < wh[1]; y++ {
			for x := int32(0); x < wh[0]; x++ {
				if tab.pMbIndexX[d][y*wh[0]+x] != int16(x) || tab.pMbIndexY[d][y*wh[0]+x] != int16(y) {
					t.Fatalf("layer %d: MB index mismatch at (%d,%d)", d, x, y)
				}
			}
		}
		if len(tab.pStrideEncBlockOffset[d]) != 24 {
			t.Fatalf("layer %d: enc block offset table size %d", d, len(tab.pStrideEncBlockOffset[d]))
		}
		// one temporal layer: only the base temporal (tid == 0) table exists
		if tab.pStrideDecBlockOffset[d][1] == nil || tab.pStrideDecBlockOffset[d][0] != nil {
			t.Fatalf("layer %d: unexpected dec block offset tables", d)
		}
	}
	// luma stride of layer 1: align(align(320,16) + 64, 32) = 384; block 1 is at x offset 4
	if tab.pStrideDecBlockOffset[1][1][1] != 4 {
		t.Errorf("dec block offset[1] = %d, want 4", tab.pStrideDecBlockOffset[1][1][1])
	}
	for d := 2; d < MAX_DEPENDENCY_LAYER; d++ {
		if tab.pMbIndexX[d] != nil || tab.pStrideEncBlockOffset[d] != nil {
			t.Errorf("layer %d should not be allocated", d)
		}
	}
}

func TestUpdateSlicepEncCtxWithPartition(t *testing.T) {
	pDq := &SDqLayer{}
	pDq.sSliceEncCtx.iMbNumInFrame = 100
	pDq.sSliceEncCtx.pOverallMbMap = make([]uint16, 100)
	UpdateSlicepEncCtxWithPartition(pDq, 3)
	if pDq.sSliceEncCtx.iSliceNumInFrame != 3 {
		t.Fatalf("iSliceNumInFrame = %d", pDq.sSliceEncCtx.iSliceNumInFrame)
	}
	wantFirst := []int32{0, 33, 66, 0}
	wantEnd := []int32{32, 65, 99, 0}
	for i := 0; i < MAX_THREADS_NUM; i++ {
		if pDq.FirstMbIdxOfPartition[i] != wantFirst[i] || pDq.EndMbIdxOfPartition[i] != wantEnd[i] {
			t.Errorf("partition %d: [%d, %d]", i, pDq.FirstMbIdxOfPartition[i], pDq.EndMbIdxOfPartition[i])
		}
	}
	for i := 0; i < 100; i++ {
		want := uint16(i / 33)
		if i >= 66 {
			want = 2
		}
		if pDq.sSliceEncCtx.pOverallMbMap[i] != want {
			t.Fatalf("mb %d mapped to %d, want %d", i, pDq.sSliceEncCtx.pOverallMbMap[i], want)
		}
	}
}

func TestInitSliceSettings(t *testing.T) {
	p := NewSWelsSvcCodingParam()
	p.IRCMode = api.RC_OFF_MODE
	p.SSpatialLayers[0].IVideoWidth, p.SSpatialLayers[0].IVideoHeight = 320, 192
	p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
	p.SSpatialLayers[0].SSliceArgument.UiSliceNum = 1
	var iSliceNum int16
	if InitSliceSettings(nil, p, 4, &iSliceNum) != 0 {
		t.Fatal("InitSliceSettings failed")
	}
	if p.IMultipleThreadIdc != 1 || iSliceNum != 1 || p.ILoopFilterDisableIdc != 0 {
		t.Errorf("single slice: threads %d slices %d lf %d", p.IMultipleThreadIdc, iSliceNum, p.ILoopFilterDisableIdc)
	}

	p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
	p.SSpatialLayers[0].SSliceArgument.UiSliceNum = 4
	if InitSliceSettings(nil, p, 4, &iSliceNum) != 0 {
		t.Fatal("InitSliceSettings failed")
	}
	if p.IMultipleThreadIdc != 4 || iSliceNum != 4 || p.ILoopFilterDisableIdc != 2 {
		t.Errorf("fixed slices: threads %d slices %d lf %d", p.IMultipleThreadIdc, iSliceNum, p.ILoopFilterDisableIdc)
	}
	sum := uint32(0)
	for i := 0; i < 4; i++ {
		sum += p.SSpatialLayers[0].SSliceArgument.UiSliceMbNum[i]
	}
	if sum != 240 {
		t.Errorf("slice MB numbers sum to %d, want 240", sum)
	}
}

func TestSliceArgumentValidationFixedSliceModeSingle(t *testing.T) {
	var arg api.SSliceArgument
	arg.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
	arg.UiSliceNum = 4
	arg.UiSliceMbNum[0] = 7
	// 64x64: 16 MBs <= MIN_NUM_MB_PER_SLICE -> single slice
	if SliceArgumentValidationFixedSliceMode(nil, &arg, api.RC_OFF_MODE, 64, 64) != ENC_RETURN_SUCCESS {
		t.Fatal("unexpected failure")
	}
	if arg.UiSliceMode != api.SM_SINGLE_SLICE || arg.UiSliceNum != 1 || arg.UiSliceMbNum[0] != 0 {
		t.Errorf("got mode %d num %d mb0 %d", arg.UiSliceMode, arg.UiSliceNum, arg.UiSliceMbNum[0])
	}
}

func TestWelsBitRateVerification(t *testing.T) {
	var l api.SSpatialLayerConfig
	l.FFrameRate = 30
	l.ISpatialBitrate = 10
	if WelsBitRateVerification(nil, &l, 0) != ENC_RETURN_UNSUPPORTED_PARA {
		t.Error("bitrate below frame rate must be rejected")
	}
	l.ISpatialBitrate = 500000
	l.UiLevelIdc = api.LEVEL_UNKNOWN
	l.IMaxSpatialBitrate = api.UNSPECIFIED_BIT_RATE
	if WelsBitRateVerification(nil, &l, 0) != ENC_RETURN_SUCCESS {
		t.Fatal("unexpected failure")
	}
	if l.IMaxSpatialBitrate != 240000*1200 {
		t.Errorf("iMaxSpatialBitrate = %d", l.IMaxSpatialBitrate)
	}
	l.IMaxSpatialBitrate = 400000
	if WelsBitRateVerification(nil, &l, 0) != ENC_RETURN_UNSUPPORTED_PARA {
		t.Error("max bitrate below bitrate must be rejected")
	}
}

func TestCheckProfileLevelRef(t *testing.T) {
	p := NewSWelsSvcCodingParam()
	CheckProfileSetting(nil, p, 1, api.PRO_BASELINE)
	if p.SSpatialLayers[1].UiProfileIdc != api.PRO_SCALABLE_BASELINE {
		t.Errorf("profile = %d", p.SSpatialLayers[1].UiProfileIdc)
	}
	CheckProfileSetting(nil, p, 0, api.PRO_SCALABLE_BASELINE)
	if p.SSpatialLayers[0].UiProfileIdc != api.PRO_UNKNOWN {
		t.Errorf("profile = %d", p.SSpatialLayers[0].UiProfileIdc)
	}
	CheckLevelSetting(nil, p, 0, api.LEVEL_3_1)
	if p.SSpatialLayers[0].UiLevelIdc != api.LEVEL_3_1 {
		t.Errorf("level = %d", p.SSpatialLayers[0].UiLevelIdc)
	}
	CheckLevelSetting(nil, p, 0, api.ELevelIdc(7))
	if p.SSpatialLayers[0].UiLevelIdc != api.LEVEL_UNKNOWN {
		t.Errorf("level = %d", p.SSpatialLayers[0].UiLevelIdc)
	}
	CheckReferenceNumSetting(nil, p, 100)
	if p.INumRefFrame != api.AUTO_REF_PIC_COUNT {
		t.Errorf("num ref = %d", p.INumRefFrame)
	}
	CheckReferenceNumSetting(nil, p, 2)
	if p.INumRefFrame != 2 {
		t.Errorf("num ref = %d", p.INumRefFrame)
	}
}

func TestGetSubSequenceIdAndTemporalLevel(t *testing.T) {
	pCtx := &sWelsEncCtx{}
	pCtx.uiTemporalId = 2
	if GetSubSequenceId(pCtx, api.VideoFrameTypeIDR) != 0 || GetSubSequenceId(pCtx, api.VideoFrameTypeI) != 1 ||
		GetSubSequenceId(pCtx, api.VideoFrameTypeP) != 5 || GetSubSequenceId(pCtx, api.VideoFrameTypeSkip) != 3+api.MAX_TEMPORAL_LAYER_NUM {
		t.Error("GetSubSequenceId mismatch")
	}
	pCtx.bCurFrameMarkedAsSceneLtr = true
	if GetSubSequenceId(pCtx, api.VideoFrameTypeP) != 2 {
		t.Error("scene LTR sub sequence id")
	}
	var dl SSpatialLayerInternal
	for i := range dl.uiCodingIdx2TemporalId {
		dl.uiCodingIdx2TemporalId[i] = uint8(i)
	}
	if GetTemporalLevel(&dl, 13, 4) != 1 {
		t.Error("GetTemporalLevel mismatch")
	}
}
