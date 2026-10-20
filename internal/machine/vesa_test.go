package machine

import (
	"encoding/binary"
	"image"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

func TestVESAFallbackAndBIOSAgree(t *testing.T) {
	t.Parallel()
	m := bootTestMachine(t, 32<<20)
	display := &machineTestDisplay{}
	m.mem[vesaFramebufferBase] = 0xa5
	m.EnableVESA(display)
	if !m.vesaEnabled || display.base != int(vesaFramebufferBase) ||
		display.width != 1024 || display.height != 768 || display.stride != 4096 {
		t.Fatalf("VESA fallback geometry = %#x %dx%d stride=%d", display.base, display.width, display.height, display.stride)
	}
	if &display.linearMem[0] != &m.mem[0] || m.mem[vesaFramebufferBase] != 0 {
		t.Fatal("initial framebuffer was not cleared in shared guest RAM")
	}
	m.mem[vesaFramebufferBase] = 0x5a
	m.EnableVESA(display)
	if m.mem[vesaFramebufferBase] != 0x5a {
		t.Fatal("reenabling VESA destroyed the guest framebuffer")
	}
	m.StartVGATextFallback(display)
	if &display.textMem[0] != &m.mem[0] {
		t.Fatal("text fallback does not observe guest RAM")
	}
	m.installVESABIOS()
	bios := m.mem[vesaBIOSBase : vesaBIOSBase+0x400]
	if bios[0] != 0x55 || bios[1] != 0xaa || string(bios[0x80:0x84]) != "VESA" {
		t.Fatal("installed VESA BIOS lacks its ROM and VBE signatures")
	}
	if size := int(bios[2]) * 512; size != len(bios) {
		t.Fatalf("ROM size = %d, want %d", size, len(bios))
	}
	oemPointer := binary.LittleEndian.Uint32(bios[0x86:0x8a])
	oemAddress := uint64(oemPointer>>16)*16 + uint64(oemPointer&0xffff)
	const oemName = "devbox-kvm VBE\x00"
	if oemAddress < vesaBIOSBase || oemAddress+uint64(len(oemName)) > vesaBIOSBase+uint64(len(bios)) {
		t.Fatalf("VBE OEM string pointer %#x is outside the BIOS ROM", oemAddress)
	}
	if got := string(m.mem[oemAddress : oemAddress+uint64(len(oemName))]); got != oemName {
		t.Errorf("VBE OEM string = %q, want %q", got, oemName)
	}
	if version := binary.LittleEndian.Uint16(bios[0x84:0x86]); version != 0x0200 {
		t.Errorf("VBE version = %#x, want 0x0200", version)
	}
	if binary.LittleEndian.Uint16(m.mem[vesaInt10Vector:]) != vesaBIOSEntry ||
		binary.LittleEndian.Uint16(m.mem[vesaInt10Vector+2:]) != vesaBIOSSegment {
		t.Fatal("INT 10 vector does not target the installed BIOS")
	}
	farPointer := binary.LittleEndian.Uint32(bios[0x8e:])
	modeList := uint64(farPointer>>16)*16 + uint64(farPointer&0xffff)
	if binary.LittleEndian.Uint16(m.mem[modeList:]) != vesaMode ||
		binary.LittleEndian.Uint16(m.mem[modeList+2:]) != 0xffff {
		t.Fatal("VBE mode list pointer or terminator is invalid")
	}
	mode := bios[0x280:]
	if binary.LittleEndian.Uint16(mode[0x10:]) != uint16(display.stride) ||
		binary.LittleEndian.Uint16(mode[0x12:]) != uint16(display.width) ||
		binary.LittleEndian.Uint16(mode[0x14:]) != uint16(display.height) ||
		binary.LittleEndian.Uint32(mode[0x28:]) != uint32(display.base) || mode[0x19] != 32 {
		t.Fatal("VBE mode description differs from the actual fallback framebuffer")
	}
}

func TestVESARequiresDisplayAndEnoughRAM(t *testing.T) {
	t.Parallel()
	m := &Machine{mem: make([]byte, 1<<20)}
	display := &machineTestDisplay{}
	m.EnableVESA(nil)
	m.EnableVESA(display)
	if m.vesaEnabled || display.linearMem != nil {
		t.Fatal("VESA enabled without sufficient guest RAM")
	}
}

type machineTestDisplay struct {
	linearMem, textMem          []byte
	base, width, height, stride int
	resize                      func(int, int) error
	closed                      int
}

func (*machineTestDisplay) Flush(int, int, *image.RGBA) error { return nil }
func (d *machineTestDisplay) Close() error {
	d.closed++

	return nil
}
func (*machineTestDisplay) SetInput(virtio.DisplayInput) {}
func (d *machineTestDisplay) SetResizeHandler(resize func(int, int) error) {
	d.resize = resize
}
func (d *machineTestDisplay) StartVGATextFallback(mem []byte) { d.textMem = mem }
func (d *machineTestDisplay) StartLinearFramebufferFallback(mem []byte, base, width, height, stride int) {
	d.linearMem, d.base, d.width, d.height, d.stride = mem, base, width, height, stride
}
