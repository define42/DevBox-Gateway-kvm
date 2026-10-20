// Package virgl provides an optional, thread-confined virglrenderer backend.
// Build with -tags virgl and CGO_ENABLED=1 to enable the native Linux backend.
package virgl

import (
	"errors"
	"image"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
)

var (
	ErrUnavailable = errors.New("virgl support is not compiled in; build on Linux with CGO_ENABLED=1 and -tags virgl")
	ErrClosed      = errors.New("virgl renderer is closed")
	ErrInUse       = errors.New("virglrenderer supports only one active renderer per process")
	errNoCapsets   = errors.New("virgl renderer returned no supported capability sets")
)

// Capset describes one guest-visible virgl capability set.
type Capset struct{ ID, MaxVersion, MaxSize uint32 }

// ResourceArgs uses the virgl/virtio-gpu wire values without translation.
type ResourceArgs struct {
	Handle, Target, Format, Bind    uint32
	Width, Height, Depth, ArraySize uint32
	LastLevel, NrSamples, Flags     uint32
}

// Box is a three-dimensional transfer region.
type Box struct{ X, Y, Z, W, H, D uint32 }

//nolint:interfacebloat // One thread-owned adapter implements the complete native GPU protocol.
type backend interface {
	Capsets() []Capset
	Capset(uint32, uint32) ([]byte, error)
	CreateContext(uint32, string) error
	DestroyContext(uint32) error
	CreateResource(ResourceArgs) error
	UnrefResource(uint32) error
	AttachBacking(uint32, [][]byte) error
	DetachBacking(uint32) error
	ContextAttachResource(uint32, uint32) error
	ContextDetachResource(uint32, uint32) error
	Transfer(uint32, uint32, uint32, uint32, uint32, Box, uint64, bool) error
	Submit(uint32, []byte) error
	ReadPixels(uint32, uint32, uint32, uint32, uint32) (*image.RGBA, error)
	Fence(uint32) error
	Poll() []uint32
	Reset() error
	Close() error
}

type request struct {
	run    func(backend) error
	result chan error
}

// Renderer serializes every operation, including initialization and destruction,
// on one locked OS thread. Call Close before releasing attached guest memory.
// A Renderer must not be copied after first use.
type Renderer struct {
	mu       sync.Mutex
	requests chan request
	done     chan struct{}
	closed   bool
	capsets  []Capset
}

//nolint:gochecknoglobals // The native library owns process-global state and permits only one active renderer.
var rendererInUse atomic.Bool

func newRenderer(factory func() (backend, error)) (*Renderer, error) {
	if !rendererInUse.CompareAndSwap(false, true) {
		return nil, ErrInUse
	}
	r := &Renderer{requests: make(chan request), done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(r.done)
		defer rendererInUse.Store(false)
		b, err := factory()
		if err != nil {
			ready <- err

			return
		}
		r.capsets = slices.Clone(b.Capsets())
		if len(r.capsets) == 0 {
			_ = b.Close()
			ready <- errNoCapsets

			return
		}
		ready <- nil
		for req := range r.requests {
			if req.run == nil {
				req.result <- b.Close()

				return
			}
			req.result <- req.run(b)
		}
	}()
	if err := <-ready; err != nil {
		<-r.done

		return nil, err
	}

	return r, nil
}

func (r *Renderer) call(fn func(backend) error) error {
	if r == nil {
		return ErrClosed
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.requests == nil {
		return ErrClosed
	}
	result := make(chan error, 1)
	r.requests <- request{run: fn, result: result}

	return <-result
}

func (r *Renderer) Capsets() []Capset {
	if r == nil {
		return nil
	}

	return slices.Clone(r.capsets)
}

func (r *Renderer) Capset(id, version uint32) (data []byte, err error) {
	err = r.call(func(b backend) error {
		data, err = b.Capset(id, version)

		return err
	})

	return
}

func (r *Renderer) CreateContext(id uint32, name string) error {
	return r.call(func(b backend) error { return b.CreateContext(id, name) })
}

func (r *Renderer) DestroyContext(id uint32) error {
	return r.call(func(b backend) error { return b.DestroyContext(id) })
}

func (r *Renderer) CreateResource(args ResourceArgs) error {
	return r.call(func(b backend) error { return b.CreateResource(args) })
}

func (r *Renderer) UnrefResource(id uint32) error {
	return r.call(func(b backend) error { return b.UnrefResource(id) })
}

// AttachBacking retains and pins the buffers until detach, unref, reset or close.
// The caller must keep any mmap-backed guest memory mapped during that period.
func (r *Renderer) AttachBacking(id uint32, backing [][]byte) error {
	return r.call(func(b backend) error { return b.AttachBacking(id, backing) })
}

func (r *Renderer) DetachBacking(id uint32) error {
	return r.call(func(b backend) error { return b.DetachBacking(id) })
}

func (r *Renderer) ContextAttachResource(ctx, res uint32) error {
	return r.call(func(b backend) error { return b.ContextAttachResource(ctx, res) })
}

func (r *Renderer) ContextDetachResource(ctx, res uint32) error {
	return r.call(func(b backend) error { return b.ContextDetachResource(ctx, res) })
}

func (r *Renderer) Transfer(
	resource, ctx, level, stride, layerStride uint32, box Box, offset uint64, toHost bool,
) error {
	return r.call(func(b backend) error {
		return b.Transfer(resource, ctx, level, stride, layerStride, box, offset, toHost)
	})
}

func (r *Renderer) Submit(ctx uint32, data []byte) error {
	return r.call(func(b backend) error { return b.Submit(ctx, data) })
}

// ReadPixels returns a top-to-bottom RGBA image of a scanout or cursor region.
func (r *Renderer) ReadPixels(resource, x, y, width, height uint32) (pixels *image.RGBA, err error) {
	err = r.call(func(b backend) error {
		pixels, err = b.ReadPixels(resource, x, y, width, height)

		return err
	})

	return
}

func (r *Renderer) Fence(id uint32) error {
	return r.call(func(b backend) error { return b.Fence(id) })
}

// Poll returns cumulative completed fence IDs. Virgl may coalesce callbacks;
// completion of a fence also completes earlier fences in the same stream.
func (r *Renderer) Poll() (ids []uint32) {
	_ = r.call(func(b backend) error {
		ids = b.Poll()

		return nil
	})

	return
}

func (r *Renderer) Reset() error {
	return r.call(func(b backend) error { return b.Reset() })
}

// Close waits for native cleanup and is safe to call repeatedly or concurrently.
func (r *Renderer) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.requests == nil {
		return nil
	}
	r.closed = true
	result := make(chan error, 1)
	r.requests <- request{result: result}
	err := <-result
	<-r.done

	return err
}
