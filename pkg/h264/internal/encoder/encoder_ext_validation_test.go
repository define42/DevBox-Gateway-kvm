package encoder

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func validationParameters(t *testing.T) *SWelsSvcCodingParam {
	t.Helper()
	p := NewSWelsSvcCodingParam()
	base := api.SEncParamBase{IPicWidth: 64, IPicHeight: 64, FMaxFrameRate: 30, ITargetBitrate: 100_000, IRCMode: api.RC_OFF_MODE}
	if got := p.ParamBaseTranscode(&base); got != 0 {
		t.Fatalf("ParamBaseTranscode = %d", got)
	}
	return p
}

func TestParameterValidationRejectsInvalidConfigurations(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		mutate func(*SWelsSvcCodingParam)
		want   int32
	}{
		{"usage", func(p *SWelsSvcCodingParam) { p.IUsageType = api.INPUT_CONTENT_TYPE_ALL }, ENC_RETURN_UNSUPPORTED_PARA},
		{"screen spatial layers", func(p *SWelsSvcCodingParam) { p.IUsageType = api.SCREEN_CONTENT_REAL_TIME; p.ISpatialLayerNum = 2 }, ENC_RETURN_UNSUPPORTED_PARA},
		{"descending resolutions", func(p *SWelsSvcCodingParam) {
			p.ISpatialLayerNum = 2
			p.SSpatialLayers[1] = p.SSpatialLayers[0]
			p.SSpatialLayers[1].IVideoWidth = 32
		}, ENC_RETURN_UNSUPPORTED_PARA},
		{"deblocking mode", func(p *SWelsSvcCodingParam) { p.ILoopFilterDisableIdc = 3 }, ENC_RETURN_UNSUPPORTED_PARA},
		{"alpha offset", func(p *SWelsSvcCodingParam) { p.ILoopFilterAlphaC0Offset = -7 }, ENC_RETURN_UNSUPPORTED_PARA},
		{"beta offset", func(p *SWelsSvcCodingParam) { p.ILoopFilterBetaOffset = 7 }, ENC_RETURN_UNSUPPORTED_PARA},
		{"output exceeds input rate", func(p *SWelsSvcCodingParam) { p.sDependencyLayers[0].fOutputFrameRate = 60 }, ENC_RETURN_INVALIDINPUT},
		{"zero input rate", func(p *SWelsSvcCodingParam) { p.sDependencyLayers[0].fInputFrameRate = 0 }, ENC_RETURN_INVALIDINPUT},
		{"zero output rate", func(p *SWelsSvcCodingParam) { p.sDependencyLayers[0].fOutputFrameRate = 0 }, ENC_RETURN_INVALIDINPUT},
		{"rate control mode", func(p *SWelsSvcCodingParam) { p.IRCMode = 99 }, ENC_RETURN_UNSUPPORTED_PARA},
		{"zero target bitrate", func(p *SWelsSvcCodingParam) { p.IRCMode = api.RC_BITRATE_MODE; p.ITargetBitrate = 0 }, ENC_RETURN_INVALIDINPUT},
		{"invalid spatial bitrate", func(p *SWelsSvcCodingParam) { p.IRCMode = api.RC_BITRATE_MODE; p.SSpatialLayers[0].ISpatialBitrate = 0 }, ENC_RETURN_INVALIDINPUT},
		{"layer bitrate exceeds total", func(p *SWelsSvcCodingParam) { p.IRCMode = api.RC_BITRATE_MODE; p.ITargetBitrate = 50_000 }, ENC_RETURN_INVALIDINPUT},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := validationParameters(t)
			tt.mutate(p)
			if got := ParamValidation(nil, p); got != tt.want {
				t.Fatalf("ParamValidation=%d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtendedParameterValidationRejectsInvalidConfigurations(t *testing.T) {
	t.Parallel()
	if got := ParamValidationExt(nil, nil); got != ENC_RETURN_INVALIDINPUT {
		t.Fatalf("nil parameters=%d", got)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*SWelsSvcCodingParam)
	}{
		{"usage", func(p *SWelsSvcCodingParam) { p.IUsageType = api.INPUT_CONTENT_TYPE_ALL }},
		{"no spatial layers", func(p *SWelsSvcCodingParam) { p.ISpatialLayerNum = 0 }},
		{"excess spatial layers", func(p *SWelsSvcCodingParam) { p.ISpatialLayerNum = MAX_DEPENDENCY_LAYER + 1 }},
		{"no temporal layers", func(p *SWelsSvcCodingParam) { p.ITemporalLayerNum = 0 }},
		{"excess temporal layers", func(p *SWelsSvcCodingParam) { p.ITemporalLayerNum = MAX_TEMPORAL_LEVEL + 1 }},
		{"zero GOP", func(p *SWelsSvcCodingParam) { p.uiGopSize = 0 }},
		{"excess GOP", func(p *SWelsSvcCodingParam) { p.uiGopSize = MAX_GOP_SIZE + 1 }},
		{"intra period below GOP", func(p *SWelsSvcCodingParam) { p.uiGopSize = 4; p.UiIntraPeriod = 2 }},
		{"intra period not GOP multiple", func(p *SWelsSvcCodingParam) { p.uiGopSize = 4; p.UiIntraPeriod = 5 }},
		{"zero width", func(p *SWelsSvcCodingParam) { p.SSpatialLayers[0].IVideoWidth = 0 }},
		{"negative height", func(p *SWelsSvcCodingParam) { p.SSpatialLayers[0].IVideoHeight = -16 }},
		{"oversized frame", func(p *SWelsSvcCodingParam) { p.SSpatialLayers[0].IVideoWidth = math.MaxInt32 }},
		{"unaligned width", func(p *SWelsSvcCodingParam) { p.SSpatialLayers[0].IVideoWidth = 63 }},
		{"unaligned height", func(p *SWelsSvcCodingParam) { p.SSpatialLayers[0].IVideoHeight = 63 }},
		{"reserved slice mode", func(p *SWelsSvcCodingParam) { p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_RESERVED }},
		{"excess raster rows", func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].IVideoWidth = 16
			p.SSpatialLayers[0].IVideoHeight = (MAX_SLICES_NUM + 1) * 16
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_RASTER_SLICE
		}},
		{"incomplete raster allocation", func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].IVideoWidth = 1024
			p.SSpatialLayers[0].IVideoHeight = 1024
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_RASTER_SLICE
			for i := range p.SSpatialLayers[0].SSliceArgument.UiSliceMbNum {
				p.SSpatialLayers[0].SSliceArgument.UiSliceMbNum[i] = 1
			}
		}},
		{"slice size below macroblock", func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SIZELIMITED_SLICE
			p.SSpatialLayers[0].SSliceArgument.UiSliceSizeConstraint = MAX_MACROBLOCK_SIZE_IN_BYTE
		}},
		{"NAL size below macroblock", func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SIZELIMITED_SLICE
			p.SSpatialLayers[0].SSliceArgument.UiSliceSizeConstraint = 1000
			p.UiMaxNalSize = 1
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := validationParameters(t)
			tt.mutate(p)
			if got := ParamValidationExt(nil, p); got != ENC_RETURN_UNSUPPORTED_PARA {
				t.Fatalf("ParamValidationExt=%d, want unsupported parameters", got)
			}
		})
	}
}

func TestParameterValidationNormalizesSupportedSettings(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		mutate func(*SWelsSvcCodingParam)
		check  func(*testing.T, *SWelsSvcCodingParam)
	}{
		{"screen feature compatibility", func(p *SWelsSvcCodingParam) {
			p.IUsageType = api.SCREEN_CONTENT_REAL_TIME
			p.BEnableAdaptiveQuant = true
			p.BEnableBackgroundDetection = true
			p.BEnableSceneChangeDetect = false
			p.BEnableLongTermReference = true
			p.BIsLosslessLink = false
			p.ESpsPpsIdStrategy = api.SPS_LISTING
		}, func(t *testing.T, p *SWelsSvcCodingParam) {
			if p.BEnableAdaptiveQuant || p.BEnableBackgroundDetection || !p.BEnableSceneChangeDetect || p.BEnableLongTermReference || p.ESpsPpsIdStrategy != api.CONSTANT_ID {
				t.Fatalf("screen features not normalized: %+v", p.SEncParamExt)
			}
		}},
		{"simulcast parameter sets", func(p *SWelsSvcCodingParam) {
			p.BSimulcastAVC = true
			p.ESpsPpsIdStrategy = api.SPS_LISTING
			p.BPrefixNalAddingCtrl = true
		}, func(t *testing.T, p *SWelsSvcCodingParam) {
			if p.ESpsPpsIdStrategy != api.INCREASING_ID || p.BPrefixNalAddingCtrl {
				t.Fatalf("simulcast strategy=%v prefix=%v", p.ESpsPpsIdStrategy, p.BPrefixNalAddingCtrl)
			}
		}},
		{"SVC parameter sets", func(p *SWelsSvcCodingParam) {
			p.ISpatialLayerNum = 2
			p.SSpatialLayers[1] = p.SSpatialLayers[0]
			p.sDependencyLayers[1] = p.sDependencyLayers[0]
			p.ESpsPpsIdStrategy = api.SPS_LISTING
		}, func(t *testing.T, p *SWelsSvcCodingParam) {
			if p.ESpsPpsIdStrategy != api.CONSTANT_ID || p.SSpatialLayers[1].UiProfileIdc != api.PRO_SCALABLE_BASELINE {
				t.Fatalf("SVC strategy=%v profile=%v", p.ESpsPpsIdStrategy, p.SSpatialLayers[1].UiProfileIdc)
			}
		}},
		{"unspecified layer resolution", func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].IVideoWidth = 0
			p.SSpatialLayers[0].IVideoHeight = 0
		}, func(t *testing.T, p *SWelsSvcCodingParam) {
			if p.SSpatialLayers[0].IVideoWidth != 64 || p.SSpatialLayers[0].IVideoHeight != 64 {
				t.Fatalf("layer dimensions=%dx%d", p.SSpatialLayers[0].IVideoWidth, p.SSpatialLayers[0].IVideoHeight)
			}
		}},
		{"baseline CABAC disabled", func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].UiProfileIdc = api.PRO_BASELINE
			p.IEntropyCodingModeFlag = 1
		}, func(t *testing.T, p *SWelsSvcCodingParam) {
			if p.IEntropyCodingModeFlag != 0 {
				t.Fatal("baseline retained CABAC")
			}
		}},
		{"unspecified CABAC profile", func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].UiProfileIdc = api.PRO_UNKNOWN
			p.IEntropyCodingModeFlag = 1
		}, func(t *testing.T, p *SWelsSvcCodingParam) {
			if p.SSpatialLayers[0].UiProfileIdc != api.PRO_HIGH {
				t.Fatalf("CABAC profile=%v", p.SSpatialLayers[0].UiProfileIdc)
			}
		}},
		{"frame rate ratio", func(p *SWelsSvcCodingParam) { p.sDependencyLayers[0].fOutputFrameRate = 13 }, func(t *testing.T, p *SWelsSvcCodingParam) {
			if p.sDependencyLayers[0].fOutputFrameRate != 30 || p.SSpatialLayers[0].FFrameRate != 30 {
				t.Fatal("non-dyadic output rate not normalized")
			}
		}},
		{"size limited NAL overhead", func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SIZELIMITED_SLICE
			p.SSpatialLayers[0].SSliceArgument.UiSliceSizeConstraint = 2000
			p.UiMaxNalSize = 1500
		}, func(t *testing.T, p *SWelsSvcCodingParam) {
			if got := p.SSpatialLayers[0].SSliceArgument.UiSliceSizeConstraint; got != 1500-2*NAL_HEADER_ADD_0X30BYTES {
				t.Fatalf("slice constraint=%d", got)
			}
		}},
		{"single macroblock slice", func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].IVideoWidth = 16
			p.SSpatialLayers[0].IVideoHeight = 16
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_RASTER_SLICE
		}, func(t *testing.T, p *SWelsSvcCodingParam) {
			if p.SSpatialLayers[0].SSliceArgument.UiSliceMode != api.SM_SINGLE_SLICE {
				t.Fatal("single macroblock kept multiple slices")
			}
		}},
		{"single raster allocation", func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_RASTER_SLICE
			p.SSpatialLayers[0].SSliceArgument.UiSliceMbNum[0] = 16
		}, func(t *testing.T, p *SWelsSvcCodingParam) {
			if p.SSpatialLayers[0].SSliceArgument.UiSliceMode != api.SM_SINGLE_SLICE {
				t.Fatal("single raster allocation kept multiple slices")
			}
		}},
		{"small raster frame", func(p *SWelsSvcCodingParam) {
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_RASTER_SLICE
			p.SSpatialLayers[0].SSliceArgument.UiSliceMbNum[0] = 8
			p.SSpatialLayers[0].SSliceArgument.UiSliceMbNum[1] = 8
		}, func(t *testing.T, p *SWelsSvcCodingParam) {
			if p.SSpatialLayers[0].SSliceArgument.UiSliceMode != api.SM_SINGLE_SLICE {
				t.Fatal("small frame kept multiple raster slices")
			}
		}},
		{"parallel deblocking", func(p *SWelsSvcCodingParam) { p.IMultipleThreadIdc = 2; p.UiMaxNalSize = 1500 }, func(t *testing.T, p *SWelsSvcCodingParam) {
			if !p.bDeblockingParallelFlag {
				t.Fatal("multiple threads did not enable parallel deblocking")
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := validationParameters(t)
			tt.mutate(p)
			if got := ParamValidationExt(nil, p); got != ENC_RETURN_SUCCESS {
				t.Fatalf("ParamValidationExt=%d", got)
			}
			tt.check(t, p)
		})
	}
	for _, usage := range []api.EUsageType{api.CAMERA_VIDEO_REAL_TIME, api.SCREEN_CONTENT_REAL_TIME} {
		t.Run(fmt.Sprintf("QP defaults usage %d", usage), func(t *testing.T) {
			t.Parallel()
			p := validationParameters(t)
			p.IUsageType = usage
			p.IRCMode = api.RC_BITRATE_MODE
			p.IMinQp = 0
			p.IMaxQp = 0
			p.BEnableFrameSkip = false
			if got := ParamValidation(nil, p); got != ENC_RETURN_SUCCESS {
				t.Fatalf("QP defaults validation=%d", got)
			}
			wantMin, wantMax := int32(GOM_MIN_QP_MODE), int32(MAX_LOW_BR_QP)
			if usage == api.SCREEN_CONTENT_REAL_TIME {
				wantMin, wantMax = MIN_SCREEN_QP, MAX_SCREEN_QP
			}
			if p.IMinQp != wantMin || p.IMaxQp != wantMax {
				t.Fatalf("usage %d QP range=%d..%d, want %d..%d", usage, p.IMinQp, p.IMaxQp, wantMin, wantMax)
			}
		})
	}
}

func TestEncoderRateUpdatesPreserveLayerRatios(t *testing.T) {
	t.Parallel()
	p := validationParameters(t)
	p.ISpatialLayerNum = 2
	p.ITargetBitrate = 400_000
	p.SSpatialLayers[1] = p.SSpatialLayers[0]
	p.SSpatialLayers[0].ISpatialBitrate = 50_000
	p.SSpatialLayers[1].ISpatialBitrate = 150_000
	if got := WelsEncoderApplyBitRate(nil, p, int32(api.SPATIAL_LAYER_ALL)); got != ENC_RETURN_SUCCESS {
		t.Fatalf("apply bitrate=%d", got)
	}
	if p.SSpatialLayers[0].ISpatialBitrate != 100_000 || p.SSpatialLayers[1].ISpatialBitrate != 300_000 {
		t.Fatalf("layer bitrates=%d/%d", p.SSpatialLayers[0].ISpatialBitrate, p.SSpatialLayers[1].ISpatialBitrate)
	}
	if got := WelsEncoderApplyBitVaryRang(nil, p, 20); got != ENC_RETURN_SUCCESS {
		t.Fatalf("apply variation=%d", got)
	}
	if p.SSpatialLayers[0].IMaxSpatialBitrate != 120_000 || p.SSpatialLayers[1].IMaxSpatialBitrate != 360_000 {
		t.Fatalf("layer ceilings=%d/%d", p.SSpatialLayers[0].IMaxSpatialBitrate, p.SSpatialLayers[1].IMaxSpatialBitrate)
	}
	p.SSpatialLayers[0].IMaxSpatialBitrate = 10
	if got := WelsEncoderApplyBitRate(nil, p, 0); got != ENC_RETURN_UNSUPPORTED_PARA {
		t.Fatalf("invalid layer maximum accepted: %d", got)
	}
	if got := WelsEncoderApplyBitVaryRang(nil, p, -1); got != ENC_RETURN_UNSUPPORTED_PARA {
		t.Fatalf("invalid variation accepted: %d", got)
	}
	if got := WelsEncoderApplyBitRate(nil, p, int32(api.SPATIAL_LAYER_ALL)); got != ENC_RETURN_UNSUPPORTED_PARA {
		t.Fatalf("invalid aggregate maximum accepted: %d", got)
	}
	p = validationParameters(t)
	p.FMaxFrameRate = 10
	p.sDependencyLayers[0].fOutputFrameRate = 15
	WelsEncoderApplyFrameRate(p)
	if p.sDependencyLayers[0].fInputFrameRate != 10 || p.SSpatialLayers[0].FFrameRate != 10 {
		t.Fatal("rate below six fps should fall back to input rate")
	}
}

func validationEncoder(t *testing.T, initialize bool) *CWelsH264SVCEncoder {
	t.Helper()
	var instance api.ISVCEncoder
	if got := WelsCreateSVCEncoder(&instance); got != 0 {
		t.Fatalf("create encoder=%d", got)
	}
	t.Cleanup(func() { WelsDestroySVCEncoder(instance) })
	quiet := int32(api.WELS_LOG_QUIET)
	if got := instance.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &quiet); got != 0 {
		t.Fatalf("quiet log=%d", got)
	}
	if initialize {
		base := api.SEncParamBase{IPicWidth: 32, IPicHeight: 32, FMaxFrameRate: 30, ITargetBitrate: 100_000, IRCMode: api.RC_OFF_MODE}
		if got := instance.Initialize(&base); got != 0 {
			t.Fatalf("initialize=%d", got)
		}
	}
	return instance.(*CWelsH264SVCEncoder)
}

func TestEncoderInitializationRejectsInvalidInternalParameters(t *testing.T) {
	t.Parallel()
	enc := validationEncoder(t, false)
	if got := enc.Initialize(nil); got != int32(api.CmInitParaError) {
		t.Fatalf("nil base parameters=%d", got)
	}
	if got := enc.InitializeExt(nil); got != int32(api.CmInitParaError) {
		t.Fatalf("nil extended parameters=%d", got)
	}
	if got := enc.InitializeInternal(nil); got != int32(api.CmInitParaError) {
		t.Fatalf("nil internal parameters=%d", got)
	}
	var unconstructed CWelsH264SVCEncoder
	if got := unconstructed.Initialize(&api.SEncParamBase{}); got != int32(api.CmMallocMemeError) {
		t.Fatalf("unconstructed Initialize=%d", got)
	}
	if got := unconstructed.InitializeExt(&api.SEncParamExt{}); got != int32(api.CmMallocMemeError) {
		t.Fatalf("unconstructed InitializeExt=%d", got)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*SWelsSvcCodingParam)
	}{
		{"layers", func(p *SWelsSvcCodingParam) { p.ISpatialLayerNum = 0 }},
		{"temporal layers", func(p *SWelsSvcCodingParam) { p.ITemporalLayerNum = MAX_TEMPORAL_LEVEL + 1 }},
		{"zero GOP", func(p *SWelsSvcCodingParam) { p.uiGopSize = 0 }},
		{"non-dyadic GOP", func(p *SWelsSvcCodingParam) { p.uiGopSize = 3 }},
		{"short intra period", func(p *SWelsSvcCodingParam) { p.uiGopSize = 4; p.UiIntraPeriod = 2 }},
		{"unaligned intra period", func(p *SWelsSvcCodingParam) { p.uiGopSize = 4; p.UiIntraPeriod = 5 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := validationParameters(t)
			tt.mutate(p)
			if got := enc.InitializeInternal(p); got != int32(api.CmInitParaError) {
				t.Fatalf("InitializeInternal=%d", got)
			}
			if enc.m_bInitialFlag || enc.m_pEncContext != nil {
				t.Fatal("failed initialization left active encoder state")
			}
		})
	}
}

func TestEncoderOptionsRejectWrongTypes(t *testing.T) {
	t.Parallel()
	enc := validationEncoder(t, true)
	for _, option := range []api.ENCODER_OPTION{
		api.ENCODER_OPTION_DATAFORMAT, api.ENCODER_OPTION_IDR_INTERVAL, api.ENCODER_OPTION_SVC_ENCODE_PARAM_BASE,
		api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, api.ENCODER_OPTION_FRAME_RATE, api.ENCODER_OPTION_BITRATE,
		api.ENCODER_OPTION_MAX_BITRATE, api.ENCODER_OPTION_RC_MODE, api.ENCODER_OPTION_RC_FRAME_SKIP,
		api.ENCODER_PADDING_PADDING, api.ENCODER_LTR_RECOVERY_REQUEST, api.ENCODER_LTR_MARKING_FEEDBACK,
		api.ENCODER_LTR_MARKING_PERIOD, api.ENCODER_OPTION_LTR, api.ENCODER_OPTION_ENABLE_SSEI,
		api.ENCODER_OPTION_ENABLE_PREFIX_NAL_ADDING, api.ENCODER_OPTION_SPS_PPS_ID_STRATEGY,
		api.ENCODER_OPTION_CURRENT_PATH, api.ENCODER_OPTION_TRACE_LEVEL, api.ENCODER_OPTION_TRACE_CALLBACK,
		api.ENCODER_OPTION_PROFILE, api.ENCODER_OPTION_LEVEL, api.ENCODER_OPTION_NUMBER_REF,
		api.ENCODER_OPTION_DELIVERY_STATUS, api.ENCODER_OPTION_COMPLEXITY, api.ENCODER_OPTION_STATISTICS_LOG_INTERVAL,
		api.ENCODER_OPTION_IS_LOSSLESS_LINK, api.ENCODER_OPTION_BITS_VARY_PERCENTAGE, api.ENCODER_OPTION(-1),
	} {
		t.Run(fmt.Sprintf("set %d", option), func(t *testing.T) {
			if got := enc.SetOption(option, &struct{}{}); got != int32(api.CmInitParaError) {
				t.Fatalf("wrong option type=%d, want parameter error", got)
			}
		})
	}
	for _, option := range []api.ENCODER_OPTION{
		api.ENCODER_OPTION_DATAFORMAT, api.ENCODER_OPTION_IDR_INTERVAL, api.ENCODER_OPTION_SVC_ENCODE_PARAM_BASE,
		api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, api.ENCODER_OPTION_FRAME_RATE, api.ENCODER_OPTION_BITRATE,
		api.ENCODER_OPTION_MAX_BITRATE, api.ENCODER_OPTION_GET_STATISTICS, api.ENCODER_OPTION_STATISTICS_LOG_INTERVAL,
		api.ENCODER_OPTION_COMPLEXITY, api.ENCODER_OPTION(-1),
	} {
		t.Run(fmt.Sprintf("get %d", option), func(t *testing.T) {
			if got := enc.GetOption(option, &struct{}{}); got != int32(api.CmInitParaError) {
				t.Fatalf("wrong output type=%d, want parameter error", got)
			}
		})
	}
	if got := enc.GetOption(api.ENCODER_OPTION_FRAME_RATE, nil); got != int32(api.CmInitParaError) {
		t.Fatalf("nil output=%d", got)
	}
	for _, option := range []api.ENCODER_OPTION{api.ENCODER_OPTION_BITRATE, api.ENCODER_OPTION_MAX_BITRATE} {
		t.Run(fmt.Sprintf("invalid layer option %d", option), func(t *testing.T) {
			value := api.SBitrateInfo{ILayer: 99}
			if got := enc.GetOption(option, &value); got != int32(api.CmInitParaError) {
				t.Fatalf("invalid output layer=%d", got)
			}
		})
	}
}

func TestEncoderOptionsRejectInvalidValues(t *testing.T) {
	t.Parallel()
	zeroInt := int32(0)
	zeroFloat := float32(0)
	for _, tt := range []struct {
		name   string
		option api.ENCODER_OPTION
		value  any
	}{
		{"color format", api.ENCODER_OPTION_DATAFORMAT, &zeroInt},
		{"frame rate", api.ENCODER_OPTION_FRAME_RATE, &zeroFloat},
		{"bitrate", api.ENCODER_OPTION_BITRATE, &api.SBitrateInfo{ILayer: api.SPATIAL_LAYER_0}},
		{"maximum bitrate", api.ENCODER_OPTION_MAX_BITRATE, &api.SBitrateInfo{ILayer: api.SPATIAL_LAYER_0}},
		{"bitrate layer", api.ENCODER_OPTION_BITRATE, &api.SBitrateInfo{ILayer: 99, IBitrate: 100_000}},
		{"maximum bitrate layer", api.ENCODER_OPTION_MAX_BITRATE, &api.SBitrateInfo{ILayer: 99, IBitrate: 100_000}},
		{"profile layer", api.ENCODER_OPTION_PROFILE, &api.SProfileInfo{ILayer: -1}},
		{"level layer", api.ENCODER_OPTION_LEVEL, &api.SLevelInfo{ILayer: 4}},
		{"extended layer count", api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &api.SEncParamExt{ISpatialLayerNum: 5}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			enc := validationEncoder(t, true)
			if got := enc.SetOption(tt.option, tt.value); got != int32(api.CmInitParaError) {
				t.Fatalf("invalid value=%d, want parameter error", got)
			}
		})
	}
}

func TestBitrateLevelAndProfileNormalization(t *testing.T) {
	t.Parallel()
	layer := api.SSpatialLayerConfig{FFrameRate: 30, ISpatialBitrate: 64_000, IMaxSpatialBitrate: 1_000_000, UiLevelIdc: api.LEVEL_1_0}
	if got := WelsBitRateVerification(nil, &layer, 0); got != ENC_RETURN_SUCCESS || layer.UiLevelIdc == api.LEVEL_1_0 {
		t.Fatalf("level not increased for bitrate: result=%d layer=%+v", got, layer)
	}
	layer.IMaxSpatialBitrate = layer.ISpatialBitrate
	if got := WelsBitRateVerification(nil, &layer, 0); got != ENC_RETURN_SUCCESS {
		t.Fatalf("equal target/maximum rejected: %d", got)
	}
	p := validationParameters(t)
	p.BSimulcastAVC = true
	untouchedLayer := p.SSpatialLayers[1]
	CheckProfileSetting(nil, p, 0, api.PRO_SCALABLE_HIGH)
	if p.SSpatialLayers[0].UiProfileIdc != api.PRO_UNKNOWN {
		t.Fatal("simulcast accepted scalable profile")
	}
	p.IUsageType = api.SCREEN_CONTENT_REAL_TIME
	CheckReferenceNumSetting(nil, p, MAX_REFERENCE_PICTURE_COUNT_NUM_SCREEN)
	if p.INumRefFrame != MAX_REFERENCE_PICTURE_COUNT_NUM_SCREEN {
		t.Fatal("screen reference limit rejected")
	}
	if p.SSpatialLayers[1] != untouchedLayer {
		t.Fatal("profile validation modified another layer")
	}
}

func TestEncoderRuntimeOptionsUpdateState(t *testing.T) {
	t.Parallel()
	enc := validationEncoder(t, true)
	set := func(option api.ENCODER_OPTION, value any) {
		t.Helper()
		if got := enc.SetOption(option, value); got != 0 {
			t.Fatalf("SetOption(%d) = %d", option, got)
		}
	}
	for _, tt := range []struct {
		name   string
		option api.ENCODER_OPTION
		field  func() *bool
	}{
		{"scalable SEI", api.ENCODER_OPTION_ENABLE_SSEI, func() *bool { return &enc.m_pEncContext.pSvcParam.BEnableSSEI }},
		{"prefix NAL", api.ENCODER_OPTION_ENABLE_PREFIX_NAL_ADDING, func() *bool { return &enc.m_pEncContext.pSvcParam.BPrefixNalAddingCtrl }},
		{"lossless link", api.ENCODER_OPTION_IS_LOSSLESS_LINK, func() *bool { return &enc.m_pEncContext.pSvcParam.BIsLosslessLink }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, value := range []bool{true, false} {
				set(tt.option, &value)
				if *tt.field() != value {
					t.Fatalf("option %d state=%v, want %v", tt.option, *tt.field(), value)
				}
			}
		})
	}
	t.Run("rate controlled frame skip", func(t *testing.T) {
		mode := api.RC_BITRATE_MODE
		set(api.ENCODER_OPTION_RC_MODE, &mode)
		for _, enabled := range []bool{true, false} {
			set(api.ENCODER_OPTION_RC_FRAME_SKIP, &enabled)
			if enc.m_pEncContext.pSvcParam.BEnableFrameSkip != enabled {
				t.Fatal("frame-skip setting not retained")
			}
		}
		mode = api.RC_OFF_MODE
		set(api.ENCODER_OPTION_RC_MODE, &mode)
		enabled := true
		set(api.ENCODER_OPTION_RC_FRAME_SKIP, &enabled)
		if enc.m_pEncContext.pSvcParam.BEnableFrameSkip {
			t.Fatal("frame skipping enabled without rate control")
		}
	})
	t.Run("IDR interval normalization", func(t *testing.T) {
		value := int(-1)
		set(api.ENCODER_OPTION_IDR_INTERVAL, &value)
		set(api.ENCODER_OPTION_IDR_INTERVAL, &value)
		var interval uint32
		if got := enc.GetOption(api.ENCODER_OPTION_IDR_INTERVAL, &interval); got != 0 || interval != 0 {
			t.Fatalf("interval=%d result=%d", interval, got)
		}
	})
	t.Run("padding and LTR period", func(t *testing.T) {
		padding, period := int32(1), int32(17)
		set(api.ENCODER_PADDING_PADDING, &padding)
		set(api.ENCODER_LTR_MARKING_PERIOD, &period)
		if enc.m_pEncContext.pSvcParam.IPaddingFlag != 1 || enc.m_pEncContext.pSvcParam.ILtrMarkPeriod != 17 {
			t.Fatal("padding/LTR settings not retained")
		}
	})
	t.Run("profile level references delivery", func(t *testing.T) {
		set(api.ENCODER_OPTION_PROFILE, &api.SProfileInfo{ILayer: 0, UiProfileIdc: api.PRO_HIGH})
		set(api.ENCODER_OPTION_LEVEL, &api.SLevelInfo{ILayer: 0, UiLevelIdc: api.LEVEL_3_1})
		references := int32(2)
		set(api.ENCODER_OPTION_NUMBER_REF, &references)
		set(api.ENCODER_OPTION_DELIVERY_STATUS, &api.SDeliveryStatus{BDeliveryFlag: true})
		p := enc.m_pEncContext.pSvcParam
		if p.SSpatialLayers[0].UiProfileIdc != api.PRO_HIGH || p.SSpatialLayers[0].UiLevelIdc != api.LEVEL_3_1 || p.INumRefFrame != 2 || !enc.m_pEncContext.bDeliveryFlag {
			t.Fatal("profile/level/reference/delivery settings not applied")
		}
	})
	t.Run("enum option round trip", func(t *testing.T) {
		format := api.VideoFormatI420
		set(api.ENCODER_OPTION_DATAFORMAT, &format)
		var readFormat api.EVideoFormatType
		if got := enc.GetOption(api.ENCODER_OPTION_DATAFORMAT, &readFormat); got != 0 || readFormat != format {
			t.Fatalf("format=%v result=%d", readFormat, got)
		}
		complexity := api.HIGH_COMPLEXITY
		set(api.ENCODER_OPTION_COMPLEXITY, &complexity)
		var readComplexity api.ECOMPLEXITY_MODE
		if got := enc.GetOption(api.ENCODER_OPTION_COMPLEXITY, &readComplexity); got != 0 || readComplexity != complexity {
			t.Fatalf("complexity=%v result=%d", readComplexity, got)
		}
	})
	t.Run("current path", func(t *testing.T) {
		path := "first"
		set(api.ENCODER_OPTION_CURRENT_PATH, &path)
		if enc.m_pEncContext.pSvcParam.pCurPath != path {
			t.Fatal("pointer path not retained")
		}
		set(api.ENCODER_OPTION_CURRENT_PATH, "second")
		if enc.m_pEncContext.pSvcParam.pCurPath != "second" {
			t.Fatal("string path not retained")
		}
	})
	t.Run("bitrate variation clamps", func(t *testing.T) {
		for _, tt := range []struct{ input, want int32 }{{-1, 0}, {200, 100}} {
			t.Run(fmt.Sprint(tt.input), func(t *testing.T) {
				set(api.ENCODER_OPTION_BITS_VARY_PERCENTAGE, &tt.input)
				if enc.m_pEncContext.pSvcParam.iBitsVaryPercentage != tt.want {
					t.Fatalf("variation=%d", enc.m_pEncContext.pSvcParam.iBitsVaryPercentage)
				}
			})
		}
	})
	t.Run("no-op compatibility options", func(t *testing.T) {
		value := int32(1)
		set(api.ENCODER_OPTION_INTER_SPATIAL_PRED, &value)
		set(api.ENCODER_OPTION_GET_STATISTICS, &value)
		if got := enc.GetOption(api.ENCODER_OPTION_INTER_SPATIAL_PRED, &value); got != 0 || value != 1 {
			t.Fatalf("compatibility option result=%d value=%d", got, value)
		}
		if got := enc.ForceIntraFrame(false, -1); got != 0 {
			t.Fatalf("non-IDR request=%d", got)
		}
	})
}

func TestEncoderTraceCallbackForms(t *testing.T) {
	t.Parallel()
	for _, form := range []string{"pointer", "typed function", "plain function"} {
		t.Run(form, func(t *testing.T) {
			t.Parallel()
			enc := validationEncoder(t, false)
			var seen bool
			var callback api.WelsTraceCallback = func(ctx any, level int32, message string) {
				if ctx != "test context" || level != api.WELS_LOG_WARNING || !strings.Contains(message, "validation callback") {
					t.Errorf("callback context=%v level=%d message=%q", ctx, level, message)
				}
				seen = true
			}
			var value any
			switch form {
			case "pointer":
				value = &callback
			case "typed function":
				value = callback
			case "plain function":
				value = func(ctx any, level int32, message string) { callback(ctx, level, message) }
			}
			if got := enc.SetOption(api.ENCODER_OPTION_TRACE_CALLBACK, value); got != 0 {
				t.Fatalf("set callback=%d", got)
			}
			var context any = "test context"
			contextValue := context
			if form == "pointer" {
				contextValue = &context
			}
			if got := enc.SetOption(api.ENCODER_OPTION_TRACE_CALLBACK_CONTEXT, contextValue); got != 0 {
				t.Fatalf("set context=%d", got)
			}
			level := int32(api.WELS_LOG_WARNING)
			if got := enc.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &level); got != 0 {
				t.Fatalf("set trace level=%d", got)
			}
			common.WelsLog(&enc.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_WARNING, "validation callback")
			if !seen {
				t.Fatal("registered callback was not invoked")
			}
		})
	}
}

func TestEncoderParameterStrategyChanges(t *testing.T) {
	t.Parallel()
	for _, strategy := range []api.EParameterSetStrategy{api.CONSTANT_ID, api.INCREASING_ID, api.SPS_LISTING, api.SPS_LISTING_AND_PPS_INCREASING, api.SPS_PPS_LISTING} {
		t.Run(fmt.Sprint(strategy), func(t *testing.T) {
			t.Parallel()
			enc := validationEncoder(t, true)
			original := enc.m_pEncContext.pSvcParam.ESpsPpsIdStrategy
			got := enc.SetOption(api.ENCODER_OPTION_SPS_PPS_ID_STRATEGY, &strategy)
			if strategy&api.SPS_LISTING != 0 {
				if got != int32(api.CmInitParaError) || enc.m_pEncContext.pSvcParam.ESpsPpsIdStrategy != original {
					t.Fatalf("forbidden strategy transition result=%d strategy=%v", got, enc.m_pEncContext.pSvcParam.ESpsPpsIdStrategy)
				}
			} else if got != 0 || enc.m_pEncContext.pSvcParam.ESpsPpsIdStrategy != strategy {
				t.Fatalf("allowed strategy transition result=%d strategy=%v", got, enc.m_pEncContext.pSvcParam.ESpsPpsIdStrategy)
			}
		})
	}
}

func TestEncoderDynamicReconfiguration(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		mutate func(*api.SEncParamExt)
		check  func(*testing.T, *api.SEncParamExt)
	}{
		{"resize", func(p *api.SEncParamExt) { p.IPicWidth = 48; p.SSpatialLayers[0].IVideoWidth = 48 }, func(t *testing.T, p *api.SEncParamExt) {
			if p.IPicWidth != 48 || p.SSpatialLayers[0].IVideoWidth != 48 {
				t.Fatal("resized dimensions not applied")
			}
		}},
		{"profile", func(p *api.SEncParamExt) { p.SSpatialLayers[0].UiProfileIdc = api.PRO_MAIN }, func(t *testing.T, p *api.SEncParamExt) {
			if p.SSpatialLayers[0].UiProfileIdc != api.PRO_MAIN {
				t.Fatal("profile not applied")
			}
		}},
		{"level", func(p *api.SEncParamExt) { p.SSpatialLayers[0].UiLevelIdc = api.LEVEL_3_1 }, func(t *testing.T, p *api.SEncParamExt) {
			if p.SSpatialLayers[0].UiLevelIdc != api.LEVEL_3_1 {
				t.Fatal("level not applied")
			}
		}},
		{"layer frame rate", func(p *api.SEncParamExt) { p.SSpatialLayers[0].FFrameRate = 15; p.ITemporalLayerNum = 2 }, func(t *testing.T, p *api.SEncParamExt) {
			if p.SSpatialLayers[0].FFrameRate != 15 {
				t.Fatal("layer frame rate not applied")
			}
		}},
		{"size limited slices", func(p *api.SEncParamExt) {
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SIZELIMITED_SLICE
			p.SSpatialLayers[0].SSliceArgument.UiSliceSizeConstraint = 1500
		}, func(t *testing.T, p *api.SEncParamExt) {
			if p.SSpatialLayers[0].SSliceArgument.UiSliceMode != api.SM_SIZELIMITED_SLICE {
				t.Fatal("slice mode not applied")
			}
		}},
		{"bitrate without reset", func(p *api.SEncParamExt) { p.ITargetBitrate = 120_000; p.SSpatialLayers[0].ISpatialBitrate = 120_000 }, func(t *testing.T, p *api.SEncParamExt) {
			if p.ITargetBitrate != 120_000 || p.SSpatialLayers[0].ISpatialBitrate != 120_000 {
				t.Fatal("bitrate not applied")
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			enc := validationEncoder(t, true)
			var params api.SEncParamExt
			if got := enc.GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &params); got != 0 {
				t.Fatalf("get parameters=%d", got)
			}
			tt.mutate(&params)
			if got := enc.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &params); got != 0 {
				t.Fatalf("reconfigure=%d", got)
			}
			if got := enc.GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &params); got != 0 {
				t.Fatalf("get updated parameters=%d", got)
			}
			tt.check(t, &params)
			yuv := make([]byte, params.IPicWidth*params.IPicHeight*3/2)
			var picture api.SSourcePicture
			setupI420Pic(&picture, yuv, params.IPicWidth, params.IPicHeight)
			var frame api.SFrameBSInfo
			if got := enc.EncodeFrame(&picture, &frame); got != 0 || frame.EFrameType != api.VideoFrameTypeIDR || frame.IFrameSizeInBytes <= 0 {
				t.Fatalf("encode after reconfigure: result=%d frame=%v bytes=%d", got, frame.EFrameType, frame.IFrameSizeInBytes)
			}
		})
	}
}

func TestEncoderLongTermReferenceReconfiguration(t *testing.T) {
	t.Parallel()
	for _, usage := range []api.EUsageType{api.CAMERA_VIDEO_REAL_TIME, api.SCREEN_CONTENT_REAL_TIME} {
		t.Run(fmt.Sprint(usage), func(t *testing.T) {
			t.Parallel()
			enc := validationEncoder(t, false)
			var params api.SEncParamExt
			enc.GetDefaultParams(&params)
			params.IPicWidth, params.IPicHeight = 32, 32
			params.FMaxFrameRate = 30
			params.ISpatialLayerNum = 1
			params.ITemporalLayerNum = 1
			params.IRCMode = api.RC_OFF_MODE
			params.IUsageType = usage
			params.BIsLosslessLink = true
			params.SSpatialLayers[0] = api.SSpatialLayerConfig{IVideoWidth: 32, IVideoHeight: 32, FFrameRate: 30, ISpatialBitrate: 100_000}
			if got := enc.InitializeExt(&params); got != 0 {
				t.Fatalf("initialize=%d", got)
			}
			for _, enabled := range []bool{true, false} {
				t.Run(fmt.Sprint(enabled), func(t *testing.T) {
					request := api.SLTRConfig{BEnableLongTermReference: enabled, ILTRRefNum: 1}
					if got := enc.SetOption(api.ENCODER_OPTION_LTR, &request); got != 0 {
						t.Fatalf("toggle LTR=%d", got)
					}
					p := enc.m_pEncContext.pSvcParam
					if p.BEnableLongTermReference != enabled || (enabled && p.ILTRRefNum < 1) || (!enabled && p.ILTRRefNum != 0) {
						t.Fatalf("LTR state=%v references=%d", p.BEnableLongTermReference, p.ILTRRefNum)
					}
				})
			}
		})
	}
}

func TestEncoderListingStrategySurvivesResize(t *testing.T) {
	t.Parallel()
	for _, strategy := range []api.EParameterSetStrategy{api.SPS_LISTING, api.SPS_LISTING_AND_PPS_INCREASING} {
		t.Run(fmt.Sprint(strategy), func(t *testing.T) {
			t.Parallel()
			enc := validationEncoder(t, true)
			var params api.SEncParamExt
			if got := enc.GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &params); got != 0 {
				t.Fatalf("get parameters=%d", got)
			}
			params.ESpsPpsIdStrategy = strategy
			if got := enc.InitializeExt(&params); got != 0 {
				t.Fatalf("initialize listing strategy=%d", got)
			}
			for _, width := range []int32{32, 48, 32} {
				t.Run(fmt.Sprint(width), func(t *testing.T) {
					params.IPicWidth, params.SSpatialLayers[0].IVideoWidth = width, width
					if got := enc.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &params); got != 0 {
						t.Fatalf("resize=%d", got)
					}
					yuv := make([]byte, width*32*3/2)
					var picture api.SSourcePicture
					setupI420Pic(&picture, yuv, width, 32)
					var frame api.SFrameBSInfo
					if got := enc.EncodeFrame(&picture, &frame); got != 0 || frame.EFrameType != api.VideoFrameTypeIDR || frame.IFrameSizeInBytes <= 0 {
						t.Fatalf("listing encode result=%d type=%v bytes=%d", got, frame.EFrameType, frame.IFrameSizeInBytes)
					}
					if enc.m_pEncContext.pSvcParam.ESpsPpsIdStrategy != strategy {
						t.Fatal("resize discarded parameter-set strategy")
					}
				})
			}
		})
	}
}

func TestEncoderRejectsInvalidFrameInputs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		picture *api.SSourcePicture
		info    *api.SFrameBSInfo
		want    api.CM_RETURN
	}{
		{"nil source", nil, &api.SFrameBSInfo{}, api.CmInitParaError},
		{"nil output", &api.SSourcePicture{}, nil, api.CmInitParaError},
		{"unsupported color format", &api.SSourcePicture{IColorFormat: int32(api.VideoFormatRGB)}, &api.SFrameBSInfo{}, api.CmInitParaError},
		{"undersized frame", &api.SSourcePicture{IColorFormat: int32(api.VideoFormatI420), IPicWidth: 8, IPicHeight: 16}, &api.SFrameBSInfo{}, api.CmUnsupportedData},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			enc := validationEncoder(t, true)
			if got := enc.EncodeFrame(tt.picture, tt.info); got != int32(tt.want) {
				t.Fatalf("EncodeFrame=%d, want %d", got, tt.want)
			}
		})
	}
}

func TestScreenReferenceBufferLifecycle(t *testing.T) {
	t.Parallel()
	info := NewSVAAFrameInfoExt()
	if got := RequestMemoryVaaScreen(&info.SVAAFrameInfo, 3, 16); got != 0 {
		t.Fatalf("allocate reference buffers=%d", got)
	}
	for i := range 3 {
		if len(info.pVaaBlockStaticIdc[i]) != 16 {
			t.Fatalf("reference %d length=%d", i, len(info.pVaaBlockStaticIdc[i]))
		}
		info.pVaaBlockStaticIdc[i][0] = byte(i + 1)
	}
	for i := range 3 {
		if info.pVaaBlockStaticIdc[i][0] != byte(i+1) {
			t.Fatal("reference buffers overlap")
		}
	}
	ReleaseMemoryVaaScreen(&info.SVAAFrameInfo, 3)
	ReleaseMemoryVaaScreen(&info.SVAAFrameInfo, 3)
	ReleaseMemoryVaaScreen(nil, 3)
	ReleaseMemoryVaaScreen(&SVAAFrameInfo{}, 3)
	for i := range 3 {
		if info.pVaaBlockStaticIdc[i] != nil {
			t.Fatalf("reference %d was not released", i)
		}
	}
}

func TestEncoderPaddingNALAndBounds(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name               string
		rawSize, frameSize int
		nalCount           int32
		want               int32
	}{
		{"valid padding", 64, 64, 1, ENC_RETURN_SUCCESS},
		{"raw buffer exhausted", 4, 64, 1, ENC_RETURN_MEMOVERFLOWFOUND},
		{"NAL slots exhausted", 64, 64, 0, ENC_RETURN_MEMOVERFLOWFOUND},
		{"output buffer exhausted", 64, 4, 1, ENC_RETURN_MEMALLOCERR},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := &sWelsEncCtx{pOut: &SWelsEncoderOutput{pBsBuffer: make([]byte, tt.rawSize), iCountNals: tt.nalCount, sNalList: make([]SWelsNalRaw, 1)}, pFrameBs: make([]byte, tt.frameSize), iFrameBsSize: int32(tt.frameSize)}
			common.InitBits(&ctx.pOut.sBsWrite, ctx.pOut.pBsBuffer, 0, int32(tt.rawSize))
			var size int32
			if got := WritePadding(ctx, 8, &size); got != tt.want {
				t.Fatalf("WritePadding=%d, want %d", got, tt.want)
			}
			if tt.want != ENC_RETURN_SUCCESS {
				if size != 0 || ctx.iPosBsBuffer != 0 {
					t.Fatal("failed padding advanced output")
				}
				return
			}
			want := append([]byte{0, 0, 0, 1, 12}, bytes.Repeat([]byte{255}, 8)...)
			want = append(want, 128)
			if size != int32(len(want)) || ctx.iPosBsBuffer != size || !bytes.Equal(ctx.pFrameBs[:size], want) {
				t.Fatalf("padding NAL=%x size=%d, want %x", ctx.pFrameBs[:size], size, want)
			}
		})
	}
}
