//go:build cgo && !purego

// Package openh264 encodes H.264 with the native OpenH264 library, including
// its NASM assembly. tools/build-openh264.sh builds the pinned release into
// third_party/openh264, which is linked statically. It is separate from avc
// because cgo packages cannot contain Go assembly.
package openh264

/*
#cgo CFLAGS: -I${SRCDIR}/../../../../../third_party/openh264/include
#cgo LDFLAGS: ${SRCDIR}/../../../../../third_party/openh264/lib/libopenh264.a -lstdc++ -lm -lpthread
#include "openh264.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

var (
	errGeometry = errors.New("invalid openh264 frame geometry")
	errOptions  = errors.New("invalid openh264 frame rate, quantizer or slices")
	errClosed   = errors.New("openh264 encoder is closed")
	errBuffer   = errors.New("invalid i420 buffer length")
	errNative   = errors.New("native openh264 operation failed")
)

// Config selects the stream geometry and the encoder policy.
type Config struct {
	Width, Height        int
	FrameRate, QP        int
	Slices               int
	IntraPeriod          uint32
	LoopFilterDisableIDC int32
}

// Settings reports the encoder's effective configuration and statistics.
type Settings struct {
	Threads              int
	Slices               int
	IntraPeriod          uint32
	LoopFilterDisableIDC int32
	MaxFrameRate         float32
	LayerFrameRate       float32
	LayerQP              int
	AverageFrameQP       uint32
	InputFrames          uint32
}

// Encoder owns one native encoder. Calls must be serialized, including Close.
type Encoder struct {
	native *C.DevBoxKVMOpenH264
	size   int
	slices int
}

// New creates a Baseline/CAVLC real-time encoder for even I420 dimensions.
func New(config Config) (*Encoder, error) {
	if config.Width < 16 || config.Height < 16 || config.Width%2 != 0 || config.Height%2 != 0 {
		return nil, errGeometry
	}
	if config.FrameRate < 1 || config.QP < 0 || config.QP > 51 || config.Slices < 1 {
		return nil, errOptions
	}
	native := C.DevBoxKVMOpenH264Config{
		width:                   C.int(config.Width),
		height:                  C.int(config.Height),
		frame_rate:              C.int(config.FrameRate),
		qp:                      C.int(config.QP),
		slices:                  C.int(config.Slices),
		intra_period:            C.uint(config.IntraPeriod),
		loop_filter_disable_idc: C.int(config.LoopFilterDisableIDC),
	}
	var detail [256]C.char
	var slices C.int
	codec := C.devbox_kvm_openh264_new(&native, &slices, &detail[0], C.size_t(len(detail)))
	if codec == nil {
		return nil, fmt.Errorf("%w: %s", errNative, C.GoString(&detail[0]))
	}

	return &Encoder{native: codec, size: config.Width * config.Height * 3 / 2, slices: int(slices)}, nil
}

// Slices reports the number of slices the encoder initialized.
func (e *Encoder) Slices() int { return e.slices }

// Encode returns one Annex-B access unit owned by the caller, or nil when the
// encoder skipped the frame. The encoder reads i420 only during the call.
//
//nolint:nlreturn // cgo-generated pointer checks trigger false positives.
func (e *Encoder) Encode(i420 []byte, forceIDR bool, timestamp int64) ([]byte, error) {
	if e.native == nil {
		return nil, errClosed
	}
	if len(i420) != e.size {
		return nil, errBuffer
	}
	force := C.int(0)
	if forceIDR {
		force = 1
	}
	size := C.devbox_kvm_openh264_encode(e.native, (*C.uint8_t)(unsafe.Pointer(&i420[0])), force, C.int64_t(timestamp))
	if size < 0 {
		return nil, fmt.Errorf("%w: encode status %d", errNative, -size)
	}
	if size == 0 {
		return nil, nil
	}
	data := make([]byte, int(size))
	C.devbox_kvm_openh264_copy(e.native, (*C.uint8_t)(unsafe.Pointer(&data[0])))

	return data, nil
}

// Settings reads the effective configuration and encoded-frame statistics.
//
//nolint:nlreturn // cgo-generated pointer checks trigger false positives.
func (e *Encoder) Settings() (Settings, error) {
	if e.native == nil {
		return Settings{}, errClosed
	}
	var native C.DevBoxKVMOpenH264Settings
	if status := C.devbox_kvm_openh264_settings(e.native, &native); status != 0 {
		return Settings{}, fmt.Errorf("%w: settings status %d", errNative, int(status))
	}

	return Settings{
		Threads:              int(native.threads),
		Slices:               int(native.slices),
		IntraPeriod:          uint32(native.intra_period),
		LoopFilterDisableIDC: int32(native.loop_filter_disable_idc),
		MaxFrameRate:         float32(native.max_frame_rate),
		LayerFrameRate:       float32(native.layer_frame_rate),
		LayerQP:              int(native.layer_qp),
		AverageFrameQP:       uint32(native.average_frame_qp),
		InputFrames:          uint32(native.input_frames),
	}, nil
}

// Close releases the native encoder. It is safe to call more than once.
func (e *Encoder) Close() {
	if e.native != nil {
		C.devbox_kvm_openh264_free(e.native)
		e.native = nil
	}
}
