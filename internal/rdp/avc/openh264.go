//go:build !cgo || purego

package avc

import (
	"fmt"

	openh264 "github.com/define42/DevBox-Gateway-kvm/pkg/h264"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

type openH264Encoder struct {
	codec         *openh264.Encoder
	frame         openh264.Frame
	width, height int
}

func newCodecEncoderWithConfig(
	width, height, threads, frameRate, qp int, config codecConfig,
) (codecEncoder, int, error) {
	slices := min(threads, (height+15)/16)
	param, err := pureGoEncoderParams(width, height, slices, frameRate, qp, config)
	if err != nil {
		return nil, 0, err
	}

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
	return e.encodeDamage(i420, force, timestamp, damageHints{})
}

func (e *openH264Encoder) encodeDamage(i420 []byte, force bool, timestamp int64, hints damageHints) ([]byte, error) {
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
	e.frame.UnchangedMacroblocks = hints.unchanged
	e.frame.MacroblockMotion = hints.motion
	data, frameType, err := e.codec.Encode(&e.frame)
	e.frame.UnchangedMacroblocks = nil
	e.frame.MacroblockMotion = nil
	if err != nil {
		return nil, fmt.Errorf("%w: encode: %w", ErrCodec, err)
	}
	if frameType == api.VideoFrameTypeSkip || len(data) == 0 {
		return nil, fmt.Errorf("%w: empty access unit", ErrCodec)
	}

	return data, nil
}

func (e *openH264Encoder) settings() (codecSettings, error) {
	var effective api.SEncParamExt
	var statistics api.SEncoderStatistics
	if rv := e.codec.Raw().GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &effective); rv != 0 {
		return codecSettings{}, fmt.Errorf("%w: read effective settings: %d", ErrCodec, rv)
	}
	if rv := e.codec.Raw().GetOption(api.ENCODER_OPTION_GET_STATISTICS, &statistics); rv != 0 {
		return codecSettings{}, fmt.Errorf("%w: read statistics: %d", ErrCodec, rv)
	}
	layer := &effective.SSpatialLayers[0]

	return codecSettings{
		threads: int(effective.IMultipleThreadIdc), slices: int(layer.SSliceArgument.UiSliceNum),
		intraPeriod: effective.UiIntraPeriod, loopFilterDisableIDC: effective.ILoopFilterDisableIdc,
		maxFrameRate: effective.FMaxFrameRate, layerFrameRate: layer.FFrameRate, layerQP: int(layer.IDLayerQp),
		averageFrameQP: statistics.UiAverageFrameQP, inputFrames: statistics.UiInputFrameCount,
		lastTimestamp: e.frame.Timestamp,
	}, nil
}

func (e *openH264Encoder) close() {
	e.codec.Close()
	e.codec = nil
	e.frame = openh264.Frame{}
}
