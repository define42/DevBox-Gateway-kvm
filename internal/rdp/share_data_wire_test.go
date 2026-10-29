package rdp

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

func TestWriteDataPDUWindowsGrantedControlWire(t *testing.T) {
	t.Parallel()
	conn := &transportBatchConn{}
	session := &Session{conn: conn}
	if err := session.WriteDataPDU(0x14, []byte{2, 0, 0xe9, 3, 0xea, 3, 0, 0}); err != nil {
		t.Fatal(err)
	}
	// MS-RDPBCGR's Server Control PDU - Granted Control wire example uses
	// 26 for both totalLength and uncompressedLength, including the header.
	// Use our client ID, zero padding and STREAM_LOW; these are independent
	// of the Windows server length convention being checked here.
	// https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpbcgr/d5624241-aa4d-465a-b66d-698b19d0d826
	want := []byte{
		3, 0, 0, 40, 2, 0xf0, 0x80,
		0x68, 0, 1, 3, 0xeb, 0x70, 26,
		26, 0, 0x17, 0, 0xea, 3, 0xea, 3, 1, 0,
		0, 1, 26, 0, 0x14, 0, 0, 0,
		2, 0, 0xe9, 3, 0xea, 3, 0, 0,
	}
	if got := conn.output.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("Granted Control wire = %x, want %x", got, want)
	}
}

func TestWriteDataPDULargeBitmapWire(t *testing.T) {
	t.Parallel()
	encoder, err := NewBitmapEncoder(64, 64, 24)
	if err != nil {
		t.Fatal(err)
	}
	conn := &transportBatchConn{}
	session := &Session{conn: conn}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	if err := encoder.WriteFrame(img, true, func(payload []byte) error {
		return session.WriteDataPDU(2, payload)
	}); err != nil {
		t.Fatal(err)
	}
	// One 64x64, 24bpp rectangle: 12,288 pixel bytes, 22 bitmap bytes,
	// 18 Share Data header bytes, and a two-byte PER length determinant.
	wire := conn.output.Bytes()
	if len(wire) != 12343 || !bytes.Equal(wire[13:15], []byte{0xb0, 0x28}) {
		t.Fatalf("bitmap transport length = %d; want 12343 and PER length b028", len(wire))
	}
	data := decodeShareDataWire(t, wire, 2)
	wantBitmapHeader := []byte{
		1, 0, 1, 0, 0, 0, 0, 0, 63, 0, 63, 0,
		64, 0, 64, 0, 24, 0, 0, 0, 0, 0x30,
	}
	if !bytes.Equal(data[18:40], wantBitmapHeader) || len(data[40:]) != 12288 {
		t.Fatal("bitmap rectangle does not match its independent wire layout")
	}
}

func TestWriteDataPDUWireLengthBoundaries(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		payloadLength int
		perLength     []byte
	}{
		{"one_byte_per", 109, []byte{0x7f}},
		{"two_byte_per", 110, []byte{0x80, 0x80}},
		// The full MCS structure is limited to 16,383 bytes, leaving 16,375
		// for user data after its six-byte header and two-byte PER length.
		{"maximum_mcs", 16357, []byte{0xbf, 0xf7}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			conn := &transportBatchConn{}
			session := &Session{conn: conn}
			payload := bytes.Repeat([]byte{0x5a}, test.payloadLength)
			if err := session.WriteDataPDU(2, payload); err != nil {
				t.Fatal(err)
			}
			wire := conn.output.Bytes()
			if !bytes.Equal(wire[13:13+len(test.perLength)], test.perLength) {
				t.Fatalf("PER length = %x, want %x", wire[13:15], test.perLength)
			}
			if data := decodeShareDataWire(t, wire, 2); !bytes.Equal(data[18:], payload) {
				t.Fatal("Share Data body changed")
			}
		})
	}
}

func TestRDPWritersRejectOversizedMCS(t *testing.T) {
	t.Parallel()
	// MS-RDPBCGR 2.2 bounds the entire MCS Send Data structure, not just
	// userData. These independent literals exercise one byte over that limit.
	// https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpbcgr/b61e1c76-5d7c-4285-b61a-b69f85e4a0e2
	t.Run("share_data", func(t *testing.T) {
		t.Parallel()
		conn := &transportBatchConn{}
		session := &Session{conn: conn}
		if err := session.WriteDataPDU(2, make([]byte, 16358)); err == nil || conn.output.Len() != 0 {
			t.Fatal("oversized Share Data was accepted or partially written")
		}
	})
	t.Run("channel", func(t *testing.T) {
		t.Parallel()
		conn := &transportBatchConn{}
		session := &Session{conn: conn}
		if err := session.writeChannel(1004, make([]byte, 16376)); err == nil || conn.output.Len() != 0 {
			t.Fatal("oversized channel data was accepted or partially written")
		}
	})
	t.Run("batched_channel", func(t *testing.T) {
		t.Parallel()
		prefix := []byte{0x12, 0x34}
		got, err := appendChannelTPKTHeader(prefix, 1004, make([]byte, 8), 16368)
		if err == nil || !bytes.Equal(got, prefix) {
			t.Fatal("oversized batched channel data was accepted or changed the batch")
		}
	})
}

func TestWriteDataPDU32BitBitmapWire(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		size       image.Point
		rectangles []image.Rectangle
	}{
		{"full_tile", image.Pt(64, 64), []image.Rectangle{image.Rect(0, 0, 64, 63), image.Rect(0, 63, 64, 64)}},
		{"edge_tiles", image.Pt(67, 65), []image.Rectangle{
			image.Rect(0, 0, 64, 63), image.Rect(0, 63, 64, 64), image.Rect(64, 0, 67, 64),
			image.Rect(0, 64, 64, 65), image.Rect(64, 64, 67, 65),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			frame := image.NewRGBA(image.Rectangle{Max: test.size})
			for y := range test.size.Y {
				for x := range test.size.X {
					frame.SetRGBA(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 5), B: uint8(x ^ y), A: 255})
				}
			}
			encoder, err := NewBitmapEncoder(test.size.X, test.size.Y, 32)
			if err != nil {
				t.Fatal(err)
			}
			conn := &transportBatchConn{}
			session := &Session{conn: conn}
			if err := encoder.WriteFrame(frame, true, func(payload []byte) error {
				return session.WriteDataPDU(2, payload)
			}); err != nil {
				t.Fatal(err)
			}
			check32BitBitmapWire(t, conn.output.Bytes(), frame, test.rectangles)
		})
	}
}

func check32BitBitmapWire(t *testing.T, wire []byte, frame *image.RGBA, rectangles []image.Rectangle) {
	t.Helper()
	decoded := image.NewRGBA(frame.Rect)
	for index, rect := range rectangles {
		if len(wire) < 4 {
			t.Fatalf("missing bitmap rectangle %d", index)
		}
		size := int(binary.BigEndian.Uint16(wire[2:4]))
		if size > len(wire) {
			t.Fatal("truncated bitmap TPKT")
		}
		data := decodeShareDataWire(t, wire[:size], 2)
		bitmap := data[18:]
		if len(bitmap) < 22 {
			t.Fatal("truncated bitmap rectangle")
		}
		got := image.Rect(int(binary.LittleEndian.Uint16(bitmap[4:])), int(binary.LittleEndian.Uint16(bitmap[6:])),
			int(binary.LittleEndian.Uint16(bitmap[8:]))+1, int(binary.LittleEndian.Uint16(bitmap[10:]))+1)
		if got != rect {
			t.Fatalf("rectangle %d = %v, want %v", index, got, rect)
		}
		decodeTestBitmap(t, decoded, bitmap, 32)
		wire = wire[size:]
	}
	if len(wire) != 0 {
		t.Fatal("unexpected duplicate bitmap rectangles")
	}
	if !bytes.Equal(decoded.Pix, frame.Pix) {
		t.Fatal("split bitmap changed pixels, row order, or edge coverage")
	}
}

// Decode the wire directly, without the production transport/PER/Share Data
// readers, so matching encoder and decoder mistakes cannot hide length errors.
func decodeShareDataWire(t *testing.T, wire []byte, pduType byte) []byte {
	t.Helper()
	if len(wire) < 32 || !bytes.Equal(wire[:2], []byte{3, 0}) ||
		int(binary.BigEndian.Uint16(wire[2:4])) != len(wire) {
		t.Fatal("invalid TPKT header or length")
	}
	if !bytes.Equal(wire[4:13], []byte{2, 0xf0, 0x80, 0x68, 0, 1, 3, 0xeb, 0x70}) {
		t.Fatal("invalid X.224/MCS header, initiator or global channel")
	}
	if len(wire)-7 > 16383 {
		t.Fatal("MCS Send Data exceeds the 16,383-byte RDP limit")
	}
	length, offset := int(wire[13]), 14
	if length&0x80 != 0 {
		if length&0xc0 != 0x80 {
			t.Fatal("MCS user data uses a fragmented PER length determinant")
		}
		length, offset = (length&0x7f)<<8|int(wire[14]), 15
	}
	data := wire[offset:]
	if length != len(data) || len(data) < 18 || int(binary.LittleEndian.Uint16(data[:2])) != length {
		t.Fatal("MCS user data and Share Control lengths disagree")
	}
	if !bytes.Equal(data[2:12], []byte{0x17, 0, 0xea, 3, 0xea, 3, 1, 0, 0, 1}) {
		t.Fatal("invalid Share Data type, source, share ID or stream")
	}
	if got := int(binary.LittleEndian.Uint16(data[12:14])); got != length {
		t.Fatalf("server uncompressedLength = %d, want complete Share Data PDU length %d", got, length)
	}
	if data[14] != pduType || !bytes.Equal(data[15:18], []byte{0, 0, 0}) {
		t.Fatal("incorrect PDU type or unexpected compression")
	}

	return data
}
