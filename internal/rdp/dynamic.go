//nolint:err113 // Protocol errors identify malformed wire structures for diagnostic logging.
package rdp

import (
	"encoding/binary"
	"errors"
	"sync"
)

const dynamicTransportBatchSize = 16 << 10

type dynamicEndpoint struct {
	phase byte // 0 disabled, 1 requested, 2 opening, 3 open, 4 closed
	data  fragmentBuffer
}

// dynamicState owns the single drdynvc transport and its two independent
// logical channels. The write lock prevents fragmented messages interleaving.
type dynamicState struct {
	mu         sync.Mutex
	writeMu    sync.Mutex
	writeBatch []byte // Reused under writeMu.
	channel    uint16
	phase      byte // 0 idle, 1 capability exchange, 2 ready
	static     fragmentBuffer
	graphics   dynamicEndpoint
	display    dynamicEndpoint
}

func (d *dynamicState) endpoint(id uint32) *dynamicEndpoint {
	switch id {
	case graphicsChannelID:
		return &d.graphics
	case displayChannelID:
		return &d.display
	default:
		return nil
	}
}

func (s *Session) beginDynamic(id uint32) (bool, error) {
	d := s.dynamic
	if d == nil || d.channel == 0 || !s.joined[d.channel] {
		return false, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	endpoint := d.endpoint(id)
	if endpoint.phase != 0 {
		return endpoint.phase != 4, nil
	}
	endpoint.phase = 1
	if d.phase == 0 {
		d.phase = 1

		return true, s.writeStatic(d.channel, []byte{0x50, 0, 1, 0})
	}
	if d.phase == 2 {
		return true, s.openDynamic(id)
	}

	return true, nil
}

func (s *Session) openDynamic(id uint32) error {
	d := s.dynamic
	name := graphicsChannelName
	if id == displayChannelID {
		name = displayChannelName
	}
	d.endpoint(id).phase = 2

	return s.writeStatic(d.channel, append([]byte{0x10, byte(id)}, name...))
}

func (s *Session) readDynamicChannel(channel uint16, data []byte) error {
	d := s.dynamic
	if d == nil || channel != d.channel {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.phase == 0 {
		return nil
	}
	message, err := d.static.static(data)
	if err != nil || message == nil {
		return err
	}

	return s.readDynamic(message)
}

// readDynamic is called by the sole network reader with dynamic.mu held.
func (s *Session) readDynamic(data []byte) error {
	if len(data) == 0 {
		return errors.New("rdp: empty dynamic channel PDU")
	}
	d := s.dynamic
	header := data[0]
	command := header >> 4
	if command == 5 {
		if d.phase != 1 || len(data) != 4 || header != 0x50 || binary.LittleEndian.Uint16(data[2:]) != 1 {
			return errors.New("rdp: invalid dynamic channel capability response")
		}
		d.phase = 2
		for _, id := range []uint32{graphicsChannelID, displayChannelID} {
			if d.endpoint(id).phase == 1 {
				if err := s.openDynamic(id); err != nil {
					return err
				}
			}
		}

		return nil
	}
	id, payload, err := readDynamicInteger(data[1:], header&3)
	if err != nil {
		return err
	}
	endpoint := d.endpoint(id)
	if endpoint == nil || endpoint.phase == 0 {
		return errors.New("rdp: unknown dynamic channel")
	}
	if endpoint.phase == 4 {
		return nil
	}
	switch command {
	case 1: // DYNVC_CREATE_RSP
		if endpoint.phase != 2 || len(payload) != 4 {
			return errors.New("rdp: invalid dynamic channel create response")
		}
		if binary.LittleEndian.Uint32(payload) != 0 {
			endpoint.phase = 4

			return nil
		}
		endpoint.phase = 3
		if id == displayChannelID {
			return s.writeDynamic(id, displayCapabilities())
		}
	case 2, 3: // DYNVC_DATA_FIRST / DYNVC_DATA
		if endpoint.phase != 3 {
			return errors.New("rdp: data before dynamic channel creation")
		}
		message, err := readDynamicFragment(endpoint, header, payload)
		if err != nil || message == nil {
			return err
		}
		if id == displayChannelID {
			s.readDisplayControl(message)

			return nil
		}
		g := s.graphics
		g.mu.Lock()
		defer g.mu.Unlock()
		if !g.closed {
			return s.readGraphics(message)
		}
	case 4: // DYNVC_CLOSE
		if len(payload) != 0 {
			return errors.New("rdp: invalid dynamic channel close")
		}
		endpoint.phase = 4
		endpoint.data = fragmentBuffer{}
		if id == graphicsChannelID {
			g := s.graphics
			g.mu.Lock()
			g.ready, g.closed = false, true
			g.resetAcknowledgments()
			g.notifyChanged()
			g.mu.Unlock()
			// A frame can already be reserved but still under construction.
			// Wait for its writer before replying, without retaining state locks.
			g.writeMu.Lock()
			defer g.writeMu.Unlock()
		}

		// Finish an outgoing fragmented message before acknowledging close.
		// Its sender no longer holds graphics.mu across transport writes.
		d.writeMu.Lock()
		defer d.writeMu.Unlock()

		return s.writeStatic(d.channel, []byte{0x40, byte(id)})
	default:
		return errors.New("rdp: unsupported dynamic channel command")
	}

	return nil
}

func readDynamicFragment(endpoint *dynamicEndpoint, header byte, payload []byte) ([]byte, error) {
	if header>>4 == 2 {
		total, fragment, err := readDynamicInteger(payload, (header>>2)&3)
		if err != nil {
			return nil, err
		}

		return endpoint.data.first(total, fragment)
	}

	return endpoint.data.next(payload)
}

// fragmentBuffer bounds both static-channel and dynamic-channel reassembly.
// The buffer is only allocated after its declared length has been validated.
type fragmentBuffer struct {
	total uint32
	data  []byte
}

func (b *fragmentBuffer) first(total uint32, data []byte) ([]byte, error) {
	if b.total != 0 || total == 0 || total > graphicsMaxIncoming || uint64(len(data)) > uint64(total) {
		return nil, errors.New("rdp: invalid channel fragment length or sequence")
	}
	if uint32(len(data)) == total {
		return data, nil
	}
	b.total = total
	b.data = make([]byte, 0, int(total))
	b.data = append(b.data, data...)

	return nil, nil
}

func (b *fragmentBuffer) next(data []byte) ([]byte, error) {
	if b.total == 0 {
		if len(data) == 0 || len(data) > graphicsMaxIncoming {
			return nil, errors.New("rdp: invalid dynamic channel data length")
		}

		return data, nil
	}
	if len(data) == 0 || uint64(len(b.data))+uint64(len(data)) > uint64(b.total) {
		return nil, errors.New("rdp: channel fragment exceeds declared length")
	}
	b.data = append(b.data, data...)
	if uint32(len(b.data)) != b.total {
		return nil, nil
	}
	message := b.data
	b.data, b.total = nil, 0

	return message, nil
}

func (b *fragmentBuffer) static(data []byte) ([]byte, error) {
	if len(data) < 8 {
		return nil, errors.New("rdp: truncated static channel header")
	}
	total, flags := binary.LittleEndian.Uint32(data), binary.LittleEndian.Uint32(data[4:])
	if flags&0x00ff0000 != 0 {
		return nil, errors.New("rdp: unsupported static channel compression")
	}
	var message []byte
	var err error
	if flags&1 != 0 {
		message, err = b.first(total, data[8:])
	} else {
		if b.total == 0 || total != b.total {
			return nil, errors.New("rdp: unexpected static channel continuation")
		}
		message, err = b.next(data[8:])
	}
	if err != nil {
		return nil, err
	}
	if (message != nil) != (flags&2 != 0) {
		return nil, errors.New("rdp: static channel last-fragment mismatch")
	}

	return message, nil
}

func readDynamicInteger(data []byte, code byte) (uint32, []byte, error) {
	if code > 2 || len(data) < 1<<code {
		return 0, nil, errors.New("rdp: invalid dynamic channel integer")
	}
	switch code {
	case 0:
		return uint32(data[0]), data[1:], nil
	case 1:
		return uint32(binary.LittleEndian.Uint16(data)), data[2:], nil
	default:
		return binary.LittleEndian.Uint32(data), data[4:], nil
	}
}

func (s *Session) writeStatic(channel uint16, data []byte) error {
	// Each dynamic fragment is at most 1600 bytes, so the default static
	// channel chunk size suffices and each has FIRST|LAST set.
	if len(data) > 1600 {
		return errors.New("rdp: static channel write exceeds chunk size")
	}
	payload := make([]byte, 8+len(data))
	binary.LittleEndian.PutUint32(payload, uint32(len(data)))
	binary.LittleEndian.PutUint32(payload[4:], 3)
	copy(payload[8:], data)

	return s.writeChannel(channel, payload)
}

func (s *Session) writeGraphics(data []byte) error {
	return s.writeDynamic(graphicsChannelID, segmentGraphics(data))
}

func (s *Session) writeDynamic(id uint32, data []byte) error {
	d := s.dynamic
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	total := len(data)
	first := true
	batch := d.writeBatch[:0]
	if len(data) != 0 && cap(batch) < dynamicTransportBatchSize {
		batch = make([]byte, 0, dynamicTransportBatchSize)
	}
	defer func() { d.writeBatch = batch[:0] }()
	for len(data) > 0 {
		var header [14]byte
		headerLength := 10 // Static-channel header followed by DYNVC_DATA.
		header[8], header[9] = 0x30, byte(id)
		if first && len(data) > 1598 {
			headerLength = 14 // Static-channel header followed by DYNVC_DATA_FIRST.
			header[8], header[9] = 0x28, byte(id)
			binary.LittleEndian.PutUint32(header[10:], uint32(total))
		}
		dynamicHeaderLength := headerLength - 8
		size := min(1600-dynamicHeaderLength, len(data))
		binary.LittleEndian.PutUint32(header[:4], uint32(dynamicHeaderLength+size))
		binary.LittleEndian.PutUint32(header[4:8], 3)
		packetSize := channelTPKTSize(headerLength + size)
		if len(batch) != 0 && len(batch)+packetSize > cap(batch) {
			if err := s.writeWire(batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
		var err error
		batch, err = appendChannelTPKT(batch, d.channel, header[:headerLength], data[:size])
		if err != nil {
			return err
		}
		data = data[size:]
		first = false
	}
	if len(batch) == 0 {
		return nil
	}

	return s.writeWire(batch)
}

// segmentGraphics emits uncompressed RDP 8.0 bulk segments. H.264 already
// compresses the payload; the mandatory ZGFX envelope still applies.
func segmentGraphics(data []byte) []byte {
	const maxSegment = 65535
	if len(data) <= maxSegment {
		return append([]byte{0xe0, 0x04}, data...)
	}
	count := (len(data) + maxSegment - 1) / maxSegment
	output := make([]byte, 7, 7+len(data)+count*5)
	output[0] = 0xe1
	binary.LittleEndian.PutUint16(output[1:], uint16(count))
	binary.LittleEndian.PutUint32(output[3:], uint32(len(data)))
	for len(data) > 0 {
		size := min(maxSegment, len(data))
		header := make([]byte, 5)
		binary.LittleEndian.PutUint32(header, uint32(size+1))
		header[4] = 4
		output = append(output, header...)
		output = append(output, data[:size]...)
		data = data[size:]
	}

	return output
}
