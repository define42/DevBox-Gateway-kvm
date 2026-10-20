package iodev

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestATAPIDiscoveryPackets(t *testing.T) {
	t.Parallel()
	inquiry := []byte("\x05\x80\x05\x02\x1f\x00\x00\x00DEVBOX  VIRTUAL CD-ROM  1.0 ")
	discInfo := make([]byte, 34)
	copy(discInfo, []byte{0, 32, 0x0e, 1, 1, 1})
	for _, test := range []struct {
		name       string
		opcode     byte
		flags      byte
		allocation byte
		want       []byte
	}{
		{name: "inquiry", opcode: 0x12, allocation: 36, want: inquiry},
		{name: "short inquiry", opcode: 0x12, allocation: 8, want: inquiry[:8]},
		{name: "mode sense", opcode: 0x1a, allocation: 4, want: []byte{3, 0, 0, 0}},
		{name: "configuration", opcode: 0x46, allocation: 8, want: []byte{0, 0, 0, 4, 0, 0, 0, 8}},
		{name: "disc information", opcode: 0x51, allocation: 34, want: discInfo},
		{name: "TOC LBA", opcode: 0x43, allocation: 20, want: []byte{
			0, 18, 1, 1, 0, 0x14, 1, 0, 0, 0, 0, 0, 0, 0x14, 0xaa, 0, 0, 0, 0, 76,
		}},
		{name: "TOC MSF", opcode: 0x43, flags: 2, allocation: 20, want: []byte{
			0, 18, 1, 1, 0, 0x14, 1, 0, 0, 0, 2, 0, 0, 0x14, 0xaa, 0, 0, 0, 3, 1,
		}},
		{name: "ready", opcode: 0},
		{name: "start stop", opcode: 0x1b},
		{name: "medium removal", opcode: 0x1e},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dev := newTestATAPICDROM(bytes.NewReader(nil), 76*atapiSectorSize)
			packet := make([]byte, 12)
			packet[0], packet[1] = test.opcode, test.flags
			if test.opcode == 0x12 || test.opcode == 0x1a {
				packet[4] = test.allocation
			} else {
				packet[8] = test.allocation
			}
			sendPacket(t, dev, packet)
			if got := readATAPIReply(t, dev, len(test.want)); !bytes.Equal(got, test.want) {
				t.Fatalf("packet %#x reply = %x, want %x", test.opcode, got, test.want)
			}
		})
	}
}

func TestATAPIErrorSenseIsReportedAndCleared(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		issue  func(*testing.T, *ATAPICDROM)
		key    byte
		asc    byte
		reader io.ReaderAt
	}{
		{name: "unknown packet", key: 5, asc: 0x20, issue: func(t *testing.T, dev *ATAPICDROM) {
			t.Helper()
			sendPacket(t, dev, []byte{0xff, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
		}},
		{name: "ATA identify device", key: 5, asc: 0x20, issue: func(t *testing.T, dev *ATAPICDROM) {
			t.Helper()
			writeATAPI(t, dev, ataRegCommand, []byte{ataCmdIdentifyDevice})
		}},
		{name: "unknown ATA command", key: 5, asc: 0x20, issue: func(t *testing.T, dev *ATAPICDROM) {
			t.Helper()
			writeATAPI(t, dev, ataRegCommand, []byte{0xff})
		}},
		{
			name: "medium read error", key: 3, asc: 0x11, reader: failingATAPIReader{},
			issue: func(t *testing.T, dev *ATAPICDROM) {
				t.Helper()
				sendPacket(t, dev, []byte{0x28, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reader := test.reader
			if reader == nil {
				reader = bytes.NewReader(nil)
			}
			dev := NewATAPICDROM(reader, 0, nil)[0].(*atapiCDROMPorts).dev
			test.issue(t, dev)
			if status := atapiRegister(t, dev, ataRegStatus); status&ataStatusErr == 0 {
				t.Fatalf("failed command status = %#x", status)
			}
			if got := atapiRegister(t, dev, ataRegError); got != ataErrABRT {
				t.Fatalf("failed command error register = %#x", got)
			}
			for attempt := range 2 {
				sendPacket(t, dev, []byte{3, 0, 0, 0, 18, 0, 0, 0, 0, 0, 0, 0})
				data := readATAPIReply(t, dev, 18)
				key, asc := test.key, test.asc
				if attempt == 1 {
					key, asc = 0, 0
				}
				if data[0] != 0x70 || data[2] != key || data[7] != 10 || data[12] != asc || data[13] != 0 {
					t.Fatalf("sense attempt %d = %x, want key=%#x ASC=%#x", attempt, data, key, asc)
				}
			}
		})
	}
}

func TestATAPIRead12ShortMediaAndZeroCount(t *testing.T) {
	t.Parallel()
	dev := newTestATAPICDROM(bytes.NewReader([]byte("short sector")), 12)
	packet := make([]byte, 12)
	packet[0] = 0xa8
	binary.BigEndian.PutUint32(packet[6:10], 1)
	sendPacket(t, dev, packet)
	want := make([]byte, atapiSectorSize)
	copy(want, "short sector")
	if got := readATAPIReply(t, dev, len(want)); !bytes.Equal(got, want) {
		t.Fatal("short media read was not zero-filled")
	}
	binary.BigEndian.PutUint32(packet[6:10], 0)
	sendPacket(t, dev, packet)
	readATAPIReply(t, dev, 0)
}

func TestATAPIPartialPacketsAndTransferLimit(t *testing.T) {
	t.Parallel()
	dev := newTestATAPICDROM(bytes.NewReader(nil), 0)
	writeATAPI(t, dev, ataRegData, []byte{0xff}) // Data outside PACKET must be ignored.
	writeATAPI(t, dev, ataRegLBAMid, []byte{4})
	writeATAPI(t, dev, ataRegLBAHigh, []byte{0})
	writeATAPI(t, dev, ataRegCommand, []byte{ataCmdPacket})
	packet := []byte{0x12, 0, 0, 0, 36, 0, 0, 0, 0, 0, 0, 0}
	writeATAPI(t, dev, ataRegData, packet[:6])
	if reason := atapiRegister(t, dev, ataRegSectorCnt); reason != 1 {
		t.Fatalf("partial command left packet-input phase: %#x", reason)
	}
	writeATAPI(t, dev, ataRegData, packet[6:])
	if got := readATAPIReply(t, dev, 4); !bytes.Equal(got, []byte{5, 0x80, 5, 2}) {
		t.Fatalf("transfer limit was not applied: %x", got)
	}
	if extra := atapiRegister(t, dev, ataRegData); extra != 0 {
		t.Fatalf("finished transfer exposed stale data: %#x", extra)
	}
}

func TestATAPIPortsResetAndInterruptMask(t *testing.T) {
	t.Parallel()
	interrupts := 0
	ports := NewATAPICDROM(bytes.NewReader(nil), 0, func() error {
		interrupts++

		return nil
	})
	if ports[0].IOPort() != 0x1f0 || ports[0].Size() != 8 || ports[1].IOPort() != 0x3f6 || ports[1].Size() != 1 {
		t.Fatal("incorrect primary IDE port layout")
	}
	for _, port := range ports {
		if err := port.Write(port.IOPort(), nil); err != nil {
			t.Fatal(err)
		}
		if err := port.Read(port.IOPort(), nil); err != nil {
			t.Fatal(err)
		}
	}
	dev := ports[0].(*atapiCDROMPorts).dev
	writeATAPI(t, dev, ataRegFeatures, []byte{7})
	writeATAPI(t, dev, ataRegLBALow, []byte{9})
	writeATAPI(t, dev, ataRegSectorCnt, []byte{3})
	if got := atapiRegister(t, dev, ataRegLBALow); got != 9 {
		t.Fatalf("LBA low register = %#x", got)
	}
	writeATAPI(t, dev, ataRegDriveHead, []byte{0xb0})
	writeATAPI(t, dev, ataRegCommand, []byte{ataCmdIdentifyPacket})
	if status := atapiRegister(t, dev, ataRegStatus); status != 0 || interrupts != 0 {
		t.Fatalf("absent slave responded: status=%#x IRQs=%d", status, interrupts)
	}
	writeATAPI(t, dev, ataRegDevControl, []byte{6}) // SRST and nIEN.
	if got := atapiRegister(t, dev, ataRegDriveHead); got != 0xa0 {
		t.Fatalf("reset drive select = %#x", got)
	}
	writeATAPI(t, dev, ataRegCommand, []byte{ataCmdExecuteDiag})
	if interrupts != 0 || atapiRegister(t, dev, ataRegError) != 1 {
		t.Fatal("masked diagnostics did not complete without an IRQ")
	}
	writeATAPI(t, dev, ataRegDevControl, []byte{0})
	writeATAPI(t, dev, ataRegCommand, []byte{ataCmdDeviceReset})
	if interrupts != 1 || atapiRegister(t, dev, ataRegAltStatus) != ataStatusDRDY|ataStatusDSC {
		t.Fatal("unmasked device reset did not complete with an IRQ")
	}
	if atapiRegister(t, dev, ataRegLBAMid) != 0x14 || atapiRegister(t, dev, ataRegLBAHigh) != 0xeb {
		t.Fatal("reset did not restore the ATAPI signature")
	}
	if value := atapiRegister(t, dev, 0x1ef); value != 0xff {
		t.Fatalf("unmapped register = %#x", value)
	}
}

type failingATAPIReader struct{}

func (failingATAPIReader) ReadAt([]byte, int64) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func atapiRegister(t *testing.T, dev *ATAPICDROM, port uint64) byte {
	t.Helper()
	var data [1]byte
	readATAPI(t, dev, port, data[:])

	return data[0]
}

func readATAPIReply(t *testing.T, dev *ATAPICDROM, size int) []byte {
	t.Helper()
	count := int(atapiRegister(t, dev, ataRegLBAMid)) | int(atapiRegister(t, dev, ataRegLBAHigh))<<8
	if count != size || atapiRegister(t, dev, ataRegError) != 0 {
		t.Fatalf("reply size = %d, want %d, error = %#x", count, size, atapiRegister(t, dev, ataRegError))
	}
	if size > 0 && atapiRegister(t, dev, ataRegSectorCnt) != 2 {
		t.Fatal("reply did not enter data-to-host phase")
	}
	data := make([]byte, size)
	readATAPI(t, dev, ataRegData, data)
	if atapiRegister(t, dev, ataRegStatus) != ataStatusDRDY|ataStatusDSC ||
		atapiRegister(t, dev, ataRegSectorCnt) != 3 {
		t.Fatal("reply did not complete its data phase")
	}

	return data
}
