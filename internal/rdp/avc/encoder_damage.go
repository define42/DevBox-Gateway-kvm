package avc

import (
	"image"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/damage"
)

// The optional extension keeps hardware backends on their existing full-picture
// API. Hints never change AVC dimensions, surface placement or the reference chain.
type damageCodecEncoder interface {
	encodeDamage([]byte, bool, int64, []bool) ([]byte, error)
}

func (e *Encoder) encodeWithDamageHints(
	source image.Rectangle, regions []image.Rectangle, fullConversion, force bool,
) ([]byte, error) {
	timestamp := e.frame * 1000 / int64(e.frameRate)
	if codec, ok := e.codec.(damageCodecEncoder); ok && !fullConversion && !force {
		return codec.encodeDamage(e.i420, false, timestamp, e.unchangedMacroblocks(source, regions))
	}

	return e.codec.encode(e.i420, force, timestamp)
}

// Every true bit certifies all Y/U/V samples in the corresponding 16x16 output
// block are unchanged. Expanding to macroblocks also covers the 2x2 chroma halo.
// Codec-owned padding in the last partial block is covered by the same bit.
// The mask is borrowed synchronously and reused; no allocation is needed after
// the first partial update. New streams, full conversions and retries omit it.
func (e *Encoder) unchangedMacroblocks(source image.Rectangle, regions []image.Rectangle) []bool {
	columns, rows := (e.width+15)/16, (e.height+15)/16
	if len(e.unchanged) != columns*rows {
		e.unchanged = make([]bool, columns*rows)
	}
	for i := range e.unchanged {
		e.unchanged[i] = true
	}
	output := image.Rect(0, 0, e.width, e.height)
	for _, region := range regions {
		mapped := damage.Align(damage.Map(region, source, output), 16, output)
		if mapped.Empty() {
			continue
		}
		for row := mapped.Min.Y / 16; row < (mapped.Max.Y+15)/16; row++ {
			clear(e.unchanged[row*columns+mapped.Min.X/16 : row*columns+(mapped.Max.X+15)/16])
		}
	}

	return e.unchanged
}
