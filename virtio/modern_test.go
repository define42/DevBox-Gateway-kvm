package virtio

import (
	"encoding/binary"
	"errors"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func TestModernConfigInterruptAndGeneration(t *testing.T) {
	t.Parallel()
	dev := &mockModernDev{numQ: 1}
	tr := NewModernTransport(dev, make([]byte, 0x10000), func() error { return nil })
	for _, configFirst := range []bool{false, true} {
		if configFirst {
			if err := tr.ConfigChanged(); err != nil {
				t.Fatal(err)
			}
		}
		if err := tr.Interrupt(); err != nil {
			t.Fatal(err)
		}
		if !configFirst {
			if err := tr.ConfigChanged(); err != nil {
				t.Fatal(err)
			}
		}
		var isr [1]byte
		tr.MMIO(isrCfgOffset, isr[:], false)
		if isr[0] != 3 {
			t.Fatalf("configFirst=%v: queue/config ISR flags=%x", configFirst, isr[0])
		}
	}
	for range 254 {
		if err := tr.ConfigChanged(); err != nil {
			t.Fatal(err)
		}
	}
	if gen := readCfg(tr, 21, 1); gen != 0 {
		t.Fatalf("configuration generation did not wrap at256: %d", gen)
	}
}

func TestModernConfigInterruptConcurrent(t *testing.T) {
	t.Parallel()
	dev := &mockModernDev{numQ: 1}
	var injected atomic.Uint32
	tr := NewModernTransport(dev, make([]byte, 0x10000), func() error {
		injected.Add(1)

		return nil
	})
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 1000 {
			if err := tr.ConfigChanged(); err != nil {
				t.Error(err)
			}
		}
	})
	workers.Go(func() {
		for range 1000 {
			if err := tr.Interrupt(); err != nil {
				t.Error(err)
			}
		}
	})
	workers.Go(func() {
		for range 1000 {
			_ = readCfg(tr, 21, 1)
		}
	})
	workers.Wait()
	var isr [1]byte
	tr.MMIO(isrCfgOffset, isr[:], false)
	if isr[0] != 3 || readCfg(tr, 21, 1) != 1000%256 || injected.Load() != 2000 {
		t.Fatal("concurrent interrupts lost status bits or configuration changes")
	}
}

// mockModernDev is a minimal ModernDevice for exercising the transport.
type mockModernDev struct {
	features uint64
	numQ     int
	cfgLen   int
	ready    map[int]*SplitQueue
	notified []int
}

func (d *mockModernDev) DeviceFeatures() uint64                 { return d.features }
func (d *mockModernDev) NumQueues() int                         { return d.numQ }
func (d *mockModernDev) DeviceConfigLen() int                   { return d.cfgLen }
func (d *mockModernDev) ReadDeviceConfig(_ uint64, data []byte) { zero(data) }
func (d *mockModernDev) WriteDeviceConfig(_ uint64, _ []byte)   {}

func (d *mockModernDev) QueueReady(idx int, q *SplitQueue) {
	if d.ready == nil {
		d.ready = map[int]*SplitQueue{}
	}

	d.ready[idx] = q
}

func (d *mockModernDev) Notify(idx int) { d.notified = append(d.notified, idx) }

func writeCfg(tr *ModernTransport, off, v uint64, n int) {
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = byte(v >> (8 * i))
	}

	tr.MMIO(commonCfgOffset+off, b, true)
}

func readCfg(tr *ModernTransport, off uint64, n int) uint64 {
	b := make([]byte, n)
	tr.MMIO(commonCfgOffset+off, b, false)

	var v uint64
	for i := 0; i < n; i++ {
		v |= uint64(b[i]) << (8 * i)
	}

	return v
}

func newTestTransport(dev ModernDevice, mem []byte, injected *int) *ModernTransport {
	return NewModernTransport(dev, mem, func() error {
		*injected++

		return nil
	})
}

func TestModernFeatureNegotiation(t *testing.T) {
	t.Parallel()

	dev := &mockModernDev{numQ: 2, cfgLen: 12}
	injected := 0
	tr := newTestTransport(dev, make([]byte, 0x10000), &injected)

	// High dword must advertise VIRTIO_F_VERSION_1 (bit 32 -> bit 0).
	writeCfg(tr, 0, 1, 4) // device_feature_select = 1
	if f := readCfg(tr, 4, 4); f&0x1 == 0 {
		t.Fatalf("VIRTIO_F_VERSION_1 not advertised: high feature dword=0x%x", f)
	}

	// Low dword is the device's own features (none for this mock).
	writeCfg(tr, 0, 0, 4)
	if f := readCfg(tr, 4, 4); f != 0 {
		t.Fatalf("low feature dword: expected 0, got 0x%x", f)
	}

	if n := readCfg(tr, 18, 2); n != 2 {
		t.Fatalf("num_queues: expected 2, got %d", n)
	}
}

func TestModernQueueSetup(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 0x10000)
	dev := &mockModernDev{numQ: 2, cfgLen: 12}
	injected := 0
	tr := newTestTransport(dev, mem, &injected)

	writeCfg(tr, 22, 1, 2) // queue_select = 1

	// Default reported queue size is the max.
	if sz := readCfg(tr, 24, 2); sz != QueueSize {
		t.Fatalf("queue_size: expected %d, got %d", QueueSize, sz)
	}

	const descAddr, availAddr, usedAddr = 0x1000, 0x2000, 0x3000

	writeCfg(tr, 32, descAddr, 4) // queue_desc lo
	writeCfg(tr, 36, 0, 4)        // queue_desc hi
	writeCfg(tr, 40, availAddr, 4)
	writeCfg(tr, 44, 0, 4)
	writeCfg(tr, 48, usedAddr, 4)
	writeCfg(tr, 52, 0, 4)

	// Addresses must read back.
	if a := readCfg(tr, 32, 4); a != descAddr {
		t.Fatalf("queue_desc readback: expected 0x%x, got 0x%x", descAddr, a)
	}

	writeCfg(tr, 28, 1, 2) // queue_enable = 1

	q := dev.ready[1]
	if q == nil {
		t.Fatal("QueueReady was not called for queue 1")
	}

	if got := uintptr(unsafe.Pointer(&q.Desc[0])); got != uintptr(unsafe.Pointer(&mem[descAddr])) {
		t.Fatalf("desc table not mapped at descAddr")
	}

	if got := uintptr(unsafe.Pointer(q.Avail)); got != uintptr(unsafe.Pointer(&mem[availAddr])) {
		t.Fatalf("avail ring not mapped at availAddr")
	}

	if got := uintptr(unsafe.Pointer(q.Used)); got != uintptr(unsafe.Pointer(&mem[usedAddr])) {
		t.Fatalf("used ring not mapped at usedAddr")
	}

	if e := readCfg(tr, 28, 2); e != 1 {
		t.Fatalf("queue_enable readback: expected 1, got %d", e)
	}
}

func TestModernISRReadClears(t *testing.T) {
	t.Parallel()

	dev := &mockModernDev{numQ: 1, cfgLen: 8}
	injected := 0
	tr := newTestTransport(dev, make([]byte, 0x1000), &injected)

	if err := tr.Interrupt(); err != nil {
		t.Fatal(err)
	}

	if injected != 1 {
		t.Fatalf("inject count: expected 1, got %d", injected)
	}

	b := make([]byte, 1)
	tr.MMIO(isrCfgOffset, b, false)

	if b[0] != 1 {
		t.Fatalf("first ISR read: expected 1, got %d", b[0])
	}

	tr.MMIO(isrCfgOffset, b, false)

	if b[0] != 0 {
		t.Fatalf("ISR not cleared on read: got %d", b[0])
	}
}

func TestModernNotifyDispatch(t *testing.T) {
	t.Parallel()

	dev := &mockModernDev{numQ: 2, cfgLen: 8}
	injected := 0
	tr := newTestTransport(dev, make([]byte, 0x1000), &injected)

	tr.MMIO(notifyCfgOffset, []byte{0x1, 0x0}, true)

	if len(dev.notified) != 1 || dev.notified[0] != 1 {
		t.Fatalf("notify dispatch: expected [1], got %v", dev.notified)
	}
}

func TestModernCapabilitiesChain(t *testing.T) {
	t.Parallel()

	dev := &mockModernDev{numQ: 2, cfgLen: 12}
	injected := 0
	tr := newTestTransport(dev, make([]byte, 0x1000), &injected)

	caps := tr.Capabilities()

	for _, tc := range []struct {
		name     string
		configAt int // absolute config-space offset of the capability
		cfgType  byte
		next     byte
	}{
		{"common", capCommonAt, cfgTypeCommon, capNotifyAt},
		{"notify", capNotifyAt, cfgTypeNotify, capISRAt},
		{"isr", capISRAt, cfgTypeISR, capDeviceAt},
		{"device", capDeviceAt, cfgTypeDevice, 0x00},
	} {
		// caps starts at config offset capCommonAt (0x40).
		at := tc.configAt - capCommonAt

		if caps[at] != capVendor {
			t.Fatalf("%s: cap_vndr expected 0x%x, got 0x%x", tc.name, capVendor, caps[at])
		}

		if caps[at+3] != tc.cfgType {
			t.Fatalf("%s: cfg_type expected %d, got %d", tc.name, tc.cfgType, caps[at+3])
		}

		if caps[at+1] != tc.next {
			t.Fatalf("%s: cap_next expected 0x%x, got 0x%x", tc.name, tc.next, caps[at+1])
		}
	}
}

type resettingModernDev struct {
	mockModernDev
	resets int
}

func (d *resettingModernDev) Reset() { d.resets++ }

func TestModernDeviceReset(t *testing.T) {
	t.Parallel()
	dev := &resettingModernDev{mockModernDev: mockModernDev{numQ: 2}}
	injected := 0
	tr := newTestTransport(dev, make([]byte, 0x10000), &injected)
	writeCfg(tr, 0, 1, 4)
	writeCfg(tr, 8, 1, 4)
	writeCfg(tr, 12, 1, 4)
	writeCfg(tr, 22, 1, 2)
	writeCfg(tr, 32, 0x1000, 4)
	writeCfg(tr, 40, 0x2000, 4)
	writeCfg(tr, 48, 0x3000, 4)
	writeCfg(tr, 28, 1, 2)
	if err := tr.Interrupt(); err != nil {
		t.Fatal(err)
	}
	writeCfg(tr, 20, 0, 1)
	if dev.resets != 1 || tr.deviceFeatureSel != 0 || tr.driverFeatureSel != 0 ||
		tr.driverFeature != [2]uint32{} || tr.queueSel != 0 || tr.isr != 0 {
		t.Fatalf("transport did not reset: %+v", tr)
	}
	for _, queue := range tr.queues {
		if queue != (queueState{size: QueueSize}) {
			t.Fatalf("reset retained queue mapping: %+v", queue)
		}
	}
}

func TestModernRejectsInvalidQueueMappings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                 string
		desc, driver, device uint64
		size                 uint16
	}{
		{"descriptor_bounds", 0x10000, 0x2000, 0x3000, QueueSize},
		{"descriptor_tail", 0xfff0, 0x2000, 0x3000, QueueSize},
		{"available_bounds", 0x1000, ^uint64(1), 0x3000, QueueSize},
		{"used_bounds", 0x1000, 0x2000, 0xfffc, QueueSize},
		{"descriptor_alignment", 0x1001, 0x2000, 0x3000, QueueSize},
		{"available_alignment", 0x1000, 0x2001, 0x3000, QueueSize},
		{"used_alignment", 0x1000, 0x2000, 0x3001, QueueSize},
		{"zero_size", 0x1000, 0x2000, 0x3000, 0},
		{"large_size", 0x1000, 0x2000, 0x3000, QueueSize * 2},
		{"non_power_of_two", 0x1000, 0x2000, 0x3000, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := &mockModernDev{numQ: 1}
			injected := 0
			tr := newTestTransport(dev, make([]byte, 0x10000), &injected)
			tr.queues[0] = queueState{desc: tc.desc, driver: tc.driver, device: tc.device, size: tc.size, enable: 1}
			writeCfg(tr, 28, 1, 2)
			if len(dev.ready) != 0 || tr.queues[0].enable != 0 {
				t.Fatal("invalid queue mapping activated")
			}
		})
	}
}

// reentrantModernDev reads transport registers from callbacks, as a device may
// do while publishing a queue or stopping workers during reset.
type reentrantModernDev struct {
	mockModernDev
	transport *ModernTransport
	active    map[int]*SplitQueue
	resets    int
}

func (d *reentrantModernDev) QueueReady(idx int, q *SplitQueue) {
	_ = readCfg(d.transport, 20, 1)
	if d.active == nil {
		d.active = map[int]*SplitQueue{}
	}
	d.active[idx] = q
}

func (d *reentrantModernDev) Reset() {
	_ = readCfg(d.transport, 20, 1)
	d.active = nil
	d.resets++
}

func TestModernConcurrentCommonConfiguration(t *testing.T) {
	t.Parallel()

	dev := &reentrantModernDev{mockModernDev: mockModernDev{
		features: 0x00000002_a5a5a5a5,
		numQ:     2,
	}}
	tr := NewModernTransport(dev, make([]byte, 0x10000), func() error { return nil })
	dev.transport = tr
	var workers sync.WaitGroup
	workers.Go(func() {
		for i := range 1000 {
			writeCfg(tr, 0, uint64(i%2), 4)
			writeCfg(tr, 8, uint64(i%2), 4)
			writeCfg(tr, 12, uint64(i), 4)
			writeCfg(tr, 16, uint64(i), 2)
		}
	})
	workers.Go(func() {
		for i := range 200 {
			writeCfg(tr, 22, uint64(i%2), 2)
			writeCfg(tr, 24, QueueSize, 2)
			writeCfg(tr, 32, 0x1000, 4)
			writeCfg(tr, 40, 0x2000, 4)
			writeCfg(tr, 48, 0x3000, 4)
			writeCfg(tr, 28, 1, 2)
		}
	})
	workers.Go(func() {
		for range 200 {
			writeCfg(tr, 20, 0, 1)
		}
	})
	workers.Go(func() {
		var cfg [commonCfgLen]byte
		for range 1000 {
			tr.MMIO(commonCfgOffset, cfg[:], false)
			selected := binary.LittleEndian.Uint32(cfg[0:])
			features := binary.LittleEndian.Uint32(cfg[4:])
			want := uint32(0xa5a5a5a5)
			if selected == 1 {
				want = 3 // Device bit 33 plus VIRTIO_F_VERSION_1.
			}
			if selected > 1 || features != want {
				t.Errorf("inconsistent feature snapshot: selector=%d features=%#x", selected, features)

				return
			}
			if binary.LittleEndian.Uint16(cfg[18:]) != 2 {
				t.Error("queue count changed during configuration")

				return
			}
		}
	})
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("configuration deadlocked during a reentrant device callback")
	}

	writeCfg(tr, 20, 0, 1)
	if len(dev.active) != 0 || dev.resets != 201 || readCfg(tr, 28, 2) != 0 {
		t.Fatal("reset retained a queue or lost a reset callback")
	}
}

func TestModernResetWaitsForQueuePublication(t *testing.T) {
	t.Parallel()

	dev := &blockingQueueModernDev{
		mockModernDev: mockModernDev{numQ: 1},
		readyEntered:  make(chan struct{}),
		releaseReady:  make(chan struct{}),
		resetEntered:  make(chan struct{}),
	}
	tr := NewModernTransport(dev, make([]byte, 0x10000), func() error { return nil })
	writeCfg(tr, 32, 0x1000, 4)
	writeCfg(tr, 40, 0x2000, 4)
	writeCfg(tr, 48, 0x3000, 4)
	readyDone := make(chan struct{})
	go func() {
		writeCfg(tr, 28, 1, 2)
		close(readyDone)
	}()
	<-dev.readyEntered
	resetDone := make(chan struct{})
	go func() {
		writeCfg(tr, 20, 0, 1)
		close(resetDone)
	}()
	select {
	case <-dev.resetEntered:
		close(dev.releaseReady)
		t.Fatal("reset overtook an unfinished queue publication")
	case <-time.After(20 * time.Millisecond):
	}
	close(dev.releaseReady)
	<-readyDone
	select {
	case <-resetDone:
	case <-time.After(time.Second):
		t.Fatal("reset did not resume after queue publication")
	}
	if dev.active.Load() || readCfg(tr, 28, 2) != 0 {
		t.Fatal("queue publication survived reset")
	}
}

type blockingQueueModernDev struct {
	mockModernDev
	readyEntered chan struct{}
	releaseReady chan struct{}
	resetEntered chan struct{}
	active       atomic.Bool
}

func (d *blockingQueueModernDev) QueueReady(_ int, _ *SplitQueue) {
	close(d.readyEntered)
	<-d.releaseReady
	d.active.Store(true)
}

func (d *blockingQueueModernDev) Reset() {
	close(d.resetEntered)
	d.active.Store(false)
}

func TestModernINTxAcknowledgement(t *testing.T) {
	t.Parallel()

	tr := NewModernTransport(&mockModernDev{numQ: 1}, nil, func() error {
		t.Error("legacy injection callback used after configuring an INTx line")

		return nil
	})
	levels := []bool{}
	tr.SetIRQLine(func(level bool) error {
		levels = append(levels, level)
		if pending := atomic.LoadUint32(&tr.isr) != 0; pending != level {
			t.Errorf("line level %v disagrees with pending ISR %v", level, pending)
		}

		return nil
	})
	if err := tr.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := tr.ConfigChanged(); err != nil {
		t.Fatal(err)
	}
	data := []byte{0}
	tr.MMIO(isrCfgOffset, data, false)
	if data[0] != 3 {
		t.Fatalf("ISR = %#x, want queue and configuration bits", data[0])
	}
	if !slices.Equal(levels, []bool{true, true, false}) {
		t.Fatalf("INTx levels = %v", levels)
	}
	if err := tr.ReinjectIfPending(); err != nil {
		t.Fatal(err)
	}
	if len(levels) != 3 {
		t.Fatal("acknowledged interrupt was reasserted")
	}
	if err := tr.Interrupt(); err != nil {
		t.Fatal(err)
	}
	writeCfg(tr, 20, 0, 1)
	if levels[len(levels)-1] {
		t.Fatal("device reset left INTx asserted")
	}
}

func TestModernINTxConcurrentAcknowledgement(t *testing.T) {
	t.Parallel()

	tr := NewModernTransport(&mockModernDev{numQ: 1}, nil, func() error { return nil })
	var high bool
	tr.SetIRQLine(func(level bool) error {
		high = level
		if pending := atomic.LoadUint32(&tr.isr) != 0; pending != level {
			t.Errorf("concurrent ISR/INTx disagreement: pending=%v high=%v", pending, level)
		}

		return nil
	})
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 1000 {
			if err := tr.Interrupt(); err != nil {
				t.Error(err)
			}
		}
	})
	workers.Go(func() {
		for range 1000 {
			if err := tr.ConfigChanged(); err != nil {
				t.Error(err)
			}
		}
	})
	workers.Go(func() {
		for range 1000 {
			tr.MMIO(isrCfgOffset, make([]byte, 1), false)
		}
	})
	workers.Wait()
	tr.MMIO(isrCfgOffset, make([]byte, 1), false)
	if high {
		t.Fatal("final acknowledgement left INTx high")
	}
}

func TestModernINTxAcknowledgementError(t *testing.T) {
	t.Parallel()

	tr := NewModernTransport(&mockModernDev{numQ: 1}, nil, func() error { return nil })
	tr.SetIRQLine(func(level bool) error {
		if !level {
			return io.ErrClosedPipe
		}

		return nil
	})
	if err := tr.Interrupt(); err != nil {
		t.Fatal(err)
	}
	tr.MMIO(isrCfgOffset, make([]byte, 1), false)
	if err := tr.ReinjectIfPending(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("lost acknowledgement error: %v", err)
	}
	if err := tr.Interrupt(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("lost acknowledgement error during next interrupt: %v", err)
	}
}
