package avc

import (
	"image"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/damage"
)

// Damage hints let the codec reuse unchanged macroblocks without changing AVC
// dimensions, surface placement, or the reference chain.
type damageCodecEncoder interface {
	encodeDamage(i420 []byte, force bool, timestamp int64, unchanged, forced []bool, shift []int16) ([]byte, error)
}

func (e *Encoder) encodeWithDamageHints() ([]byte, error) {
	timestamp := e.frame * 1000 / int64(e.frameRate)
	if codec, ok := e.codec.(damageCodecEncoder); ok && e.preparedHints {
		return codec.encodeDamage(e.i420, false, timestamp, e.unchanged, e.preparedForced, e.preparedShift)
	}

	return e.codec.encode(e.i420, e.preparedForce, timestamp)
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
