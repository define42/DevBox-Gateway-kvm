// Port of codec/encoder/core/inc/set_mb_syn_cabac.h.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

const WELS_QP_MAX = 51

type cabac_low_t = uint64

const CABAC_LOW_WIDTH = 64 // sizeof (cabac_low_t) / sizeof (uint8_t) * 8

// SStateCtx is the packed representation of state and MPS as state << 1 | MPS.
type SStateCtx struct {
	m_uiStateMps uint8
}

func (p *SStateCtx) Mps() uint8   { return p.m_uiStateMps & 1 }
func (p *SStateCtx) State() uint8 { return p.m_uiStateMps >> 1 }
func (p *SStateCtx) Set(uiState uint8, uiMps uint8) {
	p.m_uiStateMps = uiState*2 + uiMps
}

// SCabacCtx is the CABAC encoder engine.
//
// The C byte pointers m_pBufStart / m_pBufEnd / m_pBufCur are int offsets
// into m_pBuf (Go-only field), which holds the whole bitstream buffer the
// engine writes into (the same slice as the SBitStringAux it was started
// from), so PropagateCarry can walk backwards from m_pBufCur.
type SCabacCtx struct {
	m_uiLow      cabac_low_t
	m_iLowBitCnt int32
	m_iRenormCnt int32
	m_uiRange    uint32
	m_sStateCtx  [common.WELS_CONTEXT_COUNT]SStateCtx
	m_pBuf       []uint8 // Go-only: whole output buffer
	m_pBufStart  int     // C uint8_t*: offset into m_pBuf
	m_pBufEnd    int     // C uint8_t*: offset into m_pBuf
	m_pBufCur    int     // C uint8_t*: offset into m_pBuf
}

// WelsCabacEncodeUpdateLow_ is a private helper used by the public inline functions.
func WelsCabacEncodeUpdateLow_(pCbCtx *SCabacCtx) {
	if pCbCtx.m_iLowBitCnt+pCbCtx.m_iRenormCnt < CABAC_LOW_WIDTH {
		pCbCtx.m_iLowBitCnt += pCbCtx.m_iRenormCnt
		pCbCtx.m_uiLow <<= uint(pCbCtx.m_iRenormCnt)
	} else {
		WelsCabacEncodeUpdateLowNontrivial_(pCbCtx)
	}
	pCbCtx.m_iRenormCnt = 0
}

// WelsCabacEncodeDecision encodes one bin with context iCtx.
func WelsCabacEncodeDecision(pCbCtx *SCabacCtx, iCtx int32, uiBin uint32) {
	if uiBin == uint32(pCbCtx.m_sStateCtx[iCtx].Mps()) {
		kiState := int32(pCbCtx.m_sStateCtx[iCtx].State())
		uiRange := pCbCtx.m_uiRange
		uiRangeLps := uint32(common.G_kuiCabacRangeLps[kiState][(uiRange&0xff)>>6])
		uiRange -= uiRangeLps
		kiRenormAmount := int32(uiRange>>8) ^ 1
		pCbCtx.m_uiRange = uiRange << uint(kiRenormAmount)
		pCbCtx.m_iRenormCnt += kiRenormAmount
		pCbCtx.m_sStateCtx[iCtx].Set(common.G_kuiStateTransTable[kiState][1], uint8(uiBin))
	} else {
		WelsCabacEncodeDecisionLps_(pCbCtx, iCtx)
	}
}

// WelsCabacEncodeBypassOne encodes one bypass bin.
func WelsCabacEncodeBypassOne(pCbCtx *SCabacCtx, uiBin int32) {
	kuiBinBitmask := uint32(-uiBin)
	pCbCtx.m_iRenormCnt++
	WelsCabacEncodeUpdateLow_(pCbCtx)
	pCbCtx.m_uiLow += cabac_low_t(kuiBinBitmask & pCbCtx.m_uiRange)
}
