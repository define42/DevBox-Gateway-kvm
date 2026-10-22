//go:build cgo && !purego

package avc

import (
	"fmt"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/avc/internal/openh264"
)

// nativeOpenH264Encoder runs the native OpenH264 library with its NASM kernels,
// patched to accept the same unchanged-macroblock hints as the pure-Go port.
type nativeOpenH264Encoder struct {
	codec         *openh264.Encoder
	lastTimestamp int64
}

func newCodecEncoderWithConfig(
	width, height, threads, frameRate, qp int, config codecConfig,
) (codecEncoder, int, error) {
	codec, err := openh264.New(openh264.Config{
		Width: width, Height: height, FrameRate: frameRate, QP: qp,
		Slices:               min(threads, (height+15)/16),
		IntraPeriod:          config.intraPeriod,
		LoopFilterDisableIDC: config.loopFilterDisableIDC,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("%w: initialize: %w", ErrCodec, err)
	}
	if codec.Slices() < 1 {
		codec.Close()

		return nil, 0, fmt.Errorf("%w: initialized with no slices", ErrCodec)
	}

	return &nativeOpenH264Encoder{codec: codec}, codec.Slices(), nil
}

func (e *nativeOpenH264Encoder) encode(i420 []byte, force bool, timestamp int64) ([]byte, error) {
	return e.encodeDamage(i420, force, timestamp, damageHints{})
}

func (e *nativeOpenH264Encoder) encodeDamage(
	i420 []byte, force bool, timestamp int64, hints damageHints,
) ([]byte, error) {
	e.lastTimestamp = timestamp
	data, err := e.codec.EncodeHinted(i420, force, timestamp, hints.unchanged, hints.motion)
	if err != nil {
		return nil, fmt.Errorf("%w: encode: %w", ErrCodec, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty access unit", ErrCodec)
	}

	return data, nil
}

func (e *nativeOpenH264Encoder) settings() (codecSettings, error) {
	s, err := e.codec.Settings()
	if err != nil {
		return codecSettings{}, fmt.Errorf("%w: %w", ErrCodec, err)
	}

	return codecSettings{
		threads: s.Threads, slices: s.Slices,
		intraPeriod: s.IntraPeriod, loopFilterDisableIDC: s.LoopFilterDisableIDC,
		maxFrameRate: s.MaxFrameRate, layerFrameRate: s.LayerFrameRate, layerQP: s.LayerQP,
		averageFrameQP: s.AverageFrameQP, inputFrames: s.InputFrames, lastTimestamp: e.lastTimestamp,
	}, nil
}

func (e *nativeOpenH264Encoder) close() { e.codec.Close() }
