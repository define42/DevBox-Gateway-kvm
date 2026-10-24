package virtio

import (
	"encoding/binary"
	"errors"
	"image"
	"io"
	"log"
	"sync"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/guestmem"
	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

// virtio-gpu is a display device on the modern (virtio 1.0) PCI transport.
//
// The driver drives two virtqueues:
//
//	controlq (0) : resource lifecycle and 2D blits (the commands below)
//	cursorq  (1) : hardware cursor shape and position updates
//
// A typical frame goes: RESOURCE_CREATE_2D to allocate a host resource,
// RESOURCE_ATTACH_BACKING to point it at guest framebuffer pages, SET_SCANOUT
// to bind it to a display, then per update TRANSFER_TO_HOST_2D (copy guest
// pixels into the host resource) followed by RESOURCE_FLUSH (present it). On
// flush we hand the resource's pixels to a Display sink.
//
// The device implements only 2D graphics. 3D, EDID, blob resources, and
// context-init are not offered.
//
// refs https://docs.oasis-open.org/virtio/virtio/v1.1/csprd01/virtio-v1.1-csprd01.html#x1-3130007

const (
	// Virtqueue indices.
	gpuControlQueue = 0
	gpuCursorQueue  = 1
	gpuNumQueues    = 2

	// GPUMMIOBase is the guest-physical base of the GPU device's memory BAR.
	// It sits in the 32-bit MMIO hole, distinct from the net (0xd000_0000)
	// and blk (0xd001_0000) regions; the pci layer follows any reassignment
	// the guest performs.
	GPUMMIOBase = 0xd002_0000

	// Device-config geometry and defaults.
	gpuConfigLen     = 16 // sizeof(struct virtio_gpu_config)
	gpuNumScanouts   = 1
	gpuBytesPerPixel = 4
	gpuDefaultWidth  = 1024
	gpuDefaultHeight = 768
	gpuCursorSize    = 64
	gpuEventDisplay  = 1
	gpuMaxDimension  = 4096
	gpuMaxPixels     = 4096 * 2160
	gpuMaxResources  = 1024
	// Leave room for old and new triple-buffered scanouts during a 4K resize.
	gpuResourceLimit = 256 * 1024 * 1024
	gpuMaxBacking    = gpuMaxPixels * gpuBytesPerPixel / 4096
	gpuMaxRequest    = gpuCtrlHdrLen + 8 + gpuMaxBacking*16

	// Wire-format struct sizes.
	gpuCtrlHdrLen    = 24 // struct virtio_gpu_ctrl_hdr
	gpuRectLen       = 16 // struct virtio_gpu_rect
	gpuDisplayOneLen = 24 // struct virtio_gpu_display_one (rect + enabled + flags)
	gpuMaxScanouts   = 16 // VIRTIO_GPU_MAX_SCANOUTS

	// virtio_gpu_ctrl_hdr.flags.
	gpuFlagFence = 0x1 // VIRTIO_GPU_FLAG_FENCE

	// Control commands (virtio_gpu_ctrl_type, 2D subset).
	gpuCmdGetDisplayInfo        = 0x0100
	gpuCmdResourceCreate2D      = 0x0101
	gpuCmdResourceUnref         = 0x0102
	gpuCmdSetScanout            = 0x0103
	gpuCmdResourceFlush         = 0x0104
	gpuCmdTransferToHost2D      = 0x0105
	gpuCmdResourceAttachBacking = 0x0106
	gpuCmdResourceDetachBacking = 0x0107
	gpuCmdUpdateCursor          = 0x0300
	gpuCmdMoveCursor            = 0x0301

	// Responses.
	gpuRespOKNoData             = 0x1100
	gpuRespOKDisplayInfo        = 0x1101
	gpuRespErrUnspec            = 0x1200
	gpuRespErrOutOfMemory       = 0x1201
	gpuRespErrInvalidScanoutID  = 0x1202
	gpuRespErrInvalidResourceID = 0x1203
	gpuRespErrInvalidParameter  = 0x1205

	// virtio_gpu_formats. Bytes are listed most-significant-first, e.g.
	// B8G8R8A8 stores B at byte 0, A at byte 3.
	gpuFormatB8G8R8A8 = 1
	gpuFormatB8G8R8X8 = 2
	gpuFormatA8R8G8B8 = 3
	gpuFormatX8R8G8B8 = 4
	gpuFormatR8G8B8A8 = 67
	gpuFormatX8B8G8R8 = 68
	gpuFormatA8B8G8R8 = 121
	gpuFormatR8G8B8X8 = 134
)

// ErrNoGPUReq is returned by the queue processors when no request is pending.
var (
	ErrNoGPUReq       = errors.New("no virtio-gpu request")
	ErrGPUDisplaySize = errors.New("GPU display size must be even, 200..4096 per dimension, and at most 4096x2160 pixels")
	errGPUBGRXBounds  = errors.New("virtio-gpu: BGRX scanout exceeds resource pixels")
)

// GPU is a modern PCI device exposing capabilities and a memory BAR.
var _ pci.CapsAndMMIO = (*GPU)(nil)

// gpuMemEntry is one guest-physical region of a resource's backing store
// (struct virtio_gpu_mem_entry).
type gpuMemEntry struct {
	addr   uint64
	length uint32
}

// gpuResource owns transferred 2D pixels and their guest backing regions.
type gpuResource struct {
	width   uint32
	height  uint32
	format  uint32
	data    []byte
	backing []gpuMemEntry
}

type gpuCursor struct {
	image      *image.RGBA
	x, y       int
	hotX, hotY int
}

// GPU is a pure Go modern (virtio 1.0) 2D display device.
type GPU struct {
	*ModernTransport
	mu          sync.Mutex // Device state, worker, config accesses and external mode changes.
	transportMu sync.Mutex // Serializes MMIO/reset with external configuration changes.

	width         uint32
	height        uint32
	display       Display
	telemetry     *telemetry.Collector // Borrowed process-lifetime counters; preserved by reset.
	events        uint32
	resourceBytes uint64
	closed        bool
	closeErr      error

	resources   map[uint32]*gpuResource
	scanout     [gpuNumScanouts]uint32
	scanoutRect image.Rectangle
	frame       *image.RGBA     // Last presented scanout, without the hardware cursor.
	composite   *image.RGBA     // Reusable cursor composition, borrowed only during Flush.
	bgrxSource  image.Rectangle // Viewport last published directly; no GPU-side pixel copy.
	cursor      gpuCursor

	VirtQueue    [gpuNumQueues]*SplitQueue
	LastAvailIdx [gpuNumQueues]uint16

	kick      chan int
	done      chan struct{}
	closeOnce sync.Once

	irq         uint8
	IRQInjector IRQInjector
}

// SetTelemetry configures optional pipeline counters before the worker starts.
// The caller retains ownership of the collector across guest resets.
func (g *GPU) SetTelemetry(collector *telemetry.Collector) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.telemetry = collector
}

func (g *GPU) GetDeviceHeader() pci.DeviceHeader {
	return pci.DeviceHeader{
		// 0x1040 + virtio device id (16 = GPU). The 0x1050 id marks a
		// non-transitional device, so the driver uses the modern
		// interface and requires VIRTIO_F_VERSION_1.
		DeviceID:    0x1050,
		VendorID:    0x1AF4,
		RevisionID:  1,    // Modern virtio PCI revision (required for firmware discovery).
		ClassCode:   0x03, // Display controller
		Subclass:    0x80, // Other display controller; no legacy VGA register interface.
		HeaderType:  0,
		SubsystemID: 0x1050, // Non-transitional subsystem ID.
		// Memory space enable | bus master.
		Command: 0x6,
		// Bit 4: capabilities list present.
		Status:              0x10,
		CapabilitiesPointer: capCommonAt,
		BAR: [6]uint32{
			// BAR0: 32-bit non-prefetchable memory BAR (low nibble 0).
			uint32(GPUMMIOBase),
		},
		InterruptPin:  1,
		InterruptLine: g.irq,
	}
}

// ModernDevice implementation.

// DeviceFeatures advertises no optional GPU features. The transport adds
// VIRTIO_F_VERSION_1 for the modern 2D device.
func (g *GPU) DeviceFeatures() uint64 { return 0 }

func (g *GPU) NumQueues() int { return gpuNumQueues }

// DeviceConfigLen covers struct virtio_gpu_config.
func (g *GPU) DeviceConfigLen() int { return gpuConfigLen }

// MMIO serializes transport reset/configuration with external display changes.
func (g *GPU) MMIO(offset uint64, data []byte, isWrite bool) {
	g.transportMu.Lock()
	defer g.transportMu.Unlock()
	g.ModernTransport.MMIO(offset, data, isWrite)
}

// SetDisplaySize changes the preferred guest mode and notifies its DRM driver.
// The existing scanout remains visible until the guest submits its new frame.
func (g *GPU) SetDisplaySize(width, height int) error {
	if width < 200 || height < 200 || width > gpuMaxDimension || height > gpuMaxDimension ||
		width%2 != 0 || height%2 != 0 || uint64(width)*uint64(height) > gpuMaxPixels {
		return ErrGPUDisplaySize
	}
	g.transportMu.Lock()
	defer g.transportMu.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return io.ErrClosedPipe
	}
	if g.width == uint32(width) && g.height == uint32(height) {
		return nil
	}
	g.width, g.height = uint32(width), uint32(height)
	g.events |= gpuEventDisplay

	return g.ConfigChanged()
}

// ReadDeviceConfig serves struct virtio_gpu_config: events_read,
// events_clear (0), num_scanouts and num_capsets.
func (g *GPU) ReadDeviceConfig(offset uint64, data []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var cfg [gpuConfigLen]byte

	binary.LittleEndian.PutUint32(cfg[:], g.events)
	binary.LittleEndian.PutUint32(cfg[8:], gpuNumScanouts)
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

// WriteDeviceConfig implements the write-one-to-clear events_clear register.
func (g *GPU) WriteDeviceConfig(offset uint64, data []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var clearEvents [4]byte
	for i, value := range data {
		if offset <= 7 && uint64(i) <= 7-offset {
			pos := offset + uint64(i)
			if pos >= 4 {
				clearEvents[pos-4] = value
			}
		}
	}
	g.events &^= binary.LittleEndian.Uint32(clearEvents[:])
}

func (g *GPU) QueueReady(idx int, q *SplitQueue) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed && idx >= 0 && idx < gpuNumQueues {
		g.VirtQueue[idx] = q
		g.LastAvailIdx[idx] = 0
		g.Notify(idx)
	}
}

func (g *GPU) Notify(idx int) {
	// Coalesced kicks remain pending while the worker drains the queues.
	select {
	case g.kick <- idx:
	default:
	}
}

func (g *GPU) IOThreadEntry() {
	log.Println("virtio-gpu: IOThreadEntry started")

	poll := newDevicePoll(16 * time.Millisecond)
	defer poll.timer.Stop()

	for {
		active := true
		select {
		case <-g.done:
			log.Println("virtio-gpu: IOThreadEntry received done signal")

			return
		case <-g.kick:
		case <-poll.interruptWake(g.ModernTransport):
		case <-poll.timer.C:
			active = false
		}
		g.drain()
		poll.reset(active || g.interruptPending())
	}
}

func (g *GPU) drain() {
	for g.ProcessControlQueue() == nil {
	}

	for g.ProcessCursorQueue() == nil {
	}

	g.mu.Lock()
	if !g.closed {
		_ = g.ReinjectIfPending()
	}
	g.mu.Unlock()
}

// ProcessControlQueue services all pending controlq requests, writing a
// response for each. It returns nil when it serviced at least one request,
// or ErrNoGPUReq / ErrVQNotInit otherwise.
func (g *GPU) ProcessControlQueue() error { return g.process(gpuControlQueue, true) }

// ProcessCursorQueue drains the cursorq and composites the final cursor state
// over the scanout. Cursor commands complete with a zero-length used entry.
func (g *GPU) ProcessCursorQueue() error { return g.process(gpuCursorQueue, false) }

func (g *GPU) process(sel int, control bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return io.ErrClosedPipe
	}
	q := g.VirtQueue[sel]
	if q == nil || q.Desc == nil || q.Avail == nil || q.Used == nil {
		return ErrVQNotInit
	}
	queueSize := q.Size
	if LoadU16(&q.Avail.Idx)-g.LastAvailIdx[sel] > queueSize {
		g.LastAvailIdx[sel] = LoadU16(&q.Avail.Idx)

		return ErrNoGPUReq
	}

	if g.LastAvailIdx[sel] == LoadU16(&q.Avail.Idx) {
		return ErrNoGPUReq
	}

	cursorChanged := false
	completed := false
	for processed := uint16(0); processed < queueSize && g.LastAvailIdx[sel] != LoadU16(&q.Avail.Idx); processed++ {
		head := q.Avail.Ring[g.LastAvailIdx[sel]%queueSize]

		var used uint32

		if control {
			req, wr := g.collectChain(q, head)
			resp := g.handleControl(req)
			used = g.writeResponse(wr, resp)
		} else {
			req, _ := g.collectChain(q, head)
			cursorChanged = g.handleCursor(req) || cursorChanged
		}

		g.completeCommand(q, queueSize, head, used)
		completed = true
		g.LastAvailIdx[sel]++
	}
	if cursorChanged {
		g.present()
	}
	if completed {
		return g.Interrupt(sel)
	}

	return ErrNoGPUReq
}

// handleCursor applies struct virtio_gpu_update_cursor. MOVE ignores the
// resource and hotspot fields; UPDATE with resource zero hides the cursor.
func (g *GPU) handleCursor(req []byte) bool {
	if len(req) < gpuCtrlHdrLen+32 {
		return false
	}

	le := binary.LittleEndian
	if le.Uint32(req[gpuCtrlHdrLen:]) >= gpuNumScanouts {
		return false
	}

	switch le.Uint32(req) {
	case gpuCmdUpdateCursor:
		resourceID := le.Uint32(req[gpuCtrlHdrLen+16:])
		if resourceID == 0 {
			g.cursor.image = nil
		} else {
			res := g.resources[resourceID]
			hotX := le.Uint32(req[gpuCtrlHdrLen+20:])
			hotY := le.Uint32(req[gpuCtrlHdrLen+24:])
			if res == nil || res.width != gpuCursorSize || res.height != gpuCursorSize ||
				hotX >= gpuCursorSize || hotY >= gpuCursorSize {
				return false
			}

			// Snapshot the transferred resource: later transfers or unrefs do
			// not alter the cursor until another UPDATE command arrives.
			g.cursor.image = resourceImage(res, true)
			g.cursor.hotX, g.cursor.hotY = int(hotX), int(hotY)
		}
	case gpuCmdMoveCursor:
	default:
		return false
	}

	// Linux sends signed CRTC coordinates in these little-endian fields.
	g.cursor.x = int(int32(le.Uint32(req[gpuCtrlHdrLen+4:])))
	g.cursor.y = int(int32(le.Uint32(req[gpuCtrlHdrLen+8:])))

	return true
}

// writableSeg is a device-writable descriptor segment (response buffer).
type writableSeg struct {
	addr   uint64
	length uint32
}

// collectChain walks the descriptor chain from head, concatenating the
// device-readable descriptors into the request buffer and recording the
// device-writable descriptors for the response.
func (g *GPU) collectChain(q *SplitQueue, head uint16) ([]byte, []writableSeg) {
	var (
		req []byte
		wr  []writableSeg
	)

	descID := head
	var visited [QueueSize]bool
	queueSize := q.Size

	for {
		if descID >= queueSize || visited[descID] {
			return nil, nil
		}
		visited[descID] = true
		desc := q.Desc[descID]
		if desc.Flags & ^uint16(descFWrite|descFNext) != 0 ||
			!guestmem.ValidRange(g.Mem, desc.Addr, uint64(desc.Len)) {
			return nil, nil
		}

		if desc.Flags&descFWrite != 0 {
			wr = append(wr, writableSeg{addr: desc.Addr, length: desc.Len})
		} else {
			if len(wr) != 0 || uint64(desc.Len) > uint64(gpuMaxRequest-len(req)) {
				return nil, nil
			}
			end := desc.Addr + uint64(desc.Len)
			req = append(req, g.Mem[desc.Addr:end]...)
		}

		if desc.Flags&descFNext == 0 {
			break
		}

		descID = desc.Next
	}

	return req, wr
}

// writeResponse spreads resp across the writable segments and returns the
// number of bytes written (for the used-ring length).
func (g *GPU) writeResponse(wr []writableSeg, resp []byte) uint32 {
	written := uint32(0)

	for _, seg := range wr {
		if len(resp) == 0 {
			break
		}

		n := uint64(seg.length)
		if n > uint64(len(resp)) {
			n = uint64(len(resp))
		}

		end := seg.addr + n
		if guestmem.ValidRange(g.Mem, seg.addr, n) {
			copy(g.Mem[seg.addr:end], resp[:n])
		}

		resp = resp[n:]
		written += uint32(n)
	}

	return written
}

// handleControl dispatches one control command and prepares its response.
// Commands complete synchronously and preserve the guest fence ID.
func (g *GPU) handleControl(req []byte) []byte {
	if len(req) < gpuCtrlHdrLen {
		return g.respNoData(gpuRespErrUnspec)
	}

	var resp []byte

	switch binary.LittleEndian.Uint32(req[0:]) {
	case gpuCmdGetDisplayInfo:
		resp = g.cmdGetDisplayInfo()
	case gpuCmdResourceCreate2D:
		resp = g.cmdResourceCreate2D(req)
	case gpuCmdResourceUnref:
		resp = g.cmdResourceUnref(req)
	case gpuCmdSetScanout:
		resp = g.cmdSetScanout(req)
	case gpuCmdResourceFlush:
		resp = g.cmdResourceFlush(req)
	case gpuCmdTransferToHost2D:
		resp = g.cmdTransferToHost2D(req)
	case gpuCmdResourceAttachBacking:
		resp = g.cmdResourceAttachBacking(req)
	case gpuCmdResourceDetachBacking:
		resp = g.cmdResourceDetachBacking(req)
	default:
		log.Printf("virtio-gpu: unhandled control command 0x%x",
			binary.LittleEndian.Uint32(req[0:]))

		resp = g.respNoData(gpuRespErrUnspec)
	}

	g.echoFence(req, resp)

	return resp
}

// echoFence copies the request's fence into the response and sets the fence
// flag when the request asked for fencing.
func (g *GPU) echoFence(req, resp []byte) {
	if len(req) < gpuCtrlHdrLen || len(resp) < gpuCtrlHdrLen {
		return
	}

	if binary.LittleEndian.Uint32(req[4:])&gpuFlagFence == 0 {
		return
	}

	binary.LittleEndian.PutUint32(resp[4:], gpuFlagFence)
	copy(resp[8:24], req[8:24]) // fence_id, ctx_id, ring_idx
}

// respNoData builds a bare ctrl_hdr response carrying respType.
func (g *GPU) respNoData(respType uint32) []byte {
	b := make([]byte, gpuCtrlHdrLen)
	binary.LittleEndian.PutUint32(b[0:], respType)

	return b
}

// cmdGetDisplayInfo reports a single enabled scanout at the device's geometry
// (struct virtio_gpu_resp_display_info).
func (g *GPU) cmdGetDisplayInfo() []byte {
	b := make([]byte, gpuCtrlHdrLen+gpuMaxScanouts*gpuDisplayOneLen)
	le := binary.LittleEndian
	le.PutUint32(b[0:], gpuRespOKDisplayInfo)

	// pmodes[0]: rect {0, 0, width, height}, enabled = 1.
	base := gpuCtrlHdrLen
	le.PutUint32(b[base+8:], g.width)
	le.PutUint32(b[base+12:], g.height)
	le.PutUint32(b[base+16:], 1)

	return b
}

// cmdResourceCreate2D allocates a host resource (struct
// virtio_gpu_resource_create_2d).
func (g *GPU) cmdResourceCreate2D(req []byte) []byte {
	if len(req) < gpuCtrlHdrLen+16 {
		return g.respNoData(gpuRespErrUnspec)
	}

	le := binary.LittleEndian
	id := le.Uint32(req[gpuCtrlHdrLen+0:])
	format := le.Uint32(req[gpuCtrlHdrLen+4:])
	width := le.Uint32(req[gpuCtrlHdrLen+8:])
	height := le.Uint32(req[gpuCtrlHdrLen+12:])

	if id == 0 || g.resources[id] != nil {
		return g.respNoData(gpuRespErrInvalidResourceID)
	}
	pixels := uint64(width) * uint64(height)
	if width == 0 || height == 0 || width > gpuMaxDimension || height > gpuMaxDimension || pixels > gpuMaxPixels {
		return g.respNoData(gpuRespErrInvalidParameter)
	}
	switch format {
	case gpuFormatB8G8R8A8, gpuFormatB8G8R8X8, gpuFormatA8R8G8B8, gpuFormatX8R8G8B8,
		gpuFormatR8G8B8A8, gpuFormatX8B8G8R8, gpuFormatA8B8G8R8, gpuFormatR8G8B8X8:
	default:
		return g.respNoData(gpuRespErrInvalidParameter)
	}
	bytes := pixels * gpuBytesPerPixel
	if bytes > gpuResourceLimit-g.resourceBytes || len(g.resources) >= gpuMaxResources {
		return g.respNoData(gpuRespErrOutOfMemory)
	}
	g.resources[id] = &gpuResource{
		width:  width,
		height: height,
		format: format,
		data:   make([]byte, int(bytes)),
	}
	g.resourceBytes += bytes

	return g.respNoData(gpuRespOKNoData)
}

// cmdResourceUnref destroys a resource (struct virtio_gpu_resource_unref).
func (g *GPU) cmdResourceUnref(req []byte) []byte {
	if len(req) < gpuCtrlHdrLen+8 {
		return g.respNoData(gpuRespErrUnspec)
	}

	id := binary.LittleEndian.Uint32(req[gpuCtrlHdrLen:])
	if res := g.resources[id]; res != nil {
		g.resourceBytes -= uint64(len(res.data))
		delete(g.resources, id)
		if g.scanout[0] == id {
			g.scanout[0] = 0
			g.scanoutRect = image.Rectangle{}
			g.frame, g.composite = nil, nil
			g.bgrxSource = image.Rectangle{}
			g.telemetry.SourceSize(0, 0)
		}
	}

	return g.respNoData(gpuRespOKNoData)
}

// cmdSetScanout binds a resource to a scanout (struct virtio_gpu_set_scanout).
func (g *GPU) cmdSetScanout(req []byte) []byte {
	if len(req) < gpuCtrlHdrLen+gpuRectLen+8 {
		return g.respNoData(gpuRespErrUnspec)
	}

	le := binary.LittleEndian
	base := gpuCtrlHdrLen + gpuRectLen
	scanoutID := le.Uint32(req[base:])
	resourceID := le.Uint32(req[base+4:])

	if scanoutID >= gpuNumScanouts {
		return g.respNoData(gpuRespErrInvalidScanoutID)
	}
	var rect image.Rectangle
	if resourceID != 0 {
		res := g.resources[resourceID]
		if res == nil {
			return g.respNoData(gpuRespErrInvalidResourceID)
		}
		x, y := le.Uint32(req[gpuCtrlHdrLen:]), le.Uint32(req[gpuCtrlHdrLen+4:])
		w, h := le.Uint32(req[gpuCtrlHdrLen+8:]), le.Uint32(req[gpuCtrlHdrLen+12:])
		if w == 0 || h == 0 || x > res.width || y > res.height || w > res.width-x || h > res.height-y {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		if w > gpuMaxDimension || h > gpuMaxDimension || uint64(w)*uint64(h) > gpuMaxPixels {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		rect = image.Rect(int(x), int(y), int(x+w), int(y+h))
	}

	if g.scanout[scanoutID] != resourceID || g.scanoutRect != rect {
		// A different source cannot inherit unchanged pixels from the previous
		// scanout. Its first visible flush publishes the entire new viewport.
		g.frame, g.composite = nil, nil
		g.bgrxSource = image.Rectangle{}
		g.telemetry.ScanoutSwitch()
	}
	g.scanout[scanoutID] = resourceID
	g.scanoutRect = rect
	g.telemetry.SourceSize(rect.Dx(), rect.Dy())

	return g.respNoData(gpuRespOKNoData)
}

// cmdResourceFlush presents a resource to the display (struct
// virtio_gpu_resource_flush).
func (g *GPU) cmdResourceFlush(req []byte) []byte {
	if len(req) < gpuCtrlHdrLen+gpuRectLen+8 {
		return g.respNoData(gpuRespErrUnspec)
	}

	le := binary.LittleEndian
	resourceID := le.Uint32(req[gpuCtrlHdrLen+gpuRectLen:])

	res := g.resources[resourceID]
	if res == nil {
		return g.respNoData(gpuRespErrInvalidResourceID)
	}
	x, y := le.Uint32(req[gpuCtrlHdrLen:]), le.Uint32(req[gpuCtrlHdrLen+4:])
	w, h := le.Uint32(req[gpuCtrlHdrLen+8:]), le.Uint32(req[gpuCtrlHdrLen+12:])
	if x > res.width || y > res.height || w > res.width-x || h > res.height-y {
		return g.respNoData(gpuRespErrInvalidParameter)
	}

	if g.scanout[0] == resourceID {
		if err := g.flushDamage(res, image.Rect(int(x), int(y), int(x+w), int(y+h))); err != nil {
			return g.respNoData(gpuRespErrUnspec)
		}
	}

	return g.respNoData(gpuRespOKNoData)
}

// cmdTransferToHost2D copies guest pixels into a host resource (struct
// virtio_gpu_transfer_to_host_2d).
func (g *GPU) cmdTransferToHost2D(req []byte) []byte {
	if len(req) < gpuCtrlHdrLen+gpuRectLen+16 {
		return g.respNoData(gpuRespErrUnspec)
	}

	le := binary.LittleEndian
	x := le.Uint32(req[gpuCtrlHdrLen+0:])
	y := le.Uint32(req[gpuCtrlHdrLen+4:])
	w := le.Uint32(req[gpuCtrlHdrLen+8:])
	h := le.Uint32(req[gpuCtrlHdrLen+12:])
	offset := le.Uint64(req[gpuCtrlHdrLen+gpuRectLen:])
	resourceID := le.Uint32(req[gpuCtrlHdrLen+gpuRectLen+8:])

	res := g.resources[resourceID]
	if res == nil {
		return g.respNoData(gpuRespErrInvalidResourceID)
	}

	if x > res.width || y > res.height || w > res.width-x || h > res.height-y ||
		offset > uint64(len(res.data)) {
		return g.respNoData(gpuRespErrInvalidParameter)
	}
	started := g.telemetry.Begin()
	copied := g.transferToHost2D(res, x, y, w, h, offset)
	g.telemetry.Transfer(g.telemetry.Elapsed(started), copied)

	return g.respNoData(gpuRespOKNoData)
}

// transferToHost2D fills res.data from the guest backing for the rectangle
// (x, y, w, h) starting at the given backing offset, following the same row
// arithmetic QEMU's virtio-gpu uses. It returns the bytes actually copied.
func (g *GPU) transferToHost2D(res *gpuResource, x, y, w, h uint32, offset uint64) uint64 {
	bpp := uint32(gpuBytesPerPixel)
	stride := res.width * bpp

	// Full-width rows are contiguous in the backing store.
	if offset == 0 && x == 0 && y == 0 && w == res.width {
		total := int(stride) * int(min(h, res.height))
		if total > len(res.data) {
			total = len(res.data)
		}

		return g.backingRead(res, 0, res.data[:total])
	}

	// Rows advance monotonically through the backing. Retain the entry cursor
	// so a partial rectangle does not rescan every preceding guest page per row.
	reader := gpuBackingReader{entries: res.backing}
	var copied uint64
	for row := uint32(0); row < h; row++ {
		src := offset + uint64(stride)*uint64(row)
		dst := int((y+row)*stride + x*bpp)

		if dst >= len(res.data) {
			break
		}

		n := int(w * bpp)
		if dst+n > len(res.data) {
			n = len(res.data) - dst
		}

		copied += reader.read(g.Mem, src, res.data[dst:dst+n])
	}

	return copied
}

// backingRead copies len(dst) bytes from the resource's scatter-gather guest
// backing, starting at logical offset off, into dst. Missing or invalid ranges
// leave dst unchanged and do not contribute to the returned byte count.
func (g *GPU) backingRead(res *gpuResource, off uint64, dst []byte) uint64 {
	reader := gpuBackingReader{entries: res.backing}

	return reader.read(g.Mem, off, dst)
}

// gpuBackingReader retains a scatter-gather cursor for non-overlapping reads
// at increasing logical offsets. It is local to one transfer, so changes to a
// resource's backing cannot leave a stale cursor in subsequent transfers.
type gpuBackingReader struct {
	entries []gpuMemEntry
	pos     uint64 // Logical offset of entries[0].
}

func (r *gpuBackingReader) read(mem []byte, off uint64, dst []byte) uint64 {
	var copied uint64
	for len(r.entries) != 0 && len(dst) != 0 {
		e := r.entries[0]
		entryEnd := r.pos + uint64(e.length)
		if off >= entryEnd {
			r.entries = r.entries[1:]
			r.pos = entryEnd

			continue
		}

		skip := off - r.pos
		n := min(uint64(e.length)-skip, uint64(len(dst)))
		src := e.addr + skip
		if guestmem.ValidRange(mem, src, n) {
			copied += uint64(copy(dst[:n], mem[src:src+n]))
		}
		dst = dst[n:]
		off += n
	}

	return copied
}

// cmdResourceAttachBacking records a resource's guest backing pages (struct
// virtio_gpu_resource_attach_backing followed by virtio_gpu_mem_entry array).
func (g *GPU) cmdResourceAttachBacking(req []byte) []byte {
	if len(req) < gpuCtrlHdrLen+8 {
		return g.respNoData(gpuRespErrUnspec)
	}

	le := binary.LittleEndian
	resourceID := le.Uint32(req[gpuCtrlHdrLen:])
	nr := le.Uint32(req[gpuCtrlHdrLen+4:])

	res := g.resources[resourceID]
	if res == nil {
		return g.respNoData(gpuRespErrInvalidResourceID)
	}
	if nr > gpuMaxBacking || uint64(nr)*16 > uint64(len(req)-(gpuCtrlHdrLen+8)) {
		return g.respNoData(gpuRespErrInvalidParameter)
	}

	entries := make([]gpuMemEntry, 0, nr)
	off := gpuCtrlHdrLen + 8

	for i := uint32(0); i < nr; i++ {
		if off+16 > len(req) {
			break
		}

		addr, length := le.Uint64(req[off:]), le.Uint32(req[off+8:])
		if !guestmem.ValidRange(g.Mem, addr, uint64(length)) {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		entries = append(entries, gpuMemEntry{addr: addr, length: length})
		off += 16
	}

	res.backing = entries

	return g.respNoData(gpuRespOKNoData)
}

// cmdResourceDetachBacking drops a resource's backing (struct
// virtio_gpu_resource_detach_backing).
func (g *GPU) cmdResourceDetachBacking(req []byte) []byte {
	if len(req) < gpuCtrlHdrLen+8 {
		return g.respNoData(gpuRespErrUnspec)
	}

	if res := g.resources[binary.LittleEndian.Uint32(req[gpuCtrlHdrLen:])]; res != nil {
		res.backing = nil
	}

	return g.respNoData(gpuRespOKNoData)
}

// flush presents all pixels, retaining the same path as a full RESOURCE_FLUSH.
func (g *GPU) flush(res *gpuResource) {
	_ = g.flushDamage(res, image.Rect(0, 0, int(res.width), int(res.height)))
}

// flushDamage publishes only flushed pixels. A direct BGRX sink retains the
// published desktop itself; other displays use a persistent private RGBA copy.
// Later transfers outside this region must not become visible yet.
func (g *GPU) flushDamage(res *gpuResource, damage image.Rectangle) error {
	if g.display == nil || res.width == 0 || res.height == 0 {
		return nil
	}
	source := g.scanoutRect
	if source.Empty() {
		source = image.Rect(0, 0, int(res.width), int(res.height))
	}
	damage = damage.Intersect(source)
	if damage.Empty() {
		return nil
	}
	started := g.telemetry.Begin()
	bounds := image.Rect(0, 0, source.Dx(), source.Dy())
	_, separateCursor := g.display.(CursorDisplay)
	if direct, ok := g.display.(BGRXDisplay); ok && separateCursor &&
		res.format == gpuFormatB8G8R8X8 {
		return g.flushBGRX(direct, res, source, damage, started)
	}
	g.bgrxSource = image.Rectangle{}
	frame := g.frame
	fullRefresh := frame == nil || frame.Bounds() != bounds
	if fullRefresh {
		frame = image.NewRGBA(bounds)
		damage = source
	}
	resourceImageRectInto(res, false, frame, damage, source.Min)
	g.frame = frame
	if _, separate := g.display.(CursorDisplay); separate {
		var err error
		if partial, ok := g.display.(DamageDisplay); ok {
			err = partial.FlushDamage(bounds.Dx(), bounds.Dy(), g.frame, []image.Rectangle{damage.Sub(source.Min)})
		} else {
			err = g.display.Flush(bounds.Dx(), bounds.Dy(), g.frame)
		}
		if err != nil {
			log.Printf("virtio-gpu: display flush: %v", err)
		}
		g.telemetry.Flush(g.telemetry.Elapsed(started), uint64(damage.Dx())*uint64(damage.Dy()), fullRefresh)

		return nil
	}
	g.present()
	g.telemetry.Flush(g.telemetry.Elapsed(started), uint64(damage.Dx())*uint64(damage.Dy()), fullRefresh)

	return nil
}

// flushBGRX borrows resource pixels while the sink updates its own publication.
// Pixels outside damage may contain transfers that have not been flushed yet.
func (g *GPU) flushBGRX(
	display BGRXDisplay, res *gpuResource, source, damage image.Rectangle, started time.Time,
) error {
	stride := uint64(res.width) * gpuBytesPerPixel
	start := uint64(source.Min.Y)*stride + uint64(source.Min.X)*gpuBytesPerPixel
	end := start + uint64(source.Dy()-1)*stride + uint64(source.Dx())*gpuBytesPerPixel
	if !source.In(image.Rect(0, 0, int(res.width), int(res.height))) ||
		end > uint64(len(res.data)) || stride > uint64(^uint(0)>>1) {
		return errGPUBGRXBounds
	}
	fullRefresh := g.bgrxSource != source
	if fullRefresh {
		damage = source
	}
	if err := display.FlushBGRX(source.Dx(), source.Dy(), int(stride), res.data[start:end],
		[]image.Rectangle{damage.Sub(source.Min)}); err != nil {
		log.Printf("virtio-gpu: display flush: %v", err)
	} else {
		g.bgrxSource = source
	}
	// A later legacy publication must rebuild its own persistent copy.
	g.frame, g.composite = nil, nil
	g.telemetry.Flush(g.telemetry.Elapsed(started), uint64(damage.Dx())*uint64(damage.Dy()), fullRefresh)

	return nil
}

// present publishes only cursor state to capable displays. Legacy displays
// receive the last flushed desktop with the cursor composited on top.
func (g *GPU) present() {
	if g.display == nil {
		return
	}
	cursor := DisplayCursor{Image: g.cursor.image, X: g.cursor.x, Y: g.cursor.y, HotX: g.cursor.hotX, HotY: g.cursor.hotY}
	if separate, ok := g.display.(CursorDisplay); ok {
		if err := separate.SetCursor(cursor); err != nil {
			log.Printf("virtio-gpu: cursor update: %v", err)
		}

		return
	}
	if g.frame == nil || g.scanout[0] == 0 {
		return
	}
	img := compositeCursor(g.frame, &g.composite, cursor)
	if err := g.display.Flush(img.Bounds().Dx(), img.Bounds().Dy(), img); err != nil {
		log.Printf("virtio-gpu: display flush: %v", err)
	}
}

func resourceImage(res *gpuResource, cursor bool) *image.RGBA {
	return resourceImageInto(res, cursor, nil)
}

// resourceImageInto reuses private image storage. Display implementations must
// finish reading this storage before returning. Cursor shapes own a snapshot.
func resourceImageInto(res *gpuResource, cursor bool, img *image.RGBA) *image.RGBA {
	bounds := image.Rect(0, 0, int(res.width), int(res.height))
	if img == nil || img.Bounds() != bounds {
		img = image.NewRGBA(bounds)
	}
	resourceImageRectInto(res, cursor, img, bounds, image.Point{})

	return img
}

// resourceImageRectInto converts a resource rectangle into the destination at
// rect.Min-origin, respecting both the source stride and scanout viewport.
func resourceImageRectInto(res *gpuResource, cursor bool, img *image.RGBA, rect image.Rectangle, origin image.Point) {
	rOff, gOff, bOff, aOff, hasAlpha := formatOffsets(res.format)
	if cursor && !hasAlpha {
		// Linux uses dumb XRGB buffers even for ARGB cursor images.
		aOff = 6 - rOff - gOff - bOff
		hasAlpha = true
	}
	rect = rect.Intersect(image.Rect(0, 0, int(res.width), int(res.height))).Intersect(img.Bounds().Add(origin))
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		si := (y*int(res.width) + rect.Min.X) * gpuBytesPerPixel
		di := img.PixOffset(rect.Min.X-origin.X, y-origin.Y)
		pixels := min(rect.Dx()*gpuBytesPerPixel, len(res.data)-si) &^ 3
		if pixels <= 0 {
			return
		}
		src, dst := res.data[si:si+pixels], img.Pix[di:di+pixels]
		// Linux scanouts normally use BGRX. Convert whole rows with the
		// assembly channel-swap kernel when available, retaining alpha semantics.
		if rOff == 2 && gOff == 1 && bOff == 0 {
			alpha := uint32(0)
			if !hasAlpha {
				alpha = 0xff000000
			}
			bgrToRGBARow(dst, src, alpha)

			continue
		}
		for i := 0; i < pixels; i += 4 {
			dst[i], dst[i+1], dst[i+2] = src[i+rOff], src[i+gOff], src[i+bOff]
			if hasAlpha {
				dst[i+3] = src[i+aOff]
			} else {
				dst[i+3] = 0xff
			}
		}
	}
}

// formatOffsets returns the byte position of each channel within a 4-byte
// pixel for a virtio_gpu_formats value. Unknown formats are treated as BGRX.
func formatOffsets(format uint32) (rOff, gOff, bOff, aOff int, hasAlpha bool) {
	switch format {
	case gpuFormatB8G8R8A8:
		return 2, 1, 0, 3, true
	case gpuFormatB8G8R8X8:
		return 2, 1, 0, 0, false
	case gpuFormatA8R8G8B8:
		return 1, 2, 3, 0, true
	case gpuFormatX8R8G8B8:
		return 1, 2, 3, 0, false
	case gpuFormatR8G8B8A8:
		return 0, 1, 2, 3, true
	case gpuFormatX8B8G8R8:
		return 3, 2, 1, 0, false
	case gpuFormatA8B8G8R8:
		return 3, 2, 1, 0, true
	case gpuFormatR8G8B8X8:
		return 0, 1, 2, 0, false
	default:
		return 2, 1, 0, 0, false
	}
}

// Read and Write satisfy pci.Device. A modern device has no IO-port BAR, so
// these are no-ops.
func (g *GPU) Read(port uint64, bytes []byte) error { return nil }

func (g *GPU) Write(port uint64, bytes []byte) error { return nil }

func (g *GPU) IOPort() uint64 { return 0 }

func (g *GPU) Size() uint64 { return 0 }

// Reset drops guest-owned queues and resources, preserving the current host
// display preference so a reprobed guest discovers the latest requested mode.
func (g *GPU) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	hadCursor := g.cursor.image != nil
	g.resetLocked()
	if g.closed {
		return
	}
	if hadCursor {
		// Separate cursor displays retain their last shape across desktop
		// flushes. Clear it while publication is still serialized by mu.
		g.present()
	}
	if display, ok := g.display.(GraphicsResetDisplay); ok {
		display.GraphicsReset()
	}
}

func (g *GPU) resetLocked() {
	g.VirtQueue = [gpuNumQueues]*SplitQueue{}
	g.LastAvailIdx = [gpuNumQueues]uint16{}
	g.resources = map[uint32]*gpuResource{}
	g.resourceBytes = 0
	g.scanout = [gpuNumScanouts]uint32{}
	g.scanoutRect = image.Rectangle{}
	g.telemetry.SourceSize(0, 0)
	g.frame, g.composite = nil, nil
	g.bgrxSource = image.Rectangle{}
	g.cursor = gpuCursor{}
	g.events = 0
}

func (g *GPU) Close() error {
	g.closeOnce.Do(func() {
		g.mu.Lock()
		g.closed = true
		g.resetLocked()
		close(g.done)
		g.mu.Unlock()
		// Display shutdown can wait for an RDP writer which is requesting a
		// mode change. Never hold the GPU mutex while waiting for that writer.
		if g.display != nil {
			g.closeErr = g.display.Close()
		}
	})

	return g.closeErr
}

func NewGPU(irq uint8, irqInjector IRQInjector, mem []byte, display Display) *GPU {
	g := &GPU{
		width:       gpuDefaultWidth,
		height:      gpuDefaultHeight,
		display:     display,
		resources:   map[uint32]*gpuResource{},
		kick:        make(chan int, 1), // drain services both queues per wakeup.
		done:        make(chan struct{}),
		irq:         irq,
		IRQInjector: irqInjector,
	}

	g.ModernTransport = NewModernTransport(g, mem, func() error {
		if irqInjector == nil {
			return nil
		}

		return irqInjector.InjectVirtioGPUIRQ()
	})

	return g
}

func (g *GPU) completeCommand(q *SplitQueue, size, head uint16, length uint32) {
	index := LoadU16(&q.Used.Idx)
	q.Used.Ring[index%size].ID = uint32(head)
	q.Used.Ring[index%size].Len = length
	StoreAddU16(&q.Used.Idx, 1)
}
