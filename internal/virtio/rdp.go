package virtio

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"image"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/audio"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/avc"
	rdpdamage "github.com/define42/DevBox-Gateway-kvm/internal/rdp/damage"
	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
	"golang.org/x/sys/unix"
)

const rdpResizeDelay = 150 * time.Millisecond

var (
	errRDPEncoder         = errors.New("rdp: H.264 encoder must be software, nvenc, or auto and requires H.264")
	errRDPListener        = errors.New("rdp: an inherited abstract Unix listener is required")
	errRDPGatewayIdentity = errors.New("rdp: a valid gateway process identity is required")
	errRDPGatewayParent   = errors.New("rdp: gateway parent identity changed")
	errRDPPeerIdentity    = errors.New("rdp: connection peer is not the gateway")
)

// RDPConfig selects the gateway listener, identity, server certificate, H.264
// AVC420 graphics, and audio playback.
type RDPConfig struct {
	// Listener must be an inherited Linux abstract Unix listener. The display
	// owns it on success; the caller retains ownership on failure.
	Listener    net.Listener
	GatewayPID  int
	GatewayUID  uint32
	TLS         *tls.Config
	H264        bool
	H264Encoder string // Empty selects the software encoder.
	Profile     string // office (30 FPS) or smooth (60 FPS); empty selects smooth.
	Audio       bool
	H264Threads int // Slice request; zero chooses a bounded automatic count.
	GuestCPUs   int // Guest vCPUs considered when bounding the encoder slice request.
	Stats       bool
	Telemetry   *telemetry.Collector // Optional process-owned performance counters.

	// getParentPID supports same-process transport tests. It is unexported so
	// callers outside this package cannot override the live parent check.
	getParentPID func() int
}

// RDPDisplay exports the guest console to one RDP viewer at a time.
// It accepts only the authenticated parent gateway over an inherited abstract
// Unix listener. Viewer authentication and authorization belong to the gateway;
// the backend protocol does not implement account authentication or NLA.
type RDPDisplay struct {
	*framebuffer
	listener       net.Listener
	tls            *tls.Config
	h264           bool
	h264Backend    avc.Backend
	h264Slices     int
	frameRate      int
	stats          bool
	telemetry      *telemetry.Collector
	audio          *audio.Hub
	gatewayPID     int32
	gatewayUID     uint32
	getParentPID   func() int
	connMu         sync.Mutex
	conn           net.Conn // Includes an in-progress handshake; guarded by connMu.
	wg             sync.WaitGroup
	once           sync.Once
	readyOnce      sync.Once
	ready          chan struct{}
	resizeMu       sync.Mutex
	resize         func(int, int) error
	viewer         *rdp.Session
	cursorMu       sync.Mutex
	cursor         rdpCursorState
	motions        []rdpPointerMotion
	pointerInputMu sync.Mutex
}

// NewRDPDisplayWithConfig enables AVC420 for capable clients when H264 is set.
// Other clients retain bitmap updates. The caller must supply an inherited
// abstract Unix listener and the expected parent gateway process identity.
func NewRDPDisplayWithConfig(options RDPConfig) (*RDPDisplay, error) {
	frameRate, err := rdp.ProfileFrameRate(options.Profile)
	if err != nil {
		return nil, err
	}
	backend := avc.Backend(options.H264Encoder)
	switch backend {
	case "", avc.BackendSoftware, avc.BackendNVENC, avc.BackendAuto:
	default:
		return nil, errRDPEncoder
	}
	if backend != "" && !options.H264 {
		return nil, errRDPEncoder
	}
	if backend == "" {
		backend = avc.BackendSoftware
	}
	if options.H264Threads < 0 || options.H264Threads > avc.MaxThreads ||
		(options.H264Threads != 0 && !options.H264) {
		return nil, errRDPThreads
	}
	listener, ok := options.Listener.(*net.UnixListener)
	if !ok || listener == nil || !isAbstractUnixAddress(listener.Addr()) {
		return nil, errRDPListener
	}
	if options.GatewayPID <= 0 || int64(options.GatewayPID) > int64(1<<31-1) {
		return nil, errRDPGatewayIdentity
	}
	getParentPID := options.getParentPID
	if getParentPID == nil {
		getParentPID = os.Getppid
	}
	if getParentPID() != options.GatewayPID {
		return nil, errRDPGatewayParent
	}
	slices := rdpEncoderSlices(runtime.GOMAXPROCS(0), options.GuestCPUs, options.H264Threads)
	if options.H264 {
		// Fail before opening the listener if the H.264 encoder cannot initialize.
		encoder, err := avc.NewEncoderWithOptions(1024, 768, avc.Options{
			Backend: backend, Threads: slices, FrameRate: frameRate,
		})
		if err != nil {
			return nil, err
		}
		log.Printf("rdp: H.264 encoder=%s requested=%s max_fps=%d", encoder.Backend(), backend, frameRate)
		if reason := encoder.FallbackReason(); reason != nil {
			log.Printf("rdp: H.264 using software fallback: %v", reason)
		}
		encoder.Close()
	}
	config := options.TLS
	if config == nil {
		certificate, err := rdpCertificate()
		if err != nil {
			return nil, err
		}

		config = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	} else {
		config = config.Clone()
		config.MinVersion = max(config.MinVersion, tls.VersionTLS12)
	}

	display := &RDPDisplay{
		framebuffer: newFramebuffer(), listener: listener, tls: config, ready: make(chan struct{}),
		h264: options.H264, stats: options.Stats,
		h264Backend: backend, h264Slices: slices, frameRate: frameRate,
		telemetry: options.Telemetry, gatewayPID: int32(options.GatewayPID), gatewayUID: options.GatewayUID,
		getParentPID: getParentPID,
	}
	if options.Audio {
		display.audio = audio.NewHub()
	}
	if options.H264 {
		display.linearInterval = time.Second / time.Duration(frameRate)
	}
	display.wg.Add(1)
	go display.acceptLoop()

	return display, nil
}

func isAbstractUnixAddress(addr net.Addr) bool {
	unixAddr, ok := addr.(*net.UnixAddr)

	return ok && unixAddr.Net == "unix" && len(unixAddr.Name) > 1 && strings.HasPrefix(unixAddr.Name, "@")
}

func (d *RDPDisplay) Addr() string { return d.listener.Addr().String() }

// WaitReady waits for the first activated RDP display session. A transport
// connection or an incomplete protocol handshake does not release the wait.
// Cancellation and display shutdown take precedence over an already observed
// ready session.
func (d *RDPDisplay) WaitReady(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-d.done:
		return net.ErrClosed
	default:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.done:
		return net.ErrClosed
	case <-d.ready:
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-d.done:
			return net.ErrClosed
		default:
			return nil
		}
	}
}

// SetResizeHandler attaches the guest GPU. The connected viewer controls its resolution.
func (d *RDPDisplay) SetResizeHandler(resize func(int, int) error) {
	d.resizeMu.Lock()
	defer d.resizeMu.Unlock()
	d.resize = resize
	if d.viewer != nil {
		d.resizeGuestLocked(d.viewer)
	}
}

func (d *RDPDisplay) addResizeViewer(session *rdp.Session) {
	d.resizeMu.Lock()
	defer d.resizeMu.Unlock()
	d.viewer = session
	d.resizeGuestLocked(session)
}

func (d *RDPDisplay) removeResizeViewer(session *rdp.Session) {
	d.forgetPointerMotions(session)
	d.resizeMu.Lock()
	defer d.resizeMu.Unlock()
	if d.viewer == session {
		d.viewer = nil
	}
}

func (d *RDPDisplay) resizeGuest(session *rdp.Session) {
	d.resizeMu.Lock()
	defer d.resizeMu.Unlock()
	if d.viewer == session {
		d.resizeGuestLocked(session)
	}
}

// resizeMu orders GPU mode requests against viewer disconnects.
// The callback must not call back into viewer management.
func (d *RDPDisplay) resizeGuestLocked(session *rdp.Session) {
	if d.resize != nil {
		width, height := session.Size()
		if err := d.resize(width, height); err != nil {
			log.Printf("rdp: guest display resize to %dx%d: %v", width, height, err)
		}
	}
}

// WritePCM receives borrowed, clocked PCM from the virtual sound card.
func (d *RDPDisplay) WritePCM(pcm []byte) {
	if d.audio != nil {
		d.audio.WritePCM(pcm)
	}
}

func (d *RDPDisplay) Close() error {
	var err error
	d.once.Do(func() {
		d.shutdown()
		if d.audio != nil {
			d.audio.Close()
		}
		if closeErr := d.listener.Close(); !errors.Is(closeErr, net.ErrClosed) {
			err = closeErr
		}
		d.connMu.Lock()
		if d.conn != nil {
			_ = d.conn.Close()
		}
		d.connMu.Unlock()
		d.wg.Wait()
	})

	return err
}

func (d *RDPDisplay) acceptLoop() {
	defer d.wg.Done()
	for {
		conn, err := d.listener.Accept()
		if err != nil {
			return
		}
		if err := d.authorizeGateway(conn); err != nil {
			_ = conn.Close()
			if errors.Is(err, errRDPGatewayParent) {
				_ = d.listener.Close()
				d.shutdown()

				return
			}

			continue
		}

		d.connMu.Lock()
		select {
		case <-d.done:
			d.connMu.Unlock()
			_ = conn.Close()

			return
		default:
		}
		if d.conn != nil {
			d.connMu.Unlock()
			_ = conn.Close()

			continue
		}
		d.conn = conn
		d.wg.Add(1)
		d.connMu.Unlock()
		go d.handleConn(conn)
	}
}

func (d *RDPDisplay) authorizeGateway(conn net.Conn) error {
	if d.getParentPID == nil || d.getParentPID() != int(d.gatewayPID) {
		return errRDPGatewayParent
	}
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return errRDPPeerIdentity
	}
	credentials, err := rdpPeerCredentials(unixConn)
	if err != nil {
		return errors.Join(errRDPPeerIdentity, err)
	}
	if credentials.Pid != d.gatewayPID || credentials.Uid != d.gatewayUID {
		return errRDPPeerIdentity
	}

	return nil
}

func rdpPeerCredentials(conn *net.UnixConn) (*unix.Ucred, error) {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var credentials *unix.Ucred
	var optionErr error
	if err := rawConn.Control(func(fd uintptr) {
		credentials, optionErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, err
	}
	if optionErr != nil {
		return nil, optionErr
	}
	if credentials == nil {
		return nil, errRDPPeerIdentity
	}

	return credentials, nil
}

func (d *RDPDisplay) handleConn(conn net.Conn) {
	defer d.wg.Done()
	defer func() {
		_ = conn.Close()
		d.connMu.Lock()
		d.conn = nil
		d.connMu.Unlock()
	}()

	if err := d.serveConn(conn); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		log.Printf("rdp: client %s: %v", conn.RemoteAddr(), err)
	}
}

func (d *RDPDisplay) serveConn(conn net.Conn) (serveErr error) {
	session, err := rdp.Accept(conn, d.tls, 1024, 768)
	if err != nil {
		return err
	}
	defer session.Close()
	withAudio := false
	if d.audio != nil {
		withAudio, err = session.EnableAudio()
		if err != nil {
			return err
		}
	}
	if _, err := session.BeginDisplayControl(); err != nil {
		return err
	}

	if d.h264 {
		if _, err := session.BeginGraphics(); err != nil {
			return err
		}
	}
	d.addResizeViewer(session)
	defer d.removeResizeViewer(session)

	// Software cursors remain in the image until the GPU supplies a shape.
	if err := session.WritePointer(nil, 0, 0); err != nil {
		return err
	}

	var decoder rdp.InputDecoder
	defer func() { d.dispatchInput(session, decoder.ReleaseAll()) }()
	refresh := make(chan struct{}, 1)
	stop := make(chan struct{})
	writerDone := make(chan error, 2)
	writers := 1
	var suppressed atomic.Bool
	go func() {
		writerDone <- d.writeFrames(conn, session, stop, refresh, &suppressed)
		_ = session.Close()
	}()
	if withAudio {
		writers++
		go func() {
			writerDone <- d.writeAudio(session, stop)
			_ = session.Close()
		}()
	}
	defer func() {
		close(stop)
		_ = session.Close()
		for range writers {
			writerErr := <-writerDone
			if writerErr != nil && (serveErr == nil || errors.Is(serveErr, io.EOF) || errors.Is(serveErr, net.ErrClosed)) {
				serveErr = writerErr
			}
		}
	}()

	for {
		data, fastPath, err := session.ReadPacket()
		if err != nil {
			return err
		}
		if !fastPath && len(data) >= 18 && binary.LittleEndian.Uint16(data[2:])&15 == 7 {
			switch data[14] {
			case 33: // Refresh Rect: a complete repaint is always a valid response.
				requestRDPRefresh(refresh)
			case 35: // Suppress Output (minimized client).
				if len(data) >= 22 {
					suppressed.Store(data[18] == 0)
					if data[18] == 1 {
						requestRDPRefresh(refresh)
					}
				}
			}
		}
		if !fastPath && (len(data) < 18 || data[14] != 28) {
			continue
		}

		events, err := decoder.Decode(data, fastPath)
		if err != nil {
			return err
		}
		d.dispatchInput(session, events)
	}
}

func (d *RDPDisplay) writeAudio(session *rdp.Session, stop <-chan struct{}) error {
	sub := d.audio.Subscribe()
	defer sub.Close()
	changed := session.AudioChanged()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	announced := false
	for {
		if !announced && session.AudioReady() {
			log.Print("rdp: client using PCM audio (48 kHz, stereo, 16-bit)")
			announced = true
		}
		select {
		case <-stop:
			return nil
		case <-d.done:
			return nil
		case <-changed:
		case <-ticker.C:
			if err := session.CheckAudioTimeout(); err != nil {
				return err
			}
		case packet, ok := <-sub.Packets:
			if !ok {
				return nil
			}
			if !session.AudioCanSend() {
				continue
			}
			if _, err := session.WriteAudio(packet.PCM, packet.Timestamp); err != nil {
				return err
			}
		}
	}
}

func requestRDPRefresh(refresh chan<- struct{}) {
	select {
	case refresh <- struct{}{}:
	default:
	}
}

func (d *RDPDisplay) writeFrames(
	conn net.Conn, session *rdp.Session,
	stop, refresh <-chan struct{}, suppressed *atomic.Bool,
) error {
	width, height := session.Size()
	bitmap, err := rdp.NewBitmapEncoder(width, height, session.BitsPerPixel)
	if err != nil {
		return err
	}
	writer := &rdpFrameWriter{
		session: session, bitmap: bitmap, width: width, height: height, force: true,
		backend: d.h264Backend, slices: d.h264Slices, frameRate: d.frameRate,
		stats: rdpFrameStats{
			enabled: d.stats, peer: conn.RemoteAddr().String(), telemetry: d.telemetry.Connect(),
		},
	}
	writer.stats.start = writer.stats.begin()
	defer writer.shutdown()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	graphicsChanged := session.GraphicsChanged()
	displayChanged := session.DisplayChanged()
	resizeRequests := session.ResizeRequests()
	resizeTimer := time.NewTimer(time.Hour)
	resizeTimer.Stop()
	defer resizeTimer.Stop()
	var resizeDue <-chan time.Time
	var pending rdp.DesktopSize
	resizeReady := false

	// Coalesce publications while pacing or awaiting acknowledgments. Only arm
	// the timer for a pending frame; idle clients need no periodic polling.
	for {
		if resizeReady && session.DisplayReady() {
			resized, resizeErr := session.Resize(pending.Width, pending.Height)
			if resizeErr != nil {
				return resizeErr
			}
			resizeReady = false
			if resized {
				d.resizeGuest(session)
			}
		}
		if err := writer.updateSize(); err != nil {
			return err
		}
		if writer.updateGraphics(conn) {
			writer.force = true
		}
		if writer.stats.telemetry != nil {
			writer.stats.observeGraphics(session.GraphicsStats())
		}
		cursor, cursorChanged := d.cursorSnapshot()
		if session.DisplayReady() {
			// Readiness must not depend on a guest frame: boot may be waiting
			// here before any vCPU has started producing graphics.
			d.readyOnce.Do(func() { close(d.ready) })
			d.mu.Lock()
			guestWidth, guestHeight := d.width, d.height
			d.mu.Unlock()
			if err := writer.updatePointer(cursor, guestWidth, guestHeight); err != nil {
				return err
			}
		}
		changed, published := d.frameChanged(writer.sequence, writer.force)
		var paced <-chan time.Time
		pendingFrame := changed && session.DisplayReady() && !suppressed.Load()
		ackBlocked := pendingFrame && writer.graphics && !session.GraphicsCanSend()
		writer.stats.waiting(ackBlocked, writer.frameDeadline())
		if pendingFrame && !ackBlocked {
			paced, err = writer.writeWhenReady(d.framebuffer, timer)
			if err != nil {
				return err
			}
		}
		if writer.stats.telemetry != nil {
			writer.stats.observeGraphics(session.GraphicsStats())
		}
		writer.stats.report(writer.actualSlices(), false)

		select {
		case <-stop:
			return nil
		case <-d.done:
			return nil
		case <-refresh:
			writer.force = true
		case <-published:
		case <-graphicsChanged:
		case <-cursorChanged:
		case <-displayChanged:
		case pending = <-resizeRequests:
			resizeReady = false
			resizeTimer.Reset(rdpResizeDelay)
			resizeDue = resizeTimer.C
		case <-resizeDue:
			resizeDue = nil
			resizeReady = true
		case <-paced:
		}
		timer.Stop()
	}
}

type rdpFrameWriter struct {
	session   *rdp.Session
	bitmap    *rdp.BitmapEncoder
	avc       *avc.Encoder
	backend   avc.Backend
	frame     displayFrame
	width     int
	height    int
	graphics  bool
	frameRate int
	sequence  uint64
	force     bool
	nextFrame time.Time
	lastFrame time.Time
	pointer   rdpPointerWriter
	slices    int
	stats     rdpFrameStats
}

// Encoders retain geometry and the previous frame. Discard both after a resize
// so the new surface starts with a complete bitmap or an AVC keyframe.
func (w *rdpFrameWriter) updateSize() error {
	width, height := w.session.Size()
	if width == w.width && height == w.height {
		return nil
	}
	bitmap, err := rdp.NewBitmapEncoder(width, height, w.session.BitsPerPixel)
	if err != nil {
		return err
	}
	w.close()
	w.avc = nil
	w.bitmap = bitmap
	w.width, w.height = width, height
	w.force = true
	w.nextFrame = time.Time{}
	w.lastFrame = time.Time{}
	w.pointer = rdpPointerWriter{}
	w.session.ResetPointerCache()

	return nil
}

func (w *rdpFrameWriter) writeWhenReady(display *framebuffer, timer *time.Timer) (<-chan time.Time, error) {
	// Gate capture and full-picture encoding together. Publications while
	// waiting remain in the framebuffer's damage history, not an encode queue.
	deadline := w.frameDeadline()
	if delay := time.Until(deadline); delay > 0 {
		timer.Reset(delay)

		return timer.C, nil
	}
	w.lastFrame = time.Now()
	w.nextFrame = w.lastFrame.Add(w.frameInterval())
	start := w.stats.begin()
	damage := display.copyFrameChanges(&w.frame, w.force)
	w.stats.copied(w.stats.elapsed(start))
	sent, err := w.write(w.frame, damage, w.force)
	if err != nil {
		return nil, err
	}
	if w.stats.measuring() {
		encoder := "bitmap"
		timing := rdp.GraphicsStats{FrameInterval: time.Second / 30}
		if w.graphics {
			encoder = string(w.avc.Backend())
			timing = w.session.GraphicsStats()
		}
		w.stats.captured(
			encoder,
			image.Pt(w.frame.width, w.frame.height),
			image.Pt(w.width, w.height),
			timing,
		)
	}
	w.force = !sent
	if sent {
		w.sequence = w.frame.seq
	}

	return nil, nil //nolint:nilnil // A nil timer channel keeps an idle writer asleep until an event.
}

func (w *rdpFrameWriter) updateGraphics(conn net.Conn) bool {
	ready := w.session.GraphicsReady()
	if ready == w.graphics {
		return false
	}
	w.graphics = ready
	if ready {
		log.Printf("rdp: client %s using H.264 AVC420 graphics", conn.RemoteAddr())
	} else {
		w.close()
		w.stats.telemetry.Encoded("bitmap", 0)
		w.stats.telemetry.Acknowledge(0)
	}

	return true
}

func (w *rdpFrameWriter) frameInterval() time.Duration {
	if w.graphics {
		frameRate := w.frameRate
		if frameRate == 0 {
			frameRate = avc.FrameRate
		}
		base := time.Second / time.Duration(frameRate)
		if w.session != nil {
			return w.session.GraphicsFrameInterval(base)
		}

		return base
	}

	return time.Second / 30
}

// frameDeadline recomputes the deadline from the last send so ACK feedback can
// slow an already scheduled frame. nextFrame remains the initial/test hook.
func (w *rdpFrameWriter) frameDeadline() time.Time {
	if !w.lastFrame.IsZero() {
		return w.lastFrame.Add(w.frameInterval())
	}

	return w.nextFrame
}

func (w *rdpFrameWriter) close() {
	if w.avc != nil {
		w.avc.Close()
		w.avc = nil
	}
}

func (w *rdpFrameWriter) shutdown() {
	w.stats.report(w.actualSlices(), true)
	w.stats.telemetry.Close()
	w.close()
}

func (w *rdpFrameWriter) actualSlices() int {
	if w.avc != nil {
		return w.avc.Threads()
	}

	return 0
}

func (w *rdpFrameWriter) write(frame displayFrame, damage []image.Rectangle, force bool) (bool, error) {
	img := &image.RGBA{Pix: frame.pix, Stride: frame.width * 4, Rect: image.Rect(0, 0, frame.width, frame.height)}
	if len(frame.pix) == 0 {
		img = image.NewRGBA(img.Rect)
	}
	if !w.graphics {
		start := w.stats.begin()
		var writeTime time.Duration
		var bytes int
		err := w.bitmap.WriteFrameDamage(img, damage, force, func(data []byte) error {
			writeStart := w.stats.begin()
			err := w.session.WriteDataPDU(2, data)
			writeTime += w.stats.elapsed(writeStart)
			bytes += len(data)

			return err
		})
		w.stats.converted(w.stats.elapsed(start) - writeTime)
		w.stats.written(writeTime)
		if err == nil {
			w.stats.sent(bytes)
		}

		return err == nil, err
	}
	if w.avc == nil {
		width, height := w.session.Size()
		encoder, err := avc.NewEncoderWithOptions(width, height, avc.Options{
			Backend: w.backend, Threads: max(1, w.slices), Measure: w.stats.measuring(), FrameRate: w.frameRate,
		})
		if err != nil {
			return false, err
		}
		w.avc = encoder
		force = true // A new encoder starts an independent reference chain.
		log.Printf(
			"rdp: client %s H.264 encoder=%s slices=%d max_fps=%d",
			w.stats.peer,
			encoder.Backend(),
			encoder.Threads(),
			encoder.FrameRate(),
		)
		if reason := encoder.FallbackReason(); reason != nil {
			log.Printf("rdp: client %s H.264 using software fallback: %v", w.stats.peer, reason)
		}
	}
	previousBackend := w.avc.Backend()
	data, err := w.avc.EncodeDamage(img, damage, force)
	if w.avc.Backend() != previousBackend {
		log.Printf(
			"rdp: client %s H.264 encoder=%s after %s failure: %v",
			w.stats.peer,
			w.avc.Backend(),
			previousBackend,
			w.avc.FallbackReason(),
		)
	}
	if w.stats.measuring() {
		stats := w.avc.LastStats()
		w.stats.converted(stats.Conversion)
		w.stats.encoded(string(w.avc.Backend()), stats.Encoding)
	}
	if err != nil || len(data) == 0 {
		return err == nil, err
	}

	var regions []image.Rectangle
	if !force && damage != nil {
		width, height := w.session.Size()
		output := image.Rect(0, 0, width, height)
		for _, rect := range damage {
			mapped := rdpdamage.Map(rect, img.Bounds(), output)
			if !mapped.Empty() {
				regions = append(regions, mapped)
			}
		}
	}

	start := w.stats.begin()
	sent, err := w.session.WriteAVC420Damage(data, regions)
	w.stats.written(w.stats.elapsed(start))
	if sent {
		w.stats.sent(len(data))
	}

	return sent, err
}

func (d *RDPDisplay) dispatchInput(session *rdp.Session, events []rdp.InputEvent) {
	for _, event := range events {
		switch event.Kind {
		case rdp.InputKey:
			d.sendKeyEvent(event.Down, event.Key)
		case rdp.InputPointer:
			d.pointerInputMu.Lock()
			d.mu.Lock()
			input := d.input
			width, height := d.width, d.height
			d.mu.Unlock()
			clientWidth, clientHeight := session.Size()
			x := min(int(event.X), clientWidth-1) * width / clientWidth
			y := min(int(event.Y), clientHeight-1) * height / clientHeight
			d.rememberPointerMotion(session, x, y)
			// Normalize against the same geometry used above, even if the
			// guest publishes a resized framebuffer while input is in flight.
			sendPointerEventInBounds(input, event.Buttons, uint16(x), uint16(y), width, height)
			d.pointerInputMu.Unlock()
		}
	}
}

func rdpCertificate() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "devbox-kvm console"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost"}, IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
