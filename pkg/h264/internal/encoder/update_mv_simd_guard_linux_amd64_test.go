//go:build linux && amd64

package encoder

import (
	"fmt"
	"testing"
	"unsafe"
)

func TestUpdateMbMvSIMDGuardPages(t *testing.T) {
	value := SMVUnitXY{-32768, 32767}
	for _, atEnd := range []bool{false, true} {
		for _, raw := range []bool{false, true} {
			t.Run(fmt.Sprintf("end%t/raw%t", atEnd, raw), func(t *testing.T) {
				storage, _ := intraDCGuardedBuffer(t, 64, atEnd)
				dst := (*[16]SMVUnitXY)(unsafe.Pointer(&storage[0]))
				if raw {
					updateMbMvSSE2(dst, 0x7fff8000)
				} else {
					UpdateMbMv_sse2(dst[:], value)
				}
				for i, got := range dst {
					if got != value {
						t.Fatalf("entry%d: got%v want%v", i, got, value)
					}
				}
			})
		}
		for length := 1; length < 16; length++ {
			t.Run(fmt.Sprintf("end%t/short%d", atEnd, length), func(t *testing.T) {
				storage, _ := intraDCGuardedBuffer(t, length*4, atEnd)
				dst := unsafe.Slice((*SMVUnitXY)(unsafe.Pointer(&storage[0])), length)
				panicText := updateMvCallPanic(func() { UpdateMbMv_sse2(dst, value) })
				if panicText == "" {
					t.Fatal("short output did not panic")
				}
				for i, got := range dst {
					if got != value {
						t.Fatalf("partial entry%d: got%v want%v", i, got, value)
					}
				}
			})
		}
	}
}
