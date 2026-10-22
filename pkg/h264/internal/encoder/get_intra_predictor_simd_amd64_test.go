//go:build amd64

package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestIntraPredictorSIMDParity(t *testing.T) {
	var scalar, simd SWelsFuncPtrList
	WelsInitIntraPredFuncs(&scalar, 0)
	WelsInitIntraPredFuncs(&simd, common.WELS_CPU_SSE2)
	rng := rand.New(rand.NewSource(901))
	for _, size := range []int{8, 16} {
		for _, stride := range []int32{17, 31, 64, -17, -31, -64} {
			for iteration := 0; iteration < 120; iteration++ {
				ref := make([]byte, 4096)
				_, _ = rng.Read(ref)
				if iteration < 4 {
					for i := range ref {
						ref[i] = []byte{0, 255, 128, byte((i & 1) * 255)}[iteration]
					}
				}
				for mode := 0; mode < 7; mode++ {
					want := bytes.Repeat([]byte{0xa5}, size*size+26)
					got := bytes.Clone(want)
					if size == 16 {
						scalar.pfGetLumaI16x16Pred[mode](want, 13, ref, 2048, stride)
						simd.pfGetLumaI16x16Pred[mode](got, 13, ref, 2048, stride)
					} else {
						scalar.pfGetChromaPred[mode](want, 13, ref, 2048, stride)
						simd.pfGetChromaPred[mode](got, 13, ref, 2048, stride)
					}
					if !bytes.Equal(want, got) {
						t.Fatalf("size=%d stride=%d iteration=%d mode=%d mismatch", size, stride, iteration, mode)
					}
				}
			}
		}
	}
}

func TestIntraPlaneSIMDExtremeGradients(t *testing.T) {
	for _, size := range []int{8, 16} {
		for signs := 0; signs < 4; signs++ {
			const stride = 33
			ref := make([]byte, stride*17+1)
			const off = stride + 1
			for i := -1; i < size; i++ {
				top, left := byte(0), byte(0)
				if (i >= size/2) != (signs&1 != 0) {
					top = 255
				}
				if (i >= size/2) != (signs&2 != 0) {
					left = 255
				}
				ref[off-stride+i], ref[off-1+i*stride] = top, left
			}
			got, want := make([]byte, size*size), make([]byte, size*size)
			if size == 16 {
				WelsI16x16LumaPredPlane_c(want, 0, ref, off, stride)
				intra16Plane(got, 0, ref, off, stride)
			} else {
				WelsIChromaPredPlane_c(want, 0, ref, off, stride)
				intra8Plane(got, 0, ref, off, stride)
			}
			if !bytes.Equal(want, got) {
				t.Fatalf("size=%d signs=%d mismatch", size, signs)
			}
		}
	}
}

func TestIntraHorizontalSIMDAlias(t *testing.T) {
	for _, size := range []int{8, 16} {
		for _, stride := range []int32{1, 17, -1, -17} {
			for _, dstOff := range []int{0, 128, 256, 384} {
				want := make([]byte, 1024)
				for i := range want {
					want[i] = byte(i * 37)
				}
				got := bytes.Clone(want)
				if size == 16 {
					common.WelsI16x16LumaPredH_c(want, dstOff, want, 256, stride)
					intra16Horizontal(got, dstOff, got, 256, stride)
				} else {
					WelsIChromaPredH_c(want, dstOff, want, 256, stride)
					intra8Horizontal(got, dstOff, got, 256, stride)
				}
				if !bytes.Equal(want, got) {
					t.Fatalf("size=%d stride=%d dstOff=%d alias mismatch", size, stride, dstOff)
				}
			}
		}
	}
}

func TestIntraCombinedSADSIMDParity(t *testing.T) {
	rng := rand.New(rand.NewSource(902))
	for _, stride := range []int32{17, 31, -17, -31} {
		for iteration := 0; iteration < 500; iteration++ {
			ref, src := make([]byte, 4096), make([]byte, 4096)
			_, _ = rng.Read(ref)
			_, _ = rng.Read(src)
			lambda := int32(iteration % 73)
			if iteration < 3 {
				for i := range ref {
					ref[i], src[i] = 128, 128
				}
				lambda = int32(iteration) // Zero-cost and penalized ties retain V or DC respectively.
			}
			var scratch [256]byte
			var gotMode, wantMode int32
			want := WelsSampleSadIntra16x16Combined3_c(ref, 2048, stride, src, 2048, stride,
				&wantMode, lambda, scratch[:], 0)
			got := intra16CombinedSad(ref, 2048, stride, src, 2048, stride, &gotMode, lambda, nil, 0)
			if got != want || gotMode != wantMode {
				t.Fatalf("luma stride=%d iteration=%d got=(%d,%d) want=(%d,%d)",
					stride, iteration, got, gotMode, want, wantMode)
			}
			want = WelsSampleSadIntra8x8Combined3_c(ref, 2048, stride, src, 2048, stride,
				&wantMode, lambda, scratch[:], 0, ref, 2049, src, 2051)
			got = intra8CombinedSad(ref, 2048, stride, src, 2048, stride,
				&gotMode, lambda, nil, 0, ref, 2049, src, 2051)
			if got != want || gotMode != wantMode {
				t.Fatalf("chroma stride=%d iteration=%d got=(%d,%d) want=(%d,%d)",
					stride, iteration, got, gotMode, want, wantMode)
			}
		}
	}
}

func TestIntraModeDecisionSIMDParity(t *testing.T) {
	var scalar, simd SWelsFuncPtrList
	WelsInitIntraPredFuncs(&scalar, 0)
	WelsInitSampleSadFunc(&scalar, 0)
	SetFastCodingFunc(&scalar)
	WelsInitIntraPredFuncs(&simd, common.WELS_CPU_SSE2)
	WelsInitSampleSadFunc(&simd, common.WELS_CPU_SSE2)
	SetFastCodingFunc(&simd)
	rng := rand.New(rand.NewSource(903))
	layer := SDqLayer{iCsStride: [3]int32{31, 31, 31}, iEncStride: [3]int32{29, 29, 29}}
	for iteration := 0; iteration < 200; iteration++ {
		var pic SMbCachePicData
		for plane := range pic.pCsMb {
			pic.pCsMb[plane], pic.pEncMb[plane] = make([]byte, 1024), make([]byte, 1024)
			_, _ = rng.Read(pic.pCsMb[plane])
			_, _ = rng.Read(pic.pEncMb[plane])
			if iteration < 2 {
				for i := range pic.pCsMb[plane] {
					pic.pCsMb[plane][i], pic.pEncMb[plane][i] = 128, 128
				}
			}
			pic.iCsMbOff[plane], pic.iEncMbOff[plane] = 32, 17
		}
		for available := uint8(0); available < 8; available++ {
			want := SMbCache{SPicData: pic, uiNeighborIntra: available, pMemPredMb: make([]byte, 512)}
			got := SMbCache{SPicData: pic, uiNeighborIntra: available, pMemPredMb: make([]byte, 512)}
			lambda := int32(iteration % 83)
			wantCost := WelsMdI16x16(&scalar, &layer, &want, lambda)
			gotCost := WelsMdI16x16(&simd, &layer, &got, lambda)
			if wantCost != gotCost || want.uiLumaI16x16Mode != got.uiLumaI16x16Mode ||
				!bytes.Equal(want.pMemPredLuma[:256], got.pMemPredLuma[:256]) {
				t.Fatalf("luma iteration=%d available=%d cost=%d/%d mode=%d/%d", iteration, available,
					wantCost, gotCost, want.uiLumaI16x16Mode, got.uiLumaI16x16Mode)
			}
			wantCost = WelsMdIntraChroma(&scalar, &layer, &want, lambda)
			gotCost = WelsMdIntraChroma(&simd, &layer, &got, lambda)
			if wantCost != gotCost || want.uiChmaI8x8Mode != got.uiChmaI8x8Mode ||
				!bytes.Equal(want.pBestPredIntraChroma[:128], got.pBestPredIntraChroma[:128]) {
				t.Fatalf("chroma iteration=%d available=%d cost=%d/%d mode=%d/%d", iteration, available,
					wantCost, gotCost, want.uiChmaI8x8Mode, got.uiChmaI8x8Mode)
			}
		}
	}
}

func TestIntraSIMDRejectsShortBuffers(t *testing.T) {
	for i, call := range []func(){
		func() { intra16Horizontal(make([]byte, 255), 0, make([]byte, 512), 33, 32) },
		func() { intra16Horizontal(make([]byte, 256), 0, make([]byte, 511), 33, 32) },
		func() { intra16Plane(make([]byte, 255), 0, make([]byte, 512), 33, 31) },
		func() { intra16DC128(make([]byte, 255), 0, nil, 0, 0) },
		func() { intra8DC(make([]byte, 63), 0, make([]byte, 512), 33, 32) },
		func() { intra8Horizontal(make([]byte, 64), 0, make([]byte, 255), 33, 32) },
		func() { intra8Plane(make([]byte, 64), 0, make([]byte, 255), 33, 32) },
		func() {
			var mode int32
			intra16CombinedSad(make([]byte, 512), 33, 31, make([]byte, 255), 0, 16, &mode, 0, nil, 0)
		},
		func() { intra8SadCosts(make([]byte, 512), 33, 31, make([]byte, 63), 0, 8) },
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("accepted short buffer")
				}
			}()
			call()
		})
	}
}

func TestIntraPlaneCoefficientsParity(t *testing.T) {
	rng := rand.New(rand.NewSource(816))
	for test := 0; test < 50000; test++ {
		size := []int{8, 16}[test%2]
		stride := size + 1 + rng.Intn(40)
		off := stride + 1 + rng.Intn(8)
		ref := make([]byte, off+(size-1)*stride+size)
		for i := range ref {
			ref[i] = byte(rng.Uint32())
		}
		b0, h0, v0 := intraPlaneCoefficients(ref, off, int32(stride), size)
		b1, h1, v1 := intraPlaneCoefficientsChecked(ref, off, int32(stride), size)
		if b0 != b1 || h0 != h1 || v0 != v1 {
			t.Fatalf("size %d stride %d: %d/%d/%d, want %d/%d/%d", size, stride, b0, h0, v0, b1, h1, v1)
		}
	}
}
