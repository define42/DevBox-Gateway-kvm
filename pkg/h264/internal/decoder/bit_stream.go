// Port of codec/decoder/core/src/bit_stream.cpp.
//
// Reading / writing bit-stream.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// GetValue4Bytes ports inline uint32_t GetValue4Bytes (uint8_t* pDstNal).
//
// pDstNal: sub-slice starting at the C pointer.
func GetValue4Bytes(pDstNal []uint8) uint32 {
	var uiValue uint32
	uiValue = (uint32(pDstNal[0]) << 24) | (uint32(pDstNal[1]) << 16) | (uint32(pDstNal[2]) << 8) | uint32(pDstNal[3])
	return uiValue
}

// GetValue4BytesSafe ports inline uint32_t GetValue4BytesSafe (uint8_t* pDstNal, intX_t
// iAvailableBytes).
//
// pDstNal: sub-slice starting at the C pointer.
func GetValue4BytesSafe(pDstNal []uint8, iAvailableBytes int) uint32 {
	var uiValue uint32
	for i := 0; i < 4; i++ {
		uiValue <<= 8
		if i < iAvailableBytes {
			uiValue |= uint32(pDstNal[i])
		}
	}
	return uiValue
}

// InitReadBits ports int32_t InitReadBits (PBitStringAux pBitString, intX_t iEndOffset).
func InitReadBits(pBitString *common.SBitStringAux, iEndOffset int) int32 {
	kiAllowedBytes := (pBitString.PEndBuf - pBitString.PStartBuf) - iEndOffset
	kiReadBytes := pBitString.PCurBuf - pBitString.PStartBuf
	kiRemainBytes := kiAllowedBytes - kiReadBytes
	if kiRemainBytes <= 0 {
		return ERR_INFO_INVALID_ACCESS
	}
	kiSeedBytes := kiRemainBytes
	if kiSeedBytes > 4 {
		kiSeedBytes = 4
	}
	pBitString.UiCurBits = GetValue4BytesSafe(pBitString.PBuf[pBitString.PCurBuf:], kiSeedBytes)
	pBitString.PCurBuf += 4
	pBitString.ILeftBits = -16
	return ERR_NONE
}

// DecInitBits ports int32_t DecInitBits (PBitStringAux pBitString, const uint8_t* kpBuf, const
// int32_t kiSize): input bits for decoder.
//
// kpBuf/kiBufOff: (slice, offset) pair; kpBuf becomes pBitString.PBuf and the offsets are relative to
// it (cf. common.InitBits). kiSize is in bits.
func DecInitBits(pBitString *common.SBitStringAux, kpBuf []uint8, kiBufOff int, kiSize int32) int32 {
	kiSizeBuf := (kiSize + 7) >> 3

	if kpBuf == nil {
		return ERR_INFO_INVALID_ACCESS
	}

	pBitString.PBuf = kpBuf
	pBitString.PStartBuf = kiBufOff                // buffer to start position
	pBitString.PEndBuf = kiBufOff + int(kiSizeBuf) // buffer + length
	pBitString.IBits = kiSize                      // count bits of overall bitstreaming inputindex;
	pBitString.PCurBuf = pBitString.PStartBuf
	iErr := InitReadBits(pBitString, 0)
	if iErr != 0 {
		return iErr
	}
	return ERR_NONE
}

// RBSP2EBSP ports void RBSP2EBSP (uint8_t* pDstBuf, uint8_t* pSrcBuf, const int32_t kiSize).
//
// pDstBuf / pSrcBuf: sub-slices starting at the C pointers.
func RBSP2EBSP(pDstBuf []uint8, pSrcBuf []uint8, kiSize int32) {
	iSrc := 0
	iDst := 0
	iSrcEnd := int(kiSize)
	var iZeroCount int32

	for iSrc < iSrcEnd {
		if iZeroCount == 2 && pSrcBuf[iSrc] <= 3 {
			//add the code 0x03
			pDstBuf[iDst] = 3
			iDst++
			iZeroCount = 0
		}
		if pSrcBuf[iSrc] == 0 {
			iZeroCount++
		} else {
			iZeroCount = 0
		}
		pDstBuf[iDst] = pSrcBuf[iSrc]
		iDst++
		iSrc++
	}
}
