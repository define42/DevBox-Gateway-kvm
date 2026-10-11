//nolint:err113 // RFB parsing returns contextual protocol errors for clients.
package virtio

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

const (
	rfbProtocolVersion = "RFB 003.008\n"
	rfbSecurityNone    = 1

	rfbMsgSetPixelFormat           = 0
	rfbMsgSetEncodings             = 2
	rfbMsgFramebufferUpdateRequest = 3
	rfbMsgKeyEvent                 = 4
	rfbMsgPointerEvent             = 5
	rfbMsgClientCutText            = 6

	rfbEncodingRaw    = 0
	rfbEncodingCursor = 0xffffff11 // int32(-239) on the wire.

)

type rfbPixelFormat struct {
	bitsPerPixel uint8
	depth        uint8
	bigEndian    bool
	trueColor    bool
	redMax       uint16
	greenMax     uint16
	blueMax      uint16
	redShift     uint8
	greenShift   uint8
	blueShift    uint8
}

// VNCDisplay exposes flushed virtio-gpu frames over the RFB/VNC protocol.
type VNCDisplay struct {
	*framebuffer

	listener  net.Listener
	closeOnce sync.Once
	conns     map[net.Conn]struct{}
	wg        sync.WaitGroup
}

// NewVNCDisplay starts a VNC server listening on addr, such as ":5900".
func NewVNCDisplay(addr string) (*VNCDisplay, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	d := &VNCDisplay{
		framebuffer: newFramebuffer(),
		listener:    ln,
		conns:       make(map[net.Conn]struct{}),
	}

	d.wg.Add(1)
	go d.acceptLoop()

	return d, nil
}

// Addr returns the address the VNC server is listening on.
func (d *VNCDisplay) Addr() string {
	return d.listener.Addr().String()
}

// Write mirrors serial output into the VNC console until graphics takes over.
func (d *VNCDisplay) Write(p []byte) (int, error) {
	return d.writeSerial(p)
}

func (d *VNCDisplay) Close() error {
	var err error

	d.closeOnce.Do(func() {
		d.shutdown()
		err = d.listener.Close()

		d.mu.Lock()
		for conn := range d.conns {
			_ = conn.Close()
		}
		d.cond.Broadcast()
		d.mu.Unlock()

		d.wg.Wait()
	})

	return err
}

func (d *VNCDisplay) acceptLoop() {
	defer d.wg.Done()

	for {
		conn, err := d.listener.Accept()
		if err != nil {
			select {
			case <-d.done:
				return
			default:
				log.Printf("vnc: accept: %v", err)

				continue
			}
		}

		if !d.trackConn(conn) {
			_ = conn.Close()

			continue
		}

		d.wg.Add(1)
		go d.handleConn(conn)
	}
}

func (d *VNCDisplay) trackConn(conn net.Conn) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	select {
	case <-d.done:
		return false
	default:
		d.conns[conn] = struct{}{}

		return true
	}
}

func (d *VNCDisplay) untrackConn(conn net.Conn) {
	d.mu.Lock()
	delete(d.conns, conn)
	d.mu.Unlock()
}

func (d *VNCDisplay) handleConn(conn net.Conn) {
	defer d.wg.Done()
	defer d.untrackConn(conn)
	defer conn.Close()

	if err := d.serveConn(conn); err != nil {
		log.Printf("vnc: client %s: %v", conn.RemoteAddr(), err)
	}
}

func (d *VNCDisplay) serveConn(conn net.Conn) error {
	if err := d.handshake(conn); err != nil {
		return err
	}

	pf := defaultRFBPixelFormat()
	cursorEnabled := false
	frame := d.snapshot()

	if err := writeServerInit(conn, frame.width, frame.height, pf); err != nil {
		return err
	}

	updateReqs := make(chan vncUpdateRequest, 1)
	updateErrs := make(chan error, 1)
	done := make(chan struct{})
	defer func() {
		close(done)
		d.wakeFrameWaiters()
	}()

	go d.writeFramebufferUpdates(conn, updateReqs, updateErrs, done)

	for {
		select {
		case err := <-updateErrs:
			return err
		default:
		}

		var typ [1]byte
		if _, err := io.ReadFull(conn, typ[:]); err != nil {
			return err
		}

		switch typ[0] {
		case rfbMsgSetPixelFormat:
			next, err := readSetPixelFormat(conn)
			if err != nil {
				return err
			}

			pf = next
		case rfbMsgSetEncodings:
			encodings, err := readSetEncodings(conn)
			if err != nil {
				return err
			}

			cursorEnabled = encodings.cursor
		case rfbMsgFramebufferUpdateRequest:
			req, err := readFramebufferUpdateRequest(conn)
			if err != nil {
				return err
			}

			select {
			case updateReqs <- vncUpdateRequest{req: req, pf: pf, cursor: cursorEnabled}:
			default:
			}
		case rfbMsgKeyEvent:
			event, err := readKeyEvent(conn)
			if err != nil {
				return err
			}

			d.sendKeyEvent(event.down, event.keysym)
		case rfbMsgPointerEvent:
			event, err := readPointerEvent(conn)
			if err != nil {
				return err
			}

			d.sendPointerEvent(event.buttonMask, event.x, event.y)
		case rfbMsgClientCutText:
			if err := readClientCutText(conn); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown client message %d", typ[0])
		}
	}
}

type vncUpdateRequest struct {
	req    framebufferUpdateRequest
	pf     rfbPixelFormat
	cursor bool
}

func (d *VNCDisplay) writeFramebufferUpdates(
	conn net.Conn,
	reqs <-chan vncUpdateRequest,
	errs chan<- error,
	done <-chan struct{},
) {
	lastSeq := uint64(0)

	for {
		select {
		case <-done:
			return
		case req := <-reqs:
			frame, ok := d.frameForRequestUntil(req.req.incremental, lastSeq, done)
			if !ok {
				return
			}

			if err := writeFramebufferUpdate(
				conn,
				frame,
				req.pf,
				req.cursor,
				req.req.x,
				req.req.y,
				req.req.width,
				req.req.height,
			); err != nil {
				select {
				case errs <- err:
				default:
				}
				_ = conn.Close()

				return
			}

			lastSeq = frame.seq
		}
	}
}

func (d *VNCDisplay) handshake(conn net.Conn) error {
	if _, err := conn.Write([]byte(rfbProtocolVersion)); err != nil {
		return err
	}

	var version [12]byte
	if _, err := io.ReadFull(conn, version[:]); err != nil {
		return err
	}

	if _, err := conn.Write([]byte{1, rfbSecurityNone}); err != nil {
		return err
	}

	var security [1]byte
	if _, err := io.ReadFull(conn, security[:]); err != nil {
		return err
	}

	if security[0] != rfbSecurityNone {
		return fmt.Errorf("unsupported security type %d", security[0])
	}

	var result [4]byte
	if _, err := conn.Write(result[:]); err != nil {
		return err
	}

	var clientInit [1]byte
	_, err := io.ReadFull(conn, clientInit[:])

	return err
}

type framebufferUpdateRequest struct {
	incremental bool
	x           uint16
	y           uint16
	width       uint16
	height      uint16
}

type keyEvent struct {
	down   bool
	keysym uint32
}

type pointerEvent struct {
	buttonMask uint8
	x          uint16
	y          uint16
}

type clientEncodings struct {
	cursor bool
}

func readFramebufferUpdateRequest(r io.Reader) (framebufferUpdateRequest, error) {
	var b [9]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return framebufferUpdateRequest{}, err
	}

	return framebufferUpdateRequest{
		incremental: b[0] != 0,
		x:           binary.BigEndian.Uint16(b[1:3]),
		y:           binary.BigEndian.Uint16(b[3:5]),
		width:       binary.BigEndian.Uint16(b[5:7]),
		height:      binary.BigEndian.Uint16(b[7:9]),
	}, nil
}

func readKeyEvent(r io.Reader) (keyEvent, error) {
	var b [7]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return keyEvent{}, err
	}

	return keyEvent{
		down:   b[0] != 0,
		keysym: binary.BigEndian.Uint32(b[3:7]),
	}, nil
}

func readPointerEvent(r io.Reader) (pointerEvent, error) {
	var b [5]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return pointerEvent{}, err
	}

	return pointerEvent{
		buttonMask: b[0],
		x:          binary.BigEndian.Uint16(b[1:3]),
		y:          binary.BigEndian.Uint16(b[3:5]),
	}, nil
}

func readSetPixelFormat(r io.Reader) (rfbPixelFormat, error) {
	var b [19]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return rfbPixelFormat{}, err
	}

	pf := parsePixelFormat(b[3:])
	if err := validatePixelFormat(pf); err != nil {
		return rfbPixelFormat{}, err
	}

	return pf, nil
}

func readSetEncodings(r io.Reader) (clientEncodings, error) {
	var hdr [3]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return clientEncodings{}, err
	}

	n := binary.BigEndian.Uint16(hdr[1:3])
	var enc clientEncodings
	var raw [4]byte

	for range n {
		if _, err := io.ReadFull(r, raw[:]); err != nil {
			return clientEncodings{}, err
		}

		if binary.BigEndian.Uint32(raw[:]) == rfbEncodingCursor {
			enc.cursor = true
		}
	}

	return enc, nil
}

func readClientCutText(r io.Reader) error {
	var hdr [7]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}

	n := binary.BigEndian.Uint32(hdr[3:7])

	return discardFull(r, int(n))
}

func discardFull(r io.Reader, n int) error {
	if n == 0 {
		return nil
	}

	_, err := io.CopyN(io.Discard, r, int64(n))

	return err
}

func writeServerInit(w io.Writer, width, height int, pf rfbPixelFormat) error {
	name := []byte("gokvm")
	b := make([]byte, 24+len(name))

	binary.BigEndian.PutUint16(b[0:2], uint16(width))
	binary.BigEndian.PutUint16(b[2:4], uint16(height))
	putPixelFormat(b[4:20], pf)
	binary.BigEndian.PutUint32(b[20:24], uint32(len(name)))
	copy(b[24:], name)

	_, err := w.Write(b)

	return err
}

func writeFramebufferUpdate(
	w io.Writer,
	frame vncFrame,
	pf rfbPixelFormat,
	cursor bool,
	xReq, yReq, wReq, hReq uint16,
) error {
	x := int(xReq)
	y := int(yReq)
	width := int(wReq)
	height := int(hReq)

	if x >= frame.width || y >= frame.height || width == 0 || height == 0 {
		if !cursor {
			_, err := w.Write([]byte{0, 0, 0, 0})

			return err
		}

		b := append([]byte{0, 0, 0, 1}, encodeCursorPseudoRect(pf)...)
		_, err := w.Write(b)

		return err
	}

	if x+width > frame.width {
		width = frame.width - x
	}

	if y+height > frame.height {
		height = frame.height - y
	}

	pixels := encodeRawPixels(frame, pf, x, y, width, height)
	cursorRect := []byte(nil)
	rects := uint16(1)
	if cursor {
		cursorRect = encodeCursorPseudoRect(pf)
		rects++
	}

	b := make([]byte, 16+len(pixels)+len(cursorRect))

	b[0] = 0 // FramebufferUpdate
	binary.BigEndian.PutUint16(b[2:4], rects)
	binary.BigEndian.PutUint16(b[4:6], uint16(x))
	binary.BigEndian.PutUint16(b[6:8], uint16(y))
	binary.BigEndian.PutUint16(b[8:10], uint16(width))
	binary.BigEndian.PutUint16(b[10:12], uint16(height))
	binary.BigEndian.PutUint32(b[12:16], rfbEncodingRaw)
	copy(b[16:], pixels)
	copy(b[16+len(pixels):], cursorRect)

	_, err := w.Write(b)

	return err
}

func encodeCursorPseudoRect(pf rfbPixelFormat) []byte {
	const (
		width  = 1
		height = 1
	)

	bytesPerPixel := int(pf.bitsPerPixel) / 8
	maskStride := (width + 7) / 8
	pixels := make([]byte, width*height*bytesPerPixel)
	mask := make([]byte, height*maskStride)
	b := make([]byte, 12+len(pixels)+len(mask))
	binary.BigEndian.PutUint16(b[4:6], width)
	binary.BigEndian.PutUint16(b[6:8], height)
	binary.BigEndian.PutUint32(b[8:12], rfbEncodingCursor)
	copy(b[12:], pixels)
	copy(b[12+len(pixels):], mask)

	return b
}

func encodeRawPixels(frame vncFrame, pf rfbPixelFormat, x, y, width, height int) []byte {
	bytesPerPixel := int(pf.bitsPerPixel) / 8
	dst := make([]byte, width*height*bytesPerPixel)

	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			src := ((y+row)*frame.width + x + col) * 4
			dstOff := (row*width + col) * bytesPerPixel

			var r, g, b uint8
			if src+3 <= len(frame.pix) {
				r = frame.pix[src]
				g = frame.pix[src+1]
				b = frame.pix[src+2]
			}

			pixel := scaledChannel(r, pf.redMax)<<pf.redShift |
				scaledChannel(g, pf.greenMax)<<pf.greenShift |
				scaledChannel(b, pf.blueMax)<<pf.blueShift
			putPixel(dst[dstOff:dstOff+bytesPerPixel], pixel, pf.bigEndian)
		}
	}

	return dst
}

func defaultRFBPixelFormat() rfbPixelFormat {
	return rfbPixelFormat{
		bitsPerPixel: 32,
		depth:        24,
		bigEndian:    false,
		trueColor:    true,
		redMax:       255,
		greenMax:     255,
		blueMax:      255,
		redShift:     16,
		greenShift:   8,
		blueShift:    0,
	}
}

func parsePixelFormat(b []byte) rfbPixelFormat {
	return rfbPixelFormat{
		bitsPerPixel: b[0],
		depth:        b[1],
		bigEndian:    b[2] != 0,
		trueColor:    b[3] != 0,
		redMax:       binary.BigEndian.Uint16(b[4:6]),
		greenMax:     binary.BigEndian.Uint16(b[6:8]),
		blueMax:      binary.BigEndian.Uint16(b[8:10]),
		redShift:     b[10],
		greenShift:   b[11],
		blueShift:    b[12],
	}
}

func putPixelFormat(b []byte, pf rfbPixelFormat) {
	b[0] = pf.bitsPerPixel
	b[1] = pf.depth
	if pf.bigEndian {
		b[2] = 1
	}
	if pf.trueColor {
		b[3] = 1
	}
	binary.BigEndian.PutUint16(b[4:6], pf.redMax)
	binary.BigEndian.PutUint16(b[6:8], pf.greenMax)
	binary.BigEndian.PutUint16(b[8:10], pf.blueMax)
	b[10] = pf.redShift
	b[11] = pf.greenShift
	b[12] = pf.blueShift
}

func validatePixelFormat(pf rfbPixelFormat) error {
	if !pf.trueColor {
		return fmt.Errorf("unsupported color-map pixel format")
	}

	if pf.bitsPerPixel != 8 && pf.bitsPerPixel != 16 && pf.bitsPerPixel != 32 {
		return fmt.Errorf("unsupported bits-per-pixel %d", pf.bitsPerPixel)
	}

	if pf.bitsPerPixel < pf.depth {
		return fmt.Errorf("depth %d exceeds bits-per-pixel %d", pf.depth, pf.bitsPerPixel)
	}

	return nil
}

func scaledChannel(v uint8, max uint16) uint32 {
	return uint32(v) * uint32(max) / 255
}

func putPixel(dst []byte, pixel uint32, bigEndian bool) {
	switch len(dst) {
	case 1:
		dst[0] = byte(pixel)
	case 2:
		if bigEndian {
			binary.BigEndian.PutUint16(dst, uint16(pixel))
		} else {
			binary.LittleEndian.PutUint16(dst, uint16(pixel))
		}
	case 4:
		if bigEndian {
			binary.BigEndian.PutUint32(dst, pixel)
		} else {
			binary.LittleEndian.PutUint32(dst, pixel)
		}
	}
}
