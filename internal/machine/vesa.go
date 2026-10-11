package machine

import (
	"encoding/binary"
	"log"

	"github.com/define42/gokvm/internal/virtio"
)

const (
	vesaBIOSBase    = 0xc0000
	vesaBIOSSegment = 0xc000
	vesaBIOSEntry   = 0x0010
	vesaInt10Vector = 0x10 * 4

	vesaFramebufferBase        = uint64(0x01000000)
	vesaFramebufferWidth       = 1024
	vesaFramebufferHeight      = 768
	vesaFramebufferBytesPerPix = 4
	vesaFramebufferStride      = vesaFramebufferWidth * vesaFramebufferBytesPerPix
	vesaFramebufferSize        = vesaFramebufferStride * vesaFramebufferHeight
	vesaFramebufferReserveSize = uint64(4 << 20)
	vesaFramebufferEnd         = vesaFramebufferBase + vesaFramebufferReserveSize

	vesaMode = 0x118
)

func (m *Machine) EnableVESA(display virtio.ConsoleDisplay) {
	if display == nil {
		return
	}

	if vesaFramebufferEnd > uint64(len(m.mem)) {
		log.Printf("vesa: framebuffer does not fit in guest memory")

		return
	}

	framebuffer := m.mem[vesaFramebufferBase : vesaFramebufferBase+uint64(vesaFramebufferSize)]
	if !m.vesaEnabled {
		for i := range framebuffer {
			framebuffer[i] = 0
		}
	}

	m.vesaEnabled = true
	display.StartLinearFramebufferFallback(
		m.mem,
		int(vesaFramebufferBase),
		vesaFramebufferWidth,
		vesaFramebufferHeight,
		vesaFramebufferStride,
	)
}

func (m *Machine) installVESABIOS() {
	bios := buildVESABIOS()
	copy(m.mem[vesaBIOSBase:], bios)
	binary.LittleEndian.PutUint16(m.mem[vesaInt10Vector:], vesaBIOSEntry)
	binary.LittleEndian.PutUint16(m.mem[vesaInt10Vector+2:], vesaBIOSSegment)
}

func buildVESABIOS() []byte {
	const (
		currentBIOSModeOff = 0x6f
		currentVBEModeOff  = 0x70
		modeListOff        = 0x72
		oemStringOff       = 0x76
		vbeInfoOff         = 0x80
		modeInfoOff        = 0x280
	)

	code := []byte{
		0xfc, 0x3d, 0x00, 0x4f, 0x74, 0x39, 0x3d, 0x01, 0x4f, 0x74, 0x44, 0x3d,
		0x02, 0x4f, 0x74, 0x27, 0x3d, 0x03, 0x4f, 0x74, 0x1a, 0x80, 0xfc, 0x00,
		0x74, 0x09, 0x80, 0xfc, 0x0f, 0x74, 0x08, 0xb8, 0x4f, 0x01, 0xcf, 0xa2,
		0x6f, 0x00, 0xcf, 0xa0, 0x6f, 0x00, 0xb4, 0x50, 0x30, 0xff, 0xcf, 0x8b,
		0x1e, 0x70, 0x00, 0xb8, 0x4f, 0x00, 0xcf, 0x89, 0x1e, 0x70, 0x00, 0xb8,
		0x4f, 0x00, 0xcf, 0x1e, 0x0e, 0x1f, 0xbe, 0x80, 0x00, 0xb9, 0x00, 0x01,
		0xf3, 0xa5, 0x1f, 0xb8, 0x4f, 0x00, 0xcf, 0x1e, 0x0e, 0x1f, 0xbe, 0x80,
		0x02, 0xb9, 0x80, 0x00, 0xf3, 0xa5, 0x1f, 0xb8, 0x4f, 0x00, 0xcf,
	}

	bios := make([]byte, 0x380)
	copy(bios, []byte{0x55, 0xaa, 0x02})
	copy(bios[vesaBIOSEntry:], code)

	bios[currentBIOSModeOff] = 0x03
	binary.LittleEndian.PutUint16(bios[currentVBEModeOff:], 0x4000|vesaMode)
	binary.LittleEndian.PutUint16(bios[modeListOff:], vesaMode)
	binary.LittleEndian.PutUint16(bios[modeListOff+2:], 0xffff)
	copy(bios[oemStringOff:], "gokvm VBE\x00")

	copy(bios[vbeInfoOff:], "VESA")
	binary.LittleEndian.PutUint16(bios[vbeInfoOff+4:], 0x0200)
	putFarPtr(bios[vbeInfoOff+6:], oemStringOff)
	putFarPtr(bios[vbeInfoOff+0x0e:], modeListOff)
	binary.LittleEndian.PutUint16(bios[vbeInfoOff+0x12:], 64)

	mi := bios[modeInfoOff:]
	binary.LittleEndian.PutUint16(mi[0x00:], 0x0099)
	binary.LittleEndian.PutUint16(mi[0x04:], 64)
	binary.LittleEndian.PutUint16(mi[0x06:], 64)
	binary.LittleEndian.PutUint16(mi[0x10:], vesaFramebufferStride)
	binary.LittleEndian.PutUint16(mi[0x12:], vesaFramebufferWidth)
	binary.LittleEndian.PutUint16(mi[0x14:], vesaFramebufferHeight)
	mi[0x16] = 8
	mi[0x17] = 16
	mi[0x18] = 1
	mi[0x19] = 32
	mi[0x1b] = 6
	mi[0x1f] = 8
	mi[0x20] = 16
	mi[0x21] = 8
	mi[0x22] = 8
	mi[0x23] = 8
	mi[0x24] = 0
	mi[0x25] = 8
	mi[0x26] = 24
	binary.LittleEndian.PutUint32(mi[0x28:], uint32(vesaFramebufferBase))

	return bios
}

func putFarPtr(dst []byte, off int) {
	binary.LittleEndian.PutUint16(dst, uint16(off))
	binary.LittleEndian.PutUint16(dst[2:], vesaBIOSSegment)
}
