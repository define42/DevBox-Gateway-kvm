package encoder

import (
	"encoding/binary"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// The macroblock residual writer keeps the CAVLC bit-writer state in a 64-bit
// accumulator that is passed to and returned from each block call, so the
// compiler keeps it in registers for the whole macroblock. It emits the same
// big-endian 32-bit words, at the same moments, as the SBitStringAux writer:
// a word is stored exactly when 32 or more bits are pending.
//
// State: acc holds cnt (< 32) valid low bits; bits above cnt are stale and are
// never read. pos is the byte offset of the next word in the buffer.

// Mode bits of cavlcWriteBlock.
const (
	cavlcModeEndMask  = 0x0f // last coefficient index (iEndIdx)
	cavlcModeChromaDC = 0x10 // use the chroma DC total-zeros table
	cavlcModeCalc     = 0x20 // extract levels and runs (iCalRunLevelFlag)
	cavlcModeNCShift  = 8    // nC in bits 8..15
)

// Packed code tables: code | length<<16. Dimensions are powers of two so that
// masked indexes need no bounds checks; the masks never change a valid index.
var (
	cavlcNcMap        [32]uint8
	cavlcTokenTable   [8][32][4]uint32
	cavlcTotalZeros   [16][16]uint32
	cavlcTotalZerosDC [4][4]uint32
	cavlcRunBefore    [8][16]uint32
	cavlcZeroLeft     [16]uint8
	cavlcLevels       [2][8][2 * cavlcLevelLimit]cavlcLevelSymbol
)

func init() {
	copy(cavlcNcMap[:], g_kuiEncNcMapTable[:])
	pack := func(e [2]uint8) uint32 { return uint32(e[0]) | uint32(e[1])<<16 }
	for nc := range g_kuiVlcCoeffToken {
		for total := range g_kuiVlcCoeffToken[nc] {
			for t1 := range g_kuiVlcCoeffToken[nc][total] {
				cavlcTokenTable[nc][total][t1] = pack(g_kuiVlcCoeffToken[nc][total][t1])
			}
		}
	}
	for total := range g_kuiVlcTotalZeros {
		for zeros := range g_kuiVlcTotalZeros[total] {
			cavlcTotalZeros[total][zeros] = pack(g_kuiVlcTotalZeros[total][zeros])
		}
	}
	for total := range g_kuiVlcTotalZerosChromaDc {
		for zeros := range g_kuiVlcTotalZerosChromaDc[total] {
			cavlcTotalZerosDC[total][zeros] = pack(g_kuiVlcTotalZerosChromaDc[total][zeros])
		}
	}
	for left := range g_kuiVlcRunBefore {
		for run := range g_kuiVlcRunBefore[left] {
			cavlcRunBefore[left][run] = pack(g_kuiVlcRunBefore[left][run])
		}
	}
	copy(cavlcZeroLeft[:], g_kuiZeroLeftMap[:])
	for adjustment := range cavlcLevelTable {
		copy(cavlcLevels[adjustment][:], cavlcLevelTable[adjustment][:])
	}
}

// cavlcPut appends the n low bits of v (v < 1<<n, n <= 31).
func cavlcPut(out *[]byte, pos int, acc uint64, cnt uint, n uint, v uint64) (int, uint64, uint) {
	acc = acc<<(n&63) | v
	cnt += n
	if cnt >= 32 {
		cnt -= 32
		binary.BigEndian.PutUint32((*out)[pos:], uint32(acc>>(cnt&63)))
		pos += 4
	}
	return pos, acc, cnt
}

func cavlcPutCode(out *[]byte, pos int, acc uint64, cnt uint, code uint32) (int, uint64, uint) {
	return cavlcPut(out, pos, acc, cnt, uint(code>>16), uint64(code&0xffff))
}

// cavlcIsOne reports whether a nonzero level is ±1.
func cavlcIsOne(level int16) bool { return uint16(level+1)&^2 == 0 }

// cavlcWriteBlock writes one residual block. On a level overflow it returns
// the state it was given and false, as WriteBlockResidualCavlc leaves the
// caller's state unchanged in that case; words already stored match it too.
func cavlcWriteBlock(out *[]byte, pos int, acc uint64, cnt uint, coeff []int16, mode uint) (int, uint64, uint, bool) {
	var level [16]int16
	var run [16]uint8
	total, zeros := int32(0), int32(0)
	end := int32(mode & cavlcModeEndMask)
	if mode&cavlcModeCalc != 0 {
		if cavlcExtractSupported(coeff, end) {
			zeros = cavlcExtractSSSE3((*[16]int16)(coeff), &run, &level, &total, end)
		} else {
			zeros = CavlcParamCal_c(coeff, run[:], level[:], &total, end)
		}
	}

	table := &cavlcTokenTable[cavlcNcMap[(mode>>cavlcModeNCShift)&31]&7][total&31]
	if total == 0 {
		pos, acc, cnt = cavlcPutCode(out, pos, acc, cnt, table[0])
		return pos, acc, cnt, true
	}

	// At most three trailing ones, each followed by its sign bit.
	t1, sign := 0, uint64(0)
	if cavlcIsOne(level[0]) {
		t1, sign = 1, uint64(uint16(level[0])>>15)
		if total > 1 && cavlcIsOne(level[1]) {
			t1, sign = 2, sign<<1|uint64(uint16(level[1])>>15)
			if total > 2 && cavlcIsOne(level[2]) {
				t1, sign = 3, sign<<1|uint64(uint16(level[2])>>15)
			}
		}
	}
	token := table[t1&3]
	pos0, acc0, cnt0 := pos, acc, cnt
	pos, acc, cnt = cavlcPut(out, pos, acc, cnt, uint(token>>16)+uint(t1), uint64(token&0xffff)<<uint(t1)|sign)

	suffix := uint32(0)
	if total > 10 && t1 < 3 {
		suffix = 1
	}
	adjustment := 0
	if t1 < 3 {
		adjustment = 1
	}
	for i := int32(t1); i < total; i++ {
		value := int32(level[i&15])
		var symbol cavlcLevelSymbol
		if index := uint32(value + cavlcLevelLimit); index < 2*cavlcLevelLimit {
			symbol = cavlcLevels[adjustment&1][suffix&7][index]
		} else {
			var ok bool
			if symbol, ok = cavlcLevelSymbolSlow(value, int32(suffix), adjustment != 0); !ok {
				return pos0, acc0, cnt0, false
			}
		}
		pos, acc, cnt = cavlcPut(out, pos, acc, cnt, uint(symbol.length), uint64(symbol.code))
		suffix = uint32(symbol.nextSuffix)
		adjustment = 0
	}

	if total < end+1 {
		if mode&cavlcModeChromaDC != 0 {
			pos, acc, cnt = cavlcPutCode(out, pos, acc, cnt, cavlcTotalZerosDC[total&3][zeros&3])
		} else {
			pos, acc, cnt = cavlcPutCode(out, pos, acc, cnt, cavlcTotalZeros[total&15][zeros&15])
		}
	}

	left := zeros
	for i := int32(0); i+1 < total && left > 0; i++ {
		r := run[i&15]
		pos, acc, cnt = cavlcPutCode(out, pos, acc, cnt, cavlcRunBefore[cavlcZeroLeft[left&15]&7][r&15])
		left -= int32(r)
	}
	return pos, acc, cnt, true
}

func cavlcMode(end int32, calc bool, chromaDC bool, nc int8) uint {
	mode := uint(end) | uint(uint8(nc))<<cavlcModeNCShift
	if calc {
		mode |= cavlcModeCalc
	}
	if chromaDC {
		mode |= cavlcModeChromaDC
	}
	return mode
}

func cavlcLoad(pBs *common.SBitStringAux) (int, uint64, uint) {
	return pBs.PCurBuf, uint64(pBs.UiCurBits), uint(32 - pBs.ILeftBits)
}

func cavlcStore(pBs *common.SBitStringAux, pos int, acc uint64, cnt uint) {
	pBs.PCurBuf = pos
	pBs.UiCurBits = uint32(acc) & (1<<(cnt&31) - 1)
	pBs.ILeftBits = int32(32 - cnt)
}

func WriteBlockResidualCavlc(pFuncList *SWelsFuncPtrList, pCoffLevel []int16, iEndIdx int32, iCalRunLevelFlag int32, iResidualProperty int32, iNC int8, pBs *common.SBitStringAux) int32 {
	pos, acc, cnt := cavlcLoad(pBs)
	pos, acc, cnt, ok := cavlcWriteBlock(&pBs.PBuf, pos, acc, cnt, pCoffLevel, cavlcMode(iEndIdx, iCalRunLevelFlag != 0, iResidualProperty == int32(CHROMA_DC), iNC))
	if !ok {
		return ENC_RETURN_VLCOVERFLOWFOUND
	}
	cavlcStore(pBs, pos, acc, cnt)
	return ENC_RETURN_SUCCESS
}

func WelsWriteMbResidual(pFuncList *SWelsFuncPtrList, sMbCacheInfo *SMbCache, pCurMb *SMB, pBs *common.SBitStringAux) int32 {
	uiMbType := pCurMb.uiMbType
	kiCbpChroma := uint32(pCurMb.uiCbp >> 4)
	kiCbpLuma := uint32(pCurMb.uiCbp & 0x0F)
	nz := &sMbCacheInfo.iNonZeroCoeffCount
	dct := sMbCacheInfo.pDct
	out := &pBs.PBuf
	pos, acc, cnt := cavlcLoad(pBs)
	ok := true

	if common.IS_INTRA16x16(uiMbType) {
		nc := common.WELS_NON_ZERO_COUNT_AVERAGE(nz[8], nz[1])
		if pos, acc, cnt, ok = cavlcWriteBlock(out, pos, acc, cnt, dct.iLumaI16x16Dc[:], cavlcMode(15, true, false, nc)); !ok {
			goto overflow
		}
		if kiCbpLuma != 0 {
			for i := 0; i < 16; i++ {
				idx := int(common.G_kuiCache48CountScan4Idx[i])
				nc := common.WELS_NON_ZERO_COUNT_AVERAGE(nz[idx-1], nz[idx-8])
				if pos, acc, cnt, ok = cavlcWriteBlock(out, pos, acc, cnt, dct.iLumaBlock[i*16:i*16+16], cavlcMode(14, nz[idx] > 0, false, nc)); !ok {
					goto overflow
				}
			}
		}
	} else if kiCbpLuma != 0 {
		for i := 0; i < 16; i += 4 {
			if kiCbpLuma&(1<<uint(i>>2)) == 0 {
				continue
			}
			idx := int(common.G_kuiCache48CountScan4Idx[i])
			kiA, kiB, kiC, kiD := nz[idx], nz[idx+1], nz[idx+8], nz[idx+9]
			blocks := dct.iLumaBlock[i*16 : i*16+64]
			nc := common.WELS_NON_ZERO_COUNT_AVERAGE(nz[idx-1], nz[idx-8])
			if pos, acc, cnt, ok = cavlcWriteBlock(out, pos, acc, cnt, blocks[0:16], cavlcMode(15, kiA > 0, false, nc)); !ok {
				goto overflow
			}
			nc = common.WELS_NON_ZERO_COUNT_AVERAGE(kiA, nz[idx-7])
			if pos, acc, cnt, ok = cavlcWriteBlock(out, pos, acc, cnt, blocks[16:32], cavlcMode(15, kiB > 0, false, nc)); !ok {
				goto overflow
			}
			nc = common.WELS_NON_ZERO_COUNT_AVERAGE(nz[idx+7], kiA)
			if pos, acc, cnt, ok = cavlcWriteBlock(out, pos, acc, cnt, blocks[32:48], cavlcMode(15, kiC > 0, false, nc)); !ok {
				goto overflow
			}
			nc = common.WELS_NON_ZERO_COUNT_AVERAGE(kiC, kiB)
			if pos, acc, cnt, ok = cavlcWriteBlock(out, pos, acc, cnt, blocks[48:64], cavlcMode(15, kiD > 0, false, nc)); !ok {
				goto overflow
			}
		}
	}

	if kiCbpChroma != 0 {
		for plane := 0; plane < 2; plane++ {
			if pos, acc, cnt, ok = cavlcWriteBlock(out, pos, acc, cnt, dct.iChromaDc[plane][:], cavlcMode(3, true, true, CHROMA_DC_NC_OFFSET)); !ok {
				goto overflow
			}
		}
		if kiCbpChroma&0x02 != 0 {
			for plane := 0; plane < 2; plane++ {
				for i := 0; i < 4; i++ {
					idx := 24*plane + int(common.G_kuiCache48CountScan4Idx[16+i])
					nc := common.WELS_NON_ZERO_COUNT_AVERAGE(nz[idx-1], nz[idx-8])
					block := dct.iChromaBlock[(plane*4+i)*16 : (plane*4+i)*16+16]
					if pos, acc, cnt, ok = cavlcWriteBlock(out, pos, acc, cnt, block, cavlcMode(14, nz[idx] > 0, false, nc)); !ok {
						goto overflow
					}
				}
			}
		}
	}
	cavlcStore(pBs, pos, acc, cnt)
	return 0
overflow:
	// Keep the state of the last complete block, as the per-block writer did.
	cavlcStore(pBs, pos, acc, cnt)
	return ENC_RETURN_VLCOVERFLOWFOUND
}
