//go:build linux && amd64

package avc

import (
	"testing"

	"golang.org/x/sys/cpu"
)

func TestRGB32ToI420DamageGuardPages(t *testing.T) {
	t.Parallel()
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 is unavailable")
	}
	for _, width := range []int{32, 64, 96, 256} {
		for _, redIndex := range []int{0, 2} {
			src0, src1 := i420GuardedBuffer(t, width*4), i420GuardedBuffer(t, width*4)
			y0, y1 := i420GuardedBuffer(t, width), i420GuardedBuffer(t, width)
			u, v := i420GuardedBuffer(t, width/2), i420GuardedBuffer(t, width/2)
			for i := range src0 {
				src0[i], src1[i] = byte(i*37+11), byte(i*53+7)
			}
			guardedMask := make([]bool, width/16+2)
			for i := range guardedMask {
				guardedMask[i] = true
			}
			mask := guardedMask[1 : len(guardedMask)-1]
			if _, ok := rgb32ToI420Damage(y0, y1, u, v, src0, src1, mask, redIndex); !ok {
				t.Fatalf("width%d red%d: valid guarded input was rejected", width, redIndex)
			}
			if !guardedMask[0] || !guardedMask[len(guardedMask)-1] {
				t.Fatal("conversion wrote outside the unchanged mask")
			}
			wantY0, wantY1, wantU, wantV := referenceRGB32I420Rows(src0, src1, width, redIndex == 2)
			checkI420SIMDPlanes(t, y0, y1, u, v, wantY0, wantY1, wantU, wantV)
		}
	}
}
