package common

// Port of codec/common/inc/copy_mb.h and codec/common/src/copy_mb.cpp
// (C code only).
//
// Signature: func(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32)

func welsCopyNxM(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32, n, m int) {
	if copyBlockFast(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, n, m) {
		return
	}
	for i := 0; i < m; i++ {
		copy(pDst[iDstOff:iDstOff+n], pSrc[iSrcOff:iSrcOff+n])
		iDstOff += int(iStrideD)
		iSrcOff += int(iStrideS)
	}
}

func WelsCopy4x4_c(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32) {
	welsCopyNxM(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, 4, 4)
}

func WelsCopy8x4_c(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32) {
	WelsCopy4x4_c(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS)
	WelsCopy4x4_c(pDst, iDstOff+4, iStrideD, pSrc, iSrcOff+4, iStrideS)
}

func WelsCopy4x8_c(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32) {
	WelsCopy4x4_c(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS)
	WelsCopy4x4_c(pDst, iDstOff+int(iStrideD<<2), iStrideD, pSrc, iSrcOff+int(iStrideS<<2), iStrideS)
}

func WelsCopy8x8_c(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32) {
	if copyBlockFast8(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, 8) {
		return
	}
	welsCopyNxM(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, 8, 8)
}

func WelsCopy8x16_c(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32) {
	welsCopyNxM(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, 8, 16)
}

func WelsCopy16x8_c(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32) {
	welsCopyNxM(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, 16, 8)
}

func WelsCopy16x16_c(pDst []uint8, iDstOff int, iStrideD int32, pSrc []uint8, iSrcOff int, iStrideS int32) {
	if copyBlockFast16(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, 16) {
		return
	}
	welsCopyNxM(pDst, iDstOff, iStrideD, pSrc, iSrcOff, iStrideS, 16, 16)
}
