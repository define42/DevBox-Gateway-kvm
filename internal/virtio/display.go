package virtio

import (
	"image"
	"image/draw"
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

// BGRXDisplay accepts a cursor-free scanout directly from a 2D GPU resource.
// Pixels have blue, green, red and ignored bytes, with stride bytes between
// rows. The first pixel is already offset to the scanout viewport. Damage is
// viewport-relative; nil means the full image and an empty slice means none.
// Only damaged pixels may be published: other resource pixels can contain
// transfers that the guest has not flushed. Pixels and damage are borrowed
// only for the call, so implementations must retain their own published copy.
// The GPU uses this interface only when the sink also implements CursorDisplay.
type BGRXDisplay interface {
	FlushBGRX(width, height, stride int, pix []byte, damage []image.Rectangle) error
}

// GraphicsResetDisplay releases graphics ownership when the guest resets its
// GPU. The notification follows scanout removal and cursor hiding; it does not
// require clearing the last image.
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
// interface receive cursor-free desktop pixels through Flush, FlushDamage or
// FlushBGRX.
type CursorDisplay interface {
	SetCursor(cursor DisplayCursor) error
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
