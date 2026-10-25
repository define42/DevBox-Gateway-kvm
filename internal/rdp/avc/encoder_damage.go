package avc

import (
	"image"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/damage"
)

// Damage hints let the codec reuse unchanged macroblocks without changing AVC
// dimensions, surface placement, or the reference chain.
type damageCodecEncoder interface {
	encodeDamage(i420 []byte, force bool, timestamp int64, unchanged []bool) ([]byte, error)
}

func (e *Encoder) encodeWithDamageHints() ([]byte, error) {
	timestamp := e.frame * 1000 / int64(e.frameRate)
	if codec, ok := e.codec.(damageCodecEncoder); ok && e.preparedHints {
		return codec.encodeDamage(e.i420, false, timestamp, e.unchanged)
	}

	return e.codec.encode(e.i420, e.preparedForce, timestamp)
}

// convertDamageWithHints certifies source I420 equality for each 16x16 block,
// including unchanged pixels inside conservative damage rectangles. All planes
// are compared before they are overwritten; overlapping regions only clear bits.
// Full conversions and retries use the ordinary path without these hints.
func (e *Encoder) convertDamageWithHints(img *image.RGBA, regions []image.Rectangle, bgrx bool) bool {
	blocks := ((e.width + 15) / 16) * ((e.height + 15) / 16)
	if len(e.unchanged) != blocks {
		e.unchanged = make([]bool, blocks)
	}
	for i := range e.unchanged {
		e.unchanged[i] = true
	}
	source := i420DamageSource{pix: img.Pix, stride: img.Stride, width: img.Rect.Dx(), height: img.Rect.Dy()}
	if bgrx {
		source.redIndex = 2
	}
	output := image.Rect(0, 0, e.width, e.height)
	changed := false
	for _, region := range regions {
		mapped := damage.Align(damage.Map(region, img.Rect, output), 2, output)
		changed = e.convertI420DamageRegion(source, mapped) || changed
	}

	return changed
}
