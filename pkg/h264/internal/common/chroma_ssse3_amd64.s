//go:build amd64

// Paired-byte chroma interpolation and row reuse follow OpenH264's
// McChromaWidthEq8_ssse3 in codec/common/x86/mc_chroma.asm.
// Copyright (c) 2009-2013, Cisco Systems. All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// The checked caller supplies disjoint input/output spans and positive height.
// Each source row contains exactly nine accessible bytes. Two overlapping
// eight-byte loads form its eight adjacent pairs without native's wider reads.
// Weights are in [0,64] and sum to 64. Each PMADDUBSW pair is <=255*64=16320;
// the combined rounded sum is <=16352, so signed-word saturation never occurs.
TEXT ·mcChroma8SSSE3(SB), NOSPLIT, $0-44
    MOVQ src+0(FP), AX
    MOVQ srcStride+8(FP), CX
    MOVQ dst+16(FP), BX
    MOVQ dstStride+24(FP), DX
    MOVQ height+32(FP), DI
    MOVL weights+40(FP), X4
    MOVOU X4, X5
    PSHUFLW $0, X4, X4
    PSHUFL $0, X4, X4
    PSHUFLW $0x55, X5, X5
    PSHUFL $0, X5, X5
    MOVL $0x00200020, R8
    MOVL R8, X6
    PSHUFL $0, X6, X6

    MOVQ (AX), X0
    MOVQ 1(AX), X1
    PUNPCKLBW X1, X0
chroma_ssse3_row:
    ADDQ CX, AX
    MOVQ (AX), X1
    MOVQ 1(AX), X2
    PUNPCKLBW X2, X1
    MOVOU X1, X3
    PMADDUBSW X4, X0
    PMADDUBSW X5, X1
    PADDW X1, X0
    PADDW X6, X0
    PSRLW $6, X0
    PACKUSWB X0, X0
    MOVQ X0, (BX)
    MOVOU X3, X0
    ADDQ DX, BX
    DECQ DI
    JNZ chroma_ssse3_row
    RET
