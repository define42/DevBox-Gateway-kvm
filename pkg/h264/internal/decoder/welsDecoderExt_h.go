// Port of codec/decoder/plus/inc/welsDecoderExt.h.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

//#define OUTPUT_BIT_STREAM  ////for test to output bitstream (not ported)

// CWelsDecoder implements api.ISVCDecoder (class CWelsDecoder : public
// ISVCDecoder). Create it with NewCWelsDecoder (the C++ constructor) and
// release it with Destroy (the C++ destructor).
//
// Threading members are kept as inert placeholders: m_iThreadCount stays 0
// in the Go port, so the sequential code paths are always taken.
type CWelsDecoder struct {
	m_pWelsTrace         *common.WelsCodecTrace
	m_uiDecodeTimeStamp  uint32
	m_bIsBaseline        bool
	m_iCpuCount          int32
	m_iThreadCount       int32
	m_iCtxCount          int32
	m_pPicBuff           *SPicBuff
	m_bParamSetsLostFlag bool
	m_bFreezeOutput      bool
	m_DecCtxActiveCount  int32
	m_pDecThrCtx         []SWelsDecoderThreadCTX // C: new SWelsDecoderThreadCTX[m_iCtxCount]
	m_pLastDecThrCtx     *SWelsDecoderThreadCTX
	m_iLastBufferedIdx   int32
	// WELS_MUTEX m_csDecoder is dropped (no threads).
	m_sBufferingEvent     SWelsDecEvent
	m_sReleaseBufferEvent SWelsDecEvent
	m_sIsBusy             SWelsDecSemphore
	m_sPictInfoList       [16]SPictInfo
	m_sReoderingStatus    SPictReoderingStatus
	m_pDecThrCtxActive    [WELS_DEC_MAX_NUM_CPU]*SWelsDecoderThreadCTX
	m_sVlcTable           SVlcTable
	m_sLastDecPicInfo     SWelsLastDecPicInfo
	m_sDecoderStatistics  api.SDecoderStatistics // For real time debugging
	m_iStreamSeqNum       int32
}

var _ api.ISVCDecoder = (*CWelsDecoder)(nil)
