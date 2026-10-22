//go:build amd64

package common

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

func copyNxMReference(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32, n, m int) {
	for i := 0; i < m; i++ {
		copy(pDst[iDstOff:iDstOff+n], pSrc[iSrcOff:iSrcOff+n])
		iDstOff += int(iStrideD)
		iSrcOff += int(iStrideS)
	}
}

// Every width, height, stride and offset that fits must match the row loop,
// including same-buffer copies whose rows overlap.
func TestCopyBlockFastParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(4, 16))
	for _, width := range []int{4, 8, 16} {
		for height := 1; height <= 17; height++ {
			for trial := 0; trial < 64; trial++ {
				size := 64 + rng.IntN(512)
				src := make([]byte, size)
				for i := range src {
					src[i] = byte(rng.Uint32())
				}
				dstStride := int32(width + rng.IntN(24))
				srcStride := int32(width + rng.IntN(24))
				if trial%5 == 0 {
					srcStride = int32(rng.IntN(width)) // overlapping source rows
				}
				srcOff := rng.IntN(size)
				dstOff := rng.IntN(size)
				same := trial%3 == 0
				want := make([]byte, size)
				got := make([]byte, size)
				for i := range want {
					want[i] = byte(i * 7)
				}
				copy(got, want)
				wantSrc, gotSrc := src, src
				if same {
					copy(want, src)
					copy(got, src)
					wantSrc, gotSrc = want, got
				}
				fits := srcOff+(height-1)*int(srcStride)+width <= size && dstOff+(height-1)*int(dstStride)+width <= size
				if !fits {
					if copyBlockFast(got, dstOff, dstStride, gotSrc, srcOff, srcStride, width, height) {
						t.Fatalf("w%d h%d: out-of-range span accepted", width, height)
					}
					continue
				}
				copyNxMReference(want, dstOff, dstStride, wantSrc, srcOff, srcStride, width, height)
				welsCopyNxM(got, dstOff, dstStride, gotSrc, srcOff, srcStride, width, height)
				if !bytes.Equal(got, want) {
					t.Fatalf("w%d h%d strides %d/%d offsets %d/%d same=%t: copy differs", width, height, dstStride, srcStride, dstOff, srcOff, same)
				}
			}
		}
	}
}

func TestCopyBlockFastRejects(t *testing.T) {
	buf := make([]byte, 256)
	for _, c := range []struct {
		dstStride, srcStride int32
		dstOff, srcOff       int
		height               int
	}{
		{-16, 16, 200, 0, 4}, {16, -16, 0, 200, 4}, {16, 16, -1, 0, 4}, {16, 16, 0, 0, 0}, {16, 16, 241, 0, 1}, {16, 16, 0, 0, 17},
	} {
		if copyBlockFast(buf, c.dstOff, c.dstStride, buf, c.srcOff, c.srcStride, 16, c.height) {
			t.Fatalf("%+v accepted", c)
		}
	}
}
