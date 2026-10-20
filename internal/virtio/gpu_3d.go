package virtio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"log"

	"github.com/define42/DevBox-Gateway-kvm/internal/virgl"
)

const (
	gpuFeatureVirgl             = uint64(1)
	gpuCmdGetCapsetInfo         = 0x0108
	gpuCmdGetCapset             = 0x0109
	gpuCmdContextCreate         = 0x0200
	gpuCmdContextDestroy        = 0x0201
	gpuCmdContextAttachResource = 0x0202
	gpuCmdContextDetachResource = 0x0203
	gpuCmdResourceCreate3D      = 0x0204
	gpuCmdTransferToHost3D      = 0x0205
	gpuCmdTransferFromHost3D    = 0x0206
	gpuCmdSubmit3D              = 0x0207
	gpuRespOKCapsetInfo         = 0x1102
	gpuRespOKCapset             = 0x1103
	gpuRespErrInvalidContextID  = 0x1204

	// sRGB texture wire values from virglrenderer src/virgl_hw.h.
	gpuFormatA8B8G8R8SRGB = 98
	gpuFormatX8B8G8R8SRGB = 99
	gpuFormatB8G8R8A8SRGB = 100
	gpuFormatB8G8R8X8SRGB = 101
	gpuFormatA8R8G8B8SRGB = 102
	gpuFormatX8R8G8B8SRGB = 103
	gpuFormatR8G8B8A8SRGB = 104
	gpuFormatR8G8B8X8SRGB = 230

	// Bound command copies, pinned guest backing, context bookkeeping and
	// conservative native allocations independently of the 2D pixel budget.
	gpuMax3DRequest = (16 << 20) + gpuCtrlHdrLen + 8
	gpuMax3DBacking = 131072
	gpuMaxContexts  = 256
	// GNOME wallpaper transitions overlap full-size textures, mip chains and
	// native upload buffers. Ubuntu's bundled 10000x5622 wallpaper exceeds
	// one GiB of native resources during a transition. Guest backing remains
	// independently bounded; increasing native capacity must not raise it.
	gpu3DResourceLimit = uint64(2 << 30)
	gpu3DBackingLimit  = uint64(512 << 20)
)

var (
	errGPU3DNoCapabilities  = errors.New("virgl renderer exposed no capabilities")
	errGPU3DBackingAttached = errors.New("GPU resource already has backing")
	errGPU3DBackingVectors  = errors.New("GPU backing vector budget exceeded")
	errGPU3DBackingSize     = errors.New("GPU backing exceeds limit")
	errGPU3DBackingEmpty    = errors.New("empty GPU backing")
)

type (
	gpu3DResourceArgs = virgl.ResourceArgs
	gpuCapset         = virgl.Capset
)

// gpuRenderer owns native resources and serializes all GL operations, including
// reset and cleanup, onto its GL thread. Slices passed to AttachBacking remain
// attached until DetachBacking, UnrefResource, Reset or Close returns.
//
//nolint:interfacebloat // A single adapter owns the complete native GPU protocol and its resource lifecycle.
type gpuRenderer interface {
	Capsets() []virgl.Capset
	Capset(id, version uint32) ([]byte, error)
	CreateContext(id uint32, name string) error
	DestroyContext(id uint32) error
	CreateResource(virgl.ResourceArgs) error
	UnrefResource(id uint32) error
	AttachBacking(id uint32, backing [][]byte) error
	DetachBacking(id uint32) error
	ContextAttachResource(ctx, resource uint32) error
	ContextDetachResource(ctx, resource uint32) error
	Transfer(resource, ctx, level, stride, layerStride uint32, box virgl.Box, offset uint64, toHost bool) error
	Submit(ctx uint32, data []byte) error
	ReadPixels(resource, x, y, width, height uint32) (*image.RGBA, error)
	Fence(id uint32) error
	Poll() []uint32
	Reset() error
	Close() error
}

// NewGPUWith3D creates a virgl GPU. Explicitly requested acceleration fails
// with an actionable error when this build or host cannot initialize it.
// NewGPU remains the independent, pure-Go 2D constructor.
func NewGPUWith3D(irq uint8, injector IRQInjector, mem []byte, display Display) (*GPU, error) {
	renderer, err := virgl.New()
	if err != nil {
		return nil, fmt.Errorf("enable VM 3D graphics (set VM_GPU_3D=false to disable): %w", err)
	}
	if len(renderer.Capsets()) == 0 {
		return nil, errors.Join(errGPU3DNoCapabilities, renderer.Close())
	}

	return newGPUWithRenderer(irq, injector, mem, display, renderer), nil
}

//nolint:gocyclo // Keep wire-command validation next to each protocol operation in this dispatcher.
func (g *GPU) handle3D(req []byte) []byte {
	if g.renderer == nil {
		return g.respNoData(gpuRespErrUnspec)
	}
	le := binary.LittleEndian
	command, ctx := le.Uint32(req), le.Uint32(req[16:])
	// Multiple timelines require CONTEXT_INIT, which this device does not offer.
	if le.Uint32(req[4:]) & ^uint32(gpuFlagFence) != 0 {
		return g.respNoData(gpuRespErrInvalidParameter)
	}
	switch command {
	case gpuCmdGetCapsetInfo:
		if len(req) < gpuCtrlHdrLen+8 {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		index := le.Uint32(req[gpuCtrlHdrLen:])
		if uint64(index) >= uint64(len(g.capsets)) {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		caps := g.capsets[index]
		resp := make([]byte, gpuCtrlHdrLen+16)
		le.PutUint32(resp, gpuRespOKCapsetInfo)
		le.PutUint32(resp[gpuCtrlHdrLen:], caps.ID)
		le.PutUint32(resp[gpuCtrlHdrLen+4:], caps.MaxVersion)
		le.PutUint32(resp[gpuCtrlHdrLen+8:], caps.MaxSize)

		return resp
	case gpuCmdGetCapset:
		if len(req) < gpuCtrlHdrLen+8 {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		id, version := le.Uint32(req[gpuCtrlHdrLen:]), le.Uint32(req[gpuCtrlHdrLen+4:])
		for _, caps := range g.capsets {
			if caps.ID != id || version > caps.MaxVersion {
				continue
			}
			data, err := g.renderer.Capset(id, version)
			if err != nil || uint64(len(data)) > uint64(caps.MaxSize) {
				return g.respNoData(gpuRespErrUnspec)
			}

			return append(g.respNoData(gpuRespOKCapset), data...)
		}

		return g.respNoData(gpuRespErrInvalidParameter)
	case gpuCmdContextCreate:
		if len(req) < gpuCtrlHdrLen+72 {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		if ctx == 0 || g.contexts[ctx] != nil {
			return g.respNoData(gpuRespErrInvalidContextID)
		}
		if len(g.contexts) >= gpuMaxContexts {
			return g.respNoData(gpuRespErrOutOfMemory)
		}
		n := le.Uint32(req[gpuCtrlHdrLen:])
		if n > 64 || le.Uint32(req[gpuCtrlHdrLen+4:]) != 0 {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		if err := g.renderer.CreateContext(ctx, string(req[gpuCtrlHdrLen+8:gpuCtrlHdrLen+8+int(n)])); err != nil {
			return g.respNoData(gpuRespErrUnspec)
		}
		g.contexts[ctx] = make(map[uint32]struct{})
	case gpuCmdContextDestroy:
		if ctx == 0 || g.contexts[ctx] == nil {
			return g.respNoData(gpuRespErrInvalidContextID)
		}
		if err := g.renderer.DestroyContext(ctx); err != nil {
			return g.respNoData(gpuRespErrUnspec)
		}
		delete(g.contexts, ctx)
	case gpuCmdContextAttachResource, gpuCmdContextDetachResource:
		if len(req) < gpuCtrlHdrLen+8 {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		if ctx == 0 || g.contexts[ctx] == nil {
			return g.respNoData(gpuRespErrInvalidContextID)
		}
		id := le.Uint32(req[gpuCtrlHdrLen:])
		res := g.resources[id]
		if res == nil || res.handle == 0 {
			return g.respNoData(gpuRespErrInvalidResourceID)
		}
		if command == gpuCmdContextAttachResource {
			if err := g.renderer.ContextAttachResource(ctx, id); err != nil {
				return g.respNoData(gpuRespErrUnspec)
			}
			g.contexts[ctx][id] = struct{}{}
		} else {
			if err := g.renderer.ContextDetachResource(ctx, id); err != nil {
				return g.respNoData(gpuRespErrUnspec)
			}
			delete(g.contexts[ctx], id)
		}
	case gpuCmdResourceCreate3D:
		if len(req) < gpuCtrlHdrLen+48 {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		p := req[gpuCtrlHdrLen:]
		args := virgl.ResourceArgs{
			Handle: le.Uint32(p), Target: le.Uint32(p[4:]), Format: le.Uint32(p[8:]), Bind: le.Uint32(p[12:]),
			Width: le.Uint32(p[16:]), Height: le.Uint32(p[20:]), Depth: le.Uint32(p[24:]), ArraySize: le.Uint32(p[28:]),
			LastLevel: le.Uint32(p[32:]), NrSamples: le.Uint32(p[36:]), Flags: le.Uint32(p[40:]),
		}

		return g.createNativeResource(args)
	case gpuCmdTransferToHost3D, gpuCmdTransferFromHost3D:
		return g.transferNative3D(req, command == gpuCmdTransferToHost3D)
	case gpuCmdSubmit3D:
		if ctx == 0 || g.contexts[ctx] == nil {
			return g.respNoData(gpuRespErrInvalidContextID)
		}
		if len(req) < gpuCtrlHdrLen+8 {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		n := le.Uint32(req[gpuCtrlHdrLen:])
		if n == 0 || n%4 != 0 || uint64(n) > uint64(len(req)-gpuCtrlHdrLen-8) || n > gpuMax3DRequest-gpuCtrlHdrLen-8 {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
		if err := g.renderer.Submit(ctx, req[gpuCtrlHdrLen+8:gpuCtrlHdrLen+8+int(n)]); err != nil {
			return g.respNoData(gpuRespErrInvalidParameter)
		}
	default:
		return g.respNoData(gpuRespErrUnspec)
	}

	return g.respNoData(gpuRespOKNoData)
}

func (g *GPU) createNative2D(id, format, width, height uint32) []byte {
	return g.createNativeResource(virgl.ResourceArgs{
		Handle: id, Target: 2, Format: format, Bind: 1 << 1,
		Width: width, Height: height, Depth: 1, ArraySize: 1, Flags: 1,
	})
}

func (g *GPU) createNativeResource(args virgl.ResourceArgs) []byte {
	if args.Handle == 0 || g.resources[args.Handle] != nil {
		return g.respNoData(gpuRespErrInvalidResourceID)
	}
	charge, valid := nativeResourceCharge(args)
	if !valid {
		return g.respNoData(gpuRespErrInvalidParameter)
	}
	if charge > gpu3DResourceLimit-g.resource3DBytes || len(g.resources) >= gpuMaxResources {
		return g.respNoData(gpuRespErrOutOfMemory)
	}
	if err := g.renderer.CreateResource(args); err != nil {
		return g.respNoData(gpuRespErrInvalidParameter)
	}
	g.resources[args.Handle] = &gpuResource{
		width: args.Width, height: args.Height, format: args.Format,
		handle: args.Handle, charge: charge, args: args,
	}
	g.resource3DBytes += charge

	return g.respNoData(gpuRespOKNoData)
}

// Native buffers use byte widths, unlike textures. Texture charges account for
// their format, all mip levels and samples. Unknown formats retain the largest
// supported uncompressed texel charge; the renderer validates format/target pairs.
func nativeResourceCharge(a virgl.ResourceArgs) (uint64, bool) {
	if a.Width == 0 || a.Height == 0 || a.Depth == 0 ||
		a.Target > 8 || a.LastLevel > 14 || a.Flags & ^uint32(1) != 0 ||
		a.NrSamples > 16 || (a.NrSamples != 0 && a.NrSamples&(a.NrSamples-1) != 0) {
		return 0, false
	}
	if a.Target == 0 {
		return uint64(a.Width), a.Height == 1 && a.Depth == 1 && a.ArraySize <= 1 && a.LastLevel == 0 && a.NrSamples == 0
	}
	if a.Width > 16384 || a.Height > 16384 || a.Depth > 2048 || a.ArraySize > 2048 ||
		(a.Target >= 6 && a.ArraySize == 0) {
		return 0, false
	}
	var total uint64
	for level := uint32(0); level <= a.LastLevel; level++ {
		values := []uint64{
			uint64(max(a.Width>>level, 1)), uint64(max(a.Height>>level, 1)),
			uint64(max(a.Depth>>level, 1)), uint64(max(a.ArraySize, 1)), uint64(max(a.NrSamples, 1)),
			nativeResourceTexelBytes(a.Format),
		}
		size := uint64(1)
		for _, value := range values {
			if value > gpu3DResourceLimit/size {
				return gpu3DResourceLimit + 1, true
			}
			size *= value
		}
		if size > gpu3DResourceLimit-total {
			return gpu3DResourceLimit + 1, true
		}
		total += size
	}

	return total, true
}

func nativeResourceTexelBytes(format uint32) uint64 {
	switch format {
	case gpuFormatB8G8R8A8, gpuFormatB8G8R8X8, gpuFormatA8R8G8B8, gpuFormatX8R8G8B8,
		gpuFormatR8G8B8A8, gpuFormatX8B8G8R8, gpuFormatA8B8G8R8, gpuFormatR8G8B8X8,
		gpuFormatA8B8G8R8SRGB, gpuFormatX8B8G8R8SRGB, gpuFormatB8G8R8A8SRGB, gpuFormatB8G8R8X8SRGB,
		gpuFormatA8R8G8B8SRGB, gpuFormatX8R8G8B8SRGB, gpuFormatR8G8B8A8SRGB, gpuFormatR8G8B8X8SRGB:
		return 4
	default:
		// Preserve a conservative RGBA32 charge for other formats, including
		// high precision textures and native formats added in future versions.
		return 16
	}
}

func (g *GPU) attachNativeBacking(res *gpuResource, entries []gpuMemEntry) error {
	if len(res.backing) != 0 {
		return errGPU3DBackingAttached
	}
	if uint64(len(entries)) > gpuMax3DBacking-g.backing3DEntries {
		return errGPU3DBackingVectors
	}
	backing := make([][]byte, 0, len(entries))
	var total uint64
	for _, entry := range entries {
		if uint64(entry.length) > gpu3DBackingLimit-g.backing3DBytes-total {
			return errGPU3DBackingSize
		}
		total += uint64(entry.length)
		if entry.length != 0 {
			backing = append(backing, g.Mem[entry.addr:entry.addr+uint64(entry.length)])
		}
	}
	if len(backing) == 0 {
		return errGPU3DBackingEmpty
	}
	if err := g.renderer.AttachBacking(res.handle, backing); err != nil {
		return err
	}
	g.backing3DBytes += total
	g.backing3DEntries += uint64(len(entries))

	return nil
}

func (g *GPU) releaseNativeBacking(res *gpuResource) {
	for _, entry := range res.backing {
		g.backing3DBytes -= uint64(entry.length)
	}
	g.backing3DEntries -= uint64(len(res.backing))
	res.backing = nil
}

func (g *GPU) transferNative2D(res *gpuResource, x, y, w, h uint32, offset uint64) []byte {
	if x > res.width || y > res.height || w > res.width-x || h > res.height-y {
		return g.respNoData(gpuRespErrInvalidParameter)
	}
	box := virgl.Box{X: x, Y: y, W: w, H: h, D: 1}
	if err := g.renderer.Transfer(res.handle, 0, 0, 0, 0, box, offset, true); err != nil {
		return g.respNoData(gpuRespErrInvalidParameter)
	}
	// DRM uses XRGB dumb buffers for premultiplied ARGB cursors. Keep the
	// uploaded X-channel alpha across native draws; cursorImage reads current
	// RGB from GL rather than relying on the potentially stale shadow colors.
	_, cursorAlpha := nativeCursorAlphaOffset(res.format)
	if cursorAlpha && res.width == gpuCursorSize && res.height == gpuCursorSize {
		if res.data == nil {
			res.data = make([]byte, gpuCursorSize*gpuCursorSize*gpuBytesPerPixel)
			g.resourceBytes += uint64(len(res.data))
		}
		g.transferToHost2D(res, x, y, w, h, offset)
	}

	return g.respNoData(gpuRespOKNoData)
}

func (g *GPU) transferNative3D(req []byte, toHost bool) []byte {
	if len(req) < gpuCtrlHdrLen+48 {
		return g.respNoData(gpuRespErrInvalidParameter)
	}
	le := binary.LittleEndian
	ctx := le.Uint32(req[16:])
	if ctx != 0 && g.contexts[ctx] == nil {
		return g.respNoData(gpuRespErrInvalidContextID)
	}
	p := req[gpuCtrlHdrLen:]
	id, level := le.Uint32(p[32:]), le.Uint32(p[36:])
	res := g.resources[id]
	if res == nil || res.handle == 0 {
		return g.respNoData(gpuRespErrInvalidResourceID)
	}
	if level > res.args.LastLevel {
		return g.respNoData(gpuRespErrInvalidParameter)
	}
	box := virgl.Box{
		X: le.Uint32(p), Y: le.Uint32(p[4:]), Z: le.Uint32(p[8:]),
		W: le.Uint32(p[12:]), H: le.Uint32(p[16:]), D: le.Uint32(p[20:]),
	}
	// Layer and compressed-format layout validation is delegated to virgl.
	err := g.renderer.Transfer(id, ctx, level, le.Uint32(p[40:]), le.Uint32(p[44:]), box, le.Uint64(p[24:]), toHost)
	if err != nil {
		return g.respNoData(gpuRespErrInvalidParameter)
	}
	if toHost && res.data != nil {
		g.resourceBytes -= uint64(len(res.data))
		res.data = nil
	}

	return g.respNoData(gpuRespOKNoData)
}

type gpuPendingFence struct {
	id              uint32
	queue           *SplitQueue
	queueSize, head uint16
	writable        []writableSeg
	response        []byte
}

func (g *GPU) completeCommand(q *SplitQueue, size, head uint16, length uint32) {
	index := LoadU16(&q.Used.Idx)
	q.Used.Ring[index%size].ID = uint32(head)
	q.Used.Ring[index%size].Len = length
	StoreAddU16(&q.Used.Idx, 1)
}

// deferFence uses private monotonically increasing tokens; guest fence IDs
// are 64-bit, can arrive out of order, and must never be truncated to virgl's
// 32-bit callback IDs. No descriptor or response is published before completion.
func (g *GPU) deferFence(q *SplitQueue, size, head uint16, req []byte, wr []writableSeg, resp []byte) bool {
	if g.renderer == nil || len(req) < gpuCtrlHdrLen || binary.LittleEndian.Uint32(req[4:])&gpuFlagFence == 0 ||
		binary.LittleEndian.Uint32(resp) >= gpuRespErrUnspec {
		return false
	}
	g.nextFence++
	if err := g.renderer.Fence(g.nextFence); err != nil {
		binary.LittleEndian.PutUint32(resp, gpuRespErrUnspec)

		return false
	}
	g.fences = append(g.fences, gpuPendingFence{g.nextFence, q, size, head, wr, resp})

	return true
}

func (g *GPU) pollFences() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.renderer == nil || len(g.fences) == 0 {
		return
	}
	completed := false
	for _, id := range g.renderer.Poll() {
		// The legacy renderer callback may coalesce completed fences. The
		// outstanding queue is much smaller than half the token space, so
		// signed subtraction also handles the uint32 wrap correctly.
		for len(g.fences) != 0 && int32(id-g.fences[0].id) >= 0 {
			pending := g.fences[0]
			g.fences[0] = gpuPendingFence{}
			g.fences = g.fences[1:]
			if g.VirtQueue[gpuControlQueue] != pending.queue {
				continue
			}
			used := g.writeResponse(pending.writable, pending.response)
			g.completeCommand(pending.queue, pending.queueSize, pending.head, used)
			completed = true
		}
	}
	if completed {
		if err := g.Interrupt(); err != nil {
			log.Printf("virtio-gpu: fence interrupt: %v", err)
		}
	}
}

// Keep fence polling at the active interval until the host GPU completes work.
func (g *GPU) fencesPending() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return len(g.fences) != 0
}
