//go:build amd64

package encoder

import (
	"reflect"
	"unsafe"

	"golang.org/x/sys/cpu"
)

// The exact builtin check preserves instrumentation and callbacks that replace
// a transform after normal initialization. A wrapper around the builtin must
// still run. The reflection check occurs once per lazy 16x16 skip attempt.
var skipDctQuantBuiltinPC = reflect.ValueOf(WelsDctFourT4_sse2).Pointer()

// The caller validates exact spans and requires coefficient output to be
// disjoint from both pixel inputs and FF/MF; maxima is private stack scratch.
//
//go:noescape
func skipDctQuant8x8AVX2(dct *[64]int16, pixel1 *byte, stride1 int,
	pixel2 *byte, stride2 int, ff, mf *[8]int16, maxima *[4]int16)

// trySkipDctQuantPixels fuses only the built-in lazy skip path. The complete
// preflight does not write or panic: unusual inputs retain the original
// per-group checks, callback order, partial writes and early rejection. No
// future group is transformed before its predecessor has passed the skip test.
func trySkipDctQuantPixels(ctx *sWelsEncCtx, cache *SMbCache, source []byte,
	sourceOffset int, stride int32, qp uint8) (handled, accepted bool) {
	if !cpu.X86.HasAVX2 || ctx.pFuncList == nil || reflect.ValueOf(ctx.pFuncList.pfDctFourT4).Pointer() != skipDctQuantBuiltinPC {
		return false, false
	}
	if qp >= 52 || cache.pDct == nil || len(cache.pCoeffLevel) < 256 || len(cache.pSkipMb) < 256 ||
		stride < 0 || sourceOffset < 0 || sourceOffset > len(source) || len(source)-sourceOffset < 16 ||
		int64(stride)*15 > int64(len(source)-sourceOffset-16) {
		return false, false
	}
	coefficients := cache.pCoeffLevel
	prediction := cache.pSkipMb
	ff, mf := &g_kiQuantInterFF[qp], &g_kiQuantMF[qp]
	dctStart := unsafe.Pointer(&coefficients[0])
	// The existing DCT snapshots all pixels only on its disjoint AVX2 path.
	// FF/MF must also be disjoint from the DCT output: the old sequence loads
	// them after the transform has written its unquantized coefficients.
	if quantScanRangesOverlap(dctStart, 512, unsafe.Pointer(&source[sourceOffset]), uintptr(stride)*15+16) ||
		quantScanRangesOverlap(dctStart, 512, unsafe.Pointer(&prediction[0]), 256) ||
		quantScanRangesOverlap(dctStart, 512, unsafe.Pointer(ff), 16) ||
		quantScanRangesOverlap(dctStart, 512, unsafe.Pointer(mf), 16) {
		return false, false
	}
	var score int32
	var maximum [4]int16
	for group := 0; group < 4; group++ {
		x, y := (group&1)*8, (group>>1)*8
		offset := group * 64
		// Keep each group's scan after its complete quantization, including
		// zero-max blocks whose scanned scratch must remain untouched.
		levels := cache.pDct.iLumaBlock[offset:]
		skipDctQuant8x8AVX2((*[64]int16)(coefficients[offset:]),
			&source[sourceOffset+y*int(stride)+x], int(stride),
			&prediction[y*16+x], 16, ff, mf, &maximum)
		for block, value := range maximum {
			if uint16(value) > 1 {
				return true, false
			}
			if value == 1 {
				blockOffset := block * 16
				mask := coefficientScanMask(levels[blockOffset:], coefficients[offset+blockOffset:], false)
				score += coefficientScoreMask(mask)
			}
			if score >= 6 {
				return true, false
			}
		}
	}
	return true, true
}
