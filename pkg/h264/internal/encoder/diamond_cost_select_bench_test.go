package encoder

import "testing"

var diamondCostSelectSink int32

func BenchmarkDiamondCostSelect(b *testing.B) {
	for _, tc := range []struct {
		name  string
		costs [4]int32
		best  int32
	}{
		{"NoImprovement", [4]int32{4000, 4000, 4000, 4000}, 100},
		{"Top", [4]int32{100, 4000, 4000, 4000}, 8000},
		{"Bottom", [4]int32{4000, 100, 4000, 4000}, 8000},
		{"Left", [4]int32{4000, 4000, 100, 4000}, 8000},
		{"Right", [4]int32{4000, 4000, 4000, 100}, 8000},
		{"Descending", [4]int32{4000, 3000, 2000, 1000}, 8000},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var table [2048]uint16
			for i := range table {
				if i < 1024 {
					table[i] = uint16(1024 - i)
				} else {
					table[i] = uint16(i - 1024)
				}
			}
			var best, x, y int32
			b.ReportAllocs()
			for b.Loop() {
				best = tc.best
				x, y = 0, 0
				WelsMeSadCostSelect(tc.costs[:], table[:], 1024, &best, 28, -12, &x, &y)
			}
			diamondCostSelectSink = best + x + y
		})
	}
}
