//go:build amd64

package common

// mcHorRefineAVX2 filters two rows together for refinement widths 5, 9 and
// 17. The caller checks AVX2, disjoint source/destination spans and nonnegative
// strides. Each row reads only src[-2:width+3] and writes exactly width bytes;
// odd heights do not require a padded final row.
//
//go:noescape
func mcHorRefineAVX2(dst *byte, dstStride int, src *byte, srcStride, width, height int)
