//go:build linux && cgo && virgl

package virgl

/*
#cgo pkg-config: virglrenderer
#include <virglrenderer.h>
#include <stdlib.h>
#include <stdint.h>
#include <sys/uio.h>

// virglrenderer.h intentionally forward-declares this stable wire-layout type.
struct virgl_box { uint32_t x, y, z, w, h, d; };

struct devbox_virgl_state { uint32_t fence; int completed; };

static void devbox_write_fence(void *cookie, uint32_t fence) {
    struct devbox_virgl_state *state = cookie;
    state->fence = fence;
    state->completed = 1;
}

static struct virgl_renderer_callbacks devbox_callbacks = {
    .version = 1,
    .write_fence = devbox_write_fence,
};

static int devbox_init(struct devbox_virgl_state *state, int surfaceless) {
    int flags = VIRGL_RENDERER_USE_EGL;
    if (surfaceless) flags |= VIRGL_RENDERER_USE_SURFACELESS;
    // No THREAD_SYNC or ASYNC_FENCE_CB: callbacks stay on our renderer thread.

    return virgl_renderer_init(state, flags, &devbox_callbacks);
}

static void devbox_set_iov(struct iovec *iov, int index, void *data, size_t length) {
    iov[index].iov_base = data;
    iov[index].iov_len = length;
}

static int devbox_read_pixels(uint32_t resource, struct virgl_box *box, void *pixels, size_t size) {
    struct iovec iov = { .iov_base = pixels, .iov_len = size };

    return virgl_renderer_transfer_read_iov(resource, 0, 0, box->w * 4, 0, box, 0, &iov, 1);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"image"
	"math"
	"runtime"
	"slices"
	"unsafe"
)

var (
	errNativeOperation   = errors.New("native virgl operation failed")
	errAllocateCallbacks = errors.New("allocate virgl callback state")
	errInitializeEGL     = errors.New(
		"initialize headless virgl EGL renderer: check host graphics drivers and render-node access")
	errInvalidCapset     = errors.New("unsupported virgl capability set or version")
	errInvalidContext    = errors.New("invalid virgl context ID or name")
	errDuplicateContext  = errors.New("virgl context already exists")
	errUnknownContext    = errors.New("unknown virgl context")
	errInvalidResource   = errors.New("invalid virgl resource ID")
	errDuplicateResource = errors.New("virgl resource already exists")
	errUnknownResource   = errors.New("unknown virgl resource")
	errBackingAttached   = errors.New("virgl resource already has backing")
	errBackingCount      = errors.New("invalid virgl backing entry count")
	errAllocateBacking   = errors.New("allocate virgl backing vectors")
	errEmptyBacking      = errors.New("empty virgl backing entry")
	errTransferBounds    = errors.New("virgl transfer bounds exceed signed range")
	errCommandLength     = errors.New("invalid virgl command buffer length")
	errReadbackBounds    = errors.New("invalid virgl readback rectangle")
	errScanoutFormat     = errors.New("unsupported scanout format")
)

// Available reports compile-time support; New also checks host EGL availability.
func Available() bool { return true }

// New initializes headless OpenGL, preferring a DRM render node and falling back
// to surfaceless EGL where supported by the host driver.
func New() (*Renderer, error) { return newRenderer(newNativeBackend) }

type nativeBacking struct {
	iov     *C.struct_iovec
	buffers [][]byte
	pins    runtime.Pinner
}

type nativeBackend struct {
	state     *C.struct_devbox_virgl_state
	capsets   []Capset
	resources map[uint32]ResourceArgs
	contexts  map[uint32]struct{}
	backing   map[uint32]*nativeBacking
}

func nativeError(operation string, code C.int) error {
	if code == 0 {
		return nil
	}

	return fmt.Errorf("%w: %s (code %d)", errNativeOperation, operation, int(code))
}

func newNativeBackend() (backend, error) {
	state := (*C.struct_devbox_virgl_state)(C.calloc(1, C.size_t(C.sizeof_struct_devbox_virgl_state)))
	if state == nil {
		return nil, errAllocateCallbacks
	}
	code := C.devbox_init(state, 0)
	if code != 0 {
		C.virgl_renderer_cleanup(unsafe.Pointer(state))
		code = C.devbox_init(state, 1)
	}
	if code != 0 {
		C.virgl_renderer_cleanup(unsafe.Pointer(state))
		C.free(unsafe.Pointer(state))

		return nil, fmt.Errorf("%w (code %d)", errInitializeEGL, int(code))
	}
	b := &nativeBackend{
		state: state, resources: make(map[uint32]ResourceArgs),
		contexts: make(map[uint32]struct{}), backing: make(map[uint32]*nativeBacking),
	}
	// IDs 1 and 2 are VIRGL and VIRGL2. Do not expose Venus/DRM capsets.
	for _, id := range []uint32{1, 2} {
		var version, size C.uint32_t
		C.virgl_renderer_get_cap_set(C.uint32_t(id), &version, &size)
		if size > 0 && size <= 1<<20 {
			b.capsets = append(b.capsets, Capset{id, uint32(version), uint32(size)})
		}
	}

	return b, nil
}

func (b *nativeBackend) Capsets() []Capset { return slices.Clone(b.capsets) }
func (b *nativeBackend) Capset(id, version uint32) ([]byte, error) {
	for _, capset := range b.capsets {
		if capset.ID == id && version <= capset.MaxVersion {
			data := make([]byte, capset.MaxSize)
			C.virgl_renderer_fill_caps(C.uint32_t(id), C.uint32_t(version), unsafe.Pointer(&data[0]))

			return data, nil
		}
	}

	return nil, errInvalidCapset
}

//nolint:nlreturn // cgo-generated pointer-check closures contain returns outside the Go source.
func (b *nativeBackend) CreateContext(id uint32, name string) error {
	if id == 0 || len(name) > 64 {
		return errInvalidContext
	}
	if _, exists := b.contexts[id]; exists {
		return errDuplicateContext
	}
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	code := C.virgl_renderer_context_create(C.uint32_t(id), C.uint32_t(len(name)), cname)
	if err := nativeError("create context", code); err != nil {
		return err
	}
	b.contexts[id] = struct{}{}

	return nil
}

func (b *nativeBackend) DestroyContext(id uint32) error {
	if _, exists := b.contexts[id]; !exists {
		return errUnknownContext
	}
	C.virgl_renderer_context_destroy(C.uint32_t(id))
	delete(b.contexts, id)

	return nil
}

func (b *nativeBackend) CreateResource(args ResourceArgs) error {
	if args.Handle == 0 {
		return errInvalidResource
	}
	if _, exists := b.resources[args.Handle]; exists {
		return errDuplicateResource
	}
	cargs := C.struct_virgl_renderer_resource_create_args{
		handle: C.uint32_t(args.Handle), target: C.uint32_t(args.Target),
		format: C.uint32_t(args.Format), bind: C.uint32_t(args.Bind),
		width: C.uint32_t(args.Width), height: C.uint32_t(args.Height),
		depth: C.uint32_t(args.Depth), array_size: C.uint32_t(args.ArraySize),
		last_level: C.uint32_t(args.LastLevel), nr_samples: C.uint32_t(args.NrSamples), flags: C.uint32_t(args.Flags),
	}
	if err := nativeError("create resource", C.virgl_renderer_resource_create(&cargs, nil, 0)); err != nil {
		return err
	}
	b.resources[args.Handle] = args

	return nil
}

func (b *nativeBackend) UnrefResource(id uint32) error {
	if _, exists := b.resources[id]; !exists {
		return errUnknownResource
	}
	C.virgl_renderer_force_ctx_0()
	_ = b.DetachBacking(id)
	C.virgl_renderer_resource_unref(C.uint32_t(id))
	delete(b.resources, id)

	return nil
}

//nolint:nlreturn // cgo-generated pointer-check closures contain returns outside the Go source.
func (b *nativeBackend) AttachBacking(id uint32, buffers [][]byte) error {
	if _, exists := b.resources[id]; !exists {
		return errUnknownResource
	}
	if _, exists := b.backing[id]; exists {
		return errBackingAttached
	}
	if len(buffers) == 0 || len(buffers) > 131072 {
		return errBackingCount
	}
	backing := &nativeBacking{buffers: slices.Clone(buffers)}
	backing.iov = (*C.struct_iovec)(C.calloc(C.size_t(len(buffers)), C.size_t(C.sizeof_struct_iovec)))
	if backing.iov == nil {
		return errAllocateBacking
	}
	for index, buffer := range buffers {
		if len(buffer) == 0 {
			backing.release()

			return errEmptyBacking
		}
		// Pin is a no-op for mmap memory and pins heap-backed test buffers.
		backing.pins.Pin(&buffer[0])
		C.devbox_set_iov(backing.iov, C.int(index), unsafe.Pointer(&buffer[0]), C.size_t(len(buffer)))
	}
	code := C.virgl_renderer_resource_attach_iov(C.int(id), backing.iov, C.int(len(buffers)))
	if err := nativeError("attach backing", code); err != nil {
		backing.release()

		return err
	}
	b.backing[id] = backing

	return nil
}

func (backing *nativeBacking) release() {
	C.free(unsafe.Pointer(backing.iov))
	backing.pins.Unpin()
	backing.buffers = nil
}

//nolint:gocritic // cgo-generated pointer checks trigger dupSubExpr for the returned iovec/count parameters.
func (b *nativeBackend) DetachBacking(id uint32) error {
	if _, exists := b.resources[id]; !exists {
		return errUnknownResource
	}
	if backing, exists := b.backing[id]; exists {
		var iov *C.struct_iovec
		var count C.int
		C.virgl_renderer_resource_detach_iov(C.int(id), &iov, &count)
		backing.release()
		delete(b.backing, id)
	}

	return nil
}

func (b *nativeBackend) ContextAttachResource(ctx, res uint32) error {
	if _, exists := b.contexts[ctx]; !exists {
		return errUnknownContext
	}
	if _, exists := b.resources[res]; !exists {
		return errUnknownResource
	}
	C.virgl_renderer_ctx_attach_resource(C.int(ctx), C.int(res))

	return nil
}

func (b *nativeBackend) ContextDetachResource(ctx, res uint32) error {
	if _, exists := b.contexts[ctx]; !exists {
		return errUnknownContext
	}
	if _, exists := b.resources[res]; !exists {
		return errUnknownResource
	}
	C.virgl_renderer_ctx_detach_resource(C.int(ctx), C.int(res))

	return nil
}

func (b *nativeBackend) Transfer(
	resource, ctx, level, stride, layerStride uint32, box Box, offset uint64, toHost bool,
) error {
	if _, exists := b.resources[resource]; !exists {
		return errUnknownResource
	}
	if ctx != 0 {
		if _, exists := b.contexts[ctx]; !exists {
			return errUnknownContext
		}
	}
	if level > math.MaxInt32 || box.X > math.MaxInt32 || box.Y > math.MaxInt32 || box.Z > math.MaxInt32 ||
		box.W > math.MaxInt32 || box.H > math.MaxInt32 || box.D > math.MaxInt32 {
		return errTransferBounds
	}
	cbox := C.struct_virgl_box{
		x: C.uint32_t(box.X), y: C.uint32_t(box.Y), z: C.uint32_t(box.Z),
		w: C.uint32_t(box.W), h: C.uint32_t(box.H), d: C.uint32_t(box.D),
	}
	if toHost {
		return nativeError("transfer to host", C.virgl_renderer_transfer_write_iov(
			C.uint32_t(resource), C.uint32_t(ctx), C.int(level), C.uint32_t(stride),
			C.uint32_t(layerStride), &cbox, C.uint64_t(offset), nil, 0))
	}

	return nativeError("transfer from host", C.virgl_renderer_transfer_read_iov(
		C.uint32_t(resource), C.uint32_t(ctx), C.uint32_t(level), C.uint32_t(stride),
		C.uint32_t(layerStride), &cbox, C.uint64_t(offset), nil, 0))
}

//nolint:nlreturn // cgo-generated pointer-check closures contain returns outside the Go source.
func (b *nativeBackend) Submit(ctx uint32, data []byte) error {
	if _, exists := b.contexts[ctx]; !exists {
		return errUnknownContext
	}
	if len(data) == 0 || len(data)%4 != 0 || len(data) > 16<<20 {
		return errCommandLength
	}
	code := C.virgl_renderer_submit_cmd(unsafe.Pointer(&data[0]), C.int(ctx), C.int(len(data)/4))

	return nativeError("submit commands", code)
}

//nolint:nlreturn // cgo-generated pointer-check closures contain returns outside the Go source.
func (b *nativeBackend) ReadPixels(resource, x, y, width, height uint32) (*image.RGBA, error) {
	args, exists := b.resources[resource]
	if !exists {
		return nil, errUnknownResource
	}
	if width == 0 || height == 0 || uint64(x)+uint64(width) > uint64(args.Width) ||
		uint64(y)+uint64(height) > uint64(args.Height) || uint64(width)*uint64(height)*4 > 256<<20 {
		return nil, errReadbackBounds
	}
	switch args.Format {
	case 1, 2, 3, 4, 67, 68, 121, 134:
	default:

		return nil, fmt.Errorf("%w: %d", errScanoutFormat, args.Format)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	cbox := C.struct_virgl_box{x: C.uint32_t(x), y: C.uint32_t(y), z: 0, w: C.uint32_t(width), h: C.uint32_t(height), d: 1}
	C.virgl_renderer_force_ctx_0()
	code := C.devbox_read_pixels(C.uint32_t(resource), &cbox, unsafe.Pointer(&pixels.Pix[0]), C.size_t(len(pixels.Pix)))
	if err := nativeError("read pixels", code); err != nil {
		return nil, err
	}
	convertPixels(pixels.Pix, args.Format)

	return pixels, nil
}

func (b *nativeBackend) Fence(id uint32) error {
	return nativeError("create fence", C.virgl_renderer_create_fence(C.int(id), 0))
}

func (b *nativeBackend) Poll() []uint32 {
	C.virgl_renderer_poll()
	if b.state.completed == 0 {
		return nil
	}
	b.state.completed = 0

	return []uint32{uint32(b.state.fence)}
}

func (b *nativeBackend) Reset() error {
	C.virgl_renderer_reset()
	for _, backing := range b.backing {
		backing.release()
	}
	clear(b.backing)
	clear(b.resources)
	clear(b.contexts)
	b.state.completed = 0
	b.state.fence = 0

	return nil
}

func (b *nativeBackend) Close() error {
	C.virgl_renderer_cleanup(unsafe.Pointer(b.state))
	for _, backing := range b.backing {
		backing.release()
	}
	clear(b.backing)
	C.free(unsafe.Pointer(b.state))
	b.state = nil

	return nil
}
