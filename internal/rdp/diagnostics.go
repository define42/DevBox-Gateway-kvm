package rdp

import "errors"

// Backend diagnostic stages are fixed labels describing the last protocol
// operation attempted by the VM worker. They never contain peer-controlled
// packet data, credentials, addresses, or error text.
const (
	diagnosticStageNegotiation             = "negotiation"
	diagnosticStageTLS                     = "tls"
	diagnosticStageMCSConnect              = "mcs_connect"
	diagnosticStageActivationErectDomain   = "activation_erect_domain"
	diagnosticStageActivationAttachUser    = "activation_attach_user"
	diagnosticStageActivationChannelJoin   = "activation_channel_join"
	diagnosticStageActivationClientInfo    = "activation_client_info"
	diagnosticStageActivationLicense       = "activation_license"
	diagnosticStageActivationDemandActive  = "activation_demand_active"
	diagnosticStageActivationConfirmActive = "activation_confirm_active"
	diagnosticStageActivationFinalize      = "activation_finalize"
	diagnosticStageAudio                   = "audio"
	diagnosticStageDynamicChannels         = "dynamic_channels"
	diagnosticStageDisplayControl          = "display_control"
	diagnosticStageGraphics                = "graphics"
	diagnosticStageSessionRead             = "session_read"
)

type diagnosticStageError struct {
	stage string
	err   error
}

func (e *diagnosticStageError) Error() string { return e.err.Error() }
func (e *diagnosticStageError) Unwrap() error { return e.err }

func withDiagnosticStage(stage string, err error) error {
	if err == nil {
		return nil
	}
	var staged *diagnosticStageError
	if errors.As(err, &staged) {
		return err
	}

	return &diagnosticStageError{stage: stage, err: err}
}

func observeDiagnosticStage(observe func(string), stage string) {
	if observe != nil {
		observe(stage)
	}
}

// DiagnosticStage returns the fixed subsystem label attached to an RDP error.
// The error itself can still be inspected with errors.Is and errors.As.
func DiagnosticStage(err error) (string, bool) {
	var staged *diagnosticStageError
	if !errors.As(err, &staged) {
		return "", false
	}

	return staged.stage, true
}
