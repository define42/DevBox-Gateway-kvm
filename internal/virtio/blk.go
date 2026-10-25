package virtio

import (
	"encoding/binary"
	"errors"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
	"github.com/define42/DevBox-Gateway-kvm/internal/guestmem"
	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
)

const (
	SectorSize = 512

	blkFeatureSegMax = 1 << 2
	blkFeatureRO     = 1 << 5
	blkFeatureFlush  = 1 << 9

	blkNumQueues = 1
	blkQueue     = 0

	// blkMaxSegments is the seg_max offered with VIRTIO_BLK_F_SEG_MAX. A
	// request's data buffers share the ring with its header and status, and
	// Linux does not limit seg_max to the ring size when indirect descriptors
	// are not negotiated. Keep it well below the ring, so that one maximum-size
	// request per read worker fits at once.
	blkMaxSegments = QueueSize/blkReadWorkers - 2

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
	// writes counts finished write attempts and synced the count the last
	// successful Sync covered; they differ while data may not be durable.
	// Both are retained across device resets.
	writes, synced atomic.Uint64
	// Reuse snapshots and lazily started read workers under queueMu. Only the
	// queue owner publishes completions; Close drains and joins all workers.
	// segments holds the batch's data buffers; each uses a ring descriptor,
	// so a valid batch never needs more entries.
	pending     [QueueSize]blkPendingRequest
	segments    [QueueSize]blkSegment
	readJobs    chan *blkPendingRequest
	readDone    chan *blkPendingRequest
	readWorkers sync.WaitGroup
	readScratch [][]byte          // The queue owner's buffer list for vector reads.
	vector      disk.VectorReader // The image, when it supports vectored reads.
	// FLUSH requests sync on flushWorker while later requests proceed.
	// flushing counts those the queue owner has not yet published; both
	// channels have room for all of them. Protected by queueMu.
	flushJobs   chan blkFlush
	flushDone   chan blkFlush
	flushWorker sync.WaitGroup
	flushing    int

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
		return blkFeatureRO | blkFeatureFlush | blkFeatureSegMax
	}

	return blkFeatureFlush | blkFeatureSegMax
}

func (v *Blk) NumQueues() int { return blkNumQueues }

// DeviceConfigLen covers struct virtio_blk_config through seg_max.
func (v *Blk) DeviceConfigLen() int { return 16 }

// ReadDeviceConfig serves struct virtio_blk_config: capacity (le64, in
// 512-byte sectors) at offset 0, size_max (unused, SIZE_MAX is not offered)
// at 8 and seg_max at 12.
func (v *Blk) ReadDeviceConfig(offset uint64, data []byte) {
	var cfg [16]byte

	binary.LittleEndian.PutUint64(cfg[:], v.capacity)
	binary.LittleEndian.PutUint32(cfg[12:], blkMaxSegments)
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
		v.finishFlushes()
		v.VirtQueue[idx] = q
		v.LastAvailIdx[idx] = 0
		v.Notify(idx)
	}
}

// Reset discards queue mappings after any active I/O batch and FLUSH finish.
func (v *Blk) Reset() {
	v.queueMu.Lock()
	defer v.queueMu.Unlock()
	v.finishFlushes()
	v.VirtQueue = [blkNumQueues]*SplitQueue{}
	v.LastAvailIdx = [blkNumQueues]uint16{}
}

func (v *Blk) Notify(idx int) {
	// Coalesced kicks remain pending while the worker drains the queue.
	select {
	case v.kick <- true:
	default:
	}
}

func (v *Blk) IOThreadEntry() {
	log.Println("virtio-blk: IOThreadEntry started")

	poll := newDevicePoll(time.Millisecond)
	defer poll.timer.Stop()

	for {
		active := true
		select {
		case <-v.done:
			log.Println("virtio-blk: IOThreadEntry " +
				"received done signal")

			return
		case <-v.kick:
		case <-poll.interruptWake(v.ModernTransport):
		case <-poll.timer.C:
			active = false
		}
		for v.IO() == nil {
		}
		_ = v.ReinjectIfPending()
		poll.reset(active || v.interruptPending())
	}
}

type BlkReq struct {
	Type   uint32
	_      uint32
	Sector uint64
}

// blkSegment is one validated guest data buffer of a request.
type blkSegment struct {
	data []byte
	addr uint64
}

type blkPendingRequest struct {
	id, length uint32
	status     *byte
	segments   []blkSegment // Data buffers in transfer order, within Blk.segments.
	lo, hi     uint64       // Guest bytes a read may write: data and status.
	statusAddr uint64
	offset     int64
	result     byte
	write      bool
	flush      bool
}

// IO services the requests the driver has posted. FLUSH requests sync in the
// background while later requests proceed; IO then waits for them unless the
// driver posts more requests, which the next call takes first.
func (v *Blk) IO() error {
	v.queueMu.Lock()
	defer v.queueMu.Unlock()
	select {
	case <-v.done:
		return io.ErrClosedPipe
	default:
	}

	q := v.VirtQueue[blkQueue]
	if q == nil {
		return ErrVQNotInit
	}
	err := v.processQueue(q)
	if v.flushing > 0 {
		if flushErr := v.awaitFlushes(q); err == nil || errors.Is(err, ErrNoTxPacket) {
			err = flushErr
		}
	}

	return err
}

// processQueue runs one batch of posted requests, leaving any FLUSH running.
func (v *Blk) processQueue(q *SplitQueue) error {
	const sel = blkQueue
	queueSize := q.Size
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
	pending := v.pending[:count]
	defer v.releaseBatch(pending) // Do not retain guest buffers between batches/resets.
	// Snapshot validated request fields and slices before I/O. A guest may
	// change descriptors while another request is blocked in the backing store;
	// never reread unchecked addresses or next indices after this pass.
	free := v.segments[:]
	for i := uint16(0); i < count; i++ {
		head := q.Avail.Ring[(v.LastAvailIdx[sel]+i)%queueSize]
		used, err := v.snapshotRequest(q, queueSize, head, &pending[i], writeback, free)
		if err != nil {
			return err
		}
		free = free[used:]
	}
	// Every snapshotted request now completes, here or after its FLUSH.
	v.LastAvailIdx[sel] += count

	needsSync := false
	var interruptErr error
	for i := uint16(0); i < count; {
		reads := 0
		if count-i > 1 {
			reads = independentBlkReads(q, pending[i:count])
		}
		if reads > 1 {
			end := i + uint16(reads) //nolint:gosec // reads is bounded by QueueSize.
			if err := v.readRequests(q, queueSize, pending[i:end], writeback); err != nil && interruptErr == nil {
				interruptErr = err
			}
			i = end

			continue
		}
		c := &pending[i]
		if c.flush && c.result == 0 {
			// Every earlier write has returned, so a Sync starting now covers
			// it. Later requests need not wait for this one.
			if err := v.queueFlush(q, queueSize, c); err != nil && interruptErr == nil {
				interruptErr = err
			}
			i++

			continue
		}
		if c.result == 0 {
			var ioErr error
			switch {
			case c.write && v.readOnly:
				ioErr = errReadOnlyBlk
			case c.write:
				ioErr = v.writeRequest(c)
				// A failed write can still have changed data or QCOW2 metadata.
				v.writes.Add(1)
				needsSync = true
			default:
				ioErr = v.readRequest(c, &v.readScratch)
			}
			if ioErr != nil {
				c.result = 1 // VIRTIO_BLK_S_IOERR
			}
		}

		// A writeback driver controls durability with FLUSH. Publish each
		// finished request before starting potentially blocking later I/O.
		if writeback {
			if err := v.completeRequests(q, queueSize, pending[i:i+1]); err != nil && interruptErr == nil {
				interruptErr = err
			}
		}
		i++
	}
	if writeback {
		return interruptErr
	}
	if needsSync {
		if err := v.syncImage(); err != nil {
			for i := uint16(0); i < count; i++ {
				if pending[i].write {
					pending[i].result = 1
				}
			}
		}
	}

	return v.completeRequests(q, queueSize, pending[:count])
}

// setBounds records the smallest guest range holding every buffer of c. Reads
// with disjoint bounds cannot alias, which settles the common case cheaply.
func (c *blkPendingRequest) setBounds() {
	c.lo, c.hi = c.statusAddr, c.statusAddr+1
	for _, segment := range c.segments {
		if len(segment.data) != 0 {
			c.lo = min(c.lo, segment.addr)
			c.hi = max(c.hi, segment.addr+uint64(len(segment.data)))
		}
	}
}

// releaseBatch drops the batch's guest buffers, including every pool entry its
// requests referenced. A single defer keeps processQueue's defers open-coded.
func (v *Blk) releaseBatch(pending []blkPendingRequest) {
	for i := range pending {
		clear(pending[i].segments)
	}
	clear(pending)
}

// writeRequest stores the data buffers at consecutive disk offsets.
func (v *Blk) writeRequest(c *blkPendingRequest) error {
	offset := c.offset
	for _, segment := range c.segments {
		n, err := v.image.WriteAt(segment.data, offset)
		if err != nil {
			return err
		}
		if n != len(segment.data) {
			return io.ErrShortWrite
		}
		offset += int64(n)
	}

	return nil
}

// completeRequests transfers ownership of finished requests back to the guest.
// Reads and FLUSH requests may finish out of order; queueMu serializes all
// used-ring publication. Callers must not touch a completed request's guest
// buffers after Used.Idx moves.
func (v *Blk) completeRequests(q *SplitQueue, queueSize uint16, pending []blkPendingRequest) error {
	count := uint16(len(pending)) //nolint:gosec // pending is bounded by QueueSize.
	usedIdx := LoadU16(&q.Used.Idx)
	for i := uint16(0); i < count; i++ {
		c := &pending[i]
		*c.status = c.result
		q.Used.Ring[(usedIdx+i)%queueSize] = SplitUsedElem{ID: c.id, Len: c.length}
	}
	completionBarrier()
	StoreAddU16(&q.Used.Idx, count)

	return v.notifyUsed(blkQueue, q)
}

// snapshotRequest retains only validated guest slices and command fields. A
// request is a 16-byte header, up to blkMaxSegments data buffers and a status
// byte. Each guest descriptor is read once, so a guest changing descriptors
// during I/O cannot change what was validated. Data buffers are stored in
// free; it returns how many entries the request kept.
func (v *Blk) snapshotRequest(q *SplitQueue, queueSize, descID uint16, c *blkPendingRequest, writeback bool,
	free []blkSegment,
) (int, error) {
	c.id = uint32(descID)
	// Buffers go straight into free, so releaseBatch also sees those written
	// before a request is rejected.
	c.segments = free[:0]
	var seen [QueueSize]bool
	var header, status SplitDesc
	descriptors := 0
	for {
		if descID >= queueSize || seen[descID] {
			return 0, ErrBlkDesc
		}
		seen[descID] = true
		desc := q.Desc[descID]
		if !guestmem.ValidRange(v.Mem, desc.Addr, uint64(desc.Len)) {
			return 0, ErrBlkDesc
		}
		c.length += desc.Len
		switch descriptors {
		case 0:
			header = desc
		case 1:
			status = desc
		default:
			// The previous candidate for the status byte was a data buffer.
			// Only a batch whose chains share descriptors can exhaust free.
			if len(c.segments) == cap(c.segments) {
				return 0, ErrBlkDesc
			}
			c.segments = append(c.segments, blkSegment{
				data: v.Mem[status.Addr : status.Addr+uint64(status.Len)], addr: status.Addr,
			})
			status = desc
		}
		descriptors++
		if desc.Flags&descFNext == 0 {
			break
		}
		descID = desc.Next
	}
	if descriptors < 2 || header.Len < 16 || status.Len == 0 {
		return 0, ErrBlkDesc
	}

	hdr := v.Mem[header.Addr : header.Addr+16]
	typ := binary.LittleEndian.Uint32(hdr)
	c.write = typ == 1
	c.flush = typ == 4 && writeback
	sector := binary.LittleEndian.Uint64(hdr[8:])
	if sector > v.capacity {
		c.result = 1 // VIRTIO_BLK_S_IOERR
	} else {
		c.offset = int64(sector * uint64(SectorSize))
	}
	if typ != 0 && typ != 1 && !c.flush {
		// Unsupported commands, including unnegotiated FLUSH, must
		// never be mistaken for data reads or writes.
		c.result = 2 // VIRTIO_BLK_S_UNSUPP
	}
	if (c.result == 2 || c.flush) && status.Flags&descFWrite == 0 {
		return 0, ErrBlkDesc
	}
	c.status = &v.Mem[status.Addr]
	c.statusAddr = status.Addr
	if c.flush || c.result == 2 {
		if c.flush {
			c.length = 1 // Only the status byte is written by FLUSH.
		}
		// These commands transfer no data; return the pool entries.
		clear(c.segments)
		c.segments = nil

		return 0, nil
	}

	c.segments = c.segments[:len(c.segments):len(c.segments)]
	c.setBounds()
	if c.result == 0 {
		var dataLen uint64
		for _, segment := range c.segments {
			dataLen += uint64(len(segment.data))
		}
		capacityBytes := v.capacity * uint64(SectorSize)
		offset := uint64(c.offset)
		if offset > capacityBytes || dataLen > capacityBytes-offset {
			c.result = 1 // VIRTIO_BLK_S_IOERR
		}
	}

	return len(c.segments), nil
}

// Read and Write satisfy pci.Device. A modern device has no IO-port BAR, so
// these are no-ops.
func (v *Blk) Read(port uint64, bytes []byte) error { return nil }

func (v *Blk) Write(port uint64, bytes []byte) error { return nil }

func (v *Blk) IOPort() uint64 { return 0 }

func (v *Blk) Size() uint64 { return 0 }

// syncImage makes every finished write durable. A write still running counts
// only once it returns, so it stays dirty. Each stored count was covered by its
// own Sync, so overlapping Syncs can at worst cause another Sync at Close.
func (v *Blk) syncImage() error {
	written := v.writes.Load()
	if err := v.image.Sync(); err != nil {
		return err
	}
	v.synced.Store(written)

	return nil
}

func (v *Blk) Close() error {
	log.Println("virtio-blk: Close called")
	v.closeOnce.Do(func() {
		close(v.done)
		v.queueMu.Lock()
		defer v.queueMu.Unlock()
		if v.readJobs != nil {
			close(v.readJobs)
			v.readWorkers.Wait()
		}
		if v.flushJobs != nil {
			// Finish running Syncs before closing the image. A closed device
			// publishes nothing, so their completions are dropped.
			close(v.flushJobs)
			v.flushWorker.Wait()
			v.flushing = 0
		}
		var syncErr error
		if v.writes.Load() != v.synced.Load() {
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

// NewBlkWithSize opens a QCOW2 disk with the admitted virtual capacity.
func NewBlkWithSize(path string, expected int64, irq uint8, irqInjector IRQInjector, mem []byte) (*Blk, error) {
	image, err := disk.OpenQCOW2WithSize(path, expected)
	if err != nil {
		return nil, err
	}

	return newBlk(image, false, irq, irqInjector, mem), nil
}

// NewRawBlk opens a raw disk for low-level tests and benchmark harnesses.
func NewRawBlk(path string, irq uint8, irqInjector IRQInjector, mem []byte) (*Blk, error) {
	image, err := disk.OpenRaw(path)
	if err != nil {
		return nil, err
	}

	return newBlk(image, false, irq, irqInjector, mem), nil
}

// NewReadOnlyBlk opens a raw read-only disk, such as a configuration seed ISO.
func NewReadOnlyBlk(path string, irq uint8, irqInjector IRQInjector, mem []byte) (*Blk, error) {
	image, err := disk.OpenRawReadOnly(path)
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
		kick:        make(chan interface{}, 1),
		done:        make(chan struct{}),
	}

	res.vector, _ = image.(disk.VectorReader)
	res.ModernTransport = NewModernTransport(res, mem, func() error {
		return irqInjector.InjectVirtioBlkIRQ()
	})

	return res
}
