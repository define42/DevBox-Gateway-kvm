//go:build linux && amd64

package encoder

import (
	"fmt"
	"runtime/debug"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

func TestDctLumaMbExactBufferGuards(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, stride := range []int{0, 1, 16, 19, 127} {
		t.Run(fmt.Sprintf("stride%d", stride), func(t *testing.T) {
			old := debug.SetPanicOnFault(true)
			defer debug.SetPanicOnFault(old)
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("macroblock DCT faulted with exact buffers: %v", r)
				}
			}()
			source := idctGuardedBuffer(t, 15*stride+16)
			prediction := idctGuardedBuffer(t, 256)
			output := idctGuardedBuffer(t, 512)
			got := (*[256]int16)(unsafe.Pointer(&output[0]))
			for i := range source {
				source[i] = byte(i*29 + 17)
			}
			for i := range prediction {
				prediction[i] = byte(i*13 + 71)
			}
			var want [256]int16
			referenceDctLumaMb(want[:], source, 0, int32(stride), prediction, 0, WelsDctFourT4_c)
			WelsDctMb(got[:], source, 0, int32(stride), prediction, 0, WelsDctFourT4_sse2)
			if *got != want {
				t.Fatal("guarded macroblock DCT coefficient mismatch")
			}
		})
	}
}
