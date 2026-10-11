// Port of test/encoder/EncUT_MotionCompensation.cpp (C paths only). The MC
// functions live in package common; these tests check the function table
// the encoder uses (common.InitMcFunc with cpu flags 0) against the anchors
// of the C test.

package encoder

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const (
	mcBuffSrcStride = 32
	mcBuffDstStride = 32
	mcBuffHeight    = 30
)

var mcBQpelNeeded = [4][4]bool{
	{false, true, false, true},
	{true, true, true, true},
	{false, true, false, true},
	{true, true, true, true},
}
var mcIHpelRef0Array = [4][4]int{
	{0, 1, 1, 1},
	{0, 1, 1, 1},
	{2, 3, 3, 3},
	{0, 1, 1, 1},
}
var mcIHpelRef1Array = [4][4]int{
	{0, 0, 0, 0},
	{2, 2, 3, 2},
	{2, 2, 3, 2},
	{2, 2, 3, 2},
}

func mcFilter6Tap(p []int32, x int, s int) int32 {
	return p[x-2*s] + p[x+3*s] - 5*(p[x-s]+p[x+2*s]) + 20*(p[x]+p[x+s])
}

func mcClip255(x int32) uint8 {
	if x&^255 != 0 {
		return uint8((-x) >> 31 & 255)
	}
	return uint8(x)
}

func mcToI32(p []uint8) []int32 {
	o := make([]int32, len(p))
	for i, v := range p {
		o[i] = int32(v)
	}
	return o
}

// MCHalfPelFilterAnchor: dst planes and src are (slice, offset) with the same stride.
func mcHalfPelFilterAnchor(pDstH, pDstV, pDstHV []uint8, off int, pSrc []uint8, iStride, iWidth, iHeight int) {
	src := mcToI32(pSrc)
	pBuf := make([]int32, iWidth+5)
	for y := 0; y < iHeight; y++ {
		row := off + y*iStride
		for x := 0; x < iWidth; x++ {
			pDstH[row+x] = mcClip255((mcFilter6Tap(src, row+x, 1) + 16) >> 5)
		}
		for x := -2; x < iWidth+3; x++ {
			v := mcFilter6Tap(src, row+x, iStride)
			if x >= 0 && x < iWidth {
				pDstV[row+x] = mcClip255((v + 16) >> 5)
			}
			pBuf[x+2] = v
		}
		for x := 0; x < iWidth; x++ {
			pDstHV[row+x] = mcClip255((mcFilter6Tap(pBuf, x+2, 1) + 512) >> 10)
		}
	}
}

func mcPixelAvgAnchor(pDst []uint8, dOff, iDstStride int, pSrc1 []uint8, s1Off, iSrc1Stride int, pSrc2 []uint8, s2Off, iSrc2Stride int, iWidth, iHeight int) {
	for y := 0; y < iHeight; y++ {
		for x := 0; x < iWidth; x++ {
			pDst[dOff+x] = uint8((int32(pSrc1[s1Off+x]) + int32(pSrc2[s2Off+x]) + 1) >> 1)
		}
		dOff += iDstStride
		s1Off += iSrc1Stride
		s2Off += iSrc2Stride
	}
}

func mcCopyAnchor(pSrc []uint8, sOff, iSrcStride int, pDst []uint8, dOff, iDstStride int, iWidth, iHeight int) {
	for y := 0; y < iHeight; y++ {
		copy(pDst[dOff:dOff+iWidth], pSrc[sOff:sOff+iWidth])
		sOff += iSrcStride
		dOff += iDstStride
	}
}

func mcLumaAnchor(pDst []uint8, iDstStride int, pSrc [4][]uint8, srcOff int, iSrcStride int, iMvX, iMvY int, iWidth, iHeight int) {
	iMvXIdx := iMvX & 3
	iMvYIdx := iMvY & 3
	iOffset := (iMvY>>2)*iSrcStride + (iMvX >> 2)
	o1 := srcOff + iOffset
	if iMvYIdx == 3 {
		o1 += iSrcStride
	}
	pSrc1 := pSrc[mcIHpelRef0Array[iMvYIdx][iMvXIdx]]
	if mcBQpelNeeded[iMvYIdx][iMvXIdx] {
		o2 := srcOff + iOffset
		if iMvXIdx == 3 {
			o2++
		}
		pSrc2 := pSrc[mcIHpelRef1Array[iMvYIdx][iMvXIdx]]
		mcPixelAvgAnchor(pDst, 0, iDstStride, pSrc1, o1, iSrcStride, pSrc2, o2, iSrcStride, iWidth, iHeight)
	} else {
		mcCopyAnchor(pSrc1, o1, iSrcStride, pDst, 0, iDstStride, iWidth, iHeight)
	}
}

func TestMcCopy_c(t *testing.T) {
	r := rand.New(rand.NewSource(40))
	sizes := [][2]int{{2, 2}, {2, 4}, {4, 2}, {4, 4}, {4, 8}, {8, 4}, {8, 8}, {16, 8}, {8, 16}, {16, 16}}
	var sMcFunc common.SMcFunc
	common.InitMcFunc(&sMcFunc, 0)
	for _, sz := range sizes {
		iW, iH := sz[0], sz[1]
		src := make([]uint8, mcBuffHeight*mcBuffSrcStride)
		for i := range src {
			src[i] = uint8(r.Intn(256))
		}
		anchor := make([]uint8, mcBuffHeight*mcBuffDstStride)
		test := make([]uint8, mcBuffHeight*mcBuffDstStride)
		mcCopyAnchor(src, 0, mcBuffSrcStride, anchor, 0, mcBuffDstStride, iW, iH)
		sMcFunc.PMcLumaFunc(src, 0, mcBuffSrcStride, test, 0, mcBuffDstStride, 0, 0, int32(iW), int32(iH))
		for i := range anchor {
			if anchor[i] != test[i] {
				t.Fatalf("McCopy %dx%d: [%d] %d != %d", iW, iH, i, test[i], anchor[i])
			}
		}
	}
}

func TestMcHorVer_c(t *testing.T) {
	r := rand.New(rand.NewSource(41))
	sizes := [][2]int{{4, 4}, {4, 8}, {8, 4}, {8, 8}, {16, 8}, {8, 16}, {16, 16}}
	var sMcFunc common.SMcFunc
	common.InitMcFunc(&sMcFunc, 0)
	const org = 4*mcBuffSrcStride + 4
	for _, sz := range sizes {
		iW, iH := sz[0], sz[1]
		for a := 0; a < 4; a++ {
			for b := 0; b < 4; b++ {
				var anchorPlanes [4][]uint8
				for k := range anchorPlanes {
					anchorPlanes[k] = make([]uint8, mcBuffHeight*mcBuffSrcStride)
				}
				srcTest := make([]uint8, mcBuffHeight*mcBuffSrcStride)
				for i := range srcTest {
					v := uint8(r.Intn(256))
					anchorPlanes[0][i] = v
					srcTest[i] = v
				}
				dstAnchor := make([]uint8, mcBuffHeight*mcBuffDstStride)
				dstTest := make([]uint8, mcBuffHeight*mcBuffDstStride)
				mcHalfPelFilterAnchor(anchorPlanes[1], anchorPlanes[2], anchorPlanes[3], org, anchorPlanes[0], mcBuffSrcStride, iW+1, iH+1)
				mcLumaAnchor(dstAnchor, mcBuffDstStride, anchorPlanes, org, mcBuffSrcStride, a, b, iW, iH)
				sMcFunc.PMcLumaFunc(srcTest, org, mcBuffSrcStride, dstTest, 0, mcBuffDstStride, int16(a), int16(b), int32(iW), int32(iH))
				for i := range dstAnchor {
					if dstAnchor[i] != dstTest[i] {
						t.Fatalf("McHorVer %dx%d mv(%d,%d): [%d] %d != %d", iW, iH, a, b, i, dstTest[i], dstAnchor[i])
					}
				}
			}
		}
	}
}

func TestMcChroma_c(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	sizes := [][2]int{{2, 2}, {2, 4}, {4, 2}, {4, 4}, {4, 8}, {8, 4}, {8, 8}}
	var sMcFunc common.SMcFunc
	common.InitMcFunc(&sMcFunc, 0)
	for _, sz := range sizes {
		iW, iH := sz[0], sz[1]
		for a := 0; a < 8; a++ {
			for b := 0; b < 8; b++ {
				src := make([]uint8, mcBuffHeight*mcBuffSrcStride)
				for i := range src {
					src[i] = uint8(r.Intn(256))
				}
				// anchor (MCChromaAnchor on the de-interleaved plane)
				iBiPara0 := int32((8 - a) * (8 - b))
				iBiPara1 := int32(a * (8 - b))
				iBiPara2 := int32((8 - a) * b)
				iBiPara3 := int32(a * b)
				anchor := make([]uint8, mcBuffHeight*mcBuffDstStride)
				for y := 0; y < iH; y++ {
					for x := 0; x < iW; x++ {
						p := y*mcBuffSrcStride + x
						anchor[y*mcBuffDstStride+x] = uint8((iBiPara0*int32(src[p]) + iBiPara1*int32(src[p+1]) +
							iBiPara2*int32(src[p+mcBuffSrcStride]) + iBiPara3*int32(src[p+mcBuffSrcStride+1]) + 32) >> 6)
					}
				}
				test := make([]uint8, mcBuffHeight*mcBuffDstStride)
				sMcFunc.PMcChromaFunc(src, 0, mcBuffSrcStride, test, 0, mcBuffDstStride, int16(a), int16(b), int32(iW), int32(iH))
				for i := range anchor {
					if anchor[i] != test[i] {
						t.Fatalf("McChroma %dx%d mv(%d,%d): [%d] %d != %d", iW, iH, a, b, i, test[i], anchor[i])
					}
				}
			}
		}
	}
}

func TestEncMcAvg_PixelAvg(t *testing.T) {
	r := rand.New(rand.NewSource(43))
	var sMcFunc common.SMcFunc
	common.InitMcFunc(&sMcFunc, 0)
	for w := 0; w < 2; w++ {
		width := 8 << w
		height := 16
		src1 := make([]uint8, mcBuffHeight*mcBuffSrcStride)
		src2 := make([]uint8, mcBuffHeight*mcBuffSrcStride)
		for i := range src1 {
			src1[i] = uint8(r.Intn(256))
			src2[i] = uint8(r.Intn(256))
		}
		anchor := make([]uint8, mcBuffHeight*mcBuffDstStride)
		test := make([]uint8, mcBuffHeight*mcBuffDstStride)
		mcPixelAvgAnchor(anchor, 0, mcBuffDstStride, src1, 0, mcBuffSrcStride, src2, 0, mcBuffSrcStride, width, height)
		sMcFunc.PfSampleAveraging(test, 0, mcBuffDstStride, src1, 0, mcBuffSrcStride, src2, 0, mcBuffSrcStride, int32(width), int32(height))
		for j := 0; j < height; j++ {
			for i := 0; i < width; i++ {
				if anchor[j*mcBuffDstStride+i] != test[j*mcBuffDstStride+i] {
					t.Fatalf("PixelAvg w=%d (%d,%d)", width, i, j)
				}
			}
		}
	}
}

func TestEncMcHalfpel_c(t *testing.T) {
	r := rand.New(rand.NewSource(44))
	sizes := [][2]int{{4, 4}, {4, 8}, {8, 4}, {8, 8}, {8, 16}, {16, 8}, {16, 16}}
	var sMcFunc common.SMcFunc
	common.InitMcFunc(&sMcFunc, 0)
	const org = 4*mcBuffSrcStride + 4
	for _, sz := range sizes {
		width, height := sz[0], sz[1]
		var anchorPlanes [4][]uint8
		for k := range anchorPlanes {
			anchorPlanes[k] = make([]uint8, mcBuffHeight*mcBuffSrcStride)
		}
		srcTest := make([]uint8, mcBuffHeight*mcBuffSrcStride)
		uRand := make([]uint8, mcBuffHeight*mcBuffDstStride)
		for i := range srcTest {
			v := uint8(r.Intn(256))
			anchorPlanes[0][i] = v
			srcTest[i] = v
			uRand[i] = uint8(r.Intn(256))
		}
		mcHalfPelFilterAnchor(anchorPlanes[1], anchorPlanes[2], anchorPlanes[3], org, anchorPlanes[0], mcBuffSrcStride, width+1, height+1)

		check := func(name string, fn common.PWelsLumaHalfpelMcFunc, plane []uint8, w, h int) {
			dst := make([]uint8, len(uRand))
			copy(dst, uRand)
			fn(srcTest, org, mcBuffSrcStride, dst, 0, mcBuffDstStride, int32(w), int32(h))
			for j := 0; j < mcBuffHeight; j++ {
				for i := 0; i < mcBuffDstStride; i++ {
					want := uRand[j*mcBuffDstStride+i]
					if j < h && i < w {
						want = plane[org+j*mcBuffSrcStride+i]
					}
					if dst[j*mcBuffDstStride+i] != want {
						t.Fatalf("%s %dx%d at (%d,%d): %d != %d", name, width, height, i, j, dst[j*mcBuffDstStride+i], want)
					}
				}
			}
		}
		check("hor", sMcFunc.PfLumaHalfpelHor, anchorPlanes[1], width+1, height)
		check("ver", sMcFunc.PfLumaHalfpelVer, anchorPlanes[2], width, height+1)
		check("cen", sMcFunc.PfLumaHalfpelCen, anchorPlanes[3], width+1, height+1)
	}
}
