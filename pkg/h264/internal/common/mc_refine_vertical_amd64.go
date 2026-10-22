//go:build amd64

package common

// mcVerReuseAVX2 applies the vertical six-tap filter, retaining adjacent-row
// pairs in registers between output rows. src points at the first output
// sample and requires a two-row upper and three-row lower halo.
//
// The caller must check AVX2 support, positive dimensions, complete live spans,
// disjoint source/destination storage, and dstStride >= width. Supported widths
// are 4, 5, 8, 9, 10, 13, 14, 16, 17, 21, 22 and 24. Loads and stores touch
// exactly those columns; widths above 16 finish with an overlapping eight-wide
// vector, avoiding scalar tails.
//
//go:noescape
func mcVerReuseAVX2(dst *byte, dstStride int, src *byte, srcStride, width, height int)

// mcVerRawReuseAVX2 preserves the signed, unrounded filter result for the
// diagonal filter's second pass. Its preconditions match mcVerReuseAVX2.
// dstStride counts int16 elements.
//
//go:noescape
func mcVerRawReuseAVX2(dst *int16, dstStride int, src *byte, srcStride, width, height int)
