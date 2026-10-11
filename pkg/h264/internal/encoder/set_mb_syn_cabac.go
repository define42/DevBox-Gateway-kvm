// Port of codec/encoder/core/src/set_mb_syn_cabac.cpp.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

var g_kiClz5Table = [32]int8{
	6, 5, 4, 4, 3, 3, 3, 3, 2, 2, 2, 2, 2, 2, 2, 2,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
}

// (uint8_t* pBufCur, uint8_t* pBufStart) -> offsets into pBuf (SCabacCtx.m_pBuf).
func PropagateCarry(pBuf []uint8, pBufCur int, pBufStart int) {
	for ; pBufCur > pBufStart; pBufCur-- {
		pBuf[pBufCur-1]++
		if pBuf[pBufCur-1] != 0 {
			break
		}
	}
}

// pCtx: void* that is a sWelsEncCtx*.
func WelsCabacInit(pCtx *sWelsEncCtx) {
	pEncCtx := pCtx
	for iModel := 0; iModel < 4; iModel++ {
		for iQp := int32(0); iQp <= WELS_QP_MAX; iQp++ {
			for iIdx := 0; iIdx < common.WELS_CONTEXT_COUNT; iIdx++ {
				m := int32(common.G_kiCabacGlobalContextIdx[iIdx][iModel][0])
				n := int32(common.G_kiCabacGlobalContextIdx[iIdx][iModel][1])
				iPreCtxState := common.WELS_CLIP3(((m*iQp)>>4)+n, 1, 126)
				var uiValMps, uiStateIdx uint8
				if iPreCtxState <= 63 {
					uiStateIdx = uint8(63 - iPreCtxState)
					uiValMps = 0
				} else {
					uiStateIdx = uint8(iPreCtxState - 64)
					uiValMps = 1
				}
				pEncCtx.sWelsCabacContexts[iModel][iQp][iIdx].Set(uiStateIdx, uiValMps)
			}
		}
	}
}

func WelsCabacContextInit(pCtx *sWelsEncCtx, pCbCtx *SCabacCtx, iModel int32) {
	pEncCtx := pCtx
	iIdx := iModel + 1
	if pEncCtx.eSliceType == common.I_SLICE {
		iIdx = 0
	}
	iQp := pEncCtx.iGlobalQp
	pCbCtx.m_sStateCtx = pEncCtx.sWelsCabacContexts[iIdx][iQp]
}

// (uint8_t* pBuf, uint8_t* pEnd) -> the whole buffer plus the start/end offsets in it
// (e.g. WelsCabacEncodeInit (&pSlice.sCabacCtx, pBs.PBuf, pBs.PCurBuf, pBs.PEndBuf)).
func WelsCabacEncodeInit(pCbCtx *SCabacCtx, pBuf []uint8, iBufOff int, iEndOff int) {
	pCbCtx.m_uiLow = 0
	pCbCtx.m_iLowBitCnt = 9
	pCbCtx.m_iRenormCnt = 0
	pCbCtx.m_uiRange = 510
	pCbCtx.m_pBuf = pBuf
	pCbCtx.m_pBufStart = iBufOff
	pCbCtx.m_pBufEnd = iEndOff
	pCbCtx.m_pBufCur = iBufOff
}

func WelsCabacEncodeUpdateLowNontrivial_(pCbCtx *SCabacCtx) {
	iLowBitCnt := pCbCtx.m_iLowBitCnt
	iRenormCnt := pCbCtx.m_iRenormCnt
	uiLow := pCbCtx.m_uiLow
	pBuf := pCbCtx.m_pBuf

	for {
		pBufCur := pCbCtx.m_pBufCur
		kiInc := CABAC_LOW_WIDTH - 1 - iLowBitCnt

		uiLow <<= uint(kiInc)
		if uiLow&(cabac_low_t(1)<<(CABAC_LOW_WIDTH-1)) != 0 {
			PropagateCarry(pBuf, pBufCur, pCbCtx.m_pBufStart)
		}

		// CABAC_LOW_WIDTH > 32: WRITE_BE_32 (pBufCur, (uint32_t) (uiLow >> 31));
		v := uint32(uiLow >> 31)
		pBuf[pBufCur] = uint8(v >> 24)
		pBuf[pBufCur+1] = uint8(v >> 16)
		pBuf[pBufCur+2] = uint8(v >> 8)
		pBuf[pBufCur+3] = uint8(v)
		pBufCur += 4

		pBuf[pBufCur] = uint8(uiLow >> 23)
		pBufCur++
		pBuf[pBufCur] = uint8(uiLow >> 15)
		pBufCur++
		iRenormCnt -= kiInc
		iLowBitCnt = 15
		uiLow &= cabac_low_t((uint32(1) << uint(iLowBitCnt)) - 1)
		pCbCtx.m_pBufCur = pBufCur
		if !(iLowBitCnt+iRenormCnt > CABAC_LOW_WIDTH-1) {
			break
		}
	}

	pCbCtx.m_iLowBitCnt = iLowBitCnt + iRenormCnt
	pCbCtx.m_uiLow = uiLow << uint(iRenormCnt)
}

func WelsCabacEncodeDecisionLps_(pCbCtx *SCabacCtx, iCtx int32) {
	kiState := int32(pCbCtx.m_sStateCtx[iCtx].State())
	uiRange := pCbCtx.m_uiRange
	uiRangeLps := uint32(common.G_kuiCabacRangeLps[kiState][(uiRange&0xff)>>6])
	uiRange -= uiRangeLps
	var bState0 uint8
	if kiState == 0 {
		bState0 = 1
	}
	pCbCtx.m_sStateCtx[iCtx].Set(common.G_kuiStateTransTable[kiState][0],
		pCbCtx.m_sStateCtx[iCtx].Mps()^bState0)

	WelsCabacEncodeUpdateLow_(pCbCtx)
	pCbCtx.m_uiLow += cabac_low_t(uiRange)

	kiRenormAmount := int32(g_kiClz5Table[uiRangeLps>>3])
	pCbCtx.m_uiRange = uiRangeLps << uint(kiRenormAmount)
	pCbCtx.m_iRenormCnt = kiRenormAmount
}

func WelsCabacEncodeTerminate(pCbCtx *SCabacCtx, uiBin uint32) {
	pCbCtx.m_uiRange -= 2
	if uiBin != 0 {
		WelsCabacEncodeUpdateLow_(pCbCtx)
		pCbCtx.m_uiLow += cabac_low_t(pCbCtx.m_uiRange)

		const kiRenormAmount = 7
		pCbCtx.m_uiRange = 2 << kiRenormAmount
		pCbCtx.m_iRenormCnt = kiRenormAmount

		WelsCabacEncodeUpdateLow_(pCbCtx)
		pCbCtx.m_uiLow |= 0x80
	} else {
		kiRenormAmount := int32(pCbCtx.m_uiRange>>8) ^ 1
		pCbCtx.m_uiRange = pCbCtx.m_uiRange << uint(kiRenormAmount)
		pCbCtx.m_iRenormCnt += kiRenormAmount
	}
}

func WelsCabacEncodeUeBypass(pCbCtx *SCabacCtx, iExpBits int32, uiVal uint32) {
	iSufS := int32(uiVal)
	iStopLoop := int32(0)
	k := iExpBits
	for {
		if iSufS >= (1 << uint(k)) {
			WelsCabacEncodeBypassOne(pCbCtx, 1)
			iSufS = iSufS - (1 << uint(k))
			k++
		} else {
			WelsCabacEncodeBypassOne(pCbCtx, 0)
			for k > 0 {
				k--
				WelsCabacEncodeBypassOne(pCbCtx, (iSufS>>uint(k))&1)
			}
			iStopLoop = 1
		}
		if iStopLoop != 0 {
			break
		}
	}
}

func WelsCabacEncodeFlush(pCbCtx *SCabacCtx) {
	WelsCabacEncodeTerminate(pCbCtx, 1)

	uiLow := pCbCtx.m_uiLow
	iLowBitCnt := pCbCtx.m_iLowBitCnt
	pBufCur := pCbCtx.m_pBufCur
	pBuf := pCbCtx.m_pBuf

	uiLow <<= uint(CABAC_LOW_WIDTH - 1 - iLowBitCnt)
	if uiLow&(cabac_low_t(1)<<(CABAC_LOW_WIDTH-1)) != 0 {
		PropagateCarry(pBuf, pBufCur, pCbCtx.m_pBufStart)
	}
	for iLowBitCnt -= 8; iLowBitCnt >= 0; iLowBitCnt -= 8 {
		pBuf[pBufCur] = uint8(uiLow >> (CABAC_LOW_WIDTH - 9))
		pBufCur++
		uiLow <<= 8
	}

	pCbCtx.m_pBufCur = pBufCur
}

// returns the current write position as an offset into pCbCtx.m_pBuf.
func WelsCabacEncodeGetPtr(pCbCtx *SCabacCtx) int {
	return pCbCtx.m_pBufCur
}
