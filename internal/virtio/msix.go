package virtio

import (
	"encoding/binary"
	"errors"
	"log"
	"sync/atomic"
)

// MSI-X for the modern transport.
//
// Once the driver enables MSI-X it maps each virtqueue and the configuration
// change event to an entry of the MSI-X table in the memory BAR. Interrupt
// then sends the queue's own message instead of setting ISR and raising the
// shared INTx line, so the driver neither reads ISR nor deasserts a level
// interrupt. Masking follows PCI: a masked vector latches its Pending Bit and
// is sent once both the vector and the function are unmasked.
//
// refs PCI Local Bus Specification 3.0, section 6.8.2 (MSI-X)
// refs https://docs.oasis-open.org/virtio/virtio/v1.1/csprd01/virtio-v1.1-csprd01.html#x1-1180006

const (
	capMSIX    = 0x11 // PCI_CAP_ID_MSIX
	msixCapLen = 12

	// Message Control bits writable by the driver.
	msixEnable       = uint16(1) << 15
	msixFunctionMask = uint16(1) << 14

	msixEntrySize    = 16
	msixVectorMasked = 1 // Vector Control Mask Bit, set at reset.

	// msixNoVector (VIRTIO_MSI_NO_VECTOR) leaves an event unmapped; it is also
	// what a vector register reads back as when a mapping is rejected.
	msixNoVector = 0xffff
)

// MSIXRouter delivers one transport's MSI-X messages, e.g. through KVM irqfds
// bound to GSI routes. A transport serializes its calls.
type MSIXRouter interface {
	// Route installs vector's message before Signal sends it. The transport
	// routes a vector again whenever the driver changes its message.
	Route(vector int, address uint64, data uint32) error

	// Signal sends vector's routed message to the guest.
	Signal(vector int) error
}

// msixVector is one MSI-X table entry plus the message last given to the router.
type msixVector struct {
	address uint64
	data    uint32
	control uint32
	pending bool

	routed       bool
	routeAddress uint64
	routeData    uint32
}

// EnableMSIX offers an MSI-X table with MSIXVectors entries, delivered through
// router. Call it before registering the device on the PCI bus, because it
// extends the capability list the bus reads at registration.
func (t *ModernTransport) EnableMSIX(router MSIXRouter) {
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	t.msixRouter = router
	t.msixVectors = make([]msixVector, t.MSIXVectors())
	for i := range t.msixVectors {
		t.msixVectors[i].control = msixVectorMasked
	}
}

// MSIXVectors is the table size EnableMSIX offers: one vector per queue plus
// one for configuration changes, the layout Linux asks for first.
func (t *ModernTransport) MSIXVectors() int {
	return len(t.queues) + 1
}

// msixCapability builds the MSI-X capability. The table and PBA both live in
// the transport's memory BAR (BIR 0); Enable and Function Mask are live state
// served by ReadCapability.
func (t *ModernTransport) msixCapability() []byte {
	b := make([]byte, msixCapLen)
	b[0] = capMSIX
	binary.LittleEndian.PutUint16(b[2:], uint16(len(t.msixVectors)-1)) // Table Size is N-1.
	binary.LittleEndian.PutUint32(b[4:], msixTableOffset)
	binary.LittleEndian.PutUint32(b[8:], msixPBAOffset)

	return b
}

// ReadCapability implements pci.CapabilityHandler for Message Control.
func (t *ModernTransport) ReadCapability(off int, data []byte) {
	if len(t.msixVectors) == 0 {
		return
	}
	t.irqMu.Lock()
	control := t.msixControl
	t.irqMu.Unlock()
	if i := capMSIXAt + 3 - off; i >= 0 && i < len(data) {
		data[i] |= byte(control >> 8)
	}
}

// WriteCapability implements pci.CapabilityHandler. It owns the whole MSI-X
// capability, of which only Enable and Function Mask are writable.
func (t *ModernTransport) WriteCapability(off int, data []byte) bool {
	if len(t.msixVectors) == 0 || off < capMSIXAt || off+len(data) > capMSIXAt+msixCapLen {
		return false
	}
	if i := capMSIXAt + 3 - off; i >= 0 && i < len(data) {
		t.setMSIXControl((uint16(data[i]) << 8) & (msixEnable | msixFunctionMask))
	}

	return true
}

func (t *ModernTransport) setMSIXControl(control uint16) {
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	enable := control&msixEnable != 0
	switch {
	case enable && !t.msixEnabledLocked():
		// A function using MSI-X must not assert INTx. Release this device's
		// share of the line so devices sharing the GSI are unaffected.
		if t.setIRQLevel != nil {
			if err := t.setIRQLevel(false); err != nil {
				t.irqError = err
			}
		}
	case !enable && t.msixEnabledLocked():
		// The driver ignored ISR while MSI-X was enabled. Drop that status so
		// a fallback to INTx does not start with an interrupt nobody handles.
		atomic.StoreUint32(&t.isr, 0)
	}
	t.msixControl = control
	t.msixActive.Store(enable)
	if err := t.msixDeliverPendingLocked(); err != nil {
		log.Printf("virtio: MSI-X pending delivery: %v", err)
	}
}

func (t *ModernTransport) msixEnabledLocked() bool {
	return t.msixControl&msixEnable != 0
}

// mapVector returns vector when it names an MSI-X table entry. Any other value,
// including every value when MSI-X is not offered, reads back as NO_VECTOR,
// which is how the driver learns that the mapping failed.
func (t *ModernTransport) mapVector(vector uint16) uint16 {
	if int(vector) < len(t.msixVectors) {
		return vector
	}

	return msixNoVector
}

func (t *ModernTransport) queueVector(queue int) uint16 {
	t.commonMu.RLock()
	defer t.commonMu.RUnlock()
	if queue < 0 || queue >= len(t.queues) {
		return msixNoVector
	}

	return t.queues[queue].msixVector
}

func (t *ModernTransport) configVector() uint16 {
	t.commonMu.RLock()
	defer t.commonMu.RUnlock()

	return t.msixConfig
}

// msixNotifyLocked sends vector, or latches its Pending Bit while masked. An
// event the driver left unmapped sends nothing.
func (t *ModernTransport) msixNotifyLocked(vector uint16) error {
	if int(vector) >= len(t.msixVectors) {
		return nil
	}
	v := &t.msixVectors[vector]
	if t.msixControl&msixFunctionMask != 0 || v.control&msixVectorMasked != 0 {
		v.pending = true

		return nil
	}

	return t.msixSendLocked(int(vector))
}

// msixSendLocked routes the vector's current message if it changed since it
// was last routed, then sends it. Drivers rewrite messages only while masked,
// so a route is never updated under an in-flight message. A failed delivery
// leaves the vector pending for ReinjectIfPending to retry.
func (t *ModernTransport) msixSendLocked(vector int) error {
	v := &t.msixVectors[vector]
	err := t.msixRouteAndSignalLocked(vector, v)
	v.pending = err != nil
	if err != nil {
		t.msixRetry.Store(true)
	}

	return err
}

func (t *ModernTransport) msixRouteAndSignalLocked(vector int, v *msixVector) error {
	if !v.routed || v.routeAddress != v.address || v.routeData != v.data {
		if err := t.msixRouter.Route(vector, v.address, v.data); err != nil {
			return err
		}
		v.routed, v.routeAddress, v.routeData = true, v.address, v.data
	}

	return t.msixRouter.Signal(vector)
}

// msixDeliverPendingLocked sends every latched vector that is now unmasked.
// Vectors that stay masked are delivered on unmask, not by retrying.
func (t *ModernTransport) msixDeliverPendingLocked() error {
	if !t.msixEnabledLocked() || t.msixControl&msixFunctionMask != 0 {
		t.msixRetry.Store(false)

		return nil
	}
	var errs []error
	for i := range t.msixVectors {
		if v := &t.msixVectors[i]; v.pending && v.control&msixVectorMasked == 0 {
			errs = append(errs, t.msixSendLocked(i))
		}
	}
	err := errors.Join(errs...)
	t.msixRetry.Store(err != nil)

	return err
}

// msixTableAccess serves the MSI-X table. Drivers use aligned dword or qword
// accesses; an access straying outside one 16-byte entry is ignored.
func (t *ModernTransport) msixTableAccess(offset uint64, data []byte, isWrite bool) {
	if !isWrite {
		zero(data)
	}
	entry, within := offset/msixEntrySize, offset%msixEntrySize
	if entry >= uint64(len(t.msixVectors)) || within+uint64(len(data)) > msixEntrySize {
		return
	}
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	v := &t.msixVectors[entry]
	var raw [msixEntrySize]byte
	le := binary.LittleEndian
	le.PutUint64(raw[0:], v.address)
	le.PutUint32(raw[8:], v.data)
	le.PutUint32(raw[12:], v.control)
	if !isWrite {
		copy(data, raw[within:])

		return
	}
	copy(raw[within:], data)
	v.address = le.Uint64(raw[0:])
	v.data = le.Uint32(raw[8:])
	v.control = le.Uint32(raw[12:]) & msixVectorMasked // Other bits are reserved.
	if err := t.msixDeliverPendingLocked(); err != nil {
		log.Printf("virtio: MSI-X vector %d: %v", entry, err)
	}
}

// msixPBARead serves the read-only Pending Bit Array.
func (t *ModernTransport) msixPBARead(offset uint64, data []byte) {
	zero(data)
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	for i := range data {
		for bit := range 8 {
			vector := (offset+uint64(i))*8 + uint64(bit)
			if vector < uint64(len(t.msixVectors)) && t.msixVectors[vector].pending {
				data[i] |= 1 << bit
			}
		}
	}
}

// clearMSIXPending discards vectors latched before a device reset. MSI-X
// enablement and the table belong to PCI configuration and survive it.
func (t *ModernTransport) clearMSIXPending() {
	t.irqMu.Lock()
	defer t.irqMu.Unlock()
	for i := range t.msixVectors {
		t.msixVectors[i].pending = false
	}
	t.msixRetry.Store(false)
}
