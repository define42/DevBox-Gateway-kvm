package virtio

import (
	"errors"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/guestmem"
	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
)

var (
	ErrIONotPermit = errors.New("IO is not permitted for virtio device")
	ErrNoTxPacket  = errors.New("no packet for tx")
	ErrNoRxPacket  = errors.New("no packet for rx")
	ErrVQNotInit   = errors.New("vq not initialized")
	ErrNoRxBuf     = errors.New("no buffer found for rx")
	ErrNetDesc     = errors.New("invalid virtio-net descriptor chain")
)

const (
	// netHdrLen is sizeof(struct virtio_net_hdr_v1). Under
	// VIRTIO_F_VERSION_1 the header is always 12 bytes (it carries
	// num_buffers unconditionally), unlike the 10-byte legacy header.
	netHdrLen = 12

	// Virtqueue indices.
	netRxQueue   = 0
	netTxQueue   = 1
	netNumQueues = 2

	// NetMMIOBase is the guest-physical base of the net device's memory
	// BAR. It sits in the 32-bit MMIO hole above the (small) guest RAM;
	// the pci layer follows any reassignment the guest performs.
	NetMMIOBase = 0xd000_0000
)

// Net is a modern PCI device exposing capabilities and a memory BAR.
var _ pci.CapsAndMMIO = (*Net)(nil)

// Net is a modern (virtio 1.0) network device.
type Net struct {
	*ModernTransport

	tap io.ReadWriter

	// VirtQueue holds the split virtqueues once the driver enables them.
	VirtQueue    [netNumQueues]*SplitQueue
	LastAvailIdx [netNumQueues]uint16

	txKick    chan interface{}
	rxKick    chan os.Signal
	rxNotify  chan struct{}
	rxReady   <-chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
	queueMu   [netNumQueues]sync.Mutex
	rxPacket  [netHdrLen + 65536]byte
	txPacket  [netHdrLen + 65536]byte

	irq         uint8
	IRQInjector IRQInjector
}

func (v *Net) GetDeviceHeader() pci.DeviceHeader {
	return pci.DeviceHeader{
		// 0x1040 + virtio device id (1 = net). The 0x1041 id marks a
		// non-transitional device, so the driver uses the modern
		// interface and requires VIRTIO_F_VERSION_1.
		DeviceID:    0x1041,
		VendorID:    0x1AF4,
		RevisionID:  1,    // Modern virtio PCI revision (required for firmware discovery).
		ClassCode:   0x02, // Network controller
		Subclass:    0x00, // Ethernet controller
		HeaderType:  0,
		SubsystemID: 0x1041, // Non-transitional subsystem ID.
		// Memory space enable | bus master.
		Command: 0x6,
		// Bit 4: capabilities list present.
		Status:              0x10,
		CapabilitiesPointer: capCommonAt,
		BAR: [6]uint32{
			// BAR0: 32-bit non-prefetchable memory BAR (low nibble 0).
			uint32(NetMMIOBase),
		},
		// https://github.com/torvalds/linux/blob/fb3b0673b7d5b477ed104949450cd511337ba3c6/drivers/pci/setup-irq.c#L30-L55
		InterruptPin: 1,
		// https://www.webopedia.com/reference/irqnumbers/
		InterruptLine: v.irq,
	}
}

// ModernDevice implementation.

// DeviceFeatures advertises no device-specific features; the guest assigns a
// random MAC and assumes the link is always up. VIRTIO_F_VERSION_1 is added by
// the transport.
func (v *Net) DeviceFeatures() uint64 { return 0 }

func (v *Net) NumQueues() int { return netNumQueues }

// DeviceConfigLen covers struct virtio_net_config (mac, status,
// max_virtqueue_pairs). The fields are unused since no features expose them,
// but the region is still described by the device-cfg capability.
func (v *Net) DeviceConfigLen() int { return 12 }

func (v *Net) ReadDeviceConfig(offset uint64, data []byte) { zero(data) }

func (v *Net) WriteDeviceConfig(offset uint64, data []byte) {}

func (v *Net) QueueReady(idx int, q *SplitQueue) {
	if idx < 0 || idx >= netNumQueues {
		return
	}

	v.queueMu[idx].Lock()
	v.VirtQueue[idx] = q
	v.LastAvailIdx[idx] = 0
	v.queueMu[idx].Unlock()
	v.Notify(idx)
}

// Reset waits for both queue workers before forgetting guest mappings.
func (v *Net) Reset() {
	v.queueMu[netRxQueue].Lock()
	defer v.queueMu[netRxQueue].Unlock()
	v.queueMu[netTxQueue].Lock()
	defer v.queueMu[netTxQueue].Unlock()
	v.VirtQueue = [netNumQueues]*SplitQueue{}
	v.LastAvailIdx = [netNumQueues]uint16{}
}

func (v *Net) Notify(idx int) {
	switch idx {
	case netRxQueue:
		// The backend may already hold packets from when the guest ran out
		// of receive buffers. Retry when the guest replenishes the queue.
		select {
		case v.rxNotify <- struct{}{}:
		default:
		}
	case netTxQueue:
		// TX queue kick: non-blocking send.
		select {
		case v.txKick <- true:
		default:
		}
	default:
		log.Printf("virtio-net: unexpected queue %d", idx)
	}
}

func (v *Net) RxThreadEntry() {
	log.Println("virtio-net: RxThreadEntry started")
	ready := v.rxReady

	for {
		select {
		case <-v.done:
			log.Println("virtio-net: RxThreadEntry " +
				"received done signal")

			return
		case <-v.rxKick:
		case <-v.rxNotify:
		case _, ok := <-ready:
			if !ok {
				ready = nil
			}
		}
		for v.Rx() == nil {
		}
	}
}

func (v *Net) Rx() error {
	const sel = netRxQueue
	v.queueMu[sel].Lock()
	defer v.queueMu[sel].Unlock()

	select {
	case <-v.done:
		return ErrNoRxPacket
	default:
	}

	q := v.VirtQueue[sel]
	if q == nil {
		return ErrVQNotInit
	}

	avail := q.Avail
	used := q.Used

	queueSize := q.Size
	count := LoadU16(&avail.Idx) - v.LastAvailIdx[sel]
	if count == 0 {
		return ErrNoRxBuf
	}
	if count > queueSize {
		return ErrNetDesc
	}

	head := avail.Ring[v.LastAvailIdx[sel]%queueSize]
	buffers, capacity, err := v.rxBuffers(q, head)
	if err != nil {
		return err
	}

	// Read only after the guest posts a buffer: taking a packet earlier
	// would drop DHCP/DNS replies while the receive queue is empty.
	packet := v.rxPacket[:]
	n, err := v.tap.Read(packet[netHdrLen:])
	if err != nil || n == 0 {
		return ErrNoRxPacket
	}
	packet = packet[:netHdrLen+n]
	packet[10] = 1 // virtio_net_hdr_v1.num_buffers

	uidx := LoadU16(&used.Idx)
	used.Ring[uidx%queueSize] = SplitUsedElem{ID: uint32(head)}
	if len(packet) <= capacity {
		used.Ring[uidx%queueSize].Len = uint32(len(packet))
		for _, buf := range buffers {
			copied := copy(buf, packet)
			packet = packet[copied:]
			if len(packet) == 0 {
				break
			}
		}
	}

	// MRG_RXBUF is not advertised: each available chain is one packet.
	// Drop oversized packets without borrowing another available buffer
	// or changing the descriptors owned by the guest.
	v.LastAvailIdx[sel]++
	StoreAddU16(&used.Idx, 1)

	return v.notifyUsed(netRxQueue, q)
}

func (v *Net) rxBuffers(q *SplitQueue, head uint16) ([][]byte, int, error) {
	var buffers [][]byte
	capacity := 0
	descID := head
	queueSize := q.Size
	for range queueSize {
		if descID >= queueSize {
			return nil, 0, ErrNetDesc
		}
		desc := q.Desc[descID]
		if desc.Flags&descFWrite == 0 || desc.Flags & ^uint16(descFWrite|descFNext) != 0 ||
			!guestmem.ValidRange(v.Mem, desc.Addr, uint64(desc.Len)) {
			return nil, 0, ErrNetDesc
		}
		buffers = append(buffers, v.Mem[desc.Addr:desc.Addr+uint64(desc.Len)])
		capacity += int(desc.Len)
		if desc.Flags&descFNext == 0 {
			return buffers, capacity, nil
		}
		descID = desc.Next
	}

	return nil, 0, ErrNetDesc
}

func (v *Net) TxThreadEntry() {
	log.Println("virtio-net: TxThreadEntry started")

	poll := newDevicePoll(10 * time.Millisecond)
	defer poll.timer.Stop()

	for {
		active := true
		select {
		case <-v.done:
			log.Println("virtio-net: TxThreadEntry " +
				"received done signal")

			return
		case <-v.txKick:
		case <-poll.interruptWake(v.ModernTransport):
		case <-poll.timer.C:
			active = false
		}
		for v.Tx() == nil {
		}
		_ = v.ReinjectIfPending()
		poll.reset(active || v.interruptPending())
	}
}

func (v *Net) Tx() error {
	const sel = netTxQueue
	v.queueMu[sel].Lock()
	defer v.queueMu[sel].Unlock()

	select {
	case <-v.done:
		return ErrNoTxPacket
	default:
	}

	q := v.VirtQueue[sel]
	if q == nil {
		return ErrVQNotInit
	}

	avail := q.Avail
	used := q.Used

	if v.LastAvailIdx[sel] == LoadU16(&avail.Idx) {
		return ErrNoTxPacket
	}

	queueSize := q.Size
	count := LoadU16(&avail.Idx) - v.LastAvailIdx[sel]
	if count > queueSize {
		return ErrNetDesc
	}
	for range count {
		select {
		case <-v.done:
			return ErrNoTxPacket
		default:
		}
		// A private reusable packet buffer keeps a fragmented guest packet to
		// one copy per descriptor. BenchmarkNetTx measures zero allocations
		// for both individual fragmented packets and eight-packet batches.
		buf := v.txPacket[:0]
		head := avail.Ring[v.LastAvailIdx[sel]%queueSize]
		descID := head
		var seen [QueueSize]bool
		for {
			if descID >= queueSize || seen[descID] {
				return ErrNetDesc
			}
			seen[descID] = true
			desc := q.Desc[descID]
			if desc.Flags & ^uint16(descFNext) != 0 ||
				!guestmem.ValidRange(v.Mem, desc.Addr, uint64(desc.Len)) || int(desc.Len) > cap(buf)-len(buf) {
				return ErrNetDesc
			}
			buf = append(buf, v.Mem[desc.Addr:desc.Addr+uint64(desc.Len)]...)
			if desc.Flags&descFNext == 0 {
				break
			}
			descID = desc.Next
		}
		if len(buf) < netHdrLen {
			return ErrNetDesc
		}
		payload := buf[netHdrLen:]
		if n, err := v.tap.Write(payload); err != nil {
			return err
		} else if n != len(payload) {
			return io.ErrShortWrite
		}
		uidx := LoadU16(&used.Idx)
		used.Ring[uidx%queueSize] = SplitUsedElem{ID: uint32(head), Len: uint32(len(buf))}
		StoreAddU16(&used.Idx, 1)
		v.LastAvailIdx[sel]++
	}

	// TX completions ignore VRING_AVAIL_F_NO_INTERRUPT. Without EVENT_IDX,
	// Linux napi_tx drivers such as 5.14 can stop a full queue while TX
	// notifications are disabled and still wait for this interrupt; INTx
	// hid that by servicing every queue on any interrupt, MSI-X does not.
	return v.Interrupt(netTxQueue)
}

// Read and Write satisfy pci.Device. A modern device has no IO-port BAR, so
// these are no-ops.
func (v *Net) Read(port uint64, bytes []byte) error { return nil }

func (v *Net) Write(port uint64, bytes []byte) error { return nil }

func (v *Net) IOPort() uint64 { return 0 }

func (v *Net) Size() uint64 { return 0 }

func (v *Net) Close() error {
	v.closeOnce.Do(func() {
		log.Println("virtio-net: Close called")
		signal.Stop(v.rxKick)
		close(v.done)
		if c, ok := v.tap.(io.Closer); ok {
			v.closeErr = c.Close()
		}
	})

	return v.closeErr
}

func NewNet(irq uint8, irqInjector IRQInjector, tap io.ReadWriter, mem []byte) *Net {
	res := &Net{
		irq:         irq,
		IRQInjector: irqInjector,
		txKick:      make(chan interface{}, 1), // Tx drains the whole queue per wakeup.
		rxKick:      make(chan os.Signal, 1),
		rxNotify:    make(chan struct{}, 1),
		done:        make(chan struct{}),
		tap:         tap,
	}

	res.ModernTransport = NewModernTransport(res, mem, func() error {
		return irqInjector.InjectVirtioNetIRQ()
	})

	// Userspace backends signal readiness directly. TAP continues to use
	// SIGIO and its nonblocking file descriptor.
	if backend, ok := tap.(interface{ Ready() <-chan struct{} }); ok {
		res.rxReady = backend.Ready()
	} else {
		signal.Notify(res.rxKick, syscall.SIGIO)
	}

	return res
}
