package processing

// Port of codec/processing/src/vaacalc/vaacalcfuncs.cpp.
//
// The C code spells out the four 8x8 sub-blocks of every macroblock (offsets
// 0, 8, 8*stride, 8*stride+8, in that order); here they are a loop over the
// same offsets in the same order, which gives identical results.

func vaaSubBlockOffsets(pic_stride_x8 int32) [4]int {
	return [4]int{0, 8, int(pic_stride_x8), int(pic_stride_x8) + 8}
}

func VAACalcSadSsd_c(pCurData []uint8, iCurOff int, pRefData []uint8, iRefOff int, iPicWidth int32, iPicHeight int32,
	iPicStride int32,
	pFrameSad *int32, pSad8x8 [][4]int32, pSum16x16 []int32, psqsum16x16 []int32, psqdiff16x16 []int32) {
	tmp_ref := iRefOff
	tmp_cur := iCurOff
	iMbWidth := (iPicWidth >> 4)
	mb_height := (iPicHeight >> 4)
	mb_index := 0
	pic_stride_x8 := iPicStride << 3
	step := (iPicStride << 4) - iPicWidth
	kaOff := vaaSubBlockOffsets(pic_stride_x8)

	*pFrameSad = 0
	for i := int32(0); i < mb_height; i++ {
		for j := int32(0); j < iMbWidth; j++ {
			pSum16x16[mb_index] = 0
			psqsum16x16[mb_index] = 0
			psqdiff16x16[mb_index] = 0

			for n := 0; n < 4; n++ {
				var l_sad, l_sqdiff, l_sum, l_sqsum int32
				tmp_cur_row := tmp_cur + kaOff[n]
				tmp_ref_row := tmp_ref + kaOff[n]
				for k := 0; k < 8; k++ {
					for l := 0; l < 8; l++ {
						c := int32(pCurData[tmp_cur_row+l])
						diff := WELS_ABS(c - int32(pRefData[tmp_ref_row+l]))
						l_sad += diff
						l_sqdiff += diff * diff
						l_sum += c
						l_sqsum += c * c
					}
					tmp_cur_row += int(iPicStride)
					tmp_ref_row += int(iPicStride)
				}
				*pFrameSad += l_sad
				pSad8x8[mb_index][n] = l_sad
				pSum16x16[mb_index] += l_sum
				psqsum16x16[mb_index] += l_sqsum
				psqdiff16x16[mb_index] += l_sqdiff
			}

			tmp_ref += 16
			tmp_cur += 16
			mb_index++
		}
		tmp_ref += int(step)
		tmp_cur += int(step)
	}
}

func VAACalcSadVar_c(pCurData []uint8, iCurOff int, pRefData []uint8, iRefOff int, iPicWidth int32, iPicHeight int32,
	iPicStride int32,
	pFrameSad *int32, pSad8x8 [][4]int32, pSum16x16 []int32, psqsum16x16 []int32) {
	tmp_ref := iRefOff
	tmp_cur := iCurOff
	iMbWidth := (iPicWidth >> 4)
	mb_height := (iPicHeight >> 4)
	mb_index := 0
	pic_stride_x8 := iPicStride << 3
	step := (iPicStride << 4) - iPicWidth
	kaOff := vaaSubBlockOffsets(pic_stride_x8)

	*pFrameSad = 0
	for i := int32(0); i < mb_height; i++ {
		for j := int32(0); j < iMbWidth; j++ {
			pSum16x16[mb_index] = 0
			psqsum16x16[mb_index] = 0

			for n := 0; n < 4; n++ {
				var l_sad, l_sum, l_sqsum int32
				tmp_cur_row := tmp_cur + kaOff[n]
				tmp_ref_row := tmp_ref + kaOff[n]
				for k := 0; k < 8; k++ {
					for l := 0; l < 8; l++ {
						c := int32(pCurData[tmp_cur_row+l])
						diff := WELS_ABS(c - int32(pRefData[tmp_ref_row+l]))
						l_sad += diff
						l_sum += c
						l_sqsum += c * c
					}
					tmp_cur_row += int(iPicStride)
					tmp_ref_row += int(iPicStride)
				}
				*pFrameSad += l_sad
				pSad8x8[mb_index][n] = l_sad
				pSum16x16[mb_index] += l_sum
				psqsum16x16[mb_index] += l_sqsum
			}

			tmp_ref += 16
			tmp_cur += 16
			mb_index++
		}
		tmp_ref += int(step)
		tmp_cur += int(step)
	}
}

func VAACalcSad_c(pCurData []uint8, iCurOff int, pRefData []uint8, iRefOff int, iPicWidth int32, iPicHeight int32,
	iPicStride int32,
	pFrameSad *int32, pSad8x8 [][4]int32) {
	tmp_ref := iRefOff
	tmp_cur := iCurOff
	iMbWidth := (iPicWidth >> 4)
	mb_height := (iPicHeight >> 4)
	mb_index := 0
	pic_stride_x8 := iPicStride << 3
	step := (iPicStride << 4) - iPicWidth
	kaOff := vaaSubBlockOffsets(pic_stride_x8)

	*pFrameSad = 0
	for i := int32(0); i < mb_height; i++ {
		for j := int32(0); j < iMbWidth; j++ {
			for n := 0; n < 4; n++ {
				var l_sad int32
				tmp_cur_row := tmp_cur + kaOff[n]
				tmp_ref_row := tmp_ref + kaOff[n]
				for k := 0; k < 8; k++ {
					for l := 0; l < 8; l++ {
						diff := WELS_ABS(int32(pCurData[tmp_cur_row+l]) - int32(pRefData[tmp_ref_row+l]))
						l_sad += diff
					}
					tmp_cur_row += int(iPicStride)
					tmp_ref_row += int(iPicStride)
				}
				*pFrameSad += l_sad
				pSad8x8[mb_index][n] = l_sad
			}

			tmp_ref += 16
			tmp_cur += 16
			mb_index++
		}
		tmp_ref += int(step)
		tmp_cur += int(step)
	}
}

func VAACalcSadSsdBgd_c(pCurData []uint8, iCurOff int, pRefData []uint8, iRefOff int, iPicWidth int32, iPicHeight int32,
	iPicStride int32,
	pFrameSad *int32, pSad8x8 [][4]int32, pSum16x16 []int32, psqsum16x16 []int32, psqdiff16x16 []int32, pSd8x8 [][4]int32,
	pMad8x8 [][4]uint8) {
	tmp_ref := iRefOff
	tmp_cur := iCurOff
	iMbWidth := (iPicWidth >> 4)
	mb_height := (iPicHeight >> 4)
	mb_index := 0
	pic_stride_x8 := iPicStride << 3
	step := (iPicStride << 4) - iPicWidth
	kaOff := vaaSubBlockOffsets(pic_stride_x8)

	*pFrameSad = 0
	for i := int32(0); i < mb_height; i++ {
		for j := int32(0); j < iMbWidth; j++ {
			pSum16x16[mb_index] = 0
			psqsum16x16[mb_index] = 0
			psqdiff16x16[mb_index] = 0

			for n := 0; n < 4; n++ {
				var l_sad, l_sqdiff, l_sum, l_sqsum, l_sd, l_mad int32
				tmp_cur_row := tmp_cur + kaOff[n]
				tmp_ref_row := tmp_ref + kaOff[n]
				for k := 0; k < 8; k++ {
					for l := 0; l < 8; l++ {
						c := int32(pCurData[tmp_cur_row+l])
						diff := c - int32(pRefData[tmp_ref_row+l])
						abs_diff := WELS_ABS(diff)

						l_sd += diff
						if abs_diff > l_mad {
							l_mad = abs_diff
						}
						l_sad += abs_diff
						l_sqdiff += abs_diff * abs_diff
						l_sum += c
						l_sqsum += c * c
					}
					tmp_cur_row += int(iPicStride)
					tmp_ref_row += int(iPicStride)
				}
				*pFrameSad += l_sad
				pSad8x8[mb_index][n] = l_sad
				pSum16x16[mb_index] += l_sum
				psqsum16x16[mb_index] += l_sqsum
				psqdiff16x16[mb_index] += l_sqdiff
				pSd8x8[mb_index][n] = l_sd
				pMad8x8[mb_index][n] = uint8(l_mad)
			}

			tmp_ref += 16
			tmp_cur += 16
			mb_index++
		}
		tmp_ref += int(step)
		tmp_cur += int(step)
	}
}

func VAACalcSadBgd_c(pCurData []uint8, iCurOff int, pRefData []uint8, iRefOff int, iPicWidth int32, iPicHeight int32,
	iPicStride int32,
	pFrameSad *int32, pSad8x8 [][4]int32, pSd8x8 [][4]int32, pMad8x8 [][4]uint8) {
	tmp_ref := iRefOff
	tmp_cur := iCurOff
	iMbWidth := (iPicWidth >> 4)
	mb_height := (iPicHeight >> 4)
	mb_index := 0
	pic_stride_x8 := iPicStride << 3
	step := (iPicStride << 4) - iPicWidth
	kaOff := vaaSubBlockOffsets(pic_stride_x8)

	*pFrameSad = 0
	for i := int32(0); i < mb_height; i++ {
		for j := int32(0); j < iMbWidth; j++ {
			for n := 0; n < 4; n++ {
				var l_sad, l_sd, l_mad int32
				tmp_cur_row := tmp_cur + kaOff[n]
				tmp_ref_row := tmp_ref + kaOff[n]
				for k := 0; k < 8; k++ {
					for l := 0; l < 8; l++ {
						diff := int32(pCurData[tmp_cur_row+l]) - int32(pRefData[tmp_ref_row+l])
						abs_diff := WELS_ABS(diff)
						l_sd += diff
						l_sad += abs_diff
						if abs_diff > l_mad {
							l_mad = abs_diff
						}
					}
					tmp_cur_row += int(iPicStride)
					tmp_ref_row += int(iPicStride)
				}
				*pFrameSad += l_sad
				pSad8x8[mb_index][n] = l_sad
				pSd8x8[mb_index][n] = l_sd
				pMad8x8[mb_index][n] = uint8(l_mad)
			}

			tmp_ref += 16
			tmp_cur += 16
			mb_index++
		}
		tmp_ref += int(step)
		tmp_cur += int(step)
	}
}

// vaaCalcSadMasked calculates changed runs within each macroblock row. A valid
// hint certifies source equality, so the skipped SADs are exactly zero. The
// supplied kernel keeps the CPU dispatch and the portable scalar fallback.
func vaaCalcSadMasked(calc VAACalcSadFunc, cur []byte, curOff int, ref []byte, refOff int,
	width, height, stride int32, frameSad *int32, sad [][4]int32, unchanged []bool) {
	count := int64(width>>4) * int64(height>>4)
	if width <= 0 || height <= 0 || width&15 != 0 || height&15 != 0 ||
		int64(len(unchanged)) != count {
		calc(cur, curOff, ref, refOff, width, height, stride, frameSad, sad)
		return
	}
	mbWidth := int(width >> 4)
	if count > int64(len(sad)) {
		panic("h264: VAA output span out of range")
	}
	clear(sad[:int(count)])
	*frameSad = 0
	var total int32
	for y := 0; y < int(height>>4); y++ {
		row := y * mbWidth
		for x := 0; x < mbWidth; {
			if unchanged[row+x] {
				x++
				continue
			}
			start := x
			for x < mbWidth && !unchanged[row+x] {
				x++
			}
			off := y*16*int(stride) + start*16
			// Reuse the caller-owned result as kernel scratch; passing a local
			// result through the dispatched function would allocate per run.
			calc(cur, curOff+off, ref, refOff+off, int32(x-start)*16, 16, stride,
				frameSad, sad[row+start:row+x])
			total += *frameSad
		}
	}
	*frameSad = total
}
