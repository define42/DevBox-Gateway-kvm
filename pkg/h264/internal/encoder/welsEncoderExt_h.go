// Port of codec/encoder/plus/inc/welsEncoderExt.h.

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// CWelsH264SVCEncoder is the encoder (class CWelsH264SVCEncoder : public
// ISVCEncoder). It implements api.ISVCEncoder. The OUTPUT_BIT_STREAM,
// DUMP_SRC_PICTURE and REC_FRAME_COUNT debug members are not ported.
type CWelsH264SVCEncoder struct {
	m_pEncContext   *sWelsEncCtx
	m_pWelsTrace    *common.WelsCodecTrace
	m_iMaxPicWidth  int32
	m_iMaxPicHeight int32
	m_iCspInternal  int32
	m_bInitialFlag  bool
}

// NewCWelsH264SVCEncoder is the C++ constructor (new CWelsH264SVCEncoder()).
func NewCWelsH264SVCEncoder() *CWelsH264SVCEncoder {
	p := &CWelsH264SVCEncoder{}
	p.ctorCWelsH264SVCEncoder()
	return p
}

var _ api.ISVCEncoder = (*CWelsH264SVCEncoder)(nil)
