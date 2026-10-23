package common

// Port of codec/common/src/expand_pic.cpp (C code only).
//
// pDst/iDstOff points at the top-left sample of a picture plane; the slice
// must be the whole plane allocation including the padding area.

import "encoding/binary"

// memsetU8 is memset(p + off, v, n). Padding widths are multiples of eight,
// and Go lowers only zero-valued byte loops to memclr, so store whole words.
func memsetU8(p []uint8, off int, v uint8, n int) {
	s := p[off : off+n]
	word := uint64(v) * 0x0101010101010101
	for len(s) >= 8 {
		binary.LittleEndian.PutUint64(s, word)
		s = s[8:]
	}
	for i := range s {
		s[i] = v
	}
}

func MBPadTopLeftLuma_c(pDst []uint8, iDstOff int, kiStride int32) {
	kuiTL := pDst[iDstOff]
	i := 0
	pTopLeft := iDstOff
	for {
		pTopLeft -= int(kiStride)
		// pad pTop
		copy(pDst[pTopLeft:pTopLeft+16], pDst[iDstOff:iDstOff+16])
		memsetU8(pDst, pTopLeft-PADDING_LENGTH, kuiTL, PADDING_LENGTH) //pTop left
		i++
		if i >= PADDING_LENGTH {
			break
		}
	}
}

func MBPadTopLuma_c(pDst []uint8, iDstOff int, kiStride int32, kiMbX int32) {
	pTopLine := iDstOff + int(kiMbX<<4)
	i := 0
	pTop := pTopLine
	for {
		pTop -= int(kiStride)
		// pad pTop
		copy(pDst[pTop:pTop+16], pDst[pTopLine:pTopLine+16])
		i++
		if i >= PADDING_LENGTH {
			break
		}
	}
}

func MBPadBottomLuma_c(pDst []uint8, iDstOff int, kiStride int32, kiMbX int32, kiPicH int32) {
	pBottomLine := iDstOff + int((kiPicH-1)*kiStride) + int(kiMbX<<4)
	i := 0
	pBottom := pBottomLine
	for {
		pBottom += int(kiStride)
		// pad pBottom
		copy(pDst[pBottom:pBottom+16], pDst[pBottomLine:pBottomLine+16])
		i++
		if i >= PADDING_LENGTH {
			break
		}
	}
}

func MBPadTopRightLuma_c(pDst []uint8, iDstOff int, kiStride int32, kiPicW int32) {
	pTopRight := iDstOff + int(kiPicW)
	kuiTR := pDst[pTopRight-1]
	i := 0
	pTop := pTopRight
	for {
		pTop -= int(kiStride)
		// pad pTop
		copy(pDst[pTop-16:pTop], pDst[pTopRight-16:pTopRight])
		memsetU8(pDst, pTop, kuiTR, PADDING_LENGTH) //pTop Right
		i++
		if i >= PADDING_LENGTH {
			break
		}
	}
}

func MBPadBottomLeftLuma_c(pDst []uint8, iDstOff int, kiStride int32, kiPicH int32) {
	pDstLastLine := iDstOff + int((kiPicH-1)*kiStride)
	kuiBL := pDst[pDstLastLine]
	i := 0
	pBottom := pDstLastLine
	for {
		pBottom += int(kiStride)
		// pad pBottom
		copy(pDst[pBottom:pBottom+16], pDst[pDstLastLine:pDstLastLine+16])
		memsetU8(pDst, pBottom-PADDING_LENGTH, kuiBL, PADDING_LENGTH) //pBottom left
		i++
		if i >= PADDING_LENGTH {
			break
		}
	}
}

func MBPadBottomRightLuma_c(pDst []uint8, iDstOff int, kiStride int32, kiPicW int32, kiPicH int32) {
	pDstLastLine := iDstOff + int((kiPicH-1)*kiStride) + int(kiPicW)
	kuiBR := pDst[pDstLastLine-1]
	i := 0
	pBottom := pDstLastLine
	for {
		pBottom += int(kiStride)
		// pad pBottom
		copy(pDst[pBottom-16:pBottom], pDst[pDstLastLine-16:pDstLastLine])
		memsetU8(pDst, pBottom, kuiBR, PADDING_LENGTH) //pBottom Right
		i++
		if i >= PADDING_LENGTH {
			break
		}
	}
}

func MBPadLeftLuma_c(pDst []uint8, iDstOff int, kiStride int32, kiMbY int32) {
	pTmp := iDstOff + int((kiMbY<<4)*kiStride)
	for i := 0; i < 16; i++ {
		// pad left
		memsetU8(pDst, pTmp-PADDING_LENGTH, pDst[pTmp], PADDING_LENGTH)
		pTmp += int(kiStride)
	}
}

func MBPadRightLuma_c(pDst []uint8, iDstOff int, kiStride int32, kiMbY int32, kiPicW int32) {
	pTmp := iDstOff + int((kiMbY<<4)*kiStride) + int(kiPicW)
	for i := 0; i < 16; i++ {
		// pad right
		memsetU8(pDst, pTmp, pDst[pTmp-1], PADDING_LENGTH)
		pTmp += int(kiStride)
	}
}

func MBPadTopChroma_c(pDst []uint8, iDstOff int, kiStride int32, kiMbX int32) {
	pTopLine := iDstOff + int(kiMbX<<3)
	i := 0
	pTop := pTopLine
	for {
		pTop -= int(kiStride)
		// pad pTop
		copy(pDst[pTop:pTop+8], pDst[pTopLine:pTopLine+8])
		i++
		if i >= CHROMA_PADDING_LENGTH {
			break
		}
	}
}

func MBPadBottomChroma_c(pDst []uint8, iDstOff int, kiStride int32, kiMbX int32, kiPicH int32) {
	pBottomLine := iDstOff + int((kiPicH-1)*kiStride) + int(kiMbX<<3)
	i := 0
	pBottom := pBottomLine
	for {
		pBottom += int(kiStride)
		// pad pBottom
		copy(pDst[pBottom:pBottom+8], pDst[pBottomLine:pBottomLine+8])
		i++
		if i >= CHROMA_PADDING_LENGTH {
			break
		}
	}
}

func MBPadTopLeftChroma_c(pDst []uint8, iDstOff int, kiStride int32) {
	kuiTL := pDst[iDstOff]
	i := 0
	pTopLeft := iDstOff
	for {
		pTopLeft -= int(kiStride)
		// pad pTop
		copy(pDst[pTopLeft:pTopLeft+8], pDst[iDstOff:iDstOff+8])
		memsetU8(pDst, pTopLeft-CHROMA_PADDING_LENGTH, kuiTL, CHROMA_PADDING_LENGTH) //pTop left
		i++
		if i >= CHROMA_PADDING_LENGTH {
			break
		}
	}
}

func MBPadTopRightChroma_c(pDst []uint8, iDstOff int, kiStride int32, kiPicW int32) {
	pTopRight := iDstOff + int(kiPicW)
	kuiTR := pDst[pTopRight-1]
	i := 0
	pTop := pTopRight
	for {
		pTop -= int(kiStride)
		// pad pTop
		copy(pDst[pTop-8:pTop], pDst[pTopRight-8:pTopRight])
		memsetU8(pDst, pTop, kuiTR, CHROMA_PADDING_LENGTH) //pTop Right
		i++
		if i >= CHROMA_PADDING_LENGTH {
			break
		}
	}
}

func MBPadBottomLeftChroma_c(pDst []uint8, iDstOff int, kiStride int32, kiPicH int32) {
	pDstLastLine := iDstOff + int((kiPicH-1)*kiStride)
	kuiBL := pDst[pDstLastLine]
	i := 0
	pBottom := pDstLastLine
	for {
		pBottom += int(kiStride)
		// pad pBottom
		copy(pDst[pBottom:pBottom+8], pDst[pDstLastLine:pDstLastLine+8])
		memsetU8(pDst, pBottom-CHROMA_PADDING_LENGTH, kuiBL, CHROMA_PADDING_LENGTH) //pBottom left
		i++
		if i >= CHROMA_PADDING_LENGTH {
			break
		}
	}
}

func MBPadBottomRightChroma_c(pDst []uint8, iDstOff int, kiStride int32, kiPicW int32, kiPicH int32) {
	pDstLastLine := iDstOff + int((kiPicH-1)*kiStride) + int(kiPicW)
	kuiBR := pDst[pDstLastLine-1]
	i := 0
	pBottom := pDstLastLine
	for {
		pBottom += int(kiStride)
		// pad pBottom
		copy(pDst[pBottom-8:pBottom], pDst[pDstLastLine-8:pDstLastLine])
		memsetU8(pDst, pBottom, kuiBR, CHROMA_PADDING_LENGTH) //pBottom Right
		i++
		if i >= CHROMA_PADDING_LENGTH {
			break
		}
	}
}

func MBPadLeftChroma_c(pDst []uint8, iDstOff int, kiStride int32, kiMbY int32) {
	pTmp := iDstOff + int((kiMbY<<3)*kiStride)
	for i := 0; i < 8; i++ {
		// pad left
		memsetU8(pDst, pTmp-CHROMA_PADDING_LENGTH, pDst[pTmp], CHROMA_PADDING_LENGTH)
		pTmp += int(kiStride)
	}
}

func MBPadRightChroma_c(pDst []uint8, iDstOff int, kiStride int32, kiMbY int32, kiPicW int32) {
	pTmp := iDstOff + int((kiMbY<<3)*kiStride) + int(kiPicW)
	for i := 0; i < 8; i++ {
		// pad right
		memsetU8(pDst, pTmp, pDst[pTmp-1], CHROMA_PADDING_LENGTH)
		pTmp += int(kiStride)
	}
}

// PadMBLuma_c pads the picture border around luma macroblock (kiMbX, kiMbY).
func PadMBLuma_c(pDst []uint8, iDstOff int, kiStride int32, kiPicW int32, kiPicH int32,
	kiMbX int32, kiMbY int32, kiMBWidth int32, kiMBHeight int32) {
	if kiMbX == 0 && kiMbY == 0 {
		MBPadTopLeftLuma_c(pDst, iDstOff, kiStride)
	} else if kiMbY == 0 && kiMbX == kiMBWidth-1 {
		MBPadTopRightLuma_c(pDst, iDstOff, kiStride, kiPicW)
	} else if kiMbY == kiMBHeight-1 && kiMbX == 0 {
		MBPadBottomLeftLuma_c(pDst, iDstOff, kiStride, kiPicH)
	} else if kiMbY == kiMBHeight-1 && kiMbX == kiMBWidth-1 {
		MBPadBottomRightLuma_c(pDst, iDstOff, kiStride, kiPicW, kiPicH)
	}
	if kiMbX == 0 {
		MBPadLeftLuma_c(pDst, iDstOff, kiStride, kiMbY)
	} else if kiMbX == kiMBWidth-1 {
		MBPadRightLuma_c(pDst, iDstOff, kiStride, kiMbY, kiPicW)
	}
	if kiMbY == 0 && kiMbX > 0 && kiMbX < kiMBWidth-1 {
		MBPadTopLuma_c(pDst, iDstOff, kiStride, kiMbX)
	} else if kiMbY == kiMBHeight-1 && kiMbX > 0 && kiMbX < kiMBWidth-1 {
		MBPadBottomLuma_c(pDst, iDstOff, kiStride, kiMbX, kiPicH)
	}
}

// PadMBChroma_c pads the picture border around chroma macroblock (kiMbX, kiMbY).
func PadMBChroma_c(pDst []uint8, iDstOff int, kiStride int32, kiPicW int32, kiPicH int32,
	kiMbX int32, kiMbY int32, kiMBWidth int32, kiMBHeight int32) {
	if kiMbX == 0 && kiMbY == 0 {
		MBPadTopLeftChroma_c(pDst, iDstOff, kiStride)
	} else if kiMbY == 0 && kiMbX == kiMBWidth-1 {
		MBPadTopRightChroma_c(pDst, iDstOff, kiStride, kiPicW)
	} else if kiMbY == kiMBHeight-1 && kiMbX == 0 {
		MBPadBottomLeftChroma_c(pDst, iDstOff, kiStride, kiPicH)
	} else if kiMbY == kiMBHeight-1 && kiMbX == kiMBWidth-1 {
		MBPadBottomRightChroma_c(pDst, iDstOff, kiStride, kiPicW, kiPicH)
	}
	if kiMbX == 0 {
		MBPadLeftChroma_c(pDst, iDstOff, kiStride, kiMbY)
	} else if kiMbX == kiMBWidth-1 {
		MBPadRightChroma_c(pDst, iDstOff, kiStride, kiMbY, kiPicW)
	}
	if kiMbY == 0 && kiMbX > 0 && kiMbX < kiMBWidth-1 {
		MBPadTopChroma_c(pDst, iDstOff, kiStride, kiMbX)
	} else if kiMbY == kiMBHeight-1 && kiMbX > 0 && kiMbX < kiMBWidth-1 {
		MBPadBottomChroma_c(pDst, iDstOff, kiStride, kiMbX, kiPicH)
	}
}

// expandPicture is the shared body of ExpandPictureLuma_c and
// ExpandPictureChroma_c (they only differ in the padding length).
func expandPicture(pDst []uint8, iDstOff int, kiStride int32, kiPicW int32, kiPicH int32, kiPaddingLen int) {
	pTmp := iDstOff
	pDstLastLine := pTmp + int((kiPicH-1)*kiStride)
	w := int(kiPicW)
	kuiTL := pDst[pTmp]
	kuiTR := pDst[pTmp+w-1]
	kuiBL := pDst[pDstLastLine]
	kuiBR := pDst[pDstLastLine+w-1]
	i := 0

	for {
		kiStrides := (1 + i) * int(kiStride)
		pTop := pTmp - kiStrides
		pBottom := pDstLastLine + kiStrides

		// pad pTop and pBottom
		copy(pDst[pTop:pTop+w], pDst[pTmp:pTmp+w])
		copy(pDst[pBottom:pBottom+w], pDst[pDstLastLine:pDstLastLine+w])

		// pad corners
		memsetU8(pDst, pTop-kiPaddingLen, kuiTL, kiPaddingLen)    //pTop left
		memsetU8(pDst, pTop+w, kuiTR, kiPaddingLen)               //pTop right
		memsetU8(pDst, pBottom-kiPaddingLen, kuiBL, kiPaddingLen) //pBottom left
		memsetU8(pDst, pBottom+w, kuiBR, kiPaddingLen)            //pBottom right

		i++
		if i >= kiPaddingLen {
			break
		}
	}

	// pad left and right
	i = 0
	for {
		memsetU8(pDst, pTmp-kiPaddingLen, pDst[pTmp], kiPaddingLen)
		memsetU8(pDst, pTmp+w, pDst[pTmp+w-1], kiPaddingLen)

		pTmp += int(kiStride)
		i++
		if i >= int(kiPicH) {
			break
		}
	}
}

// ExpandPictureLuma_c pads a luma plane by PADDING_LENGTH on every side.
func ExpandPictureLuma_c(pDst []uint8, iDstOff int, kiStride int32, kiPicW int32, kiPicH int32) {
	expandPicture(pDst, iDstOff, kiStride, kiPicW, kiPicH, PADDING_LENGTH)
}

// ExpandPictureChroma_c pads a chroma plane by PADDING_LENGTH/2 on every side.
func ExpandPictureChroma_c(pDst []uint8, iDstOff int, kiStride int32, kiPicW int32, kiPicH int32) {
	expandPicture(pDst, iDstOff, kiStride, kiPicW, kiPicH, PADDING_LENGTH>>1)
}

// InitExpandPictureFunc fills pExpandPicFunc with the C implementations
// (kuiCPUFlag is ignored).
func InitExpandPictureFunc(pExpandPicFunc *SExpandPicFunc, kuiCPUFlag uint32) {
	pExpandPicFunc.PfExpandLumaPicture = ExpandPictureLuma_c
	pExpandPicFunc.PfExpandChromaPicture[0] = ExpandPictureChroma_c
	pExpandPicFunc.PfExpandChromaPicture[1] = ExpandPictureChroma_c
}

// ExpandReferencingPicture pads the three planes of a picture.
//
// pData[i] holds the plane buffer and iDataOff[i] the offset of the plane
// origin inside it (PORTING.md picture plane convention); iStride holds the
// line sizes. Pass e.g. pPic.PData[:], pPic.IDataOff[:], pPic.ILinesize[:]
// (only the first three entries are used).
func ExpandReferencingPicture(pData [][]uint8, iDataOff []int, iWidth int32, iHeight int32, iStride []int32,
	pExpLuma PExpandPictureFunc, pExpChrom [2]PExpandPictureFunc) {
	/*local variable*/
	pPicY, pPicCb, pPicCr := pData[0], pData[1], pData[2]
	kiWidthY := iWidth
	kiHeightY := iHeight
	kiWidthUV := kiWidthY >> 1
	kiHeightUV := kiHeightY >> 1

	pExpLuma(pPicY, iDataOff[0], iStride[0], kiWidthY, kiHeightY)
	if kiWidthUV >= 16 {
		// fix coding picture size as 16x16
		kbChrAligned := 0 // chroma planes: (16+iWidthUV) & 15
		if (kiWidthUV & 0x0F) == 0 {
			kbChrAligned = 1
		}
		pExpChrom[kbChrAligned](pPicCb, iDataOff[1], iStride[1], kiWidthUV, kiHeightUV)
		pExpChrom[kbChrAligned](pPicCr, iDataOff[2], iStride[2], kiWidthUV, kiHeightUV)
	} else {
		// fix coding picture size as 16x16
		ExpandPictureChroma_c(pPicCb, iDataOff[1], iStride[1], kiWidthUV, kiHeightUV)
		ExpandPictureChroma_c(pPicCr, iDataOff[2], iStride[2], kiWidthUV, kiHeightUV)
	}
}
