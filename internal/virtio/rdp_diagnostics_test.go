package virtio

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

var (
	errRDPDiagnosticReader = errors.New("concrete reader failure")
	errRDPDiagnosticWriter = errors.New("concrete writer failure")
)

func TestRDPBackendDiagnosticsPreserveOriginatingError(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		readErr   error
		writerErr error
		want      error
	}{
		{
			name: "concrete reader beats sibling writer close", readErr: errRDPDiagnosticReader,
			writerErr: net.ErrClosed, want: errRDPDiagnosticReader,
		},
		{
			name: "writer caused reader EOF", readErr: io.EOF,
			writerErr: errRDPDiagnosticWriter, want: errRDPDiagnosticWriter,
		},
		{
			name: "writer caused closed reader", readErr: net.ErrClosed,
			writerErr: errRDPDiagnosticWriter, want: errRDPDiagnosticWriter,
		},
		{
			name: "reader close without writer failure", readErr: io.EOF,
			want: io.EOF,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := rdpOriginatingError(test.readErr, test.writerErr); !errors.Is(got, test.want) {
				t.Fatalf("originating error = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRDPBackendDiagnosticsRetainEarlyActivationEOF(t *testing.T) {
	t.Parallel()
	collector := telemetry.New()
	display := newRDPTestDisplayWithConfig(t, RDPConfig{Telemetry: collector})
	conn := dialRDPTestDisplay(t, display)
	negotiateRDPTestDisplay(t, conn, 1)
	secured := tls.Client(conn, rdpTestClientTLS(t, display))
	if err := secured.Handshake(); err != nil {
		t.Fatal(err)
	}
	writeRDPDiagnosticMCS(t, secured, rdpDiagnosticConnectInitial())
	response := readRDPDiagnosticMCS(t, secured)
	if len(response) < 2 || response[0] != 0x7f || response[1] != 0x66 {
		t.Fatalf("expected MCS Connect Response, got %x", response)
	}
	if err := secured.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	waitRDPClientCount(t, display, 0)
	events := collector.Snapshot().RDPSessions
	if len(events) != 1 {
		t.Fatalf("terminal RDP events = %+v, want one", events)
	}
	event := events[0]
	if event.Stage != "activation_erect_domain" || event.Outcome != "closed" || event.DurationMs < 0 {
		t.Fatalf("early activation event = %+v", event)
	}
}

func rdpDiagnosticConnectInitial() []byte {
	core := make([]byte, 132)
	binary.LittleEndian.PutUint16(core, 0xc001)
	binary.LittleEndian.PutUint16(core[2:], uint16(len(core)))
	binary.LittleEndian.PutUint32(core[4:], 0x80004)
	binary.LittleEndian.PutUint16(core[8:], 800)
	binary.LittleEndian.PutUint16(core[10:], 600)
	gcc := rdpDiagnosticPERLength([]byte{0, 5, 0, 0x14, 0x7c, 0, 1}, len(core)+14)
	gcc = append(gcc, 0, 8, 0, 16, 0, 1, 0xc0, 0, 'D', 'u', 'c', 'a')
	gcc = append(rdpDiagnosticPERLength(gcc, len(core)), core...)
	body := []byte{4, 1, 1, 4, 1, 1, 1, 1, 0xff, 0x30, 0, 0x30, 0, 0x30, 0}
	body = append(body, rdpDiagnosticBER(4, gcc)...)

	return append([]byte{0x7f}, rdpDiagnosticBER(0x65, body)...)
}

func writeRDPDiagnosticMCS(t *testing.T, conn io.Writer, data []byte) {
	t.Helper()
	payload := append([]byte{2, 0xf0, 0x80}, data...)
	packet := make([]byte, 4+len(payload))
	packet[0] = 3
	binary.BigEndian.PutUint16(packet[2:], uint16(len(packet)))
	copy(packet[4:], payload)
	writeRDPTest(t, conn, packet)
}

func readRDPDiagnosticMCS(t *testing.T, conn io.Reader) []byte {
	t.Helper()
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		t.Fatal(err)
	}
	size := int(binary.BigEndian.Uint16(header[2:]))
	if header[0] != 3 || header[1] != 0 || size < 7 {
		t.Fatalf("invalid TPKT header: %x", header)
	}
	payload := make([]byte, size-4)
	if _, err := io.ReadFull(conn, payload); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(payload, []byte{2, 0xf0, 0x80}) {
		t.Fatalf("invalid X.224 data: %x", payload)
	}

	return payload[3:]
}

func rdpDiagnosticPERLength(data []byte, size int) []byte {
	if size < 128 {
		return append(data, byte(size))
	}

	return append(data, byte(size>>8)|0x80, byte(size))
}

func rdpDiagnosticBER(tag byte, body []byte) []byte {
	data := []byte{tag}
	switch {
	case len(body) < 128:
		data = append(data, byte(len(body)))
	case len(body) < 256:
		data = append(data, 0x81, byte(len(body)))
	default:
		data = append(data, 0x82, byte(len(body)>>8), byte(len(body)))
	}

	return append(data, body...)
}
