package virtio

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

// gpuBGRXSink models a display that owns only flushed pixels, while retaining
// the legacy interface so a format or renderer change can use either path.
type gpuBGRXSink struct {
	gpuDamageSink
	directCalls int
	inspect     func(width, height, stride int, pix []byte)
	err         error
}

func (d *gpuBGRXSink) FlushBGRX(width, height, stride int, pix []byte, damage []image.Rectangle) error {
	d.directCalls++
	if d.inspect != nil {
		d.inspect(width, height, stride, pix)
	}
	if d.err != nil {
		return d.err
	}
	bounds := image.Rect(0, 0, width, height)
	frame := image.NewRGBA(bounds)
	if d.frame != nil {
		draw.Draw(frame, bounds, d.frame, image.Point{}, draw.Src)
	}
	if damage == nil {
		damage = []image.Rectangle{bounds}
	}
	for _, rect := range damage {
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				i := y*stride + x*4
				frame.SetRGBA(x, y, color.RGBA{pix[i+2], pix[i+1], pix[i], 255})
			}
		}
	}
	d.frame = frame
	d.damage = append(d.damage[:0], damage...)
	d.frames++

	return nil
}

func TestGPUBGRXFlushViewportAndDamage(t *testing.T) {
	t.Parallel()
	d := &gpuBGRXSink{}
	g := NewGPU(11, nil, nil, d)
	res := &gpuResource{width: 6, height: 5, format: gpuFormatB8G8R8X8, data: make([]byte, 6*5*4)}
	for y := 0; y < 5; y++ {
		for x := 0; x < 6; x++ {
			copy(res.data[(y*6+x)*4:], []byte{byte(x), byte(y), byte(x + y), 7})
		}
	}
	g.resources[1] = res
	viewport := image.Rect(2, 1, 6, 4)
	d.inspect = func(width, height, stride int, pix []byte) {
		if width != 4 || height != 3 || stride != 24 || len(pix) != 64 {
			t.Fatalf("source layout: %dx%d, stride %d, length %d", width, height, stride, len(pix))
		}
		if &pix[0] != &res.data[(1*6+2)*4] {
			t.Fatal("publication copied the resource or did not offset the viewport")
		}
	}
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, viewport))
	partial := image.Rect(3, 2, 4, 3)
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, partial))
	if d.directCalls != 1 || g.frame != nil || g.composite != nil {
		t.Fatal("BGRX publication retained an intermediate RGBA image")
	}
	if len(d.damage) != 1 || d.damage[0] != d.frame.Bounds() {
		t.Fatalf("first publication damage: %v", d.damage)
	}
	old := d.frame
	if got := old.RGBAAt(3, 2); got != (color.RGBA{8, 3, 5, 255}) {
		t.Fatalf("viewport source pixel: %v", got)
	}
	copy(res.data, bytes.Repeat([]byte{31, 37, 41, 0}, 30))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, partial))
	if len(d.damage) != 1 || d.damage[0] != image.Rect(1, 1, 2, 2) {
		t.Fatalf("viewport-relative damage: %v", d.damage)
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			want := old.RGBAAt(x, y)
			if x == 1 && y == 1 {
				want = color.RGBA{41, 37, 31, 255}
			}
			if got := d.frame.RGBAAt(x, y); got != want {
				t.Fatalf("pixel (%d,%d): got %v, want %v", x, y, got, want)
			}
		}
	}
	if got := old.RGBAAt(1, 1); got != (color.RGBA{5, 2, 3, 255}) {
		t.Fatalf("new publication changed a previous snapshot: %v", got)
	}
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, image.Rect(0, 0, 2, 1)))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, image.Rect(2, 1, 2, 1)))
	if d.directCalls != 2 {
		t.Fatal("offscreen or empty damage published a frame")
	}
}

func TestGPUBGRXRefreshAfterScanoutChanges(t *testing.T) {
	t.Parallel()
	d := &gpuBGRXSink{}
	g := NewGPU(11, nil, nil, d)
	viewport := image.Rect(1, 1, 5, 4)
	partial := image.Rect(2, 2, 3, 3)
	for id := uint32(1); id <= 2; id++ {
		g.resources[id] = &gpuResource{
			width: 6, height: 5, format: gpuFormatB8G8R8X8,
			data: bytes.Repeat([]byte{7, 13, byte(id * 29), 0}, 30),
		}
		gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, id, viewport))
		gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, id, partial))
		if d.damage[0] != d.frame.Bounds() || d.frame.RGBAAt(0, 0).R != byte(id*29) {
			t.Fatal("resource switch retained previously published pixels")
		}
	}
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 2, image.Rect(2, 2, 6, 5)))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 2, partial))
	if d.damage[0] != d.frame.Bounds() {
		t.Fatal("viewport move did not refresh every pixel")
	}
	g.resourceBytes = uint64(len(g.resources[1].data) + len(g.resources[2].data))
	gpuDamageOK(t, g, gpuResizeRequest(gpuCmdResourceUnref, 2, 0))
	if !g.bgrxSource.Empty() {
		t.Fatal("resource removal retained direct publication state")
	}
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, viewport))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, partial))
	g.MMIO(20, []byte{0}, true)
	if !g.bgrxSource.Empty() {
		t.Fatal("GPU reset retained direct publication state")
	}
	g.resources[1] = &gpuResource{
		width: 6, height: 5, format: gpuFormatB8G8R8X8,
		data: bytes.Repeat([]byte{43, 47, 53, 0}, 30),
	}
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, viewport))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, partial))
	if d.damage[0] != d.frame.Bounds() || d.frame.RGBAAt(0, 0).R != 53 {
		t.Fatal("reset did not refresh every pixel")
	}
}

func TestGPUBGRXFormatTransitions(t *testing.T) {
	t.Parallel()
	d := &gpuBGRXSink{}
	g := NewGPU(11, nil, nil, d)
	bounds := image.Rect(0, 0, 4, 4)
	partial := image.Rect(1, 1, 2, 2)
	g.resources[1] = &gpuResource{
		width: 4, height: 4, format: gpuFormatB8G8R8X8,
		data: bytes.Repeat([]byte{7, 13, 29, 0}, 16),
	}
	g.resources[2] = &gpuResource{
		width: 4, height: 4, format: gpuFormatR8G8B8A8,
		data: bytes.Repeat([]byte{43, 47, 53, 255}, 16),
	}
	for _, id := range []uint32{1, 2, 1} {
		gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, id, bounds))
		gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, id, partial))
		if d.damage[0] != bounds {
			t.Fatal("format transition did not refresh every pixel")
		}
		want := color.RGBA{29, 13, 7, 255}
		if id == 2 {
			want = color.RGBA{43, 47, 53, 255}
		}
		if got := d.frame.RGBAAt(0, 0); got != want {
			t.Fatalf("format transition pixel: got %v, want %v", got, want)
		}
		if (g.frame == nil) != (id == 1) {
			t.Fatal("format transition retained the wrong publication storage")
		}
	}
	if d.directCalls != 2 || d.frames != 3 {
		t.Fatalf("direct calls %d, total frames %d", d.directCalls, d.frames)
	}
}

func TestGPUBGRXCursorPublication(t *testing.T) {
	t.Parallel()
	d := &gpuBGRXSink{}
	g := NewGPU(11, nil, nil, d)
	g.resources[1] = &gpuResource{
		width: 8, height: 8, format: gpuFormatB8G8R8X8,
		data: bytes.Repeat([]byte{7, 13, 29, 0}, 64),
	}
	g.resources[2] = &gpuResource{
		width: 64, height: 64, format: gpuFormatB8G8R8X8,
		data: bytes.Repeat([]byte{0, 255, 0, 255}, 64*64),
	}
	bounds := image.Rect(0, 0, 8, 8)
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, bounds))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, bounds))
	want := bytes.Clone(d.frame.Pix)
	if !g.handleCursor(gpuResizeRequest(gpuCmdUpdateCursor, 0, 2, 3, 0, 2, 1, 1, 0)) {
		t.Fatal("cursor update rejected")
	}
	g.present()
	if d.cursor.Image == nil || d.cursor.X != 2 || d.cursor.HotX != 1 {
		t.Fatal("direct display did not receive separate cursor")
	}
	clear(g.resources[1].data)
	clear(g.resources[2].data)
	if !g.handleCursor(gpuResizeRequest(gpuCmdMoveCursor, 0, 4, 5, 0, 0, 0, 0, 0)) {
		t.Fatal("cursor move rejected")
	}
	g.present()
	if d.frames != 1 || d.directCalls != 1 || !bytes.Equal(d.frame.Pix, want) ||
		d.cursor.Image.RGBAAt(0, 0) != (color.RGBA{0, 255, 0, 255}) {
		t.Fatal("cursor move exposed unflushed pixels or republished the desktop")
	}
	if !g.handleCursor(gpuResizeRequest(gpuCmdUpdateCursor, 0, 4, 5, 0, 0, 0, 0, 0)) {
		t.Fatal("cursor hide rejected")
	}
	g.present()
	if d.cursor.Image != nil || d.frames != 1 || g.frame != nil {
		t.Fatal("cursor hide retained a cursor or allocated a desktop copy")
	}
}

func TestGPUBGRXRequiresSeparateCursor(t *testing.T) {
	t.Parallel()
	d := &gpuBGRXSink{}
	// Expose direct publication without exposing the separate cursor method.
	sink := struct {
		Display
		BGRXDisplay
	}{d, d}
	g := NewGPU(11, nil, nil, sink)
	g.scanout[0] = 1
	res := &gpuResource{
		width: 4, height: 4, format: gpuFormatB8G8R8X8,
		data: bytes.Repeat([]byte{7, 13, 29, 0}, 16),
	}
	g.cursor.image = image.NewRGBA(image.Rect(0, 0, 1, 1))
	g.cursor.image.SetRGBA(0, 0, color.RGBA{G: 255, A: 255})
	g.flush(res)
	if d.directCalls != 0 || g.frame == nil || d.frame.RGBAAt(0, 0) != (color.RGBA{G: 255, A: 255}) {
		t.Fatal("display without separate cursor skipped legacy composition")
	}
}

func TestGPUBGRXNativeResourceUsesReadback(t *testing.T) {
	t.Parallel()
	d := &gpuBGRXSink{}
	r := &gpuRendererStub{readColor: color.RGBA{G: 255, A: 255}}
	g := newGPUWithRenderer(11, nil, nil, d, r)
	t.Cleanup(func() { _ = g.Close() })
	gpuDamageOK(t, g, gpu3DTestRequest(gpuCmdResourceCreate2D, 0, 1, gpuFormatB8G8R8X8, 4, 4))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, image.Rect(0, 0, 4, 4)))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, image.Rect(1, 1, 2, 2)))
	if d.directCalls != 0 || g.frame == nil || d.frame.RGBAAt(0, 0) != r.readColor {
		t.Fatal("native resource bypassed renderer readback")
	}
}

func TestGPUBGRXValidatesPixelsAndRetriesFirstPublication(t *testing.T) {
	t.Parallel()
	d := &gpuBGRXSink{}
	g := NewGPU(11, nil, nil, d)
	res := &gpuResource{width: 4, height: 4, format: gpuFormatB8G8R8X8, data: make([]byte, 63)}
	partial := image.Rect(1, 1, 2, 2)
	if err := g.flushDamage(res, partial); err == nil || d.directCalls != 0 {
		t.Fatal("short resource pixels reached display")
	}
	res.data = bytes.Repeat([]byte{7, 13, 29, 0}, 16)
	d.err = errInvalidFramebuffer
	if err := g.flushDamage(res, partial); err != nil {
		t.Fatalf("display error changed guest flush response: %v", err)
	}
	if !g.bgrxSource.Empty() {
		t.Fatal("failed first publication retained direct source state")
	}
	d.err = nil
	if err := g.flushDamage(res, partial); err != nil {
		t.Fatal(err)
	}
	if d.damage[0] != d.frame.Bounds() || d.frame.RGBAAt(0, 0) != (color.RGBA{29, 13, 7, 255}) {
		t.Fatal("first successful publication did not refresh every pixel")
	}
}
