//go:build amd64

package encoder

// The complete kernels consume every top/left reference before storing output,
// preserving aliases. They read only the corner, the weighted top samples, and
// single-byte left samples; no extra horizontal halo is required.
//
//go:noescape
func intraPredPlane16FullSSE2(dst, corner *byte, stride int)

//go:noescape
func intraPredPlane8FullSSE2(dst, corner *byte, stride int)

func intraPlaneReferencesFit(ref []byte, off int, stride int32, size int) bool {
	s := int(stride)
	// Positive strides make the corner and final left sample the endpoints.
	// Subtract only after off is bounded; size<=16 and int32 strides make the
	// product safe on amd64. Invalid/nonpositive cases retain the old path,
	// including its source-first panic order and output slice check.
	return s > 0 && off > s && off <= len(ref) &&
		(size-1)*s <= len(ref)-off && size <= len(ref)-(off-s)
}
