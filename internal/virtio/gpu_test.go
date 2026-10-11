package virtio_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"sync"
	"testing"

	"github.com/define42/gokvm/internal/virtio"
)

// Wire-format command/response values, mirrored here so the external test does
// not depend on the package's unexported constants.
const (
	gpuCmdGetDisplayInfo        = 0x0100
	gpuCmdResourceCreate2D      = 0x0101
	gpuCmdSetScanout            = 0x0103
	gpuCmdResourceFlush         = 0x0104
	gpuCmdTransferToHost2D      = 0x0105
	gpuCmdResourceAttachBacking = 0x0106

	gpuRespOKNoData            = 0x1100
	gpuRespOKDisplayInfo       = 0x1101
	gpuRespErrInvalidScanoutID = 0x1202
	gpuCtrlHdrLen              = 24
	gpuDisplayInfoLen          = gpuCtrlHdrLen + 16*24
)

// mockDisplay captures the most recent flushed frame.
type mockDisplay struct {
	mu      sync.Mutex
	width   int
	height  int
	img     *image.RGBA
	flushes int
}

func (d *mockDisplay) Flush(width, height int, img *image.RGBA) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.width, d.height = width, height
	// Flush only lends the pixels for this call; retain an owned snapshot.
	d.img = image.NewRGBA(img.Bounds())
	copy(d.img.Pix, img.Pix)
	d.flushes++

	return nil
}

func (d *mockDisplay) Close() error { return nil }

// gpuReq builds a virtio_gpu_ctrl_hdr of the given type with payload appended.
func gpuReq(cmdType uint32, payload []byte) []byte {
	b := make([]byte, gpuCtrlHdrLen+len(payload))
	binary.LittleEndian.PutUint32(b[0:], cmdType)
	copy(b[gpuCtrlHdrLen:], payload)

	return b
}

// gpuSubmit places req on the control queue, runs the processor, and returns
// the device's response bytes.
func gpuSubmit(t *testing.T, v *virtio.GPU, mem []byte, q *virtio.SplitQueue, req []byte, respCap int) []byte {
	t.Helper()

	const reqAddr, respAddr = 0x1000, 0x2000

	copy(mem[reqAddr:reqAddr+len(req)], req)
	clear(mem[respAddr : respAddr+respCap])

	cur := q.Avail.Idx
	q.Desc[0] = virtio.SplitDesc{Addr: reqAddr, Len: uint32(len(req)), Flags: 0x1, Next: 1}
	q.Desc[1] = virtio.SplitDesc{Addr: respAddr, Len: uint32(respCap), Flags: 0x2}
	q.Avail.Ring[cur%virtio.QueueSize] = 0
	q.Avail.Idx = cur + 1

	if err := v.ProcessControlQueue(); err != nil {
		t.Fatalf("ProcessControlQueue: %v", err)
	}

	resp := make([]byte, respCap)
	copy(resp, mem[respAddr:respAddr+respCap])

	return resp
}

// mustOK asserts a response is VIRTIO_GPU_RESP_OK_NODATA.
func mustOK(t *testing.T, resp []byte) {
	t.Helper()

	if got := binary.LittleEndian.Uint32(resp); got != gpuRespOKNoData {
		t.Fatalf("resp type: got 0x%x want 0x%x", got, gpuRespOKNoData)
	}
}

func TestGPUGetDeviceHeader(t *testing.T) {
	t.Parallel()

	v := virtio.NewGPU(11, &mockInjector{}, []byte{}, nil)
	hdr := v.GetDeviceHeader()

	// 0x1050 is the non-transitional (modern) virtio-gpu device id.
	if hdr.DeviceID != 0x1050 {
		t.Fatalf("DeviceID: expected 0x1050, actual 0x%x", hdr.DeviceID)
	}

	if hdr.ClassCode != 0x03 || hdr.Subclass != 0x80 {
		t.Fatalf("class: got %#x/%#x, want 0x03/0x80", hdr.ClassCode, hdr.Subclass)
	}

	if hdr.Status&0x10 == 0 {
		t.Fatal("capabilities-list status bit not set")
	}

	if hdr.CapabilitiesPointer != 0x40 {
		t.Fatalf("CapabilitiesPointer: expected 0x40, actual 0x%x", hdr.CapabilitiesPointer)
	}
}

func TestGPUMMIOSize(t *testing.T) {
	t.Parallel()

	v := virtio.NewGPU(11, &mockInjector{}, []byte{}, nil)

	if sz := v.MMIOSize(); sz != 0x4000 {
		t.Fatalf("MMIOSize: expected 0x4000, actual 0x%x", sz)
	}

	if idx := v.MMIOBARIndex(); idx != 0 {
		t.Fatalf("MMIOBARIndex: expected 0, actual %d", idx)
	}
}

func TestGPUDeviceConfig(t *testing.T) {
	t.Parallel()

	v := virtio.NewGPU(11, &mockInjector{}, []byte{}, nil)

	buf := make([]byte, 16)
	v.ReadDeviceConfig(0, buf)

	// num_scanouts lives at offset 8 of struct virtio_gpu_config.
	if ns := binary.LittleEndian.Uint32(buf[8:]); ns != 1 {
		t.Fatalf("num_scanouts: expected 1, got %d", ns)
	}
}

func TestGPUGetDisplayInfo(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 0x100000)
	v := virtio.NewGPU(11, &mockInjector{}, mem, nil)
	q := newSplitQueue()
	v.VirtQueue[0] = q

	resp := gpuSubmit(t, v, mem, q, gpuReq(gpuCmdGetDisplayInfo, nil), gpuDisplayInfoLen)

	le := binary.LittleEndian
	if got := le.Uint32(resp[0:]); got != gpuRespOKDisplayInfo {
		t.Fatalf("resp type: got 0x%x want 0x%x", got, gpuRespOKDisplayInfo)
	}

	// pmodes[0]: rect.width @ hdr+8, rect.height @ hdr+12, enabled @ hdr+16.
	if w := le.Uint32(resp[gpuCtrlHdrLen+8:]); w != 1024 {
		t.Fatalf("pmodes[0].width: got %d want 1024", w)
	}

	if h := le.Uint32(resp[gpuCtrlHdrLen+12:]); h != 768 {
		t.Fatalf("pmodes[0].height: got %d want 768", h)
	}

	if en := le.Uint32(resp[gpuCtrlHdrLen+16:]); en != 1 {
		t.Fatalf("pmodes[0].enabled: got %d want 1", en)
	}
}

func TestGPUFenceEcho(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 0x100000)
	v := virtio.NewGPU(11, &mockInjector{}, mem, nil)
	q := newSplitQueue()
	v.VirtQueue[0] = q

	le := binary.LittleEndian
	req := gpuReq(gpuCmdGetDisplayInfo, nil)
	le.PutUint32(req[4:], 0x1) // flags = VIRTIO_GPU_FLAG_FENCE
	le.PutUint64(req[8:], 0xdeadbeefcafe)

	resp := gpuSubmit(t, v, mem, q, req, gpuDisplayInfoLen)

	if got := le.Uint32(resp[4:]); got&0x1 == 0 {
		t.Fatalf("resp flags: FENCE not echoed, got 0x%x", got)
	}

	if got := le.Uint64(resp[8:]); got != 0xdeadbeefcafe {
		t.Fatalf("resp fence_id: got 0x%x want 0xdeadbeefcafe", got)
	}
}

func TestGPUSetScanoutInvalid(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 0x100000)
	v := virtio.NewGPU(11, &mockInjector{}, mem, nil)
	q := newSplitQueue()
	v.VirtQueue[0] = q

	le := binary.LittleEndian
	setScanout := make([]byte, 16+8)
	le.PutUint32(setScanout[16:], 5) // scanout_id out of range (only 1 scanout)
	le.PutUint32(setScanout[20:], 1)

	resp := gpuSubmit(t, v, mem, q, gpuReq(gpuCmdSetScanout, setScanout), gpuCtrlHdrLen)

	if got := le.Uint32(resp[0:]); got != gpuRespErrInvalidScanoutID {
		t.Fatalf("resp type: got 0x%x want 0x%x", got, gpuRespErrInvalidScanoutID)
	}
}

// TestGPUCreateTransferFlush drives the full 2D pipeline and verifies the
// flushed frame reaches the display with correct (format-converted) pixels.
func TestGPUCreateTransferFlush(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 0x100000)
	disp := &mockDisplay{}
	v := virtio.NewGPU(11, &mockInjector{}, mem, disp)
	q := newSplitQueue()
	v.VirtQueue[0] = q

	le := binary.LittleEndian

	const resID = 1

	// RESOURCE_CREATE_2D: id=1, format=B8G8R8A8 (1), 2x2.
	create := make([]byte, 16)
	le.PutUint32(create[0:], resID)
	le.PutUint32(create[4:], 1) // VIRTIO_GPU_FORMAT_B8G8R8A8_UNORM
	le.PutUint32(create[8:], 2)
	le.PutUint32(create[12:], 2)

	mustOK(t, gpuSubmit(t, v, mem, q, gpuReq(gpuCmdResourceCreate2D, create), gpuCtrlHdrLen))

	// Guest framebuffer: 4 pixels in B,G,R,A byte order.
	const fb = 0x40000

	pixels := []byte{
		0x01, 0x02, 0x03, 0x04,
		0x11, 0x12, 0x13, 0x14,
		0x21, 0x22, 0x23, 0x24,
		0x31, 0x32, 0x33, 0x34,
	}
	copy(mem[fb:fb+len(pixels)], pixels)

	// RESOURCE_ATTACH_BACKING: id=1, one entry {addr=fb, len=16}.
	attach := make([]byte, 8+16)
	le.PutUint32(attach[0:], resID)
	le.PutUint32(attach[4:], 1)
	le.PutUint64(attach[8:], fb)
	le.PutUint32(attach[16:], uint32(len(pixels)))

	mustOK(t, gpuSubmit(t, v, mem, q, gpuReq(gpuCmdResourceAttachBacking, attach), gpuCtrlHdrLen))

	// SET_SCANOUT: scanout 0 -> resource 1, rect 2x2.
	setScanout := make([]byte, 16+8)
	le.PutUint32(setScanout[8:], 2)
	le.PutUint32(setScanout[12:], 2)
	le.PutUint32(setScanout[16:], 0)
	le.PutUint32(setScanout[20:], resID)

	mustOK(t, gpuSubmit(t, v, mem, q, gpuReq(gpuCmdSetScanout, setScanout), gpuCtrlHdrLen))

	// TRANSFER_TO_HOST_2D: rect 2x2, offset 0, resource 1.
	xfer := make([]byte, 16+8+8)
	le.PutUint32(xfer[8:], 2)
	le.PutUint32(xfer[12:], 2)
	le.PutUint32(xfer[24:], resID)

	mustOK(t, gpuSubmit(t, v, mem, q, gpuReq(gpuCmdTransferToHost2D, xfer), gpuCtrlHdrLen))

	// RESOURCE_FLUSH: rect 2x2, resource 1.
	flush := make([]byte, 16+8)
	le.PutUint32(flush[8:], 2)
	le.PutUint32(flush[12:], 2)
	le.PutUint32(flush[16:], resID)

	mustOK(t, gpuSubmit(t, v, mem, q, gpuReq(gpuCmdResourceFlush, flush), gpuCtrlHdrLen))

	if disp.flushes != 1 {
		t.Fatalf("display flushes: got %d want 1", disp.flushes)
	}

	if disp.width != 2 || disp.height != 2 {
		t.Fatalf("display dims: got %dx%d want 2x2", disp.width, disp.height)
	}

	// B8G8R8A8 pixel 0 {B=1,G=2,R=3,A=4} -> RGBA {3,2,1,4}.
	if got := disp.img.Pix[0:4]; !bytes.Equal(got, []byte{0x03, 0x02, 0x01, 0x04}) {
		t.Fatalf("px0 RGBA: got %v want [3 2 1 4]", got)
	}

	// Pixel 3 {B=0x31,G=0x32,R=0x33,A=0x34} -> RGBA {0x33,0x32,0x31,0x34}.
	if got := disp.img.Pix[12:16]; !bytes.Equal(got, []byte{0x33, 0x32, 0x31, 0x34}) {
		t.Fatalf("px3 RGBA: got %v want [51 50 49 52]", got)
	}
}

func TestGPUCursorQueue(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 0x10000)
	v := virtio.NewGPU(11, &mockInjector{}, mem, nil)
	q := newSplitQueue()
	v.VirtQueue[1] = q // cursorq

	q.Desc[0] = virtio.SplitDesc{Addr: 0x1000, Len: 56}
	q.Avail.Ring[0] = 0
	q.Avail.Idx = 1

	if err := v.ProcessCursorQueue(); err != nil {
		t.Fatalf("ProcessCursorQueue: %v", err)
	}

	// Cursor commands complete with a zero-length used entry.
	if q.Used.Idx != 1 {
		t.Fatalf("used.Idx: got %d want 1", q.Used.Idx)
	}

	if q.Used.Ring[0].Len != 0 {
		t.Fatalf("cursor used len: got %d want 0", q.Used.Ring[0].Len)
	}
}

func TestGPUProcessNilQueue(t *testing.T) {
	t.Parallel()

	v := virtio.NewGPU(11, &mockInjector{}, make([]byte, 0x1000), nil)

	if err := v.ProcessControlQueue(); err == nil {
		t.Fatal("expected error for nil control queue")
	}

	if err := v.ProcessCursorQueue(); err == nil {
		t.Fatal("expected error for nil cursor queue")
	}
}

func TestGPUClose(t *testing.T) {
	t.Parallel()

	v := virtio.NewGPU(11, &mockInjector{}, make([]byte, 0x1000), &mockDisplay{})

	if err := v.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A second Close must not panic (closeOnce guards the done channel).
	if err := v.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
