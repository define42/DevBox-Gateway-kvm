package virtio_test

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"testing"

	"github.com/define42/gokvm/internal/virtio"
)

const (
	gpuCmdUpdateCursor = 0x0300
	gpuCmdMoveCursor   = 0x0301
)

type gpuCursorFixture struct {
	gpu     *virtio.GPU
	mem     []byte
	control *virtio.SplitQueue
	cursor  *virtio.SplitQueue
	display *mockDisplay
}

func newGPUCursorFixture(t *testing.T) *gpuCursorFixture {
	t.Helper()
	f := &gpuCursorFixture{
		mem: make([]byte, 0x100000), control: newSplitQueue(),
		cursor: newSplitQueue(), display: &mockDisplay{},
	}
	f.gpu = virtio.NewGPU(11, &mockInjector{}, f.mem, f.display)
	f.gpu.VirtQueue[0], f.gpu.VirtQueue[1] = f.control, f.cursor
	f.resource(t, 1, 16, 16, solidGPUPixels(16, 16, color.RGBA{B: 255, A: 255}))
	setScanout := make([]byte, 24)
	binary.LittleEndian.PutUint32(setScanout[8:], 16)
	binary.LittleEndian.PutUint32(setScanout[12:], 16)
	binary.LittleEndian.PutUint32(setScanout[20:], 1)
	f.command(t, gpuCmdSetScanout, setScanout)
	f.flush(t, 1)

	return f
}

func solidGPUPixels(width, height int, c color.RGBA) []byte {
	return bytes.Repeat([]byte{c.R, c.G, c.B, c.A}, width*height)
}

func (f *gpuCursorFixture) command(t *testing.T, cmd uint32, payload []byte) {
	t.Helper()
	mustOK(t, gpuSubmit(t, f.gpu, f.mem, f.control, gpuReq(cmd, payload), gpuCtrlHdrLen))
}

func (f *gpuCursorFixture) resource(t *testing.T, id, width, height uint32, pixels []byte) {
	t.Helper()
	f.resourceFormat(t, id, width, height, 67, pixels) // R8G8B8A8_UNORM
}

func (f *gpuCursorFixture) resourceFormat(t *testing.T, id, width, height, format uint32, pixels []byte) {
	t.Helper()
	le := binary.LittleEndian
	create := make([]byte, 16)
	le.PutUint32(create, id)
	le.PutUint32(create[4:], format)
	le.PutUint32(create[8:], width)
	le.PutUint32(create[12:], height)
	f.command(t, gpuCmdResourceCreate2D, create)

	addr := uint64(0x10000) + uint64(id)*0x10000
	copy(f.mem[addr:], pixels)
	attach := make([]byte, 24)
	le.PutUint32(attach, id)
	le.PutUint32(attach[4:], 1)
	le.PutUint64(attach[8:], addr)
	le.PutUint32(attach[16:], uint32(len(pixels)))
	f.command(t, gpuCmdResourceAttachBacking, attach)
	f.transfer(t, id, width, height)
}

func (f *gpuCursorFixture) transfer(t *testing.T, id, width, height uint32) {
	t.Helper()
	xfer := make([]byte, 32)
	binary.LittleEndian.PutUint32(xfer[8:], width)
	binary.LittleEndian.PutUint32(xfer[12:], height)
	binary.LittleEndian.PutUint32(xfer[24:], id)
	f.command(t, gpuCmdTransferToHost2D, xfer)
}

func (f *gpuCursorFixture) flush(t *testing.T, id uint32) {
	t.Helper()
	flush := make([]byte, 24)
	binary.LittleEndian.PutUint32(flush[8:], 16)
	binary.LittleEndian.PutUint32(flush[12:], 16)
	binary.LittleEndian.PutUint32(flush[16:], id)
	f.command(t, gpuCmdResourceFlush, flush)
}

func gpuCursorRequest(cmd, resource uint32, x, y int32, hotX, hotY uint32) []byte {
	payload := make([]byte, 32)
	le := binary.LittleEndian
	le.PutUint32(payload[4:], uint32(x))
	le.PutUint32(payload[8:], uint32(y))
	le.PutUint32(payload[16:], resource)
	le.PutUint32(payload[20:], hotX)
	le.PutUint32(payload[24:], hotY)

	return gpuReq(cmd, payload)
}

func (f *gpuCursorFixture) submit(t *testing.T, req []byte) {
	t.Helper()
	const addr = 0x3000
	copy(f.mem[addr:], req)
	q := f.cursor
	idx := q.Avail.Idx
	q.Desc[0] = virtio.SplitDesc{Addr: addr, Len: uint32(len(req))}
	q.Avail.Ring[idx%virtio.QueueSize] = 0
	q.Avail.Idx++
	if err := f.gpu.ProcessCursorQueue(); err != nil {
		t.Fatalf("ProcessCursorQueue: %v", err)
	}
	if q.Used.Idx != idx+1 || q.Used.Ring[idx%virtio.QueueSize].Len != 0 {
		t.Fatalf("cursor completion: index %d, entry %+v", q.Used.Idx, q.Used.Ring[idx%virtio.QueueSize])
	}
}

func (f *gpuCursorFixture) pixel(t *testing.T, x, y int, want color.RGBA) {
	t.Helper()
	if got := f.display.img.RGBAAt(x, y); got != want {
		t.Fatalf("pixel (%d,%d): got %v, want %v", x, y, got, want)
	}
}

func TestGPUCursorCompositeMoveHide(t *testing.T) {
	t.Parallel()
	f := newGPUCursorFixture(t)
	pixels := make([]byte, 64*64*4)
	copy(pixels, []byte{0, 255, 0, 255, 0, 0, 0, 0, 128, 0, 0, 128})
	f.resource(t, 2, 64, 64, pixels)
	f.submit(t, gpuCursorRequest(gpuCmdUpdateCursor, 2, 3, 3, 1, 1))
	green := color.RGBA{G: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	// pos is the image origin; a nonzero hotspot must not shift it again.
	f.pixel(t, 3, 3, green)
	f.pixel(t, 4, 3, blue)
	f.pixel(t, 5, 3, color.RGBA{R: 128, B: 127, A: 255})
	f.pixel(t, 2, 2, blue)

	// MOVE must ignore resource and hotspot fields, even if invalid.
	f.submit(t, gpuCursorRequest(gpuCmdMoveCursor, 999, 8, 6, 999, 999))
	f.pixel(t, 3, 3, blue)
	f.pixel(t, 8, 6, green)
	f.submit(t, gpuCursorRequest(gpuCmdUpdateCursor, 0, 8, 6, 0, 0))
	f.pixel(t, 8, 6, blue)
	f.submit(t, gpuCursorRequest(gpuCmdMoveCursor, 2, 3, 3, 0, 0))
	f.pixel(t, 3, 3, blue)
}

func TestGPUCursorClipsSignedCoordinates(t *testing.T) {
	t.Parallel()
	f := newGPUCursorFixture(t)
	pixels := make([]byte, 64*64*4)
	copy(pixels[(1*64+1)*4:], []byte{255, 255, 0, 255})
	f.resource(t, 2, 64, 64, pixels)
	f.submit(t, gpuCursorRequest(gpuCmdUpdateCursor, 2, -1, -1, 1, 1))
	f.pixel(t, 0, 0, color.RGBA{R: 255, G: 255, A: 255})
	f.submit(t, gpuCursorRequest(gpuCmdMoveCursor, 0, -64, -64, 0, 0))
	f.pixel(t, 0, 0, color.RGBA{B: 255, A: 255})
	f.submit(t, gpuCursorRequest(gpuCmdMoveCursor, 0, 14, 14, 0, 0))
	f.pixel(t, 15, 15, color.RGBA{R: 255, G: 255, A: 255})
}

func TestGPUCursorPreservesAlphaInDumbBufferFormats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		format uint32
		pixel  []byte
	}{
		{"B8G8R8X8", 2, []byte{0, 64, 128, 128}},
		{"X8R8G8B8", 4, []byte{128, 128, 64, 0}},
		{"X8B8G8R8", 68, []byte{128, 0, 64, 128}},
		{"R8G8B8X8", 134, []byte{128, 64, 0, 128}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newGPUCursorFixture(t)
			pixels := make([]byte, 64*64*4)
			copy(pixels, tc.pixel)
			f.resourceFormat(t, 2, 64, 64, tc.format, pixels)
			f.submit(t, gpuCursorRequest(gpuCmdUpdateCursor, 2, 3, 3, 0, 0))
			f.pixel(t, 3, 3, color.RGBA{R: 128, G: 64, B: 127, A: 255})
			f.pixel(t, 4, 3, color.RGBA{B: 255, A: 255})
		})
	}
}

func TestGPUCursorPreservesPresentedFrameAndShape(t *testing.T) {
	t.Parallel()
	f := newGPUCursorFixture(t)
	pixels := make([]byte, 64*64*4)
	copy(pixels, []byte{0, 255, 0, 255})
	f.resource(t, 2, 64, 64, pixels)
	f.submit(t, gpuCursorRequest(gpuCmdUpdateCursor, 2, 2, 2, 0, 0))

	copy(f.mem[0x20000:], solidGPUPixels(16, 16, color.RGBA{R: 255, A: 255}))
	f.transfer(t, 1, 16, 16)
	clear(f.mem[0x30000 : 0x30000+len(pixels)])
	f.transfer(t, 2, 64, 64)
	f.submit(t, gpuCursorRequest(gpuCmdMoveCursor, 0, 4, 4, 0, 0))
	f.pixel(t, 0, 0, color.RGBA{B: 255, A: 255}) // desktop transfer not flushed
	f.pixel(t, 4, 4, color.RGBA{G: 255, A: 255}) // cursor not updated yet
	count := f.display.flushes
	f.flush(t, 2)
	if f.display.flushes != count {
		t.Fatal("offscreen cursor resource replaced the desktop")
	}
	f.flush(t, 1)
	f.pixel(t, 0, 0, color.RGBA{R: 255, A: 255})
	f.pixel(t, 4, 4, color.RGBA{G: 255, A: 255})
	f.submit(t, gpuCursorRequest(gpuCmdUpdateCursor, 2, 4, 4, 0, 0))
	f.pixel(t, 4, 4, color.RGBA{R: 255, A: 255})
}

func TestGPUCursorRejectsInvalidUpdates(t *testing.T) {
	t.Parallel()
	f := newGPUCursorFixture(t)
	f.resource(t, 2, 64, 64, solidGPUPixels(64, 64, color.RGBA{G: 255, A: 255}))
	f.submit(t, gpuCursorRequest(gpuCmdUpdateCursor, 2, 2, 2, 0, 0))
	badScanout := gpuCursorRequest(gpuCmdUpdateCursor, 0, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(badScanout[gpuCtrlHdrLen:], 1)
	requests := [][]byte{
		gpuReq(gpuCmdUpdateCursor, nil),
		gpuCursorRequest(gpuCmdUpdateCursor, 999, 0, 0, 0, 0),
		gpuCursorRequest(gpuCmdUpdateCursor, 1, 0, 0, 0, 0), // wrong resource dimensions
		gpuCursorRequest(gpuCmdUpdateCursor, 2, 0, 0, 64, 0),
		gpuCursorRequest(0xdead, 0, 0, 0, 0, 0),
		badScanout,
	}
	count := f.display.flushes
	for _, req := range requests {
		f.submit(t, req)
	}
	if f.display.flushes != count {
		t.Fatal("invalid cursor request changed the display")
	}
	f.pixel(t, 2, 2, color.RGBA{G: 255, A: 255})
}
