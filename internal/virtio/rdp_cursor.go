package virtio

import (
	"bytes"
	"image"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/damage"
)

type rdpCursorState struct {
	image                               *image.RGBA
	x, y, hotX, hotY                    int
	serial, shapeSerial, positionSerial uint64
	echo                                *rdp.Session
	changed                             chan struct{}
}

type rdpPointerMotion struct {
	session *rdp.Session
	x, y    int
	when    time.Time
}

type rdpPointerWriter struct {
	serial, shapeSerial, positionSerial uint64
	guestWidth                          int
	guestHeight                         int
	visible                             bool
}

type rdpPointerSink interface {
	WritePointer(*image.RGBA, int, int) error
	WritePointerPosition(int, int) error
}

// FlushDamage receives cursor-free scanout pixels and copies only changed
// regions. The guest owns img until this call returns.
func (d *RDPDisplay) FlushDamage(width, height int, img *image.RGBA, rects []image.Rectangle) error {
	d.textMu.Lock()
	defer d.textMu.Unlock()
	if d.stopped {
		return nil
	}
	// Serial may have replaced every pixel, even at the same dimensions.
	// Reclaim the whole scanout on the first real update after a reset.
	if rects != nil {
		bounds := image.Rect(0, 0, width, height)
		changed := false
		for _, rect := range rects {
			if !rect.Intersect(bounds).Empty() {
				changed = true

				break
			}
		}
		if !changed {
			return nil
		}
		if !d.textDisabled {
			rects = nil
		}
	}
	d.mu.Lock()
	before := d.seq
	d.mu.Unlock()
	if err := d.flushDamage(width, height, img, rects); err != nil {
		return err
	}
	d.mu.Lock()
	published := d.seq != before
	d.mu.Unlock()
	if published {
		d.textDisabled = true
		d.legacyFallbackDisabled = true
	}

	return nil
}

// SetCursor snapshots hardware cursor shapes independently of desktop frames.
// X/Y are the image's top-left CRTC position; the pointer is at X/Y + hotspot.
func (d *RDPDisplay) SetCursor(cursor DisplayCursor) error {
	if cursor.Image != nil && (!damage.ValidRGBA(cursor.Image) ||
		cursor.Image.Rect.Dx() > 256 || cursor.Image.Rect.Dy() > 256 ||
		cursor.HotX < 0 || cursor.HotY < 0 ||
		cursor.HotX >= cursor.Image.Rect.Dx() || cursor.HotY >= cursor.Image.Rect.Dy()) {
		return errInvalidFramebuffer
	}
	d.cursorMu.Lock()
	defer d.cursorMu.Unlock()
	c := &d.cursor
	shapeChanged := !sameCursorImage(c.image, cursor.Image) || c.hotX != cursor.HotX || c.hotY != cursor.HotY
	x, y := cursor.X+cursor.HotX, cursor.Y+cursor.HotY
	if c.serial != 0 && !shapeChanged && c.x == x && c.y == y {
		return nil
	}
	if shapeChanged || c.serial == 0 {
		c.image = cloneCursorImage(cursor.Image)
		c.hotX, c.hotY = cursor.HotX, cursor.HotY
		c.shapeSerial++
	}
	if c.serial == 0 || c.x != x || c.y != y {
		c.positionSerial++
		c.echo = d.consumePointerMotionLocked(x, y)
	}
	c.x, c.y = x, y
	c.serial++
	if c.changed != nil {
		close(c.changed)
	}
	c.changed = make(chan struct{})

	return nil
}

func sameCursorImage(left, right *image.RGBA) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if left.Rect.Size() != right.Rect.Size() {
		return false
	}
	width := left.Rect.Dx() * 4
	for y := 0; y < left.Rect.Dy(); y++ {
		if !bytes.Equal(left.Pix[y*left.Stride:y*left.Stride+width], right.Pix[y*right.Stride:y*right.Stride+width]) {
			return false
		}
	}

	return true
}

func cloneCursorImage(src *image.RGBA) *image.RGBA {
	if src == nil {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, src.Rect.Dx(), src.Rect.Dy()))
	for y := 0; y < dst.Rect.Dy(); y++ {
		copy(dst.Pix[y*dst.Stride:(y+1)*dst.Stride], src.Pix[y*src.Stride:y*src.Stride+dst.Stride])
	}

	return dst
}

func (d *RDPDisplay) cursorSnapshot() (rdpCursorState, <-chan struct{}) {
	d.cursorMu.Lock()
	defer d.cursorMu.Unlock()
	if d.cursor.changed == nil {
		d.cursor.changed = make(chan struct{})
	}

	return d.cursor, d.cursor.changed
}

func (d *RDPDisplay) rememberPointerMotion(session *rdp.Session, x, y int) {
	d.cursorMu.Lock()
	defer d.cursorMu.Unlock()
	if len(d.motions) > 0 {
		last := d.motions[len(d.motions)-1]
		if last.x == x && last.y == y {
			return
		}
	} else if d.cursor.serial != 0 && d.cursor.x == x && d.cursor.y == y {
		// Button/wheel events at the same point do not cause a hardware cursor
		// move; retaining them would mistake a later warp for delayed input.
		return
	}
	const maxPendingMotions = 256
	if len(d.motions) == maxPendingMotions {
		copy(d.motions, d.motions[1:])
		d.motions = d.motions[:maxPendingMotions-1]
	}
	d.motions = append(d.motions, rdpPointerMotion{session: session, x: x, y: y, when: time.Now()})
}

// Do not send a delayed guest echo back to the client moving its local cursor.
// Consuming the matched history still allows subsequent guest warps to that
// same position to be sent.
func (d *RDPDisplay) consumePointerMotionLocked(x, y int) *rdp.Session {
	for i := len(d.motions) - 1; i >= 0; i-- {
		motion := d.motions[i]
		if time.Since(motion.when) > 2*time.Second {
			break
		}
		if x >= motion.x-1 && x <= motion.x+1 && y >= motion.y-1 && y <= motion.y+1 {
			copy(d.motions, d.motions[i+1:])
			remaining := len(d.motions) - i - 1
			clear(d.motions[remaining:])
			d.motions = d.motions[:remaining]

			return motion.session
		}
	}

	return nil
}

func (w *rdpFrameWriter) updatePointer(cursor rdpCursorState, guestWidth, guestHeight int) error {
	return w.pointer.update(w.session, w.session,
		cursor, guestWidth, guestHeight, w.width, w.height)
}

func (p *rdpPointerWriter) update(
	sink rdpPointerSink, owner *rdp.Session, cursor rdpCursorState, guestWidth, guestHeight, width, height int,
) error {
	if cursor.serial == 0 || guestWidth <= 0 || guestHeight <= 0 {
		return nil
	}
	geometryChanged := p.guestWidth != guestWidth || p.guestHeight != guestHeight
	if cursor.serial == p.serial && !geometryChanged {
		return nil
	}
	if cursor.shapeSerial != p.shapeSerial || geometryChanged {
		shape, hotX, hotY := scaleRDPCursor(cursor, guestWidth, guestHeight, width, height)
		if err := sink.WritePointer(shape, hotX, hotY); err != nil {
			return err
		}
	}
	positionChanged := cursor.positionSerial != p.positionSerial
	becameVisible := cursor.image != nil && !p.visible
	if cursor.image != nil && (positionChanged || geometryChanged || becameVisible) &&
		(cursor.echo != owner || p.serial == 0 || geometryChanged) {
		x := min(max(cursor.x, 0)*width/guestWidth, width-1)
		y := min(max(cursor.y, 0)*height/guestHeight, height-1)
		if err := sink.WritePointerPosition(x, y); err != nil {
			return err
		}
	}
	*p = rdpPointerWriter{
		serial: cursor.serial, shapeSerial: cursor.shapeSerial, positionSerial: cursor.positionSerial,
		guestWidth: guestWidth, guestHeight: guestHeight,
		visible: cursor.image != nil,
	}

	return nil
}

func (d *RDPDisplay) forgetPointerMotions(session *rdp.Session) {
	d.cursorMu.Lock()
	defer d.cursorMu.Unlock()
	kept := 0
	for _, motion := range d.motions {
		if motion.session != session {
			d.motions[kept] = motion
			kept++
		}
	}
	clear(d.motions[kept:])
	d.motions = d.motions[:kept]
	if d.cursor.echo == session {
		d.cursor.echo = nil
	}
}

func scaleRDPCursor(cursor rdpCursorState, guestWidth, guestHeight, width, height int) (*image.RGBA, int, int) {
	if cursor.image == nil {
		return nil, 0, 0
	}
	src := cursor.image
	if guestWidth == width && guestHeight == height {
		return src, cursor.hotX, cursor.hotY
	}
	// Bound temporary storage even for a tiny guest displayed at a large size.
	w := max(1, min(256, (src.Rect.Dx()*width+guestWidth-1)/guestWidth))
	h := max(1, min(256, (src.Rect.Dy()*height+guestHeight-1)/guestHeight))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			from := y*src.Rect.Dy()/h*src.Stride + x*src.Rect.Dx()/w*4
			to := y*dst.Stride + x*4
			copy(dst.Pix[to:to+4], src.Pix[from:from+4])
		}
	}

	return dst, min(w-1, cursor.hotX*w/src.Rect.Dx()), min(h-1, cursor.hotY*h/src.Rect.Dy())
}
