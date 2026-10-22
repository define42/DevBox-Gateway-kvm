//go:build openh264_native && cgo && linux

package avc

/*
#cgo pkg-config: openh264
#cgo LDFLAGS: -lstdc++
#include <stdlib.h>
#include "native_openh264.h"
*/
import "C"

import (
    "fmt"
    "unsafe"
)

type openH264Encoder struct {
    handle *C.devbox_h264
    width, height int
}

func newCodecEncoder(width, height, threads, frameRate, qp int) (codecEncoder, int, error) {
    slices := min(threads, (height+15)/16)
    var actual, status C.int
    handle := C.devbox_h264_new(C.int(width),C.int(height),C.int(frameRate),C.int(qp),
        C.int(slices),&actual,&status)
    if handle == nil {
        return nil,0,fmt.Errorf("%w: native OpenH264 initialization: %d",ErrCodec,int(status))
    }
    return &openH264Encoder{handle:handle,width:width,height:height},int(actual),nil
}

func (e *openH264Encoder) encode(i420 []byte, force bool, timestamp int64) ([]byte,error) {
    if e.handle == nil || len(i420) != e.width*e.height*3/2 {
        return nil,ErrCodec
    }
    var out *C.uint8_t
    var size C.size_t
    var idr C.int
    if force { idr=1 }
    status:=C.devbox_h264_encode(e.handle,(*C.uint8_t)(unsafe.Pointer(&i420[0])),
        idr,C.uint64_t(timestamp),&out,&size)
    if out != nil { defer C.free(unsafe.Pointer(out)) }
    if status != 0 || size == 0 {
        return nil,fmt.Errorf("%w: native OpenH264 encode: %d",ErrCodec,int(status))
    }
    return C.GoBytes(unsafe.Pointer(out),C.int(size)),nil
}

func (e *openH264Encoder) close() {
    if e.handle != nil { C.devbox_h264_free(e.handle); e.handle=nil }
}
