//go:build amd64

package encoder

// These leaves consume copied values, not reference pixels. Gathering in the
// scalar predictor's order keeps exact byte reads, panic precedence and all
// source/destination aliases, including negative and overlapping strides.
//
//go:noescape
func intra4PredHDSSE2(dst *byte, samples uint64)

//go:noescape
func intra4PredHUSSE2(dst *byte, samples uint64)

//go:noescape
func intra4PredVRSSE2(dst *byte, samples uint64)

func WelsI4x4LumaPredHD_sse2(dst []byte, off int, ref []byte, refOff int, stride int32) {
	s := int(stride)
	lt := uint64(ref[refOff+(-s-1)])
	l0 := uint64(ref[refOff-1])
	l1 := uint64(ref[refOff+(s-1)])
	l2 := uint64(ref[refOff+((s<<1)-1)])
	l3 := uint64(ref[refOff+(s+(s<<1)-1)])
	t0 := uint64(ref[refOff-s])
	t1 := uint64(ref[refOff+(1-s)])
	t2 := uint64(ref[refOff+(2-s)])
	// Low to high: L3 L2 L1 L0 LT T0 T1 T2.
	packed := l3 | l2<<8 | l1<<16 | l0<<24 | lt<<32 | t0<<40 | t1<<48 | t2<<56
	block := dst[off : off+16]
	intra4PredHDSSE2(&block[0], packed)
}

func WelsI4x4LumaPredHU_sse2(dst []byte, off int, ref []byte, refOff int, stride int32) {
	s := int(stride)
	l0 := uint64(ref[refOff-1])
	l1 := uint64(ref[refOff+(s-1)])
	l2 := uint64(ref[refOff+((s<<1)-1)])
	l3 := uint64(ref[refOff+(s+(s<<1)-1)])
	// Repeated L3 extends the left edge without additional pixel reads.
	packed := l0 | l1<<8 | l2<<16 | l3*0x0101010101000000
	block := dst[off : off+16]
	intra4PredHUSSE2(&block[0], packed)
}

func WelsI4x4LumaPredVR_sse2(dst []byte, off int, ref []byte, refOff int, stride int32) {
	s := int(stride)
	lt := uint64(ref[refOff+(-s-1)])
	l0 := uint64(ref[refOff-1])
	l1 := uint64(ref[refOff+(s-1)])
	l2 := uint64(ref[refOff+((s<<1)-1)])
	t0 := uint64(ref[refOff-s])
	t1 := uint64(ref[refOff+(1-s)])
	t2 := uint64(ref[refOff+(2-s)])
	t3 := uint64(ref[refOff+(3-s)])
	// Low to high: L2 L1 L0 LT T0 T1 T2 T3.
	packed := l2 | l1<<8 | l0<<16 | lt<<24 | t0<<32 | t1<<40 | t2<<48 | t3<<56
	block := dst[off : off+16]
	intra4PredVRSSE2(&block[0], packed)
}
