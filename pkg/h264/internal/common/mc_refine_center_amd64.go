//go:build amd64

package common

// mcHorRawRefineAVX2 filters signed intermediates for widths 5, 9 or 17.
// The caller provides positive height, nonnegative strides and disjoint, checked buffers.
// src starts at the leftmost tap; each row contains exactly width+5 inputs.
//
//go:noescape
func mcHorRawRefineAVX2(dst *byte, dstStride int, src *int16, srcStride, width, height int)
