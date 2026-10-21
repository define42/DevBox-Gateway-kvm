package common

import (
	"bytes"
	"math/rand"
	"reflect"
	"testing"
)

func TestChromaSIMDParity(t *testing.T) {
	var funcs SMcFunc
	InitMcFunc(&funcs, WELS_CPU_SSE2)
	if reflect.ValueOf(funcs.PMcChromaFunc).Pointer() != reflect.ValueOf(McChroma_sse2).Pointer() {
		t.Fatal("SSE2 chroma dispatch was not selected")
	}
	rng := rand.New(rand.NewSource(912))
	for _, size := range [][2]int32{{8, 8}, {8, 4}, {4, 8}, {4, 4}, {4, 2}, {2, 4}, {2, 2}, {3, 5}} {
		for dx := int16(-8); dx < 8; dx++ {
			for dy := int16(-8); dy < 8; dy++ {
				const srcOff, dstOff, srcStride, dstStride = 3, 5, 19, 13
				src := make([]byte, srcOff+int(size[1])*srcStride+int(size[0])+1)
				rng.Read(src)
				srcBefore := append([]byte(nil), src...)
				got := bytes.Repeat([]byte{0xa5}, dstOff+(int(size[1])-1)*dstStride+int(size[0])+7)
				want := append([]byte(nil), got...)
				McChroma_c(src, srcOff, srcStride, want, dstOff, dstStride, dx, dy, size[0], size[1])
				funcs.PMcChromaFunc(src, srcOff, srcStride, got, dstOff, dstStride, dx, dy, size[0], size[1])
				if !bytes.Equal(got, want) || !bytes.Equal(src, srcBefore) {
					t.Fatalf("size %v MV(%d,%d): got=%v want=%v", size, dx, dy, got, want)
				}
			}
		}
	}
}

func TestChromaSIMDAliasesAndStrides(t *testing.T) {
	for _, width := range []int32{2, 4, 8} {
		for _, stride := range []int32{0, 2, 19, -19} {
			for _, delta := range []int{-1, 0, 1, 20, 400} {
				got := make([]byte, 1024)
				for i := range got {
					got[i] = byte(i*37 + 11)
				}
				want := bytes.Clone(got)
				McChroma_c(want, 200, stride, want, 200+delta, stride, -3, 5, width, 8)
				McChroma_sse2(got, 200, stride, got, 200+delta, stride, -3, 5, width, 8)
				if !bytes.Equal(got, want) {
					t.Fatalf("width=%d stride=%d delta=%d", width, stride, delta)
				}
			}
		}
	}
}
