//go:build amd64

#include "textflag.h"

// Each row is loaded completely before it is stored, matching the per-row
// copy() of welsCopyNxM even when source and destination rows overlap.

// func copyRows16(dst *byte, dstStride int, src *byte, srcStride int, height int)
TEXT ·copyRows16(SB), NOSPLIT, $0-40
	MOVQ dst+0(FP), DI
	MOVQ dstStride+8(FP), DX
	MOVQ src+16(FP), SI
	MOVQ srcStride+24(FP), BX
	MOVQ height+32(FP), CX
loop16:
	MOVOU (SI), X0
	MOVOU X0, (DI)
	ADDQ  BX, SI
	ADDQ  DX, DI
	DECQ  CX
	JNZ   loop16
	RET

// func copyRows8(dst *byte, dstStride int, src *byte, srcStride int, height int)
TEXT ·copyRows8(SB), NOSPLIT, $0-40
	MOVQ dst+0(FP), DI
	MOVQ dstStride+8(FP), DX
	MOVQ src+16(FP), SI
	MOVQ srcStride+24(FP), BX
	MOVQ height+32(FP), CX
loop8:
	MOVQ (SI), AX
	MOVQ AX, (DI)
	ADDQ BX, SI
	ADDQ DX, DI
	DECQ CX
	JNZ  loop8
	RET

// func copyRows4(dst *byte, dstStride int, src *byte, srcStride int, height int)
TEXT ·copyRows4(SB), NOSPLIT, $0-40
	MOVQ dst+0(FP), DI
	MOVQ dstStride+8(FP), DX
	MOVQ src+16(FP), SI
	MOVQ srcStride+24(FP), BX
	MOVQ height+32(FP), CX
loop4:
	MOVL (SI), AX
	MOVL AX, (DI)
	ADDQ BX, SI
	ADDQ DX, DI
	DECQ CX
	JNZ  loop4
	RET
