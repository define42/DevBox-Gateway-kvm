//go:build amd64

#include "textflag.h"

// Copy exactly 16 bytes per row; padding and neighboring macroblocks are untouched.
TEXT ·copyReconstructed16x16SSE2(SB), NOSPLIT, $0-64
	MOVQ dst_base+0(FP), AX
	MOVQ dstStride+24(FP), CX
	MOVQ src_base+32(FP), DX
	MOVQ srcStride+56(FP), BX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVOU (DX), X0
	MOVOU X0, (AX)
	RET

// Copy exactly 8 bytes per row; padding and neighboring macroblocks are untouched.
TEXT ·copyReconstructed8x8SSE2(SB), NOSPLIT, $0-64
	MOVQ dst_base+0(FP), AX
	MOVQ dstStride+24(FP), CX
	MOVQ src_base+32(FP), DX
	MOVQ srcStride+56(FP), BX
	MOVQ (DX), R8
	MOVQ R8, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVQ (DX), R8
	MOVQ R8, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVQ (DX), R8
	MOVQ R8, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVQ (DX), R8
	MOVQ R8, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVQ (DX), R8
	MOVQ R8, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVQ (DX), R8
	MOVQ R8, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVQ (DX), R8
	MOVQ R8, (AX)
	ADDQ CX, AX
	ADDQ BX, DX
	MOVQ (DX), R8
	MOVQ R8, (AX)
	RET
