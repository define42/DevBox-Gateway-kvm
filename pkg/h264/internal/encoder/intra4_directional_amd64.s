//go:build amd64

// The directional average construction follows OpenH264's MMX predictors in
// codec/encoder/core/x86/intra_pred.asm, adapted to SSE2 and checked byte gathers.
// Copyright (c) 2009-2013, Cisco Systems. All rights reserved.
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
// 1. Redistributions of source code must retain the above copyright notice,
//    this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright notice,
//    this list of conditions and the following disclaimer in the documentation
//    and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
// LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
// CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
// SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
// CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
// ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
// POSSIBILITY OF SUCH DAMAGE.

#include "textflag.h"

DATA ·intra4DirectionalOnes+0(SB)/8, $0x0101010101010101
DATA ·intra4DirectionalOnes+8(SB)/8, $0x0101010101010101
GLOBL ·intra4DirectionalOnes(SB), RODATA|NOPTR, $16

// Average construction: floor((a+c)/2) is PAVGB(a,c) minus (a XOR c)&1;
// PAVGB of that result with b is exactly (a+2*b+c+2)>>2 for all bytes.
// Source pixels have already been gathered before the destination is validated.
// All source operands below are copied register values; only 16 bytes are stored.

// samples = L3 L2 L1 L0 LT T0 T1 T2, low byte first.
TEXT ·intra4PredHDSSE2(SB), NOSPLIT, $0-16
	MOVQ dst+0(FP), AX
	MOVQ samples+8(FP), X0
	MOVOU X0, X1
	PSRLDQ $1, X1
	MOVOU X0, X2
	PSRLDQ $2, X2
	MOVOU X2, X3
	PXOR X0, X3
	PAND ·intra4DirectionalOnes(SB), X3
	PAVGB X0, X2
	PSUBUSB X3, X2
	PAVGB X1, X2
	PAVGB X0, X1
	// X2 begins j h f b c d; X1 begins i g e a.
	PUNPCKLBW X2, X1
	// Interleave overlapping dwords, then reverse: temporary row0,row1,row2,row3.
	MOVOU X1, X3
	PSRLDQ $2, X3
	PUNPCKLLQ X3, X1
	PSHUFL $0x1b, X1, X1
	// Replace row0's upper two bytes with c,d (X2 bytes 4,5).
	PSRLDQ $4, X2
	MOVQ X2, CX
	PINSRW $1, CX, X1
	MOVOU X1, (AX)
	RET

// samples = L0 L1 L2 L3 L3 L3 L3 L3, low byte first.
TEXT ·intra4PredHUSSE2(SB), NOSPLIT, $0-16
	MOVQ dst+0(FP), AX
	MOVQ samples+8(FP), X0
	MOVOU X0, X1
	PSRLDQ $1, X1
	MOVOU X0, X2
	PSRLDQ $2, X2
	MOVOU X2, X3
	PXOR X0, X3
	PAND ·intra4DirectionalOnes(SB), X3
	PAVGB X0, X2
	PSUBUSB X3, X2
	PAVGB X1, X2
	PAVGB X0, X1
	// X1/X2 interleave to a b c d e f g g; overlapping dwords are the rows.
	PUNPCKLBW X2, X1
	MOVOU X1, X3
	PSRLDQ $2, X3
	PUNPCKLLQ X3, X1
	MOVOU X1, (AX)
	RET

// samples = L2 L1 L0 LT T0 T1 T2 T3, low byte first.
TEXT ·intra4PredVRSSE2(SB), NOSPLIT, $0-16
	MOVQ dst+0(FP), AX
	MOVQ samples+8(FP), X0
	MOVOU X0, X1
	PSLLDQ $1, X1
	MOVOU X0, X2
	PSLLDQ $2, X2
	MOVOU X2, X3
	PXOR X0, X3
	PAND ·intra4DirectionalOnes(SB), X3
	PAVGB X0, X2
	PSUBUSB X3, X2
	PAVGB X1, X2
	PAVGB X0, X1
	// X2 bytes 2,3 are j,i. Copy them before selecting the e,f,g,h row.
	MOVQ X2, CX
	SHRQ $16, CX
	MOVL CX, DX
	SHRL $8, DX
	ANDL $255, DX
	ANDL $255, CX
	PSRLDQ $4, X1
	PSRLDQ $4, X2
	MOVL X1, BX
	MOVL BX, 0(AX)
	SHLL $8, BX
	ORL DX, BX
	MOVL BX, 8(AX)
	MOVL X2, BX
	MOVL BX, 4(AX)
	SHLL $8, BX
	ORL CX, BX
	MOVL BX, 12(AX)
	RET
