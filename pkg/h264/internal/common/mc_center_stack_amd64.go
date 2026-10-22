//go:build amd64

package common

// mcCenterRefineAVX2 accepts only widths 4, 5, 8, 9, 16, 17 and heights 1..17, after
// McHorVer22_avx2 validates the complete source halo and destination span.
// src points two columns before the first destination sample. Source rows
// include two preceding and three following rows. All source reads finish
// before any destination stores, including for aliased or overlapping rows.
// The no-call leaf uses 748 bytes of initialized taps in a 752-byte frame.
//
//go:noescape
func mcCenterRefineAVX2(dst *byte, dstStride int, src *byte, srcStride, width, height int)
