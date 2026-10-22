//go:build amd64

// Plane prediction follows the SSE2 reduction used in Cisco OpenH264,
// codec/encoder/core/x86/intra_pred.asm. Exact byte gathers replace its
// wider left-column loads so no unchecked horizontal halo is accessed.
// Copyright (c) 2009-2013, Cisco Systems.
// All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

DATA ·intraPlane16Negative+0(SB)/8, $0xfffbfffafff9fff8
DATA ·intraPlane16Negative+8(SB)/8, $0xfffffffefffdfffc
GLOBL ·intraPlane16Negative(SB), RODATA|NOPTR, $16

DATA ·intraPlane16Positive+0(SB)/8, $0x0004000300020001
DATA ·intraPlane16Positive+8(SB)/8, $0x0008000700060005
GLOBL ·intraPlane16Positive(SB), RODATA|NOPTR, $16

// Complete 16x16: weighted reductions, rounding, and clipped output.
TEXT ·intraPredPlane16FullSSE2(SB), NOSPLIT, $0-24
	MOVQ dst+0(FP), AX
	MOVQ corner+8(FP), DX
	MOVQ stride+16(FP), SI
	PXOR X7, X7
	MOVQ (DX), X0
	MOVQ 9(DX), X1
	PUNPCKLBW X7, X0
	PUNPCKLBW X7, X1
	MOVOU ·intraPlane16Negative(SB), X4
	MOVOU ·intraPlane16Positive(SB), X5
	PMADDWL X4, X0
	PMADDWL X5, X1
	PADDL X1, X0
	MOVBLZX 16(DX), R8

	// Gather exactly the weighted left samples, omitting the center.
	LEAQ (SI)(SI*2), R10
	MOVQ DX, R9
	MOVBLZX (R9), R11
	PINSRW $0, R11, X2
	MOVBLZX (R9)(SI*1), R11
	PINSRW $1, R11, X2
	MOVBLZX (R9)(SI*2), R11
	PINSRW $2, R11, X2
	MOVBLZX (R9)(R10*1), R11
	PINSRW $3, R11, X2
	LEAQ (R9)(SI*4), R9
	MOVBLZX (R9), R11
	PINSRW $4, R11, X2
	MOVBLZX (R9)(SI*1), R11
	PINSRW $5, R11, X2
	MOVBLZX (R9)(SI*2), R11
	PINSRW $6, R11, X2
	MOVBLZX (R9)(R10*1), R11
	PINSRW $7, R11, X2
	LEAQ (DX)(SI*8), R9
	ADDQ SI, R9
	MOVBLZX (R9), R11
	PINSRW $0, R11, X3
	MOVBLZX (R9)(SI*1), R11
	PINSRW $1, R11, X3
	MOVBLZX (R9)(SI*2), R11
	PINSRW $2, R11, X3
	MOVBLZX (R9)(R10*1), R11
	PINSRW $3, R11, X3
	LEAQ (R9)(SI*4), R9
	MOVBLZX (R9), R11
	PINSRW $4, R11, X3
	MOVBLZX (R9)(SI*1), R11
	PINSRW $5, R11, X3
	MOVBLZX (R9)(SI*2), R11
	PINSRW $6, R11, X3
	MOVBLZX (R9)(R10*1), R11
	PINSRW $7, R11, X3
	ADDL R11, R8
	SHLL $4, R8
	PMADDWL X4, X2
	PMADDWL X5, X3
	PADDL X3, X2

	// Reduce four dword sums without signed-word overflow.
	PSHUFL $0x4e, X0, X1
	PADDL X1, X0
	PSHUFL $0xb1, X0, X1
	PADDL X1, X0
	MOVD X0, BX
	IMULL $5, BX
	ADDL $32, BX
	SARL $6, BX
	PSHUFL $0x4e, X2, X3
	PADDL X3, X2
	PSHUFL $0xb1, X2, X3
	PADDL X3, X2
	MOVD X2, DX
	IMULL $5, DX
	ADDL $32, DX
	SARL $6, DX

	// base = a - (size/2-1)*(horizontal+vertical) + 16.
	MOVL BX, R9
	ADDL DX, R9
	IMULL $7, R9
	SUBL R9, R8
	ADDL $16, R8
	MOVD R8, X0
	PUNPCKLWL X0, X0
	PSHUFL $0, X0, X0
	MOVD DX, X1
	PUNPCKLWL X1, X1
	PSHUFL $0, X1, X1
	MOVD BX, X2
	PUNPCKLWL X2, X2
	PSHUFL $0, X2, X2
	MOVOU ·intraPlaneColumns(SB), X3
	PMULLW X2, X3
	PADDW X0, X3
	PSLLW $3, X2
	MOVQ $16, CX
plane_full16_loop:
	MOVOU X3, X4
	MOVOU X3, X5
	PADDW X2, X5
	PSRAW $5, X4
	PSRAW $5, X5
	PACKUSWB X5, X4
	MOVOU X4, (AX)
	PADDW X1, X3
	ADDQ $16, AX
	DECQ CX
	JNZ plane_full16_loop
	RET

DATA ·intraPlane8Negative+0(SB)/8, $0xfffffffefffdfffc
DATA ·intraPlane8Negative+8(SB)/8, $0x0000000000000000
GLOBL ·intraPlane8Negative(SB), RODATA|NOPTR, $16

DATA ·intraPlane8Positive+0(SB)/8, $0x0004000300020001
DATA ·intraPlane8Positive+8(SB)/8, $0x0000000000000000
GLOBL ·intraPlane8Positive(SB), RODATA|NOPTR, $16

// Complete 8x8: weighted reductions, rounding, and clipped output.
TEXT ·intraPredPlane8FullSSE2(SB), NOSPLIT, $0-24
	MOVQ dst+0(FP), AX
	MOVQ corner+8(FP), DX
	MOVQ stride+16(FP), SI
	PXOR X7, X7
	MOVL (DX), X0
	MOVL 5(DX), X1
	PUNPCKLBW X7, X0
	PUNPCKLBW X7, X1
	MOVOU ·intraPlane8Negative(SB), X4
	MOVOU ·intraPlane8Positive(SB), X5
	PMADDWL X4, X0
	PMADDWL X5, X1
	PADDL X1, X0
	MOVBLZX 8(DX), R8

	// Gather exactly the weighted left samples, omitting the center.
	LEAQ (SI)(SI*2), R10
	MOVQ DX, R9
	PXOR X2, X2
	MOVBLZX (R9), R11
	PINSRW $0, R11, X2
	MOVBLZX (R9)(SI*1), R11
	PINSRW $1, R11, X2
	MOVBLZX (R9)(SI*2), R11
	PINSRW $2, R11, X2
	MOVBLZX (R9)(R10*1), R11
	PINSRW $3, R11, X2
	LEAQ (DX)(SI*4), R9
	ADDQ SI, R9
	PXOR X3, X3
	MOVBLZX (R9), R11
	PINSRW $0, R11, X3
	MOVBLZX (R9)(SI*1), R11
	PINSRW $1, R11, X3
	MOVBLZX (R9)(SI*2), R11
	PINSRW $2, R11, X3
	MOVBLZX (R9)(R10*1), R11
	PINSRW $3, R11, X3
	ADDL R11, R8
	SHLL $4, R8
	PMADDWL X4, X2
	PMADDWL X5, X3
	PADDL X3, X2

	// Reduce four dword sums without signed-word overflow.
	PSHUFL $0x4e, X0, X1
	PADDL X1, X0
	PSHUFL $0xb1, X0, X1
	PADDL X1, X0
	MOVD X0, BX
	IMULL $17, BX
	ADDL $16, BX
	SARL $5, BX
	PSHUFL $0x4e, X2, X3
	PADDL X3, X2
	PSHUFL $0xb1, X2, X3
	PADDL X3, X2
	MOVD X2, DX
	IMULL $17, DX
	ADDL $16, DX
	SARL $5, DX

	// base = a - (size/2-1)*(horizontal+vertical) + 16.
	MOVL BX, R9
	ADDL DX, R9
	IMULL $3, R9
	SUBL R9, R8
	ADDL $16, R8
	MOVD R8, X0
	PUNPCKLWL X0, X0
	PSHUFL $0, X0, X0
	MOVD DX, X1
	PUNPCKLWL X1, X1
	PSHUFL $0, X1, X1
	MOVD BX, X2
	PUNPCKLWL X2, X2
	PSHUFL $0, X2, X2
	MOVOU ·intraPlaneColumns(SB), X3
	PMULLW X2, X3
	PADDW X0, X3
	MOVQ $8, CX
plane_full8_loop:
	MOVOU X3, X4
	PSRAW $5, X4
	PACKUSWB X4, X4
	MOVQ X4, (AX)
	PADDW X1, X3
	ADDQ $8, AX
	DECQ CX
	JNZ plane_full8_loop
	RET
