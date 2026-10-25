//go:build integration

package rdp

import (
	"image"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/avc"
)

const (
	graphicsACKWidth  = 1280
	graphicsACKHeight = 720
)

type graphicsACKFixture struct {
	encoder *avc.Encoder
	frames  [2]*image.RGBA
	damage  []image.Rectangle
}

// graphicsACKFrame holds the encoded access unit and its repaint regions.
type graphicsACKFrame struct {
	data    []byte
	regions []image.Rectangle
}

func newGraphicsACKFixture(tb testing.TB, workload string, sliceCount int) *graphicsACKFixture {
	tb.Helper()
	var fixture graphicsACKFixture
	switch workload {
	case "Scroll":
		fixture.frames, fixture.damage = graphicsACKScrollFrames()
	case "WindowDrag":
		fixture.frames, fixture.damage = graphicsACKWindowFrames()
	default:
		tb.Fatalf("unknown graphics ACK workload %q", workload)

		return nil
	}
	encoder, err := avc.NewEncoderWithOptions(graphicsACKWidth, graphicsACKHeight, avc.Options{
		Threads: sliceCount,
	})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(encoder.Close)
	if got := encoder.Threads(); got != sliceCount {
		tb.Fatalf("effective H.264 slices = %d, requested %d", got, sliceCount)
	}
	fixture.encoder = encoder

	return &fixture
}

func (f *graphicsACKFixture) encode(tb testing.TB, step int) graphicsACKFrame {
	tb.Helper()
	force := step == 0
	data, err := f.encoder.EncodeDamage(f.frames[step%len(f.frames)], f.damage, force)
	if err != nil || len(data) == 0 {
		tb.Fatalf("graphics ACK frame %d: bytes=%d, error=%v", step, len(data), err)
	}
	frame := graphicsACKFrame{data: data, regions: f.damage}
	if force {
		frame.regions = nil
	}

	return frame
}

func graphicsACKScrollFrames() ([2]*image.RGBA, []image.Rectangle) {
	pane := image.Rect(128, 64, 1152, 656)
	var frames [2]*image.RGBA
	for frame := range frames {
		img := image.NewRGBA(image.Rect(0, 0, graphicsACKWidth, graphicsACKHeight))
		for y := range graphicsACKHeight {
			for x := range graphicsACKWidth {
				value := byte(40 + (y/4+x/8)%70)
				if image.Pt(x, y).In(pane) && x >= pane.Min.X+64 && x < pane.Max.X-64 {
					value = graphicsACKTextGray(x-pane.Min.X-64, y-pane.Min.Y+frame*18, pane.Dx()-128)
				}
				pixel := img.Pix[img.PixOffset(x, y):][:4]
				pixel[0], pixel[1], pixel[2], pixel[3] = value, value, value, 255
			}
		}
		frames[frame] = img
	}

	return frames, []image.Rectangle{pane}
}

// Draw sparse glyph strokes, word gaps, and varying line lengths. The
// grayscale two-pixel strokes compress like desktop text.
func graphicsACKTextGray(x, y, width int) byte {
	const background = 244
	line, row := y/24, y%24
	if row < 4 || row >= 18 || x%8 >= 5 || x%80 >= 72 || x >= width-48-(line*37)%128 {
		return background
	}
	glyph := uint32(x/8)*0x9e3779b9 ^ uint32(line)*0x85ebca6b
	glyph ^= glyph >> 16
	glyph *= 0xc2b2ae35
	glyph ^= glyph >> 13
	if x%8 != 0 && glyph&(1<<((row-4)/2)) == 0 {
		return background
	}

	return byte(48 + line%4*6)
}

func graphicsACKWindowFrames() ([2]*image.RGBA, []image.Rectangle) {
	first := image.Rect(224, 64, 1024, 664)
	second := first.Add(image.Pt(18, 0))

	return [2]*image.RGBA{graphicsACKWindowFrame(first), graphicsACKWindowFrame(second)},
		[]image.Rectangle{first.Union(second)}
}

// The opaque window combines repeated text, whitespace, and a colored gradient.
// Moving it horizontally exercises desktop window dragging.
func graphicsACKWindowFrame(window image.Rectangle) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, graphicsACKWidth, graphicsACKHeight))
	for y := range graphicsACKHeight {
		for x := range graphicsACKWidth {
			r, g, blue := byte(34+x/64%10), byte(45+y/64%10), byte(55+(x+y)/64%10)
			if image.Pt(x, y).In(window) {
				xp, yp := x-window.Min.X, y-window.Min.Y
				r, g, blue = 240, 242, 245
				switch {
				case xp < 3 || yp < 3 || xp >= 797 || yp >= 597:
					r, g, blue = 20, 25, 35
				case yp < 32:
					r, g, blue = 60, 80, 140
				case xp >= 560 && yp >= 300:
					r, g, blue = byte(xp/3), byte(yp/3), byte((xp+yp)/5)
				case yp%24 < 10 && xp%16 < 11 && xp%480 < 420-yp%120:
					r, g, blue = 55, 60, 65
				}
			}
			pixel := img.Pix[img.PixOffset(x, y):][:4]
			pixel[0], pixel[1], pixel[2], pixel[3] = r, g, blue, 255
		}
	}

	return img
}
