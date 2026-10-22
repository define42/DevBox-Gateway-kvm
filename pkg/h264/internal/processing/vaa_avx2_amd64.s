//go:build amd64

#include "textflag.h"

// Eight rows use four independent accumulators. X registers select the exact
// 16-byte tail, Y registers calculate two macroblocks at once. SI/DX point at
// the first source/reference row; R9/R10/R11 hold 3/5/7 times the stride.
#define VAA_EIGHT_ROWS(A, B, C, D, TMP, OUT) \
	VMOVDQU (SI), TMP; \
	VPSADBW (DX), TMP, A; \
	VMOVDQU (SI)(CX*1), TMP; \
	VPSADBW (DX)(CX*1), TMP, B; \
	VMOVDQU (SI)(CX*2), TMP; \
	VPSADBW (DX)(CX*2), TMP, C; \
	VMOVDQU (SI)(R9*1), TMP; \
	VPSADBW (DX)(R9*1), TMP, D; \
	VMOVDQU (SI)(CX*4), TMP; \
	VPSADBW (DX)(CX*4), TMP, TMP; \
	VPADDD TMP, A, A; \
	VMOVDQU (SI)(R10*1), TMP; \
	VPSADBW (DX)(R10*1), TMP, TMP; \
	VPADDD TMP, B, B; \
	VMOVDQU (SI)(R9*2), TMP; \
	VPSADBW (DX)(R9*2), TMP, TMP; \
	VPADDD TMP, C, C; \
	VMOVDQU (SI)(R11*1), TMP; \
	VPSADBW (DX)(R11*1), TMP, TMP; \
	VPADDD TMP, D, D; \
	VPADDD B, A, A; \
	VPADDD D, C, C; \
	VPADDD C, A, OUT

// Returns the row's wrapped int32 sum. The caller retains the original
// frame-SAD update after each 16-pixel row band, including pointer aliases.
TEXT ·vaaSadRowAVX2(SB), NOSPLIT, $0-44
	MOVQ cur+0(FP), AX
	MOVQ ref+8(FP), BX
	MOVQ stride+16(FP), CX
	MOVQ sad+24(FP), DI
	MOVQ macroblocks+32(FP), R8
	VPXOR Y15, Y15, Y15
	TESTQ R8, R8
	JLE vaa_avx2_done
	LEAQ (CX)(CX*2), R9
	LEAQ (CX)(CX*4), R10
	LEAQ (CX)(R9*2), R11
	CMPQ R8, $2
	JL vaa_avx2_tail
vaa_avx2_pair:
	MOVQ AX, SI
	MOVQ BX, DX
	VAA_EIGHT_ROWS(Y0, Y1, Y2, Y3, Y8, Y4)
	LEAQ (SI)(CX*8), SI
	LEAQ (DX)(CX*8), DX
	VAA_EIGHT_ROWS(Y0, Y1, Y2, Y3, Y8, Y5)
	VPADDD Y4, Y15, Y15
	VPADDD Y5, Y15, Y15
	// Pack TL/TR/BL/BR for each macroblock independently in its 128-bit lane.
	VPSHUFD $0x88, Y4, Y6
	VPSHUFD $0x88, Y5, Y7
	VPUNPCKLQDQ Y7, Y6, Y6
	VMOVDQU Y6, (DI)
	ADDQ $32, AX
	ADDQ $32, BX
	ADDQ $32, DI
	SUBQ $2, R8
	CMPQ R8, $2
	JGE vaa_avx2_pair
	TESTQ R8, R8
	JZ vaa_avx2_done
vaa_avx2_tail:
	MOVQ AX, SI
	MOVQ BX, DX
	VAA_EIGHT_ROWS(X0, X1, X2, X3, X8, X4)
	LEAQ (SI)(CX*8), SI
	LEAQ (DX)(CX*8), DX
	VAA_EIGHT_ROWS(X0, X1, X2, X3, X8, X5)
	// VEX 128-bit writes zero the unused upper lanes of Y4/Y5.
	VPADDD Y4, Y15, Y15
	VPADDD Y5, Y15, Y15
	VPSHUFD $0x88, X4, X6
	VPSHUFD $0x88, X5, X7
	VPUNPCKLQDQ X7, X6, X6
	VMOVDQU X6, (DI)
vaa_avx2_done:
	VEXTRACTI128 $1, Y15, X0
	VPADDD X15, X0, X0
	VPSRLDQ $8, X0, X1
	VPADDD X1, X0, X0
	VMOVQ X0, AX
	MOVL AX, ret+40(FP)
	VZEROUPPER
	RET
