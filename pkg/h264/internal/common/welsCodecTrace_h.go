package common

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"

// Port of codec/common/inc/welsCodecTrace.h.

// WelsCodecTrace mirrors the C++ class welsCodecTrace. Create it with
// NewWelsCodecTrace (the C++ constructor); M_sLogCtx is the SLogContext to
// hand to WelsLog.
type WelsCodecTrace struct {
	m_iTraceLevel int32
	m_fpTrace     api.WelsTraceCallback
	m_pTraceCtx   any

	M_sLogCtx SLogContext
}
