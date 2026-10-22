//nolint:err113 // Protocol errors identify malformed wire structures for diagnostic logging.
package rdp

import (
	"encoding/binary"
	"errors"
	"image"
	"sync"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/damage"
)

const (
	graphicsChannelName      = "Microsoft::Windows::RDS::Graphics\x00"
	graphicsChannelID        = 1
	graphicsMaxMessage       = 8 << 20
	graphicsMaxIncoming      = 1 << 20
	graphicsMaxFrames        = 2
	graphicsMaxRegions       = 128
	graphicsVersion81        = 0x00080105
	graphicsMinFrameInterval = time.Second / 60
	graphicsMaxFrameInterval = 250 * time.Millisecond
)

// graphicsState implements the AVC420 subset of MS-RDPEGFX over MS-RDPEDYC.
// mu protects negotiation and frame acknowledgments; writeMu orders outgoing
// frames and resizes without blocking the reader during transport writes.
// A single reader owns both inbound reassembly buffers.
type graphicsPendingFrame struct {
	id     uint32
	sentAt time.Time
}

type graphicsState struct {
	mu            sync.Mutex
	writeMu       sync.Mutex
	ackGeneration uint64
	supported     bool
	ready         bool
	closed        bool
	ackOff        bool
	frameID       uint32
	inFlight      []graphicsPendingFrame
	averageBytes  uint64
	ackTurnaround time.Duration
	frameInterval time.Duration
	baseInterval  time.Duration
	changed       chan struct{}
}

// BeginGraphics starts optional AVC420 negotiation. A false result means the
// client did not offer the graphics channel; the bitmap path remains available.
// ReadPacket must continue running to process the client's negotiation replies.
func (s *Session) BeginGraphics() (bool, error) {
	if s.graphics == nil || !s.graphics.supported {
		return false, nil
	}

	return s.beginDynamic(graphicsChannelID)
}

// GraphicsReady reports whether AVC420 capabilities and the output surface
// have been established. Bitmap output should stop once this returns true.
func (s *Session) GraphicsReady() bool {
	if s.graphics == nil {
		return false
	}
	s.graphics.mu.Lock()
	defer s.graphics.mu.Unlock()

	return s.graphics.ready
}

// GraphicsCanSend applies a bounded frame acknowledgment window. Call this
// before encoding a predicted frame: dropping an encoded frame can invalidate
// the following frame's references. There must be only one video writer.
func (s *Session) GraphicsCanSend() bool {
	if s.graphics == nil {
		return false
	}
	s.graphics.mu.Lock()
	defer s.graphics.mu.Unlock()

	return s.graphics.canSend() && s.DisplayReady()
}

// GraphicsFrameInterval returns acknowledgement-adapted graphics pacing.
// Ordinary frame acknowledgements report client queue depth; their turnaround
// also provides a fallback for clients that report queue depth as unavailable.
// Slowdown is immediate, while recovery is deliberately gradual.
func (s *Session) GraphicsFrameInterval(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	if s.graphics == nil {
		return base
	}
	g := s.graphics
	g.mu.Lock()
	defer g.mu.Unlock()
	g.baseInterval = base
	if g.frameInterval < base {
		g.frameInterval = base
	}

	return min(g.frameInterval, graphicsMaxFrameInterval)
}

// GraphicsStats is a read-only snapshot of acknowledgement-driven pacing.
// ACKTurnaround is the smoothed time from sending a frame to receiving its
// acknowledgement, including client processing; it is not network RTT.
type GraphicsStats struct {
	FrameInterval   time.Duration
	ACKTurnaround   time.Duration
	OutstandingACKs uint32 // Frames currently waiting for acknowledgment; zero when ACKs are disabled.
}

// GraphicsStats returns a coherent snapshot without updating pacing state.
// Values are zero before pacing or acknowledgement samples are available.
func (s *Session) GraphicsStats() GraphicsStats {
	if s.graphics == nil {
		return GraphicsStats{}
	}
	g := s.graphics
	g.mu.Lock()
	defer g.mu.Unlock()

	return GraphicsStats{
		FrameInterval:   g.frameInterval,
		ACKTurnaround:   g.ackTurnaround,
		OutstandingACKs: uint32(len(g.inFlight)),
	}
}

// GraphicsChanged wakes the single video writer when negotiation or frame
// acknowledgments change graphics readiness. Notifications are coalesced; the
// writer must check the current state after subscribing and after each wake.
func (s *Session) GraphicsChanged() <-chan struct{} {
	if s.graphics == nil {
		return nil
	}
	g := s.graphics
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.changed == nil {
		g.changed = make(chan struct{}, 1)
	}

	return g.changed
}

// notifyChanged is called with mu held and never blocks the protocol reader.
func (g *graphicsState) notifyChanged() {
	select {
	case g.changed <- struct{}{}:
	default:
	}
}

func (g *graphicsState) canSend() bool {
	return g.ready && (g.ackOff || len(g.inFlight) < graphicsMaxFrames)
}

// WriteAVC420 sends one Annex B H.264 frame covering the entire desktop. The
// encoder must use YUV420 and dimensions rounded up to multiples of 16. Returns
// false without writing if negotiation or frame acknowledgments are pending.
func (s *Session) WriteAVC420(annexB []byte) (bool, error) {
	return s.WriteAVC420Damage(annexB, nil)
}

// WriteAVC420Damage sends a complete H.264 access unit with a repaint mask in
// output desktop coordinates. The client decodes the entire access unit while
// converting and repainting only the listed regions. Nil, empty, invalid-only,
// or excessively fragmented masks repaint everything: an already encoded
// interframe must never be dropped because its dirty region became empty.
// IDR keyframes automatically repaint the complete desktop.
func (s *Session) WriteAVC420Damage(annexB []byte, regions []image.Rectangle) (bool, error) {
	if len(annexB) == 0 || len(annexB) > avc420MaxPayload(1) {
		return false, errors.New("rdp: invalid AVC420 frame length")
	}
	g := s.graphics
	if g == nil {
		return false, nil
	}
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	g.mu.Lock()
	canSend := g.canSend()
	g.mu.Unlock()
	if !canSend {
		return false, nil
	}
	width, height := s.Size()
	if width <= 0 || height <= 0 || width > 4096 || height > 4096 {
		return false, errors.New("rdp: invalid AVC420 desktop geometry")
	}
	if len(regions) != 0 && avc420Keyframe(annexB) {
		// Periodic IDRs may re-quantize unchanged macroblocks. Repaint the
		// complete picture, including keyframes not explicitly forced by us.
		regions = nil
	}
	regions = avc420Regions(width, height, regions)
	if len(annexB) > avc420MaxPayload(len(regions)) {
		return false, errors.New("rdp: AVC420 frame and region metadata exceed message limit")
	}
	g.mu.Lock()
	if !g.canSend() {
		g.mu.Unlock()

		return false, nil
	}
	g.frameID++
	frameID, generation := g.frameID, g.ackGeneration
	if !g.ackOff {
		// Reserve before writing: the reader can receive an ACK as the final
		// transport Write returns. Completion must not reinsert an ACKed frame.
		g.inFlight = append(g.inFlight, graphicsPendingFrame{id: frameID})
	}
	g.mu.Unlock()

	var metadata [57 + 10*graphicsMaxRegions]byte
	header, end := avc420FrameMetadata(metadata[:], width, height, frameID,
		graphicsTimestamp(time.Now()), len(annexB), regions)
	frameLength := len(header) + len(annexB) + len(end)
	err := s.writeGraphicsParts([][]byte{header, annexB, end})
	g.mu.Lock()
	defer g.mu.Unlock()
	// Suspension and channel close invalidate the old ACK window, including
	// any frame still being written. Do not restore its pacing state afterward.
	if generation != g.ackGeneration {
		return err == nil, err
	}
	for i := range g.inFlight {
		if g.inFlight[i].id != frameID {
			continue
		}
		if err != nil {
			g.inFlight = append(g.inFlight[:i], g.inFlight[i+1:]...)
			g.notifyChanged()
		} else {
			g.inFlight[i].sentAt = time.Now()
		}

		break
	}
	if err == nil {
		g.observeFrame(frameLength)
	}

	return err == nil, err
}

// MS-RDPEGFX encodes UTC time of day, not Unix milliseconds, in START_FRAME.
func graphicsTimestamp(now time.Time) uint32 {
	now = now.UTC()

	return uint32(now.Hour())<<22 | uint32(now.Minute())<<16 | uint32(now.Second())<<10 |
		uint32(now.Nanosecond()/int(time.Millisecond))
}

func (s *Session) readGraphics(data []byte) error {
	for len(data) > 0 {
		if len(data) < 8 {
			return errors.New("rdp: truncated graphics header")
		}
		size := binary.LittleEndian.Uint32(data[4:])
		if size < 8 || uint64(size) > uint64(len(data)) || binary.LittleEndian.Uint16(data[2:]) != 0 {
			return errors.New("rdp: invalid graphics PDU size or flags")
		}
		if err := s.readGraphicsPDU(binary.LittleEndian.Uint16(data), data[8:size]); err != nil {
			return err
		}
		data = data[size:]
	}

	return nil
}

func (s *Session) readGraphicsPDU(command uint16, data []byte) error {
	g := s.graphics
	switch command {
	case 0x12: // RDPGFX_CAPS_ADVERTISE
		if g.ready {
			return errors.New("rdp: duplicate graphics capabilities")
		}
		version, flags, err := selectGraphicsCapabilities(data)
		if err != nil {
			return err
		}
		if version == 0 {
			g.closed = true

			return s.writeStatic(s.dynamic.channel, []byte{0x40, graphicsChannelID})
		}
		width, height := s.Size()
		if err := s.writeGraphics(graphicsInitialization(width, height, version, flags)); err != nil {
			return err
		}
		g.ready = true
		g.notifyChanged()
	case 0x0d: // RDPGFX_FRAME_ACKNOWLEDGE
		if !g.ready || len(data) != 12 {
			return errors.New("rdp: invalid graphics frame acknowledgment")
		}
		queueDepth := binary.LittleEndian.Uint32(data)
		if queueDepth == ^uint32(0) {
			g.ackOff = true
			g.resetAcknowledgments()
			g.notifyChanged()

			return nil
		}
		wasOff := g.ackOff
		g.ackOff = false
		frameID := binary.LittleEndian.Uint32(data[4:])
		for i, frame := range g.inFlight {
			if frame.id == frameID {
				g.inFlight = g.inFlight[i+1:]
				g.adaptAcknowledgments(queueDepth, frame, time.Now())
				g.notifyChanged()

				return nil
			}
		}
		// A normal acknowledgement opts the client back in after suspension,
		// even though frames sent while acknowledgements were off are untracked.
		if wasOff {
			g.notifyChanged()
		}
	case 0x10: // Optional persistent cache offer; we import no cache entries.
		if len(data) < 2 || len(data) != 2+int(binary.LittleEndian.Uint16(data))*12 {
			return errors.New("rdp: invalid graphics cache offer")
		}

		return s.writeGraphics(graphicsPDU(0x11, []byte{0, 0}))
	case 0x16: // RDPGFX_QOE_FRAME_ACKNOWLEDGE: no quality adaptation yet.
		if len(data) != 12 {
			return errors.New("rdp: invalid graphics quality acknowledgment")
		}
	default:
		return errors.New("rdp: unsupported client graphics command")
	}

	return nil
}

func (g *graphicsState) observeFrame(bytes int) {
	sample := uint64(bytes)
	if g.averageBytes == 0 {
		g.averageBytes = sample

		return
	}
	g.averageBytes = (7*g.averageBytes + sample) / 8
}

func (g *graphicsState) resetAcknowledgments() {
	g.ackGeneration++
	g.inFlight = nil
	g.averageBytes = 0
	g.ackTurnaround = 0
	g.frameInterval = 0
}

func (g *graphicsState) adaptAcknowledgments(queueDepth uint32, frame graphicsPendingFrame, now time.Time) {
	base := g.baseInterval
	if base <= 0 {
		base = graphicsMinFrameInterval
	}
	// An ACK can race the final transport Write returning. Retire that frame
	// normally, but sample turnaround only after its send completion is known.
	if elapsed := now.Sub(frame.sentAt); !frame.sentAt.IsZero() && elapsed > 0 {
		if g.ackTurnaround == 0 {
			g.ackTurnaround = elapsed
		} else {
			g.ackTurnaround += (elapsed - g.ackTurnaround) / 8
		}
	}

	target := base
	if turnaround := g.ackTurnaround / graphicsMaxFrames; turnaround > target {
		target = turnaround
	}
	if queueDepth != 0 && g.averageBytes != 0 {
		buffered := (uint64(queueDepth) + g.averageBytes - 1) / g.averageBytes
		maxBuffered := uint64(0)
		if base < graphicsMaxFrameInterval {
			maxBuffered = uint64(graphicsMaxFrameInterval/base) - 1
		}
		buffered = min(buffered, maxBuffered)
		target = max(target, base*time.Duration(1+buffered))
	}
	target = min(target, graphicsMaxFrameInterval)

	current := max(g.frameInterval, base)
	if target >= current {
		g.frameInterval = target

		return
	}
	// Recover by at least a quarter frame interval, or one quarter of the
	// remaining gap. This avoids an immediate burst after congestion clears.
	step := max(base/4, (current-target)/4)
	g.frameInterval = max(target, current-step)
}

func selectGraphicsCapabilities(data []byte) (uint32, uint32, error) {
	if len(data) < 2 {
		return 0, 0, errors.New("rdp: truncated graphics capabilities")
	}
	count := int(binary.LittleEndian.Uint16(data))
	if count == 0 || count > 64 {
		return 0, 0, errors.New("rdp: invalid graphics capability count")
	}
	data = data[2:]
	var selected, selectedFlags uint32
	for range count {
		if len(data) < 8 {
			return 0, 0, errors.New("rdp: truncated graphics capability")
		}
		version, size := binary.LittleEndian.Uint32(data), binary.LittleEndian.Uint32(data[4:])
		if uint64(size) > uint64(len(data)-8) {
			return 0, 0, errors.New("rdp: invalid graphics capability size")
		}
		if size == 4 {
			flags := binary.LittleEndian.Uint32(data[8:])
			if version == graphicsVersion81 && flags&0x10 != 0 {
				selected, selectedFlags = version, flags&3|0x10
			} else if selected != graphicsVersion81 && graphicsVersionSupportsAVC(version) && flags&0x20 == 0 {
				selected, selectedFlags = version, flags&0x43
			}
		}
		data = data[8+size:]
	}
	if len(data) != 0 {
		return 0, 0, errors.New("rdp: trailing graphics capabilities")
	}

	return selected, selectedFlags, nil
}

func graphicsVersionSupportsAVC(version uint32) bool {
	switch version {
	case 0x000a0002, 0x000a0200, 0x000a0301, 0x000a0400, 0x000a0502, 0x000a0600, 0x000a0701:
		return true
	default:
		return false
	}
}

func graphicsPDU(command uint16, payload []byte) []byte {
	data := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint16(data, command)
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)))
	copy(data[8:], payload)

	return data
}

func graphicsInitialization(width, height int, version, flags uint32) []byte {
	caps := make([]byte, 12)
	binary.LittleEndian.PutUint32(caps, version)
	binary.LittleEndian.PutUint32(caps[4:], 4)
	binary.LittleEndian.PutUint32(caps[8:], flags)
	data := graphicsPDU(0x13, caps)

	return append(data, graphicsSurface(width, height)...)
}

func graphicsSurface(width, height int) []byte {
	var data []byte
	reset := make([]byte, 332)
	binary.LittleEndian.PutUint32(reset, uint32(width))
	binary.LittleEndian.PutUint32(reset[4:], uint32(height))
	binary.LittleEndian.PutUint32(reset[8:], 1)
	binary.LittleEndian.PutUint32(reset[20:], uint32(width-1))
	binary.LittleEndian.PutUint32(reset[24:], uint32(height-1))
	binary.LittleEndian.PutUint32(reset[28:], 1)
	data = append(data, graphicsPDU(0x0e, reset)...)
	create := make([]byte, 7)
	put16(create, 2, width)
	put16(create, 4, height)
	create[6] = 0x20
	data = append(data, graphicsPDU(9, create)...)

	return append(data, graphicsPDU(0x0f, make([]byte, 12))...)
}

// Annex B uses both three-byte and four-byte start codes. An emulation-
// prevention byte keeps these delimiters out of NAL payloads.
func avc420Keyframe(annexB []byte) bool {
	for index := 0; index+3 < len(annexB); index++ {
		if annexB[index] == 0 && annexB[index+1] == 0 && annexB[index+2] == 1 && annexB[index+3]&31 == 5 {
			return true
		}
	}

	return false
}

// avc420MaxPayload reserves the complete graphics frame envelope and the
// largest possible ZGFX segmentation overhead, keeping the DVC message bounded.
func avc420MaxPayload(rectCount int) int {
	const segmentOverhead = 7 + 5*((graphicsMaxMessage+65534)/65535)

	return graphicsMaxMessage - segmentOverhead - 57 - 10*rectCount
}

func avc420Regions(width, height int, regions []image.Rectangle) []image.Rectangle {
	bounds := image.Rect(0, 0, width, height)
	if len(regions) == 0 || len(regions) > graphicsMaxRegions {
		return []image.Rectangle{bounds}
	}
	result := make([]image.Rectangle, 0, len(regions))
	for _, rect := range regions {
		rect = rect.Intersect(bounds)
		if rect.Empty() {
			continue
		}
		// Include neighboring samples affected by chroma and filtering, then
		// repaint complete H.264 macroblocks. Right/bottom remain exclusive.
		rect = damage.Align(rect.Inset(-1), 16, bounds)
		// Expansion can make originally disjoint damage overlap. FreeRDP's
		// threaded YUV conversion skips an entire rectangle when it overlaps
		// a later one, leaving stale pixels. Send disjoint repaint regions.
		for index := 0; index < len(result); {
			if !rect.Overlaps(result[index]) {
				index++

				continue
			}
			rect = rect.Union(result[index])
			result = append(result[:index], result[index+1:]...)
			// A bounding union can overlap regions we already examined.
			index = 0
		}
		if rect == bounds {
			return []image.Rectangle{bounds}
		}
		result = append(result, rect)
	}
	if len(result) == 0 {
		return []image.Rectangle{bounds}
	}

	return result
}

func avc420Frame(width, height int, frameID, timestamp uint32, annexB []byte) []byte {
	return avc420FrameDamage(width, height, frameID, timestamp, annexB,
		[]image.Rectangle{image.Rect(0, 0, width, height)})
}

// regions have already been normalized by WriteAVC420Damage. MS-RDPEGFX
// 2.2.4.4.1 places all RECT16 entries before the array of QP/quality pairs.
func avc420FrameDamage(width, height int, frameID, timestamp uint32,
	annexB []byte, regions []image.Rectangle,
) []byte {
	metadata := make([]byte, 57+10*len(regions))
	header, end := avc420FrameMetadata(metadata, width, height, frameID, timestamp, len(annexB), regions)
	data := make([]byte, 0, len(metadata)+len(annexB))
	data = append(data, header...)
	data = append(data, annexB...)

	return append(data, end...)
}

// avc420FrameMetadata fills only the small frame header and trailer. The
// access unit remains separate until the transport fragments are assembled.
func avc420FrameMetadata(data []byte, width, height int, frameID, timestamp uint32,
	payloadLength int, regions []image.Rectangle,
) ([]byte, []byte) {
	metadataLength := 4 + 10*len(regions)
	wireLength := 17 + metadataLength
	data = data[:16+8+wireLength+12]
	clear(data)

	start := data[:16]
	binary.LittleEndian.PutUint16(start, 0x0b)
	binary.LittleEndian.PutUint32(start[4:], uint32(len(start)))
	binary.LittleEndian.PutUint32(start[8:], timestamp)
	binary.LittleEndian.PutUint32(start[12:], frameID)

	wirePDU := data[16 : 16+8+wireLength]
	binary.LittleEndian.PutUint16(wirePDU, 1)
	binary.LittleEndian.PutUint32(wirePDU[4:], uint32(len(wirePDU)+payloadLength))
	// WIRE_TO_SURFACE_1 describes the complete coded desktop; its AVC420
	// metadata mask tells the client which decoded regions to repaint.
	wire := wirePDU[8:]
	put16(wire, 2, 0x0b)
	wire[4] = 0x20
	put16(wire, 9, width)
	put16(wire, 11, height)
	binary.LittleEndian.PutUint32(wire[13:], uint32(metadataLength+payloadLength))
	binary.LittleEndian.PutUint32(wire[17:], uint32(len(regions)))
	for index, rect := range regions {
		offset := 21 + 8*index
		put16(wire, offset, rect.Min.X)
		put16(wire, offset+2, rect.Min.Y)
		put16(wire, offset+4, rect.Max.X)
		put16(wire, offset+6, rect.Max.Y)
	}
	for index := range regions {
		offset := 21 + 8*len(regions) + 2*index
		// Quantization and quality are advisory progressive-rendering metadata.
		wire[offset], wire[offset+1] = 22, 100
	}

	end := data[len(data)-12:]
	binary.LittleEndian.PutUint16(end, 0x0c)
	binary.LittleEndian.PutUint32(end[4:], uint32(len(end)))
	binary.LittleEndian.PutUint32(end[8:], frameID)

	return data[:len(data)-12], end
}
