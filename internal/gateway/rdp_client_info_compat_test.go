package gateway

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func compatibleClientInfoSecurityHeaders() []struct {
	name  string
	flags uint32
} {
	return []struct {
		name  string
		flags uint32
	}{
		{name: "info", flags: 0x00000040},
		{name: "encrypt", flags: 0x00000048},
		{name: "reset sequence", flags: 0x00000050},
		{name: "ignore sequence", flags: 0x00000060},
		{name: "all tolerated low flags", flags: 0x00000078},
		{name: "ignored high flags", flags: 0xa5a50040},
		{name: "valid high flags", flags: 0xa5a58040},
		{name: "all tolerated flags with high flags", flags: 0xa5a58078},
	}
}

func clientInfoTestPacket(t *testing.T, username, password string) ([]byte, []byte) {
	t.Helper()
	mcs := clientInfoPacket(username, password, true)[7:]
	info, err := clientInfoData(mcs)
	if err != nil {
		t.Fatalf("build valid Client Info: %v", err)
	}

	return mcs, info
}

func requireRDPClientInfoError(t *testing.T, err error, want rdpClientInfoError) {
	t.Helper()
	var got rdpClientInfoError
	if !errors.As(err, &got) {
		t.Fatalf("error type = %T, want rdpClientInfoError", err)
	}
	if got != want {
		t.Fatalf("Client Info error = %d, want %d", got, want)
	}
	if !errors.Is(err, ErrAuthentication) {
		t.Fatal("Client Info rejection does not unwrap to ErrAuthentication")
	}
}

func TestClientInfoAcceptsCompatibleSecurityHeaders(t *testing.T) {
	t.Parallel()
	for _, tc := range compatibleClientInfoSecurityHeaders() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mcs, info := clientInfoTestPacket(t, "alice", "rdp-password")
			binary.LittleEndian.PutUint32(info[:4], tc.flags)

			credentials, sanitized, err := parseClientInfo(mcs)
			if err != nil {
				t.Fatalf("compatible security header rejected: %v", err)
			}
			want := rdpCredentials{Username: "alice", Password: "rdp-password"}
			if credentials != want {
				t.Fatalf("credentials = %+v, want username and password unchanged", credentials)
			}
			cleanInfo, err := clientInfoData(sanitized[7:])
			if err != nil {
				t.Fatalf("parse sanitized Client Info: %v", err)
			}
			if got := binary.LittleEndian.Uint32(cleanInfo[:4]); got != 0x40 {
				t.Fatalf("forwarded security header = %#x, want 0x40", got)
			}
			if got := binary.LittleEndian.Uint16(cleanInfo[16:18]); got != 0 {
				t.Fatalf("forwarded password length = %d, want 0", got)
			}
		})
	}
}

func TestCompatibleClientInfoHeadersCannotBypassAuthentication(t *testing.T) {
	t.Parallel()
	for _, tc := range compatibleClientInfoSecurityHeaders() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mcs, info := clientInfoTestPacket(t, "alice", "rdp-password")
			binary.LittleEndian.PutUint32(info[:4], tc.flags)
			calls := 0
			forwarded, err := authenticatedClientInfoWithTrace(
				t.Context(), mcs,
				func(_ context.Context, credentials rdpCredentials) error {
					calls++
					want := rdpCredentials{Username: "alice", Password: "rdp-password"}
					if credentials != want {
						t.Fatalf("authentication credentials = %+v, want exact credentials", credentials)
					}

					return ErrAuthentication
				}, nil,
			)
			if calls != 1 {
				t.Fatalf("authentication calls = %d, want 1", calls)
			}
			if !errors.Is(err, ErrAuthentication) {
				t.Fatalf("authentication error = %v, want ErrAuthentication", err)
			}
			if len(forwarded) != 0 {
				t.Fatal("Client Info was forwarded after failed authentication")
			}
		})
	}
}

func TestClientInfoRejectsEveryOtherSecurityFlag(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		flag uint16
	}{
		{name: "exchange packet", flag: 0x0001},
		{name: "transport request", flag: 0x0002},
		{name: "transport response", flag: 0x0004},
		{name: "license packet", flag: 0x0080},
		{name: "reserved bit", flag: 0x0100},
		{name: "license encryption", flag: 0x0200},
		{name: "redirection packet", flag: 0x0400},
		{name: "secure checksum", flag: 0x0800},
		{name: "autodetect request", flag: 0x1000},
		{name: "autodetect response", flag: 0x2000},
		{name: "heartbeat", flag: 0x4000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mcs, info := clientInfoTestPacket(t, "alice", "rdp-password")
			// Combine the forbidden bit with SEC_ENCRYPT, every ignored bit,
			// SEC_FLAGSHI_VALID and nonzero flagsHi to catch mask bypasses.
			binary.LittleEndian.PutUint32(info[:4], 0xa5a58078|uint32(tc.flag))
			_, forwarded, err := parseClientInfo(mcs)
			requireRDPClientInfoError(t, err, rdpClientInfoSecurityFlags)
			if len(forwarded) != 0 {
				t.Fatal("unsupported security flags produced a backend packet")
			}
		})
	}
}

func TestClientInfoRejectionReasons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		username string
		password string
		mutate   func(mcs, info []byte)
		want     rdpClientInfoError
	}{
		{
			name: "MCS header", username: "alice", password: "rdp-password",
			mutate: func(mcs, _ []byte) { mcs[0] = 0x65 }, want: rdpClientInfoMCSHeader,
		},
		{
			name: "packet length", username: "alice", password: "rdp-password",
			mutate: func(mcs, _ []byte) { mcs[6]-- }, want: rdpClientInfoPacketLength,
		},
		{
			name: "missing info type", username: "alice", password: "rdp-password",
			mutate: func(_, info []byte) {
				binary.LittleEndian.PutUint32(info[:4], 0x08)
			},
			want: rdpClientInfoSecurityFlags,
		},
		{
			name: "contradictory PDU type", username: "alice", password: "rdp-password",
			mutate: func(_, info []byte) {
				binary.LittleEndian.PutUint32(info[:4], 0xc0)
			},
			want: rdpClientInfoSecurityFlags,
		},
		{
			name: "smart card PIN", username: "alice", password: "rdp-password",
			mutate: func(_, info []byte) { info[10] |= 0x04 }, want: rdpClientInfoSmartCardPIN,
		},
		{
			name: "field length", username: "alice", password: "rdp-password",
			mutate: func(_, info []byte) { info[14]++ }, want: rdpClientInfoFieldLength,
		},
		{
			name: "field terminator", username: "alice", password: "rdp-password",
			mutate: func(_, info []byte) { info[22] = 1 }, want: rdpClientInfoFieldTerminator,
		},
		{
			name: "credential encoding", username: "alice", password: "rdp-password",
			mutate: func(_, info []byte) {
				binary.LittleEndian.PutUint16(info[24:26], 0xd800)
			},
			want: rdpClientInfoCredentialEncoding,
		},
		{
			name: "credential length", username: strings.Repeat("u", 129), password: "p",
			want: rdpClientInfoCredentialLength,
		},
		{
			name: "empty username", password: "rdp-password",
			want: rdpClientInfoEmptyUsername,
		},
		{
			name: "empty password", username: "alice",
			want: rdpClientInfoEmptyPassword,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mcs, info := clientInfoTestPacket(t, tc.username, tc.password)
			if tc.mutate != nil {
				tc.mutate(mcs, info)
			}
			_, forwarded, err := parseClientInfo(mcs)
			requireRDPClientInfoError(t, err, tc.want)
			if len(forwarded) != 0 {
				t.Fatal("rejected Client Info produced a backend packet")
			}
		})
	}
}
