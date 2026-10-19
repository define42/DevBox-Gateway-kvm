//go:build amd64 && !purego

#include "textflag.h"

// VPSHUFB operates independently in each 128-bit lane. Swap B/R and retain G/A.
DATA ·bgrToRGBAShuffle<>+0(SB)/8, $0x0704050603000102
DATA ·bgrToRGBAShuffle<>+8(SB)/8, $0x0f0c0d0e0b08090a
DATA ·bgrToRGBAShuffle<>+16(SB)/8, $0x0704050603000102
DATA ·bgrToRGBAShuffle<>+24(SB)/8, $0x0f0c0d0e0b08090a
GLOBL ·bgrToRGBAShuffle<>(SB), RODATA|NOPTR, $32

// func bgrToRGBAAVX2(dst, src *byte, n int, alpha uint32)
// n is a positive multiple of 32; the Go wrapper handles the suffix.
TEXT ·bgrToRGBAAVX2(SB), NOSPLIT, $0-28
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ n+16(FP), CX
	MOVL alpha+24(FP), AX
	VMOVD AX, X2
	VPBROADCASTD X2, Y2
	VMOVDQU ·bgrToRGBAShuffle<>(SB), Y1

bgrToRGBALoop:
	VMOVDQU (SI), Y0
	VPSHUFB Y1, Y0, Y0
	VPOR Y2, Y0, Y0
	VMOVDQU Y0, (DI)
	ADDQ $32, SI
	ADDQ $32, DI
	SUBQ $32, CX
	JNZ bgrToRGBALoop
	VZEROUPPER
	RET
