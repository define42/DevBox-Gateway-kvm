package virtio

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func TestGPUBootTransitionPreservesDesktopPixels(t *testing.T) {
	t.Parallel()
	display := &RDPDisplay{framebuffer: newFramebuffer()}
	t.Cleanup(display.shutdown)
	gpu := NewGPU(11, nil, make([]byte, 8<<20), display)

	firmware := gpuBootPattern(image.Rect(0, 0, 640, 480))
	gpuBootAttach(t, gpu, 1, firmware, 0)
	gpuDamageOK(t, gpu, gpuTelemetryTransferCommand(1, firmware.Bounds(), 0))
	gpuDamageOK(t, gpu, gpuDamageCommand(gpuCmdSetScanout, 1, firmware.Bounds()))
	gpuDamageOK(t, gpu, gpuDamageCommand(gpuCmdResourceFlush, 1, firmware.Bounds()))
	var connected displayFrame
	display.copyFrameChanges(&connected, false)
	gpuBootAssertFrame(t, connected, firmware)
	firmwareFrame := connected
	firmwarePixels := bytes.Clone(connected.pix)

	// OVMF releases the GPU at ExitBootServices. The last boot picture stays
	// visible until the OS supplies the first frame from its new scanout.
	gpu.MMIO(20, []byte{0}, true)
	gpuBootAssertFrame(t, display.snapshot(), firmware)

	// The resource stride is deliberately neither the old desktop stride nor
	// the new viewport stride. Its rows also cross fragmented backing pages.
	desktop := gpuBootPattern(image.Rect(0, 0, 1293, 733))
	writeDesktop := gpuBootAttach(t, gpu, 1, desktop, 2<<20)
	gpuDamageOK(t, gpu, gpuTelemetryTransferCommand(1, desktop.Bounds(), 0))
	viewport := image.Rect(5, 7, 1285, 727)
	gpuDamageOK(t, gpu, gpuDamageCommand(gpuCmdSetScanout, 1, viewport))
	partial := image.Rect(67, 69, 78, 82)
	gpuDamageOK(t, gpu, gpuDamageCommand(gpuCmdResourceFlush, 1, partial))

	want := image.NewRGBA(image.Rect(0, 0, viewport.Dx(), viewport.Dy()))
	draw.Draw(want, want.Bounds(), desktop, viewport.Min, draw.Src)
	damage := display.copyFrameChanges(&connected, false)
	if len(damage) != 1 || damage[0] != want.Bounds() {
		t.Fatalf("first desktop damage = %v, want full viewport %v", damage, want.Bounds())
	}
	gpuBootAssertFrame(t, connected, want)
	gpuBootAssertFrame(t, display.snapshot(), want)
	if !bytes.Equal(firmwareFrame.pix, firmwarePixels) {
		t.Fatal("desktop transition changed previously captured firmware pixels")
	}

	// Transfer two regions, then flush them separately. A capture must not
	// expose the other transferred region before its RESOURCE_FLUSH arrives.
	pending := image.Rect(1100, 650, 1120, 670)
	for _, rect := range []image.Rectangle{partial, pending} {
		draw.Draw(desktop, rect, image.NewUniform(color.RGBA{R: 213, G: 37, B: 89, A: 255}), image.Point{}, draw.Src)
	}
	writeDesktop()
	for _, rect := range []image.Rectangle{partial, pending} {
		offset := uint64(desktop.PixOffset(rect.Min.X, rect.Min.Y))
		gpuDamageOK(t, gpu, gpuTelemetryTransferCommand(1, rect, offset))
	}
	for _, rect := range []image.Rectangle{partial, pending} {
		gpuDamageOK(t, gpu, gpuDamageCommand(gpuCmdResourceFlush, 1, rect))
		draw.Draw(want, rect.Sub(viewport.Min), desktop, rect.Min, draw.Src)
		damage = display.copyFrameChanges(&connected, false)
		if len(damage) == 0 {
			t.Fatal("partial desktop flush did not notify the connected reader")
		}
		gpuBootAssertFrame(t, connected, want)
		gpuBootAssertFrame(t, display.snapshot(), want)

		// A newly connected reader has no old dimensions or damage history.
		// It must reconstruct the same complete published desktop.
		var reconnected displayFrame
		damage = display.copyFrameChanges(&reconnected, false)
		if len(damage) != 1 || damage[0] != want.Bounds() {
			t.Fatalf("new reader damage = %v, want full viewport %v", damage, want.Bounds())
		}
		gpuBootAssertFrame(t, reconnected, want)
	}
}

func gpuBootPattern(bounds image.Rectangle) *image.RGBA {
	img := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: byte(x*17 + y/3), G: byte(y*29 + x/5), B: byte(x/7 + y/11), A: 255,
			})
		}
	}

	return img
}

// gpuBootAttach sends real resource commands and places logical backing pages
// in reverse physical order with gaps. The returned function updates only
// guest memory, leaving transfers and flushes under the test's control.
func gpuBootAttach(t *testing.T, gpu *GPU, id uint32, img *image.RGBA, base int) func() {
	t.Helper()
	gpuDamageOK(t, gpu, gpuResizeRequest(gpuCmdResourceCreate2D,
		id, gpuFormatB8G8R8X8, uint32(img.Rect.Dx()), uint32(img.Rect.Dy())))
	const pageSize = 4096
	pages := (len(img.Pix) + pageSize - 1) / pageSize
	request := gpuResizeRequest(gpuCmdResourceAttachBacking, id, uint32(pages))
	write := func() {
		pix, _ := framebufferBGRX(img, 0)
		for page := range pages {
			start := page * pageSize
			end := min(start+pageSize, len(pix))
			addr := base + (pages-page-1)*(pageSize+64)
			copy(gpu.Mem[addr:addr+end-start], pix[start:end])
		}
	}
	for page := range pages {
		var entry [16]byte
		addr := base + (pages-page-1)*(pageSize+64)
		length := min(pageSize, len(img.Pix)-page*pageSize)
		binary.LittleEndian.PutUint64(entry[:], uint64(addr))
		binary.LittleEndian.PutUint32(entry[8:], uint32(length))
		request = append(request, entry[:]...)
	}
	write()
	gpuDamageOK(t, gpu, request)

	return write
}

func gpuBootAssertFrame(t *testing.T, frame displayFrame, want *image.RGBA) {
	t.Helper()
	if frame.width != want.Rect.Dx() || frame.height != want.Rect.Dy() || len(frame.pix) != len(want.Pix) {
		t.Fatalf("frame = %dx%d (%d bytes), want %v (%d bytes)",
			frame.width, frame.height, len(frame.pix), want.Bounds(), len(want.Pix))
	}
	if bytes.Equal(frame.pix, want.Pix) {
		return
	}
	for i := range want.Pix {
		if frame.pix[i] != want.Pix[i] {
			t.Fatalf("pixel (%d,%d) channel %d = %d, want %d",
				i/4%frame.width, i/4/frame.width, i%4, frame.pix[i], want.Pix[i])
		}
	}
}
