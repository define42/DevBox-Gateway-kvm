package virtio

import (
	"encoding/binary"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
)

var errTestRoute = errors.New("no route")

type msixRoute struct {
	address uint64
	data    uint32
}

// recordingRouter stands in for the VMM's KVM irqfds.
type recordingRouter struct {
	mu       sync.Mutex
	routes   map[int]msixRoute
	routings int
	signals  []int
	err      error
}

func (r *recordingRouter) Route(vector int, address uint64, data uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.routes == nil {
		r.routes = map[int]msixRoute{}
	}
	r.routes[vector] = msixRoute{address: address, data: data}
	r.routings++

	return nil
}

func (r *recordingRouter) Signal(vector int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signals = append(r.signals, vector)

	return nil
}

func (r *recordingRouter) sent() []int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.signals)
}

func newMSIXTransport(t *testing.T, queues int) (*ModernTransport, *recordingRouter, *int) {
	t.Helper()
	injected := new(int)
	tr := newTestTransport(&mockModernDev{numQ: queues, cfgLen: 8}, make([]byte, 0x10000), injected)
	router := &recordingRouter{}
	tr.EnableMSIX(router)

	return tr, router, injected
}

func msixControl(tr *ModernTransport) uint16 {
	b := msixCapabilityBytes(tr)[2:4]
	tr.ReadCapability(capMSIXAt+2, b)

	return binary.LittleEndian.Uint16(b)
}

func msixCapabilityBytes(tr *ModernTransport) []byte {
	caps := tr.Capabilities()

	return slices.Clone(caps[capMSIXAt-capCommonAt:])
}

func setMSIXControl(t *testing.T, tr *ModernTransport, control uint16) {
	t.Helper()
	if !tr.WriteCapability(capMSIXAt+2, binary.LittleEndian.AppendUint16(nil, control)) {
		t.Fatal("transport did not own its Message Control register")
	}
}

// programVector writes an entry's message and then unmasks it, like Linux:
// message dwords first, then Vector Control.
func programVector(tr *ModernTransport, vector int, address uint64, data uint32) {
	base := msixTableOffset + uint64(vector)*msixEntrySize
	tr.MMIO(base, binary.LittleEndian.AppendUint32(nil, uint32(address)), true)
	tr.MMIO(base+4, binary.LittleEndian.AppendUint32(nil, uint32(address>>32)), true)
	tr.MMIO(base+8, binary.LittleEndian.AppendUint32(nil, data), true)
	maskVector(tr, vector, false)
}

func maskVector(tr *ModernTransport, vector int, masked bool) {
	var control uint32
	if masked {
		control = msixVectorMasked
	}
	tr.MMIO(msixTableOffset+uint64(vector)*msixEntrySize+12, binary.LittleEndian.AppendUint32(nil, control), true)
}

func pendingBits(tr *ModernTransport) uint64 {
	var pba [8]byte
	tr.MMIO(msixPBAOffset, pba[:], false)

	return binary.LittleEndian.Uint64(pba[:])
}

func mapQueueVector(tr *ModernTransport, queue int, vector uint16) {
	writeCfg(tr, 22, uint64(queue), 2)
	writeCfg(tr, 26, uint64(vector), 2)
}

func TestMSIXCapabilityAdvertisesTable(t *testing.T) {
	t.Parallel()

	legacy := newTestTransport(&mockModernDev{numQ: 2}, nil, new(int))
	if caps := legacy.Capabilities(); len(caps) != capMSIXAt-capCommonAt || caps[capDeviceAt-capCommonAt+1] != 0 {
		t.Fatal("transport without a router advertised MSI-X")
	}
	if legacy.WriteCapability(capMSIXAt+2, []byte{0, 0x80}) {
		t.Fatal("transport without a router claimed MSI-X registers")
	}

	tr, _, _ := newMSIXTransport(t, 2)
	caps := tr.Capabilities()
	if caps[capDeviceAt-capCommonAt+1] != capMSIXAt {
		t.Fatalf("device capability links to %#x, want MSI-X at %#x", caps[capDeviceAt-capCommonAt+1], capMSIXAt)
	}
	msix := msixCapabilityBytes(tr)
	le := binary.LittleEndian
	if len(msix) != msixCapLen || msix[0] != capMSIX || msix[1] != 0 || le.Uint16(msix[2:]) != 2 ||
		le.Uint32(msix[4:]) != msixTableOffset || le.Uint32(msix[8:]) != msixPBAOffset {
		t.Fatalf("MSI-X capability = %x", msix)
	}
	if end := msixPBAOffset + msixPBALen; end > modernBARSize || msixTableOffset+128*msixEntrySize > msixPBAOffset {
		t.Fatal("MSI-X structures do not fit the memory BAR")
	}
	// Only Enable and Function Mask are writable; Table Size is read-only.
	setMSIXControl(t, tr, 0xffff)
	if got := msixControl(tr); got != msixEnable|msixFunctionMask|2 {
		t.Fatalf("Message Control = %#x", got)
	}
	setMSIXControl(t, tr, msixEnable)
	if got := msixControl(tr); got != msixEnable|2 {
		t.Fatalf("Message Control after clearing Function Mask = %#x", got)
	}
	if tr.WriteCapability(capDeviceAt, []byte{0}) {
		t.Fatal("MSI-X handler claimed a virtio capability")
	}
}

func TestMSIXVectorRegistersRejectUnknownVectors(t *testing.T) {
	t.Parallel()

	tr, _, _ := newMSIXTransport(t, 2)
	if readCfg(tr, 16, 2) != msixNoVector || readCfg(tr, 26, 2) != msixNoVector {
		t.Fatal("events are not unmapped initially")
	}
	for _, tc := range []struct {
		vector uint64
		want   uint64
	}{{0, 0}, {2, 2}, {3, msixNoVector}, {0x1234, msixNoVector}, {msixNoVector, msixNoVector}} {
		writeCfg(tr, 16, tc.vector, 2)
		mapQueueVector(tr, 1, uint16(tc.vector))
		if got := readCfg(tr, 16, 2); got != tc.want {
			t.Fatalf("config vector %#x reads %#x, want %#x", tc.vector, got, tc.want)
		}
		if got := readCfg(tr, 26, 2); got != tc.want {
			t.Fatalf("queue vector %#x reads %#x, want %#x", tc.vector, got, tc.want)
		}
	}

	legacy := newTestTransport(&mockModernDev{numQ: 1}, nil, new(int))
	writeCfg(legacy, 16, 0, 2)
	if readCfg(legacy, 16, 2) != msixNoVector {
		t.Fatal("transport without MSI-X accepted a vector mapping")
	}
}

func TestMSIXQueueInterruptSendsMappedVector(t *testing.T) {
	t.Parallel()

	tr, router, injected := newMSIXTransport(t, 2)
	var asserted int
	tr.SetIRQLine(func(high bool) error {
		if high {
			asserted++
		}

		return nil
	})
	setMSIXControl(t, tr, msixEnable)
	programVector(tr, 2, 0xfee01000, 0x4041)
	mapQueueVector(tr, 1, 2)

	for range 2 {
		if err := tr.Interrupt(1); err != nil {
			t.Fatal(err)
		}
	}
	// An unmapped queue (NO_VECTOR) notifies nothing.
	if err := tr.Interrupt(0); err != nil {
		t.Fatal(err)
	}
	if got := router.sent(); !slices.Equal(got, []int{2, 2}) {
		t.Fatalf("signals = %v, want vector 2 twice", got)
	}
	if router.routings != 1 || router.routes[2] != (msixRoute{0xfee01000, 0x4041}) {
		t.Fatalf("routes = %+v after %d routings", router.routes, router.routings)
	}
	if *injected != 0 || asserted != 0 || tr.isr != 0 || tr.interruptPending() || len(tr.irqWake) != 0 {
		t.Fatalf("MSI-X queue interrupt used INTx: injected=%d asserted=%d isr=%d", *injected, asserted, tr.isr)
	}
	if err := tr.ReinjectIfPending(); err != nil || len(router.sent()) != 2 {
		t.Fatal("MSI-X recovery re-sent a message")
	}

	// A message rewritten while masked is routed again before its next use.
	maskVector(tr, 2, true)
	programVector(tr, 2, 0xfee02000, 0x4042)
	if err := tr.Interrupt(1); err != nil {
		t.Fatal(err)
	}
	if router.routings != 2 || router.routes[2] != (msixRoute{0xfee02000, 0x4042}) {
		t.Fatalf("changed message was not rerouted: %+v", router.routes)
	}
}

func TestMSIXMaskingLatchesPendingBit(t *testing.T) {
	t.Parallel()

	tr, router, _ := newMSIXTransport(t, 2)
	setMSIXControl(t, tr, msixEnable)
	mapQueueVector(tr, 0, 1)
	// Entries start masked, as after a PCI reset.
	if err := tr.Interrupt(0); err != nil {
		t.Fatal(err)
	}
	if len(router.sent()) != 0 || pendingBits(tr) != 1<<1 {
		t.Fatalf("masked vector: signals=%v PBA=%#x", router.sent(), pendingBits(tr))
	}
	programVector(tr, 1, 0xfee00000, 0x31)
	if !slices.Equal(router.sent(), []int{1}) || pendingBits(tr) != 0 {
		t.Fatalf("unmasking did not deliver the pending vector: signals=%v PBA=%#x", router.sent(), pendingBits(tr))
	}

	setMSIXControl(t, tr, msixEnable|msixFunctionMask)
	if err := tr.Interrupt(0); err != nil {
		t.Fatal(err)
	}
	if len(router.sent()) != 1 || pendingBits(tr) != 1<<1 {
		t.Fatal("Function Mask did not latch the vector")
	}
	// Disabling MSI-X keeps the bit; it is sent once enabled and unmasked.
	setMSIXControl(t, tr, 0)
	setMSIXControl(t, tr, msixEnable|msixFunctionMask)
	if len(router.sent()) != 1 || pendingBits(tr) != 1<<1 {
		t.Fatal("vector was sent while the function was masked")
	}
	setMSIXControl(t, tr, msixEnable)
	if !slices.Equal(router.sent(), []int{1, 1}) || pendingBits(tr) != 0 {
		t.Fatalf("clearing Function Mask: signals=%v PBA=%#x", router.sent(), pendingBits(tr))
	}
}

func TestMSIXConfigChangeUsesConfigVector(t *testing.T) {
	t.Parallel()

	tr, router, injected := newMSIXTransport(t, 1)
	setMSIXControl(t, tr, msixEnable)
	programVector(tr, 0, 0xfee00000, 0x30)
	writeCfg(tr, 16, 0, 2)
	if err := tr.ConfigChanged(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(router.sent(), []int{0}) || readCfg(tr, 21, 1) != 1 || *injected != 0 {
		t.Fatalf("config change: signals=%v generation=%d injected=%d", router.sent(), readCfg(tr, 21, 1), *injected)
	}
	// ISR still records the change, without arming INTx recovery.
	if tr.isr != 2 || tr.interruptPending() || len(tr.irqWake) != 0 {
		t.Fatalf("config change ISR=%d pending=%v", tr.isr, tr.interruptPending())
	}
	if err := tr.ReinjectIfPending(); err != nil || *injected != 0 {
		t.Fatal("recovery raised INTx while MSI-X was enabled")
	}
	// Falling back to INTx must not start with the status the driver ignored.
	setMSIXControl(t, tr, 0)
	var isr [1]byte
	tr.MMIO(isrCfgOffset, isr[:], false)
	if isr[0] != 0 {
		t.Fatalf("ISR after disabling MSI-X = %d", isr[0])
	}
	if err := tr.ConfigChanged(); err != nil || *injected != 1 || !tr.interruptPending() {
		t.Fatal("configuration change without MSI-X did not use INTx")
	}
}

func TestMSIXEnableReleasesINTx(t *testing.T) {
	t.Parallel()

	tr, router, _ := newMSIXTransport(t, 1)
	var levels []bool
	tr.SetIRQLine(func(high bool) error {
		levels = append(levels, high)

		return nil
	})
	if err := tr.Interrupt(0); err != nil {
		t.Fatal(err)
	}
	setMSIXControl(t, tr, msixEnable|msixFunctionMask)
	if !slices.Equal(levels, []bool{true, false}) {
		t.Fatalf("INTx levels = %v, want released when MSI-X is enabled", levels)
	}
	if tr.interruptPending() || len(router.sent()) != 0 {
		t.Fatal("enabling MSI-X converted the INTx interrupt into a message")
	}
}

func TestMSIXResetUnmapsEventsAndDropsPendingBits(t *testing.T) {
	t.Parallel()

	tr, router, _ := newMSIXTransport(t, 2)
	setMSIXControl(t, tr, msixEnable)
	writeCfg(tr, 16, 0, 2)
	mapQueueVector(tr, 1, 2)
	if err := tr.Interrupt(1); err != nil {
		t.Fatal(err)
	}
	if pendingBits(tr) != 1<<2 {
		t.Fatalf("PBA = %#x", pendingBits(tr))
	}
	writeCfg(tr, 20, 0, 1)
	if pendingBits(tr) != 0 || readCfg(tr, 16, 2) != msixNoVector || readCfg(tr, 26, 2) != msixNoVector {
		t.Fatal("device reset kept vector mappings or pending bits")
	}
	// PCI configuration, including the MSI-X enable bit, survives the reset.
	if msixControl(tr)&msixEnable == 0 {
		t.Fatal("device reset disabled MSI-X")
	}
	programVector(tr, 2, 0xfee00000, 0x32)
	if len(router.sent()) != 0 {
		t.Fatal("a dropped pending bit was delivered after reset")
	}
}

func TestMSIXTableAccesses(t *testing.T) {
	t.Parallel()

	tr, _, _ := newMSIXTransport(t, 1)
	entry := msixTableOffset + uint64(msixEntrySize)
	tr.MMIO(entry, binary.LittleEndian.AppendUint64(nil, 0x00000001_fee0300c), true) // Aligned qword address.
	tr.MMIO(entry+8, binary.LittleEndian.AppendUint64(nil, 0xfffffffe_00004a51), true)
	var raw [msixEntrySize]byte
	tr.MMIO(entry, raw[:8], false)
	tr.MMIO(entry+8, raw[8:12], false)
	tr.MMIO(entry+12, raw[12:], false)
	le := binary.LittleEndian
	// Reserved Vector Control bits read as zero and the write unmasked it.
	if le.Uint64(raw[0:]) != 0x00000001_fee0300c || le.Uint32(raw[8:]) != 0x4a51 || le.Uint32(raw[12:]) != 0 {
		t.Fatalf("table entry = %x", raw)
	}
	var b [1]byte
	tr.MMIO(entry+9, b[:], false)
	if b[0] != 0x4a {
		t.Fatalf("byte read = %#x", b[0])
	}
	// Accesses crossing an entry or beyond the table have no effect.
	before := tr.msixVectors[0]
	tr.MMIO(msixTableOffset+12, make([]byte, 8), true)
	tr.MMIO(msixTableOffset+2*msixEntrySize, make([]byte, 4), true)
	cross := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	tr.MMIO(msixTableOffset+12, cross, false)
	if tr.msixVectors[0] != before || !slices.Equal(cross, make([]byte, 8)) {
		t.Fatal("entry-crossing table access was not ignored")
	}
	tr.MMIO(msixPBAOffset, []byte{0xff}, true)
	if pendingBits(tr) != 0 {
		t.Fatal("PBA is writable")
	}
}

func TestMSIXRouteFailureIsReturned(t *testing.T) {
	t.Parallel()

	tr, router, _ := newMSIXTransport(t, 1)
	router.err = errTestRoute
	setMSIXControl(t, tr, msixEnable)
	programVector(tr, 1, 0xfee00000, 0x31)
	mapQueueVector(tr, 0, 1)
	if err := tr.Interrupt(0); !errors.Is(err, router.err) {
		t.Fatalf("Interrupt with failed route = %v", err)
	}
	router.err = nil
	if err := tr.Interrupt(0); err != nil || !slices.Equal(router.sent(), []int{1}) {
		t.Fatal("route was not retried after a failure")
	}
}

func TestMSIXConcurrentInterruptsAndMasking(t *testing.T) {
	t.Parallel()

	tr, router, _ := newMSIXTransport(t, 2)
	setMSIXControl(t, tr, msixEnable)
	programVector(tr, 1, 0xfee00000, 0x31)
	mapQueueVector(tr, 0, 1)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 500 {
				if err := tr.Interrupt(0); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Go(func() {
		for i := range 500 {
			maskVector(tr, 1, i%2 == 0)
			_ = pendingBits(tr)
		}
	})
	workers.Wait()
	maskVector(tr, 1, false)
	if pendingBits(tr) != 0 || len(router.sent()) == 0 {
		t.Fatalf("interrupts lost across masking: PBA=%#x signals=%d", pendingBits(tr), len(router.sent()))
	}
}

func TestMSIXControlThroughPCIConfigSpace(t *testing.T) {
	t.Parallel()

	keyboard := NewInputKeyboard(5, func() error { return nil }, make([]byte, 0x10000))
	router := &recordingRouter{}
	keyboard.EnableMSIX(router)
	bus := pci.New(keyboard)
	access := func(offset int) {
		_ = bus.PciConfAddrOut(0xcf8, pci.NumToBytes(uint32(0x80000000|offset&0xfc)))
	}
	access(capMSIXAt)
	header := make([]byte, 4)
	_ = bus.PciConfDataIn(0xcfc, header)
	if header[0] != capMSIX || binary.LittleEndian.Uint16(header[2:]) != uint16(keyboard.MSIXVectors()-1) {
		t.Fatalf("MSI-X capability through config space = %x", header)
	}
	_ = bus.PciConfDataOut(0xcfe, []byte{0x02, 0xc0}) // Linux writes Message Control as a word.
	control := make([]byte, 2)
	_ = bus.PciConfDataIn(0xcfe, control)
	if got := binary.LittleEndian.Uint16(control); got != msixEnable|msixFunctionMask|uint16(keyboard.MSIXVectors()-1) {
		t.Fatalf("Message Control through config space = %#x", got)
	}
	if _, offset, ok := bus.LookupMMIO(InputKeyboardMMIOBase + msixPBAOffset); !ok || offset != msixPBAOffset {
		t.Fatal("PBA is outside the decoded memory BAR")
	}
}

func TestMSIXFailedDeliveryIsRetried(t *testing.T) {
	t.Parallel()

	for _, latched := range []bool{true, false} {
		tr, router, _ := newMSIXTransport(t, 1)
		setMSIXControl(t, tr, msixEnable)
		mapQueueVector(tr, 0, 1)
		if latched {
			// Masked at reset: the interrupt latches, then unmasking fails.
			if err := tr.Interrupt(0); err != nil {
				t.Fatal(err)
			}
			router.err = errTestRoute
			programVector(tr, 1, 0xfee00000, 0x31)
		} else {
			programVector(tr, 1, 0xfee00000, 0x31)
			router.err = errTestRoute
			if err := tr.Interrupt(0); !errors.Is(err, errTestRoute) {
				t.Fatalf("failed delivery = %v", err)
			}
		}
		if pendingBits(tr) != 1<<1 || !tr.interruptPending() {
			t.Fatalf("latched=%v: failed delivery dropped the interrupt: PBA=%#x", latched, pendingBits(tr))
		}
		router.err = nil
		if err := tr.ReinjectIfPending(); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(router.sent(), []int{1}) || pendingBits(tr) != 0 || tr.interruptPending() {
			t.Fatalf("latched=%v: recovery signals=%v PBA=%#x", latched, router.sent(), pendingBits(tr))
		}
		// A delivered message is never sent again.
		if err := tr.ReinjectIfPending(); err != nil || len(router.sent()) != 1 {
			t.Fatal("recovery re-sent a delivered message")
		}
	}
}
