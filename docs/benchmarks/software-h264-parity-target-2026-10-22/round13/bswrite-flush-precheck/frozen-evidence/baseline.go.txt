package common

// Port of codec/common/inc/golomb_common.h (exponential golomb writing).

// WRITE_BE_32 writes val big endian to ptr[off:off+4].
func WRITE_BE_32(ptr []uint8, off int, val uint32) {
	ptr[off+0] = uint8(val >> 24)
	ptr[off+1] = uint8(val >> 16)
	ptr[off+2] = uint8(val >> 8)
	ptr[off+3] = uint8(val >> 0)
}

// InitBits initializes bitstream writing on kpBuf[kiOff:kiOff+kiSize].
// PBuf is set to kpBuf (the whole buffer) and the position fields to offsets
// into it.
//
// Returns the size of the buffer in bytes.
func InitBits(pBs *SBitStringAux, kpBuf []uint8, kiOff int, kiSize int32) int32 {
	pBs.PBuf = kpBuf
	pBs.PStartBuf = kiOff
	pBs.PCurBuf = kiOff
	pBs.PEndBuf = kiOff + int(kiSize)
	pBs.ILeftBits = 32
	pBs.UiCurBits = 0

	return kiSize
}

// BsWriteBits writes the iLen low bits of kuiValue.
func BsWriteBits(pBitString PBitStringAux, iLen int32, kuiValue uint32) int32 {
	if iLen < pBitString.ILeftBits {
		pBitString.UiCurBits = (pBitString.UiCurBits << uint32(iLen)) | kuiValue
		pBitString.ILeftBits -= iLen
	} else {
		iLen -= pBitString.ILeftBits
		pBitString.UiCurBits = (pBitString.UiCurBits << uint32(pBitString.ILeftBits)) | (kuiValue >> uint32(iLen))
		WRITE_BE_32(pBitString.PBuf, pBitString.PCurBuf, pBitString.UiCurBits)
		pBitString.PCurBuf += 4
		pBitString.UiCurBits = kuiValue & ((uint32(1) << uint32(iLen)) - 1)
		pBitString.ILeftBits = 32 - iLen
	}
	return 0
}

// BsWriteOneBit writes 1 bit.
func BsWriteOneBit(pBitString PBitStringAux, kuiValue uint32) int32 {
	BsWriteBits(pBitString, 1, kuiValue)
	return 0
}

// BsFlush flushes the pending bits to the buffer.
func BsFlush(pBitString PBitStringAux) int32 {
	if pBitString.ILeftBits < 32 {
		WRITE_BE_32(pBitString.PBuf, pBitString.PCurBuf, pBitString.UiCurBits<<uint32(pBitString.ILeftBits))
		pBitString.PCurBuf += int(4 - pBitString.ILeftBits/8)
		pBitString.ILeftBits = 32
		pBitString.UiCurBits = 0
	}
	return 0
}

// BsWriteUE writes an unsigned exp golomb code.
func BsWriteUE(pBitString PBitStringAux, kuiValue uint32) int32 {
	iTmpValue := kuiValue + 1
	if 256 > kuiValue {
		BsWriteBits(pBitString, int32(G_kuiGolombUELength[kuiValue]), kuiValue+1)
	} else {
		var n uint32
		if iTmpValue&0xffff0000 != 0 {
			iTmpValue >>= 16
			n += 16
		}
		if iTmpValue&0xff00 != 0 {
			iTmpValue >>= 8
			n += 8
		}

		//n += (g_kuiGolombUELength[iTmpValue] >> 1);

		n += G_kuiGolombUELength[iTmpValue-1] >> 1
		BsWriteBits(pBitString, int32((n<<1)+1), kuiValue+1)
	}
	return 0
}

// BsWriteSE writes a signed exp golomb code.
func BsWriteSE(pBitString PBitStringAux, kiValue int32) int32 {
	var iTmpValue uint32
	if 0 == kiValue {
		BsWriteOneBit(pBitString, 1)
	} else if 0 < kiValue {
		iTmpValue = uint32((kiValue << 1) - 1)
		BsWriteUE(pBitString, iTmpValue)
	} else {
		iTmpValue = uint32((-kiValue) << 1)
		BsWriteUE(pBitString, iTmpValue)
	}
	return 0
}

// BsRbspTrailingBits writes the RBSP trailing bits.
func BsRbspTrailingBits(pBitString PBitStringAux) int32 {
	BsWriteOneBit(pBitString, 1)
	BsFlush(pBitString)

	return 0
}
