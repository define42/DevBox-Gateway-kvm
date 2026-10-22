//go:build amd64

package common

import "unsafe"

//go:noescape
func copyRows16(dst *byte, dstStride int, src *byte, srcStride int, height int)

//go:noescape
func copyRows8(dst *byte, dstStride int, src *byte, srcStride int, height int)

//go:noescape
func copyRows4(dst *byte, dstStride int, src *byte, srcStride int, height int)

// copyBlockPtrs validates a width×height copy with one span check per buffer
// and returns the first destination and source bytes. It reports false when
// the height is not positive, a stride is negative or a span is out of range;
// the caller then runs the original row loop, which keeps its exact behavior
// for those inputs.
func copyBlockPtrs(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32, width, height int) (*byte, *byte, bool) {
	dstEnd := iDstOff + (height-1)*int(iStrideD) + width
	srcEnd := iSrcOff + (height-1)*int(iStrideS) + width
	if height <= 0 || iStrideD < 0 || iStrideS < 0 || iDstOff < 0 || iSrcOff < 0 || dstEnd > len(pDst) || srcEnd > len(pSrc) {
		return nil, nil, false
	}
	return (*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(pDst)), iDstOff)),
		(*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(pSrc)), iSrcOff)), true
}

func copyBlockFast16(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32, height int) bool {
	dst, src, ok := copyBlockPtrs(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, 16, height)
	if ok {
		copyRows16(dst, int(iStrideD), src, int(iStrideS), height)
	}
	return ok
}

func copyBlockFast8(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32, height int) bool {
	dst, src, ok := copyBlockPtrs(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, 8, height)
	if ok {
		copyRows8(dst, int(iStrideD), src, int(iStrideS), height)
	}
	return ok
}

func copyBlockFast4(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32, height int) bool {
	dst, src, ok := copyBlockPtrs(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, 4, height)
	if ok {
		copyRows4(dst, int(iStrideD), src, int(iStrideS), height)
	}
	return ok
}

// copyBlockFast copies a width×height block (width 4, 8 or 16); see
// copyBlockPtrs for when it declines.
func copyBlockFast(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32, width, height int) bool {
	switch width {
	case 16:
		return copyBlockFast16(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, height)
	case 8:
		return copyBlockFast8(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, height)
	case 4:
		return copyBlockFast4(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, height)
	}
	return false
}
