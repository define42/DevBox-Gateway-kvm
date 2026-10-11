//go:build nvenc && cgo

// Package nvenc implements synchronous H.264 encoding through FFmpeg's NVENC
// backend. It is separate from avc because cgo packages cannot contain Go assembly.
package nvenc

/*
#cgo pkg-config: libavcodec libavutil
#include "nvenc.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

var (
	errGeometry = errors.New("invalid nvenc frame geometry")
	errOptions  = errors.New("invalid nvenc frame rate or quantizer")
	errClosed   = errors.New("nvenc encoder is closed")
	errBuffer   = errors.New("invalid i420 buffer length")
	errNative   = errors.New("native nvenc operation failed")
)

// Encoder owns one native encoder. Calls must be serialized, including Close.
type Encoder struct {
	native *C.GokvmNVENC
	size   int
}

// New creates an encoder for even I420 dimensions. Timestamps use milliseconds.
func New(width, height, frameRate, qp int) (*Encoder, error) {
	if width < 16 || height < 16 || width > 4096 || height > 4096 || width%2 != 0 || height%2 != 0 {
		return nil, errGeometry
	}
	if frameRate < 1 || frameRate > 240 || qp < 0 || qp > 51 {
		return nil, errOptions
	}
	var detail [256]C.char
	native := C.gokvm_nvenc_new(C.int(width), C.int(height), C.int(frameRate), C.int(qp),
		&detail[0], C.size_t(len(detail)))
	if native == nil {
		return nil, fmt.Errorf("%w: %s", errNative, C.GoString(&detail[0]))
	}

	return &Encoder{native: native, size: width * height * 3 / 2}, nil
}

// Encode returns one Annex-B access unit owned by the caller. No Go pointer is
// retained by the encoder: input samples are copied into a native AVFrame.
//
//nolint:gocritic,nlreturn // cgo-generated pointer checks and byte copying trigger false positives.
func (e *Encoder) Encode(i420 []byte, force bool, timestamp int64) ([]byte, error) {
	if e.native == nil {
		return nil, errClosed
	}
	if len(i420) != e.size {
		return nil, fmt.Errorf("%w %d, want %d", errBuffer, len(i420), e.size)
	}
	var idr C.int
	if force {
		idr = 1
	}
	var detail [256]C.char
	var data *C.uint8_t
	var size C.int
	result := C.gokvm_nvenc_encode(e.native, (*C.uint8_t)(unsafe.Pointer(&i420[0])),
		idr, C.int64_t(timestamp), &data, &size, &detail[0], C.size_t(len(detail)))
	if result < 0 {
		return nil, fmt.Errorf("%w: %s", errNative, C.GoString(&detail[0]))
	}

	return C.GoBytes(unsafe.Pointer(data), size), nil
}

// Close releases all native and GPU resources and is safe to call repeatedly.
func (e *Encoder) Close() {
	if e.native != nil {
		C.gokvm_nvenc_free(e.native)
		e.native = nil
	}
}
