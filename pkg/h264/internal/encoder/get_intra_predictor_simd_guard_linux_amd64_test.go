//go:build linux && amd64

package encoder

import (
	"bytes"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestIntraSIMDGuardPages(t *testing.T) {
	var scalar, simd SWelsFuncPtrList
	WelsInitIntraPredFuncs(&scalar, 0)
	WelsInitIntraPredFuncs(&simd, common.WELS_CPU_SSE2)
	for _, size := range []int{8, 16} {
		for _, direction := range []int{1, -1} {
			stride := size + 1
			refOff, refLen := stride+1, size*stride+1
			if direction < 0 {
				refOff, refLen = (size-1)*stride+1, size*stride+size+1
			}
			ref := idctGuardedBuffer(t, refLen)
			for i := range ref {
				ref[i] = byte(i * 127)
			}
			stride *= direction
			pred := idctGuardedBuffer(t, size*size)
			want := make([]byte, size*size)
			for mode := 0; mode < 7; mode++ {
				if size == 16 {
					scalar.pfGetLumaI16x16Pred[mode](want, 0, ref, refOff, int32(stride))
					simd.pfGetLumaI16x16Pred[mode](pred, 0, ref, refOff, int32(stride))
				} else {
					scalar.pfGetChromaPred[mode](want, 0, ref, refOff, int32(stride))
					simd.pfGetChromaPred[mode](pred, 0, ref, refOff, int32(stride))
				}
				if !bytes.Equal(want, pred) {
					t.Fatalf("size=%d stride=%d mode=%d prediction mismatch", size, stride, mode)
				}
			}
			srcStride := size + 3
			src := idctGuardedBuffer(t, (size-1)*srcStride+size)
			for i := range src {
				src[i] = byte(i * 73)
			}
			var mode, wantMode int32
			var gotCost, wantCost int32
			if size == 16 {
				gotCost = intra16CombinedSad(ref, refOff, int32(stride), src, 0, int32(srcStride), &mode, 37, nil, 0)
				wantCost = WelsSampleSadIntra16x16Combined3_c(ref, refOff, int32(stride), src, 0,
					int32(srcStride), &wantMode, 37, want, 0)
			} else {
				gotCost = intra8CombinedSad(ref, refOff, int32(stride), src, 0, int32(srcStride),
					&mode, 37, nil, 0, ref, refOff, src, 0)
				wantCost = WelsSampleSadIntra8x8Combined3_c(ref, refOff, int32(stride), src, 0,
					int32(srcStride), &wantMode, 37, make([]byte, 128), 0, ref, refOff, src, 0)
			}
			if gotCost != wantCost || mode != wantMode {
				t.Fatalf("size=%d stride=%d SAD/mode mismatch", size, stride)
			}
		}
	}
}

// The packed result is exactly three int32 values. In Go's assembler MOVD
// with an XMM source and memory destination is an eight-byte store; these
// kernels must use MOVL so the final cost stops at the guard page.
func TestIntraSADSIMDGuardedCosts(t *testing.T) {
	for _, size := range []int{8, 16} {
		stride, srcStride := size+1, size+3
		ref := idctGuardedBuffer(t, size*stride+1)
		src := idctGuardedBuffer(t, (size-1)*srcStride+size)
		for i := range ref {
			ref[i] = byte(i * 127)
		}
		for i := range src {
			src[i] = byte(i * 73)
		}
		storage := idctGuardedBuffer(t, 12)
		costs := (*[3]int32)(unsafe.Pointer(&storage[0]))
		refOff := stride + 1
		if size == 16 {
			intraSad16x16SSE2(src, srcStride, ref[1:17], ref[stride:], stride, costs)
		} else {
			t0 := intraTopSum(ref, refOff, int32(stride), 4)
			t1 := intraTopSum(ref, refOff+4, int32(stride), 4)
			l0 := intraLeftSum(ref, refOff, int32(stride), 4)
			l1 := intraLeftSum(ref, refOff+4*stride, int32(stride), 4)
			top := intraChromaDCRow((t0+l0+4)>>3, (t1+2)>>2)
			bottom := intraChromaDCRow((l1+2)>>2, (t1+l1+4)>>3)
			intraSad8x8SSE2(src, srcStride, ref[1:9], ref[stride:], stride, top, bottom, costs)
		}
		predictors := [3]PGetIntraPredFunc{common.WelsI16x16LumaPredV_c,
			common.WelsI16x16LumaPredH_c, WelsI16x16LumaPredDc_c}
		cost := common.WelsSampleSad16x16_c
		if size == 8 {
			predictors = [3]PGetIntraPredFunc{WelsIChromaPredV_c, WelsIChromaPredH_c, WelsIChromaPredDc_c}
			cost = common.WelsSampleSad8x8_c
		}
		pred := make([]byte, size*size)
		for mode, predictor := range predictors {
			predictor(pred, 0, ref, refOff, int32(stride))
			want := cost(pred, 0, int32(size), src, 0, int32(srcStride))
			if costs[mode] != want {
				t.Fatalf("size=%d mode=%d got=%d want=%d", size, mode, costs[mode], want)
			}
		}
	}
}
