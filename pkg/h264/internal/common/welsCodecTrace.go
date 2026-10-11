package common

import (
	"fmt"
	"os"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

// Port of codec/common/src/welsCodecTrace.cpp.

func welsStderrTrace(ctx any, level int32, str string) {
	fmt.Fprintf(os.Stderr, "%s\n", str)
}

// NewWelsCodecTrace is the welsCodecTrace constructor: trace level
// WELS_LOG_DEFAULT, output to stderr.
func NewWelsCodecTrace() *WelsCodecTrace {
	t := &WelsCodecTrace{}
	t.m_iTraceLevel = api.WELS_LOG_DEFAULT
	t.m_fpTrace = welsStderrTrace
	t.m_pTraceCtx = nil

	t.M_sLogCtx.PLogCtx = t
	t.M_sLogCtx.PfLog = StaticCodecTrace
	t.M_sLogCtx.PCodecInstance = nil
	return t
}

// StaticCodecTrace is the SLogContext.PfLog callback; pCtx must be the
// *WelsCodecTrace.
func StaticCodecTrace(pCtx any, iLevel int32, Str_Format string, vl []any) {
	self, ok := pCtx.(*WelsCodecTrace)
	if !ok || self == nil {
		return
	}
	self.CodecTrace(iLevel, Str_Format, vl)
}

// CodecTrace formats the message (truncated to MAX_LOG_SIZE-1 bytes) and
// forwards it to the trace callback when iLevel is enabled.
func (t *WelsCodecTrace) CodecTrace(iLevel int32, Str_Format string, vl []any) {
	if t.m_iTraceLevel < iLevel {
		return
	}

	var pBuf string
	WelsVsnprintf(&pBuf, MAX_LOG_SIZE, Str_Format, vl) // confirmed_safe_unsafe_usage
	if t.m_fpTrace != nil {
		t.m_fpTrace(t.m_pTraceCtx, iLevel, pBuf)
	}
}

func (t *WelsCodecTrace) SetCodecInstance(pCodecInstance any) {
	t.M_sLogCtx.PCodecInstance = pCodecInstance
}

func (t *WelsCodecTrace) SetTraceLevel(iLevel int32) {
	if iLevel >= 0 {
		t.m_iTraceLevel = iLevel
	}
}

// SetTraceCallback sets the trace callback (nil disables output).
func (t *WelsCodecTrace) SetTraceCallback(fn api.WelsTraceCallback) {
	t.m_fpTrace = fn
}

func (t *WelsCodecTrace) SetTraceCallbackContext(ctx any) {
	t.m_pTraceCtx = ctx
}
