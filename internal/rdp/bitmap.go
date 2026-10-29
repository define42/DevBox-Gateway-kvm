package rdp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/damage"
)

var ErrBitmapFormat = errors.New("unsupported RDP bitmap geometry or color depth")

const bitmapTileSize = 64

// BitmapEncoder scales the console into a fixed session desktop and sends only
// changed tiles. Each update contains one uncompressed, bottom-up bitmap, as
// specified by MS-RDPBCGR 2.2.9.1.1.3.1.2. Large tiles are sent as row strips
// that keep the complete MCS Send Data PDU within its 16,383-byte limit.
type BitmapEncoder struct {
	width, height, bytesPerPixel int
	pixels, previous             []byte
	source                       image.Rectangle
	pending                      []bool
	refresh                      bool
}

func NewBitmapEncoder(width, height, bitsPerPixel int) (*BitmapEncoder, error) {
	if width <= 0 || height <= 0 || width > 4096 || height > 4096 ||
		(bitsPerPixel != 16 && bitsPerPixel != 24 && bitsPerPixel != 32) {
		return nil, ErrBitmapFormat
	}

	return &BitmapEncoder{width: width, height: height, bytesPerPixel: bitsPerPixel / 8}, nil
}

// WriteFrame calls send with TS_UPDATE_BITMAP_DATA payloads (including the
// updateType field). A failed send retains all dirty tiles, so a retry sends
// every tile that might not have reached the client.
func (e *BitmapEncoder) WriteFrame(img *image.RGBA, force bool, send func([]byte) error) error {
	return e.WriteFrameDamage(img, nil, force, send)
}

// WriteFrameDamage converts only changed source regions into a retained packed
// image. Nil damage means a full update, while an empty, nonnil slice adds no new
// damage. Initial frames, source geometry changes, and forced refreshes convert
// the entire image. Callers must include changes from any skipped source frames.
func (e *BitmapEncoder) WriteFrameDamage(
	img *image.RGBA, regions []image.Rectangle, force bool, send func([]byte) error,
) error {
	if img == nil || img.Bounds().Empty() {
		return nil
	}
	if !damage.ValidRGBA(img) {
		return ErrBitmapFormat
	}

	output := image.Rect(0, 0, e.width, e.height)
	if e.pixels == nil {
		e.pixels = make([]byte, e.width*e.height*e.bytesPerPixel)
		e.previous = make([]byte, len(e.pixels))
		e.pending = make([]bool, e.columns()*((e.height+bitmapTileSize-1)/bitmapTileSize))
		e.refresh = true
	}
	if force || e.source != img.Rect {
		e.refresh = true
	}
	if regions == nil || e.refresh {
		e.scaleRegion(img, output)
		e.markPending(output)
	} else {
		for _, region := range regions {
			mapped := damage.Map(region, img.Rect, output)
			e.scaleRegion(img, mapped)
			e.markPending(mapped)
		}
	}
	e.source = img.Rect

	for index, pending := range e.pending {
		if !pending {
			continue
		}
		left, top, width, height := e.tileBounds(index)
		if !e.refresh && e.unchanged(e.pixels, left, top, width, height) {
			continue
		}
		if err := e.sendTile(left, top, width, height, send); err != nil {
			return err
		}
	}

	// Commit only after every send succeeds; failed writes preserve all pending
	// changes even if the caller supplies different or empty damage on retry.
	for index, pending := range e.pending {
		if !pending {
			continue
		}
		left, top, width, height := e.tileBounds(index)
		for y := top; y < top+height; y++ {
			start := (y*e.width + left) * e.bytesPerPixel
			end := start + width*e.bytesPerPixel
			copy(e.previous[start:end], e.pixels[start:end])
		}
		e.pending[index] = false
	}
	e.refresh = false

	return nil
}

func (e *BitmapEncoder) sendTile(left, top, width, height int, send func([]byte) error) error {
	stride := (width*e.bytesPerPixel + 3) &^ 3
	// Include the Share Data (18 bytes) and bitmap update (22 bytes)
	// headers, in addition to the MCS header reserved by maxUserData.
	rows := (maxUserData - 18 - 22) / stride
	for offset := 0; offset < height; offset += rows {
		if err := send(e.tile(e.pixels, left, top+offset, width, min(rows, height-offset))); err != nil {
			return err
		}
	}

	return nil
}

func (e *BitmapEncoder) columns() int { return (e.width + bitmapTileSize - 1) / bitmapTileSize }

func (e *BitmapEncoder) tileBounds(index int) (left, top, width, height int) {
	left, top = index%e.columns()*bitmapTileSize, index/e.columns()*bitmapTileSize

	return left, top, min(bitmapTileSize, e.width-left), min(bitmapTileSize, e.height-top)
}

func (e *BitmapEncoder) markPending(region image.Rectangle) {
	if region.Empty() {
		return
	}
	for y := region.Min.Y / bitmapTileSize; y <= (region.Max.Y-1)/bitmapTileSize; y++ {
		for x := region.Min.X / bitmapTileSize; x <= (region.Max.X-1)/bitmapTileSize; x++ {
			e.pending[y*e.columns()+x] = true
		}
	}
}

func (e *BitmapEncoder) scaleRegion(img *image.RGBA, region image.Rectangle) {
	if img.Rect.Dx() == e.width && img.Rect.Dy() == e.height {
		e.packRegion(img, region)

		return
	}
	e.scaleRegionNearest(img, region)
}

func (e *BitmapEncoder) scaleRegionNearest(img *image.RGBA, region image.Rectangle) {
	for y := region.Min.Y; y < region.Max.Y; y++ {
		srcY := y * img.Rect.Dy() / e.height
		for x := region.Min.X; x < region.Max.X; x++ {
			src := srcY*img.Stride + x*img.Rect.Dx()/e.width*4
			dst := (y*e.width + x) * e.bytesPerPixel
			r, g, b := img.Pix[src], img.Pix[src+1], img.Pix[src+2]
			if e.bytesPerPixel == 2 {
				value := uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
				binary.LittleEndian.PutUint16(e.pixels[dst:], value)
			} else {
				e.pixels[dst], e.pixels[dst+1], e.pixels[dst+2] = b, g, r
			}
		}
	}
}

// packRegion handles the common unscaled desktop without coordinate divisions
// per pixel. Pix already begins at Rect.Min, including for a padded subimage.
func (e *BitmapEncoder) packRegion(img *image.RGBA, region image.Rectangle) {
	if region.Empty() {
		return
	}
	width := region.Dx()
	for y := region.Min.Y; y < region.Max.Y; y++ {
		srcStart := y*img.Stride + region.Min.X*4
		dstStart := (y*e.width + region.Min.X) * e.bytesPerPixel
		src := img.Pix[srcStart : srcStart+width*4]
		dst := e.pixels[dstStart : dstStart+width*e.bytesPerPixel]
		switch e.bytesPerPixel {
		case 2:
			for s, d := 0, 0; s < len(src); s, d = s+4, d+2 {
				r, g, b := src[s], src[s+1], src[s+2]
				value := uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
				binary.LittleEndian.PutUint16(dst[d:], value)
			}
		case 3:
			for s, d := 0, 0; s < len(src); s, d = s+4, d+3 {
				dst[d], dst[d+1], dst[d+2] = src[s+2], src[s+1], src[s]
			}
		case 4:
			for offset := 0; offset < len(src); offset += 4 {
				// The fourth byte is reserved, not the source alpha channel.
				dst[offset], dst[offset+1], dst[offset+2] = src[offset+2], src[offset+1], src[offset]
			}
		}
	}
}

func (e *BitmapEncoder) unchanged(pixels []byte, left, top, width, height int) bool {
	if len(e.previous) != len(pixels) {
		return false
	}

	for y := top; y < top+height; y++ {
		start := (y*e.width + left) * e.bytesPerPixel
		end := start + width*e.bytesPerPixel
		if !bytes.Equal(pixels[start:end], e.previous[start:end]) {
			return false
		}
	}

	return true
}

func (e *BitmapEncoder) tile(pixels []byte, left, top, width, height int) []byte {
	stride := (width*e.bytesPerPixel + 3) &^ 3
	data := make([]byte, 22+stride*height)
	fields := []uint16{
		1, 1, // Bitmap update; one rectangle.
		uint16(left), uint16(top), uint16(left + width - 1), uint16(top + height - 1),
		uint16(width), uint16(height), uint16(e.bytesPerPixel * 8), 0, uint16(stride * height),
	}
	for i, field := range fields {
		binary.LittleEndian.PutUint16(data[i*2:], field)
	}

	for row := 0; row < height; row++ {
		src := ((top+height-1-row)*e.width + left) * e.bytesPerPixel
		copy(data[22+row*stride:], pixels[src:src+width*e.bytesPerPixel])
	}

	return data
}
