package encoder

import (
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/processing"
)

func vaaUnchangedFixture() (*sWelsEncCtx, *api.SSourcePicture, *SPicture, *SPicture) {
	param := unchangedTestParams()
	param.SUsedPicRect = SUsedPicRect{iWidth: 32, iHeight: 32}
	ctx := &sWelsEncCtx{pSvcParam: param, eSliceType: common.P_SLICE}
	ctx.pVpp = &CWelsPreProcess{m_pEncCtx: ctx}
	cur, ref := AllocPicture(32, 32, false, 0), AllocPicture(32, 32, false, 0)
	ref.iFramePoc = 2
	ctx.pVpp.m_pLastSpatialPicture[0][0] = ref
	ctx.pVpp.vaaUnchanged = vaaUnchangedFrameState{reference: ref, poc: 2, width: 32, height: 32}
	source := unchangedTestSource()
	source.IColorFormat = int32(api.VideoFormatI420)
	return ctx, source, cur, ref
}

func TestVAAUnchangedReferenceFallbacks(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*sWelsEncCtx, *SPicture, *SPicture)
	}{
		{name: "IDR", change: func(c *sWelsEncCtx, _, _ *SPicture) { c.eSliceType = common.I_SLICE }},
		{name: "recycled source", change: func(_ *sWelsEncCtx, _, r *SPicture) { r.iFramePoc++ }},
		{name: "different successful source", change: func(c *sWelsEncCtx, _, _ *SPicture) { c.pVpp.vaaUnchanged.reference = &SPicture{} }},
		{name: "different previous source", change: func(c *sWelsEncCtx, _, _ *SPicture) { c.pVpp.m_pLastSpatialPicture[0][0] = nil }},
		{name: "different current source", change: func(c *sWelsEncCtx, _, _ *SPicture) { c.pVpp.vaaUnchanged.current = nil }},
		{name: "different geometry", change: func(_ *sWelsEncCtx, p, _ *SPicture) { p.iWidthInPixel = 16 }},
		{name: "different stride", change: func(_ *sWelsEncCtx, p, _ *SPicture) { p.iLineSize[0]++ }},
		{name: "policy changed", change: func(c *sWelsEncCtx, _, _ *SPicture) { c.pSvcParam.BEnableDenoise = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, source, cur, ref := vaaUnchangedFixture()
			s := &ctx.pVpp.vaaUnchanged
			s.begin(ctx, source)
			s.current = cur
			if len(s.maskFor(ctx, cur, ref)) != 4 {
				t.Fatal("valid source identity rejected")
			}
			test.change(ctx, cur, ref)
			if s.maskFor(ctx, cur, ref) != nil {
				t.Fatal("uncertain source reference accepted")
			}
		})
	}
}

func TestVAAUnchangedInputFallbacks(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*sWelsEncCtx, *api.SSourcePicture)
	}{
		{name: "crop left", change: func(c *sWelsEncCtx, _ *api.SSourcePicture) { c.pSvcParam.SUsedPicRect.iLeft = 2 }},
		{name: "crop top", change: func(c *sWelsEncCtx, _ *api.SSourcePicture) { c.pSvcParam.SUsedPicRect.iTop = 2 }},
		{name: "crop size", change: func(c *sWelsEncCtx, _ *api.SSourcePicture) { c.pSvcParam.SUsedPicRect.iWidth = 16 }},
		{name: "rescaling", change: func(c *sWelsEncCtx, _ *api.SSourcePicture) { c.pVpp.m_sScaledPicture.pScaledInputPicture = &SPicture{} }},
		{name: "source dimensions", change: func(_ *sWelsEncCtx, s *api.SSourcePicture) { s.IPicWidth = 16 }},
		{name: "source format", change: func(_ *sWelsEncCtx, s *api.SSourcePicture) { s.IColorFormat = -1 }},
		{name: "nil mask", change: func(_ *sWelsEncCtx, s *api.SSourcePicture) { s.UnchangedMacroblocks = nil }},
		{name: "short mask", change: func(_ *sWelsEncCtx, s *api.SSourcePicture) { s.UnchangedMacroblocks = []bool{true} }},
		{name: "long mask", change: func(_ *sWelsEncCtx, s *api.SSourcePicture) { s.UnchangedMacroblocks = make([]bool, 5) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, source, cur, ref := vaaUnchangedFixture()
			test.change(ctx, source)
			s := &ctx.pVpp.vaaUnchanged
			s.begin(ctx, source)
			s.current = cur
			if s.maskFor(ctx, cur, ref) != nil {
				t.Fatal("unsupported source used hints")
			}
		})
	}
}

func TestVAAUnchangedFinishInvalidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		success bool
		kind    api.EVideoFrameType
		size    int32
	}{
		{name: "error", kind: api.VideoFrameTypeP, size: 100},
		{name: "skip", success: true, kind: api.VideoFrameTypeSkip, size: 100},
		{name: "invalid", success: true, kind: api.VideoFrameTypeInvalid, size: 100},
		{name: "empty", success: true, kind: api.VideoFrameTypeP},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, source, cur, _ := vaaUnchangedFixture()
			s := &ctx.pVpp.vaaUnchanged
			s.begin(ctx, source)
			s.current = cur
			ctx.pEncPic = cur
			ctx.pVpp.m_pLastSpatialPicture[0][0] = cur
			s.finish(ctx, &api.SFrameBSInfo{EFrameType: test.kind, IFrameSizeInBytes: test.size}, test.success)
			if s.reference != nil || s.current != nil || s.mask != nil || s.enabled {
				t.Fatal("failed call retained source or mask")
			}
		})
	}
}

type observingVAA struct {
	processing.IWelsVP
	usedHint    bool
	hintPending bool
	result      processing.EResult
}

func (v *observingVAA) Set(kind int32, param any) processing.EResult {
	if kind == processing.METHOD_VAA_STATISTICS {
		v.hintPending = len(param.(*processing.SVAACalcParam).UnchangedMacroblocks) > 0
		v.usedHint = v.usedHint || v.hintPending
	}
	return v.IWelsVP.Set(kind, param)
}

func (v *observingVAA) Process(kind int32, src, dst *processing.SPixMap) processing.EResult {
	v.result = v.IWelsVP.Process(kind, src, dst)
	return v.result
}

func TestVAAUnchangedReleaseAfterFrameworkRejection(t *testing.T) {
	t.Parallel()
	ctx, source, cur, ref := vaaUnchangedFixture()
	var result processing.EResult
	observer := &observingVAA{IWelsVP: processing.NewCVpFrameWork(1, &result)}
	ctx.pVpp.m_pInterfaceVp = observer
	s := &ctx.pVpp.vaaUnchanged
	s.begin(ctx, source)
	s.current = cur
	cur.iLineSize[0], ref.iLineSize[0] = 0, 0 // Rejected before the VAA strategy runs.
	ctx.pVpp.VaaCalculation(&SVAAFrameInfo{}, cur, ref, false, false, false)
	if !observer.usedHint || observer.hintPending || observer.result != processing.RET_INVALIDPARAM {
		t.Fatalf("hint=%v retained=%v result=%d", observer.usedHint, observer.hintPending, observer.result)
	}
}

func TestVAAUnchangedMatchesFullAcrossSourceReuse(t *testing.T) {
	t.Parallel()
	const width, height = 32, 24 // Exercises the constant bottom padding too.
	var codecs [2]*CWelsH264SVCEncoder
	for i := range codecs {
		codec := NewCWelsH264SVCEncoder()
		codecs[i] = codec
		t.Cleanup(func() { codec.Uninitialize() })
		param := unchangedTestParams().SEncParamExt
		param.IPicWidth, param.IPicHeight = width, height
		param.FMaxFrameRate = 60
		param.SSpatialLayers[0].IVideoWidth, param.SSpatialLayers[0].IVideoHeight = width, height
		param.SSpatialLayers[0].FFrameRate = 60
		param.SSpatialLayers[0].SSliceArgument.UiSliceNum = 1
		param.BUseLoadBalancing, param.BEnableSceneChangeDetect, param.BEnableFrameSkip = false, false, false
		if code := codec.InitializeExt(&param); code != 0 {
			t.Fatalf("initialize: %d", code)
		}
	}
	vpp := codecs[1].m_pEncContext.pVpp
	observer := &observingVAA{}
	t.Cleanup(func() {
		if observer.IWelsVP != nil {
			vpp.m_pInterfaceVp = observer.IWelsVP
		}
	})
	source := &api.SSourcePicture{IColorFormat: int32(api.VideoFormatI420), IPicWidth: width, IPicHeight: height, IStride: [4]int32{width, width / 2, width / 2}}
	source.PData[0] = make([]byte, width*height)
	source.PData[1] = make([]byte, width*height/4)
	source.PData[2] = make([]byte, width*height/4)
	seen := map[*SPicture]bool{}
	for frame := range 12 {
		mask := []bool{true, true, true, true}
		if frame == 0 {
			for p := range 3 {
				for i := range source.PData[p] {
					source.PData[p][i] = byte(i*7 + p*19)
				}
			}
		}
		if frame > 1 {
			mb := frame % 4
			mask[mb] = false
			plane := 0
			if frame == 3 {
				plane = 1
			} // A chroma-only change still has exact zero luma SAD.
			block, stride := 16, width
			if plane > 0 {
				block, stride = 8, width/2
			}
			off := (mb/2)*block*stride + (mb%2)*block
			source.PData[plane][off] ^= 0x5a
		}
		if frame == 5 {
			for _, codec := range codecs {
				var output api.SFrameBSInfo
				if codec.EncodeFrame(nil, &output) == 0 {
					t.Fatal("nil input succeeded")
				}
				if codec.m_pEncContext.pVpp.vaaUnchanged.reference != nil {
					t.Fatal("invalid input retained source")
				}
			}
			mask = []bool{true, true, true, true} // Must be ignored after the invalid call.
		}
		if frame == 8 {
			for _, codec := range codecs {
				codec.ForceIntraFrame(true, -1)
			}
		}
		source.UiTimeStamp = int64(frame * 16)
		observer.usedHint = false
		for i, codec := range codecs {
			source.UnchangedMacroblocks = nil
			if i == 1 {
				source.UnchangedMacroblocks = mask
			}
			var output api.SFrameBSInfo
			if code := codec.EncodeFrame(source, &output); code != 0 {
				t.Fatalf("frame %d encoder %d: %d", frame, i, code)
			}
			ctx := codec.m_pEncContext
			s := &ctx.pVpp.vaaUnchanged
			if s.reference != ctx.pEncPic || s.mask != nil || s.current != nil {
				t.Fatalf("frame %d wrong source or borrowed mask retained", frame)
			}
		}
		want := &codecs[0].m_pEncContext.pVaa.sVaaCalcInfo
		got := &codecs[1].m_pEncContext.pVaa.sVaaCalcInfo
		if got.IFrameSad != want.IFrameSad || !slices.Equal(got.PSad8x8, want.PSad8x8) {
			t.Fatalf("frame %d VAA changed: %d/%v want %d/%v", frame, got.IFrameSad, got.PSad8x8, want.IFrameSad, want.PSad8x8)
		}
		wantHint := frame > 0 && frame != 5 && frame != 8
		if observer.usedHint != wantHint {
			t.Fatalf("frame %d usedHint=%v want=%v", frame, observer.usedHint, wantHint)
		}
		seen[vpp.vaaUnchanged.reference] = true
		if frame == 0 {
			observer.IWelsVP = vpp.m_pInterfaceVp
			vpp.m_pInterfaceVp = observer
		}
	}
	if len(seen) != 2 {
		t.Fatalf("expected two reused original source buffers, got %d", len(seen))
	}
	if codecs[1].EncodeFrame(source, nil) == 0 || vpp.vaaUnchanged.reference != nil {
		t.Fatal("nil output retained original source reference")
	}
}
