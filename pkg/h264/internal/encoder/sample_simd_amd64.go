//go:build amd64

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

//go:noescape
func satd4x4(src []byte, srcStride int, ref []byte, refStride int) int

//go:noescape
func satd8x8(src []byte, srcStride int, ref []byte, refStride int) int

func sampleSIMDBlockSlice(buf []byte, off int, stride int32, width, height int) []byte {
	if stride < 0 {
		panic("h264: SIMD sample buffer span out of range")
	}
	end := off + (height-1)*int(stride) + width
	return buf[off:end]
}

func checkSampleSIMDBlock(buf []byte, off int, stride int32, width, height int) {
	first := int64(off)
	last := first + int64(height-1)*int64(stride)
	if last < first {
		first, last = last, first
	}
	last += int64(width - 1)
	if first < 0 || last < first || last >= int64(len(buf)) {
		panic("h264: SIMD sample buffer span out of range")
	}
}

func WelsSampleSatd4x4_sse41(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	aBlock := sampleSIMDBlockSlice(a, aOff, aStride, 4, 4)
	refBlock := sampleSIMDBlockSlice(ref, refOff, refStride, 4, 4)
	return int32(satd4x4(aBlock, int(aStride), refBlock, int(refStride)))
}

func WelsSampleSatd8x8_sse41(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	aBlock := sampleSIMDBlockSlice(a, aOff, aStride, 8, 8)
	refBlock := sampleSIMDBlockSlice(ref, refOff, refStride, 8, 8)
	return int32(satd8x8(aBlock, int(aStride), refBlock, int(refStride)))
}

// The private assembly entries receive only pointers after every input span
// has been checked against its actual slice length. This avoids marshaling
// unused slice lengths and capacities for every candidate.
//
//go:noescape
func satdBlockAVX2(src *byte, srcStride int, ref *byte, refStride int, width, height int) int

//go:noescape
func satdAvgBlockAVX2(src *byte, srcStride int, a *byte, aStride int, b *byte, bStride int, width, height int) int

func sampleSatdBlockAVX2(a []byte, aOff int, aStride int32, ref []byte, refOff int, refStride int32, width, height int) int32 {
	checkSampleSIMDBlock(a, aOff, aStride, width, height)
	checkSampleSIMDBlock(ref, refOff, refStride, width, height)
	return int32(satdBlockAVX2(&a[aOff], int(aStride), &ref[refOff], int(refStride), width, height))
}

func WelsSampleSatd8x8_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	checkSampleSIMDBlock(a, aOff, aStride, 8, 8)
	checkSampleSIMDBlock(ref, refOff, refStride, 8, 8)
	return int32(satdBlockAVX2(&a[aOff], int(aStride), &ref[refOff], int(refStride), 8, 8))
}

func WelsSampleSatd16x8_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	checkSampleSIMDBlock(a, aOff, aStride, 16, 8)
	checkSampleSIMDBlock(ref, refOff, refStride, 16, 8)
	return int32(satdBlockAVX2(&a[aOff], int(aStride), &ref[refOff], int(refStride), 16, 8))
}

func WelsSampleSatd8x16_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	checkSampleSIMDBlock(a, aOff, aStride, 8, 16)
	checkSampleSIMDBlock(ref, refOff, refStride, 8, 16)
	return int32(satdBlockAVX2(&a[aOff], int(aStride), &ref[refOff], int(refStride), 8, 16))
}

func WelsSampleSatd16x16_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	checkSampleSIMDBlock(a, aOff, aStride, 16, 16)
	checkSampleSIMDBlock(ref, refOff, refStride, 16, 16)
	return int32(satdBlockAVX2(&a[aOff], int(aStride), &ref[refOff], int(refStride), 16, 16))
}

func sampleAvgSatdAVX2(enc []byte, encOff int, encStride int32,
	a []byte, aOff int, aStride int32, b []byte, bOff int, bStride int32,
	width, height int32) int32 {
	if (width != 8 && width != 16) || height <= 0 || height > 16 || height%4 != 0 {
		panic("h264: invalid averaged SATD dimensions")
	}
	checkSampleSIMDBlock(enc, encOff, encStride, int(width), int(height))
	checkSampleSIMDBlock(a, aOff, aStride, int(width), int(height))
	checkSampleSIMDBlock(b, bOff, bStride, int(width), int(height))
	return int32(satdAvgBlockAVX2(&enc[encOff], int(encStride), &a[aOff], int(aStride), &b[bOff], int(bStride), int(width), int(height)))
}

func initSampleAvgSatdSIMD(s *SSampleDealingFunc, flags uint32) {
	s.pfSampleAvgSatd = nil
	s.pfSampleAvgSatdValidated = nil
	s.bSampleAvgSatdFour = false
	if flags&common.WELS_CPU_AVX2 != 0 {
		s.pfSampleAvgSatd = sampleAvgSatdAVX2
		s.pfSampleAvgSatdValidated = satdAvgBlockAVX2
		s.bSampleAvgSatdFour = true
	}
}
