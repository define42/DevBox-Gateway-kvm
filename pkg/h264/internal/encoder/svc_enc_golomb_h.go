// Port of codec/encoder/core/inc/svc_enc_golomb.h.
// (The generic golomb writers BsWriteBits, BsWriteUE, ... live in
// package common, codec/common/inc/golomb_common.h.)

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

/************************************************************************/
/* GOLOMB CODIMG FOR WELS ENCODER ONLY                                  */
/************************************************************************/

// BsSizeUE gets the size of unsigned exp golomb codes.
func BsSizeUE(kiValue uint32) uint32 {
	if 256 > kiValue {
		return common.G_kuiGolombUELength[kiValue]
	} else {
		n := uint32(0)
		iTmpValue := kiValue + 1

		if iTmpValue&0xffff0000 != 0 {
			iTmpValue >>= 16
			n += 16
		}
		if iTmpValue&0xff00 != 0 {
			iTmpValue >>= 8
			n += 8
		}

		//n += (g_kuiGolombUELength[iTmpValue] >> 1);
		n += (common.G_kuiGolombUELength[iTmpValue-1] >> 1)
		return ((n << 1) + 1)
	}
}

// BsSizeSE gets the size of signed exp golomb codes.
func BsSizeSE(kiValue int32) uint32 {
	var iTmpValue uint32
	if 0 == kiValue {
		return 1
	} else if 0 < kiValue {
		iTmpValue = uint32((kiValue << 1) - 1)
		return BsSizeUE(iTmpValue)
	} else {
		iTmpValue = uint32((-kiValue) << 1)
		return BsSizeUE(iTmpValue)
	}
}

// BsWriteTE writes truncated exp golomb codes.
func BsWriteTE(pBs *common.SBitStringAux, kiX int32, kuiValue uint32) {
	if 1 == kiX {
		var uiBit uint32
		if kuiValue == 0 { // !kuiValue
			uiBit = 1
		}
		common.BsWriteOneBit(pBs, uiBit)
	} else {
		common.BsWriteUE(pBs, kuiValue)
	}
}

// BsGetBitsPos returns the current writing position in bits.
func BsGetBitsPos(pBs *common.SBitStringAux) int32 {
	return int32(((pBs.PCurBuf - pBs.PStartBuf) << 3) + 32 - int(pBs.ILeftBits))
}

// BsAlign byte-aligns the writer with 1 bits and flushes it.
func BsAlign(pBs *common.SBitStringAux) {
	if pBs.ILeftBits&7 != 0 {
		pBs.UiCurBits <<= uint32(pBs.ILeftBits & 7)
		pBs.UiCurBits |= (uint32(1) << uint32(pBs.ILeftBits&7)) - 1
		pBs.ILeftBits &= ^int32(7)
	}
	common.BsFlush(pBs)
}
