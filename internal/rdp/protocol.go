// Package rdp implements the TLS-protected framebuffer subset of MS-RDPBCGR.
// Protocol structures follow Microsoft's Open Specifications, MS-RDPBCGR
// sections 2.2.1 (connection sequence) and 2.2.8 (transport and input).
//
//nolint:err113 // Protocol errors include the failed wire structure for diagnostic logging.
package rdp

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	clientUser             = 1001
	serverUser             = 1002
	globalChannel          = 1003
	shareID                = 0x103ea
	maxUserData            = 32767
	relativePointerVersion = 0x00080011
)

// Session is an activated RDP display connection. One goroutine may read while
// other goroutines write. Writes are serialized; reads must have one owner.
type Session struct {
	// Width and Height retain the initial negotiated size. Use Size for live geometry.
	Width, Height          int
	BitsPerPixel           int
	conn                   net.Conn
	writeMu                sync.Mutex
	writeTimeout           time.Duration
	pending                []queuedPacket
	joined                 map[uint16]bool
	geometry               atomic.Uint64
	display                displayState
	dynamic                *dynamicState
	graphics               *graphicsState
	audio                  *audioState
	pointer                pointerState
	relativePointerOffered bool
	relativePointer        bool
}

type queuedPacket struct {
	data []byte
	fast bool
}

type acceptOptions struct {
	relativePointer bool
	diagnosticStage func(string)
}

// AcceptOption configures optional capabilities advertised during activation.
type AcceptOption func(*acceptOptions)

// WithRelativePointer enables negotiation of signed relative mouse input.
// Callers must have an input sink that can consume relative pointer events.
func WithRelativePointer() AcceptOption {
	return func(options *acceptOptions) { options.relativePointer = true }
}

// WithDiagnosticStage observes fixed, non-sensitive backend handshake stages.
// It is intended for bounded operational telemetry. The callback is invoked
// synchronously by Accept and never receives packet or error data.
func WithDiagnosticStage(observe func(string)) AcceptOption {
	return func(options *acceptOptions) { options.diagnosticStage = observe }
}

// RelativePointerSupported reports whether both peers negotiated relative input.
// The result remains fixed for the activated session, including display resizing.
func (s *Session) RelativePointerSupported() bool { return s.relativePointer }

// Accept negotiates TLS and the RDP connection sequence. Credentials in Client
// Info are intentionally ignored; callers must authenticate and authorize the
// transport before calling Accept. NLA is not supported. The caller owns conn,
// including closing it when Accept returns an error.
func Accept(conn net.Conn, config *tls.Config, width, height int, options ...AcceptOption) (*Session, error) {
	if config == nil || width < 1 || width > 8192 || height < 1 || height > 8192 {
		return nil, errors.New("rdp: invalid TLS configuration or desktop size")
	}
	var opts acceptOptions
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	observeDiagnosticStage(opts.diagnosticStage, diagnosticStageNegotiation)
	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return nil, err
	}
	packet, fast, err := readTransport(conn)
	if err != nil {
		return nil, fmt.Errorf("rdp negotiation: %w", err)
	}
	if fast || len(packet) < 7 || int(packet[0])+1 != len(packet) || packet[1] != 0xe0 {
		return nil, errors.New("rdp: invalid X.224 connection request")
	}
	requested := uint32(0)
	neg := packet[7:]
	if bytes.HasPrefix(neg, []byte("Cookie:")) || bytes.HasPrefix(neg, []byte("RoutingToken:")) {
		i := bytes.Index(neg, []byte("\r\n"))
		if i < 0 {
			return nil, errors.New("rdp: unterminated routing cookie")
		}
		neg = neg[i+2:]
	}
	if len(neg) >= 8 && neg[0] == 1 && binary.LittleEndian.Uint16(neg[2:4]) == 8 {
		requested = binary.LittleEndian.Uint32(neg[4:8])
	}
	// X.224 CC followed by RDP_NEG_RSP (or SSL_REQUIRED_BY_SERVER).
	response := []byte{14, 0xd0, packet[4], packet[5], 0, 0, 0, 2, 0, 8, 0, 1, 0, 0, 0}
	if requested&1 == 0 {
		response[7] = 3
	}
	if err := writeTPKT(conn, response); err != nil {
		return nil, err
	}
	if requested&1 == 0 {
		return nil, errors.New("rdp: client must support TLS security")
	}
	observeDiagnosticStage(opts.diagnosticStage, diagnosticStageTLS)
	secured := tls.Server(conn, config)
	if err := secured.Handshake(); err != nil {
		return nil, fmt.Errorf("rdp TLS: %w", err)
	}
	s := &Session{Width: width, Height: height, BitsPerPixel: 24, conn: secured}
	observeDiagnosticStage(opts.diagnosticStage, diagnosticStageMCSConnect)
	mcs, err := readMCS(secured)
	if err != nil {
		return nil, err
	}
	client, err := parseConnectInitialDetails(mcs)
	if err != nil {
		return nil, err
	}
	version := s.offerRelativePointer(opts.relativePointer, client.version)
	if err := s.writeMCS(connectResponse(requested, client.channels, version)); err != nil {
		return nil, err
	}
	s.dynamic = &dynamicState{channel: client.dynamicChannel}
	s.graphics = &graphicsState{supported: client.graphics}
	if size, valid := normalizeDesktopSize(client.width, client.height); valid {
		s.Width, s.Height = size.Width, size.Height
	}
	s.storeSize(s.Width, s.Height)
	s.audio = &audioState{channel: client.audioChannel}
	if err := s.activate(client.channels, opts.diagnosticStage); err != nil {
		return nil, fmt.Errorf("rdp activation: %w", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}

	s.writeTimeout = 10 * time.Second

	return s, nil
}

func (s *Session) offerRelativePointer(enabled bool, clientVersion uint32) uint32 {
	// MS-RDPBCGR 2.2.7.1.6 requires ignoring the client's relative input bit
	// when its core protocol version predates RDP 10.12.
	s.relativePointerOffered = enabled && clientVersion >= relativePointerVersion
	if s.relativePointerOffered {
		return relativePointerVersion
	}

	return 0x80004
}

type activation struct {
	session                                        *Session
	channels                                       int
	erected, attached, info, confirmed, controlled bool
	reactivation                                   bool
	observe                                        func(string)
}

func (a *activation) stage(value string) {
	if a.observe != nil {
		a.observe(value)
	}
}

func (a *activation) nextStage() string {
	switch {
	case !a.erected:
		return diagnosticStageActivationErectDomain
	case !a.attached:
		return diagnosticStageActivationAttachUser
	case !a.info:
		return diagnosticStageActivationChannelJoin
	case !a.confirmed:
		return diagnosticStageActivationConfirmActive
	default:
		return diagnosticStageActivationFinalize
	}
}

func (s *Session) activate(channels int, observe func(string)) error {
	s.joined = make(map[uint16]bool)
	state := activation{session: s, channels: channels, observe: observe}
	for packets := 0; packets < 256; packets++ {
		state.stage(state.nextStage())
		packet, fast, err := readTransport(s.conn)
		if err != nil {
			return err
		}
		if fast {
			if !state.confirmed {
				return errors.New("input before Confirm Active")
			}
			if err := s.queueInput(packet, true); err != nil {
				return err
			}

			continue
		}
		mcs, err := unwrapX224(packet)
		if err != nil {
			return err
		}
		done, err := state.handle(mcs)
		if err != nil || done {
			return err
		}
	}

	return errors.New("too many activation packets")
}

func (a *activation) handle(mcs []byte) (bool, error) {
	if len(mcs) == 0 {
		return false, errors.New("empty MCS PDU")
	}
	s := a.session
	switch mcs[0] >> 2 {
	case 1: // Erect Domain Request; two PER integers.
		if a.erected || a.attached || !bytes.Equal(mcs, []byte{4, 1, 0, 1, 0}) {
			return false, errors.New("unexpected Erect Domain")
		}
		a.erected = true
	case 10: // Attach User Request.
		if !a.erected || a.attached || len(mcs) != 1 {
			return false, errors.New("unexpected Attach User")
		}
		a.attached = true

		return false, s.writeMCS([]byte{0x2e, 0, 0, 0})
	case 14: // Channel Join Request.
		if !a.attached || a.info || len(mcs) != 5 || binary.BigEndian.Uint16(mcs[1:3]) != 0 {
			return false, errors.New("invalid Channel Join")
		}
		channel := binary.BigEndian.Uint16(mcs[3:5])
		if channel != clientUser && (channel < globalChannel || int(channel) >= globalChannel+1+a.channels) {
			return false, errors.New("unknown MCS channel")
		}
		s.joined[channel] = true
		reply := append([]byte{0x3e, 0}, mcs[1:]...)
		reply = append(reply, mcs[3:5]...)

		return false, s.writeMCS(reply)
	case 25:
		if !a.attached || !s.joined[clientUser] || !s.joined[globalChannel] {
			return false, errors.New("MCS data before channel join")
		}
		channel, data, err := parseSendData(mcs)
		if err != nil {
			return false, err
		}
		if !s.joined[channel] {
			return false, errors.New("data on unjoined MCS channel")
		}
		if channel != globalChannel {
			return false, nil
		}

		return a.globalData(data)
	case 8:
		return false, io.EOF
	default:
		return false, fmt.Errorf("unexpected MCS PDU %#x", mcs[0])
	}

	return false, nil
}

func (a *activation) globalData(data []byte) (bool, error) {
	s := a.session
	if !a.info {
		a.stage(diagnosticStageActivationClientInfo)
		if err := validateClientInfo(data); err != nil {
			return false, err
		}
		if s.audio != nil {
			// Remote-console audio requests playback on the host, which this
			// redirected playback device intentionally does not provide.
			s.audio.disabled = binary.LittleEndian.Uint32(data[8:12])&(0x80000|0x2000) != 0
		}
		a.info = true
		license := []byte{0x80, 0, 0, 0, 0xff, 3, 16, 0, 7, 0, 0, 0, 2, 0, 0, 0, 4, 0, 0, 0}
		a.stage(diagnosticStageActivationLicense)
		if err := s.writeGlobal(license); err != nil {
			return false, err
		}

		a.stage(diagnosticStageActivationDemandActive)

		return false, s.writeGlobal(s.demandActive())
	}
	kind, err := shareType(data)
	if err != nil {
		return false, err
	}
	if kind == 3 {
		a.stage(diagnosticStageActivationConfirmActive)
		if a.confirmed {
			return false, errors.New("duplicate Confirm Active")
		}
		if err := s.confirmActiveDepth(data, !a.reactivation); err != nil {
			return false, err
		}
		a.confirmed = true
		if err := s.WriteDataPDU(0x1f, []byte{1, 0, 0xe9, 3}); err != nil {
			return false, err
		}

		return false, s.WriteDataPDU(0x14, []byte{4, 0, 0, 0, 0, 0, 0, 0})
	}
	if !a.confirmed {
		return false, errors.New("unexpected activation Share Data")
	}
	a.stage(diagnosticStageActivationFinalize)
	if kind != 7 || len(data) < 18 || data[15] != 0 {
		return false, errors.New("unexpected activation Share Data")
	}

	return a.finalize(data)
}

func (a *activation) finalize(data []byte) (bool, error) {
	s := a.session
	switch data[14] {
	case 0x1f: // Synchronize.
		if len(data) != 22 {
			return false, errors.New("invalid Synchronize")
		}
	case 0x14: // Control.
		if len(data) != 26 {
			return false, errors.New("invalid Control")
		}
		switch binary.LittleEndian.Uint16(data[18:20]) {
		case 1:
			a.controlled = true

			return false, s.WriteDataPDU(0x14, []byte{2, 0, 0xe9, 3, 0xea, 3, 0, 0})
		case 4: // Cooperate.
		default:
			return false, errors.New("unsupported activation control action")
		}
	case 0x27: // Font List; the final list activates the connection.
		if !a.controlled || len(data) != 26 {
			return false, errors.New("invalid Font List")
		}
		if binary.LittleEndian.Uint16(data[22:24])&2 != 0 {
			if err := s.WriteDataPDU(0x28, []byte{0, 0, 0, 0, 3, 0, 4, 0}); err != nil {
				return false, err
			}
			if !a.reactivation {
				// MS-RDPBCGR 3.3.5.10.1: notify the client that logon has
				// completed before exposing the session to frame writers.
				// Authentication remains the caller's responsibility; the
				// plain notification contains no credentials or user identity.
				// A display reactivation is not a new logon.
				notify := make([]byte, 4+576)
				binary.LittleEndian.PutUint32(notify, 2) // INFOTYPE_LOGON_PLAINNOTIFY
				if err := s.WriteDataPDU(0x26, notify); err != nil {
					return false, err
				}
			}

			return true, nil
		}
	case 0x2b: // Optional persistent bitmap cache key list.
	case 0x1c: // Input is permitted after Confirm Active.

		return false, s.queueInput(data, false)
	default:
		return false, fmt.Errorf("unexpected activation data PDU %#x", data[14])
	}

	return false, nil
}

func (s *Session) queueInput(data []byte, fast bool) error {
	if len(s.pending) >= 16 {
		return errors.New("excessive input before activation")
	}
	s.pending = append(s.pending, queuedPacket{data: data, fast: fast})

	return nil
}

// ReadPacket returns global-channel Share Control data, or fast-path input as
// the original first header byte followed by the event payload (without the
// transport length bytes). Negotiated graphics and audio traffic is handled internally.
func (s *Session) ReadPacket() ([]byte, bool, error) {
	for {
		if len(s.pending) > 0 && s.DisplayReady() {
			p := s.pending[0]
			s.pending = s.pending[1:]

			return p.data, p.fast, nil
		}
		packet, fast, err := readTransport(s.conn)
		if err != nil {
			return packet, fast, withDiagnosticStage(diagnosticStageSessionRead, err)
		}
		if fast {
			if s.DisplayReady() {
				return packet, true, nil
			}
			if err := s.queueInput(packet, true); err != nil {
				return nil, false, err
			}

			continue
		}
		mcs, err := unwrapX224(packet)
		if err != nil {
			return nil, false, withDiagnosticStage(diagnosticStageSessionRead, err)
		}
		if len(mcs) > 0 && mcs[0]>>2 == 8 {
			return nil, false, withDiagnosticStage(diagnosticStageSessionRead, io.EOF)
		}
		channel, data, err := parseSendData(mcs)
		if err != nil {
			return nil, false, withDiagnosticStage(diagnosticStageSessionRead, err)
		}
		if channel != globalChannel {
			if channel < 1004 || !s.joined[channel] {
				return nil, false, withDiagnosticStage(
					diagnosticStageSessionRead, errors.New("rdp: data on unknown static channel"),
				)
			}

			if err := s.readAudioChannel(channel, data); err != nil {
				return nil, false, withDiagnosticStage(diagnosticStageAudio, err)
			}
			if err := s.readDynamicChannel(channel, data); err != nil {
				return nil, false, withDiagnosticStage(diagnosticStageDynamicChannels, err)
			}

			continue
		}
		if handled, err := s.readReactivation(data); handled || err != nil {
			if err != nil {
				return nil, false, withDiagnosticStage(diagnosticStageDisplayControl, err)
			}

			continue
		}
		kind, err := shareType(data)
		if err != nil {
			return nil, false, withDiagnosticStage(diagnosticStageSessionRead, err)
		}
		if kind == 6 {
			return nil, false, withDiagnosticStage(diagnosticStageSessionRead, io.EOF)
		}

		return data, false, nil
	}
}

// WriteDataPDU writes an uncompressed Share Data PDU on the global channel.
func (s *Session) WriteDataPDU(pduType byte, payload []byte) error {
	if len(payload) > maxUserData-18 {
		return errors.New("rdp: Share Data exceeds PER limit")
	}
	data := make([]byte, 18+len(payload))
	put16(data, 0, len(data))
	put16(data, 2, 0x17)
	put16(data, 4, serverUser)
	binary.LittleEndian.PutUint32(data[6:10], shareID)
	data[11] = 1
	// Match the Windows server wire examples in MS-RDPBCGR: their
	// uncompressedLength includes the complete Share Data header and body.
	put16(data, 12, len(data))
	data[14] = pduType
	copy(data[18:], payload)

	return s.writeGlobal(data)
}

// Close releases the connection and unblocks any reader or writer.
func (s *Session) Close() error { return s.conn.Close() }

func (s *Session) writeGlobal(data []byte) error {
	return s.writeChannel(globalChannel, data)
}

func (s *Session) writeChannel(channel uint16, data []byte) error {
	if len(data) > maxUserData {
		return errors.New("rdp: MCS data exceeds PER limit")
	}
	packet := []byte{0x68, 0, 1, byte(channel >> 8), byte(channel), 0x70}
	packet = appendPERLength(packet, len(data))

	return s.writeMCS(append(packet, data...))
}

func (s *Session) writeMCS(data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.writeTimeout != 0 {
		if err := s.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
			return err
		}
	}
	err := writeTPKT(s.conn, append([]byte{2, 0xf0, 0x80}, data...))
	if s.writeTimeout != 0 {
		if clearErr := s.conn.SetWriteDeadline(time.Time{}); err == nil {
			err = clearErr
		}
	}

	return err
}

// writeWire writes one or more complete TPKT packets as a contiguous batch.
// Callers must not reuse data until this synchronous call returns.
func (s *Session) writeWire(data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.writeTimeout != 0 {
		if err := s.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
			return err
		}
	}
	err := writeFull(s.conn, data)
	if s.writeTimeout != 0 {
		if clearErr := s.conn.SetWriteDeadline(time.Time{}); err == nil {
			err = clearErr
		}
	}

	return err
}

func channelTPKTSize(payloadLength int) int {
	perLength := 1
	if payloadLength >= 128 {
		perLength = 2
	}

	return 4 + 3 + 6 + perLength + payloadLength
}

// appendChannelTPKTHeader reserves the complete packet and fills its headers.
// The caller fills the final payloadLength bytes before the batch is written.
func appendChannelTPKTHeader(dst []byte, channel uint16, header []byte, payloadLength int) ([]byte, error) {
	payloadLength += len(header)
	if payloadLength > maxUserData {
		return dst, errors.New("rdp: MCS data exceeds PER limit")
	}
	packetLength := channelTPKTSize(payloadLength)
	start := len(dst)
	if cap(dst)-start < packetLength {
		capacity := max(cap(dst)*2, start+packetLength)
		grown := make([]byte, start, capacity)
		copy(grown, dst)
		dst = grown
	}
	dst = dst[:start+packetLength]
	packet := dst[start:]
	packet[0], packet[1] = 3, 0
	binary.BigEndian.PutUint16(packet[2:4], uint16(packetLength))
	packet[4], packet[5], packet[6] = 2, 0xf0, 0x80
	packet[7], packet[8], packet[9] = 0x68, 0, 1
	packet[10], packet[11], packet[12] = byte(channel>>8), byte(channel), 0x70
	offset := 13
	if payloadLength < 128 {
		packet[offset] = byte(payloadLength)
		offset++
	} else {
		packet[offset], packet[offset+1] = byte(payloadLength>>8)|0x80, byte(payloadLength)
		offset += 2
	}
	copy(packet[offset:], header)

	return dst, nil
}

func readTransport(r io.Reader) ([]byte, bool, error) {
	var prefix [2]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return nil, false, err
	}
	if prefix[0] == 3 {
		var size [2]byte
		if prefix[1] != 0 {
			return nil, false, errors.New("rdp: invalid TPKT version/reserved byte")
		}
		if _, err := io.ReadFull(r, size[:]); err != nil {
			return nil, false, err
		}
		n := int(binary.BigEndian.Uint16(size[:]))
		if n < 7 {
			return nil, false, errors.New("rdp: short TPKT")
		}
		packet := make([]byte, n-4)
		_, err := io.ReadFull(r, packet)

		return packet, false, err
	}
	if prefix[0]&0xc3 != 0 {
		return nil, true, errors.New("rdp: unsupported fast-path action or encryption")
	}
	n, headerSize := int(prefix[1]), 2
	if n&0x80 != 0 {
		var last [1]byte
		if _, err := io.ReadFull(r, last[:]); err != nil {
			return nil, true, err
		}
		n, headerSize = (n&0x7f)<<8|int(last[0]), 3
	}
	if n < headerSize+1 {
		return nil, true, errors.New("rdp: short fast-path packet")
	}
	packet := make([]byte, 1+n-headerSize)
	packet[0] = prefix[0]
	_, err := io.ReadFull(r, packet[1:])

	return packet, true, err
}

func writeTPKT(w io.Writer, payload []byte) error {
	if len(payload) > 65531 {
		return errors.New("rdp: TPKT too large")
	}
	packet := make([]byte, 4+len(payload))
	packet[0] = 3
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[4:], payload)

	return writeFull(w, packet)
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}

	return nil
}

func readMCS(r io.Reader) ([]byte, error) {
	p, fast, err := readTransport(r)
	if err != nil {
		return nil, err
	}
	if fast {
		return nil, errors.New("rdp: fast-path input before activation")
	}

	return unwrapX224(p)
}

func unwrapX224(p []byte) ([]byte, error) {
	if len(p) < 4 || p[0] != 2 || p[1] != 0xf0 || p[2] != 0x80 {
		return nil, errors.New("rdp: invalid X.224 data")
	}

	return p[3:], nil
}

func parseSendData(p []byte) (uint16, []byte, error) {
	if len(p) < 7 || p[0] != 0x64 || binary.BigEndian.Uint16(p[1:3]) != 0 || p[5] != 0x70 {
		return 0, nil, errors.New("rdp: invalid MCS Send Data Request")
	}
	data, err := takePER(p[6:])
	if err != nil {
		return 0, nil, err
	}

	return binary.BigEndian.Uint16(p[3:5]), data, nil
}

func takePER(p []byte) ([]byte, error) {
	if len(p) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	n, off := int(p[0]), 1
	if n&0x80 != 0 {
		if len(p) < 2 {
			return nil, io.ErrUnexpectedEOF
		}
		n, off = (n&0x7f)<<8|int(p[1]), 2
	}
	if n != len(p)-off {
		return nil, errors.New("rdp: invalid PER payload length")
	}

	return p[off:], nil
}

func appendPERLength(p []byte, n int) []byte {
	if n < 128 {
		return append(p, byte(n))
	}

	return append(p, byte(n>>8)|0x80, byte(n))
}

func takeBER(p []byte, tag byte) (value, rest []byte, err error) {
	if len(p) < 2 || p[0] != tag {
		return nil, nil, errors.New("rdp: unexpected BER tag")
	}
	n, off := int(p[1]), 2
	if n&0x80 != 0 {
		count := n & 0x7f
		if count == 0 || count > 2 || len(p) < off+count {
			return nil, nil, errors.New("rdp: invalid BER length")
		}
		n = 0
		for _, b := range p[off : off+count] {
			n = n<<8 | int(b)
		}
		off += count
	}
	if n > len(p)-off {
		return nil, nil, io.ErrUnexpectedEOF
	}

	return p[off : off+n], p[off+n:], nil
}

type clientSettings struct {
	version        uint32
	width, height  int
	channels       int
	dynamicChannel uint16
	audioChannel   uint16
	graphics       bool
}

func (c *clientSettings) staticChannel(name string, channel uint16) error {
	var dest *uint16
	switch name {
	case "rdpsnd":
		dest = &c.audioChannel
	case "drdynvc":
		dest = &c.dynamicChannel
	default:
		return nil
	}
	if *dest != 0 {
		return fmt.Errorf("rdp: duplicate %s channel", name)
	}
	*dest = channel

	return nil
}

func parseConnectInitial(p []byte) (int, error) {
	settings, err := parseConnectInitialDetails(p)

	return settings.channels, err
}

func parseConnectInitialDetails(p []byte) (clientSettings, error) {
	var settings clientSettings
	if len(p) < 3 || p[0] != 0x7f {
		return settings, errors.New("rdp: missing MCS Connect Initial")
	}
	body, rest, err := takeBER(p[1:], 0x65)
	if err != nil || len(rest) != 0 {
		return settings, errors.New("rdp: invalid MCS Connect Initial")
	}
	for _, tag := range []byte{4, 4, 1, 0x30, 0x30, 0x30} {
		_, body, err = takeBER(body, tag)
		if err != nil {
			return settings, err
		}
	}
	gcc, rest, err := takeBER(body, 4)
	if err != nil || len(rest) != 0 {
		return settings, errors.New("rdp: invalid GCC envelope")
	}
	// ConferenceCreateRequest carries a non-standard H.221 key, "Duca".
	// Require the key's PER choice/length prefix and validate every following
	// user-data block; do not scan arbitrary data for individual block types.
	key := bytes.Index(gcc, []byte{0xc0, 0, 'D', 'u', 'c', 'a'})
	if key < 0 {
		return settings, errors.New("rdp: missing GCC client key")
	}
	blocks, err := takePER(gcc[key+6:])
	if err != nil {
		return settings, err
	}
	core, channels, network := false, 0, false
	for len(blocks) > 0 {
		if len(blocks) < 4 {
			return settings, errors.New("rdp: truncated GCC block")
		}
		typ, size := binary.LittleEndian.Uint16(blocks), int(binary.LittleEndian.Uint16(blocks[2:4]))
		if size < 4 || size > len(blocks) {
			return settings, errors.New("rdp: invalid GCC block size")
		}
		payload := blocks[4:size]
		switch typ {
		case 0xc001:
			if core || len(payload) < 128 {
				return settings, errors.New("rdp: invalid GCC core data")
			}
			if len(payload) >= 212 && binary.LittleEndian.Uint32(payload[208:212]) != 1 {
				return settings, errors.New("rdp: client core TLS negotiation mismatch")
			}
			if len(payload) >= 142 {
				settings.graphics = binary.LittleEndian.Uint16(payload[140:142])&0x100 != 0
			}
			settings.version = binary.LittleEndian.Uint32(payload[:4])
			settings.width = int(binary.LittleEndian.Uint16(payload[4:6]))
			settings.height = int(binary.LittleEndian.Uint16(payload[6:8]))
			core = true
		case 0xc003:
			if network || len(payload) < 4 {
				return settings, errors.New("rdp: invalid GCC network data")
			}
			network = true
			count := binary.LittleEndian.Uint32(payload)
			if count > 31 || len(payload) != 4+12*int(count) {
				return settings, errors.New("rdp: invalid static channel count")
			}
			channels = int(count)
			for index := 0; index < channels; index++ {
				name := bytes.TrimRight(payload[4+12*index:12+12*index], "\x00")
				if err := settings.staticChannel(string(name), uint16(1004+index)); err != nil {
					return settings, err
				}
			}
		}
		blocks = blocks[size:]
	}
	if !core {
		return settings, errors.New("rdp: missing client core data")
	}

	settings.channels = channels

	return settings, nil
}

func ber(tag byte, body []byte) []byte {
	p := []byte{tag}
	switch {
	case len(body) < 128:
		p = append(p, byte(len(body)))
	case len(body) < 256:
		p = append(p, 0x81, byte(len(body)))
	default:
		p = append(p, 0x82, byte(len(body)>>8), byte(len(body)))
	}

	return append(p, body...)
}

func connectResponse(requested uint32, channels int, version uint32) []byte {
	core := make([]byte, 12)
	put16(core, 0, 0x0c01)
	put16(core, 2, len(core))
	binary.LittleEndian.PutUint32(core[4:8], version)
	binary.LittleEndian.PutUint32(core[8:12], requested)
	security := []byte{2, 12, 12, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	network := make([]byte, 8+2*channels+2*(channels%2))
	put16(network, 0, 0x0c03)
	put16(network, 2, len(network))
	put16(network, 4, globalChannel)
	put16(network, 6, channels)
	for i := 0; i < channels; i++ {
		put16(network, 8+2*i, 1004+i)
	}
	userData := make([]byte, 0, len(core)+len(security)+len(network))
	userData = append(userData, core...)
	userData = append(userData, security...)
	userData = append(userData, network...)
	conference := []byte{0x14, 0x76, 0x0a, 1, 1, 0, 1, 0xc0, 0, 'M', 'c', 'D', 'n'}
	conference = append(appendPERLength(conference, len(userData)), userData...)
	gcc := append(appendPERLength([]byte{0, 5, 0, 0x14, 0x7c, 0, 1}, len(conference)), conference...)
	// BER INTEGERs are signed: maxMCSPDUsize (65528) needs a leading zero.
	params := []byte{2, 1, 34, 2, 1, 2, 2, 1, 0, 2, 1, 1, 2, 1, 0, 2, 1, 1, 2, 3, 0, 0xff, 0xf8, 2, 1, 2}
	body := []byte{0x0a, 1, 0, 2, 1, 0}
	body = append(body, ber(0x30, params)...)
	body = append(body, ber(4, gcc)...)

	return append([]byte{0x7f}, ber(0x66, body)...)
}

func validateClientInfo(p []byte) error {
	if len(p) < 22 || binary.LittleEndian.Uint32(p[:4]) != 0x40 {
		return errors.New("rdp: expected TLS Client Info")
	}
	// The five variable strings are terminated even when their lengths are 0.
	terminator := 1
	if binary.LittleEndian.Uint32(p[8:12])&0x10 != 0 {
		terminator = 2
	}
	off := 22
	for i := 0; i < 5; i++ {
		n := int(binary.LittleEndian.Uint16(p[12+2*i : 14+2*i]))
		if n%terminator != 0 || n+terminator > len(p)-off {
			return errors.New("rdp: truncated Client Info string")
		}
		off += n
		for _, b := range p[off : off+terminator] {
			if b != 0 {
				return errors.New("rdp: unterminated Client Info string")
			}
		}
		off += terminator
	}

	return nil
}

func shareType(p []byte) (uint16, error) {
	if len(p) < 6 || int(binary.LittleEndian.Uint16(p[:2])) != len(p) ||
		binary.LittleEndian.Uint16(p[2:4])&0xfff0 != 0x10 {
		return 0, errors.New("rdp: invalid Share Control length")
	}

	return binary.LittleEndian.Uint16(p[2:4]) & 15, nil
}

func (s *Session) demandActive() []byte {
	general := make([]byte, 20)
	put16(general, 0, 1)
	put16(general, 2, 3)
	put16(general, 4, 0x200)
	general[18], general[19] = 1, 1
	bitmap := make([]byte, 24)
	put16(bitmap, 0, 24)
	put16(bitmap, 2, 1)
	put16(bitmap, 4, 1)
	put16(bitmap, 6, 1)
	width, height := s.Size()
	put16(bitmap, 8, width)
	put16(bitmap, 10, height)
	put16(bitmap, 14, 1)
	put16(bitmap, 16, 1)
	put16(bitmap, 20, 1) // multipleRectangleSupport is mandatory (MS-RDPBCGR 2.2.7.1.2).
	// The Order capability is mandatory even for a bitmap-only server. Leave
	// orderSupport empty so no unsupported primary drawing orders are offered.
	order := make([]byte, 84)
	put16(order, 20, 1)        // desktopSaveXGranularity
	put16(order, 22, 20)       // desktopSaveYGranularity
	put16(order, 26, 1)        // ORD_LEVEL_1_ORDERS
	put16(order, 30, 2)        // NEGOTIATEORDERSUPPORT
	channel := make([]byte, 8) // VCCAPS_NO_COMPR
	binary.LittleEndian.PutUint32(channel[4:], 1600)
	input := make([]byte, 84)
	put16(input, 0, 0x0031)
	if s.relativePointerOffered {
		put16(input, 0, 0x00b1)
	}
	binary.LittleEndian.PutUint32(input[8:12], 4)
	binary.LittleEndian.PutUint32(input[16:20], 12)
	sets := [...]struct {
		kind int
		data []byte
	}{
		{1, general},
		{2, bitmap},
		{3, order},
		{8, []byte{1, 0, pointerCacheEntries, 0, pointerCacheEntries, 0}},
		{13, input},
		{14, []byte{1, 0, 0, 0}},
		{9, []byte{0xea, 3, 0, 0}},
		{20, channel},
	}
	capabilities := make([]byte, 0, 280)
	for _, c := range sets {
		header := make([]byte, 4)
		put16(header, 0, c.kind)
		put16(header, 2, len(c.data)+4)
		capabilities = append(append(capabilities, header...), c.data...)
	}
	description := []byte("devbox-kvm\x00")
	p := make([]byte, 14+len(description)+4+len(capabilities)+4)
	put16(p, 0, len(p))
	put16(p, 2, 0x11)
	put16(p, 4, serverUser)
	binary.LittleEndian.PutUint32(p[6:10], shareID)
	put16(p, 10, len(description))
	put16(p, 12, len(capabilities)+4)
	copy(p[14:], description)
	off := 14 + len(description)
	put16(p, off, len(sets))
	copy(p[off+4:], capabilities)

	return p
}

func (s *Session) confirmActive(p []byte) error {
	return s.confirmActiveDepth(p, true)
}

func (s *Session) confirmActiveDepth(p []byte, update bool) error {
	if len(p) < 20 || binary.LittleEndian.Uint32(p[6:10]) != shareID {
		return errors.New("rdp: invalid Confirm Active")
	}
	off := 16 + int(binary.LittleEndian.Uint16(p[12:14]))
	length := int(binary.LittleEndian.Uint16(p[14:16]))
	if off > len(p)-4 || length < 4 || length != len(p)-off {
		return errors.New("rdp: invalid Confirm Active capabilities")
	}
	count := int(binary.LittleEndian.Uint16(p[off : off+2]))
	off += 4
	bitmap := false
	pointerSeen := false
	pointer := pointerCapabilities{}
	inputSeen, relativePointer := false, false
	for i := 0; i < count; i++ {
		if off > len(p)-4 {
			return errors.New("rdp: missing capability header")
		}
		kind, n := binary.LittleEndian.Uint16(p[off:off+2]), int(binary.LittleEndian.Uint16(p[off+2:off+4]))
		if n < 4 || n > len(p)-off {
			return errors.New("rdp: invalid capability size")
		}
		if kind == 2 {
			if bitmap || n < 28 {
				return errors.New("rdp: invalid bitmap capability")
			}
			bitmap = true
			bpp := int(binary.LittleEndian.Uint16(p[off+4 : off+6]))
			if bpp != 16 && bpp != 24 && bpp != 32 {
				return fmt.Errorf("rdp: unsupported client color depth %d", bpp)
			}
			if update {
				s.BitsPerPixel = bpp
			} else if s.BitsPerPixel != bpp {
				return errors.New("rdp: color depth changed during reactivation")
			}
		}
		if kind == 8 {
			if pointerSeen || n < 8 || n == 9 {
				return errors.New("rdp: invalid pointer capability")
			}
			pointerSeen = true
			pointer.colors = int(binary.LittleEndian.Uint16(p[off+6 : off+8]))
			if n >= 10 {
				pointer.alpha = int(binary.LittleEndian.Uint16(p[off+8 : off+10]))
			}
		}
		if kind == 13 {
			if inputSeen || n < 88 {
				return errors.New("rdp: invalid input capability")
			}
			inputSeen = true
			relativePointer = s.relativePointerOffered && binary.LittleEndian.Uint16(p[off+4:off+6])&0x0080 != 0
		}
		off += n
	}
	if !bitmap || off != len(p) {
		return errors.New("rdp: incomplete capability data")
	}
	if err := s.confirmRelativePointer(relativePointer, update); err != nil {
		return err
	}
	s.pointer.mu.Lock()
	s.pointer.caps = pointer
	s.pointer.reset()
	s.pointer.mu.Unlock()

	return nil
}

func (s *Session) confirmRelativePointer(enabled, update bool) error {
	if !update && s.relativePointer != enabled {
		return errors.New("rdp: relative input capability changed during reactivation")
	}
	if update {
		s.relativePointer = enabled
	}

	return nil
}

func put16(p []byte, off, value int) { binary.LittleEndian.PutUint16(p[off:off+2], uint16(value)) }
