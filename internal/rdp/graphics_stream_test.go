package rdp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"net"
	"testing"
)

func TestGraphicsFrameStreamWireParity(t *testing.T) {
	t.Parallel()
	for _, size := range []int{
		0, 1, 1528, 1529, 1598, 65535 - 67, 65536 - 67, 65535, 65536, 140000, avc420MaxPayload(128),
	} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Parallel()
			data := make([]byte, size)
			for i := range data {
				data[i] = byte(i * 31)
			}
			for _, count := range []int{1, 128} {
				regions := make([]image.Rectangle, count)
				for i := range regions {
					regions[i] = image.Rect(i*2, i*2, i*2+16, i*2+16)
				}
				oldFrame := avc420FrameDamagePrior(1920, 1080, 37, 51, data, regions)
				var metadata [57 + 10*graphicsMaxRegions]byte
				header, end := avc420FrameMetadata(metadata[:], 1920, 1080, 37, 51, len(data), regions)
				parts := [][]byte{nil, header, nil, data, {}, end, nil}
				wantConn, gotConn := &transportBatchConn{}, &transportBatchConn{}
				want := &Session{conn: wantConn, dynamic: &dynamicState{channel: 1004}}
				got := &Session{conn: gotConn, dynamic: &dynamicState{channel: 1004}}
				if err := want.writeDynamic(graphicsChannelID, segmentGraphics(oldFrame)); err != nil {
					t.Fatal(err)
				}
				for repeat := 0; repeat < 2; repeat++ {
					gotConn.output.Reset()
					if err := got.writeGraphicsParts(parts); err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(gotConn.output.Bytes(), wantConn.output.Bytes()) {
						t.Fatalf("wire changed for %d regions, repeat %d", count, repeat)
					}
				}
				if !bytes.Equal(avc420FrameDamage(1920, 1080, 37, 51, data, regions), oldFrame) {
					t.Fatal("contiguous frame changed")
				}
			}
		})
	}
}

var errGraphicsStreamWrite = errors.New("test transport failure")

type graphicsStreamErrorConn struct {
	net.Conn
	writes, failAt int
}

func (c *graphicsStreamErrorConn) Write(data []byte) (int, error) {
	c.writes++
	if c.writes == c.failAt {
		return 0, errGraphicsStreamWrite
	}

	return len(data), nil
}

func TestGraphicsFrameStreamWriteError(t *testing.T) {
	t.Parallel()
	for _, failAt := range []int{1, 2, 5} {
		conn := &graphicsStreamErrorConn{failAt: failAt}
		session := &Session{conn: conn, dynamic: &dynamicState{channel: 1004}}
		if err := session.writeGraphics(make([]byte, 140000)); !errors.Is(err, errGraphicsStreamWrite) {
			t.Fatalf("failure %d: %v", failAt, err)
		}
		// The lock and reusable buffer must still be usable by the caller's
		// subsequent control write, even after a partial frame was transmitted.
		if err := session.writeDynamic(displayChannelID, []byte{1, 2, 3}); err != nil {
			t.Fatal(err)
		}
	}
}

func BenchmarkGraphicsFrameStream(b *testing.B) {
	for _, size := range []int{64 << 10, 256 << 10, 1 << 20} {
		data := make([]byte, size)
		regions := []image.Rectangle{image.Rect(0, 0, 1920, 1080)}
		for _, prior := range []bool{true, false} {
			b.Run(fmt.Sprintf("bytes=%d/prior=%t", size, prior), func(b *testing.B) {
				session := &Session{conn: &discardTransportConn{}, dynamic: &dynamicState{channel: 1004}}
				b.ReportAllocs()
				b.SetBytes(int64(size))
				for b.Loop() {
					if prior {
						frame := avc420FrameDamagePrior(1920, 1080, 37, 51, data, regions)
						if err := session.writeDynamic(graphicsChannelID, segmentGraphics(frame)); err != nil {
							b.Fatal(err)
						}
					} else {
						var metadata [57 + 10*graphicsMaxRegions]byte
						header, end := avc420FrameMetadata(metadata[:], 1920, 1080, 37, 51, len(data), regions)
						if err := session.writeGraphicsParts([][]byte{header, data, end}); err != nil {
							b.Fatal(err)
						}
					}
				}
			})
		}
	}
}

// Preserved contiguous framing builder, independent of the streamed metadata.
func avc420FrameDamagePrior(width, height int, frameID, timestamp uint32,
	annexB []byte, regions []image.Rectangle,
) []byte {
	// Allocate the complete START_FRAME, WIRE_TO_SURFACE_1 and END_FRAME
	// sequence once. The encoded access unit is the only large copy.
	metadataLength := 4 + 10*len(regions)
	wireLength := 17 + metadataLength + len(annexB)
	data := make([]byte, 16+8+wireLength+12)

	start := data[:16]
	binary.LittleEndian.PutUint16(start, 0x0b)
	binary.LittleEndian.PutUint32(start[4:], uint32(len(start)))
	binary.LittleEndian.PutUint32(start[8:], timestamp)
	binary.LittleEndian.PutUint32(start[12:], frameID)

	wirePDU := data[16 : 16+8+wireLength]
	binary.LittleEndian.PutUint16(wirePDU, 1)
	binary.LittleEndian.PutUint32(wirePDU[4:], uint32(len(wirePDU)))
	// WIRE_TO_SURFACE_1 describes the complete coded desktop; its AVC420
	// metadata mask tells the client which decoded regions to repaint.
	wire := wirePDU[8:]
	put16(wire, 2, 0x0b)
	wire[4] = 0x20
	put16(wire, 9, width)
	put16(wire, 11, height)
	binary.LittleEndian.PutUint32(wire[13:], uint32(metadataLength+len(annexB)))
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
	copy(wire[17+metadataLength:], annexB)

	end := data[len(data)-12:]
	binary.LittleEndian.PutUint16(end, 0x0c)
	binary.LittleEndian.PutUint32(end[4:], uint32(len(end)))
	binary.LittleEndian.PutUint32(end[8:], frameID)

	return data
}
