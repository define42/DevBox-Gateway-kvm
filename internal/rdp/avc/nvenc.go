//go:build nvenc && cgo

package avc

import (
	"fmt"

	"github.com/define42/gokvm/internal/rdp/avc/internal/nvenc"
)

type nvencEncoder struct {
	codec *nvenc.Encoder
}

func newNVENCEncoder(width, height int) (codecEncoder, error) {
	codec, err := nvenc.New(width, height, FrameRate, codecQP)
	if err != nil {
		return nil, fmt.Errorf("%w: initialize nvenc: %w", ErrCodec, err)
	}

	return &nvencEncoder{codec: codec}, nil
}

func (e *nvencEncoder) encode(i420 []byte, force bool, timestamp int64) ([]byte, error) {
	data, err := e.codec.Encode(i420, force, timestamp)
	if err != nil {
		return nil, fmt.Errorf("%w: nvenc encode: %w", ErrCodec, err)
	}

	return data, nil
}

func (e *nvencEncoder) close() { e.codec.Close() }
