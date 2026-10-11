//nolint:err113 // Protocol errors identify malformed wire structures for diagnostic logging.
package rdp

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

const (
	displayChannelID   = 2
	displayChannelName = "Microsoft::Windows::RDS::DisplayControl\x00"
	maxDesktopArea     = 4096 * 2160
)

// DesktopSize describes the visible, single-monitor RDP desktop in pixels.
type DesktopSize struct {
	Width, Height int
}

type displayState struct {
	mu           sync.Mutex
	requests     chan DesktopSize
	changed      chan struct{}
	reactivation *activation
	packets      int
}

func validDesktopSize(width, height int) bool {
	return width >= 200 && height >= 200 && width <= 4096 && height <= 4096 &&
		width%2 == 0 && height%2 == 0 && width*height <= maxDesktopArea
}

func normalizeDesktopSize(width, height int) (DesktopSize, bool) {
	if width < 200 || height < 200 || width > 4096 || height > 4096 {
		return DesktopSize{}, false
	}
	// Window clients can request odd heights. AVC420 needs even dimensions;
	// lose at most one edge pixel rather than ignoring alternate drag events.
	width, height = width&^1, height&^1

	return DesktopSize{Width: width, Height: height}, validDesktopSize(width, height)
}

func (s *Session) storeSize(width, height int) {
	s.geometry.Store(uint64(uint32(width))<<32 | uint64(uint32(height)))
}

// Size returns one consistent snapshot of the currently configured desktop.
func (s *Session) Size() (width, height int) {
	size := s.geometry.Load()
	if size == 0 {
		return s.Width, s.Height
	}

	return int(size >> 32), int(uint32(size))
}

// BeginDisplayControl enables client-requested desktop resizing independently
// of the selected graphics codec. Clients without drdynvc keep their initial size.
func (s *Session) BeginDisplayControl() (bool, error) {
	return s.beginDynamic(displayChannelID)
}

// ResizeRequests delivers the latest valid client size. Requests are coalesced
// so dragging a client window cannot build an unbounded encoder-reset queue.
func (s *Session) ResizeRequests() <-chan DesktopSize {
	d := &s.display
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.requests == nil {
		d.requests = make(chan DesktopSize, 1)
	}

	return d.requests
}

// DisplayReady is false during bitmap capability reactivation. The network
// reader continues processing channel traffic while the frame writer pauses.
func (s *Session) DisplayReady() bool {
	s.display.mu.Lock()
	defer s.display.mu.Unlock()

	return s.display.reactivation == nil
}

// DisplayChanged wakes the frame writer after a resize or bitmap reactivation.
func (s *Session) DisplayChanged() <-chan struct{} {
	d := &s.display
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.changed == nil {
		d.changed = make(chan struct{}, 1)
	}

	return d.changed
}

func (d *displayState) notifyChanged() {
	select {
	case d.changed <- struct{}{}:
	default:
	}
}

// Resize applies a validated desktop size. Only the frame writer may call it,
// between frames. A true result means the geometry changed; bitmap rendering
// must still wait for DisplayReady. Invalid, duplicate, and overlapping requests
// return false without changing the active desktop.
func (s *Session) Resize(width, height int) (bool, error) {
	size, valid := normalizeDesktopSize(width, height)
	if !valid {
		return false, nil
	}
	width, height = size.Width, size.Height
	if g := s.graphics; g != nil {
		g.writeMu.Lock()
		defer g.writeMu.Unlock()
		g.mu.Lock()
		defer g.mu.Unlock()
	}
	d := &s.display
	d.mu.Lock()
	defer d.mu.Unlock()
	currentWidth, currentHeight := s.Size()
	if d.reactivation != nil || (currentWidth == width && currentHeight == height) {
		return false, nil
	}
	if g := s.graphics; g != nil && g.ready {
		// Writes on the graphics DVC are ordered. Retire the old ACK window
		// without reusing frame IDs, so delayed ACKs cannot acknowledge new frames.
		deleteSurface := graphicsPDU(0x0a, []byte{0, 0})
		// Keep frame/resize ordering under writeMu, while allowing the reader
		// to process ACKs and check DisplayReady during a blocked transport.
		d.mu.Unlock()
		g.mu.Unlock()
		err := s.writeGraphics(append(deleteSurface, graphicsSurface(width, height)...))
		g.mu.Lock()
		d.mu.Lock()
		if err != nil {
			return false, err
		}
		g.resetAcknowledgments()
		g.notifyChanged()
		s.storeSize(width, height)
		d.notifyChanged()

		return true, nil
	}
	if s.writeTimeout != 0 {
		if err := s.conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
			return false, err
		}
	}
	d.reactivation = &activation{session: s, info: true, reactivation: true}
	d.packets = 0
	s.storeSize(width, height)
	if err := s.writeGlobal(s.deactivateAll()); err != nil {
		return false, err
	}
	if err := s.writeGlobal(s.demandActive()); err != nil {
		return false, err
	}
	d.notifyChanged()

	return true, nil
}

func (s *Session) deactivateAll() []byte {
	data := make([]byte, 12)
	put16(data, 0, len(data))
	put16(data, 2, 0x16)
	put16(data, 4, serverUser)
	binary.LittleEndian.PutUint32(data[6:], shareID)
	// lengthSourceDescriptor is zero: the descriptor is optional.

	return data
}

func (s *Session) readReactivation(data []byte) (bool, error) {
	d := &s.display
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.reactivation == nil {
		return false, nil
	}
	d.packets++
	if d.packets > 256 {
		return true, errors.New("rdp: excessive bitmap reactivation packets")
	}
	kind, err := shareType(data)
	if err != nil {
		return true, err
	}
	if kind == 7 && len(data) >= 18 && data[14] == 28 {
		// In-flight keyboard/button releases can precede Confirm Active.
		return true, s.queueInput(data, false)
	}
	// Old refresh/suppression notifications may be in flight when the server
	// deactivates. Ignore those rather than treating them as handshake packets.
	if kind == 7 && len(data) >= 18 && (data[14] == 33 || data[14] == 35) {
		return true, nil
	}
	done, err := d.reactivation.globalData(data)
	if err != nil || !done {
		return true, err
	}
	d.reactivation = nil
	d.notifyChanged()
	if s.writeTimeout != 0 {
		return true, s.conn.SetReadDeadline(time.Time{})
	}

	return true, nil
}

func displayCapabilities() []byte {
	data := make([]byte, 20)
	binary.LittleEndian.PutUint32(data, 5)
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[8:], 1)
	binary.LittleEndian.PutUint32(data[12:], 4096)
	binary.LittleEndian.PutUint32(data[16:], 2160)

	return data
}

func (s *Session) readDisplayControl(data []byte) {
	// A layout requests policy-controlled geometry. Invalid or unsupported
	// requests leave the current desktop active instead of disconnecting it.
	if len(data) != 56 || binary.LittleEndian.Uint32(data) != 2 ||
		binary.LittleEndian.Uint32(data[4:]) != 56 || binary.LittleEndian.Uint32(data[8:]) != 40 ||
		binary.LittleEndian.Uint32(data[12:]) != 1 {
		return
	}
	monitor := data[16:]
	if binary.LittleEndian.Uint32(monitor) != 1 || binary.LittleEndian.Uint32(monitor[4:]) != 0 ||
		binary.LittleEndian.Uint32(monitor[8:]) != 0 {
		return
	}
	width, height := int(binary.LittleEndian.Uint32(monitor[12:])), int(binary.LittleEndian.Uint32(monitor[16:]))
	size, valid := normalizeDesktopSize(width, height)
	if !valid {
		return
	}
	d := &s.display
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.requests == nil {
		d.requests = make(chan DesktopSize, 1)
	}
	select {
	case <-d.requests:
	default:
	}
	d.requests <- size
}
