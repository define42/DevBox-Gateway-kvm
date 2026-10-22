//go:build amd64

#include "textflag.h"
// func copy8x16SSE2(dst *byte, dstStride int, src *byte, srcStride int)
TEXT ·copy8x16SSE2(SB), NOSPLIT, $0-32
	MOVQ dst+0(FP), AX
	MOVQ dstStride+8(FP), CX
	MOVQ src+16(FP), DX
	MOVQ srcStride+24(FP), BX
	LEAQ (CX)(CX*2), R8
	LEAQ (BX)(BX*2), R9
	MOVQ (DX), X0
	MOVQ (DX)(BX*1), X1
	MOVQ (DX)(BX*2), X2
	MOVQ (DX)(R9*1), X3
	MOVQ X0, (AX)
	MOVQ X1, (AX)(CX*1)
	MOVQ X2, (AX)(CX*2)
	MOVQ X3, (AX)(R8*1)
	LEAQ (DX)(BX*4), DX
	LEAQ (AX)(CX*4), AX
	MOVQ (DX), X0
	MOVQ (DX)(BX*1), X1
	MOVQ (DX)(BX*2), X2
	MOVQ (DX)(R9*1), X3
	MOVQ X0, (AX)
	MOVQ X1, (AX)(CX*1)
	MOVQ X2, (AX)(CX*2)
	MOVQ X3, (AX)(R8*1)
	LEAQ (DX)(BX*4), DX
	LEAQ (AX)(CX*4), AX
	MOVQ (DX), X0
	MOVQ (DX)(BX*1), X1
	MOVQ (DX)(BX*2), X2
	MOVQ (DX)(R9*1), X3
	MOVQ X0, (AX)
	MOVQ X1, (AX)(CX*1)
	MOVQ X2, (AX)(CX*2)
	MOVQ X3, (AX)(R8*1)
	LEAQ (DX)(BX*4), DX
	LEAQ (AX)(CX*4), AX
	MOVQ (DX), X0
	MOVQ (DX)(BX*1), X1
	MOVQ (DX)(BX*2), X2
	MOVQ (DX)(R9*1), X3
	MOVQ X0, (AX)
	MOVQ X1, (AX)(CX*1)
	MOVQ X2, (AX)(CX*2)
	MOVQ X3, (AX)(R8*1)
	RET

// func copy16x16SSE2(dst *byte, dstStride int, src *byte, srcStride int)
TEXT ·copy16x16SSE2(SB), NOSPLIT, $0-32
	MOVQ dst+0(FP), AX
	MOVQ dstStride+8(FP), CX
	MOVQ src+16(FP), DX
	MOVQ srcStride+24(FP), BX
	LEAQ (CX)(CX*2), R8
	LEAQ (BX)(BX*2), R9
	MOVOU (DX), X0
	MOVOU (DX)(BX*1), X1
	MOVOU (DX)(BX*2), X2
	MOVOU (DX)(R9*1), X3
	MOVOU X0, (AX)
	MOVOU X1, (AX)(CX*1)
	MOVOU X2, (AX)(CX*2)
	MOVOU X3, (AX)(R8*1)
	LEAQ (DX)(BX*4), DX
	LEAQ (AX)(CX*4), AX
	MOVOU (DX), X0
	MOVOU (DX)(BX*1), X1
	MOVOU (DX)(BX*2), X2
	MOVOU (DX)(R9*1), X3
	MOVOU X0, (AX)
	MOVOU X1, (AX)(CX*1)
	MOVOU X2, (AX)(CX*2)
	MOVOU X3, (AX)(R8*1)
	LEAQ (DX)(BX*4), DX
	LEAQ (AX)(CX*4), AX
	MOVOU (DX), X0
	MOVOU (DX)(BX*1), X1
	MOVOU (DX)(BX*2), X2
	MOVOU (DX)(R9*1), X3
	MOVOU X0, (AX)
	MOVOU X1, (AX)(CX*1)
	MOVOU X2, (AX)(CX*2)
	MOVOU X3, (AX)(R8*1)
	LEAQ (DX)(BX*4), DX
	LEAQ (AX)(CX*4), AX
	MOVOU (DX), X0
	MOVOU (DX)(BX*1), X1
	MOVOU (DX)(BX*2), X2
	MOVOU (DX)(R9*1), X3
	MOVOU X0, (AX)
	MOVOU X1, (AX)(CX*1)
	MOVOU X2, (AX)(CX*2)
	MOVOU X3, (AX)(R8*1)
	RET
