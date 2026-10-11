package iodev

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestATAPICDROMIdentifyPacket(t *testing.T) {
	t.Parallel()

	dev := newTestATAPICDROM(bytes.NewReader(make([]byte, 4096)), 4096)

	writeATAPI(t, dev, ataRegCommand, []byte{ataCmdIdentifyPacket})

	var status [1]byte
	readATAPI(t, dev, ataRegStatus, status[:])
	if status[0]&ataStatusDRQ == 0 {
		t.Fatalf("status %#x does not have DRQ set", status[0])
	}

	data := make([]byte, 512)
	readATAPI(t, dev, ataRegData, data)
	if got := binary.LittleEndian.Uint16(data[0:2]); got != 0x85c0 {
		t.Fatalf("identify word0 = %#x, want 0x85c0", got)
	}
	if got := binary.LittleEndian.Uint16(data[93*2:]); got != 0x4041 {
		t.Fatalf("identify word93 = %#x, want 0x4041", got)
	}

	readATAPI(t, dev, ataRegStatus, status[:])
	if status[0]&ataStatusDRQ != 0 {
		t.Fatalf("status %#x still has DRQ set", status[0])
	}
}

func TestATAPICDROMReadCapacity(t *testing.T) {
	t.Parallel()

	dev := newTestATAPICDROM(bytes.NewReader(make([]byte, 3*atapiSectorSize)), 3*atapiSectorSize)

	packet := make([]byte, 12)
	packet[0] = 0x25
	sendPacket(t, dev, packet)

	data := make([]byte, 8)
	readATAPI(t, dev, ataRegData, data)

	if got := binary.BigEndian.Uint32(data[0:4]); got != 2 {
		t.Fatalf("last block = %d, want 2", got)
	}
	if got := binary.BigEndian.Uint32(data[4:8]); got != atapiSectorSize {
		t.Fatalf("block size = %d, want %d", got, atapiSectorSize)
	}
}

func TestATAPICDROMRead10(t *testing.T) {
	t.Parallel()

	image := make([]byte, 4*atapiSectorSize)
	copy(image[2*atapiSectorSize:], []byte("sector two"))
	dev := newTestATAPICDROM(bytes.NewReader(image), int64(len(image)))

	packet := make([]byte, 12)
	packet[0] = 0x28
	binary.BigEndian.PutUint32(packet[2:6], 2)
	binary.BigEndian.PutUint16(packet[7:9], 1)
	sendPacket(t, dev, packet)

	data := make([]byte, atapiSectorSize)
	readATAPI(t, dev, ataRegData, data)
	if !bytes.HasPrefix(data, []byte("sector two")) {
		t.Fatalf("read data prefix = %q", data[:10])
	}
}

func newTestATAPICDROM(r *bytes.Reader, size int64) *ATAPICDROM {
	devs := NewATAPICDROM(r, size, nil)

	return devs[0].(*atapiCDROMPorts).dev
}

func sendPacket(t *testing.T, dev *ATAPICDROM, packet []byte) {
	t.Helper()

	writeATAPI(t, dev, ataRegLBAMid, []byte{0})
	writeATAPI(t, dev, ataRegLBAHigh, []byte{0})
	writeATAPI(t, dev, ataRegCommand, []byte{ataCmdPacket})
	writeATAPI(t, dev, ataRegData, packet)
}

func readATAPI(t *testing.T, dev *ATAPICDROM, port uint64, data []byte) {
	t.Helper()

	if err := dev.Read(port, data); err != nil {
		t.Fatalf("Read(%#x): %v", port, err)
	}
}

func writeATAPI(t *testing.T, dev *ATAPICDROM, port uint64, data []byte) {
	t.Helper()

	if err := dev.Write(port, data); err != nil {
		t.Fatalf("Write(%#x): %v", port, err)
	}
}
