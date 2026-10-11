package virtio

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/define42/gokvm/internal/rdp"
)

func TestRDPCursorMovementDoesNotPublishDesktop(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	defer d.shutdown()
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	if err := d.FlushDamage(128, 128, img, nil); err != nil {
		t.Fatal(err)
	}
	before := d.snapshot()
	shape := image.NewRGBA(image.Rect(0, 0, 16, 16))
	shape.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	cursor := DisplayCursor{Image: shape, X: -2, Y: -3, HotX: 2, HotY: 3}
	if err := d.SetCursor(cursor); err != nil {
		t.Fatal(err)
	}
	first, changed := d.cursorSnapshot()
	if first.x != 0 || first.y != 0 || first.shapeSerial == 0 {
		t.Fatalf("cursor top-left was not adjusted for hotspot: %+v", first)
	}
	cursor.X, cursor.Y = 60, 50
	if err := d.SetCursor(cursor); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	default:
		t.Fatal("cursor movement did not wake its writer")
	}
	moved, _ := d.cursorSnapshot()
	if moved.image != first.image || moved.shapeSerial != first.shapeSerial ||
		moved.positionSerial == first.positionSerial {
		t.Fatal("cursor movement replaced the shape or lost its position update")
	}
	clear(shape.Pix)
	if moved.image.Pix[0] != 255 {
		t.Fatal("cursor publication retained borrowed shape storage")
	}
	if after := d.snapshot(); after.seq != before.seq || !bytes.Equal(after.pix, before.pix) {
		t.Fatal("cursor-only movement modified the video framebuffer")
	}
	if err := d.SetCursor(DisplayCursor{}); err != nil {
		t.Fatal(err)
	}
	if hidden, _ := d.cursorSnapshot(); hidden.image != nil || hidden.shapeSerial == moved.shapeSerial {
		t.Fatal("cursor hide did not publish a new shape state")
	}
}

func TestRDPCursorSuppressesInputEchoButPreservesWarps(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{}
	first, second := &rdp.Session{}, &rdp.Session{}
	shape := image.NewRGBA(image.Rect(0, 0, 16, 16))
	shape.SetRGBA(0, 0, color.RGBA{A: 255})
	d.rememberPointerMotion(first, 100, 80)
	d.rememberPointerMotion(first, 200, 120)
	if err := d.SetCursor(DisplayCursor{Image: shape, X: 200, Y: 120}); err != nil {
		t.Fatal(err)
	}
	cursor, _ := d.cursorSnapshot()
	if cursor.echo != first || len(d.motions) != 0 {
		t.Fatal("coalesced mouse input was not consumed for echo suppression")
	}
	if err := d.SetCursor(DisplayCursor{Image: shape, X: 100, Y: 80}); err != nil {
		t.Fatal(err)
	}
	cursor, _ = d.cursorSnapshot()
	if cursor.echo != nil {
		t.Fatal("a guest warp back to an earlier input position was mistaken for an echo")
	}
	d.forgetPointerMotions(first)
	d.rememberPointerMotion(second, 20, 30)
	if err := d.SetCursor(DisplayCursor{Image: shape, X: 20, Y: 30}); err != nil {
		t.Fatal(err)
	}
	cursor, _ = d.cursorSnapshot()
	if cursor.echo != second {
		t.Fatal("input ownership did not follow the reconnected viewer")
	}
	serial := cursor.positionSerial
	shape.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	if err := d.SetCursor(DisplayCursor{Image: shape, X: 20, Y: 30}); err != nil {
		t.Fatal(err)
	}
	cursor, _ = d.cursorSnapshot()
	if cursor.positionSerial != serial {
		t.Fatal("a shape-only update unnecessarily moved the client cursor")
	}
	d.rememberPointerMotion(second, 20, 30) // Click in place, followed by two guest warps.
	if len(d.motions) != 0 {
		t.Fatal("stationary button input was retained as pending cursor motion")
	}
	for _, point := range []image.Point{{X: 40, Y: 50}, {X: 20, Y: 30}} {
		if err := d.SetCursor(DisplayCursor{Image: shape, X: point.X, Y: point.Y}); err != nil {
			t.Fatal(err)
		}
		cursor, _ = d.cursorSnapshot()
		if cursor.echo != nil {
			t.Fatal("a stationary click suppressed a subsequent guest warp")
		}
	}
}

func TestRDPCursorScalingPreservesHotspot(t *testing.T) {
	t.Parallel()
	shape := image.NewRGBA(image.Rect(0, 0, 16, 12))
	shape.SetRGBA(3, 2, color.RGBA{R: 100, A: 128})
	scaled, x, y := scaleRDPCursor(rdpCursorState{image: shape, hotX: 3, hotY: 2}, 800, 600, 1600, 1200)
	if scaled.Rect.Dx() != 32 || scaled.Rect.Dy() != 24 || x != 6 || y != 4 {
		t.Fatalf("scaled cursor dimensions or hotspot: %v (%d,%d)", scaled.Rect, x, y)
	}
	if scaled.RGBAAt(x, y) != shape.RGBAAt(3, 2) {
		t.Fatal("scaled cursor changed premultiplied alpha or hotspot pixel")
	}
}

type recordedRDPPointer struct {
	shapes    []*image.RGBA
	positions []image.Point
}

func (p *recordedRDPPointer) WritePointer(shape *image.RGBA, _, _ int) error {
	p.shapes = append(p.shapes, shape)

	return nil
}

func (p *recordedRDPPointer) WritePointerPosition(x, y int) error {
	p.positions = append(p.positions, image.Pt(x, y))

	return nil
}

func TestRDPCursorWriterRestoresHiddenPosition(t *testing.T) {
	t.Parallel()
	sink := &recordedRDPPointer{}
	owner := &rdp.Session{}
	var writer rdpPointerWriter
	shape := image.NewRGBA(image.Rect(0, 0, 8, 8))
	cursor := rdpCursorState{image: shape, x: 10, y: 20, serial: 1, shapeSerial: 1, positionSerial: 1}
	if err := writer.update(sink, owner, cursor, 800, 600, 800, 600); err != nil {
		t.Fatal(err)
	}
	cursor.image = nil
	cursor.serial++
	cursor.shapeSerial++
	if err := writer.update(sink, owner, cursor, 800, 600, 800, 600); err != nil {
		t.Fatal(err)
	}
	cursor.x, cursor.y = 100, 120
	cursor.serial++
	cursor.positionSerial++
	if err := writer.update(sink, owner, cursor, 800, 600, 800, 600); err != nil {
		t.Fatal(err)
	}
	if len(sink.positions) != 1 {
		t.Fatal("hidden cursor position was unnecessarily transmitted")
	}
	cursor.image = shape
	cursor.serial++
	cursor.shapeSerial++
	if err := writer.update(sink, owner, cursor, 800, 600, 800, 600); err != nil {
		t.Fatal(err)
	}
	if len(sink.positions) != 2 || sink.positions[1] != image.Pt(100, 120) {
		t.Fatalf("shown cursor did not restore its latest hidden position: %v", sink.positions)
	}
}

func TestRDPCursorWriterDoesNotEchoLocalMotion(t *testing.T) {
	t.Parallel()
	sink := &recordedRDPPointer{}
	owner := &rdp.Session{}
	var writer rdpPointerWriter
	shape := image.NewRGBA(image.Rect(0, 0, 8, 8))
	cursor := rdpCursorState{image: shape, x: 10, y: 20, serial: 1, shapeSerial: 1, positionSerial: 1}
	if err := writer.update(sink, owner, cursor, 800, 600, 800, 600); err != nil {
		t.Fatal(err)
	}
	cursor.x, cursor.y, cursor.echo = 200, 150, owner
	cursor.serial++
	cursor.positionSerial++
	if err := writer.update(sink, owner, cursor, 800, 600, 800, 600); err != nil {
		t.Fatal(err)
	}
	if len(sink.positions) != 1 {
		t.Fatal("guest echo moved the already-positioned local client cursor")
	}
	// A guest warp back to the first server-sent position must still be sent.
	cursor.x, cursor.y, cursor.echo = 10, 20, nil
	cursor.serial++
	cursor.positionSerial++
	if err := writer.update(sink, owner, cursor, 800, 600, 800, 600); err != nil {
		t.Fatal(err)
	}
	if len(sink.positions) != 2 || sink.positions[1] != image.Pt(10, 20) {
		t.Fatalf("guest warp was incorrectly suppressed: %v", sink.positions)
	}
}
