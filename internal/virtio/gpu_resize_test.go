package virtio

import (
	"encoding/binary"
	"errors"
	"image"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newResizeGPU(t *testing.T) (*GPU, *atomic.Uint32) {
	t.Helper()
	g := NewGPU(11, nil, make([]byte, 1<<20), nil)
	injected := new(atomic.Uint32)
	g.ModernTransport = NewModernTransport(g, g.Mem, func() error {
		injected.Add(1)

		return nil
	})
	t.Cleanup(func() { _ = g.Close() })

	return g, injected
}

func gpuResizeRead(g *GPU, offset uint64, size int) uint32 {
	var result [4]byte
	g.MMIO(offset, result[:size], false)

	return binary.LittleEndian.Uint32(result[:])
}

func gpuResizeRequest(kind uint32, values ...uint32) []byte {
	request := make([]byte, gpuCtrlHdrLen+4*len(values))
	binary.LittleEndian.PutUint32(request, kind)
	for i, value := range values {
		binary.LittleEndian.PutUint32(request[gpuCtrlHdrLen+4*i:], value)
	}

	return request
}

func TestGPUDisplayResizeNotification(t *testing.T) {
	t.Parallel()
	g, injected := newResizeGPU(t)
	if err := g.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := g.SetDisplaySize(1280, 800); err != nil {
		t.Fatal(err)
	}
	if gen := gpuResizeRead(g, 21, 1); gen != 1 {
		t.Fatalf("configuration generation=%d, want1", gen)
	}
	if flags := gpuResizeRead(g, isrCfgOffset, 1); flags != 3 {
		t.Fatalf("configuration change lost queue interrupt: ISR=%d", flags)
	}
	if flags := gpuResizeRead(g, isrCfgOffset, 1); flags != 0 {
		t.Fatalf("ISR was not read-to-clear: %d", flags)
	}
	if events := gpuResizeRead(g, deviceCfgOffset, 4); events != gpuEventDisplay {
		t.Fatalf("display event missing: %x", events)
	}
	// Repeated mode requests are coalesced without an interrupt or generation
	// change; different pending sizes replace the advertised preferred mode.
	if err := g.SetDisplaySize(1280, 800); err != nil {
		t.Fatal(err)
	}
	if injected.Load() != 2 || gpuResizeRead(g, 21, 1) != 1 {
		t.Fatal("duplicate mode change was not coalesced")
	}
	if err := g.SetDisplaySize(1360, 768); err != nil {
		t.Fatal(err)
	}
	if err := g.SetDisplaySize(1280, 720); err != nil {
		t.Fatal(err)
	}
	info := g.cmdGetDisplayInfo()
	if binary.LittleEndian.Uint32(info[gpuCtrlHdrLen+8:]) != 1280 ||
		binary.LittleEndian.Uint32(info[gpuCtrlHdrLen+12:]) != 720 || gpuResizeRead(g, 21, 1) != 3 {
		t.Fatal("GET_DISPLAY_INFO did not report the latest requested mode")
	}
	g.WriteDeviceConfig(0, []byte{1, 0, 0, 0}) // events_read is read-only.
	g.WriteDeviceConfig(4, []byte{2, 0, 0, 0}) // Unknown event bits do not clear DISPLAY.
	g.WriteDeviceConfig(5, []byte{1})
	if gpuResizeRead(g, deviceCfgOffset, 4) != gpuEventDisplay {
		t.Fatal("unrelated config write cleared the display event")
	}
	g.WriteDeviceConfig(3, []byte{0, 1}) // A partial access overlapping events_clear.
	if gpuResizeRead(g, deviceCfgOffset, 4) != 0 || gpuResizeRead(g, deviceCfgOffset+4, 4) != 0 {
		t.Fatal("events_clear is not write-one-to-clear/read-as-zero")
	}
}

func TestGPUDisplaySizeValidationAndReset(t *testing.T) {
	t.Parallel()
	g, injected := newResizeGPU(t)
	for _, size := range [][2]int{
		{0, 768},
		{-2, 768},
		{198, 768},
		{1024, 199},
		{1025, 768},
		{1024, 769},
		{4098, 2160},
		{4096, 4096},
	} {
		if err := g.SetDisplaySize(size[0], size[1]); !errors.Is(err, ErrGPUDisplaySize) {
			t.Fatalf("accepted invalid display size %v: %v", size, err)
		}
	}
	if injected.Load() != 0 || gpuResizeRead(g, 21, 1) != 0 {
		t.Fatal("invalid mode changed guest state")
	}
	if err := g.SetDisplaySize(1920, 1080); err != nil {
		t.Fatal(err)
	}
	g.resources[1] = &gpuResource{data: make([]byte, 16)}
	g.resourceBytes = 16
	g.scanout[0] = 1
	g.frame = image.NewRGBA(image.Rect(0, 0, 2, 2))
	g.QueueReady(0, &SplitQueue{Desc: new([QueueSize]SplitDesc), Avail: new(SplitAvail), Used: new(SplitUsed)})
	g.MMIO(20, []byte{0}, true) // Driver probe/reset must preserve host preference.
	if g.width != 1920 || g.height != 1080 || len(g.resources) != 0 || g.resourceBytes != 0 ||
		g.frame != nil || g.scanout[0] != 0 || g.VirtQueue[0] != nil || g.events != 0 {
		t.Fatal("reset retained guest resources or discarded the preferred mode")
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if err := g.SetDisplaySize(1280, 800); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("resize after close: %v", err)
	}
}

func TestGPUResizeConcurrentWithQueuesConfigAndReset(t *testing.T) {
	t.Parallel()
	g, _ := newResizeGPU(t)
	finished := make(chan struct{})
	go func() {
		g.IOThreadEntry()
		close(finished)
	}()
	var workers sync.WaitGroup
	workers.Go(func() {
		for i := range 300 {
			if err := g.SetDisplaySize(1280+8*(i%2), 800); err != nil {
				t.Error(err)
			}
		}
	})
	workers.Go(func() {
		for range 300 {
			g.ReadDeviceConfig(0, make([]byte, gpuConfigLen))
			g.WriteDeviceConfig(4, []byte{1, 0, 0, 0})
			_ = gpuResizeRead(g, 21, 1)
			_ = gpuResizeRead(g, isrCfgOffset, 1)
		}
	})
	workers.Go(func() {
		for range 300 {
			g.QueueReady(0, &SplitQueue{Desc: new([QueueSize]SplitDesc), Avail: new(SplitAvail), Used: new(SplitUsed)})
			g.Notify(0)
			_ = g.ProcessControlQueue()
			g.MMIO(20, []byte{0}, true)
		}
	})
	workers.Wait()
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("GPU worker did not stop")
	}
}

type resizeCloseDisplay struct {
	close func() error
}

func (d resizeCloseDisplay) Flush(_ int, _ int, _ *image.RGBA) error { return nil }
func (d resizeCloseDisplay) Close() error                            { return d.close() }

func TestGPUCloseReleasesMutexBeforeDisplayShutdown(t *testing.T) {
	t.Parallel()
	g, _ := newResizeGPU(t)
	var closes atomic.Uint32
	g.display = resizeCloseDisplay{close: func() error {
		closes.Add(1)

		return g.SetDisplaySize(1280, 800)
	}}
	done := make(chan error, 1)
	go func() { done <- g.Close() }()
	select {
	case err := <-done:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("display close result=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("display shutdown deadlocked with a resize request")
	}
	_ = g.Close()
	if closes.Load() != 1 {
		t.Fatal("display was closed more than once")
	}
}

func TestGPURejectsUnboundedResourcesAndBacking(t *testing.T) {
	t.Parallel()
	g, _ := newResizeGPU(t)
	for _, size := range [][2]uint32{{0, 64}, {64, 0}, {4097, 1}, {1, 4097}, {4096, 4096}, {^uint32(0), ^uint32(0)}} {
		response := g.handleControl(gpuResizeRequest(gpuCmdResourceCreate2D, 1, gpuFormatB8G8R8X8, size[0], size[1]))
		if binary.LittleEndian.Uint32(response) != gpuRespErrInvalidParameter || len(g.resources) != 0 {
			t.Fatalf("invalid resource geometry %v allocated memory", size)
		}
	}
	g.resourceBytes = gpuResourceLimit
	request := gpuResizeRequest(gpuCmdResourceCreate2D, 1, gpuFormatB8G8R8X8, 2, 2)
	if response := g.handleControl(request); binary.LittleEndian.Uint32(response) != gpuRespErrOutOfMemory {
		t.Fatal("total GPU resource memory limit was ignored")
	}
	g.resourceBytes = 0
	response := g.handleControl(request)
	if binary.LittleEndian.Uint32(response) != gpuRespOKNoData || g.resourceBytes != 16 {
		t.Fatal("valid small resource could not be created")
	}
	if response := g.handleControl(request); binary.LittleEndian.Uint32(response) != gpuRespErrInvalidResourceID {
		t.Fatal("duplicate resource ID replaced existing storage")
	}
	for _, request := range [][]byte{
		gpuResizeRequest(gpuCmdResourceAttachBacking, 1, ^uint32(0)),
		gpuResizeRequest(gpuCmdResourceAttachBacking, 1, 1),
		gpuResizeRequest(gpuCmdResourceAttachBacking, 1, 1, 0xffffffff, 0xffffffff, 16, 0),
	} {
		if response := g.handleControl(request); binary.LittleEndian.Uint32(response) != gpuRespErrInvalidParameter {
			t.Fatal("invalid backing entry array was accepted")
		}
	}
	g.handleControl(gpuResizeRequest(gpuCmdResourceUnref, 1, 0))
	if g.resourceBytes != 0 {
		t.Fatal("resource unref retained memory budget")
	}
}

func TestGPUResizeAllowsOverlappingScanoutBuffers(t *testing.T) {
	t.Parallel()
	g, _ := newResizeGPU(t)
	// Account for three old maximum-size buffers without allocating them.
	// The guest must be able to create a replacement before releasing those.
	g.resourceBytes = 3 * gpuMaxPixels * gpuBytesPerPixel
	request := gpuResizeRequest(gpuCmdResourceCreate2D, 1, gpuFormatB8G8R8X8, 4096, 2160)
	if response := g.handleControl(request); binary.LittleEndian.Uint32(response) != gpuRespOKNoData {
		t.Fatal("old scanout buffers prevented allocation of the resized desktop")
	}
}

func TestGPURejectsCyclicAndOversizedDescriptorChains(t *testing.T) {
	t.Parallel()
	g, _ := newResizeGPU(t)
	q := &SplitQueue{Desc: new([QueueSize]SplitDesc), Avail: new(SplitAvail), Used: new(SplitUsed)}
	for _, descriptor := range []SplitDesc{
		{Addr: 4096, Len: 24, Flags: descFNext, Next: 0},
		{Addr: 4096, Len: gpuMaxRequest + 1},
		{Addr: ^uint64(0), Len: 24},
		{Addr: 4096, Len: 24, Flags: descFNext, Next: QueueSize},
	} {
		q.Desc[0] = descriptor
		if request, response := g.collectChain(q, 0); request != nil || response != nil {
			t.Fatalf("invalid chain accepted: %+v", descriptor)
		}
	}
}
