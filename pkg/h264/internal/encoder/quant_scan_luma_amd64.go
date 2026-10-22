//go:build amd64

package encoder

import (
	"unsafe"

	"golang.org/x/sys/cpu"
)

//go:noescape
func quantScanLumaSSSE3(dct, levels *[256]int16, ff, mf *[8]int16, maxima *[16]int16, masks *[16]uint16)

//go:noescape
func quantScanLumaAVX2(dct, levels *[256]int16, ff, mf *[8]int16, maxima *[16]int16, masks *[16]uint16)

// Completed inter-luma encoding consumes every group, so one checked call can
// share the quantization and zigzag tables across all sixteen blocks. Lazy skip
// attempts retain their four-block calls and early rejection behavior.
func quantScanLuma(dct, levels, ff, mf, maxima []int16, masks []uint16) {
	_ = dct[255]
	_ = levels[255]
	_ = ff[7]
	_ = mf[7]
	_ = maxima[15]
	_ = masks[15]
	if !cpu.X86.HasSSSE3 || quantScanLumaAliases(unsafe.Pointer(&dct[0]), unsafe.Pointer(&levels[0]),
		unsafe.Pointer(&ff[0]), unsafe.Pointer(&mf[0]), unsafe.Pointer(&maxima[0]), unsafe.Pointer(&masks[0])) {
		quantScanLumaSeparate(dct, levels, ff, mf, maxima, masks)
		return
	}
	if cpu.X86.HasAVX2 {
		quantScanLumaAVX2((*[256]int16)(dct), (*[256]int16)(levels), (*[8]int16)(ff), (*[8]int16)(mf),
			(*[16]int16)(maxima), (*[16]uint16)(masks))
		return
	}
	quantScanLumaSSSE3((*[256]int16)(dct), (*[256]int16)(levels), (*[8]int16)(ff), (*[8]int16)(mf),
		(*[16]int16)(maxima), (*[16]uint16)(masks))
}

func quantScanLumaAliases(d, l, f, q, x, m unsafe.Pointer) bool {
	// All outputs must be disjoint. FF/MF also cannot overlap any output:
	// the old sequence reloads them before each group, whereas the batch
	// snapshots them only once. FF and MF may overlap one another.
	return quantScanRangesOverlap(d, 512, l, 512) ||
		quantScanRangesOverlap(d, 512, x, 32) || quantScanRangesOverlap(d, 512, m, 32) ||
		quantScanRangesOverlap(l, 512, x, 32) || quantScanRangesOverlap(l, 512, m, 32) ||
		quantScanRangesOverlap(x, 32, m, 32) ||
		quantScanRangesOverlap(f, 16, d, 512) || quantScanRangesOverlap(f, 16, l, 512) ||
		quantScanRangesOverlap(f, 16, x, 32) || quantScanRangesOverlap(f, 16, m, 32) ||
		quantScanRangesOverlap(q, 16, d, 512) || quantScanRangesOverlap(q, 16, l, 512) ||
		quantScanRangesOverlap(q, 16, x, 32) || quantScanRangesOverlap(q, 16, m, 32)
}
