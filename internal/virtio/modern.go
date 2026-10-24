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
//	notify     : queue kick doorbells, one per queue
//	isr        : interrupt status (read-to-clear, for legacy INTx)
//	device cfg : device-specific configuration (e.g. the net MAC)
//	MSI-X      : vector table and pending bits, when MSI-X is offered
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
	// Queue n's doorbell is at notifyCfgOffset + n*notifyOffMultiplier, so
	// the VMM can bind a separate KVM ioeventfd to the address of each queue.
	notifyOffMultiplier = 4
	// The MSI-X table and PBA share a page apart from the virtio registers,
	// as PCI requires; the table has room for 128 vectors.
	msixTableOffset = 0x4000
	msixPBAOffset   = 0x4800
	msixPBALen      = 0x0800
	modernBARSize   = 0x8000

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
	capMSIXAt   = 0x84

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

	// irqMu keeps ISR read-to-clear, INTx level changes and MSI-X state
	// ordered across CPUs.
	irqMu       sync.Mutex
	setIRQLevel func(bool) error
	irqError    error
	irqWake     chan struct{} // Wakes recovery when an interrupt becomes pending.

	// EnableMSIX sizes msixVectors before the device is registered; the slice
	// length is fixed afterward. Vector contents and msixControl use irqMu.
	msixRouter  MSIXRouter
	msixVectors []msixVector
	msixControl uint16      // Enable and Function Mask bits of Message Control.
	msixActive  atomic.Bool // Mirrors the Enable bit for lock-free readers.
	msixRetry   atomic.Bool // A delivery failed; its vector stays pending until resent.

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
		// Events start unmapped from MSI-X vectors.
		msixConfig: msixNoVector,
	}

	// Advertise the maximum queue size to the driver up front; it reads
	// this before programming a queue.
	for i := range t.queues {
		t.queues[i] = resetQueueState()
	}

	return t
}

// resetQueueState is a queue's register state after construction or reset.
func resetQueueState() queueState {
	return queueState{size: QueueSize, msixVector: msixNoVector}
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

// Interrupt notifies the driver of used buffers in queue. With MSI-X enabled
// it sends the vector the driver mapped to that queue; otherwise it sets the
// queue-interrupt bit in ISR and asserts the (level) INTx line.
func (t *ModernTransport) Interrupt(queue int) error {
	vector := t.queueVector(queue)
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	if t.msixEnabledLocked() {
		return t.msixNotifyLocked(vector)
	}
	t.setPendingInterrupt(0x1)

	return t.raiseIRQ()
}

// ConfigChanged publishes a device configuration update and raises its
// configuration interrupt without discarding an outstanding queue interrupt.
func (t *ModernTransport) ConfigChanged() error {
	vector := t.configVector()
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	atomic.AddUint32(&t.configGen, 1)
	if t.msixEnabledLocked() {
		// ISR still reports the change, but a driver using MSI-X never reads
		// it, so the bit must not drive INTx recovery (see interruptPending).
		atomic.OrUint32(&t.isr, 0x2)

		return t.msixNotifyLocked(vector)
	}
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

// interruptPending reports an INTx interrupt the driver has not acknowledged,
// or under MSI-X a message whose delivery failed. Delivered messages need no
// acknowledgement.
func (t *ModernTransport) interruptPending() bool {
	if t.msixActive.Load() {
		return t.msixRetry.Load()
	}

	return atomic.LoadUint32(&t.isr) != 0
}

// ReinjectIfPending re-asserts the interrupt when ISR is still set, i.e. the
// driver has not yet read (and thereby acknowledged) it. Device IO threads
// call this periodically so a missed interrupt does not stall the queue.
// Under MSI-X it only resends messages whose delivery failed: re-sending a
// delivered one would reach the driver as an unhandled interrupt rather than
// a harmless ISR read.
func (t *ModernTransport) ReinjectIfPending() error {
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	if t.msixEnabledLocked() {
		if t.msixRetry.Load() {
			return t.msixDeliverPendingLocked()
		}
	} else if atomic.LoadUint32(&t.isr) != 0 {
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
// offset 0x40, chaining common -> notify -> isr -> device, then MSI-X when
// EnableMSIX has been called.
func (t *ModernTransport) Capabilities() []byte {
	caps := make([]byte, 0, capMSIXAt+msixCapLen-capCommonAt)
	caps = append(caps, virtioCap(16, cfgTypeCommon, capNotifyAt, commonCfgOffset, commonCfgLen)...)
	// The notify cap's extra dword is notify_off_multiplier.
	notify := virtioCap(20, cfgTypeNotify, capISRAt, notifyCfgOffset, notifyCfgLen)
	binary.LittleEndian.PutUint32(notify[16:], notifyOffMultiplier)
	caps = append(caps, notify...)
	caps = append(caps, virtioCap(16, cfgTypeISR, capDeviceAt, isrCfgOffset, isrCfgLen)...)
	if len(t.msixVectors) == 0 {
		return append(caps, virtioCap(16, cfgTypeDevice, 0x00, deviceCfgOffset, uint32(t.configLen))...)
	}
	caps = append(caps, virtioCap(16, cfgTypeDevice, capMSIXAt, deviceCfgOffset, uint32(t.configLen))...)

	return append(caps, t.msixCapability()...)
}

// QueueDoorbells returns the BAR-relative doorbell address of every queue.
// A guest write to one of them is equivalent to NotifyQueue.
func (t *ModernTransport) QueueDoorbells() []uint64 {
	doorbells := make([]uint64, len(t.queues))
	for i := range doorbells {
		doorbells[i] = notifyCfgOffset + uint64(i)*notifyOffMultiplier
	}

	return doorbells
}

// NotifyQueue delivers a driver kick of queue idx to the device. The VMM calls
// it for kicks that KVM absorbed through an ioeventfd on the queue doorbell.
func (t *ModernTransport) NotifyQueue(idx int) {
	if idx >= 0 && idx < len(t.queues) {
		t.dev.Notify(idx)
	}
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
			// Like an ioeventfd, decode the queue from the doorbell address;
			// without VIRTIO_F_NOTIFICATION_DATA the value is just its index.
			t.NotifyQueue(int((offset - notifyCfgOffset) / notifyOffMultiplier))
		}
	case offset >= msixTableOffset && offset < msixPBAOffset:
		t.msixTableAccess(offset-msixTableOffset, data, isWrite)
	case offset >= msixPBAOffset && offset < msixPBAOffset+msixPBALen:
		if !isWrite {
			t.msixPBARead(offset-msixPBAOffset, data)
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
		le.PutUint16(b[30:], t.queueSel) // queue_notify_off: each queue has its own doorbell.
		le.PutUint64(b[32:], q.desc)
		le.PutUint64(b[40:], q.driver)
		le.PutUint64(b[48:], q.device)
	}

	return b
}

// validCommonWrite requires an exact writable register and access width.
// Queue addresses use independent, aligned 32-bit low/high accesses.
func validCommonWrite(off uint64, size int) bool {
	switch off {
	case 0, 8, 12, 32, 36, 40, 44, 48, 52:
		return size == 4
	case 16, 22, 24, 26, 28:
		return size == 2
	case 20:
		return size == 1
	default:
		return false
	}
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

	// Validate before decoding or invoking callbacks: malformed status writes
	// must not reset the device, and malformed queue writes must not enable it.
	if !validCommonWrite(off, len(data)) {
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
		t.clearMSIXPending()
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
		t.msixConfig = t.mapVector(le.Uint16(data))
	case 20:
		if data[0] == 0 {
			// Reset also unmaps every event from its MSI-X vector.
			t.deviceFeatureSel = 0
			t.driverFeatureSel = 0
			t.driverFeature = [2]uint32{}
			t.queueSel = 0
			t.msixConfig = msixNoVector
			for i := range t.queues {
				t.queues[i] = resetQueueState()
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
			q.msixVector = t.mapVector(le.Uint16(data))
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
