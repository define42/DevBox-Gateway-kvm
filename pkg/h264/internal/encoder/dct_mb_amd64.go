//go:build amd64

package encoder

import (
	"reflect"
	"unsafe"

	"golang.org/x/sys/cpu"
)

// Checking the current callback preserves scalar selection and instrumentation
// installed after initialization, including wrappers around the builtin.
var dctLumaMbBuiltinPC = reflect.ValueOf(WelsDctFourT4_sse2).Pointer()

//go:noescape
func welsDctLumaMbAVX2(dct *[256]int16, enc *byte, encStride int, pred *[256]byte)

// dctLumaMbFast batches the builtin transform after one complete preflight.
// Rejected inputs retain the original per-quadrant validation, write order,
// alias semantics and callback behavior; this function never writes on false.
func dctLumaMbFast(coeff []int16, source []byte, sourceOffset int, stride int32,
	prediction []byte, predictionOffset int, transform PDctFunc) bool {
	if !cpu.X86.HasAVX2 || reflect.ValueOf(transform).Pointer() != dctLumaMbBuiltinPC {
		return false
	}
	if len(coeff) < 256 || stride < 0 || sourceOffset < 0 || sourceOffset > len(source) ||
		len(source)-sourceOffset < 16 || int64(stride)*15 > int64(len(source)-sourceOffset-16) ||
		predictionOffset < 0 || predictionOffset > len(prediction) || len(prediction)-predictionOffset < 256 {
		return false
	}
	dct := (*[256]int16)(coeff)
	enc := &source[sourceOffset]
	pred := (*[256]byte)(prediction[predictionOffset:])
	if quantScanRangesOverlap(unsafe.Pointer(dct), 512, unsafe.Pointer(enc), uintptr(stride)*15+16) ||
		quantScanRangesOverlap(unsafe.Pointer(dct), 512, unsafe.Pointer(pred), 256) {
		return false
	}
	welsDctLumaMbAVX2(dct, enc, int(stride), pred)
	return true
}
