package gateway

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"sync"
	"syscall"
	"time"
)

const rdpDiagnosticEventLimit = 512

// rdpDiagnosticEvent is a redacted, user-visible RDP connection event. The
// connection owner is deliberately kept in rdpDiagnosticRecord instead, so it
// cannot be serialized accidentally.
type rdpDiagnosticEvent struct {
	At                 time.Time `json:"timestamp"`
	AttemptID          uint64    `json:"attemptId"`
	Stage              string    `json:"stage"`
	Outcome            string    `json:"outcome"`
	Message            string    `json:"message"`
	Error              string    `json:"error,omitempty"`
	VMID               string    `json:"vmId,omitempty"`
	RequestedProtocols *uint32   `json:"requestedProtocols,omitempty"`
	SelectedProtocol   *uint32   `json:"selectedProtocol,omitempty"`
	TLSVersion         string    `json:"tlsVersion,omitempty"`
	CipherSuite        string    `json:"cipherSuite,omitempty"`
}

type rdpDiagnosticRecord struct {
	owner string
	event rdpDiagnosticEvent
}

// rdpDiagnosticLog is a bounded in-memory event log. Its zero value is ready
// for use.
type rdpDiagnosticLog struct {
	mu            sync.Mutex
	nextAttemptID uint64
	records       [rdpDiagnosticEventLimit]rdpDiagnosticRecord
	start         int
	count         int
}

type rdpDiagnosticAttempt struct {
	log   *rdpDiagnosticLog
	owner string
	event rdpDiagnosticEvent
}

func (l *rdpDiagnosticLog) begin() *rdpDiagnosticAttempt {
	l.mu.Lock()
	l.nextAttemptID++
	id := l.nextAttemptID
	l.mu.Unlock()

	return &rdpDiagnosticAttempt{
		log:   l,
		event: rdpDiagnosticEvent{AttemptID: id},
	}
}

func (a *rdpDiagnosticAttempt) stage(stage string) {
	if a == nil || a.log == nil {
		return
	}
	stage, message := rdpDiagnosticStage(stage)
	a.event.Stage = stage
	a.event.Outcome = "progress"
	a.event.Message = message
	a.event.Error = ""
	a.emit()
}

func (a *rdpDiagnosticAttempt) record(outcome string) {
	if a == nil || a.log == nil {
		return
	}
	a.event.Stage, _ = rdpDiagnosticStage(a.event.Stage)
	a.event.Error = ""
	switch outcome {
	case "authenticated":
		a.event.Outcome = "authenticated"
		a.event.Message = "The RDP client was authenticated."
	default:
		a.event.Outcome = "progress"
		a.event.Message = "RDP connection processing advanced."
	}
	a.emit()
}

func (a *rdpDiagnosticAttempt) finish(err error) {
	if a == nil || a.log == nil {
		return
	}
	if stage, _ := rdpDiagnosticStage(a.event.Stage); stage == "unknown" {
		a.event.Stage = stage
	}
	if err == nil || (a.event.Stage == "session_proxy" && rdpDiagnosticNormalClose(err)) {
		a.event.Outcome = "closed"
		a.event.Message = "The RDP connection closed."
		a.event.Error = ""
	} else {
		a.event.Outcome = "failed"
		a.event.Message = "The RDP connection attempt failed."
		a.event.Error = rdpDiagnosticError(err)
	}
	a.emit()
}

func (a *rdpDiagnosticAttempt) emit() {
	a.event.At = time.Now().UTC()
	a.log.append(a.owner, a.event)
}

func (l *rdpDiagnosticLog) append(owner string, event rdpDiagnosticEvent) {
	record := rdpDiagnosticRecord{owner: owner, event: cloneRDPDiagnosticEvent(event)}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.count < len(l.records) {
		index := (l.start + l.count) % len(l.records)
		l.records[index] = record
		l.count++

		return
	}
	l.records[l.start] = record
	l.start = (l.start + 1) % len(l.records)
}

// snapshot returns events in insertion order. Ordinary users can see only
// records that were associated with their known owner at the time they were
// emitted. Administrators can request all records, including pre-routing
// records whose owner was not yet known.
func (l *rdpDiagnosticLog) snapshot(owner, vm string, all bool) []rdpDiagnosticEvent {
	l.mu.Lock()
	defer l.mu.Unlock()

	events := make([]rdpDiagnosticEvent, 0, l.count)
	for offset := 0; offset < l.count; offset++ {
		record := l.records[(l.start+offset)%len(l.records)]
		if !all && (owner == "" || record.owner == "" || record.owner != owner) {
			continue
		}
		if vm != "" && record.event.VMID != vm {
			continue
		}
		events = append(events, cloneRDPDiagnosticEvent(record.event))
	}

	return events
}

func cloneRDPDiagnosticEvent(event rdpDiagnosticEvent) rdpDiagnosticEvent {
	event.RequestedProtocols = cloneRDPDiagnosticUint32(event.RequestedProtocols)
	event.SelectedProtocol = cloneRDPDiagnosticUint32(event.SelectedProtocol)

	return event
}

func cloneRDPDiagnosticUint32(value *uint32) *uint32 {
	if value == nil {
		return nil
	}
	copy := *value

	return &copy
}

func rdpDiagnosticStage(stage string) (string, string) {
	switch stage {
	case "connection_request":
		return stage, "Reading the RDP connection request."
	case "negotiation_request":
		return stage, "Validating the RDP negotiation request."
	case "security_negotiation":
		return stage, "Selecting the frontend RDP security protocol."
	case "negotiation_response":
		return stage, "Sending the frontend RDP negotiation response."
	case "frontend_tls":
		return stage, "Negotiating TLS with the RDP client."
	case "frontend_server_name":
		return stage, "Validating the frontend TLS server name."
	case "routing_grant":
		return stage, "Resolving the one-time RDP routing grant."
	case "backend_lookup":
		return stage, "Looking up the VM RDP endpoint."
	case "backend_tls":
		return stage, "Connecting securely to the VM RDP endpoint."
	case "client_mcs_connect":
		return stage, "Reading the client MCS Connect Initial PDU."
	case "backend_mcs_connect":
		return stage, "Sending the MCS Connect Initial PDU to the VM."
	case "client_mcs_response":
		return stage, "Sending the MCS Connect Response PDU to the client."
	case "client_erect_domain":
		return stage, "Relaying the client MCS Erect Domain request."
	case "client_attach_user":
		return stage, "Reading the client MCS Attach User request."
	case "backend_attach_user":
		return stage, "Waiting for the VM MCS Attach User confirmation."
	case "client_attach_confirm":
		return stage, "Relaying the MCS Attach User confirmation to the client."
	case "client_channel_join_or_info":
		return stage, "Reading a client channel join or Client Info PDU."
	case "backend_channel_join":
		return stage, "Relaying a channel join request to the VM."
	case "client_channel_confirm":
		return stage, "Relaying a channel join confirmation to the client."
	case "client_info_validation":
		return stage, "Validating the RDP Client Info PDU."
	case "credential_authentication":
		return stage, "Authenticating the RDP credentials."
	case "routing_grant_claim":
		return stage, "Claiming the one-time RDP routing grant."
	case "connection_admission":
		return stage, "Admitting the authenticated RDP connection."
	case "backend_client_info":
		return stage, "Sending the validated Client Info PDU to the VM."
	case "session_proxy":
		return stage, "Proxying the established RDP session."
	default:
		return "unknown", "RDP connection processing advanced."
	}
}

func rdpDiagnosticError(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "operation canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "operation timed out"
	case errors.Is(err, ErrAuthentication):
		return "authentication failed"
	case errors.Is(err, ErrInvalid):
		return "invalid request or protocol data"
	case errors.Is(err, ErrNotFound):
		return "resource not found"
	case errors.Is(err, ErrConflict):
		return "resource conflict"
	case errors.Is(err, ErrStartupFailed):
		return "backend startup failed"
	case errors.Is(err, ErrMemoryCapacity):
		return "insufficient backend capacity"
	case rdpDiagnosticX509Error(err):
		return "TLS certificate validation failed"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.ECONNABORTED):
		return "connection reset"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, net.ErrClosed), errors.Is(err, syscall.EPIPE):
		return "connection closed"
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return "network timeout"
	}

	return "internal connection error"
}

func rdpDiagnosticNormalClose(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE)
}

func rdpDiagnosticX509Error(err error) bool {
	if errors.Is(err, x509.IncorrectPasswordError) {
		return true
	}
	var certificateInvalid x509.CertificateInvalidError
	var certificateInvalidPointer *x509.CertificateInvalidError
	var constraintViolation x509.ConstraintViolationError
	var constraintViolationPointer *x509.ConstraintViolationError
	var hostname x509.HostnameError
	var hostnamePointer *x509.HostnameError
	var insecureAlgorithm x509.InsecureAlgorithmError
	var insecureAlgorithmPointer *x509.InsecureAlgorithmError
	var systemRoots x509.SystemRootsError
	var systemRootsPointer *x509.SystemRootsError
	var unknownAuthority x509.UnknownAuthorityError
	var unknownAuthorityPointer *x509.UnknownAuthorityError

	return errors.As(err, &certificateInvalid) || errors.As(err, &certificateInvalidPointer) ||
		errors.As(err, &constraintViolation) || errors.As(err, &constraintViolationPointer) ||
		errors.As(err, &hostname) || errors.As(err, &hostnamePointer) ||
		errors.As(err, &insecureAlgorithm) || errors.As(err, &insecureAlgorithmPointer) ||
		errors.As(err, &systemRoots) || errors.As(err, &systemRootsPointer) ||
		errors.As(err, &unknownAuthority) || errors.As(err, &unknownAuthorityPointer)
}
