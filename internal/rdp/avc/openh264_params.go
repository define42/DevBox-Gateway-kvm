package avc

import (
	"fmt"

	openh264 "github.com/define42/DevBox-Gateway-kvm/pkg/h264"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

// pureGoEncoderParams is the pure-Go backend's configuration. The native
// backend (internal/openh264) sets the same fields, and its tests compare the
// two streams, so the policy has one definition on each side of cgo.
func pureGoEncoderParams(width, height, slices, frameRate, qp int, config codecConfig) (*api.SEncParamExt, error) {
	param, err := openh264.DefaultEncoderParams(width, height, api.UNSPECIFIED_BIT_RATE, float32(frameRate))
	if err != nil {
		return nil, fmt.Errorf("%w: defaults: %w", ErrCodec, err)
	}

	param.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	param.UiIntraPeriod = config.intraPeriod
	param.ITemporalLayerNum = 1
	param.INumRefFrame = 1
	param.IRCMode = api.RC_OFF_MODE
	param.ILoopFilterDisableIdc = config.loopFilterDisableIDC
	param.IMultipleThreadIdc = uint16(slices)
	param.BUseLoadBalancing = false
	param.BEnableFrameSkip = false
	param.BEnableSceneChangeDetect = false
	param.BEnableBackgroundDetection = false
	param.BEnableAdaptiveQuant = false
	param.BEnableLongTermReference = false
	param.IEntropyCodingModeFlag = 0
	param.ESpsPpsIdStrategy = api.CONSTANT_ID

	layer := &param.SSpatialLayers[0]
	layer.IDLayerQp = int32(qp)
	layer.UiProfileIdc = api.PRO_BASELINE
	layer.SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
	layer.SSliceArgument.UiSliceNum = 1
	if slices > 1 {
		layer.SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
		layer.SSliceArgument.UiSliceNum = uint32(slices)
	}
	// MS-RDPEGFX 3.3.8.3.1 requires full-range BT.709. Advertise the
	// matrix used by scaleI420 in the SPS so independent decoders do not
	// assume studio-range BT.601.
	layer.BVideoSignalTypePresent = true
	layer.UiVideoFormat = uint8(api.VF_UNDEF)
	layer.BFullRange = true
	layer.BColorDescriptionPresent = true
	layer.UiColorPrimaries = uint8(api.CP_BT709)
	layer.UiTransferCharacteristics = uint8(api.TRC_BT709)
	layer.UiColorMatrix = uint8(api.CM_BT709)

	return param, nil
}
