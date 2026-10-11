//go:build !nvenc || !cgo

package avc

import "fmt"

func newNVENCEncoder(_, _ int) (codecEncoder, error) {
	return nil, fmt.Errorf("%w: nvenc support requires building with CGO_ENABLED=1 and -tags nvenc", ErrCodec)
}
