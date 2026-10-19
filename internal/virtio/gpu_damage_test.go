package virtio

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

// gpuDamageSink owns snapshots, just like an asynchronous remote display must.
type gpuDamageSink struct {
	frame           *image.RGBA
	damage          []image.Rectangle
	frames, cursors int
	cursor          DisplayCursor
}

func (d *gpuDamageSink) Flush(width, height int, img *image.RGBA) error {
	return d.FlushDamage(width, height, img, nil)
}

func (d *gpuDamageSink) FlushDamage(_, _ int, img *image.RGBA, damage []image.Rectangle) error {
	d.frame = image.NewRGBA(img.Bounds())
	draw.Draw(d.frame, img.Bounds(), img, img.Bounds().Min, draw.Src)
	d.damage = append([]image.Rectangle(nil), damage...)
	d.frames++

	return nil
}

func (d *gpuDamageSink) SetCursor(cursor DisplayCursor) error {
	d.cursor = cursor
	if cursor.Image != nil {
		d.cursor.Image = image.NewRGBA(cursor.Image.Bounds())
		draw.Draw(d.cursor.Image, cursor.Image.Bounds(), cursor.Image, cursor.Image.Bounds().Min, draw.Src)
	}
	d.cursors++

	return nil
}

func (d *gpuDamageSink) Close() error { return nil }

func gpuDamageCommand(cmd, id uint32, rect image.Rectangle) []byte {
	req := make([]byte, gpuCtrlHdrLen+gpuRectLen+8)
	le := binary.LittleEndian
	le.PutUint32(req, cmd)
	le.PutUint32(req[gpuCtrlHdrLen:], uint32(rect.Min.X))
	le.PutUint32(req[gpuCtrlHdrLen+4:], uint32(rect.Min.Y))
	le.PutUint32(req[gpuCtrlHdrLen+8:], uint32(rect.Dx()))
	le.PutUint32(req[gpuCtrlHdrLen+12:], uint32(rect.Dy()))
	off := gpuCtrlHdrLen + gpuRectLen
	if cmd == gpuCmdSetScanout {
		off += 4
	}
	le.PutUint32(req[off:], id)

	return req
}

func gpuDamageOK(t *testing.T, g *GPU, req []byte) {
	t.Helper()
	if got := binary.LittleEndian.Uint32(g.handleControl(req)); got != gpuRespOKNoData {
		t.Fatalf("command %#x response %#x", binary.LittleEndian.Uint32(req), got)
	}
}

func TestGPUFlushDamageAndScanoutViewport(t *testing.T) {
	t.Parallel()
	d := &gpuDamageSink{}
	g := NewGPU(11, nil, nil, d)
	res := &gpuResource{width: 6, height: 5, format: gpuFormatB8G8R8X8, data: bytes.Repeat([]byte{7, 13, 29, 0}, 30)}
	g.resources[1] = res
	viewport := image.Rect(2, 1, 6, 4)
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, viewport))
	// The first visible flush must supply the complete new scanout.
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, image.Rect(3, 2, 4, 3)))
	if got := d.frame.Bounds(); got != image.Rect(0, 0, 4, 3) {
		t.Fatalf("scanout viewport: got %v", got)
	}
	if len(d.damage) != 1 || d.damage[0] != d.frame.Bounds() {
		t.Fatalf("initial damage: %v", d.damage)
	}
	old := d.frame
	// Simulate several transfers, then flush just one of them. Pixels outside
	// that flush must remain from the last presented image.
	copy(res.data, bytes.Repeat([]byte{31, 37, 41, 0}, 30))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, image.Rect(3, 2, 4, 3)))
	if len(d.damage) != 1 || d.damage[0] != image.Rect(1, 1, 2, 2) {
		t.Fatalf("viewport-relative damage: %v", d.damage)
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			want := color.RGBA{29, 13, 7, 255}
			if x == 1 && y == 1 {
				want = color.RGBA{41, 37, 31, 255}
			}
			if got := d.frame.RGBAAt(x, y); got != want {
				t.Fatalf("pixel (%d,%d): got %v, want %v", x, y, got, want)
			}
		}
	}
	if old.RGBAAt(1, 1) != (color.RGBA{29, 13, 7, 255}) {
		t.Fatal("new flush mutated the display's in-flight snapshot")
	}
	frames := d.frames
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, image.Rect(0, 0, 2, 1)))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, image.Rect(2, 1, 2, 1)))
	if d.frames != frames {
		t.Fatal("empty or offscreen flush published a frame")
	}
	bad := gpuDamageCommand(gpuCmdResourceFlush, 1, image.Rect(5, 4, 7, 6))
	if got := binary.LittleEndian.Uint32(g.handleControl(bad)); got != gpuRespErrInvalidParameter {
		t.Fatalf("out-of-bounds flush response: %#x", got)
	}
	// A resource change of identical dimensions must replace every pixel.
	g.resources[2] = &gpuResource{
		width: 6, height: 5, format: gpuFormatB8G8R8X8,
		data: bytes.Repeat([]byte{43, 47, 53, 0}, 30),
	}
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 2, viewport))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 2, image.Rect(3, 2, 4, 3)))
	if d.damage[0] != d.frame.Bounds() || d.frame.RGBAAt(0, 0) != (color.RGBA{53, 47, 43, 255}) {
		t.Fatal("scanout resource switch retained pixels from the old resource")
	}
	// Moving the viewport in the same resource also requires a full refresh.
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 2, image.Rect(1, 2, 5, 5)))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 2, image.Rect(3, 2, 4, 3)))
	if d.damage[0] != d.frame.Bounds() {
		t.Fatal("viewport switch was not refreshed")
	}
}

func TestGPUSeparateCursorDoesNotPublishFrames(t *testing.T) {
	t.Parallel()
	d := &gpuDamageSink{}
	g := NewGPU(11, nil, nil, d)
	g.scanout[0] = 1
	res := &gpuResource{width: 8, height: 8, format: gpuFormatB8G8R8X8, data: bytes.Repeat([]byte{7, 13, 29, 0}, 64)}
	g.resources[1] = res
	g.resources[2] = &gpuResource{
		width: 64, height: 64, format: gpuFormatB8G8R8X8,
		data: bytes.Repeat([]byte{0, 255, 0, 255}, 64*64),
	}
	g.flush(res)
	want := bytes.Clone(d.frame.Pix)
	update := make([]byte, gpuCtrlHdrLen+32)
	le := binary.LittleEndian
	le.PutUint32(update, gpuCmdUpdateCursor)
	le.PutUint32(update[gpuCtrlHdrLen+4:], 0xfffffffd) // top-left x=-3
	le.PutUint32(update[gpuCtrlHdrLen+8:], 0xfffffffe) // top-left y=-2
	le.PutUint32(update[gpuCtrlHdrLen+16:], 2)
	le.PutUint32(update[gpuCtrlHdrLen+20:], 5)
	le.PutUint32(update[gpuCtrlHdrLen+24:], 7)
	if !g.handleCursor(update) {
		t.Fatal("valid cursor rejected")
	}
	g.present()
	if d.frames != 1 || d.cursors != 1 || !bytes.Equal(d.frame.Pix, want) {
		t.Fatal("cursor update changed the desktop image")
	}
	if d.cursor.X != -3 || d.cursor.Y != -2 || d.cursor.HotX != 5 || d.cursor.HotY != 7 {
		t.Fatalf("cursor coordinates: %+v", d.cursor)
	}
	if got := d.cursor.Image.RGBAAt(0, 0); got != (color.RGBA{0, 255, 0, 255}) {
		t.Fatalf("XRGB cursor alpha: %v", got)
	}
	clear(g.resources[2].data)
	clear(res.data)
	le.PutUint32(update, gpuCmdMoveCursor)
	le.PutUint32(update[gpuCtrlHdrLen+4:], 4)
	if !g.handleCursor(update) {
		t.Fatal("cursor move rejected")
	}
	g.present()
	if d.frames != 1 || d.cursors != 2 || d.cursor.Image.RGBAAt(0, 0).A != 255 || !bytes.Equal(d.frame.Pix, want) {
		t.Fatal("cursor move exposed unflushed pixels or changed its shape")
	}
	le.PutUint32(update, gpuCmdUpdateCursor)
	le.PutUint32(update[gpuCtrlHdrLen+16:], 0)
	if !g.handleCursor(update) {
		t.Fatal("cursor hide rejected")
	}
	g.present()
	if d.frames != 1 || d.cursor.Image != nil {
		t.Fatal("cursor hide republished the desktop or remained visible")
	}
}

func TestGPUResetClearsPublishedCursor(t *testing.T) {
	t.Parallel()
	remote := &gpuDamageSink{}
	g := NewGPU(11, nil, nil, remote)
	t.Cleanup(func() { _ = g.Close() })
	// The initial driver probe must not replace the fallback system
	// pointer before the guest has published a hardware cursor.
	g.MMIO(20, []byte{0}, true)
	if remote.cursors != 0 {
		t.Fatal("initial probe published an unnecessary cursor update")
	}
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
	if !g.handleCursor(gpuResizeRequest(gpuCmdUpdateCursor, 0, 2, 3, 0, 2, 0, 0, 0)) {
		t.Fatal("valid cursor rejected")
	}
	g.present()
	if remote.cursor.Image == nil {
		t.Fatal("cursor was not presented before reset")
	}
	g.MMIO(20, []byte{0}, true)
	if remote.cursor.Image != nil || remote.frames != 1 {
		t.Fatal("reset retained the remote cursor or republished the desktop")
	}
	// A new scanout must remain cursor-free without requiring another
	// guest cursor command after the reset.
	g.resources[1] = &gpuResource{
		width: 8, height: 8, format: gpuFormatB8G8R8X8,
		data: bytes.Repeat([]byte{31, 37, 41, 0}, 64),
	}
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, bounds))
	gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, bounds))
	if remote.cursor.Image != nil || remote.frame.RGBAAt(2, 3) != (color.RGBA{41, 37, 31, 255}) {
		t.Fatal("new scanout restored the old remote cursor or stale pixels")
	}
}

type graphicsResetSink struct {
	gpuDamageSink
	resets, closes int
	cursorAtReset  bool
}

func (d *graphicsResetSink) GraphicsReset() {
	d.resets++
	d.cursorAtReset = d.cursor.Image != nil
}

func (d *graphicsResetSink) Close() error {
	d.closes++

	return nil
}

func TestGPUResetReleasesGraphicsAfterHidingCursor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		cursor bool
	}{
		{name: "without cursor", cursor: false},
		{name: "with cursor", cursor: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			display := &graphicsResetSink{}
			g := NewGPU(11, nil, nil, display)
			t.Cleanup(func() { _ = g.Close() })
			g.resources[1] = &gpuResource{
				width: 4, height: 4, format: gpuFormatB8G8R8X8,
				data: bytes.Repeat([]byte{7, 13, 29, 0}, 16),
			}
			bounds := image.Rect(0, 0, 4, 4)
			gpuDamageOK(t, g, gpuDamageCommand(gpuCmdSetScanout, 1, bounds))
			gpuDamageOK(t, g, gpuDamageCommand(gpuCmdResourceFlush, 1, bounds))
			if tc.cursor {
				g.resources[2] = &gpuResource{
					width: 64, height: 64, format: gpuFormatB8G8R8X8,
					data: bytes.Repeat([]byte{0, 255, 0, 255}, 64*64),
				}
				if !g.handleCursor(gpuResizeRequest(gpuCmdUpdateCursor, 0, 2, 3, 0, 2, 0, 0, 0)) {
					t.Fatal("valid cursor rejected")
				}
				g.present()
			}
			pixels := bytes.Clone(display.frame.Pix)
			g.MMIO(20, []byte{0}, true)
			if display.resets != 1 || display.cursorAtReset {
				t.Fatalf("graphics release: %d notifications, cursor visible=%v", display.resets, display.cursorAtReset)
			}
			if display.frames != 1 || !bytes.Equal(display.frame.Pix, pixels) {
				t.Fatal("reset replaced the last visible frame")
			}
			response := g.handleControl(gpuDamageCommand(gpuCmdResourceFlush, 1, bounds))
			if got := binary.LittleEndian.Uint32(response); got != gpuRespErrInvalidResourceID {
				t.Fatalf("reset retained the old scanout resource: response %#x", got)
			}
			if err := g.Close(); err != nil {
				t.Fatal(err)
			}
			g.Reset()
			if display.resets != 1 || display.closes != 1 {
				t.Fatal("closing the GPU or resetting a closed GPU rearmed the display")
			}
		})
	}
}
