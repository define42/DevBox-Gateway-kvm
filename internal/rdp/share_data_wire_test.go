package rdp

import (
	"bytes"
	"encoding/binary"
	"image"
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
		{"maximum_per", 32749, []byte{0xff, 0xff}},
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
	length, offset := int(wire[13]), 14
	if length&0x80 != 0 {
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
