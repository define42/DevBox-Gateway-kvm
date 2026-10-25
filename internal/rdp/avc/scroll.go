package avc

import (
	"encoding/binary"
	"image"
)

const (
	// scrollMinBlocks is the smallest region, in macroblocks per side, worth
	// searching for a scroll.
	scrollMinBlocks = 4
	// scrollProbeWidth is the width of the columns whose rows vote for a
	// vertical shift. Probes stay inside the region, where its edges may be static.
	scrollProbeWidth = 32
	// scrollRowRepeats is how many old rows sharing a probe hash may each be
	// voted for; rows repeated more often, such as blank ones, carry no position.
	scrollRowRepeats = 3
	// scrollVoteStride samples the new rows that vote; every old row is a candidate.
	scrollVoteStride = 4
	// scrollMinVotes rows must agree on a shift before it is verified.
	scrollMinVotes = 8
	// scrollChromaTolerance bounds an odd shift's chroma, which straddles two
	// old chroma rows and so cannot be compared exactly; luma always must be.
	scrollChromaTolerance = 8
	// scrollMaxBands limits the moved rectangles taken from one area.
	scrollMaxBands = 4
)

// regionSnapshot is a region's previous I420 samples, taken before conversion
// overwrites them, for comparing the new samples against.
type regionSnapshot struct {
	region image.Rectangle // Even-aligned output pixels.
	luma   []byte
	u, v   []byte
}

// regionMatch is what detection proved about a region's interior macroblocks:
// unchanged blocks, and the largest rectangles moved by one vertical shift.
type regionMatch struct {
	unchanged []image.Point     // Macroblock coordinates.
	bands     []image.Rectangle // Macroblock coordinates, disjoint.
	shift     int               // New row y shows old row y+shift; nonzero with bands.
}

// detectionAreas merges touching damage rectangles into disjoint bounding
// boxes. A scrolled pane often arrives as stacked rectangles of different
// widths; its content moves between them, so they are compared as one area.
func detectionAreas(areas, rects []image.Rectangle) []image.Rectangle {
	areas = append(areas[:0], rects...)
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(areas); i++ {
			for j := i + 1; j < len(areas); j++ {
				if areas[i].Inset(-1).Overlaps(areas[j]) {
					areas[i] = areas[i].Union(areas[j])
					areas = append(areas[:j], areas[j+1:]...)
					merged = true
					j = i
				}
			}
		}
	}

	return areas
}

// scrollDetector holds reusable scratch for one encoder.
type scrollDetector struct {
	snapshot regionSnapshot
	rows     map[uint64]rowCandidates
	probe    []byte
	votes    []int32
	verified []bool
	heights  []int32
	stack    []int32
}

// rowCandidates holds the old rows sharing a probe hash; count exceeds the
// stored rows when there are too many to vote for.
type rowCandidates struct {
	count int32
	rows  [scrollRowRepeats]int32
}

func scrollCandidate(region image.Rectangle) bool {
	columns, rows := interiorBlocks(region)

	return columns.Y-columns.X >= scrollMinBlocks && rows.Y-rows.X >= scrollMinBlocks
}

// interiorBlocks returns the macroblock columns and rows lying entirely inside
// region, as [Min, Max) ranges in X.
func interiorBlocks(region image.Rectangle) (image.Point, image.Point) {
	return image.Pt((region.Min.X+15)/16, region.Max.X/16), image.Pt((region.Min.Y+15)/16, region.Max.Y/16)
}

// take copies the region's samples from the encoder's I420 picture.
func (s *regionSnapshot) take(i420 []byte, width, height int, region image.Rectangle) {
	s.region = region
	w, h := region.Dx(), region.Dy()
	s.luma = growBytes(s.luma, w*h)
	s.u = growBytes(s.u, w/2*(h/2))
	s.v = growBytes(s.v, w/2*(h/2))
	for y := range h {
		copy(s.luma[y*w:][:w], i420[(region.Min.Y+y)*width+region.Min.X:])
	}
	luma, chroma := width*height, width/2*(height/2)
	for y := range h / 2 {
		offset := (region.Min.Y/2+y)*(width/2) + region.Min.X/2
		copy(s.u[y*(w/2):][:w/2], i420[luma+offset:])
		copy(s.v[y*(w/2):][:w/2], i420[luma+chroma+offset:])
	}
}

func growBytes(b []byte, n int) []byte {
	if cap(b) < n {
		return make([]byte, n)
	}

	return b[:n]
}

// detect fills match with comparisons against the snapshot: every interior
// block at shift rows lower when shift is nonzero, and the rest at their own
// position. Each area owns its result slices and reuses them across frames.
func (d *scrollDetector) detect(match *regionMatch, i420 []byte, width, height, shift int) {
	snapshot := &d.snapshot
	columns, rows := interiorBlocks(snapshot.region)
	cols, blockRows := columns.Y-columns.X, rows.Y-rows.X
	match.unchanged, match.bands, match.shift = match.unchanged[:0], match.bands[:0], 0
	pictures := blockPictures{i420: i420, width: width, height: height, old: snapshot}
	d.verified = growBools(d.verified, cols*blockRows)
	if shift != 0 {
		pictures.verify(d.verified, columns, rows, shift, nil)
	} else {
		clear(d.verified)
	}
	for len(match.bands) < scrollMaxBands {
		band := d.largestRectangle(cols, blockRows)
		if band.Dx() < 2 || band.Dy() < 2 {
			break
		}
		for row := band.Min.Y; row < band.Max.Y; row++ {
			clear(d.verified[row*cols+band.Min.X : row*cols+band.Max.X])
		}
		match.bands = append(match.bands, band.Add(image.Pt(columns.X, rows.X)))
	}
	if len(match.bands) != 0 {
		match.shift = shift
	}
	// The rest of the area may be unchanged, such as static pane edges.
	pictures.verify(d.verified, columns, rows, 0, match.bands)
	for row := range blockRows {
		for column := range cols {
			block := image.Pt(columns.X+column, rows.X+row)
			if d.verified[row*cols+column] && !inBands(match.bands, block.X, block.Y) {
				match.unchanged = append(match.unchanged, block)
			}
		}
	}
}

// rowCovered reports whether bands cover every column of the block row.
func rowCovered(bands []image.Rectangle, columns image.Point, row int) bool {
	for _, band := range bands {
		if row >= band.Min.Y && row < band.Max.Y && band.Min.X <= columns.X && band.Max.X >= columns.Y {
			return true
		}
	}

	return false
}

func inBands(bands []image.Rectangle, column, row int) bool {
	for _, band := range bands {
		if image.Pt(column, row).In(band) {
			return true
		}
	}

	return false
}

func growBools(b []bool, n int) []bool {
	if cap(b) < n {
		return make([]bool, n)
	}

	return b[:n]
}

// vote returns the vertical shift most probe rows of area agree on, or 0. It
// runs before conversion: new rows are the source pixels' luma, computed as
// conversion would, and old rows still sit in the encoder's I420 picture.
func (d *scrollDetector) vote(img *image.RGBA, bgrx bool, area image.Rectangle, i420 []byte, width int) int {
	columns, _ := interiorBlocks(area)
	h := area.Dy()
	if d.rows == nil {
		d.rows = make(map[uint64]rowCandidates, h)
	}
	if cap(d.votes) < 2*h {
		d.votes = make([]int32, 2*h)
	}
	d.votes = d.votes[:2*h]
	clear(d.votes)
	span := (columns.Y - columns.X) * 16
	probe := min(scrollProbeWidth, span)
	d.probe = growBytes(d.probe, probe)
	for _, fraction := range [...]int{1, 2, 3} {
		x := columns.X*16 + (span-probe)*fraction/4
		clear(d.rows)
		for y := range h {
			key := rowHash(i420[(area.Min.Y+y)*width+x:][:probe])
			candidates := d.rows[key]
			if candidates.count < scrollRowRepeats {
				candidates.rows[candidates.count] = int32(y)
			}
			candidates.count++
			d.rows[key] = candidates
		}
		for y := 0; y < h; y += scrollVoteStride {
			sourceLuma(d.probe, img, bgrx, x, area.Min.Y+y)
			candidates, found := d.rows[rowHash(d.probe)]
			if !found || candidates.count > scrollRowRepeats {
				continue
			}
			for _, old := range candidates.rows[:candidates.count] {
				if int(old) != y {
					d.votes[int(old)-y+h]++
				}
			}
		}
	}
	best := 0
	for index, votes := range d.votes {
		if votes > d.votes[best] {
			best = index
		}
	}
	if d.votes[best] < scrollMinVotes {
		return 0
	}

	return best - h
}

// sourceLuma fills dst with the luma of the source row y from column x, with
// the arithmetic every conversion path uses.
func sourceLuma(dst []byte, img *image.RGBA, bgrx bool, x, y int) {
	row := img.Pix[y*img.Stride+x*4:][:len(dst)*4]
	red, blue := 0, 2
	if bgrx {
		red, blue = 2, 0
	}
	for i := range dst {
		pixel := row[i*4:][:3]
		dst[i] = byte((54*int(pixel[red]) + 183*int(pixel[1]) + 18*int(pixel[blue])) >> 8)
	}
}

// rowHash mixes a row's bytes; collisions only cost a wasted vote.
func rowHash(row []byte) uint64 {
	hash := uint64(len(row)) * 0x9e3779b97f4a7c15
	for len(row) >= 8 {
		hash = (hash ^ binary.LittleEndian.Uint64(row)) * 0xbf58476d1ce4e5b9
		hash ^= hash >> 31
		row = row[8:]
	}
	for _, b := range row {
		hash = (hash ^ uint64(b)) * 0x94d049bb133111eb
	}

	return hash
}

// largestRectangle finds the largest all-verified rectangle of the cols by
// rows matrix, using the stack method on each row's column heights.
func (d *scrollDetector) largestRectangle(cols, rows int) image.Rectangle {
	if cap(d.heights) < cols {
		d.heights = make([]int32, cols)
	}
	d.heights = d.heights[:cols]
	clear(d.heights)
	var best image.Rectangle
	for row := range rows {
		for column := range cols {
			if d.verified[row*cols+column] {
				d.heights[column]++
			} else {
				d.heights[column] = 0
			}
		}
		d.stack = d.stack[:0]
		for column := 0; column <= cols; column++ {
			height := int32(0)
			if column < cols {
				height = d.heights[column]
			}
			for len(d.stack) > 0 && d.heights[d.stack[len(d.stack)-1]] >= height {
				top := d.heights[d.stack[len(d.stack)-1]]
				d.stack = d.stack[:len(d.stack)-1]
				left := 0
				if len(d.stack) > 0 {
					left = int(d.stack[len(d.stack)-1]) + 1
				}
				candidate := image.Rect(left, row+1-int(top), column, row+1)
				if area(candidate) > area(best) {
					best = candidate
				}
			}
			if column < cols {
				d.stack = append(d.stack, int32(column))
			}
		}
	}

	return best
}

func area(rect image.Rectangle) int { return rect.Dx() * rect.Dy() }

// blockPictures compares a macroblock of the new picture with the snapshot's
// samples shift rows lower.
type blockPictures struct {
	i420          []byte
	width, height int
	old           *regionSnapshot
}

// verify sets each interior block's entry of verified, row-major over the
// blocks in columns and rows, to whether its new samples repeat the snapshot's
// shift rows lower. Each picture row is compared in one pass across the area.
// Block rows that skip covers completely are left false without comparing.
func (p blockPictures) verify(verified []bool, columns, rows image.Point, shift int, skip []image.Rectangle) {
	region := p.old.region
	cols := columns.Y - columns.X
	for i := range verified {
		verified[i] = true
	}
	w := region.Dx()
	for row := rows.X; row < rows.Y; row++ {
		blocks := verified[(row-rows.X)*cols:][:cols]
		y := row * 16
		if y+shift < region.Min.Y || y+16+shift > region.Max.Y || rowCovered(skip, columns, row) {
			clear(blocks)

			continue
		}
		for line := range 16 {
			current := p.i420[(y+line)*p.width+columns.X*16:][:cols*16]
			previous := p.old.luma[(y+line+shift-region.Min.Y)*w+columns.X*16-region.Min.X:][:cols*16]
			for column, i := 0, 0; column < cols; column, i = column+1, i+16 {
				if binary.LittleEndian.Uint64(current[i:]) != binary.LittleEndian.Uint64(previous[i:]) ||
					binary.LittleEndian.Uint64(current[i+8:]) != binary.LittleEndian.Uint64(previous[i+8:]) {
					blocks[column] = false
				}
			}
		}
		luma, chroma := p.width*p.height, p.width/2*(p.height/2)
		for plane, old := range [2][]byte{p.old.u, p.old.v} {
			base := luma + plane*chroma
			for line := range 8 {
				current := p.i420[base+(y/2+line)*(p.width/2)+columns.X*8:][:cols*8]
				if !chromaRowMatches(blocks, current, old, region, columns.X*8, y/2+line, shift) {
					clear(blocks)
				}
			}
		}
	}
}

// chromaRowMatches clears the blocks whose 8 samples of the chroma row y, from
// column x, do not repeat the old samples they should. An even shift moves whole
// chroma rows. An odd one starts halfway through an old row, so the new samples
// must lie close to the mean of the two rows they straddle. It reports false
// when the old rows lie outside the snapshot.
func chromaRowMatches(blocks []bool, current, old []byte, region image.Rectangle, x, y, shift int) bool {
	w := region.Dx() / 2
	top, rows := region.Min.Y/2, region.Dy()/2
	if shift%2 == 0 {
		row := y + shift/2 - top
		if row < 0 || row >= rows {
			return false
		}
		previous := old[row*w+x-region.Min.X/2:][:len(current)]
		for column, i := 0, 0; column < len(blocks); column, i = column+1, i+8 {
			if binary.LittleEndian.Uint64(current[i:]) != binary.LittleEndian.Uint64(previous[i:]) {
				blocks[column] = false
			}
		}

		return true
	}
	first := y + (shift-1)/2 - top
	if first < 0 || first+1 >= rows {
		return false
	}
	a := old[first*w+x-region.Min.X/2:][:len(current)]
	b := old[(first+1)*w+x-region.Min.X/2:][:len(current)]
	for column := range blocks {
		for i := column * 8; i < column*8+8; i++ {
			mean := (int(a[i]) + int(b[i]) + 1) / 2
			if delta := int(current[i]) - mean; delta > scrollChromaTolerance || delta < -scrollChromaTolerance {
				blocks[column] = false

				break
			}
		}
	}

	return true
}
