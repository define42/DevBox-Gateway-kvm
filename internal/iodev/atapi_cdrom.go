package iodev

import (
	"encoding/binary"
	"io"
	"log"
	"os"
)

const (
	ataPrimaryBase    = uint64(0x1f0)
	ataPrimaryControl = uint64(0x3f6)

	ataRegData       = 0x1f0
	ataRegError      = 0x1f1
	ataRegFeatures   = 0x1f1
	ataRegSectorCnt  = 0x1f2
	ataRegLBALow     = 0x1f3
	ataRegLBAMid     = 0x1f4
	ataRegLBAHigh    = 0x1f5
	ataRegDriveHead  = 0x1f6
	ataRegStatus     = 0x1f7
	ataRegCommand    = 0x1f7
	ataRegAltStatus  = 0x3f6
	ataRegDevControl = 0x3f6

	ataStatusErr  = 0x01
	ataStatusDRQ  = 0x08
	ataStatusDSC  = 0x10
	ataStatusDRDY = 0x40
	ataStatusBSY  = 0x80

	ataErrABRT = 0x04

	ataCmdDeviceReset    = 0x08
	ataCmdExecuteDiag    = 0x90
	ataCmdPacket         = 0xa0
	ataCmdIdentifyPacket = 0xa1
	ataCmdIdentifyDevice = 0xec

	atapiSectorSize = 2048
)

// ATAPICDROM is a small read-only primary-master ATAPI CD-ROM. It implements
// enough of the packet command set for BIOS firmware to find an ISO9660 disc
// and execute its El Torito boot entry.
type ATAPICDROM struct {
	r      io.ReaderAt
	size   int64
	inject func() error

	errorReg    byte
	featuresReg byte
	sectorCount byte
	lbaLow      byte
	lbaMid      byte
	lbaHigh     byte
	driveHead   byte
	status      byte
	devControl  byte

	data       []byte
	dataOffset int

	awaitingPacket bool
	packet         []byte

	senseKey  byte
	senseASC  byte
	senseASCQ byte
}

type atapiCDROMPorts struct {
	dev  *ATAPICDROM
	base uint64
	size uint64
}

func NewATAPICDROM(r io.ReaderAt, size int64, inject func() error) []Device {
	dev := &ATAPICDROM{
		r:      r,
		size:   size,
		inject: inject,
	}
	dev.reset()

	return []Device{
		&atapiCDROMPorts{dev: dev, base: ataPrimaryBase, size: 8},
		&atapiCDROMPorts{dev: dev, base: ataPrimaryControl, size: 1},
	}
}

func (p *atapiCDROMPorts) Read(port uint64, data []byte) error {
	return p.dev.Read(port, data)
}

func (p *atapiCDROMPorts) Write(port uint64, data []byte) error {
	return p.dev.Write(port, data)
}

func (p *atapiCDROMPorts) IOPort() uint64 { return p.base }

func (p *atapiCDROMPorts) Size() uint64 { return p.size }

func (c *ATAPICDROM) Read(port uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	if c.slaveSelected() {
		for i := range data {
			data[i] = 0
		}

		return nil
	}

	switch port {
	case ataRegData:
		c.readData(data)
	case ataRegError:
		data[0] = c.errorReg
	case ataRegSectorCnt:
		data[0] = c.sectorCount
	case ataRegLBALow:
		data[0] = c.lbaLow
	case ataRegLBAMid:
		data[0] = c.lbaMid
	case ataRegLBAHigh:
		data[0] = c.lbaHigh
	case ataRegDriveHead:
		data[0] = c.driveHead
	case ataRegStatus, ataRegAltStatus:
		data[0] = c.status
	default:
		for i := range data {
			data[i] = 0xff
		}
	}

	return nil
}

func (c *ATAPICDROM) Write(port uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	switch port {
	case ataRegData:
		c.writeData(data)
	case ataRegFeatures:
		c.featuresReg = data[0]
	case ataRegSectorCnt:
		c.sectorCount = data[0]
	case ataRegLBALow:
		c.lbaLow = data[0]
	case ataRegLBAMid:
		c.lbaMid = data[0]
	case ataRegLBAHigh:
		c.lbaHigh = data[0]
	case ataRegDriveHead:
		c.driveHead = data[0]
	case ataRegCommand:
		c.command(data[0])
	case ataRegDevControl:
		c.devControl = data[0]
		if data[0]&0x04 != 0 {
			c.reset()
		}
	default:
	}

	return nil
}

func (c *ATAPICDROM) reset() {
	c.errorReg = 0x01
	c.featuresReg = 0
	c.sectorCount = 0x01
	c.lbaLow = 0x01
	c.lbaMid = 0x14
	c.lbaHigh = 0xeb
	c.driveHead = 0xa0
	c.status = ataStatusDRDY | ataStatusDSC
	c.data = nil
	c.dataOffset = 0
	c.awaitingPacket = false
	c.packet = nil
	c.clearSense()
}

func (c *ATAPICDROM) slaveSelected() bool {
	return c.driveHead&0x10 != 0
}

func (c *ATAPICDROM) command(cmd byte) {
	if c.slaveSelected() {
		return
	}

	c.tracef("cmd %#x", cmd)

	switch cmd {
	case ataCmdDeviceReset:
		c.reset()
		c.interrupt()
	case ataCmdExecuteDiag:
		c.errorReg = 0x01
		c.status = ataStatusDRDY | ataStatusDSC
		c.interrupt()
	case ataCmdIdentifyDevice:
		c.abort()
	case ataCmdIdentifyPacket:
		c.prepareData(c.identifyPacket(), 0)
	case ataCmdPacket:
		c.awaitingPacket = true
		c.packet = c.packet[:0]
		c.errorReg = 0
		c.status = ataStatusDRDY | ataStatusDRQ
		c.sectorCount = 0x01 // CoD=1, IO=0: host writes command packet.
	default:
		c.abort()
	}
}

func (c *ATAPICDROM) abort() {
	c.errorReg = ataErrABRT
	c.status = ataStatusDRDY | ataStatusDSC | ataStatusErr
	c.setSense(0x05, 0x20) // Illegal request, invalid command operation code.
	c.interrupt()
}

func (c *ATAPICDROM) readData(dst []byte) {
	for i := range dst {
		if c.dataOffset >= len(c.data) {
			dst[i] = 0

			continue
		}

		dst[i] = c.data[c.dataOffset]
		c.dataOffset++
	}

	if c.dataOffset >= len(c.data) {
		c.data = nil
		c.dataOffset = 0
		c.status = ataStatusDRDY | ataStatusDSC
		c.sectorCount = 0x03 // CoD=1, IO=1: command complete.
	}
}

func (c *ATAPICDROM) writeData(src []byte) {
	if !c.awaitingPacket {
		return
	}

	c.packet = append(c.packet, src...)
	if len(c.packet) < 12 {
		return
	}

	packet := make([]byte, 12)
	copy(packet, c.packet[:12])
	c.awaitingPacket = false
	c.packet = nil
	c.handlePacket(packet)
}

func (c *ATAPICDROM) handlePacket(cmd []byte) {
	c.tracef("packet %#x", cmd[0])

	switch cmd[0] {
	case 0x00: // TEST UNIT READY
		c.clearSense()
		c.finishPacket(nil, 0)
	case 0x03: // REQUEST SENSE
		c.finishPacket(c.requestSense(), int(cmd[4]))
		c.clearSense()
	case 0x12: // INQUIRY
		c.clearSense()
		c.finishPacket(c.inquiry(), int(cmd[4]))
	case 0x1a: // MODE SENSE(6)
		c.clearSense()
		c.finishPacket([]byte{3, 0, 0, 0}, int(cmd[4]))
	case 0x1b, 0x1e: // START STOP UNIT, PREVENT/ALLOW MEDIUM REMOVAL
		c.clearSense()
		c.finishPacket(nil, 0)
	case 0x25: // READ CAPACITY(10)
		c.clearSense()
		c.finishPacket(c.readCapacity(), 8)
	case 0x28: // READ(10)
		lba := binary.BigEndian.Uint32(cmd[2:6])
		blocks := uint32(binary.BigEndian.Uint16(cmd[7:9]))
		c.readBlocks(lba, blocks)
	case 0x43: // READ TOC/PMA/ATIP
		c.clearSense()
		alloc := int(binary.BigEndian.Uint16(cmd[7:9]))
		c.finishPacket(c.readTOC(cmd[1]&0x02 != 0), alloc)
	case 0x46: // GET CONFIGURATION
		c.clearSense()
		alloc := int(binary.BigEndian.Uint16(cmd[7:9]))
		c.finishPacket(c.getConfiguration(), alloc)
	case 0x51: // READ DISC INFORMATION
		c.clearSense()
		alloc := int(binary.BigEndian.Uint16(cmd[7:9]))
		c.finishPacket(c.readDiscInformation(), alloc)
	case 0xa8: // READ(12)
		lba := binary.BigEndian.Uint32(cmd[2:6])
		blocks := binary.BigEndian.Uint32(cmd[6:10])
		c.readBlocks(lba, blocks)
	default:
		c.setSense(0x05, 0x20)
		c.errorReg = ataErrABRT
		c.status = ataStatusDRDY | ataStatusDSC | ataStatusErr
		c.sectorCount = 0x03
		c.interrupt()
	}
}

func (c *ATAPICDROM) readBlocks(lba, blocks uint32) {
	if blocks == 0 {
		c.clearSense()
		c.finishPacket(nil, 0)

		return
	}

	byteCount := uint64(blocks) * atapiSectorSize
	if byteCount > uint64(int(^uint(0)>>1)) {
		c.setSense(0x05, 0x21)
		c.errorReg = ataErrABRT
		c.status = ataStatusDRDY | ataStatusDSC | ataStatusErr
		c.interrupt()

		return
	}

	data := make([]byte, int(byteCount))
	off := int64(lba) * atapiSectorSize
	n, err := c.r.ReadAt(data, off)
	if err != nil && err != io.EOF {
		c.setSense(0x03, 0x11)
		c.errorReg = ataErrABRT
		c.status = ataStatusDRDY | ataStatusDSC | ataStatusErr
		c.interrupt()

		return
	}

	if n < len(data) {
		clear(data[n:])
	}

	c.clearSense()
	c.finishPacket(data, 0)
}

func (c *ATAPICDROM) finishPacket(data []byte, alloc int) {
	c.errorReg = 0
	c.prepareData(data, alloc)
}

func (c *ATAPICDROM) prepareData(data []byte, alloc int) {
	if alloc > 0 && len(data) > alloc {
		data = data[:alloc]
	}

	maxTransfer := int(binary.LittleEndian.Uint16([]byte{c.lbaMid, c.lbaHigh}))
	if maxTransfer == 0 {
		maxTransfer = 65536
	}
	if len(data) > maxTransfer {
		data = data[:maxTransfer]
	}

	c.data = data
	c.dataOffset = 0

	c.lbaMid = byte(len(data))
	c.lbaHigh = byte(len(data) >> 8)

	if len(data) == 0 {
		c.status = ataStatusDRDY | ataStatusDSC
		c.sectorCount = 0x03
	} else {
		c.status = ataStatusDRDY | ataStatusDRQ
		c.sectorCount = 0x02 // CoD=0, IO=1: data goes to host.
	}

	c.interrupt()
}

func (c *ATAPICDROM) identifyPacket() []byte {
	data := make([]byte, 512)
	putWord := func(idx int, v uint16) {
		binary.LittleEndian.PutUint16(data[idx*2:], v)
	}

	putWord(0, 0x85c0)  // ATAPI, removable CD-ROM, 12-byte packet commands.
	putWord(49, 0x0200) // LBA supported.
	putWord(53, 0x0003) // Words 64-70 and 88 are valid.
	putWord(64, 0x0003) // PIO modes 3 and 4 supported.
	putWord(80, 0x007e) // ATA/ATAPI-4 through ATA/ATAPI-8.
	putWord(82, 0x4000)
	putWord(83, 0x4000)
	putWord(84, 0x4000)
	putWord(93, 0x4041) // Master-only device; no slave shares this channel.

	copyATAString(data[23*2:27*2], "1.0")
	copyATAString(data[27*2:47*2], "gokvm ATAPI CD-ROM")

	return data
}

func (c *ATAPICDROM) inquiry() []byte {
	data := make([]byte, 36)
	data[0] = 0x05 // CD/DVD device.
	data[1] = 0x80 // Removable.
	data[2] = 0x05 // SPC-3.
	data[3] = 0x02 // Response data format.
	data[4] = 31
	copyPadded(data[8:16], "GOKVM")
	copyPadded(data[16:32], "VIRTUAL CD-ROM")
	copyPadded(data[32:36], "1.0")

	return data
}

func (c *ATAPICDROM) requestSense() []byte {
	data := make([]byte, 18)
	data[0] = 0x70
	data[2] = c.senseKey
	data[7] = 10
	data[12] = c.senseASC
	data[13] = c.senseASCQ

	return data
}

func (c *ATAPICDROM) readCapacity() []byte {
	blocks := c.blockCount()
	lastBlock := uint32(0)
	if blocks > 0 {
		lastBlock = blocks - 1
	}

	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[0:4], lastBlock)
	binary.BigEndian.PutUint32(data[4:8], atapiSectorSize)

	return data
}

func (c *ATAPICDROM) readTOC(msf bool) []byte {
	data := make([]byte, 20)
	binary.BigEndian.PutUint16(data[0:2], uint16(len(data)-2))
	data[2] = 1
	data[3] = 1

	data[5] = 0x14
	data[6] = 1
	c.putTOCAddress(data[8:12], 0, msf)

	data[13] = 0x14
	data[14] = 0xaa
	c.putTOCAddress(data[16:20], c.blockCount(), msf)

	return data
}

func (c *ATAPICDROM) getConfiguration() []byte {
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[0:4], uint32(len(data)-4))
	binary.BigEndian.PutUint16(data[6:8], 0x0008) // Current profile: CD-ROM.

	return data
}

func (c *ATAPICDROM) readDiscInformation() []byte {
	data := make([]byte, 34)
	binary.BigEndian.PutUint16(data[0:2], uint16(len(data)-2))
	data[2] = 0x0e // Complete disc, last session complete.
	data[3] = 1
	data[4] = 1
	data[5] = 1

	return data
}

func (c *ATAPICDROM) blockCount() uint32 {
	if c.size <= 0 {
		return 0
	}

	blocks := (uint64(c.size) + atapiSectorSize - 1) / atapiSectorSize
	if blocks > uint64(^uint32(0)) {
		return ^uint32(0)
	}

	return uint32(blocks)
}

func (c *ATAPICDROM) putTOCAddress(dst []byte, lba uint32, msf bool) {
	if !msf {
		binary.BigEndian.PutUint32(dst, lba)

		return
	}

	minutes, seconds, frames := lbaToMSF(lba + 150)
	dst[0] = 0
	dst[1] = minutes
	dst[2] = seconds
	dst[3] = frames
}

func lbaToMSF(lba uint32) (byte, byte, byte) {
	minutes := lba / (60 * 75)
	lba %= 60 * 75
	seconds := lba / 75
	frames := lba % 75

	return byte(minutes), byte(seconds), byte(frames)
}

func (c *ATAPICDROM) clearSense() {
	c.senseKey = 0
	c.senseASC = 0
	c.senseASCQ = 0
}

func (c *ATAPICDROM) setSense(key, asc byte) {
	c.senseKey = key
	c.senseASC = asc
	c.senseASCQ = 0
}

func (c *ATAPICDROM) interrupt() {
	if c.devControl&0x02 != 0 || c.inject == nil {
		return
	}

	_ = c.inject()
}

func (c *ATAPICDROM) tracef(format string, args ...interface{}) {
	if os.Getenv("GOKVM_TRACE_ATAPI") == "" {
		return
	}

	log.Printf("atapi-cdrom: "+format, args...)
}

func copyPadded(dst []byte, s string) {
	for i := range dst {
		dst[i] = ' '
	}
	copy(dst, s)
}

func copyATAString(dst []byte, s string) {
	copyPadded(dst, s)
	for i := 0; i+1 < len(dst); i += 2 {
		dst[i], dst[i+1] = dst[i+1], dst[i]
	}
}
