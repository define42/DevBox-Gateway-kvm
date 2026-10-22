//go:build linux && amd64

package encoder

import (
	"fmt"
	"runtime/debug"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

func TestDCTFourAVX2ExactBufferGuards(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, stride := range []int{0, 1, 8, 19, 127} {
		t.Run(fmt.Sprintf("stride%d", stride), func(t *testing.T) {
			old := debug.SetPanicOnFault(true)
			defer debug.SetPanicOnFault(old)
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("DCT faulted with valid exact buffers: %v", r)
				}
			}()
			p1 := idctGuardedBuffer(t, 7*stride+8)
			p2 := idctGuardedBuffer(t, 7*stride+8)
			output := idctGuardedBuffer(t, 128)
			got := (*[64]int16)(unsafe.Pointer(&output[0]))
			for i := range p1 {
				p1[i], p2[i] = byte(i*29+17), byte(i*13+71)
			}
			var want [64]int16
			WelsDctFourT4_c(want[:], p1, 0, int32(stride), p2, 0, int32(stride))
			welsDctFourT4AVX2(got, &p1[0], stride, &p2[0], stride)
			if *got != want {
				t.Fatal("guarded DCT coefficient mismatch")
			}
		})
	}
}
