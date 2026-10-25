package avc

import (
	"image"
	"slices"
	"testing"
)

func TestDetectionAreasMergesTouchingRectangles(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		rects []image.Rectangle
		want  []image.Rectangle
	}{
		{
			name:  "stacked pane",
			rects: []image.Rectangle{image.Rect(0, 0, 100, 50), image.Rect(0, 50, 80, 100)},
			want:  []image.Rectangle{image.Rect(0, 0, 100, 100)},
		},
		{
			name:  "separate",
			rects: []image.Rectangle{image.Rect(0, 0, 100, 50), image.Rect(0, 52, 80, 100)},
			want:  []image.Rectangle{image.Rect(0, 0, 100, 50), image.Rect(0, 52, 80, 100)},
		},
		{
			// The first and last touch only through the merged middle one.
			name: "transitive",
			rects: []image.Rectangle{
				image.Rect(0, 0, 40, 40), image.Rect(200, 0, 240, 40), image.Rect(30, 40, 210, 60),
			},
			want: []image.Rectangle{image.Rect(0, 0, 240, 60)},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rects := slices.Clone(test.rects)
			if got := detectionAreas(nil, rects); !slices.Equal(got, test.want) {
				t.Fatalf("areas %v, want %v", got, test.want)
			}
			if !slices.Equal(rects, test.rects) {
				t.Fatal("detectionAreas changed its input")
			}
		})
	}
}

func TestLargestVerifiedRectangle(t *testing.T) {
	t.Parallel()
	rows := []string{
		".XX...",
		".XXXX.",
		"XXXXX.",
		".XXXXX",
		"......",
	}
	var d scrollDetector
	for _, row := range rows {
		for _, cell := range row {
			d.verified = append(d.verified, cell == 'X')
		}
	}
	if got, want := d.largestRectangle(len(rows[0]), len(rows)), image.Rect(1, 1, 5, 4); got != want {
		t.Fatalf("largest rectangle %v, want %v", got, want)
	}
	clear(d.verified)
	if got := d.largestRectangle(len(rows[0]), len(rows)); !got.Empty() {
		t.Fatalf("largest rectangle of nothing %v", got)
	}
}

func TestBlockRegions(t *testing.T) {
	t.Parallel()
	mask := func(rows ...string) ([]bool, int) {
		var cells []bool
		for _, row := range rows {
			for _, cell := range row {
				cells = append(cells, cell == 'X')
			}
		}

		return cells, len(rows[0])
	}
	for _, test := range []struct {
		name          string
		rows          []string
		width, height int
		want          []image.Rectangle
	}{
		{
			// The last column and row are clipped to the picture.
			name: "clipped", rows: []string{"XX.X", "XX.X", ".XXX"}, width: 56, height: 40,
			want: []image.Rectangle{image.Rect(0, 0, 32, 32), image.Rect(48, 0, 56, 32), image.Rect(16, 32, 56, 40)},
		},
		{
			// A run continues its region even when a later region ended earlier.
			name: "continued", rows: []string{"XX.X..", "XX...X", "XX...."}, width: 96, height: 48,
			want: []image.Rectangle{image.Rect(0, 0, 32, 48), image.Rect(48, 0, 64, 16), image.Rect(80, 16, 96, 32)},
		},
		{name: "empty", rows: []string{"....", "...."}, width: 64, height: 32},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cells, columns := mask(test.rows...)
			got, ok := blockRegions(nil, cells, columns, test.width, test.height)
			if !ok || !slices.Equal(got, test.want) {
				t.Fatalf("regions %v (%t), want %v", got, ok, test.want)
			}
		})
	}

	t.Run("fragmented", func(t *testing.T) {
		t.Parallel()
		const columns, rows = 32, 32
		cells := make([]bool, columns*rows)
		for index := range cells {
			cells[index] = (index%columns+index/columns)%2 == 0
		}
		if got, ok := blockRegions(nil, cells, columns, columns*16, rows*16); ok || len(got) != MaxUpdateRegions {
			t.Fatalf("checkerboard gave %d regions, ok %t", len(got), ok)
		}
	})
}

func TestChromaRowMatches(t *testing.T) {
	t.Parallel()
	// The snapshot covers chroma columns 16..48 and rows 8..40.
	region := image.Rect(32, 16, 96, 80)
	const w, rows, x = 32, 32, 16
	old := make([]byte, w*rows)
	for index := range old {
		old[index] = byte(index/w*7 + index%w*3)
	}
	oldRow := func(row int) []byte { return old[row*w:][:w] }
	check := func(t *testing.T, current []byte, y, shift int, want []bool) {
		t.Helper()
		blocks := []bool{true, true, true, true}
		if !chromaRowMatches(blocks, current, old, region, x, y, shift) || !slices.Equal(blocks, want) {
			t.Fatalf("blocks %v, want %v", blocks, want)
		}
	}
	all := []bool{true, true, true, true}

	t.Run("even", func(t *testing.T) {
		t.Parallel()
		// Chroma row 10 moved down four luma rows shows snapshot row 10+2-8.
		current := slices.Clone(oldRow(4))
		check(t, current, 10, 4, all)
		current[9]++
		check(t, current, 10, 4, []bool{true, false, true, true})
	})

	for _, test := range []struct {
		name         string
		shift, first int
	}{
		{"odd down", 3, 3},
		{"odd up", -3, 0},
		{"one up", -1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// An odd shift lands between two snapshot rows; their mean is
			// accepted within the tolerance.
			a, b := oldRow(test.first), oldRow(test.first+1)
			current := make([]byte, w)
			for i := range current {
				current[i] = byte((int(a[i])+int(b[i])+1)/2 + scrollChromaTolerance - i%(2*scrollChromaTolerance+1))
			}
			check(t, current, 10, test.shift, all)
			current[20] = byte((int(a[20])+int(b[20])+1)/2 + scrollChromaTolerance + 1)
			check(t, current, 10, test.shift, []bool{true, true, false, true})
		})
	}

	t.Run("outside snapshot", func(t *testing.T) {
		t.Parallel()
		for _, test := range [][2]int{{8, -4}, {39, 2}, {8, -1}, {39, 1}} {
			if chromaRowMatches(make([]bool, 4), make([]byte, w), old, region, x, test[0], test[1]) {
				t.Fatalf("row %d shift %d compared outside the snapshot", test[0], test[1])
			}
		}
	})
}

func TestScrollVote(t *testing.T) {
	t.Parallel()
	const width, height = 128, 256
	area := image.Rect(16, 32, 112, 224)
	// content draws row r of a document whose rows repeat every period rows,
	// with blank lines between blocks of text.
	content := func(img *image.RGBA, row, source, period int, salt uint32) {
		line := ((source % period) + period) % period
		for x := range width {
			pixel := img.Pix[row*img.Stride+x*4:][:4]
			if line%10 < 2 {
				copy(pixel, []byte{255, 255, 255, 255})

				continue
			}
			v := (uint32(line)*2654435761 + uint32(x)*40503 + salt) * 0x9e3779b1
			copy(pixel, []byte{byte(v >> 8), byte(v >> 16), byte(v >> 24), 255})
		}
	}
	for _, test := range []struct {
		name          string
		period, shift int
		salt          uint32
		want          int
	}{
		{name: "unique rows", period: 1 << 20, shift: 9, want: 9},
		{name: "repeated text down", period: 80, shift: 7, want: 7},
		{name: "repeated text up", period: 80, shift: -12, want: -12},
		{name: "unrelated", period: 80, shift: 5, salt: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			previous := image.NewRGBA(image.Rect(0, 0, width, height))
			current := image.NewRGBA(image.Rect(0, 0, width, height))
			for row := range height {
				content(previous, row, row, test.period, 0)
				content(current, row, row+test.shift, test.period, test.salt)
			}
			i420 := make([]byte, width*height*3/2)
			for row := range height {
				sourceLuma(i420[row*width:][:width], previous, false, 0, row)
			}
			var d scrollDetector
			if got := d.vote(current, false, area, i420, width); got != test.want {
				t.Fatalf("vote %d, want %d", got, test.want)
			}
		})
	}
}

func TestSurfaceTrackerFollowsStaleBlocks(t *testing.T) {
	t.Parallel()
	const width, height, columns = 128, 128, 8
	var tracker surfaceTracker
	unchanged := make([]bool, columns*columns)
	staleBlocks := func() int {
		count := 0
		for _, stale := range tracker.stale {
			if stale {
				count++
			}
		}
		if count != tracker.staleSet {
			t.Fatalf("%d stale blocks, staleSet %d", count, tracker.staleSet)
		}

		return count
	}
	plan := func(damage image.Rectangle, matches ...regionMatch) ([]bool, []int16) {
		tracker.begin()
		tracker.matches = append(tracker.matches, matches...)
		for index := range unchanged {
			unchanged[index] = true
		}

		return tracker.plan(unchanged, width, height, []image.Rectangle{damage})
	}
	block := func(column, row int) int { return row*columns + column }

	// An odd scroll copies blocks whose decoded chroma is then interpolated.
	scroll := regionMatch{bands: []image.Rectangle{image.Rect(1, 1, 7, 5)}, shift: 3}
	forced, shifts := plan(image.Rect(16, 16, 112, 96), scroll)
	update := tracker.update
	if forced == nil || !forced[block(1, 1)] || shifts[block(1, 1)] != 3 || unchanged[block(1, 1)] {
		t.Fatal("band blocks are not forced")
	}
	wantCopies := []Copy{{Source: image.Rect(16, 19, 112, 83), Destination: image.Pt(16, 16)}}
	if !update.Tracked || !slices.Equal(update.Copies, wantCopies) ||
		!slices.Equal(update.Regions, []image.Rectangle{image.Rect(16, 80, 112, 96)}) {
		t.Fatalf("odd scroll update %+v", update)
	}
	tracker.sent(true)
	if staleBlocks() != 24 || !tracker.stale[block(1, 1)] || tracker.stale[block(1, 5)] {
		t.Fatal("odd shift did not leave exactly its forced blocks stale")
	}

	// A lost frame leaves the client's surface, and so the stale set, as it was.
	plan(image.Rect(32, 32, 48, 48))
	tracker.sent(false)
	if staleBlocks() != 24 {
		t.Fatal("unsent frame changed stale blocks")
	}

	// Repainting a stale block codes it from the source and refreshes it.
	if forced, _ := plan(image.Rect(32, 32, 48, 48)); forced == nil || unchanged[block(2, 2)] ||
		!slices.Equal(tracker.update.Regions, []image.Rectangle{image.Rect(32, 32, 48, 48)}) {
		t.Fatalf("stale repaint update %+v, unchanged %t", tracker.update, unchanged[block(2, 2)])
	}
	tracker.sent(true)
	if staleBlocks() != 23 || tracker.stale[block(2, 2)] {
		t.Fatal("repainted block is still stale")
	}

	// An even shift is exact, unless its source rows were stale. The band's
	// row 0 copies stale row 1 while the damaged row 1 is repainted.
	plan(image.Rect(16, 0, 112, 32), regionMatch{bands: []image.Rectangle{image.Rect(1, 0, 7, 1)}, shift: 16})
	if unchanged[block(3, 1)] || !tracker.painted[block(3, 1)] || tracker.painted[block(3, 0)] {
		t.Fatal("stale damaged blocks are not repainted")
	}
	tracker.sent(true)
	if !tracker.stale[block(3, 0)] || tracker.stale[block(3, 1)] || staleBlocks() != 23 {
		t.Fatal("copy of stale rows is not stale")
	}
	plan(image.Rect(16, 96, 112, 128), regionMatch{bands: []image.Rectangle{image.Rect(1, 6, 7, 7)}, shift: 16})
	tracker.sent(true)
	if tracker.stale[block(3, 6)] || staleBlocks() != 23 {
		t.Fatal("exact even copy became stale")
	}
}

// Damage too fragmented to describe repaints every block, stale or forced.
func TestSurfaceTrackerFragmentedDamageRepaintsAll(t *testing.T) {
	t.Parallel()
	const size, columns = 512, 32
	var tracker surfaceTracker
	tracker.begin()
	tracker.matches = append(tracker.matches, regionMatch{bands: []image.Rectangle{image.Rect(0, 4, 2, 6)}, shift: 2})
	tracker.stale, tracker.staleSet = make([]bool, columns*columns), 1
	tracker.stale[5] = true
	var damage []image.Rectangle
	for index := range columns * columns {
		if (index%columns+index/columns)%2 == 0 {
			x, y := index%columns*16, index/columns*16
			damage = append(damage, image.Rect(x, y, x+16, y+16))
		}
	}
	unchanged := make([]bool, columns*columns)
	for index := range unchanged {
		unchanged[index] = true
	}
	forced, _ := tracker.plan(unchanged, size, size, damage)
	if forced != nil || unchanged[5] || unchanged[4*columns] || tracker.update.Regions != nil ||
		len(tracker.update.Copies) != 0 || !tracker.update.Tracked {
		t.Fatalf("fragmented update %+v", tracker.update)
	}
	tracker.sent(true)
	if tracker.staleSet != 0 || slices.Contains(tracker.stale, true) {
		t.Fatal("full repaint left stale blocks")
	}
}

// A frame whose damage is entirely copied still repaints one correct block.
func TestSurfaceTrackerCopiedFrameKeepsARegion(t *testing.T) {
	t.Parallel()
	var tracker surfaceTracker
	tracker.begin()
	tracker.matches = append(tracker.matches, regionMatch{bands: []image.Rectangle{image.Rect(1, 1, 7, 5)}, shift: 2})
	unchanged := make([]bool, 64)
	if forced, _ := tracker.plan(unchanged, 128, 128, []image.Rectangle{image.Rect(16, 16, 112, 80)}); forced == nil ||
		!slices.Equal(tracker.update.Regions, []image.Rectangle{image.Rect(0, 0, 16, 16)}) || !tracker.painted[0] {
		t.Fatalf("copied frame update %+v", tracker.update)
	}
}
