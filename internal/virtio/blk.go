package virtio

import (
	"encoding/binary"
	"errors"
	"io"
	"log"
	"sync"
	"time"

	"github.com/define42/gokvm/internal/disk"
	"github.com/define42/gokvm/internal/guestmem"
	"github.com/define42/gokvm/internal/pci"
)

const (
	SectorSize = 512

	blkFeatureRO    = 1 << 5
	blkFeatureFlush = 1 << 9

	blkNumQueues = 1
	blkQueue     = 0

	// BlkMMIOBase is the guest-physical base of the block device's memory
	// BAR. It sits in the 32-bit MMIO hole, distinct from the net device's
	// region; the pci layer follows any reassignment the guest performs.
	BlkMMIOBase = 0xd001_0000
)

// LoadU16 reads a uint16 through a non-inlined function
// call, preventing the compiler from caching the value
// across iterations. This is needed for shared memory
// fields (AvailRing.Idx, UsedRing.Idx) that are written
// by KVM vCPU threads via unsafe.Pointer. Go goroutines must still
// synchronize concurrent reads and writes; this is not an atomic load.
//
//go:noinline
func LoadU16(p *uint16) uint16 { return *p }

// StoreAddU16 updates a shared ring index through a non-inlined call.
// The caller must be the sole writer or externally serialize access;
// this is not an atomic read-modify-write or a Go synchronization primitive.
//
//go:noinline
func StoreAddU16(p *uint16, delta uint16) {
	*p += delta
}

// Blk is a modern (virtio 1.0) block device.
var _ pci.CapsAndMMIO = (*Blk)(nil)

var (
	errReadOnlyBlk = errors.New("virtio-blk: write to read-only device")
	ErrBlkDesc     = errors.New("invalid virtio-blk descriptor chain")
)

type Blk struct {
	*ModernTransport

	image disk.Image

	// capacity is the device size in 512-byte sectors.
	capacity uint64
	readOnly bool
	mmioBase uint32

	VirtQueue    [blkNumQueues]*SplitQueue
	LastAvailIdx [blkNumQueues]uint16

	kick      chan interface{}
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
	queueMu   sync.Mutex
	dirty     bool // Protected by queueMu; retained across device resets.

	irq         uint8
	IRQInjector IRQInjector
}

// SetInitialMMIOBase assigns the initial, MMIOSize-aligned BAR address. Call it
// before registering the device on the PCI bus; the bus owns guest BAR writes.
func (v *Blk) SetInitialMMIOBase(base uint32) {
	v.mmioBase = base
}

func (v *Blk) GetDeviceHeader() pci.DeviceHeader {
	base := v.mmioBase
	if base == 0 {
		base = BlkMMIOBase
	}

	return pci.DeviceHeader{
		// 0x1040 + virtio device id (2 = block). The 0x1042 id marks a
		// non-transitional device, so the driver uses the modern
		// interface and requires VIRTIO_F_VERSION_1.
		DeviceID:    0x1042,
		VendorID:    0x1AF4,
		RevisionID:  1,    // Modern virtio PCI revision (required for firmware discovery).
		ClassCode:   0x01, // Mass storage controller
		Subclass:    0x00, // SCSI storage controller
		HeaderType:  0,
		SubsystemID: 0x1042, // Non-transitional subsystem ID.
		// Memory space enable | bus master.
		Command: 0x6,
		// Bit 4: capabilities list present.
		Status:              0x10,
		CapabilitiesPointer: capCommonAt,
		BAR: [6]uint32{
			// BAR0: 32-bit non-prefetchable memory BAR (low nibble 0).
			base,
		},
		// https://github.com/torvalds/linux/blob/fb3b0673b7d5b477ed104949450cd511337ba3c6/drivers/pci/setup-irq.c#L30-L55
		InterruptPin: 1,
		// https://www.webopedia.com/reference/irqnumbers/
		InterruptLine: v.irq,
	}
}

// ModernDevice implementation.

// DeviceFeatures advertises device-specific block features; VIRTIO_F_VERSION_1
// is added by the transport.
func (v *Blk) DeviceFeatures() uint64 {
	if v.readOnly {
		return blkFeatureRO | blkFeatureFlush
	}

	return blkFeatureFlush
}

func (v *Blk) NumQueues() int { return blkNumQueues }

// DeviceConfigLen covers struct virtio_blk_config.capacity (the only field the
// driver reads without further feature negotiation).
func (v *Blk) DeviceConfigLen() int { return 8 }

// ReadDeviceConfig serves struct virtio_blk_config: capacity (le64, in
// 512-byte sectors) at offset 0.
func (v *Blk) ReadDeviceConfig(offset uint64, data []byte) {
	var cfg [8]byte

	binary.LittleEndian.PutUint64(cfg[:], v.capacity)
	zero(data)

	if offset >= uint64(len(cfg)) {
		return
	}

	end := offset + uint64(len(data))
	if end > uint64(len(cfg)) {
		end = uint64(len(cfg))
	}

	copy(data, cfg[offset:end])
}

func (v *Blk) WriteDeviceConfig(offset uint64, data []byte) {}

func (v *Blk) QueueReady(idx int, q *SplitQueue) {
	v.queueMu.Lock()
	defer v.queueMu.Unlock()
	if idx == blkQueue {
		v.VirtQueue[idx] = q
		v.LastAvailIdx[idx] = 0
	}
}

// Reset discards queue mappings after any active I/O batch finishes.
func (v *Blk) Reset() {
	v.queueMu.Lock()
	defer v.queueMu.Unlock()
	v.VirtQueue = [blkNumQueues]*SplitQueue{}
	v.LastAvailIdx = [blkNumQueues]uint16{}
}

func (v *Blk) Notify(idx int) {
	// Non-blocking kick; the IO thread also polls on a ticker.
	select {
	case v.kick <- true:
	default:
	}
}

func (v *Blk) IOThreadEntry() {
	log.Println("virtio-blk: IOThreadEntry started")

	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-v.done:
			log.Println("virtio-blk: IOThreadEntry " +
				"received done signal")

			return
		case <-v.kick:
			for v.IO() == nil {
			}

			_ = v.ReinjectIfPending()
		case <-ticker.C:
			for v.IO() == nil {
			}

			_ = v.ReinjectIfPending()
		}
	}
}

type BlkReq struct {
	Type   uint32
	_      uint32
	Sector uint64
}

type blkPendingRequest struct {
	id, length uint32
	status     *byte
	data       []byte
	offset     int64
	result     byte
	write      bool
	flush      bool
}

func (v *Blk) IO() error {
	v.queueMu.Lock()
	defer v.queueMu.Unlock()
	select {
	case <-v.done:
		return io.ErrClosedPipe
	default:
	}

	const sel = blkQueue
	q := v.VirtQueue[sel]
	if q == nil {
		return ErrVQNotInit
	}
	queueSize := uint16(QueueSize)
	if q.Size != 0 && q.Size <= QueueSize {
		queueSize = q.Size
	}
	// Drivers negotiating FLUSH control durability boundaries explicitly.
	// Other drivers retain writethrough: one Sync covers a bounded batch,
	// and no write completes before that Sync succeeds.
	writeback := v.negotiatedFeatures()&blkFeatureFlush != 0
	count := LoadU16(&q.Avail.Idx) - v.LastAvailIdx[sel]
	if count == 0 {
		return ErrNoTxPacket
	}
	if count > queueSize {
		return ErrBlkDesc
	}
	var pending [QueueSize]blkPendingRequest
	// Snapshot validated request fields and slices before I/O. A guest may
	// change descriptors while another request is blocked in the backing store;
	// never reread unchecked addresses or next indices after this pass.
	for i := uint16(0); i < count; i++ {
		head := q.Avail.Ring[(v.LastAvailIdx[sel]+i)%queueSize]
		if err := v.snapshotRequest(q, queueSize, head, &pending[i], writeback); err != nil {
			return err
		}
	}

	needsSync := false
	for i := uint16(0); i < count; i++ {
		c := &pending[i]
		if c.result == 2 {
			continue
		}
		var ioErr error
		switch {
		case c.flush:
			ioErr = v.syncImage()
		case c.write && v.readOnly:
			ioErr = errReadOnlyBlk
		case c.write:
			// A failed write can still have changed data or QCOW2 metadata.
			v.dirty = true
			_, ioErr = v.image.WriteAt(c.data, c.offset)
			needsSync = true
		default:
			_, ioErr = v.image.ReadAt(c.data, c.offset)
		}
		if ioErr != nil {
			c.result = 1 // VIRTIO_BLK_S_IOERR
		}
	}
	if needsSync && !writeback {
		if err := v.syncImage(); err != nil {
			for i := uint16(0); i < count; i++ {
				if pending[i].write {
					pending[i].result = 1
				}
			}
		}
	}
	usedIdx := LoadU16(&q.Used.Idx)
	for i := uint16(0); i < count; i++ {
		c := &pending[i]
		*c.status = c.result
		q.Used.Ring[(usedIdx+i)%queueSize] = SplitUsedElem{ID: c.id, Len: c.length}
	}
	StoreAddU16(&q.Used.Idx, count)
	v.LastAvailIdx[sel] += count

	return v.Interrupt()
}

// snapshotRequest retains only validated guest slices and command fields.
func (v *Blk) snapshotRequest(q *SplitQueue, queueSize, descID uint16, c *blkPendingRequest, writeback bool) error {
	c.id = uint32(descID)
	var seen [QueueSize]bool
	for j := uint16(0); j < queueSize; j++ {
		if descID >= queueSize || seen[descID] {
			return ErrBlkDesc
		}
		seen[descID] = true
		desc := q.Desc[descID]
		if !guestmem.ValidRange(v.Mem, desc.Addr, uint64(desc.Len)) ||
			(j == 0 && desc.Len < 16) {
			return ErrBlkDesc
		}
		data := v.Mem[desc.Addr : desc.Addr+uint64(desc.Len)]
		c.length += desc.Len
		if j == 0 {
			typ := binary.LittleEndian.Uint32(data)
			c.write = typ == 1
			c.flush = typ == 4 && writeback
			c.offset = int64(binary.LittleEndian.Uint64(data[8:]) * SectorSize)
			if typ != 0 && typ != 1 && !c.flush {
				// Unsupported commands, including unnegotiated FLUSH, must
				// never be mistaken for data reads or writes.
				c.result = 2 // VIRTIO_BLK_S_UNSUPP
			}
		} else if c.result != 2 && !c.flush && j == 1 {
			c.data = data
		}
		last := j == 2
		if c.result == 2 || c.flush {
			last = desc.Flags&descFNext == 0
		}
		if last {
			if j == 0 || len(data) == 0 || ((c.result == 2 || c.flush) && desc.Flags&descFWrite == 0) {
				return ErrBlkDesc
			}
			c.status = &data[0]
			if c.flush {
				c.length = 1 // Only the status byte is written by FLUSH.
			}

			break
		}
		descID = desc.Next
	}
	if c.status == nil {
		return ErrBlkDesc
	}

	return nil
}

// Read and Write satisfy pci.Device. A modern device has no IO-port BAR, so
// these are no-ops.
func (v *Blk) Read(port uint64, bytes []byte) error { return nil }

func (v *Blk) Write(port uint64, bytes []byte) error { return nil }

func (v *Blk) IOPort() uint64 { return 0 }

func (v *Blk) Size() uint64 { return 0 }

func (v *Blk) syncImage() error {
	if err := v.image.Sync(); err != nil {
		return err
	}
	v.dirty = false

	return nil
}

func (v *Blk) Close() error {
	log.Println("virtio-blk: Close called")
	v.closeOnce.Do(func() {
		close(v.done)
		v.queueMu.Lock()
		defer v.queueMu.Unlock()
		var syncErr error
		if v.dirty {
			syncErr = v.syncImage()
		}
		v.closeErr = errors.Join(syncErr, v.image.Close())
	})

	return v.closeErr
}

func NewBlk(path string, irq uint8, irqInjector IRQInjector, mem []byte) (*Blk, error) {
	image, err := disk.Open(path)
	if err != nil {
		return nil, err
	}

	return newBlk(image, false, irq, irqInjector, mem), nil
}

func NewReadOnlyBlk(path string, irq uint8, irqInjector IRQInjector, mem []byte) (*Blk, error) {
	image, err := disk.OpenReadOnly(path)
	if err != nil {
		return nil, err
	}

	return newBlk(image, true, irq, irqInjector, mem), nil
}

func newBlk(image disk.Image, readOnly bool, irq uint8, irqInjector IRQInjector, mem []byte) *Blk {
	res := &Blk{
		image:       image,
		capacity:    uint64(image.Size()) / SectorSize,
		readOnly:    readOnly,
		irq:         irq,
		IRQInjector: irqInjector,
		kick:        make(chan interface{}, QueueSize),
		done:        make(chan struct{}),
	}

	res.ModernTransport = NewModernTransport(res, mem, func() error {
		return irqInjector.InjectVirtioBlkIRQ()
	})

	return res
}
