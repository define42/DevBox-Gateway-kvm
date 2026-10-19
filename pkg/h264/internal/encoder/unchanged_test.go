package encoder

import (
	"bytes"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestUnchangedConfigurationFallbacks(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*SWelsSvcCodingParam)
	}{
		{name: "screen mode", change: func(p *SWelsSvcCodingParam) { p.IUsageType = api.SCREEN_CONTENT_REAL_TIME }},
		{name: "multiple spatial layers", change: func(p *SWelsSvcCodingParam) { p.ISpatialLayerNum = 2 }},
		{name: "multiple temporal layers", change: func(p *SWelsSvcCodingParam) { p.ITemporalLayerNum = 2 }},
		{name: "multiple references", change: func(p *SWelsSvcCodingParam) { p.INumRefFrame = 2 }},
		{name: "rate control", change: func(p *SWelsSvcCodingParam) { p.IRCMode = api.RC_BITRATE_MODE }},
		{name: "complex mode", change: func(p *SWelsSvcCodingParam) { p.IComplexityMode = api.HIGH_COMPLEXITY }},
		{name: "CABAC", change: func(p *SWelsSvcCodingParam) { p.IEntropyCodingModeFlag = 1 }},
		{name: "deblocking", change: func(p *SWelsSvcCodingParam) { p.ILoopFilterDisableIdc = 0 }},
		{name: "within slice deblocking", change: func(p *SWelsSvcCodingParam) { p.ILoopFilterDisableIdc = 2 }},
		{name: "denoising", change: func(p *SWelsSvcCodingParam) { p.BEnableDenoise = true }},
		{name: "adaptive QP", change: func(p *SWelsSvcCodingParam) { p.BEnableAdaptiveQuant = true }},
		{name: "background detection", change: func(p *SWelsSvcCodingParam) { p.BEnableBackgroundDetection = true }},
		{name: "LTR", change: func(p *SWelsSvcCodingParam) { p.BEnableLongTermReference = true }},
		{name: "simulcast", change: func(p *SWelsSvcCodingParam) { p.BSimulcastAVC = true }},
		{name: "dynamic slices", change: func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SIZELIMITED_SLICE
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			param := unchangedTestParams()
			if !unchangedConfiguration(param) {
				t.Fatal("test's supported baseline was rejected")
			}
			test.change(param)
			if unchangedConfiguration(param) {
				t.Fatal("unsupported configuration enabled unchanged-block encoding")
			}
		})
	}
}

func TestUnchangedMaskValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*sWelsEncCtx, *api.SSourcePicture)
	}{
		{name: "nil mask", change: func(_ *sWelsEncCtx, s *api.SSourcePicture) { s.UnchangedMacroblocks = nil }},
		{name: "short mask", change: func(_ *sWelsEncCtx, s *api.SSourcePicture) {
			s.UnchangedMacroblocks = []bool{true}
		}},
		{name: "oversized mask", change: func(_ *sWelsEncCtx, s *api.SSourcePicture) {
			s.UnchangedMacroblocks = make([]bool, 5)
		}},
		{name: "wrong geometry", change: func(_ *sWelsEncCtx, s *api.SSourcePicture) { s.IPicWidth = 16 }},
		{name: "scaled output", change: func(c *sWelsEncCtx, _ *api.SSourcePicture) {
			c.pSvcParam.sDependencyLayers[0].iActualWidth = 16
		}},
		{name: "no successful reference", change: func(c *sWelsEncCtx, _ *api.SSourcePicture) {
			c.unchanged.reference = nil
		}},
		{name: "changed QP", change: func(c *sWelsEncCtx, _ *api.SSourcePicture) {
			c.pSvcParam.SSpatialLayers[0].IDLayerQp++
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, _, _ := unchangedTestMacroblock()
			source := unchangedTestSource()
			test.change(ctx, source)
			ctx.unchanged.begin(ctx, source)
			if ctx.unchanged.macroblocks != nil {
				t.Fatal("invalid mask/configuration was accepted")
			}
		})
	}
}

func TestUnchangedStateInvalidatedAfterFailureOrSkip(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		success bool
		kind    api.EVideoFrameType
	}{
		{name: "encode error", kind: api.VideoFrameTypeP},
		{name: "skipped picture", success: true, kind: api.VideoFrameTypeSkip},
		{name: "invalid picture", success: true, kind: api.VideoFrameTypeInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, _, _ := unchangedTestMacroblock()
			source := unchangedTestSource()
			ctx.unchanged.begin(ctx, source)
			output := &api.SFrameBSInfo{EFrameType: test.kind, IFrameSizeInBytes: 100}
			ctx.unchanged.finish(ctx, source, output, test.success)
			if ctx.unchanged.reference != nil || ctx.unchanged.macroblocks != nil {
				t.Fatal("failed/skipped call retained reference or borrowed mask")
			}
			ctx.unchanged.begin(ctx, source)
			if ctx.unchanged.macroblocks != nil {
				t.Fatal("next call reused an uncertain reference chain")
			}
		})
	}
}

func TestUnchangedMacroblockCopiesReconstruction(t *testing.T) {
	t.Parallel()
	ctx, slice, mb := unchangedTestMacroblock()
	// P_SKIP inherits the preceding syntax QP, even when it differs from the
	// fixed QP initially assigned to this MB. With deblocking off, reconstruction
	// still has to preserve the reference samples exactly.
	slice.uiLastMbQp = 23
	ctx.unchanged.begin(ctx, unchangedTestSource())
	var md SWelsMD
	if !tryUnchangedMacroblock(ctx, &md, slice, mb) {
		t.Fatal("eligible unchanged block did not use skip coding")
	}
	cache := &slice.sMbCacheInfo
	for plane := range 3 {
		size := 8
		if plane == 0 {
			size = 16
		}
		for row := range size {
			ref := cache.SPicData.iRefMbOff[plane] + row*int(ctx.pCurDqLayer.pRefPic.iLineSize[plane])
			rec := cache.SPicData.iCsMbOff[plane] + row*int(ctx.pCurDqLayer.iCsStride[plane])
			if !bytes.Equal(cache.SPicData.pRefMb[plane][ref:ref+size], cache.SPicData.pCsMb[plane][rec:rec+size]) {
				t.Fatalf("plane %d row %d did not preserve the reconstructed reference", plane, row)
			}
		}
	}
	if mb.uiMbType != common.MB_TYPE_SKIP || mb.uiCbp != 0 || mb.uiLumaQp != slice.uiLastMbQp ||
		!cache.bCollocatedPredFlag || md.iCostLuma != 17 || md.iCostSkipMb != 29 {
		t.Fatalf("incomplete skip metadata: mb=%+v md=%+v", mb, md)
	}
	for _, count := range mb.pNonZeroCount[:24] {
		if count != 0 {
			t.Fatal("skip retained residual coefficients")
		}
	}
	for _, mv := range mb.sMv {
		if mv != (SMVUnitXY{}) {
			t.Fatal("skip retained a motion vector")
		}
	}
}

func TestUnchangedMacroblockFallbacks(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*sWelsEncCtx, *SSlice, *SMB)
	}{
		{name: "changed block", change: func(c *sWelsEncCtx, _ *SSlice, m *SMB) {
			c.unchanged.macroblocks[m.iMbXY] = false
		}},
		{name: "IDR", change: func(c *sWelsEncCtx, _ *SSlice, _ *SMB) { c.eSliceType = common.I_SLICE }},
		{name: "reference replaced", change: func(c *sWelsEncCtx, _ *SSlice, _ *SMB) {
			c.unchanged.reference = &SPicture{}
		}},
		{name: "reference recycled", change: func(c *sWelsEncCtx, _ *SSlice, _ *SMB) { c.unchanged.poc++ }},
		{name: "other active reference", change: func(c *sWelsEncCtx, _ *SSlice, _ *SMB) { c.pRefList0[0] = nil }},
		{name: "multiple active references", change: func(c *sWelsEncCtx, _ *SSlice, _ *SMB) { c.iNumRef0 = 2 }},
		{name: "deblocking", change: func(c *sWelsEncCtx, _ *SSlice, _ *SMB) { c.pCurDqLayer.iLoopFilterDisableIdc = 2 }},
		{name: "QP changed", change: func(_ *sWelsEncCtx, _ *SSlice, m *SMB) { m.uiLumaQp++ }},
		{name: "reference intra", change: func(c *sWelsEncCtx, _ *SSlice, m *SMB) {
			c.pCurDqLayer.pRefPic.uiRefMbType[m.iMbXY] = common.MB_TYPE_INTRA16x16
		}},
		{name: "reference motion", change: func(c *sWelsEncCtx, _ *SSlice, m *SMB) {
			c.pCurDqLayer.pRefPic.sMvList[m.iMbXY].iMvX = 4
		}},
		{name: "predicted motion", change: func(_ *sWelsEncCtx, s *SSlice, _ *SMB) {
			for i := range s.sMbCacheInfo.sMvComponents.sMotionVectorCache {
				s.sMbCacheInfo.sMvComponents.sMotionVectorCache[i] = SMVUnitXY{iMvX: 4, iMvY: 4}
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, slice, mb := unchangedTestMacroblock()
			ctx.unchanged.begin(ctx, unchangedTestSource())
			test.change(ctx, slice, mb)
			if tryUnchangedMacroblock(ctx, &SWelsMD{}, slice, mb) {
				t.Fatal("uncertain block bypassed normal coding")
			}
		})
	}
}

func TestUnchangedHintsReachEncoderAndReleaseBorrow(t *testing.T) {
	t.Parallel()
	codec := NewCWelsH264SVCEncoder()
	defer codec.Uninitialize()
	param := unchangedTestParams().SEncParamExt
	param.FMaxFrameRate = 60
	param.SSpatialLayers[0].IVideoWidth, param.SSpatialLayers[0].IVideoHeight = 32, 32
	param.SSpatialLayers[0].FFrameRate = 60
	param.SSpatialLayers[0].SSliceArgument.UiSliceNum = 1
	param.BUseLoadBalancing, param.BEnableSceneChangeDetect, param.BEnableFrameSkip = false, false, false
	if code := codec.InitializeExt(&param); code != 0 {
		t.Fatalf("initialize: %d", code)
	}
	source := unchangedTestSource()
	source.IColorFormat = int32(api.VideoFormatI420)
	source.IStride = [4]int32{32, 16, 16}
	source.PData[0] = bytes.Repeat([]byte{75}, 32*32)
	source.PData[1] = bytes.Repeat([]byte{128}, 16*16)
	source.PData[2] = bytes.Repeat([]byte{128}, 16*16)
	var output api.SFrameBSInfo
	for frame := range 3 {
		source.UiTimeStamp = int64(frame * 16)
		if code := codec.EncodeFrame(source, &output); code != 0 {
			t.Fatalf("frame %d encode: %d", frame, code)
		}
		ctx := codec.m_pEncContext
		if ctx.unchanged.reference != ctx.pCurDqLayer.pDecPic || ctx.unchanged.macroblocks != nil {
			t.Fatal("successful encoding retained caller storage or the wrong reference")
		}
	}
	ctx := codec.m_pEncContext
	previousDecision := ctx.pFuncList.pfInterMdBackgroundDecision
	fallbacks := 0
	ctx.pFuncList.pfInterMdBackgroundDecision = func(
		c *sWelsEncCtx, md *SWelsMD, s *SSlice, m *SMB, cache *SMbCache, keep *bool,
	) bool {
		fallbacks++

		return previousDecision(c, md, s, m, cache, keep)
	}
	if code := codec.EncodeFrame(source, &output); code != 0 || fallbacks != 0 {
		t.Fatalf("stable picture did not bypass mode decision: code=%d fallback blocks=%d", code, fallbacks)
	}
	source.UnchangedMacroblocks = nil
	if code := codec.EncodeFrame(source, &output); code != 0 || fallbacks != 4 {
		t.Fatalf("nil hints did not restore normal coding: code=%d fallback blocks=%d", code, fallbacks)
	}
	if code := codec.EncodeFrame(nil, &output); code == 0 || ctx.unchanged.reference != nil {
		t.Fatalf("invalid call retained the reference chain: code=%d", code)
	}
}

func unchangedTestParams() *SWelsSvcCodingParam {
	param := NewSWelsSvcCodingParam()
	param.IPicWidth, param.IPicHeight = 32, 32
	param.sDependencyLayers[0].iActualWidth, param.sDependencyLayers[0].iActualHeight = 32, 32
	param.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	param.ISpatialLayerNum, param.ITemporalLayerNum, param.INumRefFrame = 1, 1, 1
	param.IRCMode, param.IComplexityMode = api.RC_OFF_MODE, api.LOW_COMPLEXITY
	param.ILoopFilterDisableIdc, param.IEntropyCodingModeFlag = 1, 0
	param.BEnableDenoise, param.BEnableAdaptiveQuant, param.BEnableBackgroundDetection = false, false, false
	param.BEnableLongTermReference, param.BSimulcastAVC = false, false
	param.SSpatialLayers[0].IDLayerQp = 20
	param.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE

	return param
}

func unchangedTestSource() *api.SSourcePicture {
	return &api.SSourcePicture{IPicWidth: 32, IPicHeight: 32, UnchangedMacroblocks: []bool{true, true, true, true}}
}

func unchangedTestMacroblock() (*sWelsEncCtx, *SSlice, *SMB) {
	ref, rec := AllocPicture(32, 32, true, 0), AllocPicture(32, 32, true, 0)
	ref.iPictureType, ref.iFrameNum, ref.iFramePoc = common.P_SLICE, 1, 2
	ref.bUsedAsRef = true
	for i := range ref.pBuffer {
		ref.pBuffer[i] = byte(i%251 + 1)
	}
	for index := range ref.uiRefMbType {
		ref.uiRefMbType[index], ref.pMbSkipSad[index] = common.MB_TYPE_SKIP, 29
	}
	ctx := &sWelsEncCtx{
		pSvcParam: unchangedTestParams(), pFuncList: &SWelsFuncPtrList{pfUpdateMbMv: UpdateMbMv_c},
		eSliceType: common.P_SLICE, iNumRef0: 1,
		pCurDqLayer: &SDqLayer{pRefPic: ref, pDecPic: rec, iLoopFilterDisableIdc: 1, iCsStride: rec.iLineSize},
		unchanged:   unchangedFrameState{reference: ref, frameNum: 1, poc: 2, qp: 20, width: 32, height: 32},
	}
	ctx.pCurDqLayer.sLayerInfo.pPpsP = &SWelsPPS{}
	ctx.pRefList0[0] = ref
	lumaCost := int32(17)
	mb := &SMB{iMbXY: 1, iMbX: 1, uiLumaQp: 20, pSadCost: &lumaCost,
		sMv: make([]SMVUnitXY, 16), pRefIndex: make([]int8, 4), pNonZeroCount: make([]int8, 24)}
	for i := range mb.pNonZeroCount {
		mb.pNonZeroCount[i] = 1
	}
	slice := &SSlice{uiLastMbQp: 20}
	for plane := range 3 {
		size := 8
		if plane == 0 {
			size = 16
		}
		slice.sMbCacheInfo.SPicData.pRefMb[plane] = ref.pData[plane]
		slice.sMbCacheInfo.SPicData.iRefMbOff[plane] = ref.iDataOff[plane] + size
		slice.sMbCacheInfo.SPicData.pCsMb[plane] = rec.pData[plane]
		slice.sMbCacheInfo.SPicData.iCsMbOff[plane] = rec.iDataOff[plane] + size
	}

	return ctx, slice, mb
}
