package avc

import (
	"image"
	"slices"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/damage"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

const (
	// MaxUpdateRegions and MaxCopies match the RDP AVC420 limits.
	MaxUpdateRegions = 128
	MaxCopies        = 16
)

// Copy moves the client's pixels in Source so that its top-left corner lands at
// Destination, in output coordinates; the two may overlap.
type Copy struct {
	Source      image.Rectangle
	Destination image.Point
}

// Update tells the caller how the client must present the frame that
// EncodePrepared returned, when Options.SurfaceCopies is set. Without Tracked,
// the caller repaints the frame's damage as usual. Otherwise it must perform
// Copies in order, repaint exactly Regions (nil repaints everything), and
// report the outcome with Sent. The slices are reused by the next preparation.
type Update struct {
	Tracked bool
	Regions []image.Rectangle
	Copies  []Copy
}

// surfaceTracker follows how the client's surface departs from the decoded
// picture. A copied macroblock is coded as its reference moved by the same
// shift, so the decoder moves its picture with the client. An even shift moves
// whole chroma rows and stays exact; an odd one leaves interpolated chroma in
// the decoder, while the client copied exact pixels. Such a stale block, or a
// copy of one, must be coded from its source before any repaint covers it.
type surfaceTracker struct {
	enabled  bool
	detector scrollDetector
	areas    []image.Rectangle // Reused detection areas.
	matches  []regionMatch
	stale    []bool
	staleSet int // true entries in stale
	previous []bool
	forced   []bool
	shift    []int16 // Vertical luma displacement of each forced block.
	painted  []bool
	columns  int
	update   Update
	planned  bool // The prepared frame's update has not been reported yet.
	full     bool // The prepared frame repaints everything.
}

func (t *surfaceTracker) begin() {
	t.matches = t.matches[:0]
	t.update = Update{Regions: t.update.Regions[:0], Copies: t.update.Copies[:0]}
	t.planned, t.full = false, false
}

// repaintAll plans a frame coded entirely from its source, such as a keyframe,
// which the client repaints completely, leaving nothing stale.
func (t *surfaceTracker) repaintAll() {
	t.planned, t.full = true, true
	t.update.Tracked, t.update.Regions = t.staleSet != 0, nil
}

// plan adjusts unchanged for a hinted frame and builds its update, returning
// the forced macroblocks and their shifts, or nil. damageRegions is the frame's
// converted damage in output coordinates.
func (t *surfaceTracker) plan(unchanged []bool, width, height int, damageRegions []image.Rectangle) ([]bool, []int16) {
	columns, rows := (width+15)/16, (height+15)/16
	count := columns * rows
	if len(t.stale) != count {
		t.stale, t.staleSet = make([]bool, count), 0
	}
	t.forced, t.painted = growBools(t.forced, count), growBools(t.painted, count)
	if cap(t.shift) < count {
		t.shift = make([]int16, count)
	}
	t.shift = t.shift[:count]
	t.planned, t.columns = true, columns

	// Detection proved these blocks inside the damage unchanged.
	bands := false
	for _, match := range t.matches {
		for _, block := range match.unchanged {
			unchanged[block.Y*columns+block.X] = true
		}
		bands = bands || len(match.bands) != 0
	}
	if !bands && t.staleSet == 0 {
		// Nothing departs from the decoded picture: repaint as usual.
		return nil, nil
	}
	t.paint(damageRegions, width, height)
	t.planCopies(unchanged)
	if len(t.update.Copies) == 0 && t.staleSet == 0 {
		// Nothing departs from the decoded picture: repaint as usual.
		return nil, nil
	}

	t.update.Tracked = true
	// A repainted stale block would show its old decoded content.
	for index, painted := range t.painted {
		if painted && t.stale[index] {
			unchanged[index] = false
		}
	}
	regions, ok := blockRegions(t.update.Regions[:0], t.painted, columns, width, height)
	if !ok {
		// Too fragmented to describe: code and repaint every departed block.
		for index := range count {
			if t.stale[index] || t.forced[index] {
				unchanged[index] = false
			}
			t.painted[index] = true
		}
		clear(t.forced)
		t.update.Regions, t.update.Copies, t.full = nil, t.update.Copies[:0], true

		return nil, nil
	}
	if len(regions) == 0 {
		// The access unit still needs a region; repaint a block that the
		// decoder shows correctly, or make one correct.
		index := 0
		for index < count && (t.stale[index] || t.forced[index]) {
			index++
		}
		if index == count {
			index = 0
			t.forced[0], unchanged[0] = false, false
		}
		t.painted[index] = true
		column, row := index%columns, index/columns
		regions = append(regions, image.Rect(column*16, row*16, min(column*16+16, width), min(row*16+16, height)))
	}
	t.update.Regions = regions

	return t.forced, t.shift
}

// paint marks the macroblocks that the damage regions touch.
func (t *surfaceTracker) paint(damageRegions []image.Rectangle, width, height int) {
	clear(t.painted)
	output := image.Rect(0, 0, width, height)
	for _, rect := range damageRegions {
		rect = damage.Align(rect, 16, output)
		for row := rect.Min.Y / 16; row < (rect.Max.Y+15)/16; row++ {
			for column := rect.Min.X / 16; column < (rect.Max.X+15)/16; column++ {
				t.painted[row*t.columns+column] = true
			}
		}
	}
}

// planCopies turns the matched bands into copies, forcing their blocks to
// follow the copy instead of being repainted.
func (t *surfaceTracker) planCopies(unchanged []bool) {
	clear(t.forced)
	copies := t.update.Copies[:0]
	for _, match := range t.matches {
		// Keep unsupported motion in the repaint mask. Dropping only the
		// codec hint would let the client copy pixels the decoder did not move.
		if match.shift < -api.MaxForcedShift || match.shift > api.MaxForcedShift {
			continue
		}
		// Copies run in order. Taking the bands nearest the content's source
		// side first keeps each one from overwriting another's source rows.
		bands := match.bands
		slices.SortFunc(bands, func(a, b image.Rectangle) int {
			if match.shift > 0 {
				return a.Min.Y - b.Min.Y
			}

			return b.Min.Y - a.Min.Y
		})
		for _, band := range bands {
			if len(copies) == MaxCopies {
				break
			}
			for row := band.Min.Y; row < band.Max.Y; row++ {
				for column := band.Min.X; column < band.Max.X; column++ {
					index := row*t.columns + column
					t.forced[index], unchanged[index], t.painted[index] = true, false, false
					t.shift[index] = int16(match.shift)
				}
			}
			destination := image.Rect(band.Min.X*16, band.Min.Y*16, band.Max.X*16, band.Max.Y*16)
			copies = append(copies, Copy{Source: destination.Add(image.Pt(0, match.shift)), Destination: destination.Min})
		}
	}
	t.update.Copies = copies
}

// blockRegions describes the true macroblocks of mask as disjoint rectangles:
// each row's runs, merged with the run directly above when they span the same
// columns. It reports false when more than MaxUpdateRegions are needed.
func blockRegions(regions []image.Rectangle, mask []bool, columns, width, height int) ([]image.Rectangle, bool) {
	rows := len(mask) / columns
	for row := range rows {
		for column := 0; column < columns; {
			if !mask[row*columns+column] {
				column++

				continue
			}
			start := column
			for column < columns && mask[row*columns+column] {
				column++
			}
			rect := image.Rect(start*16, row*16, min(column*16, width), min(row*16+16, height))
			merged := false
			for i := len(regions) - 1; i >= 0; i-- {
				if regions[i].Max.Y == rect.Min.Y && regions[i].Min.X == rect.Min.X && regions[i].Max.X == rect.Max.X {
					regions[i].Max.Y = rect.Max.Y
					merged = true

					break
				}
			}
			if !merged {
				if len(regions) == MaxUpdateRegions {
					return regions, false
				}
				regions = append(regions, rect)
			}
		}
	}

	return regions, true
}

// sourceStale reports whether a forced block was copied from rows that were
// stale: the up to two blocks its shifted source spans in its column.
func (t *surfaceTracker) sourceStale(index int) bool {
	column, row := index%t.columns, index/t.columns
	rows := len(t.previous) / t.columns
	top := row*16 + int(t.shift[index])
	for _, sourceRow := range [2]int{floorDiv(top, 16), floorDiv(top+15, 16)} {
		if sourceRow >= 0 && sourceRow < rows && t.previous[sourceRow*t.columns+column] {
			return true
		}
	}

	return false
}

func floorDiv(a, b int) int {
	if a < 0 {
		return -((-a + b - 1) / b)
	}

	return a / b
}

// sent records whether the client received the planned frame.
func (t *surfaceTracker) sent(ok bool) {
	if !t.planned {
		return
	}
	t.planned = false
	if !ok || !t.update.Tracked {
		return
	}
	if t.full {
		clear(t.stale)
		t.staleSet = 0

		return
	}
	t.previous = append(t.previous[:0], t.stale...)
	for index := range t.stale {
		stale := t.stale[index]
		if t.forced[index] {
			stale = t.shift[index]%2 != 0 || t.sourceStale(index)
		} else if t.painted[index] {
			stale = false
		}
		if stale != t.stale[index] {
			t.stale[index] = stale
			if stale {
				t.staleSet++
			} else {
				t.staleSet--
			}
		}
	}
}
