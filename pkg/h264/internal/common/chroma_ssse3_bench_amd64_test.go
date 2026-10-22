//go:build amd64

package common

import "testing"

// The existing dispatch API makes this checked whole-call benchmark identical
// on the baseline and candidate; only the installed interpolation path changes.
func BenchmarkChromaSSSE3Dispatch(b *testing.B) {
	flags := WelsCPUFeatureDetect(nil)
	if flags&WELS_CPU_SSSE3 == 0 {
		b.Skip("SSSE3 required")
	}
	var funcs SMcFunc
	InitMcFunc(&funcs, flags)
	for _, tc := range []struct {
		name          string
		width, height int32
		x, y          int16
	}{
		{"8x4", 8, 4, 3, 5},
		{"8x7", 8, 7, 3, 5},
		{"8x8", 8, 8, 3, 5},
		{"8x16", 8, 16, 3, 5},
		{"8x8Horizontal", 8, 8, 5, 0},
		{"8x8Vertical", 8, 8, 0, 5},
		{"8x8IntegerControl", 8, 8, 0, 0},
		{"8x16IntegerControl", 8, 16, 0, 0},
		{"4x8Control", 4, 8, 3, 5},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var src, dst [1024]byte
			for i := range src {
				src[i] = byte(i*37 + 11)
			}
			b.ReportAllocs()
			for b.Loop() {
				funcs.PMcChromaFunc(src[:], 3, 31, dst[:], 5, 23, tc.x, tc.y, tc.width, tc.height)
			}
		})
	}
}
