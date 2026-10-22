//go:build amd64

package encoder

//go:noescape
func updateMbMvSSE2(dst *[16]SMVUnitXY, packed uint32)

// UpdateMbMv_sse2 fills exactly sixteen motion vectors. SSE2 is part of the
// amd64 baseline; the initializer still respects the requested feature mask.
func UpdateMbMv_sse2(dst []SMVUnitXY, value SMVUnitXY) {
	if len(dst) < 16 {
		// The scalar API writes the available prefix before an index panic.
		UpdateMbMv_c(dst, value)
		return
	}
	packed := uint32(uint16(value.iMvX)) | uint32(uint16(value.iMvY))<<16
	updateMbMvSSE2((*[16]SMVUnitXY)(dst), packed)
}
