package encoder

// A small snapshot of the effective DC quantization parameters. The source
// tables remain mutable: use a cached threshold only when both live parameters
// still equal this tuple. Other quantization table entries are not cached.
var chromaSkipQPThresholds = func() [52]struct{ ff, mf, threshold int16 } {
	var result [52]struct{ ff, mf, threshold int16 }
	for qp := range result {
		ff, mf := int16(int32(g_kiQuantInterFF[qp][0])<<1), g_kiQuantMF[qp][0]>>1
		result[qp].ff, result[qp].mf = ff, mf
		if mf != 0 {
			result[qp].threshold = int16(65535/int32(mf) - int32(ff))
		}
	}
	return result
}()

func hadamardQuant2x2SkipQP(rs []int16, qp uint8, ff, mf int16) int32 {
	if qp >= uint8(len(chromaSkipQPThresholds)) || mf == 0 {
		return WelsHadamardQuant2x2Skip_c(rs, ff, mf)
	}
	entry := &chromaSkipQPThresholds[qp]
	if ff != entry.ff || mf != entry.mf {
		return WelsHadamardQuant2x2Skip_c(rs, ff, mf)
	}
	threshold := int32(entry.threshold)

	// Preserve the scalar int16 wrap and DC read order. All four source
	// positions are read before any early return, including short inputs.
	s0 := rs[0] + rs[32]
	s1 := rs[0] - rs[32]
	s2 := rs[16] + rs[48]
	s3 := rs[16] - rs[48]
	a, b := int32(s0+s2), int32(s0-s2)
	c, d := int32(s1+s3), int32(s1-s3)
	// The widened signed comparisons include abs(MinInt16)==32768 and
	// preserve the scalar result even when the cached int16 threshold is <0.
	if a > threshold || a < -threshold || b > threshold || b < -threshold ||
		c > threshold || c < -threshold || d > threshold || d < -threshold {
		return 1
	}
	return 0
}
