package virtio

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/internal/guestmem"
)

// Modern (virtio 1.0) PCI transport.
//
// Unlike the legacy transport, which exposes a single IO-port BAR and a page
// frame number per queue, the modern transport places a set of structures in
// a memory BAR and describes their location through PCI capabilities:
//
//	common cfg : device/driver feature negotiation, per-queue setup
//	notify     : queue kick doorbell
//	isr        : interrupt status (read-to-clear, for legacy INTx)
//	device cfg : device-specific configuration (e.g. the net MAC)
//
// The driver also programs the three parts of each split virtqueue
// (descriptor table, available ring, used ring) at independent guest-physical
// addresses, rather than the single contiguous PFN used by the legacy ring.
//
// refs https://docs.oasis-open.org/virtio/virtio/v1.1/csprd01/virtio-v1.1-csprd01.html#x1-1090002

const (
	// VIRTIO_F_VERSION_1 is feature bit 32; advertising it switches the
	// device into modern (non-transitional) operation.
	featureVersion1 = uint64(1) << 32

	// Layout of the structures within the memory BAR. Each is page aligned
	// so the driver may map them independently.
	commonCfgOffset = 0x0000
	commonCfgLen    = 0x38 // sizeof(struct virtio_pci_common_cfg)
	isrCfgOffset    = 0x1000
	isrCfgLen       = 0x04
	deviceCfgOffset = 0x2000
	notifyCfgOffset = 0x3000
	notifyCfgLen    = 0x0100
	modernBARSize   = 0x4000

	// PCI capability cfg_type values (virtio_pci_cap.cfg_type).
	capVendor     = 0x09 // PCI_CAP_ID_VNDR
	cfgTypeCommon = 0x1
	cfgTypeNotify = 0x2
	cfgTypeISR    = 0x3
	cfgTypeDevice = 0x4

	// Config-space offsets of the capability list entries.
	capCommonAt = 0x40
	capNotifyAt = 0x50
	capISRAt    = 0x64
	capDeviceAt = 0x74

	// Split virtqueue descriptor flags.
	descFNext  = 0x1
	descFWrite = 0x2
)

// Split virtqueue layout (the same wire format the legacy ring uses, but here
// the three areas are mapped from independent addresses).
type SplitDesc struct {
	Addr  uint64
	Len   uint32
	Flags uint16
	Next  uint16
}

type SplitAvail struct {
	Flags     uint16
	Idx       uint16
	Ring      [QueueSize]uint16
	UsedEvent uint16
}

type SplitUsedElem struct {
	ID  uint32
	Len uint32
}

type SplitUsed struct {
	Flags      uint16
	Idx        uint16
	Ring       [QueueSize]SplitUsedElem
	AvailEvent uint16
}

// SplitQueue is a virtqueue the driver has fully programmed and enabled. The
// pointers alias guest memory directly.
type SplitQueue struct {
	Desc  *[QueueSize]SplitDesc
	Avail *SplitAvail
	Used  *SplitUsed
	// Size is the negotiated ring size. Zero preserves the maximum-size
	// convention used by devices and tests predating this field.
	Size uint16
}

// ModernDevice is the device-specific behaviour the modern transport drives.
type ModernDevice interface {
	// DeviceFeatures returns the device-specific feature bits; the
	// transport adds VIRTIO_F_VERSION_1 on top.
	DeviceFeatures() uint64

	// NumQueues is the number of virtqueues exposed.
	NumQueues() int

	// DeviceConfigLen is the size in bytes of the device-specific config.
	DeviceConfigLen() int

	// ReadDeviceConfig / WriteDeviceConfig access the device-specific
	// configuration region at a BAR-relative-to-device-cfg offset.
	ReadDeviceConfig(offset uint64, data []byte)
	WriteDeviceConfig(offset uint64, data []byte)

	// QueueReady is invoked once when the driver enables queue idx.
	QueueReady(idx int, q *SplitQueue)

	// Notify is invoked when the driver kicks queue idx.
	Notify(idx int)
}

// queueState holds the registers the driver programs for one virtqueue.
type queueState struct {
	size       uint16
	msixVector uint16
	enable     uint16
	desc       uint64 // descriptor table address (queue_desc)
	driver     uint64 // available ring address (queue_driver)
	device     uint64 // used ring address (queue_device)
}

// ModernTransport implements the virtio 1.0 PCI transport. A device embeds it
// and forwards the pci.CapsAndMMIO methods to it.
type ModernTransport struct {
	// writeMu preserves the order of configuration writes and their device
	// callbacks. Device callbacks may not recursively issue common writes.
	// commonMu protects register snapshots and is never held while
	// calling the device, so callbacks may read configuration or raise IRQs.
	writeMu  sync.Mutex
	commonMu sync.RWMutex

	// irqMu keeps ISR read-to-clear and INTx level changes ordered across CPUs.
	irqMu       sync.Mutex
	setIRQLevel func(bool) error
	irqError    error
	irqWake     chan struct{} // Wakes recovery when an interrupt becomes pending.

	dev       ModernDevice
	Mem       []byte
	inject    func() error
	features  uint64
	configLen int

	deviceFeatureSel uint32
	driverFeatureSel uint32
	driverFeature    [2]uint32
	deviceStatus     uint8
	configGen        uint32 // Atomic; device configuration can change outside the vCPU thread.
	msixConfig       uint16
	queueSel         uint16
	queues           []queueState

	// isr is the interrupt status byte (read-to-clear). Written by device
	// IO goroutines and read by the vCPU thread, hence atomic.
	isr uint32
}

// NewModernTransport builds a transport for dev. inject raises the device's
// (legacy INTx) interrupt line.
func NewModernTransport(dev ModernDevice, mem []byte, inject func() error) *ModernTransport {
	t := &ModernTransport{
		dev:       dev,
		Mem:       mem,
		inject:    inject,
		features:  dev.DeviceFeatures() | featureVersion1,
		configLen: dev.DeviceConfigLen(),
		queues:    make([]queueState, dev.NumQueues()),
		irqWake:   make(chan struct{}, 1),
	}

	// Advertise the maximum queue size to the driver up front; it reads
	// this before programming a queue.
	for i := range t.queues {
		t.queues[i].size = QueueSize
	}

	return t
}

// negotiatedFeatures returns only offered features accepted by the driver.
// Register writes and device workers can run on different host threads.
func (t *ModernTransport) negotiatedFeatures() uint64 {
	t.commonMu.RLock()
	defer t.commonMu.RUnlock()
	if t.deviceStatus&0x08 == 0 { // VIRTIO_CONFIG_S_FEATURES_OK
		return 0
	}

	return (uint64(t.driverFeature[1])<<32 | uint64(t.driverFeature[0])) & t.features
}

// MMIOBARIndex reports which BAR carries the modern structures.
func (t *ModernTransport) MMIOBARIndex() int { return 0 }

// MMIOSize is the size of that BAR.
func (t *ModernTransport) MMIOSize() uint64 { return modernBARSize }

// SetIRQLine installs a level-triggered INTx callback. Configure it before
// starting the guest. The callback must not call back into this transport.
// Without it the constructor's legacy injection callback remains in use.
func (t *ModernTransport) SetIRQLine(setLevel func(bool) error) {
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	t.setIRQLevel = setLevel
}

// Interrupt raises the device's interrupt: it sets the queue-interrupt bit in
// ISR and asserts the (level) INTx line.
func (t *ModernTransport) Interrupt() error {
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	t.setPendingInterrupt(0x1)

	return t.raiseIRQ()
}

// ConfigChanged publishes a device configuration update and raises its INTx
// configuration interrupt without discarding an outstanding queue interrupt.
func (t *ModernTransport) ConfigChanged() error {
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	atomic.AddUint32(&t.configGen, 1)
	t.setPendingInterrupt(0x2)

	return t.raiseIRQ()
}

// setPendingInterrupt runs under irqMu. Wake the recovery worker only on the
// first pending interrupt; repeated recovery must not wake itself indefinitely.
func (t *ModernTransport) setPendingInterrupt(bits uint32) {
	if atomic.OrUint32(&t.isr, bits) == 0 {
		select {
		case t.irqWake <- struct{}{}:
		default:
		}
	}
}

func (t *ModernTransport) interruptPending() bool {
	return atomic.LoadUint32(&t.isr) != 0
}

// ReinjectIfPending re-asserts the interrupt when ISR is still set, i.e. the
// driver has not yet read (and thereby acknowledged) it. Device IO threads
// call this periodically so a missed interrupt does not stall the queue.
func (t *ModernTransport) ReinjectIfPending() error {
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	if atomic.LoadUint32(&t.isr) != 0 {
		return t.raiseIRQ()
	}

	return t.irqError
}

// raiseIRQ runs under irqMu. A failed acknowledgement is surfaced through the
// next device interrupt/poll because the MMIO interface cannot return errors.
func (t *ModernTransport) raiseIRQ() error {
	if t.irqError != nil {
		return t.irqError
	}
	if t.setIRQLevel != nil {
		return t.setIRQLevel(true)
	}

	return t.inject()
}

func (t *ModernTransport) clearInterrupt() byte {
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	status := byte(atomic.SwapUint32(&t.isr, 0))
	if t.setIRQLevel != nil {
		if err := t.setIRQLevel(false); err != nil {
			t.irqError = err
		}
	}

	return status
}

// virtioCap builds a struct virtio_pci_cap of the given length.
func virtioCap(capLen, cfgType, next uint8, offset, length uint32) []byte {
	b := make([]byte, capLen)
	b[0] = capVendor
	b[1] = next   // cap_next
	b[2] = capLen // cap_len
	b[3] = cfgType
	b[4] = 0 // bar 0
	// b[5:8] padding
	binary.LittleEndian.PutUint32(b[8:], offset)
	binary.LittleEndian.PutUint32(b[12:], length)

	return b
}

// Capabilities returns the PCI capability list bytes, starting at config
// offset 0x40, chaining common -> notify -> isr -> device.
func (t *ModernTransport) Capabilities() []byte {
	caps := make([]byte, 0, 68)
	caps = append(caps, virtioCap(16, cfgTypeCommon, capNotifyAt, commonCfgOffset, commonCfgLen)...)
	// notify cap is 20 bytes: the extra dword (notify_off_multiplier) is 0,
	// so all queues share the single notify doorbell.
	caps = append(caps, virtioCap(20, cfgTypeNotify, capISRAt, notifyCfgOffset, notifyCfgLen)...)
	caps = append(caps, virtioCap(16, cfgTypeISR, capDeviceAt, isrCfgOffset, isrCfgLen)...)
	caps = append(caps, virtioCap(16, cfgTypeDevice, 0x00, deviceCfgOffset, uint32(t.configLen))...)

	return caps
}

// MMIO dispatches a guest access within the BAR to the right structure.
func (t *ModernTransport) MMIO(offset uint64, data []byte, isWrite bool) {
	if len(data) == 0 {
		return
	}

	switch {
	case offset < commonCfgOffset+commonCfgLen:
		t.mmioCommonCfg(offset-commonCfgOffset, data, isWrite)
	case offset >= isrCfgOffset && offset < isrCfgOffset+isrCfgLen:
		if !isWrite {
			// Reading ISR returns the status and clears it.
			zero(data)
			data[0] = t.clearInterrupt()
		}
	case offset >= deviceCfgOffset && offset < deviceCfgOffset+0x1000:
		if isWrite {
			t.dev.WriteDeviceConfig(offset-deviceCfgOffset, data)
		} else {
			t.dev.ReadDeviceConfig(offset-deviceCfgOffset, data)
		}
	case offset >= notifyCfgOffset && offset < notifyCfgOffset+notifyCfgLen:
		if isWrite {
			t.dev.Notify(int(binary.LittleEndian.Uint16(pad2(data))))
		}
	}
}

// deviceFeature returns the 32-bit window of the device feature set selected
// by sel (0 = low bits, 1 = high bits including VIRTIO_F_VERSION_1).
func (t *ModernTransport) deviceFeature(sel uint32) uint32 {
	f := t.features

	switch sel {
	case 0:
		return uint32(f)
	case 1:
		return uint32(f >> 32)
	default:
		return 0
	}
}

func (t *ModernTransport) curQueue() *queueState {
	if int(t.queueSel) >= len(t.queues) {
		return nil
	}

	return &t.queues[t.queueSel]
}

// commonImage materializes the current state of the common configuration
// structure so that reads of any width/offset can be served by slicing. The
// caller holds commonMu.
func (t *ModernTransport) commonImage() [commonCfgLen]byte {
	var b [commonCfgLen]byte

	le := binary.LittleEndian
	le.PutUint32(b[0:], t.deviceFeatureSel)
	le.PutUint32(b[4:], t.deviceFeature(t.deviceFeatureSel))
	le.PutUint32(b[8:], t.driverFeatureSel)
	le.PutUint32(b[12:], t.driverFeature[t.driverFeatureSel&0x1])
	le.PutUint16(b[16:], t.msixConfig)
	le.PutUint16(b[18:], uint16(len(t.queues)))
	b[20] = t.deviceStatus
	b[21] = byte(atomic.LoadUint32(&t.configGen))
	le.PutUint16(b[22:], t.queueSel)

	if q := t.curQueue(); q != nil {
		le.PutUint16(b[24:], q.size)
		le.PutUint16(b[26:], q.msixVector)
		le.PutUint16(b[28:], q.enable)
		le.PutUint16(b[30:], 0) // queue_notify_off (multiplier 0 => always 0)
		le.PutUint64(b[32:], q.desc)
		le.PutUint64(b[40:], q.driver)
		le.PutUint64(b[48:], q.device)
	}

	return b
}

// mmioCommonCfg serves reads/writes of the common configuration structure.
func (t *ModernTransport) mmioCommonCfg(off uint64, data []byte, isWrite bool) {
	if !isWrite {
		zero(data)

		if off >= commonCfgLen {
			return
		}

		end := off + uint64(len(data))
		if end > commonCfgLen {
			end = commonCfgLen
		}

		t.commonMu.RLock()
		img := t.commonImage()
		t.commonMu.RUnlock()
		copy(data, img[off:end])

		return
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	// Stop workers before clearing their mappings. Other configuration writes
	// wait for this callback, while register reads can proceed without deadlock.
	if off == 20 && data[0] == 0 {
		if dev, ok := t.dev.(interface{ Reset() }); ok {
			dev.Reset()
		}
		t.clearInterrupt()
	}

	le := binary.LittleEndian
	var ready *SplitQueue
	var readyIndex int
	t.commonMu.Lock()
	switch off {
	case 0:
		t.deviceFeatureSel = le.Uint32(data)
	case 8:
		t.driverFeatureSel = le.Uint32(data)
	case 12:
		t.driverFeature[t.driverFeatureSel&0x1] = le.Uint32(data)
	case 16:
		t.msixConfig = le.Uint16(data)
	case 20:
		if data[0] == 0 {
			t.deviceFeatureSel = 0
			t.driverFeatureSel = 0
			t.driverFeature = [2]uint32{}
			t.queueSel = 0
			t.msixConfig = 0
			for i := range t.queues {
				t.queues[i] = queueState{size: QueueSize}
			}
		}
		t.deviceStatus = data[0]
	case 22:
		t.queueSel = le.Uint16(data)
	case 24:
		if q := t.curQueue(); q != nil {
			q.size = le.Uint16(data)
		}
	case 26:
		if q := t.curQueue(); q != nil {
			q.msixVector = le.Uint16(data)
		}
	case 28:
		if q := t.curQueue(); q != nil {
			q.enable = le.Uint16(data)
			if q.enable == 1 {
				readyIndex = int(t.queueSel)
				ready = t.mapQueue(readyIndex)
			}
		}
	// queue_desc/driver/device are 64-bit, each programmed as a low then a
	// high 32-bit half.
	case 32:
		writeQueueAddr(t.curQueue(), 0, 0, le.Uint32(data))
	case 36:
		writeQueueAddr(t.curQueue(), 0, 1, le.Uint32(data))
	case 40:
		writeQueueAddr(t.curQueue(), 1, 0, le.Uint32(data))
	case 44:
		writeQueueAddr(t.curQueue(), 1, 1, le.Uint32(data))
	case 48:
		writeQueueAddr(t.curQueue(), 2, 0, le.Uint32(data))
	case 52:
		writeQueueAddr(t.curQueue(), 2, 1, le.Uint32(data))
	}
	t.commonMu.Unlock()

	if ready != nil {
		t.dev.QueueReady(readyIndex, ready)
	}
}

// writeQueueAddr updates one 32-bit half of queue_desc (which 0), queue_driver
// (1) or queue_device (2). A nil queue (no queue selected) is ignored.
func writeQueueAddr(q *queueState, which, half int, v uint32) {
	if q == nil {
		return
	}

	switch which {
	case 0:
		setLoHi(&q.desc, half, v)
	case 1:
		setLoHi(&q.driver, half, v)
	case 2:
		setLoHi(&q.device, half, v)
	}
}

// mapQueue validates and maps a programmed queue while commonMu is held. The
// caller publishes the returned queue to the device after releasing commonMu.
func (t *ModernTransport) mapQueue(idx int) *SplitQueue {
	q := &t.queues[idx]
	// SplitQueue uses fixed-size views. Validate each entire view before
	// taking an unsafe pointer, even when the driver selects a smaller ring.
	valid := func(addr, size, align uint64) bool {
		return addr%align == 0 && guestmem.ValidRange(t.Mem, addr, size)
	}
	if q.size == 0 || q.size > QueueSize || q.size&(q.size-1) != 0 ||
		!valid(q.desc, uint64(unsafe.Sizeof([QueueSize]SplitDesc{})), 16) ||
		!valid(q.driver, uint64(unsafe.Sizeof(SplitAvail{})), 2) ||
		!valid(q.device, uint64(unsafe.Sizeof(SplitUsed{})), 4) {
		q.enable = 0

		return nil
	}

	return &SplitQueue{
		Desc:  (*[QueueSize]SplitDesc)(unsafe.Pointer(&t.Mem[q.desc])),
		Avail: (*SplitAvail)(unsafe.Pointer(&t.Mem[q.driver])),
		Used:  (*SplitUsed)(unsafe.Pointer(&t.Mem[q.device])),
		Size:  q.size,
	}
}

// setLoHi writes the low (half 0) or high (half 1) 32 bits of a 64-bit field.
func setLoHi(p *uint64, half int, v uint32) {
	if half == 0 {
		*p = (*p &^ 0xffffffff) | uint64(v)
	} else {
		*p = (*p & 0xffffffff) | (uint64(v) << 32)
	}
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// pad2 returns a little-endian 2-byte view of data, tolerating 1-byte writes.
func pad2(data []byte) []byte {
	if len(data) >= 2 {
		return data
	}

	return []byte{data[0], 0}
}
