package virtio

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"sync"
)

// Display is the sink virtio-gpu presents flushed frames to. Implementations
// must be safe for the single GPU IO goroutine to call.
type Display interface {
	// Flush publishes the scanout image, including any hardware cursor unless
	// the display also implements CursorDisplay.
	// The image is owned by the caller only for the duration of the call.
	Flush(width, height int, img *image.RGBA) error

	// Close releases any resources held by the display.
	Close() error
}

// DamageDisplay accepts changed regions in scanout coordinates. Pixels and
// rectangles are borrowed only for the call; a nil damage slice means that the
// entire image changed. An empty, non-nil slice means no pixels changed.
type DamageDisplay interface {
	FlushDamage(width, height int, img *image.RGBA, damage []image.Rectangle) error
}

// GraphicsResetDisplay releases graphics ownership when the guest resets its
// GPU, allowing serial console fallbacks to resume. The notification follows
// scanout removal and cursor hiding; it does not require clearing the last image.
// Calls are serialized with frame/cursor publication and must not call into the GPU.
type GraphicsResetDisplay interface {
	GraphicsReset()
}

// DisplayCursor describes a guest hardware cursor. X and Y are the image's
// top-left CRTC position, which may be negative. The pointer position is
// (X+HotX, Y+HotY). Image is borrowed for the call; nil hides the pointer.
type DisplayCursor struct {
	Image            *image.RGBA
	X, Y, HotX, HotY int
}

// CursorDisplay receives the cursor separately. Implementations of this
// interface receive cursor-free desktop pixels through Flush or FlushDamage.
type CursorDisplay interface {
	SetCursor(cursor DisplayCursor) error
}

// PNGDisplay writes each flushed frame to a PNG file, replacing it in place.
// It is dependency-free (stdlib image/png) and serves as the default backend.
type PNGDisplay struct {
	path string
	mu   sync.Mutex
}

// NewPNGDisplay returns a Display that writes frames to path.
func NewPNGDisplay(path string) *PNGDisplay {
	return &PNGDisplay{path: path}
}

// Flush encodes img to a temporary file and atomically renames it over the
// target, so a reader never observes a half-written PNG.
func (d *PNGDisplay) Flush(width, height int, img *image.RGBA) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tmp := d.path + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	if err := png.Encode(f, img); err != nil {
		f.Close()

		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(tmp, d.path)
}

func (d *PNGDisplay) Close() error { return nil }

// MultiDisplay fans out each flushed frame to multiple displays.
type MultiDisplay struct {
	displays  []Display
	legacy    bool
	frame     *image.RGBA // Owned, cursor-free desktop for legacy displays.
	composite *image.RGBA
	cursor    DisplayCursor
}

// NewMultiDisplay returns a Display that flushes to each provided display.
func NewMultiDisplay(displays ...Display) *MultiDisplay {
	d := &MultiDisplay{displays: displays}
	for _, display := range displays {
		if _, separate := display.(CursorDisplay); !separate {
			d.legacy = true

			break
		}
	}

	return d
}

// GraphicsReset releases cached scanouts and notifies capable child displays.
// Visible frames remain owned by the children until fresh output replaces them.
func (d *MultiDisplay) GraphicsReset() {
	d.frame, d.composite = nil, nil
	d.cursor = DisplayCursor{}
	for _, display := range d.displays {
		if reset, ok := display.(GraphicsResetDisplay); ok {
			reset.GraphicsReset()
		}
	}
}

func (d *MultiDisplay) Flush(width, height int, img *image.RGBA) error {
	return d.FlushDamage(width, height, img, nil)
}

func (d *MultiDisplay) FlushDamage(width, height int, img *image.RGBA, damage []image.Rectangle) error {
	if d.legacy {
		if d.frame == nil || d.frame.Bounds() != img.Bounds() {
			d.frame = image.NewRGBA(img.Bounds())
			damage = nil
		}
		if damage == nil {
			draw.Draw(d.frame, d.frame.Bounds(), img, img.Bounds().Min, draw.Src)
		} else {
			for _, rect := range damage {
				rect = rect.Intersect(img.Bounds())
				draw.Draw(d.frame, rect, img, rect.Min, draw.Src)
			}
		}
	}
	var composed *image.RGBA
	for _, display := range d.displays {
		var err error
		if _, separate := display.(CursorDisplay); separate {
			if partial, ok := display.(DamageDisplay); ok {
				err = partial.FlushDamage(width, height, img, damage)
			} else {
				err = display.Flush(width, height, img)
			}
		} else {
			if composed == nil {
				composed = d.compose()
			}
			err = display.Flush(width, height, composed)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *MultiDisplay) SetCursor(cursor DisplayCursor) error {
	if d.legacy {
		shape := d.cursor.Image
		d.cursor = cursor
		if cursor.Image != nil {
			if shape == nil || shape.Bounds() != cursor.Image.Bounds() {
				shape = image.NewRGBA(cursor.Image.Bounds())
			}
			draw.Draw(shape, cursor.Image.Bounds(), cursor.Image, cursor.Image.Bounds().Min, draw.Src)
			d.cursor.Image = shape
		}
	}
	var composed *image.RGBA
	for _, display := range d.displays {
		var err error
		if separate, ok := display.(CursorDisplay); ok {
			err = separate.SetCursor(cursor)
		} else if d.frame != nil {
			if composed == nil {
				composed = d.compose()
			}
			err = display.Flush(composed.Bounds().Dx(), composed.Bounds().Dy(), composed)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *MultiDisplay) compose() *image.RGBA {
	return compositeCursor(d.frame, &d.composite, d.cursor)
}

// compositeCursor never modifies the raw desktop, so moving or hiding the
// cursor restores the last flushed pixels rather than unflushed guest writes.
func compositeCursor(frame *image.RGBA, storage **image.RGBA, cursor DisplayCursor) *image.RGBA {
	if cursor.Image == nil {
		return frame
	}
	rect := cursor.Image.Bounds().Add(image.Pt(cursor.X, cursor.Y).Sub(cursor.Image.Bounds().Min))
	if !rect.Overlaps(frame.Bounds()) {
		return frame
	}
	if *storage == nil || (*storage).Bounds() != frame.Bounds() {
		*storage = image.NewRGBA(frame.Bounds())
	}
	img := *storage
	draw.Draw(img, img.Bounds(), frame, frame.Bounds().Min, draw.Src)
	// DRM cursor pixels use premultiplied alpha, matching image.RGBA.
	draw.Draw(img, rect, cursor.Image, cursor.Image.Bounds().Min, draw.Over)

	return img
}

// SetResizeHandler connects remote display requests to the shared guest GPU.
func (d *MultiDisplay) SetResizeHandler(resize func(width, height int) error) {
	for _, display := range d.displays {
		if resizable, ok := display.(interface{ SetResizeHandler(func(int, int) error) }); ok {
			resizable.SetResizeHandler(resize)
		}
	}
}

func (d *MultiDisplay) Close() error {
	var firstErr error

	for _, display := range d.displays {
		if err := display.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
