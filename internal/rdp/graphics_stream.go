package rdp

import "encoding/binary"

const graphicsSegmentSize = 65535

// graphicsSegments streams an uncompressed ZGFX envelope around caller-owned
// parts. Only small headers are buffered; payload bytes go straight into the
// transport batch. All parts remain owned by the caller until writing returns.
type graphicsSegments struct {
	parts        [][]byte
	partOffset   int
	remaining    int
	segmentLeft  int
	header       [7]byte
	pendingStart int
	pendingEnd   int
}

func (s *Session) writeGraphicsParts(parts [][]byte) error {
	var stream graphicsSegments
	stream.parts = parts
	for _, part := range parts {
		stream.remaining += len(part)
	}
	total := stream.remaining
	if total <= graphicsSegmentSize {
		stream.header[0], stream.header[1] = 0xe0, 0x04
		stream.pendingEnd = 2
		stream.segmentLeft = total
		total += 2
	} else {
		count := (total + graphicsSegmentSize - 1) / graphicsSegmentSize
		stream.header[0] = 0xe1
		binary.LittleEndian.PutUint16(stream.header[1:], uint16(count))
		binary.LittleEndian.PutUint32(stream.header[3:], uint32(total))
		stream.pendingEnd = len(stream.header)
		total += 7 + count*5
	}

	return s.writeDynamicPayload(graphicsChannelID, total, stream.fill)
}

func (g *graphicsSegments) fill(dst []byte) {
	for len(dst) != 0 {
		if g.pendingStart != g.pendingEnd {
			n := copy(dst, g.header[g.pendingStart:g.pendingEnd])
			g.pendingStart += n
			dst = dst[n:]

			continue
		}
		if g.segmentLeft == 0 {
			g.segmentLeft = min(graphicsSegmentSize, g.remaining)
			binary.LittleEndian.PutUint32(g.header[:], uint32(g.segmentLeft+1))
			g.header[4] = 4
			g.pendingStart, g.pendingEnd = 0, 5

			continue
		}
		for g.partOffset == len(g.parts[0]) {
			g.parts = g.parts[1:]
			g.partOffset = 0
		}
		part := g.parts[0][g.partOffset:]
		n := copy(dst, part[:min(len(part), g.segmentLeft)])
		g.partOffset += n
		g.segmentLeft -= n
		g.remaining -= n
		dst = dst[n:]
	}
}
