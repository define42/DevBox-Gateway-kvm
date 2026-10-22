//go:build amd64

// The fused Hadamard/quantization pattern follows Cisco OpenH264,
// codec/encoder/core/x86/quant.asm (WelsHadamardQuant2x2_mmx).
// Copyright (c) 2009-2013, Cisco Systems.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// Gather only the four DC words, including when rs has exactly 49 words.
// Both Hadamard stages wrap in int16. All inputs are captured before clears;
// outputs follow the scalar order: clear residual DC, write dct, copy to block.
// Keeping the completed vector in a register makes the copy overlap-safe.
// func hadamardQuant2x2SSE2(rs *[49]int16, ff, mf int64, dct, block *[4]int16) int32
TEXT ·hadamardQuant2x2SSE2(SB), NOSPLIT, $0-44
	MOVQ rs+0(FP), AX
	MOVQ dct+24(FP), BX
	MOVQ block+32(FP), CX
	PXOR X0, X0
	PINSRW $0, (AX), X0
	PINSRW $1, 32(AX), X0
	PINSRW $2, 64(AX), X0
	PINSRW $3, 96(AX), X0

	// [a,b,c,d] -> [a+c,b+d,a-c,b-d].
	PSHUFLW $0x4e, X0, X1
	MOVOU X0, X2
	PADDW X1, X2
	PSUBW X1, X0
	PUNPCKLLQ X0, X2
	// [s0,s2,s1,s3] -> [s0+s2,s0-s2,s1+s3,s1-s3].
	PSHUFLW $0xb1, X2, X1
	MOVOU X2, X0
	PADDW X1, X0
	PSUBW X1, X2
	PUNPCKLWL X2, X0
	PSHUFD $0x88, X0, X0

	MOVQ ff+8(FP), DX
	MOVD DX, X4
	PSHUFLW $0, X4, X4
	PSHUFD $0, X4, X4
	MOVQ mf+16(FP), DX
	MOVD DX, X5
	PSHUFLW $0, X5, X5
	PSHUFD $0, X5, X5
	PXOR X3, X3
	PCMPGTW X0, X3
	PXOR X3, X0
	PSUBW X3, X0
	PADDUSW X4, X0
	PMULHUW X5, X0
	PXOR X3, X0
	PSUBW X3, X0

	MOVW $0, (AX)
	MOVW $0, 32(AX)
	MOVW $0, 64(AX)
	MOVW $0, 96(AX)
	MOVQ X0, (BX)
	MOVQ X0, (CX)

	// Saturating byte packing preserves zero/nonzero. Zero the unused words
	// first, since quantizing a zero input can produce a nonzero rounded value.
	MOVQ X0, X0
	PXOR X1, X1
	PACKSSWB X1, X0
	PCMPEQB X1, X0
	PCMPEQB X2, X2
	PSUBSB X2, X0
	PSADBW X1, X0
	MOVD X0, DX
	MOVL DX, ret+40(FP)
	RET
