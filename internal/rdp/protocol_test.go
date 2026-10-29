package rdp

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"
)

// This client fixture exercises the complete TLS/MCS/activation sequence and
// packet exchange. Independent FreeRDP interoperability also validates that the
// server's wire encoding is accepted outside this package.
func TestAcceptCompleteSession(t *testing.T) {
	t.Parallel()

	serverTLS, clientTLS := protocolTestTLS(t)
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	type result struct {
		session *Session
		err     error
	}
	accepted := make(chan result, 1)
	go func() {
		// The client requests 800x600 in GCC; the default must not override it.
		session, err := Accept(server, serverTLS, 1024, 768)
		if err != nil {
			_ = server.Close()
		}
		accepted <- result{session, err}
	}()

	secured := negotiateSessionFixture(t, client, clientTLS)
	joinChannelsFixture(t, secured)
	activateSessionFixture(t, secured)
	readLogonNotificationFixture(t, secured)
	completed := <-accepted
	if completed.err != nil {
		t.Fatal(completed.err)
	}
	session := completed.session
	if session.Width != 800 || session.Height != 600 || session.BitsPerPixel != 24 {
		t.Fatal("incorrect negotiated desktop")
	}
	input, fast, err := session.ReadPacket()
	if err != nil || !fast || !bytes.Equal(input, []byte{4, 0, 0x1e}) {
		t.Fatalf("lost early input: %x, fast=%v, err=%v", input, fast, err)
	}
	written := make(chan error, 1)
	go func() { written <- session.WriteDataPDU(2, []byte{1, 0, 0, 0}) }()
	bitmap := readGlobalFixture(t, secured)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if len(bitmap) != 22 || bitmap[14] != 2 || !bytes.Equal(bitmap[18:], []byte{1, 0, 0, 0}) {
		t.Fatalf("incorrect post-activation update: %x", bitmap)
	}
	go func() {
		_, err := secured.Write([]byte{4, 4, 1, 0x1e})
		written <- err
	}()
	input, fast, err = session.ReadPacket()
	if err != nil || !fast || !bytes.Equal(input, []byte{4, 1, 0x1e}) {
		t.Fatalf("lost active input: %x, fast=%v, err=%v", input, fast, err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if _, _, err := session.ReadPacket(); err == nil {
		t.Fatal("client disconnect did not terminate ReadPacket")
	}
}

func negotiateSessionFixture(t *testing.T, client net.Conn, clientTLS *tls.Config) *tls.Conn {
	t.Helper()
	request := []byte{14, 0xe0, 0, 0, 0, 0, 0, 1, 0, 8, 0, 1, 0, 0, 0}
	if err := writeTPKT(client, request); err != nil {
		t.Fatal(err)
	}
	response, fast, err := readTransport(client)
	if err != nil || fast || len(response) != 15 || response[7] != 2 || response[11] != 1 {
		t.Fatalf("TLS negotiation failed: %x, %v", response, err)
	}
	secured := tls.Client(client, clientTLS)
	if err := secured.Handshake(); err != nil {
		t.Fatal(err)
	}

	return secured
}

type relativeInputFixture struct {
	clientVersion, serverVersion uint32
	offered                      bool
}

func joinChannelsFixture(t *testing.T, secured net.Conn, input ...relativeInputFixture) {
	t.Helper()
	settings := relativeInputFixture{clientVersion: 0x80004, serverVersion: 0x80004}
	if len(input) != 0 {
		settings = input[0]
	}
	writeMCSFixture(t, secured, connectInitialFixture(settings.clientVersion))
	response := readMCSFixture(t, secured)
	if len(response) < 2 || response[0] != 0x7f || response[1] != 0x66 {
		t.Fatalf("expected MCS Connect Response, got %x", response)
	}
	checkConnectResponseEncoding(t, response)
	core := bytes.Index(response, []byte{1, 12, 12, 0})
	if core < 0 || core+8 > len(response) || binary.LittleEndian.Uint32(response[core+4:]) != settings.serverVersion {
		t.Fatalf("incorrect server core version: %x", response)
	}
	writeMCSFixture(t, secured, []byte{4, 1, 0, 1, 0})
	writeMCSFixture(t, secured, []byte{0x28})
	if response := readMCSFixture(t, secured); !bytes.Equal(response, []byte{0x2e, 0, 0, 0}) {
		t.Fatalf("expected Attach User Confirm, got %x", response)
	}
	for _, channel := range []uint16{1001, 1003} {
		join := []byte{0x38, 0, 0, byte(channel >> 8), byte(channel)}
		writeMCSFixture(t, secured, join)
		if response := readMCSFixture(t, secured); len(response) != 8 || response[0] != 0x3e || response[1] != 0 {
			t.Fatalf("expected Channel Join Confirm, got %x", response)
		}
	}
	info := make([]byte, 32)
	info[0], info[8] = 0x40, 0x10
	writeGlobalFixture(t, secured, info)
	license := readGlobalFixture(t, secured)
	if len(license) != 20 || license[0] != 0x80 || license[4] != 0xff || license[8] != 7 {
		t.Fatalf("expected valid-client license, got %x", license)
	}
	demand := readGlobalFixture(t, secured)
	if kind, err := shareType(demand); err != nil || kind != 1 {
		t.Fatalf("expected Demand Active, got %x, %v", demand, err)
	}
	checkDemandActiveBitmap(t, demand)
	const description = "devbox-kvm\x00"
	if len(demand) < 14+len(description)+4 || int(binary.LittleEndian.Uint16(demand[10:12])) != len(description) {
		t.Fatalf("invalid Demand Active source descriptor length: %x", demand)
	}
	if got := string(demand[14 : 14+len(description)]); got != description {
		t.Errorf("Demand Active source descriptor = %q, want %q", got, description)
	}
	if got := int(binary.LittleEndian.Uint16(demand[:2])); got != len(demand) {
		t.Errorf("Demand Active length = %d, want %d", got, len(demand))
	}
	inputOffset := bytes.Index(demand, []byte{13, 0, 88, 0})
	if inputOffset < 0 || inputOffset+6 > len(demand) {
		t.Fatal("missing input capability")
	}
	flags := binary.LittleEndian.Uint16(demand[inputOffset+4:])
	if (flags&0x80 != 0) != settings.offered || flags&^uint16(0x80) != 0x31 {
		t.Fatalf("input capability flags = %#x; relative offered=%v", flags, settings.offered)
	}
}

func activateSessionFixture(t *testing.T, secured net.Conn, inputFlags ...uint16) {
	t.Helper()
	const baseSize = 16 + 6 + 4 + 28
	size := baseSize
	if len(inputFlags) != 0 {
		size += 88
	}
	confirm := make([]byte, size)
	put16(confirm, 0, len(confirm))
	put16(confirm, 2, 0x13)
	put16(confirm, 4, clientUser)
	binary.LittleEndian.PutUint32(confirm[6:10], shareID)
	put16(confirm, 12, 6)
	put16(confirm, 14, 32)
	put16(confirm, 22, 1)
	put16(confirm, 26, 2)
	put16(confirm, 28, 28)
	put16(confirm, 30, 24)
	if len(inputFlags) != 0 {
		capability := confirm[baseSize:]
		put16(capability, 0, 13)
		put16(capability, 2, len(capability))
		put16(capability, 4, int(inputFlags[0]))
		put16(confirm, 14, 32+len(capability))
		put16(confirm, 22, 2)
	}
	writeGlobalFixture(t, secured, confirm)
	for _, expected := range []byte{0x1f, 0x14} {
		if response := readGlobalFixture(t, secured); len(response) < 18 || response[14] != expected {
			t.Fatalf("expected activation data %#x, got %x", expected, response)
		}
	}
	// RDP permits input after Confirm Active, even before Font List.
	if _, err := secured.Write([]byte{4, 4, 0, 0x1e}); err != nil {
		t.Fatal(err)
	}
	writeGlobalFixture(t, secured, shareDataFixture(0x1f, []byte{1, 0, 0xea, 3}))
	writeGlobalFixture(t, secured, shareDataFixture(0x14, []byte{4, 0, 0, 0, 0, 0, 0, 0}))
	writeGlobalFixture(t, secured, shareDataFixture(0x14, []byte{1, 0, 0, 0, 0, 0, 0, 0}))
	if grant := readGlobalFixture(t, secured); len(grant) != 26 || grant[14] != 0x14 || grant[18] != 2 {
		t.Fatalf("expected Granted Control, got %x", grant)
	}
	writeGlobalFixture(t, secured, shareDataFixture(0x27, []byte{0, 0, 0, 0, 3, 0, 0x32, 0}))
	if font := readGlobalFixture(t, secured); len(font) != 26 || font[14] != 0x28 {
		t.Fatalf("expected Font Map, got %x", font)
	}
}

func protocolTestTLS(tb testing.TB) (*tls.Config, *tls.Config) {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{"localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		tb.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		tb.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)

	return &tls.Config{
		MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "localhost"}
}

func connectInitialFixture(versions ...uint32) []byte {
	core := make([]byte, 132)
	put16(core, 0, 0xc001)
	put16(core, 2, len(core))
	version := uint32(0x80004)
	if len(versions) != 0 {
		version = versions[0]
	}
	binary.LittleEndian.PutUint32(core[4:8], version)
	put16(core, 8, 800)
	put16(core, 10, 600)
	gcc := appendPERLength([]byte{0, 5, 0, 0x14, 0x7c, 0, 1}, len(core)+14)
	gcc = append(gcc, 0, 8, 0, 16, 0, 1, 0xc0, 0, 'D', 'u', 'c', 'a')
	gcc = append(appendPERLength(gcc, len(core)), core...)
	body := []byte{4, 1, 1, 4, 1, 1, 1, 1, 0xff, 0x30, 0, 0x30, 0, 0x30, 0}
	body = append(body, ber(4, gcc)...)

	return append([]byte{0x7f}, ber(0x65, body)...)
}

func writeMCSFixture(t *testing.T, conn net.Conn, p []byte) {
	t.Helper()
	if err := writeTPKT(conn, append([]byte{2, 0xf0, 0x80}, p...)); err != nil {
		t.Fatal(err)
	}
}

func readMCSFixture(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	p, err := readMCS(conn)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func writeGlobalFixture(t *testing.T, conn net.Conn, data []byte) {
	t.Helper()
	p := appendPERLength([]byte{0x64, 0, 0, 3, 0xeb, 0x70}, len(data))
	writeMCSFixture(t, conn, append(p, data...))
}

func readGlobalFixture(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	p := readMCSFixture(t, conn)
	if len(p) < 7 || p[0] != 0x68 || p[3] != 3 || p[4] != 0xeb {
		t.Fatalf("expected global-channel indication, got %x", p)
	}
	data, err := takePER(p[6:])
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func shareDataFixture(kind byte, payload []byte) []byte {
	p := make([]byte, 18+len(payload))
	put16(p, 0, len(p))
	put16(p, 2, 0x17)
	put16(p, 4, clientUser)
	binary.LittleEndian.PutUint32(p[6:10], shareID)
	p[11], p[14] = 1, kind
	put16(p, 12, len(p)-6)
	copy(p[18:], payload)

	return p
}

func TestTransport(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		wire []byte
		want []byte
		fast bool
	}{
		{"tpkt", []byte{3, 0, 0, 8, 2, 0xf0, 0x80, 0x28}, []byte{2, 0xf0, 0x80, 0x28}, false},
		{"fast short length", []byte{4, 4, 0, 0x1e}, []byte{4, 0, 0x1e}, true},
		{"fast long length", []byte{4, 0x80, 5, 0, 0x1e}, []byte{4, 0, 0x1e}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, fast, err := readTransport(bytes.NewReader(tc.wire))
			if err != nil || fast != tc.fast || !bytes.Equal(got, tc.want) {
				t.Fatalf("readTransport = %x, %v, %v", got, fast, err)
			}
			for i := range tc.wire {
				if _, _, err := readTransport(bytes.NewReader(tc.wire[:i])); err == nil {
					t.Fatalf("accepted truncation at %d", i)
				}
			}
		})
	}
	for _, wire := range [][]byte{{3, 1, 0, 8}, {3, 0, 0, 3}, {0x84, 4, 0, 1}, {1, 3, 0}, {4, 1}, {4, 0x80, 2}} {
		if _, _, err := readTransport(bytes.NewReader(wire)); err == nil {
			t.Errorf("accepted invalid transport %x", wire)
		}
	}
}

func TestAcceptRequiresTLS(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := Accept(server, &tls.Config{MinVersion: tls.VersionTLS12}, 800, 600)
		done <- err
	}()
	// A client requesting only CredSSP cannot silently downgrade to TLS.
	request := []byte{14, 0xe0, 0, 0, 0, 0, 0, 1, 0, 8, 0, 2, 0, 0, 0}
	if err := writeTPKT(client, request); err != nil {
		t.Fatal(err)
	}
	response, fast, err := readTransport(client)
	if err != nil || fast || len(response) != 15 || response[7] != 3 || response[11] != 1 {
		t.Fatalf("expected SSL_REQUIRED negotiation failure, got %x, %v", response, err)
	}
	if err := <-done; err == nil {
		t.Fatal("accepted client without TLS")
	}
}

func TestClientInfoBounds(t *testing.T) {
	t.Parallel()

	p := make([]byte, 32)
	p[0] = 0x40
	p[8] = 0x10 // Unicode, five empty UTF-16 strings.
	if err := validateClientInfo(p); err != nil {
		t.Fatal(err)
	}
	for i := range p {
		if err := validateClientInfo(p[:i]); err == nil {
			t.Errorf("accepted Client Info truncated to %d", i)
		}
	}
	put16(p, 12, 65534)
	if err := validateClientInfo(p); err == nil {
		t.Fatal("accepted oversized domain")
	}
	put16(p, 12, 1)
	if err := validateClientInfo(p); err == nil {
		t.Fatal("accepted partial UTF-16 character")
	}
}

func TestMCSDataBounds(t *testing.T) {
	t.Parallel()

	valid := []byte{0x64, 0, 0, 3, 0xeb, 0x70, 3, 1, 2, 3}
	channel, payload, err := parseSendData(valid)
	if err != nil || channel != 1003 || !bytes.Equal(payload, []byte{1, 2, 3}) {
		t.Fatalf("parse: %d, %x, %v", channel, payload, err)
	}
	for i := range valid {
		if _, _, err := parseSendData(valid[:i]); err == nil {
			t.Errorf("accepted MCS truncation at %d", i)
		}
	}
	for _, offset := range []int{0, 1, 5, 6} {
		bad := bytes.Clone(valid)
		bad[offset] ^= 1
		if _, _, err := parseSendData(bad); err == nil {
			t.Errorf("accepted altered header byte %d", offset)
		}
	}
}

type readerConn struct {
	net.Conn
	io.Reader
}

func (c readerConn) Read(p []byte) (int, error) { return c.Reader.Read(p) }

func TestReadPacketValidatesStaticChannels(t *testing.T) {
	t.Parallel()

	global := make([]byte, 18)
	put16(global, 0, len(global))
	put16(global, 2, 0x17)
	for _, tc := range []struct {
		name    string
		channel uint16
		joined  bool
		wantErr bool
	}{
		{"joined static channel", 1004, true, false},
		{"unjoined static channel", 1004, false, true},
		{"unknown static channel", 65535, false, true},
		{"user channel is not a static channel", 1001, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var wire bytes.Buffer
			for _, packet := range []struct {
				channel uint16
				data    []byte
			}{{tc.channel, []byte{0}}, {globalChannel, global}} {
				mcs := []byte{2, 0xf0, 0x80, 0x64, 0, 0, 0, 0, 0x70, byte(len(packet.data))}
				binary.BigEndian.PutUint16(mcs[6:8], packet.channel)
				if err := writeTPKT(&wire, append(mcs, packet.data...)); err != nil {
					t.Fatal(err)
				}
			}
			s := &Session{conn: readerConn{Reader: &wire}, joined: map[uint16]bool{tc.channel: tc.joined}}
			data, fast, err := s.ReadPacket()
			if (err != nil) != tc.wantErr {
				t.Fatalf("ReadPacket error = %v, want error = %v", err, tc.wantErr)
			}
			if err == nil && (fast || !bytes.Equal(data, global)) {
				t.Fatalf("ReadPacket returned %x, fast=%v", data, fast)
			}
		})
	}
}

func TestConfirmActiveBounds(t *testing.T) {
	t.Parallel()

	// Confirm Active with one bitmap capability and a 6-byte source name.
	p := make([]byte, 16+6+4+28)
	put16(p, 0, len(p))
	put16(p, 2, 0x13)
	binary.LittleEndian.PutUint32(p[6:10], shareID)
	put16(p, 12, 6)
	put16(p, 14, 32)
	put16(p, 22, 1)
	put16(p, 26, 2)
	put16(p, 28, 28)
	put16(p, 30, 24)
	s := &Session{BitsPerPixel: 24}
	if err := s.confirmActive(p); err != nil {
		t.Fatal(err)
	}
	if s.BitsPerPixel != 24 {
		t.Fatal("changed selected bitmap depth")
	}
	for i := range p {
		if err := s.confirmActive(p[:i]); err == nil {
			t.Errorf("accepted Confirm Active truncation at %d", i)
		}
	}
	put16(p, 28, 3)
	if err := s.confirmActive(p); err == nil {
		t.Fatal("accepted short capability")
	}
	put16(p, 28, 28)
	put16(p, 30, 8)
	if err := s.confirmActive(p); err != nil || s.BitsPerPixel != 24 {
		t.Fatalf("client preference changed the selected session depth: %v", err)
	}
}

func TestConcurrentWritersPreservePackets(t *testing.T) {
	t.Parallel()

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if err := a.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := b.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	s := &Session{conn: a}
	var writers sync.WaitGroup
	writeErrors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		writers.Add(1)
		go func(value byte) {
			defer writers.Done()
			writeErrors <- s.WriteDataPDU(2, bytes.Repeat([]byte{value}, 1024))
		}(byte(i))
	}
	seen := make(map[byte]bool)
	for i := 0; i < 8; i++ {
		mcs, err := readMCS(b)
		if err != nil {
			t.Fatal(err)
		}
		if mcs[0] != 0x68 {
			t.Fatalf("wrong MCS indication: %x", mcs[:6])
		}
		data, err := takePER(mcs[6:])
		if err != nil {
			t.Fatal(err)
		}
		if len(data) != 1042 || data[14] != 2 {
			t.Fatal("corrupted Share Data packet")
		}
		for _, value := range data[18:] {
			if value != data[18] {
				t.Fatal("interleaved writer payload")
			}
		}
		seen[data[18]] = true
	}
	writers.Wait()
	close(writeErrors)
	for err := range writeErrors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 8 {
		t.Fatal("missing writer payload")
	}
	if err := s.WriteDataPDU(2, make([]byte, maxUserData)); err == nil {
		t.Fatal("accepted oversized PDU")
	}
}

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > 2 {
		p = p[:2]
	}

	return w.Buffer.Write(p)
}

func TestTPKTHandlesShortWrites(t *testing.T) {
	t.Parallel()

	var w shortWriter
	if err := writeTPKT(&w, []byte{2, 0xf0, 0x80, 0x28}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.Bytes(), []byte{3, 0, 0, 8, 2, 0xf0, 0x80, 0x28}) {
		t.Fatalf("lost bytes: %x", w.Bytes())
	}
}

func TestPERRejectsUnsupportedFragmentedLengths(t *testing.T) {
	t.Parallel()
	// 10xxxxxx starts the two-byte length form. 11xxxxxx instead marks
	// fragmentation, which the RDP MCS size restriction avoids.
	valid := append([]byte{0xbf, 0xff}, make([]byte, 16383)...)
	if data, err := takePER(valid); err != nil || len(data) != 16383 {
		t.Fatalf("maximum nonfragmented PER payload: length=%d error=%v", len(data), err)
	}
	for _, prefix := range []byte{0xc0, 0xc1, 0xff} {
		packet := append([]byte{prefix, 0}, make([]byte, int(prefix&0x7f)<<8)...)
		if _, err := takePER(packet); err == nil {
			t.Fatalf("accepted fragmented PER prefix %02x as a two-byte length", prefix)
		}
	}
}

func TestSendDataRequestSizeLimit(t *testing.T) {
	t.Parallel()
	for _, size := range []int{16375, 16376} {
		packet := appendPERLength([]byte{0x64, 0, 0, 3, 0xeb, 0x70}, size)
		packet = append(packet, make([]byte, size)...)
		_, _, err := parseSendData(packet)
		if (err == nil) != (size == 16375) {
			t.Fatalf("MCS size=%d, error=%v", len(packet), err)
		}
	}
}

func FuzzProtocolParsers(f *testing.F) {
	f.Add([]byte{0x64, 0, 0, 3, 0xeb, 0x70, 1, 0})
	f.Add([]byte{0x7f, 0x65, 0})
	f.Fuzz(func(t *testing.T, p []byte) {
		_, _, _ = readTransport(bytes.NewReader(p))
		_, _, _ = parseSendData(p)
		_, _ = parseConnectInitial(p)
		_ = validateClientInfo(p)
		_ = (&Session{}).confirmActive(p)
		_, _, _ = takeBER(p, 4)
		_, _ = takePER(p)
		_, _ = shareType(p)
	})
}

var _ io.Writer = (*shortWriter)(nil)

func TestAcceptRelativePointerNegotiation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		version uint32
		enable  bool
		flags   uint16
		want    bool
	}{
		{name: "default off", version: 0x80011, flags: 0xb1},
		{name: "legacy version", version: 0x80004, enable: true, flags: 0xb1},
		{name: "previous version", version: 0x80010, enable: true, flags: 0xb1},
		{name: "negotiated", version: 0x80011, enable: true, flags: 0xb1, want: true},
		{name: "client declined", version: 0x80011, enable: true, flags: 0x31},
		{name: "future client", version: 0x80012, enable: true, flags: 0xb1, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			serverTLS, clientTLS := protocolTestTLS(t)
			server, client := net.Pipe()
			t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
			if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			type result struct {
				session *Session
				err     error
			}
			accepted := make(chan result, 1)
			go func() {
				var options []AcceptOption
				if test.enable {
					options = append(options, WithRelativePointer())
				}
				session, err := Accept(server, serverTLS, 800, 600, options...)
				if err != nil {
					_ = server.Close()
				}
				accepted <- result{session, err}
			}()
			secured := negotiateSessionFixture(t, client, clientTLS)
			settings := relativeInputFixture{clientVersion: test.version, serverVersion: 0x80004}
			if test.enable && test.version >= 0x80011 {
				settings.offered, settings.serverVersion = true, 0x80011
			}
			joinChannelsFixture(t, secured, settings)
			activateSessionFixture(t, secured, test.flags)
			readLogonNotificationFixture(t, secured)
			completed := <-accepted
			if completed.err != nil {
				t.Fatal(completed.err)
			}
			if got := completed.session.RelativePointerSupported(); got != test.want {
				t.Fatalf("negotiated relative input = %v, want %v", got, test.want)
			}
		})
	}
}

func relativeConfirmFixture(flags uint16) []byte {
	p := make([]byte, 20+28+88)
	put16(p, 0, len(p))
	put16(p, 2, 0x13)
	binary.LittleEndian.PutUint32(p[6:], shareID)
	put16(p, 14, len(p)-16)
	put16(p, 16, 2)
	put16(p, 20, 2)
	put16(p, 22, 28)
	put16(p, 24, 24)
	put16(p, 48, 13)
	put16(p, 50, 88)
	put16(p, 52, int(flags))

	return p
}

func TestConfirmRelativePointerCapability(t *testing.T) {
	t.Parallel()
	s := &Session{relativePointerOffered: true}
	valid := relativeConfirmFixture(0xb1)
	if err := s.confirmActive(valid); err != nil || !s.RelativePointerSupported() {
		t.Fatalf("relative negotiation failed: %v", err)
	}
	if err := s.confirmActiveDepth(valid, false); err != nil {
		t.Fatalf("same capability during reactivation: %v", err)
	}
	if err := s.confirmActiveDepth(relativeConfirmFixture(0x31), false); err == nil || !s.RelativePointerSupported() {
		t.Fatal("reactivation changed the negotiated input mode")
	}
	short := relativeConfirmFixture(0xb1)
	short = short[:len(short)-1]
	put16(short, 0, len(short))
	put16(short, 14, len(short)-16)
	put16(short, 50, 87)
	duplicate := append(append([]byte(nil), valid...), valid[48:]...)
	put16(duplicate, 0, len(duplicate))
	put16(duplicate, 14, len(duplicate)-16)
	put16(duplicate, 16, 3)
	for _, packet := range [][]byte{short, duplicate} {
		session := &Session{relativePointerOffered: true}
		if err := session.confirmActive(packet); err == nil || session.RelativePointerSupported() {
			t.Fatal("malformed input capability changed negotiation")
		}
	}
}
