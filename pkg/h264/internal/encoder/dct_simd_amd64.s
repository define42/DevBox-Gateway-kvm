// This forward transform follows OpenH264's SSE2 implementation in
// codec/common/x86/dct.asm.
// Copyright (c) 2009-2013, Cisco Systems.
// All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// func welsDctT4SSE2(dct *[16]int16, pixel1 *byte, stride1 int,
//     pixel2 *byte, stride2 int)
TEXT ·welsDctT4SSE2(SB), NOSPLIT, $0-40
	MOVQ dct+0(FP), AX
	MOVQ pixel1+8(FP), BX
	MOVQ stride1+16(FP), CX
	MOVQ pixel2+24(FP), DX
	MOVQ stride2+32(FP), SI
	JMP dctT4SSE2Kernel<>(SB)

// func welsDctFourT4SSE2(dct []int16, pixel1 []byte, stride1 int,
//     pixel2 []byte, stride2 int)
TEXT ·welsDctFourT4SSE2(SB), NOSPLIT, $0-88
	MOVQ dct_base+0(FP), R8
	MOVQ pixel1_base+24(FP), R9
	MOVQ stride1+48(FP), CX
	MOVQ pixel2_base+56(FP), R10
	MOVQ stride2+80(FP), SI

	// Transform each pair of horizontally adjacent blocks together.
	MOVQ R8, AX
	MOVQ R9, BX
	MOVQ R10, DX
	CALL dctTwoT4SSE2Kernel<>(SB)

	LEAQ 64(R8), AX
	LEAQ (R9)(CX*4), BX
	LEAQ (R10)(SI*4), DX
	CALL dctTwoT4SSE2Kernel<>(SB)
	RET

// dctT4SSE2Kernel uses a private register ABI:
// AX=destination, BX=pixel1, CX=stride1, DX=pixel2, SI=stride2.
TEXT dctT4SSE2Kernel<>(SB), NOSPLIT|NOFRAME, $0-0

	// Load four rows of four unsigned pixels and form signed word differences.
	PXOR X10, X10
	MOVL (BX), X0
	MOVL (DX), X4
	PUNPCKLBW X10, X0
	PUNPCKLBW X10, X4
	PSUBW X4, X0
	ADDQ CX, BX
	ADDQ SI, DX

	MOVL (BX), X1
	MOVL (DX), X4
	PUNPCKLBW X10, X1
	PUNPCKLBW X10, X4
	PSUBW X4, X1
	ADDQ CX, BX
	ADDQ SI, DX

	MOVL (BX), X2
	MOVL (DX), X4
	PUNPCKLBW X10, X2
	PUNPCKLBW X10, X4
	PSUBW X4, X2
	ADDQ CX, BX
	ADDQ SI, DX

	MOVL (BX), X3
	MOVL (DX), X4
	PUNPCKLBW X10, X3
	PUNPCKLBW X10, X4
	PSUBW X4, X3

	// Apply the vertical 4-point transform in parallel to all four columns.
	// A=x0+x3, B=x1+x2, C=x1-x2, D=x0-x3.
	MOVOU X0, X4
	PADDW X3, X4
	MOVOU X1, X5
	PADDW X2, X5
	MOVOU X1, X6
	PSUBW X2, X6
	MOVOU X0, X7
	PSUBW X3, X7

	MOVOU X4, X0
	PADDW X5, X0
	MOVOU X7, X1
	PADDW X7, X1
	PADDW X6, X1
	MOVOU X4, X2
	PSUBW X5, X2
	MOVOU X6, X8
	PADDW X6, X8
	MOVOU X7, X3
	PSUBW X8, X3

	// Transpose the low 4x4 words so the same packed transform computes the
	// horizontal dimension.
	MOVOU X0, X4
	PUNPCKLWL X1, X4
	MOVOU X2, X5
	PUNPCKLWL X3, X5
	MOVOU X4, X6
	PUNPCKLLQ X5, X6
	MOVOU X4, X7
	PUNPCKHLQ X5, X7
	MOVOU X6, X0
	MOVOU X6, X1
	PSRLDQ $8, X1
	MOVOU X7, X2
	MOVOU X7, X3
	PSRLDQ $8, X3

	// Apply the horizontal 4-point transform.
	MOVOU X0, X4
	PADDW X3, X4
	MOVOU X1, X5
	PADDW X2, X5
	MOVOU X1, X6
	PSUBW X2, X6
	MOVOU X0, X7
	PSUBW X3, X7

	MOVOU X4, X0
	PADDW X5, X0
	MOVOU X7, X1
	PADDW X7, X1
	PADDW X6, X1
	MOVOU X4, X2
	PSUBW X5, X2
	MOVOU X6, X8
	PADDW X6, X8
	MOVOU X7, X3
	PSUBW X8, X3

	// Restore row-major coefficient order.
	MOVOU X0, X4
	PUNPCKLWL X1, X4
	MOVOU X2, X5
	PUNPCKLWL X3, X5
	MOVOU X4, X6
	PUNPCKLLQ X5, X6
	MOVOU X4, X7
	PUNPCKHLQ X5, X7
	MOVOU X6, X0
	MOVOU X6, X1
	PSRLDQ $8, X1
	MOVOU X7, X2
	MOVOU X7, X3
	PSRLDQ $8, X3

	MOVQ X0, 0(AX)
	MOVQ X1, 8(AX)
	MOVQ X2, 16(AX)
	MOVQ X3, 24(AX)
	RET

// dctTwoT4SSE2Kernel transforms an 8x4 rectangle as two independent 4x4
// transforms. The register ABI matches the single-block kernel; R8-R10 retain
// the caller's bases. Pixel differences and both transforms fit signed words.
TEXT dctTwoT4SSE2Kernel<>(SB), NOSPLIT|NOFRAME, $0-0
	PXOR X10, X10
	MOVQ (BX), X0
	MOVQ (DX), X4
	PUNPCKLBW X10, X0
	PUNPCKLBW X10, X4
	PSUBW X4, X0
	ADDQ CX, BX
	ADDQ SI, DX
	MOVQ (BX), X1
	MOVQ (DX), X4
	PUNPCKLBW X10, X1
	PUNPCKLBW X10, X4
	PSUBW X4, X1
	ADDQ CX, BX
	ADDQ SI, DX
	MOVQ (BX), X2
	MOVQ (DX), X4
	PUNPCKLBW X10, X2
	PUNPCKLBW X10, X4
	PSUBW X4, X2
	ADDQ CX, BX
	ADDQ SI, DX
	MOVQ (BX), X3
	MOVQ (DX), X4
	PUNPCKLBW X10, X3
	PUNPCKLBW X10, X4
	PSUBW X4, X3

	// Vertical transform, then horizontal transform of both blocks.
	MOVOU X0, X4
	PADDW X3, X4
	MOVOU X1, X5
	PADDW X2, X5
	MOVOU X1, X6
	PSUBW X2, X6
	MOVOU X0, X7
	PSUBW X3, X7
	MOVOU X4, X0
	PADDW X5, X0
	MOVOU X7, X1
	PADDW X7, X1
	PADDW X6, X1
	MOVOU X4, X2
	PSUBW X5, X2
	MOVOU X6, X8
	PADDW X6, X8
	MOVOU X7, X3
	PSUBW X8, X3
	// Transpose both 4x4 word matrices, retaining one in each 64-bit half.
	MOVOU X0, X4
	PUNPCKLWL X1, X4
	MOVOU X0, X6
	PUNPCKHWL X1, X6
	MOVOU X2, X5
	PUNPCKLWL X3, X5
	MOVOU X2, X7
	PUNPCKHWL X3, X7
	MOVOU X4, X8
	PUNPCKLLQ X5, X8
	PUNPCKHLQ X5, X4
	MOVOU X6, X9
	PUNPCKLLQ X7, X9
	PUNPCKHLQ X7, X6
	MOVOU X8, X0
	PUNPCKLQDQ X9, X0
	MOVOU X8, X1
	PUNPCKHQDQ X9, X1
	MOVOU X4, X2
	PUNPCKLQDQ X6, X2
	MOVOU X4, X3
	PUNPCKHQDQ X6, X3
	MOVOU X0, X4
	PADDW X3, X4
	MOVOU X1, X5
	PADDW X2, X5
	MOVOU X1, X6
	PSUBW X2, X6
	MOVOU X0, X7
	PSUBW X3, X7
	MOVOU X4, X0
	PADDW X5, X0
	MOVOU X7, X1
	PADDW X7, X1
	PADDW X6, X1
	MOVOU X4, X2
	PSUBW X5, X2
	MOVOU X6, X8
	PADDW X6, X8
	MOVOU X7, X3
	PSUBW X8, X3
	// Transpose both 4x4 word matrices, retaining one in each 64-bit half.
	MOVOU X0, X4
	PUNPCKLWL X1, X4
	MOVOU X0, X6
	PUNPCKHWL X1, X6
	MOVOU X2, X5
	PUNPCKLWL X3, X5
	MOVOU X2, X7
	PUNPCKHWL X3, X7
	MOVOU X4, X8
	PUNPCKLLQ X5, X8
	PUNPCKHLQ X5, X4
	MOVOU X6, X9
	PUNPCKLLQ X7, X9
	PUNPCKHLQ X7, X6
	MOVOU X8, X0
	PUNPCKLQDQ X9, X0
	MOVOU X8, X1
	PUNPCKHQDQ X9, X1
	MOVOU X4, X2
	PUNPCKLQDQ X6, X2
	MOVOU X4, X3
	PUNPCKHQDQ X6, X3

	// Store each 4x4 block contiguously in row-major coefficient order.
	MOVOU X0, X4
	PUNPCKLQDQ X1, X4
	PUNPCKHQDQ X1, X0
	MOVOU X4, 0(AX)
	MOVOU X0, 32(AX)
	MOVOU X2, X4
	PUNPCKLQDQ X3, X4
	PUNPCKHQDQ X3, X2
	MOVOU X4, 16(AX)
	MOVOU X2, 48(AX)
	RET
