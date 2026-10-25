package avc

import (
	"fmt"

	openh264 "github.com/define42/DevBox-Gateway-kvm/pkg/h264"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

const (
	// RDP runs the AVC stream over an ordered reliable channel and explicitly
	// requests IDRs for initialization, refreshes, resizes, and encode retries.
	// Avoid periodic IDR latency and full-desktop repaints between those events.
	codecIntraPeriod          uint32 = 0
	codecLoopFilterDisableIDC int32  = 1
)

// codecConfig keeps policy alternatives private while allowing benchmarks to
// compare them through the same construction path used in production.
type codecConfig struct {
	intraPeriod          uint32
	loopFilterDisableIDC int32
}

type openH264Encoder struct {
	codec         *openh264.Encoder
	frame         openh264.Frame
	width, height int
}

func newCodecEncoder(width, height, threads, frameRate, qp int) (codecEncoder, int, error) {
	return newCodecEncoderWithConfig(width, height, threads, frameRate, qp, codecConfig{
		intraPeriod:          codecIntraPeriod,
		loopFilterDisableIDC: codecLoopFilterDisableIDC,
	})
}

func newCodecEncoderWithConfig(
	width, height, threads, frameRate, qp int, config codecConfig,
) (codecEncoder, int, error) {
	slices := min(threads, (height+15)/16)
	param, err := openh264.DefaultEncoderParams(width, height, api.UNSPECIFIED_BIT_RATE, float32(frameRate))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: defaults: %w", ErrCodec, err)
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

	codec, err := openh264.NewEncoder(param)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: initialize: %w", ErrCodec, err)
	}

	var effective api.SEncParamExt
	if rv := codec.Raw().GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &effective); rv != 0 {
		codec.Close()

		return nil, 0, fmt.Errorf("%w: read initialized parameters: %d", ErrCodec, rv)
	}
	slices = int(effective.SSpatialLayers[0].SSliceArgument.UiSliceNum)
	if slices < 1 {
		codec.Close()

		return nil, 0, fmt.Errorf("%w: initialized with no slices", ErrCodec)
	}

	return &openH264Encoder{codec: codec, width: width, height: height}, slices, nil
}

func (e *openH264Encoder) encode(i420 []byte, force bool, timestamp int64) ([]byte, error) {
	return e.encodeDamage(i420, force, timestamp, nil)
}

func (e *openH264Encoder) encodeDamage(
	i420 []byte, force bool, timestamp int64, unchanged []bool,
) ([]byte, error) {
	if force {
		if err := e.codec.ForceIntraFrame(); err != nil {
			return nil, fmt.Errorf("%w: force IDR: %w", ErrCodec, err)
		}
	}

	luma := e.width * e.height
	chroma := luma / 4
	e.frame.Width, e.frame.Height = e.width, e.height
	e.frame.Y = i420[:luma]
	e.frame.U = i420[luma : luma+chroma]
	e.frame.V = i420[luma+chroma:]
	e.frame.Timestamp = timestamp
	e.frame.UnchangedMacroblocks = unchanged
	data, frameType, err := e.codec.Encode(&e.frame)
	e.frame.UnchangedMacroblocks = nil
	if err != nil {
		return nil, fmt.Errorf("%w: encode: %w", ErrCodec, err)
	}
	if frameType == api.VideoFrameTypeSkip || len(data) == 0 {
		return nil, fmt.Errorf("%w: empty access unit", ErrCodec)
	}

	return data, nil
}

func (e *openH264Encoder) close() {
	e.codec.Close()
	e.codec = nil
	e.frame = openh264.Frame{}
}
