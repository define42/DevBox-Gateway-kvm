//go:build linux && cgo && virgl && integration

package virtio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"os"
	"testing"
	"time"
)

// This test uses the real native renderer and publishes its output through the
// RDP display's framebuffer. It needs EGL and an accessible rendering device,
// but neither a guest image nor a running VM or external RDP client.
//
//nolint:paralleltest // The native virgl library permits only one active renderer per process.
func TestGPUVirglRDPIntegration(t *testing.T) {
	if os.Getenv("DEVBOX_KVM_TEST_VIRGL") != "1" {
		t.Skip("set DEVBOX_KVM_TEST_VIRGL=1 to exercise the native virgl renderer")
	}

	const (
		width       = 16
		height      = 8
		resourceID  = 1
		contextID   = 1
		backingAddr = 0x30000
		backingSize = width * height * 4
	)
	display := newRDPTestDisplay(t)
	memory := make([]byte, 1<<20)
	gpu, err := NewGPUWith3D(11, nil, memory, display)
	if err != nil {
		t.Fatalf("initialize native virgl renderer: %v", err)
	}
	t.Cleanup(func() {
		if err := gpu.Close(); err != nil {
			t.Errorf("close native GPU: %v", err)
		}
	})
	queue := &SplitQueue{
		Desc:  new([QueueSize]SplitDesc),
		Avail: new(SplitAvail),
		Used:  new(SplitUsed),
	}
	gpu.VirtQueue[gpuControlQueue] = queue
	submit := func(request []byte, responseType uint32, responseSize int) []byte {
		t.Helper()

		return gpuVirglIntegrationSubmit(t, gpu, memory, queue, request, responseType, responseSize)
	}
	ok := func(request []byte) {
		t.Helper()
		submit(request, gpuRespOKNoData, gpuCtrlHdrLen)
	}

	if gpu.DeviceFeatures()&1 == 0 {
		t.Fatal("native renderer did not advertise VIRTIO_GPU_F_VIRGL")
	}
	config := make([]byte, gpuConfigLen)
	gpu.ReadDeviceConfig(0, config)
	capsets := binary.LittleEndian.Uint32(config[12:])
	if capsets == 0 || capsets > 2 {
		t.Fatalf("virgl capset count = %d, want 1 or 2", capsets)
	}
	for index := uint32(0); index < capsets; index++ {
		info := submit(gpuVirglIntegrationRequest(0x0108, 0, index, 0), 0x1102, 40)
		id := binary.LittleEndian.Uint32(info[24:])
		version := binary.LittleEndian.Uint32(info[28:])
		size := binary.LittleEndian.Uint32(info[32:])
		if (id != 1 && id != 2) || size == 0 || size > 64*1024 {
			t.Fatalf("capset %d: id=%d version=%d size=%d", index, id, version, size)
		}
		caps := submit(gpuVirglIntegrationRequest(0x0109, 0, id, version), 0x1103, gpuCtrlHdrLen+int(size))
		if bytes.Equal(caps[gpuCtrlHdrLen:], make([]byte, size)) {
			t.Fatalf("capset %d was entirely empty", id)
		}
	}

	createContext := gpuVirglIntegrationRequest(0x0200, contextID, make([]uint32, 18)...)
	name := []byte("devbox-virgl-integration")
	binary.LittleEndian.PutUint32(createContext[24:], uint32(len(name)))
	copy(createContext[32:], name)
	ok(createContext)
	// Mesa's legacy fence resource is PIPE_BUFFER/R8_UNORM with array_size=0.
	// Array layers do not apply to buffers, and rejecting this valid shape
	// prevents older guests from constructing their fence handles.
	ok(gpuVirglIntegrationRequest(0x0204, 0,
		2, 0, 64, 1<<17, 8, 1, 1, 0, 0, 0, 0, 0))
	ok(gpuVirglIntegrationRequest(gpuCmdResourceUnref, 0, 2, 0))
	// PIPE_TEXTURE_2D, B8G8R8X8_UNORM, render target + scanout, level 0,
	// one array layer, no multisampling, and Y_0_TOP.
	ok(gpuVirglIntegrationRequest(0x0204, 0,
		resourceID, 2, gpuFormatB8G8R8X8, (1<<1)|(1<<18),
		width, height, 1, 1, 0, 0, 1, 0))
	ok(gpuVirglIntegrationRequest(0x0202, contextID, resourceID, 0))
	ok(gpuVirglIntegrationRequest(gpuCmdResourceAttachBacking, 0,
		resourceID, 1, backingAddr, 0, backingSize, 0))

	// These commands create a surface, bind it as color buffer 0, and clear it
	// green through host GL. They follow virglrenderer tests/testvirgl_encode.c:
	// https://gitlab.freedesktop.org/virgl/virglrenderer/-/blob/main/tests/testvirgl_encode.c
	commands := []uint32{
		0x00050801, 1, resourceID, gpuFormatB8G8R8X8, 0, 0,
		0x00030005, 1, 0, 1,
		0x00080007, 4, 0, 0x3f800000, 0, 0x3f800000, 0, 0, 0,
	}
	payload := append([]uint32{uint32(len(commands) * 4), 0}, commands...)
	request := gpuVirglIntegrationRequest(0x0207, contextID, payload...)
	// A guest fence is 64 bits even though the renderer's completion token is
	// 32 bits. Requiring the exact echo catches accidental truncation.
	gpuVirglIntegrationFence(request, uint64(1)<<48|0x12345678)
	ok(request)

	viewport := image.Rect(0, 0, width, height)
	ok(gpuDamageCommand(gpuCmdSetScanout, resourceID, viewport))
	ok(gpuDamageCommand(gpuCmdResourceFlush, resourceID, viewport))
	frame := display.snapshot()
	if frame.width != width || frame.height != height {
		t.Fatalf("RDP frame size = %dx%d, want %dx%d", frame.width, frame.height, width, height)
	}
	if want := bytes.Repeat([]byte{0, 255, 0, 255}, width*height); !bytes.Equal(frame.pix, want) {
		t.Fatalf("native GL clear did not reach the RDP framebuffer: first pixel %v", frame.pix[:min(4, len(frame.pix))])
	}

	// Read the rendered result into attached guest memory before overwriting
	// it. This exercises the reverse transfer used by Mesa buffer mappings.
	transfer := gpuVirglIntegrationRequest(0x0206, contextID,
		0, 0, 0, width, height, 1, 0, 0, resourceID, 0, width*4, backingSize)
	gpuVirglIntegrationFence(transfer, uint64(1)<<48|0x12345679)
	ok(transfer)
	backing := memory[backingAddr : backingAddr+backingSize]
	for pixel := 0; pixel < width*height; pixel++ {
		if got := backing[pixel*4 : pixel*4+3]; !bytes.Equal(got, []byte{0, 255, 0}) {
			t.Fatalf("rendered guest backing pixel %d = %v, want BGR green", pixel, got)
		}
	}

	// Nonuniform rows detect flipped or stale scanout readback and channel
	// ordering mistakes that the uniform GL clear cannot expose.
	want := make([]byte, backingSize)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b := byte(17+x*7), byte(23+y*11), byte(191-y*13)
			offset := (y*width + x) * 4
			copy(backing[offset:], []byte{b, g, r, 0})
			copy(want[offset:], []byte{r, g, b, 255})
		}
	}
	binary.LittleEndian.PutUint32(transfer, 0x0205)
	gpuVirglIntegrationFence(transfer, uint64(1)<<48|0x1234567a)
	ok(transfer)
	ok(gpuDamageCommand(gpuCmdResourceFlush, resourceID, viewport))
	frame = display.snapshot()
	if !bytes.Equal(frame.pix, want) {
		for offset := 0; offset+4 <= min(len(frame.pix), len(want)); offset += 4 {
			if !bytes.Equal(frame.pix[offset:offset+4], want[offset:offset+4]) {
				t.Fatalf("RDP pixel (%d,%d) = %v, want %v",
					offset/4%width, offset/4/width, frame.pix[offset:offset+4], want[offset:offset+4])
			}
		}
		t.Fatalf("RDP framebuffer length = %d, want %d", len(frame.pix), len(want))
	}
	ok(gpuVirglIntegrationRequest(0x0203, contextID, resourceID, 0))
	ok(gpuVirglIntegrationRequest(gpuCmdResourceDetachBacking, 0, resourceID, 0))
	ok(gpuVirglIntegrationRequest(gpuCmdResourceUnref, 0, resourceID, 0))
	ok(gpuVirglIntegrationRequest(0x0201, contextID))
}

//nolint:paralleltest // The native virgl library permits only one active renderer per process.
func TestGPUVirglCursorTransparencyIntegration(t *testing.T) {
	if os.Getenv("DEVBOX_KVM_TEST_VIRGL") != "1" {
		t.Skip("set DEVBOX_KVM_TEST_VIRGL=1 to exercise the native virgl renderer")
	}
	const cursorID, frameID, contextID, backingAddr = 2, 1, 1, 0x30000
	display := newRDPTestDisplay(t)
	memory := make([]byte, 1<<20)
	gpu, err := NewGPUWith3D(11, nil, memory, display)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := gpu.Close(); err != nil {
			t.Errorf("close GPU: %v", err)
		}
	})
	queue := &SplitQueue{Desc: new([QueueSize]SplitDesc), Avail: new(SplitAvail), Used: new(SplitUsed)}
	gpu.VirtQueue[gpuControlQueue] = queue
	var fence uint64
	submit := func(request []byte) {
		t.Helper()
		fence++
		gpuVirglIntegrationFence(request, fence)
		gpuVirglIntegrationSubmit(t, gpu, memory, queue, request, gpuRespOKNoData, gpuCtrlHdrLen)
	}
	submit(gpuVirglIntegrationRequest(gpuCmdContextCreate, contextID, make([]uint32, 18)...))
	submit(gpuVirglIntegrationRequest(gpuCmdResourceCreate3D, 0,
		frameID, 2, gpuFormatB8G8R8X8, 1<<1, 16, 8, 1, 1, 0, 0, 1, 0))
	submit(gpuVirglIntegrationRequest(gpuCmdContextAttachResource, contextID, frameID, 0))
	// Linux creates an XRGB dumb buffer, attaches it to the compositor's
	// context, then uploads premultiplied ARGB cursor pixels through 2D.
	submit(gpuVirglIntegrationRequest(gpuCmdResourceCreate2D, 0,
		cursorID, gpuFormatB8G8R8X8, gpuCursorSize, gpuCursorSize))
	submit(gpuVirglIntegrationRequest(gpuCmdContextAttachResource, contextID, cursorID, 0))
	const cursorBytes = gpuCursorSize * gpuCursorSize * 4
	submit(gpuVirglIntegrationRequest(gpuCmdResourceAttachBacking, 0,
		cursorID, 1, backingAddr, 0, cursorBytes, 0))
	want := make([]byte, cursorBytes)
	for y := 0; y < gpuCursorSize; y++ {
		for x := 0; x < gpuCursorSize; x++ {
			a := []byte{0, 128, 255}[y%3]
			r, g, b := min(byte(x*2), a), min(byte(y), a), min(byte(42), a)
			i := (y*gpuCursorSize + x) * 4
			copy(memory[backingAddr+i:], []byte{b, g, r, a})
			copy(want[i:], []byte{r, g, b, a})
		}
	}
	submit(gpuVirglIntegrationRequest(gpuCmdTransferToHost2D, 0,
		0, 0, gpuCursorSize, gpuCursorSize, 0, 0, cursorID, 0))
	checkCursor := func() {
		t.Helper()
		if !gpu.handleCursor(gpuVirglIntegrationRequest(gpuCmdUpdateCursor, 0,
			0, 4, 4, 0, cursorID, 0, 0, 0)) {
			t.Fatal("cursor update rejected")
		}
		gpu.present()
		cursor, _ := display.cursorSnapshot()
		if cursor.image == nil || !bytes.Equal(cursor.image.Pix, want) {
			t.Fatal("native cursor update lost colors or transparency before reaching RDP")
		}
	}
	checkCursor()
	// Submit real GL clears to another resource in the same context. These
	// must not change either the partially transparent cursor or its border.
	for _, color := range [][3]uint32{{0x3f800000, 0, 0}, {0, 0x3f800000, 0}, {0, 0, 0x3f800000}} {
		commands := []uint32{
			0x00050801, 1, frameID, gpuFormatB8G8R8X8, 0, 0,
			0x00030005, 1, 0, 1,
			0x00080007, 4, color[0], color[1], color[2], 0x3f800000, 0, 0, 0,
		}
		payload := append([]uint32{uint32(len(commands) * 4), 0}, commands...)
		submit(gpuVirglIntegrationRequest(gpuCmdSubmit3D, contextID, payload...))
		checkCursor()
	}
}

// A wallpaper transition can retain both mipmapped source images alongside
// the compositor's scanout buffers. Ordinary RGBA8 images must fit according to
// their actual pixel size while retaining the device's aggregate memory limit.
//
//nolint:paralleltest // The native virgl library permits only one active renderer per process.
func TestGPUVirglWallpaperTransitionIntegration(t *testing.T) {
	if os.Getenv("DEVBOX_KVM_TEST_VIRGL") != "1" {
		t.Skip("set DEVBOX_KVM_TEST_VIRGL=1 to exercise the native virgl renderer")
	}
	memory := make([]byte, 1<<20)
	gpu, err := NewGPUWith3D(11, nil, memory, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := gpu.Close(); err != nil {
			t.Errorf("close GPU: %v", err)
		}
	})
	queue := &SplitQueue{Desc: new([QueueSize]SplitDesc), Avail: new(SplitAvail), Used: new(SplitUsed)}
	gpu.VirtQueue[gpuControlQueue] = queue
	var fence uint64
	submit := func(request []byte) {
		t.Helper()
		fence++
		gpuVirglIntegrationFence(request, fence)
		gpuVirglIntegrationSubmit(t, gpu, memory, queue, request, gpuRespOKNoData, gpuCtrlHdrLen)
	}
	for id := uint32(1); id <= 3; id++ {
		// Triple-buffered 1080p compositor targets, B8G8R8X8_UNORM.
		submit(gpuVirglIntegrationRequest(gpuCmdResourceCreate3D, 0,
			id, 2, gpuFormatB8G8R8X8, (1<<1)|(1<<18), 1920, 1080, 1, 1, 0, 0, 1, 0))
	}
	for id := uint32(4); id <= 5; id++ {
		// VIRGL_FORMAT_R8G8B8A8_SRGB, sampler view, thirteen mip levels.
		submit(gpuVirglIntegrationRequest(gpuCmdResourceCreate3D, 0,
			id, 2, 104, 1<<3, 4096, 4096, 1, 1, 12, 0, 0, 0))
	}
	// Keep earlier wallpaper textures cached while uploading Ubuntu 24.04's
	// 10000x5622 Province image. GNOME briefly retains the source texture,
	// a page-aligned upload buffer, and the mipmapped destination together.
	submit(gpuVirglIntegrationRequest(gpuCmdResourceCreate3D, 0,
		6, 2, gpuFormatR8G8B8X8, 1<<3, 10000, 5622, 1, 1, 13, 0, 0, 0))
	submit(gpuVirglIntegrationRequest(gpuCmdResourceCreate3D, 0,
		7, 2, gpuFormatR8G8B8X8, 1<<3, 10000, 5622, 1, 1, 0, 0, 0, 0))
	submit(gpuVirglIntegrationRequest(gpuCmdResourceCreate3D, 0,
		8, 0, 64, 1<<17, 224882688, 1, 1, 0, 0, 0, 0, 0))
	submit(gpuVirglIntegrationRequest(gpuCmdResourceCreate3D, 0,
		9, 2, gpuFormatR8G8B8X8, 1<<3, 10000, 5622, 1, 1, 13, 0, 0, 0))
	const transitionBytes = 1253258584
	if gpu.resource3DBytes != transitionBytes {
		t.Fatalf("native wallpaper transition charged %d bytes, want %d", gpu.resource3DBytes, transitionBytes)
	}
	for id := uint32(1); id <= 9; id++ {
		submit(gpuVirglIntegrationRequest(gpuCmdResourceUnref, 0, id, 0))
	}
	if gpu.resource3DBytes != 0 {
		t.Fatalf("resource bytes after transition cleanup = %d", gpu.resource3DBytes)
	}
}

func gpuVirglIntegrationRequest(command, context uint32, words ...uint32) []byte {
	request := make([]byte, gpuCtrlHdrLen+len(words)*4)
	binary.LittleEndian.PutUint32(request, command)
	binary.LittleEndian.PutUint32(request[16:], context)
	for index, word := range words {
		binary.LittleEndian.PutUint32(request[gpuCtrlHdrLen+index*4:], word)
	}

	return request
}

func gpuVirglIntegrationFence(request []byte, fence uint64) {
	binary.LittleEndian.PutUint32(request[4:], gpuFlagFence)
	binary.LittleEndian.PutUint64(request[8:], fence)
}

func gpuVirglIntegrationSubmit(t *testing.T, gpu *GPU, memory []byte, queue *SplitQueue,
	request []byte, responseType uint32, responseSize int,
) []byte {
	t.Helper()
	const requestAddr, responseAddr = 0x1000, 0x10000
	if len(request) > responseAddr-requestAddr || responseSize > 0x20000 {
		t.Fatal("integration request or response exceeds its guest-memory region")
	}
	copy(memory[requestAddr:], request)
	clear(memory[responseAddr : responseAddr+responseSize])
	used := LoadU16(&queue.Used.Idx)
	available := LoadU16(&queue.Avail.Idx)
	queue.Desc[0] = SplitDesc{Addr: requestAddr, Len: uint32(len(request)), Flags: 1, Next: 1}
	queue.Desc[1] = SplitDesc{Addr: responseAddr, Len: uint32(responseSize), Flags: 2}
	queue.Avail.Ring[available%QueueSize] = 0
	queue.Avail.Idx = available + 1
	if err := gpu.ProcessControlQueue(); err != nil && !errors.Is(err, ErrNoGPUReq) {
		t.Fatalf("process command %#x: %v", binary.LittleEndian.Uint32(request), err)
	}
	fenced := binary.LittleEndian.Uint32(request[4:])&gpuFlagFence != 0
	if fenced && LoadU16(&queue.Used.Idx) != used &&
		binary.LittleEndian.Uint32(memory[responseAddr:]) < gpuRespErrUnspec {
		t.Fatal("fenced command completed before renderer fence polling")
	}
	deadline := time.Now().Add(5 * time.Second)
	for LoadU16(&queue.Used.Idx) == used {
		gpu.pollFences()
		if LoadU16(&queue.Used.Idx) != used {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for renderer fence completion")
		}
		time.Sleep(time.Millisecond)
	}
	if got := LoadU16(&queue.Used.Idx); got != used+1 {
		t.Fatalf("used queue index = %d, want %d", got, used+1)
	}
	completion := queue.Used.Ring[used%QueueSize]
	if completion.ID != 0 || completion.Len != uint32(responseSize) {
		t.Fatalf("command %#x completion = %+v, want descriptor 0 and %d response bytes",
			binary.LittleEndian.Uint32(request), completion, responseSize)
	}
	response := bytes.Clone(memory[responseAddr : responseAddr+responseSize])
	if got := binary.LittleEndian.Uint32(response); got != responseType {
		t.Fatalf("command %#x response = %#x, want %#x", binary.LittleEndian.Uint32(request), got, responseType)
	}
	if fenced {
		if binary.LittleEndian.Uint32(response[4:])&gpuFlagFence == 0 ||
			binary.LittleEndian.Uint64(response[8:]) != binary.LittleEndian.Uint64(request[8:]) ||
			binary.LittleEndian.Uint32(response[16:]) != binary.LittleEndian.Uint32(request[16:]) {
			t.Fatalf("fence response did not preserve guest fence and context: request %x, response %x",
				request[:gpuCtrlHdrLen], response)
		}
	}

	return response
}
