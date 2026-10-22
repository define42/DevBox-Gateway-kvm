//go:build amd64

#include "textflag.h"

// SUM4ROWS sums each 4x4 block of four 16-pixel rows into dwords and
// divides by 16: PMADDUBSW adds horizontal pixel pairs, PADDW adds rows and
// PMADDWL adds the two pairs of each block.
#define SUM4ROWS(out, tmp) \
	MOVOU     (SI), out; \
	PMADDUBSW X7, out; \
	MOVOU     (SI)(BX*1), tmp; \
	PMADDUBSW X7, tmp; \
	PADDW     tmp, out; \
	MOVOU     (SI)(BX*2), tmp; \
	PMADDUBSW X7, tmp; \
	PADDW     tmp, out; \
	MOVOU     (SI)(DX*1), tmp; \
	PMADDUBSW X7, tmp; \
	PADDW     tmp, out; \
	PMADDWL   X6, out; \
	PSRLL     $4, out; \
	LEAQ      (SI)(BX*4), SI

// func vaaIntraSumsSSSE3(src *byte, stride int) (sumAvg, sumSqr int32)
// Requires: SSSE3
TEXT ·vaaIntraSumsSSSE3(SB), NOSPLIT, $0-24
	MOVQ src+0(FP), SI
	MOVQ stride+8(FP), BX
	LEAQ (BX)(BX*2), DX
	MOVQ $0x0101010101010101, AX
	MOVQ AX, X7
	PUNPCKLQDQ X7, X7
	MOVQ $0x0001000100010001, AX
	MOVQ AX, X6
	PUNPCKLQDQ X6, X6

	SUM4ROWS(X0, X5)
	SUM4ROWS(X1, X5)
	SUM4ROWS(X2, X5)
	SUM4ROWS(X3, X5)

	// Averages are at most 255, so packing to words is exact.
	PACKSSLW X1, X0
	PACKSSLW X3, X2

	MOVO    X0, X4
	PMADDWL X6, X4
	MOVO    X2, X5
	PMADDWL X6, X5
	PADDL   X5, X4

	PMADDWL X0, X0
	PMADDWL X2, X2
	PADDL   X2, X0

	PSHUFD $0x4e, X4, X5
	PADDL  X5, X4
	PSHUFD $0xb1, X4, X5
	PADDL  X5, X4
	PSHUFD $0x4e, X0, X5
	PADDL  X5, X0
	PSHUFD $0xb1, X0, X5
	PADDL  X5, X0

	MOVQ X4, AX
	MOVL AX, sumAvg+16(FP)
	MOVQ X0, AX
	MOVL AX, sumSqr+20(FP)
	RET
