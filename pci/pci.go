package pci

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// Configuration Space Access Mechanism #1
//
// refs
// https://wiki.osdev.org/PCI
// http://www2.comp.ufscar.br/~helio/boot-int/pci.html
type address uint32

func (a address) getRegisterOffset() uint32 {
	return uint32(a) & 0xfc
}

func (a address) getFunctionNumber() uint32 {
	return (uint32(a) >> 8) & 0x7
}

func (a address) getDeviceNumber() uint32 {
	return (uint32(a) >> 11) & 0x1f
}

func (a address) getBusNumber() uint32 {
	return (uint32(a) >> 16) & 0xff
}

func (a address) isEnable() bool {
	return ((uint32(a) >> 31) | 0x1) == 0x1
}

// interface for a PCI device.
type Device interface {
	GetDeviceHeader() DeviceHeader
	Read(uint64, []byte) error
	Write(uint64, []byte) error

	// IO port range for this PCI device.
	// This range corresponds to IO Range in BAR0.
	IOPort() uint64
	Size() uint64
}

// CapsAndMMIO is implemented by PCI devices that expose a capabilities list
// and decode a memory BAR (i.e. modern virtio devices). The base address of
// the BAR is owned by the pci package; the device works purely in
// BAR-relative offsets and never needs to know where the BAR was mapped.
type CapsAndMMIO interface {
	Device

	// Capabilities returns config-space bytes starting at offset 0x40,
	// i.e. the PCI capabilities list pointed to by CapabilitiesPointer.
	Capabilities() []byte

	// MMIOBARIndex is the index (0-5) of the memory BAR carrying the
	// device's MMIO structures.
	MMIOBARIndex() int

	// MMIOSize is the size in bytes of that memory BAR.
	MMIOSize() uint64

	// MMIO services a guest memory access within the BAR. offset is
	// relative to the start of the BAR. For reads (isWrite false) the
	// handler fills data; for writes it consumes data.
	MMIO(offset uint64, data []byte, isWrite bool)
}

type DeviceHeader struct {
	VendorID uint16
	DeviceID uint16
	Command  uint16
	// Status bit 4 (0x10) advertises a PCI capabilities list; modern
	// virtio devices set it together with CapabilitiesPointer.
	Status              uint16
	RevisionID          uint8
	ProgIF              uint8
	Subclass            uint8
	ClassCode           uint8
	_                   uint8 // cacheLineSize
	_                   uint8 // latencyTimer
	HeaderType          uint8
	_                   uint8 // bist
	BAR                 [6]uint32
	_                   uint32 // cardbusCISPointer
	SubsystemVendorID   uint16
	SubsystemID         uint16
	_                   uint32 // expansionROMBaseAddress
	CapabilitiesPointer uint8  // offset of the first PCI capability
	_                   [7]uint8
	InterruptLine       uint8
	InterruptPin        uint8
	_                   uint8 // minGnt
	_                   uint8 // maxLat
}

func (h DeviceHeader) Bytes() ([]byte, error) {
	buf := new(bytes.Buffer)

	if err := binary.Write(buf, binary.LittleEndian, h); err != nil {
		return []byte{}, err
	}

	return buf.Bytes(), nil
}

// registeredDevice stores PCI function metadata. Device callbacks run only
// while registering, outside the bus lock. Adding another function in the same
// slot updates function zero's multifunction header bit under the bus lock.
type registeredDevice struct {
	dev      Device
	header   DeviceHeader
	caps     []byte
	barSizes [6]uint64
	mmio     CapsAndMMIO
	mmioBAR  int
	slot     int
	function int
}

var ErrDeviceAddress = errors.New("invalid PCI slot or function")

var ErrDeviceOccupied = errors.New("PCI function is already occupied")

var ErrNilDevice = errors.New("PCI device is nil")

type PCI struct {
	mu      sync.RWMutex
	addr    address
	devices []registeredDevice

	// functions maps bus-zero device/function addresses to registry indices.
	// The registry retains insertion order for device lifecycle callbacks.
	functions map[int]int
	nextSlot  int

	// A single size-probe can be in flight at a time: the guest writes
	// 0xffffffff to a BAR then immediately reads it back. probeSlot is -1
	// when no probe is pending.
	probeSlot int
	probeBar  int

	// barOverride records guest-assigned base addresses for the memory
	// BARs of modern (CapsAndMMIO) devices, keyed by registry index then BAR.
	barOverride map[int]map[int]uint32

	// configOverride records writable configuration-space bytes outside BARs.
	// Firmware uses this for chipset registers such as i440FX PAM shadow RAM
	// controls, and later reads expect to see the values it wrote.
	configOverride map[int]map[int]byte
}

func New(devices ...Device) *PCI {
	p := &PCI{
		probeSlot:      -1,
		functions:      map[int]int{},
		barOverride:    map[int]map[int]uint32{},
		configOverride: map[int]map[int]byte{},
	}
	p.AddDevices(devices...)

	return p
}

// AddDevices appends PCI functions in slot order. Headers, capabilities and BAR
// sizes must be fully initialized before registration and remain fixed afterward.
// Guest configuration writes are owned by the bus, not the device metadata.
func (p *PCI) AddDevices(devices ...Device) {
	registered := make([]registeredDevice, 0, len(devices))
	for _, dev := range devices {
		registered = append(registered, registerDevice(dev))
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range registered {
		p.registerAt(p.nextSlot, 0, entry)
	}
}

func registerDevice(dev Device) registeredDevice {
	entry := registeredDevice{dev: dev, header: dev.GetDeviceHeader()}
	entry.barSizes[0] = dev.Size()
	if cm, ok := dev.(CapsAndMMIO); ok {
		entry.caps = append([]byte(nil), cm.Capabilities()...)
		bar := cm.MMIOBARIndex()
		if bar >= 0 && bar < len(entry.barSizes) {
			entry.mmio, entry.mmioBAR = cm, bar
			entry.barSizes[bar] = cm.MMIOSize()
		}
	}

	return entry
}

// AddDeviceAt registers a function at an explicit bus-zero PCI address. Later
// AddDevices calls continue at the next slot after the highest registered slot.
func (p *PCI) AddDeviceAt(slot, function int, dev Device) error {
	if slot < 0 || slot >= 32 || function < 0 || function >= 8 {
		return fmt.Errorf("%w: %02x.%x", ErrDeviceAddress, slot, function)
	}
	if dev == nil {
		return ErrNilDevice
	}
	entry := registerDevice(dev)

	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.functions[slot*8+function]; exists {
		return fmt.Errorf("%w: %02x.%x", ErrDeviceOccupied, slot, function)
	}
	p.registerAt(slot, function, entry)

	return nil
}

// registerAt installs precomputed metadata while holding mu.
func (p *PCI) registerAt(slot, function int, entry registeredDevice) {
	if p.functions == nil {
		p.functions = map[int]int{}
	}
	if len(p.devices) == 0 {
		p.probeSlot = -1
	}
	if function != 0 {
		if index, ok := p.functions[slot*8]; ok {
			p.devices[index].header.HeaderType |= 0x80
		}
	} else {
		for fn := 1; fn < 8; fn++ {
			if _, ok := p.functions[slot*8+fn]; ok {
				entry.header.HeaderType |= 0x80

				break
			}
		}
	}
	entry.slot, entry.function = slot, function
	p.functions[slot*8+function] = len(p.devices)
	p.devices = append(p.devices, entry)
	if slot >= p.nextSlot {
		p.nextSlot = slot + 1
	}
}

// Devices returns a snapshot of the registered functions. Mutating the returned
// slice does not change the bus; callbacks on its devices run without a bus lock.
func (p *PCI) Devices() []Device {
	p.mu.RLock()
	defer p.mu.RUnlock()

	devices := make([]Device, len(p.devices))
	for i, entry := range p.devices {
		devices[i] = entry.dev
	}

	return devices
}

// Function identifies a registered PCI function on bus zero.
type Function struct {
	Slot     int
	Function int
	Device   Device
}

// Functions returns the devices and their addresses in registration order.
// The returned slice is independent of the bus registry.
func (p *PCI) Functions() []Function {
	p.mu.RLock()
	defer p.mu.RUnlock()

	functions := make([]Function, len(p.devices))
	for i, entry := range p.devices {
		functions[i] = Function{Slot: entry.slot, Function: entry.function, Device: entry.dev}
	}

	return functions
}

// barAtOffset returns the BAR index (0-5) addressed by a config-space byte
// offset, or -1 if the offset is not within the BAR registers (0x10-0x27).
func barAtOffset(offset int) int {
	if offset < 0x10 || offset >= 0x28 {
		return -1
	}

	return offset/4 - 4
}

// barSize returns the size of a device's BAR. Modern devices report the
// size of their memory BAR; every device reports its legacy IO BAR as BAR0.
// The caller holds mu.
func (p *PCI) barSize(slot, bar int) uint64 {
	return p.devices[slot].barSizes[bar]
}

// barBase returns the guest-physical base address a device's BAR currently
// decodes at, with the low type bits masked off. It honors a guest-assigned
// override when present, otherwise the value baked into the device header.
// The caller holds mu.
func (p *PCI) barBase(slot, bar int) uint64 {
	raw := p.devices[slot].header.BAR[bar]
	if v, ok := p.barOverride[slot][bar]; ok {
		raw = v
	}

	if raw&0x1 == 0x1 {
		return uint64(raw &^ 0x3) // IO space BAR
	}

	return uint64(raw &^ 0xf) // memory space BAR
}

// configSpace builds the 256-byte PCI configuration space image for a slot:
// the 64-byte header followed by the device's capability list, with any
// guest-assigned BAR overrides applied. The caller holds mu.
func (p *PCI) configSpace(slot int) ([]byte, error) {
	b, err := p.devices[slot].header.Bytes()
	if err != nil {
		return nil, err
	}

	cfg := make([]byte, 256)
	copy(cfg, b)

	copy(cfg[0x40:], p.devices[slot].caps)

	for bar, v := range p.barOverride[slot] {
		copy(cfg[0x10+bar*4:], NumToBytes(v))
	}

	for off, v := range p.configOverride[slot] {
		if off >= 0 && off < len(cfg) {
			cfg[off] = v
		}
	}

	return cfg, nil
}

// LookupMMIO finds the modern device whose memory BAR decodes addr and
// returns it together with the BAR-relative offset.
func (p *PCI) LookupMMIO(addr uint64) (CapsAndMMIO, uint64, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for slot, entry := range p.devices {
		if entry.mmio == nil {
			continue
		}

		base := p.barBase(slot, entry.mmioBAR)
		size := entry.barSizes[entry.mmioBAR]
		if addr >= base && addr-base < size {
			return entry.mmio, addr - base, true
		}
	}

	return nil, 0, false
}

func (p *PCI) PciConfDataIn(port uint64, values []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	fillAbsent := func() {
		for i := range values {
			values[i] = 0xff
		}
	}

	// offset can be obtained from many source as below:
	//        (address from IO port 0xcf8) & 0xfc + (IO port address for Data) - 0xCFC
	// see pci_conf1_read in linux/arch/x86/pci/direct.c for more detail.
	offset := int(p.addr.getRegisterOffset() + uint32(port-0xCFC))

	if !p.addr.isEnable() {
		return nil
	}

	if p.addr.getBusNumber() != 0 {
		fillAbsent()

		return nil
	}

	function := int(p.addr.getDeviceNumber()*8 + p.addr.getFunctionNumber())
	slot, exists := p.functions[function]
	if !exists {
		fillAbsent()

		return nil
	}

	// Reply to a pending BAR size probe with the size mask.
	if bar := barAtOffset(offset); bar >= 0 && p.probeSlot == slot && p.probeBar == bar {
		barOffset := 0x10 + bar*4
		mask := NumToBytes(SizeToBits(p.barSize(slot, bar)))
		copy(values, mask[offset-barOffset:])

		if offset+len(values) >= barOffset+4 {
			p.probeSlot, p.probeBar = -1, 0
		}

		return nil
	}

	b, err := p.configSpace(slot)
	if err != nil {
		return err
	}

	if offset >= len(b) {
		fillAbsent()

		return nil
	}

	end := offset + len(values)
	if end > len(b) {
		end = len(b)
	}

	copy(values, b[offset:end])
	for i := end - offset; i < len(values); i++ {
		values[i] = 0xff
	}

	return nil
}

func (p *PCI) PciConfDataOut(port uint64, values []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	offset := int(p.addr.getRegisterOffset() + uint32(port-0xCFC))

	if !p.addr.isEnable() {
		return nil
	}

	if p.addr.getBusNumber() != 0 {
		return nil
	}

	function := int(p.addr.getDeviceNumber()*8 + p.addr.getFunctionNumber())
	slot, exists := p.functions[function]
	if !exists {
		return nil
	}

	bar := barAtOffset(offset)
	if bar < 0 {
		if p.configOverride == nil {
			p.configOverride = map[int]map[int]byte{}
		}
		if p.configOverride[slot] == nil {
			p.configOverride[slot] = map[int]byte{}
		}
		for i, v := range values {
			if off := offset + i; off >= 0 && off < 256 {
				p.configOverride[slot][off] = v
			}
		}

		return nil
	}
	barOffset := 0x10 + bar*4

	// 0xffffffff arms a size probe; the next read of this BAR returns the
	// size mask instead of the address.
	if offset == barOffset && len(values) == 4 && BytesToNum(values) == 0xffffffff {
		p.probeSlot, p.probeBar = slot, bar

		return nil
	}

	// Capture guest-assigned base addresses for modern devices' memory
	// BARs so reads return the assigned value and MMIO decoding follows
	// the driver. Legacy IO BARs keep their header-baked address.
	if entry := p.devices[slot]; entry.mmio != nil && bar == entry.mmioBAR {
		if p.barOverride == nil {
			p.barOverride = map[int]map[int]uint32{}
		}
		if p.barOverride[slot] == nil {
			p.barOverride[slot] = map[int]uint32{}
		}

		cur := p.devices[slot].header.BAR[bar]
		if v, ok := p.barOverride[slot][bar]; ok {
			cur = v
		}

		raw := NumToBytes(cur)
		copy(raw[offset-barOffset:], values)
		p.barOverride[slot][bar] = uint32(BytesToNum(raw))
	}

	return nil
}

func (p *PCI) PciConfAddrIn(port uint64, values []byte) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(values) != 4 {
		return nil
	}

	copy(values[:4], NumToBytes(uint32(p.addr)))

	return nil
}

func (p *PCI) PciConfAddrOut(port uint64, values []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(values) != 4 {
		return nil
	}

	p.addr = address(BytesToNum(values))

	return nil
}

func SizeToBits(size uint64) uint32 {
	if size == 0 {
		return 0
	}

	return ^uint32(1) - uint32(size-2)
}

func BytesToNum(bytes []byte) uint64 {
	res := uint64(0)

	for i, x := range bytes {
		res |= uint64(x) << (i * 8)
	}

	return res
}

func NumToBytes(x interface{}) []byte {
	res := []byte{}
	l := 0
	y := uint64(0)

	switch v := x.(type) {
	case uint8:
		l = 1
		y = uint64(v)
	case uint16:
		l = 2
		y = uint64(v)
	case uint32:
		l = 4
		y = uint64(v)
	case uint64:
		l = 8
		y = v
	default:
		return []byte{}
	}

	for i := 0; i < l; i++ {
		res = append(res, uint8(y))
		y >>= 8
	}

	return res
}
