//go:build amd64

package encoder

import (
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

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
	_ = sampleBlockPtr(buf, off, stride, width, height)
}

// sampleBlockPtr validates a block whose stride may be negative and returns
// its first byte. It inlines into the checked wrappers. Once off is inside
// buf, 64-bit span arithmetic with int32 strides and block sizes cannot wrap.
func sampleBlockPtr(buf []byte, off int, stride int32, width, height int) *byte {
	if uint(off) >= uint(len(buf)) {
		panic("h264: SIMD sample buffer span out of range")
	}
	first, last := off, off+(height-1)*int(stride)
	if stride < 0 {
		first, last = last, first
	}
	if first < 0 || last+width > len(buf) {
		panic("h264: SIMD sample buffer span out of range")
	}
	return (*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(buf)), off))
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
	return int32(satdBlockAVX2(sampleBlockPtr(a, aOff, aStride, width, height), int(aStride),
		sampleBlockPtr(ref, refOff, refStride, width, height), int(refStride), width, height))
}

func WelsSampleSatd8x8_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return sampleSatdBlockAVX2(a, aOff, aStride, ref, refOff, refStride, 8, 8)
}

func WelsSampleSatd16x8_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return sampleSatdBlockAVX2(a, aOff, aStride, ref, refOff, refStride, 16, 8)
}

func WelsSampleSatd8x16_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return sampleSatdBlockAVX2(a, aOff, aStride, ref, refOff, refStride, 8, 16)
}

func WelsSampleSatd16x16_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return sampleSatdBlockAVX2(a, aOff, aStride, ref, refOff, refStride, 16, 16)
}

func sampleAvgSatdAVX2(enc []byte, encOff int, encStride int32,
	a []byte, aOff int, aStride int32, b []byte, bOff int, bStride int32,
	width, height int32) int32 {
	if (width != 8 && width != 16) || height <= 0 || height > 16 || height%4 != 0 {
		panic("h264: invalid averaged SATD dimensions")
	}
	w, h := int(width), int(height)
	return int32(satdAvgBlockAVX2(sampleBlockPtr(enc, encOff, encStride, w, h), int(encStride),
		sampleBlockPtr(a, aOff, aStride, w, h), int(aStride), sampleBlockPtr(b, bOff, bStride, w, h), int(bStride), w, h))
}

func initSampleAvgSatdSIMD(s *SSampleDealingFunc, flags uint32) {
	s.pfSampleAvgSatd = nil
	if flags&common.WELS_CPU_AVX2 != 0 {
		s.pfSampleAvgSatd = sampleAvgSatdAVX2
	}
}
