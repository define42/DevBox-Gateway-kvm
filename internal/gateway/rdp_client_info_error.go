package gateway

import "errors"

// rdpClientInfoError identifies a fixed, non-sensitive Client Info validation
// failure. It deliberately does not retain the rejected value, field contents,
// flags, or lengths.
type rdpClientInfoError uint8

const (
	rdpClientInfoMissingChannels rdpClientInfoError = iota + 1
	rdpClientInfoMCSHeader
	rdpClientInfoPacketLength
	rdpClientInfoSecurityFlags
	rdpClientInfoSmartCardPIN
	rdpClientInfoFieldLength
	rdpClientInfoFieldTerminator
	rdpClientInfoCredentialEncoding
	rdpClientInfoCredentialLength
	rdpClientInfoEmptyUsername
	rdpClientInfoEmptyPassword
)

func (rdpClientInfoError) Error() string { return "RDP Client Info validation failed" }

func (rdpClientInfoError) Unwrap() error { return ErrAuthentication }

func rdpClientInfoReason(err error) string {
	var clientInfoError rdpClientInfoError
	if !errors.As(err, &clientInfoError) {
		return ""
	}
	switch clientInfoError {
	case rdpClientInfoMissingChannels:
		return "missing_required_channels"
	case rdpClientInfoMCSHeader:
		return "invalid_mcs_header"
	case rdpClientInfoPacketLength:
		return "invalid_packet_length"
	case rdpClientInfoSecurityFlags:
		return "unsupported_security_flags"
	case rdpClientInfoSmartCardPIN:
		return "smart_card_pin_not_supported"
	case rdpClientInfoFieldLength:
		return "invalid_field_length"
	case rdpClientInfoFieldTerminator:
		return "invalid_field_terminator"
	case rdpClientInfoCredentialEncoding:
		return "invalid_credential_encoding"
	case rdpClientInfoCredentialLength:
		return "credential_too_long"
	case rdpClientInfoEmptyUsername:
		return "empty_username"
	case rdpClientInfoEmptyPassword:
		return "empty_password"
	default:
		return ""
	}
}
