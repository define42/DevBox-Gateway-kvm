//go:build amd64

// Derived from github.com/oops1/go.264 v1.12.0/internal/simd.
// Copyright (c) 2026 Valery Chukalin. BSD-2-Clause; see LICENSE-go.264.

#include "textflag.h"
// func sad16x16(src *byte, srcStride int, ref *byte, refStride int) int
// Requires: SSE2. Four-row addressing and independent sums shorten dependency chains.
TEXT ·sad16x16(SB), NOSPLIT, $0-40
	MOVQ src+0(FP), AX
	MOVQ srcStride+8(FP), CX
	MOVQ ref+16(FP), DX
	MOVQ refStride+24(FP), BX
	LEAQ (CX)(CX*2), R8
	LEAQ (BX)(BX*2), R9
	PXOR X6, X6
	PXOR X7, X7
	MOVOU (AX), X0
	MOVOU (DX), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(CX*1), X1
	MOVOU (DX)(BX*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	MOVOU (AX)(CX*2), X0
	MOVOU (DX)(BX*2), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(R8*1), X1
	MOVOU (DX)(R9*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	LEAQ (AX)(CX*4), AX
	LEAQ (DX)(BX*4), DX
	MOVOU (AX), X0
	MOVOU (DX), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(CX*1), X1
	MOVOU (DX)(BX*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	MOVOU (AX)(CX*2), X0
	MOVOU (DX)(BX*2), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(R8*1), X1
	MOVOU (DX)(R9*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	LEAQ (AX)(CX*4), AX
	LEAQ (DX)(BX*4), DX
	MOVOU (AX), X0
	MOVOU (DX), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(CX*1), X1
	MOVOU (DX)(BX*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	MOVOU (AX)(CX*2), X0
	MOVOU (DX)(BX*2), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(R8*1), X1
	MOVOU (DX)(R9*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	LEAQ (AX)(CX*4), AX
	LEAQ (DX)(BX*4), DX
	MOVOU (AX), X0
	MOVOU (DX), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(CX*1), X1
	MOVOU (DX)(BX*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	MOVOU (AX)(CX*2), X0
	MOVOU (DX)(BX*2), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(R8*1), X1
	MOVOU (DX)(R9*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	PADDW X7, X6
	MOVHLPS X6, X0
	PADDW X6, X0
	MOVQ X0, AX
	MOVQ AX, ret+32(FP)
	RET

// func sad16x8(src *byte, srcStride int, ref *byte, refStride int) int
// Requires: SSE2. Four-row addressing and independent sums shorten dependency chains.
TEXT ·sad16x8(SB), NOSPLIT, $0-40
	MOVQ src+0(FP), AX
	MOVQ srcStride+8(FP), CX
	MOVQ ref+16(FP), DX
	MOVQ refStride+24(FP), BX
	LEAQ (CX)(CX*2), R8
	LEAQ (BX)(BX*2), R9
	PXOR X6, X6
	PXOR X7, X7
	MOVOU (AX), X0
	MOVOU (DX), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(CX*1), X1
	MOVOU (DX)(BX*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	MOVOU (AX)(CX*2), X0
	MOVOU (DX)(BX*2), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(R8*1), X1
	MOVOU (DX)(R9*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	LEAQ (AX)(CX*4), AX
	LEAQ (DX)(BX*4), DX
	MOVOU (AX), X0
	MOVOU (DX), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(CX*1), X1
	MOVOU (DX)(BX*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	MOVOU (AX)(CX*2), X0
	MOVOU (DX)(BX*2), X2
	PSADBW X2, X0
	PADDW X0, X6
	MOVOU (AX)(R8*1), X1
	MOVOU (DX)(R9*1), X3
	PSADBW X3, X1
	PADDW X1, X7
	PADDW X7, X6
	MOVHLPS X6, X0
	PADDW X6, X0
	MOVQ X0, AX
	MOVQ AX, ret+32(FP)
	RET

// func sad8x16(src *byte, srcStride int, ref *byte, refStride int) int
// Requires: SSE2. Exact-width loads pack independent rows into both SAD lanes.
TEXT ·sad8x16(SB), NOSPLIT, $0-40
	MOVQ src+0(FP), AX
	MOVQ srcStride+8(FP), CX
	MOVQ ref+16(FP), DX
	MOVQ refStride+24(FP), BX
	LEAQ (CX)(CX*2), R8
	LEAQ (BX)(BX*2), R9
	MOVQ (AX), X0
	MOVQ (AX)(CX*1), X1
	MOVHPS (AX)(CX*2), X0
	MOVHPS (AX)(R8*1), X1
	MOVQ (DX), X2
	MOVQ (DX)(BX*1), X3
	MOVHPS (DX)(BX*2), X2
	MOVHPS (DX)(R9*1), X3
	PSADBW X2, X0
	PSADBW X3, X1
	PADDD X1, X0
	LEAQ (AX)(CX*4), AX
	LEAQ (DX)(BX*4), DX
	MOVQ (AX), X1
	MOVQ (AX)(CX*1), X2
	MOVHPS (AX)(CX*2), X1
	MOVHPS (AX)(R8*1), X2
	MOVQ (DX), X3
	MOVQ (DX)(BX*1), X4
	MOVHPS (DX)(BX*2), X3
	MOVHPS (DX)(R9*1), X4
	PSADBW X3, X1
	PSADBW X4, X2
	PADDD X1, X0
	PADDD X2, X0
	LEAQ (AX)(CX*4), AX
	LEAQ (DX)(BX*4), DX
	MOVQ (AX), X1
	MOVQ (AX)(CX*1), X2
	MOVHPS (AX)(CX*2), X1
	MOVHPS (AX)(R8*1), X2
	MOVQ (DX), X3
	MOVQ (DX)(BX*1), X4
	MOVHPS (DX)(BX*2), X3
	MOVHPS (DX)(R9*1), X4
	PSADBW X3, X1
	PSADBW X4, X2
	PADDD X1, X0
	PADDD X2, X0
	LEAQ (AX)(CX*4), AX
	LEAQ (DX)(BX*4), DX
	MOVQ (AX), X1
	MOVQ (AX)(CX*1), X2
	MOVHPS (AX)(CX*2), X1
	MOVHPS (AX)(R8*1), X2
	MOVQ (DX), X3
	MOVQ (DX)(BX*1), X4
	MOVHPS (DX)(BX*2), X3
	MOVHPS (DX)(R9*1), X4
	PSADBW X3, X1
	PSADBW X4, X2
	PADDD X1, X0
	PADDD X2, X0
	MOVOU X0, X1
	PSRLDQ $8, X1
	PADDD X1, X0
	MOVQ X0, AX
	MOVQ AX, ret+32(FP)
	RET

// func sad8x8(src *byte, srcStride int, ref *byte, refStride int) int
// Requires: SSE2. Exact-width loads pack independent rows into both SAD lanes.
TEXT ·sad8x8(SB), NOSPLIT, $0-40
	MOVQ src+0(FP), AX
	MOVQ srcStride+8(FP), CX
	MOVQ ref+16(FP), DX
	MOVQ refStride+24(FP), BX
	LEAQ (CX)(CX*2), R8
	LEAQ (BX)(BX*2), R9
	MOVQ (AX), X0
	MOVQ (AX)(CX*1), X1
	MOVHPS (AX)(CX*2), X0
	MOVHPS (AX)(R8*1), X1
	MOVQ (DX), X2
	MOVQ (DX)(BX*1), X3
	MOVHPS (DX)(BX*2), X2
	MOVHPS (DX)(R9*1), X3
	PSADBW X2, X0
	PSADBW X3, X1
	PADDD X1, X0
	LEAQ (AX)(CX*4), AX
	LEAQ (DX)(BX*4), DX
	MOVQ (AX), X1
	MOVQ (AX)(CX*1), X2
	MOVHPS (AX)(CX*2), X1
	MOVHPS (AX)(R8*1), X2
	MOVQ (DX), X3
	MOVQ (DX)(BX*1), X4
	MOVHPS (DX)(BX*2), X3
	MOVHPS (DX)(R9*1), X4
	PSADBW X3, X1
	PSADBW X4, X2
	PADDD X1, X0
	PADDD X2, X0
	MOVOU X0, X1
	PSRLDQ $8, X1
	PADDD X1, X0
	MOVQ X0, AX
	MOVQ AX, ret+32(FP)
	RET

// func sad8x4(src *byte, srcStride int, ref *byte, refStride int) int
// Requires: SSE2. Exact-width loads pack independent rows into both SAD lanes.
TEXT ·sad8x4(SB), NOSPLIT, $0-40
	MOVQ src+0(FP), AX
	MOVQ srcStride+8(FP), CX
	MOVQ ref+16(FP), DX
	MOVQ refStride+24(FP), BX
	LEAQ (CX)(CX*2), R8
	LEAQ (BX)(BX*2), R9
	MOVQ (AX), X0
	MOVQ (AX)(CX*1), X1
	MOVHPS (AX)(CX*2), X0
	MOVHPS (AX)(R8*1), X1
	MOVQ (DX), X2
	MOVQ (DX)(BX*1), X3
	MOVHPS (DX)(BX*2), X2
	MOVHPS (DX)(R9*1), X3
	PSADBW X2, X0
	PSADBW X3, X1
	PADDD X1, X0
	MOVOU X0, X1
	PSRLDQ $8, X1
	PADDD X1, X0
	MOVQ X0, AX
	MOVQ AX, ret+32(FP)
	RET

// func sad4x8(src *byte, srcStride int, ref *byte, refStride int) int
// Requires: SSE2. Exact-width loads pack independent rows into both SAD lanes.
TEXT ·sad4x8(SB), NOSPLIT, $0-40
	MOVQ src+0(FP), AX
	MOVQ srcStride+8(FP), CX
	MOVQ ref+16(FP), DX
	MOVQ refStride+24(FP), BX
	LEAQ (CX)(CX*2), R8
	LEAQ (BX)(BX*2), R9
	MOVL (AX), X0
	MOVL (AX)(CX*1), X1
	MOVL (AX)(CX*2), X2
	MOVL (AX)(R8*1), X3
	PUNPCKLLQ X1, X0
	PUNPCKLLQ X3, X2
	PUNPCKLQDQ X2, X0
	MOVL (DX), X4
	MOVL (DX)(BX*1), X5
	MOVL (DX)(BX*2), X6
	MOVL (DX)(R9*1), X7
	PUNPCKLLQ X5, X4
	PUNPCKLLQ X7, X6
	PUNPCKLQDQ X6, X4
	PSADBW X4, X0
	LEAQ (AX)(CX*4), AX
	LEAQ (DX)(BX*4), DX
	MOVL (AX), X1
	MOVL (AX)(CX*1), X2
	MOVL (AX)(CX*2), X3
	MOVL (AX)(R8*1), X4
	PUNPCKLLQ X2, X1
	PUNPCKLLQ X4, X3
	PUNPCKLQDQ X3, X1
	MOVL (DX), X5
	MOVL (DX)(BX*1), X6
	MOVL (DX)(BX*2), X7
	MOVL (DX)(R9*1), X8
	PUNPCKLLQ X6, X5
	PUNPCKLLQ X8, X7
	PUNPCKLQDQ X7, X5
	PSADBW X5, X1
	PADDD X1, X0
	MOVOU X0, X1
	PSRLDQ $8, X1
	PADDD X1, X0
	MOVQ X0, AX
	MOVQ AX, ret+32(FP)
	RET

// func sad4x4(src *byte, srcStride int, ref *byte, refStride int) int
// Requires: SSE2. Exact-width loads pack independent rows into both SAD lanes.
TEXT ·sad4x4(SB), NOSPLIT, $0-40
	MOVQ src+0(FP), AX
	MOVQ srcStride+8(FP), CX
	MOVQ ref+16(FP), DX
	MOVQ refStride+24(FP), BX
	LEAQ (CX)(CX*2), R8
	LEAQ (BX)(BX*2), R9
	MOVL (AX), X0
	MOVL (AX)(CX*1), X1
	MOVL (AX)(CX*2), X2
	MOVL (AX)(R8*1), X3
	PUNPCKLLQ X1, X0
	PUNPCKLLQ X3, X2
	PUNPCKLQDQ X2, X0
	MOVL (DX), X4
	MOVL (DX)(BX*1), X5
	MOVL (DX)(BX*2), X6
	MOVL (DX)(R9*1), X7
	PUNPCKLLQ X5, X4
	PUNPCKLLQ X7, X6
	PUNPCKLQDQ X6, X4
	PSADBW X4, X0
	MOVOU X0, X1
	PSRLDQ $8, X1
	PADDD X1, X0
	MOVQ X0, AX
	MOVQ AX, ret+32(FP)
	RET
