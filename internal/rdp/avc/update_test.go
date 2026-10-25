package avc

import (
	"image"
	"slices"
	"testing"
)

func TestSurfaceTrackerScrollRange(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		shift int
		copy  bool
	}{
		{"positive limit", 63, true},
		{"negative limit", -63, true},
		{"positive outside", 64, false},
		{"negative outside", -64, false},
		{"large positive", 600, false},
		{"large negative", -600, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			const width, height, columns = 128, 2048, 8
			band := image.Rect(1, 48, 7, 80)
			damaged := image.Rect(16, 768, 112, 1280)
			unchanged := make([]bool, columns*(height/16))
			var tracker surfaceTracker
			plan := func(shift int) []bool {
				tracker.begin()
				tracker.matches = append(tracker.matches, regionMatch{bands: []image.Rectangle{band}, shift: shift})
				clear(unchanged)
				forced, _ := tracker.plan(unchanged, width, height, []image.Rectangle{damaged})

				return forced
			}

			// Run once with a clean surface, then reuse the masks after an odd
			// copy has made the same band stale. A rejected copy must repaint it.
			for _, stale := range []bool{false, true} {
				if stale {
					plan(3)
					tracker.sent(true)
					if tracker.staleSet == 0 {
						t.Fatal("odd copy did not exercise stale pixels")
					}
				}
				forced := plan(test.shift)
				if test.copy {
					want := Copy{Source: damaged.Add(image.Pt(0, test.shift)), Destination: damaged.Min}
					if !slices.Equal(tracker.update.Copies, []Copy{want}) || !slices.Contains(forced, true) {
						t.Fatalf("supported scroll was not copied: %+v", tracker.update)
					}
					tracker.sent(true)

					continue
				}
				if len(tracker.update.Copies) != 0 || slices.Contains(forced, true) {
					t.Fatalf("unsupported scroll was copied: %+v", tracker.update)
				}
				if stale && (!tracker.update.Tracked || !slices.Equal(tracker.update.Regions, []image.Rectangle{damaged})) {
					t.Fatalf("stale rejected copy was not repainted: %+v", tracker.update)
				}
				if !stale && (tracker.update.Tracked || forced != nil) {
					t.Fatalf("clean rejected copy did not use ordinary damage: %+v", tracker.update)
				}
				tracker.sent(true)
				if tracker.staleSet != 0 {
					t.Fatal("repaint left rejected copy stale")
				}
			}
		})
	}
}
