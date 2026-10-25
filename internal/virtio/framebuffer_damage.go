package virtio

import "image"

const (
	framebufferTileSize = 64
	maxFrameDamageRects = 128
)

// flushDamage is called with textMu held. The display owns one persistent
// image; readers consume changed tiles while holding the frame lock.
func (d *framebuffer) flushDamage(width, height int, img *image.RGBA, damage []image.Rectangle) error {
	if img == nil {
		if width <= 0 || height <= 0 {
			return nil
		}

		return errInvalidFramebuffer
	}

	return d.flushPixels(width, height, img.Pix, img.Stride, img.Rect, false, damage)
}

// flushPixels accepts either RGBA or BGRX rows, borrowing them only for this
// call. Format changes preserve pixels outside the flushed rectangles.
// The caller holds textMu; mu protects publication and synchronous conversion.
func (d *framebuffer) flushPixels(
	width, height int, pix []byte, stride int, source image.Rectangle, bgrx bool, damage []image.Rectangle,
) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	const maxInt = int(^uint(0) >> 1)
	if width > maxInt/4 || height > maxInt/(width*4) ||
		width > source.Dx() || height > source.Dy() {
		return errInvalidFramebuffer
	}
	rowBytes := width * 4
	if stride < rowBytes || len(pix) < rowBytes ||
		height-1 > (len(pix)-rowBytes)/stride {
		return errInvalidFramebuffer
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	bounds := image.Rect(0, 0, width, height)
	full := damage == nil || d.width != width || d.height != height || len(d.frame) != width*height*4
	if full {
		damage = []image.Rectangle{bounds}
	}
	anyDamage := false
	for _, rect := range damage {
		if !rect.Intersect(bounds).Empty() {
			anyDamage = true

			break
		}
	}
	if !anyDamage {
		return nil
	}
	if len(d.frame) != width*height*4 {
		d.frame = make([]byte, width*height*4)
	} else if d.frameShared {
		// Legacy immutable snapshots may still be in use by another reader.
		d.frame = append([]byte(nil), d.frame...)
	}
	d.frameShared = false
	if !full && d.frameBGRX != bgrx {
		// Convert the retained publication, never unflushed input pixels.
		for i := 0; i < len(d.frame); i += 4 {
			d.frame[i], d.frame[i+2], d.frame[i+3] = d.frame[i+2], d.frame[i], 255
		}
	}
	d.frameBGRX = bgrx
	d.width, d.height = width, height
	for _, rect := range damage {
		rect = rect.Intersect(bounds)
		for y := rect.Min.Y; y < rect.Max.Y && !rect.Empty(); y++ {
			src := y*stride + rect.Min.X*4
			dst := (y*width + rect.Min.X) * 4
			copy(d.frame[dst:dst+rect.Dx()*4], pix[src:src+rect.Dx()*4])
		}
	}
	d.seq++
	if full {
		d.markFrameDamageLocked(nil)
	} else {
		d.markFrameDamageLocked(damage)
	}
	d.publishInputGeometry(width, height)
	close(d.changed)
	d.changed = make(chan struct{})

	return nil
}

// Versions, rather than a queue of old rectangles, let every client catch up
// independently after pacing, acknowledgments, minimization or slow writes.
func (d *framebuffer) markFrameDamageLocked(damage []image.Rectangle) {
	cols := (d.width + framebufferTileSize - 1) / framebufferTileSize
	rows := (d.height + framebufferTileSize - 1) / framebufferTileSize
	if len(d.tileVersions) != cols*rows {
		d.tileVersions = make([]uint64, cols*rows)
		damage = nil
	}
	if damage == nil {
		for i := range d.tileVersions {
			d.tileVersions[i] = d.seq
		}

		return
	}
	bounds := image.Rect(0, 0, d.width, d.height)
	for _, rect := range damage {
		rect = rect.Intersect(bounds)
		if rect.Empty() {
			continue
		}
		for y := rect.Min.Y / framebufferTileSize; y <= (rect.Max.Y-1)/framebufferTileSize; y++ {
			for x := rect.Min.X / framebufferTileSize; x <= (rect.Max.X-1)/framebufferTileSize; x++ {
				d.tileVersions[y*cols+x] = d.seq
			}
		}
	}
}

func (d *framebuffer) frameChanged(sequence uint64, force bool) (bool, <-chan struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return force || d.seq != sequence, d.changed
}

// copyFrameChanges refreshes a reader-owned image. No borrowed framebuffer
// pixels escape the lock, and no pixel copy is needed for cursor-only updates.
func (d *framebuffer) copyFrameChanges(frame *displayFrame, force bool) []image.Rectangle {
	d.mu.Lock()
	defer d.mu.Unlock()
	bounds := image.Rect(0, 0, d.width, d.height)
	full := force || frame.width != d.width || frame.height != d.height || len(frame.pix) != d.width*d.height*4
	if len(frame.pix) != d.width*d.height*4 {
		frame.pix = make([]byte, d.width*d.height*4)
	}
	var damage []image.Rectangle
	if full || len(d.tileVersions) == 0 {
		damage = []image.Rectangle{bounds}
	} else {
		damage = d.changedTileRectsLocked(frame.seq)
	}
	for _, rect := range damage {
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			start := (y*d.width + rect.Min.X) * 4
			end := start + rect.Dx()*4
			if end <= len(d.frame) {
				if d.frameBGRX {
					bgrToRGBARow(frame.pix[start:end], d.frame[start:end], 0xff000000)
				} else {
					copy(frame.pix[start:end], d.frame[start:end])
				}
			} else {
				clear(frame.pix[start:end])
			}
		}
	}
	frame.width, frame.height, frame.seq = d.width, d.height, d.seq
	if damage == nil {
		return []image.Rectangle{}
	}

	return damage
}

func (d *framebuffer) changedTileRectsLocked(sequence uint64) []image.Rectangle {
	cols := (d.width + framebufferTileSize - 1) / framebufferTileSize
	rows := (d.height + framebufferTileSize - 1) / framebufferTileSize
	var rects []image.Rectangle
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; {
			if d.tileVersions[y*cols+x] <= sequence {
				x++

				continue
			}
			start := x
			for x < cols && d.tileVersions[y*cols+x] > sequence {
				x++
			}
			rect := image.Rect(start*framebufferTileSize, y*framebufferTileSize,
				min(x*framebufferTileSize, d.width), min((y+1)*framebufferTileSize, d.height))
			// Common scrolling areas collapse into one rectangle across rows.
			merged := false
			for i := len(rects) - 1; i >= 0; i-- {
				if rects[i].Max.Y == rect.Min.Y && rects[i].Min.X == rect.Min.X && rects[i].Max.X == rect.Max.X {
					rects[i].Max.Y = rect.Max.Y
					merged = true

					break
				}
			}
			if !merged {
				rects = append(rects, rect)
				if len(rects) > maxFrameDamageRects {
					return []image.Rectangle{image.Rect(0, 0, d.width, d.height)}
				}
			}
		}
	}

	return rects
}

// readFrameChanges borrows the publication only until consume returns. A video
// reader converts directly into its own I420 storage here, then encodes after
// releasing mu. The callback boolean identifies BGRX bytes; the image descriptor
// supplies geometry in either format. Neither pixels nor the descriptor may escape consume.
// Frame metadata advances only after successful consumption, so failed readers
// and independently paced viewers retain all pending damage.
func (d *framebuffer) readFrameChanges(
	frame *displayFrame, force bool, consume func(*image.RGBA, bool, []image.Rectangle) error,
) ([]image.Rectangle, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	bounds := image.Rect(0, 0, d.width, d.height)
	var damage []image.Rectangle
	if force || frame.width != d.width || frame.height != d.height || len(d.tileVersions) == 0 {
		damage = []image.Rectangle{bounds}
	} else {
		damage = d.changedTileRectsLocked(frame.seq)
	}
	if damage == nil {
		damage = []image.Rectangle{}
	}
	// Before the first guest publication, expose the same black image as the
	// bitmap path without allocating a private RGB image per viewer.
	if len(d.frame) == 0 {
		d.frame = make([]byte, d.width*d.height*4)
	}
	img := &image.RGBA{Pix: d.frame, Stride: d.width * 4, Rect: bounds}
	if err := consume(img, d.frameBGRX, damage); err != nil {
		return nil, err
	}
	*frame = displayFrame{width: d.width, height: d.height, seq: d.seq}

	return damage, nil
}
