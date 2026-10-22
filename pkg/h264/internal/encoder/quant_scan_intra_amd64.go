//go:build amd64

package encoder

import (
	"reflect"
	"unsafe"

	"golang.org/x/sys/cpu"
)

var intraACQuantFourSSE2PC = reflect.ValueOf(WelsQuantFour4x4_sse2).Pointer()

//go:noescape
func quantScanIntraLumaAVX2(dct, levels *[256]int16, ff, mf *[8]int16, masks *[16]uint16)

// Try after DCT, so a custom transform can replace the quantization callback.
// Only the exact built-in SSE2 quantizer has this packed unsigned arithmetic;
// custom/scalar callbacks retain the original group-by-group call sequence.
// Unlike inter/chroma quant-scan, intra16 always scans every coefficient: a
// signed maximum of zero must not discard unsigned magnitudes >= 32768.
func tryQuantScanIntraLuma(funcs *SWelsFuncPtrList, dct, levels, ff, mf []int16, masks *[16]uint16) bool {
	if !cpu.X86.HasAVX2 || funcs == nil || len(dct) < 256 || len(levels) < 256 ||
		len(ff) < 8 || len(mf) < 8 || masks == nil ||
		reflect.ValueOf(funcs.pfQuantizationFour4x4).Pointer() != intraACQuantFourSSE2PC {
		return false
	}
	d, l := unsafe.Pointer(&dct[0]), unsafe.Pointer(&levels[0])
	f, q, m := unsafe.Pointer(&ff[0]), unsafe.Pointer(&mf[0]), unsafe.Pointer(masks)
	// The batch snapshots tables once and interleaves raster/scan stores.
	// Reject overlaps across the entire macroblock, including future groups.
	// FF and MF may alias each other: neither is written on this path.
	if quantScanRangesOverlap(d, 512, l, 512) ||
		quantScanRangesOverlap(d, 512, m, 32) || quantScanRangesOverlap(l, 512, m, 32) ||
		quantScanRangesOverlap(f, 16, d, 512) || quantScanRangesOverlap(f, 16, l, 512) || quantScanRangesOverlap(f, 16, m, 32) ||
		quantScanRangesOverlap(q, 16, d, 512) || quantScanRangesOverlap(q, 16, l, 512) || quantScanRangesOverlap(q, 16, m, 32) {
		return false
	}
	quantScanIntraLumaAVX2((*[256]int16)(dct), (*[256]int16)(levels), (*[8]int16)(ff), (*[8]int16)(mf), masks)
	return true
}
