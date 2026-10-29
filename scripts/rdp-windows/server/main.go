// Command server runs a single, unauthenticated RDP fixture on IPv4 loopback.
// It exercises the production protocol and encoders, not gateway authorization
// or a KVM guest. It must never expose a configurable/public listening address.
package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/buildinfo"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/avc"
)

const (
	fixtureWidth    = 1024
	fixtureHeight   = 768
	frameInterval   = 500 * time.Millisecond
	minimumSession  = 30 * time.Second
	graphicsTimeout = 20 * time.Second
	f8Keysym        = 0xffc5
)

var errConfiguration = errors.New("invalid fixture configuration")

type configuration struct {
	mode, stateDir, output string
	timeout                time.Duration
}

type readiness struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	Mode    string `json:"mode"`
}

type report struct {
	Version          int            `json:"version"`
	Mode             string         `json:"mode"`
	ActualEncoder    string         `json:"actualEncoder"`
	Outcome          string         `json:"outcome"`
	Stage            string         `json:"stage"`
	Reason           string         `json:"reason"`
	StartedAt        time.Time      `json:"startedAt"`
	EndedAt          time.Time      `json:"endedAt"`
	SessionSeconds   float64        `json:"sessionSeconds"`
	FramesSent       uint64         `json:"framesSent"`
	InputTransitions uint64         `json:"inputTransitions"`
	GraphicsReady    bool           `json:"graphicsReady"`
	OutstandingACKs  uint32         `json:"outstandingAcks"`
	ACKMilliseconds  float64        `json:"ackMs"`
	Application      buildinfo.Info `json:"application"`
}

func main() { os.Exit(command(os.Args[1:])) }

func command(args []string) int {
	config, err := parseConfiguration(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration: failed")

		return 1
	}
	result := run(config)
	if err := writeJSON(filepath.Join(config.output, "server.json"), result); err != nil {
		fmt.Fprintln(os.Stderr, "report: failed")

		return 1
	}
	fmt.Printf("%s: %s (%s)\n", result.Stage, result.Outcome, result.Reason)
	if result.Outcome != "completed" {
		return 1
	}

	return 0
}

func parseConfiguration(args []string) (configuration, error) {
	var config configuration
	flags := flag.NewFlagSet("rdp-loopback-fixture", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&config.mode, "mode", "", "bitmap or h264")
	flags.StringVar(&config.stateDir, "state-dir", "", "fresh private temporary directory")
	flags.StringVar(&config.output, "output", "", "fresh artifact directory")
	flags.DurationVar(&config.timeout, "timeout", 150*time.Second, "overall fixture deadline")
	if err := flags.Parse(args); err != nil {
		return config, errConfiguration
	}
	if flags.NArg() != 0 || (config.mode != "bitmap" && config.mode != "h264") ||
		config.stateDir == "" || config.output == "" || config.timeout < 45*time.Second || config.timeout > 10*time.Minute {
		return config, errConfiguration
	}

	return config, nil
}

func run(config configuration) (result report) {
	result = report{
		Version: 1, Mode: config.mode, Outcome: "failed", Stage: "startup",
		Reason: "startup_failed", StartedAt: time.Now().UTC(), Application: buildinfo.Current(),
	}
	defer func() { result.EndedAt = time.Now().UTC() }()
	ctx, cancel := context.WithTimeout(context.Background(), config.timeout)
	defer cancel()
	if err := os.MkdirAll(config.output, 0o700); err != nil {
		return result
	}
	configTLS, certificate, err := newCertificate(time.Now())
	if err != nil {
		result.Reason = "certificate_failed"

		return result
	}
	// The fixture deliberately has no authentication: rdp.Accept ignores Client
	// Info credentials. Binding exactly IPv4 loopback is its isolation boundary.
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		result.Reason = "listen_failed"

		return result
	}
	defer listener.Close()
	stopListener := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stopListener()
	port := listener.Addr().(*net.TCPAddr).Port
	if err := publishReadiness(config, certificate, port); err != nil {
		result.Reason = "readiness_failed"

		return result
	}
	result.Stage, result.Reason = "accept", "client_not_connected"
	conn, err := listener.Accept()
	if err != nil {
		return result
	}
	_ = listener.Close() // Exactly one connection; never accept a second client.
	defer conn.Close()
	stopConnection := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopConnection()
	session, err := rdp.Accept(conn, configTLS, fixtureWidth, fixtureHeight,
		rdp.WithDiagnosticStage(func(stage string) { result.Stage = stage }))
	if err != nil {
		result.Reason = "handshake_failed"

		return result
	}
	width, height := session.Size()
	if width != fixtureWidth || height != fixtureHeight {
		result.Stage, result.Reason = "activation", "unexpected_geometry"

		return result
	}
	runSession(ctx, config.mode, session, &result)

	return result
}

func newCertificate(now time.Time) (*tls.Config, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	certificate := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: now.Add(-5 * time.Minute), NotAfter: now.Add(time.Hour),
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}, der, nil
}

func publishReadiness(config configuration, certificate []byte, port int) error {
	if port < 1 || port > 65535 {
		return errConfiguration
	}
	if err := os.MkdirAll(config.stateDir, 0o700); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(config.stateDir, "server.cer"), certificate); err != nil {
		return err
	}

	return writeJSON(filepath.Join(config.stateDir, "ready.json"), readiness{
		Address: net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), Port: port, Mode: config.mode,
	})
}

func writeNew(path string, data []byte) error {
	//nolint:gosec // Paths are fixed filenames beneath the caller's private test directory.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()

	return errors.Join(writeErr, closeErr)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	// Fresh directories are required. Never replace a stale readiness/report file.
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return os.ErrExist
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".rdp-fixture-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}

	return os.Rename(file.Name(), path)
}

type inputState struct {
	held        bool
	transitions atomic.Uint64
	refresh     atomic.Bool
	suppressed  atomic.Bool
}

func (state *inputState) apply(events []rdp.InputEvent) {
	for _, event := range events {
		if event.Kind != rdp.InputKey || event.Key != f8Keysym {
			continue
		}
		if event.Down && !state.held {
			state.transitions.Add(1)
		}
		state.held = event.Down
	}
}

func readInput(session *rdp.Session, state *inputState, done chan<- error) {
	decoder := rdp.InputDecoder{}
	for {
		packet, fast, err := session.ReadPacket()
		if err != nil {
			done <- err

			return
		}
		if !fast && (len(packet) < 18 || packet[14] != 28) {
			state.control(packet)

			continue
		}
		events, err := decoder.Decode(packet, fast)
		if err != nil {
			done <- err

			return
		}
		state.apply(events)
	}
}

func (state *inputState) control(packet []byte) {
	if len(packet) < 18 || binary.LittleEndian.Uint16(packet[2:])&15 != 7 {
		return
	}
	switch packet[14] {
	case 33: // Refresh Rect: repaint the whole fixture on the next frame.
		state.refresh.Store(true)
	case 35: // Suppress Output: honor minimization and repaint on restoration.
		if len(packet) >= 22 {
			state.suppressed.Store(packet[18] == 0)
			if packet[18] == 1 {
				state.refresh.Store(true)
			}
		}
	}
}

func runSession(ctx context.Context, mode string, session *rdp.Session, result *report) {
	started := time.Now()
	state := &inputState{}
	readErrors := make(chan error, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		readInput(session, state, readErrors)
	}()
	defer func() {
		_ = session.Close()
		<-readDone
		result.InputTransitions = state.transitions.Load()
		result.SessionSeconds = time.Since(started).Seconds()
		stats := session.GraphicsStats()
		result.OutstandingACKs = stats.OutstandingACKs
		result.ACKMilliseconds = float64(stats.ACKTurnaround) / float64(time.Millisecond)
	}()
	result.Stage, result.Reason = "graphics", "graphics_not_negotiated"
	if mode == "h264" {
		begun, err := session.BeginGraphics()
		if err != nil || !begun {
			return
		}
	}
	writer, err := newFrameWriter(mode, session)
	if err != nil {
		result.Reason = "encoder_initialization_failed"

		return
	}
	defer writer.close()
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()
	frame := image.NewRGBA(image.Rect(0, 0, fixtureWidth, fixtureHeight))
	var sequence uint64
	for {
		select {
		case <-ctx.Done():
			result.Stage, result.Reason = "session", "session_timeout"

			return
		case err := <-readErrors:
			finishOrTimeout(ctx, result, state.transitions.Load(), time.Since(started), err)

			return
		case <-ticker.C:
		}
		if mode == "h264" && !session.GraphicsReady() {
			if time.Since(started) > graphicsTimeout {
				return
			}

			continue
		}
		result.GraphicsReady = session.GraphicsReady()
		if state.suppressed.Load() || (mode == "h264" && !session.GraphicsCanSend()) {
			continue
		}
		paintFrame(frame, state.transitions.Load()%2, sequence)
		sent, err := writer.write(frame, state.refresh.Swap(false))
		if err != nil {
			finishOrTimeout(ctx, result, state.transitions.Load(), time.Since(started), err)

			return
		}
		if sent {
			result.FramesSent++
			result.ActualEncoder = writer.name()
			sequence++
		}
	}
}

func finishOrTimeout(ctx context.Context, result *report, transitions uint64, duration time.Duration, err error) {
	if ctx.Err() != nil {
		result.Stage, result.Reason = "session", "session_timeout"

		return
	}
	finishSession(result, transitions, duration, err)
}

// Completion means the server observed sufficient protocol activity and a peer
// close. Only the parent combining this with the Microsoft client's rendering,
// input, duration and intentional-disconnect checks may declare the test passed.
func finishSession(result *report, transitions uint64, duration time.Duration, err error) {
	result.Stage, result.Reason = "session", "protocol_failed"
	if stage, ok := rdp.DiagnosticStage(err); ok {
		result.Stage = stage
	}
	// ReadPacket maps the MCS Disconnect Provider Ultimatum used by Microsoft
	// clients to a diagnostic-stage-wrapped EOF; errors.Is preserves that case.
	closed := errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || platformConnectionReset(err)
	if !closed {
		return
	}
	result.Reason = "peer_closed_before_requirements"
	validMode := (result.Mode == "bitmap" && result.ActualEncoder == "bitmap" && !result.GraphicsReady) ||
		(result.Mode == "h264" && result.ActualEncoder == "software" && result.GraphicsReady)
	if duration >= minimumSession && result.FramesSent > 0 && transitions > 0 && validMode {
		result.Outcome, result.Reason = "completed", "protocol_activity_and_peer_close_observed"
	}
}

type frameWriter struct {
	session  *rdp.Session
	bitmap   *rdp.BitmapEncoder
	graphics *avc.Encoder
	retry    bool
}

func newFrameWriter(mode string, session *rdp.Session) (*frameWriter, error) {
	writer := &frameWriter{session: session}
	var err error
	if mode == "h264" {
		writer.graphics, err = avc.NewEncoderWithOptions(fixtureWidth, fixtureHeight,
			avc.Options{Threads: 1, FrameRate: 2})
	} else {
		writer.bitmap, err = rdp.NewBitmapEncoder(fixtureWidth, fixtureHeight, session.BitsPerPixel)
	}

	return writer, err
}

func (writer *frameWriter) close() {
	if writer.graphics != nil {
		writer.graphics.Close()
	}
}

func (writer *frameWriter) name() string {
	if writer.graphics != nil {
		return "software"
	}

	return "bitmap"
}

func (writer *frameWriter) write(frame *image.RGBA, force bool) (bool, error) {
	if writer.graphics != nil {
		data, err := writer.graphics.Encode(frame, force || writer.retry)
		if err != nil || len(data) == 0 {
			return false, err
		}
		sent, err := writer.session.WriteAVC420(data)
		writer.retry = !sent || err != nil

		return sent, err
	}
	sent := false
	err := writer.bitmap.WriteFrame(frame, force, func(data []byte) error {
		if err := writer.session.WriteDataPDU(2, data); err != nil {
			return err
		}
		sent = true

		return nil
	})

	return sent, err
}

// Match Client.cs Fixture. The small corner heartbeat forces ongoing traffic
// without changing any sampled quadrant centre or the F8-controlled layout.
func paintFrame(frame *image.RGBA, layout, sequence uint64) {
	colors := [...]color.RGBA{{220, 40, 40, 255}, {40, 200, 60, 255}, {40, 80, 220, 255}, {220, 190, 40, 255}}
	for quadrant := range 4 {
		index := (uint64(quadrant) + 2*(layout%2)) % 4
		x, y := (quadrant%2)*fixtureWidth/2, (quadrant/2)*fixtureHeight/2
		draw.Draw(frame, image.Rect(x, y, x+fixtureWidth/2, y+fixtureHeight/2),
			image.NewUniform(colors[index]), image.Point{}, draw.Src)
	}
	heartbeat := color.RGBA{R: uint8(sequence%2) * 255, A: 255}
	draw.Draw(frame, image.Rect(0, 0, 16, 16), image.NewUniform(heartbeat), image.Point{}, draw.Src)
}
