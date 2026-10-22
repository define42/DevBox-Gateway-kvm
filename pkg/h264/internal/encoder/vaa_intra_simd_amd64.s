//go:build amd64

// Four-by-four means and variance follow AnalysisVaaInfoIntra_ssse3 in
// OpenH264's codec/common/x86/vaa.asm.
// Copyright (c) 2010-2013, Cisco Systems. All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// The checked caller supplies sixteen complete rows. Read-only overlapping
// rows and zero stride are valid. Each byte pair sums to at most 510, each
// 4x4 sum to 4080 and each mean to 255. The mean sum is at most 4080; its
// square (16646400) and all sixteen squared means (1040400) fit in int32.
// func intraVaaSSSE3(src *byte, stride int) int32
TEXT ·intraVaaSSSE3(SB), NOSPLIT, $0-20
    MOVQ src+0(FP), AX
    MOVQ stride+8(FP), CX
    LEAQ (CX)(CX*2), DX
    MOVL $0x01010101, R8
    MOVL R8, X7
    PSHUFL $0, X7, X7
    PXOR X6, X6
    PXOR X8, X8
    PXOR X9, X9
    MOVQ $4, DI
intra_vaa_rows:
    MOVOU (AX), X0
    MOVOU (AX)(CX*1), X1
    MOVOU (AX)(CX*2), X2
    MOVOU (AX)(DX*1), X3
    PMADDUBSW X7, X0
    PMADDUBSW X7, X1
    PMADDUBSW X7, X2
    PMADDUBSW X7, X3
    PADDW X1, X0
    PADDW X3, X2
    PADDW X2, X0
    PHADDW X6, X0
    PSRLW $4, X0
    PADDW X0, X8
    PMADDWL X0, X0
    PADDD X0, X9
    LEAQ (AX)(CX*4), AX
    DECQ DI
    JNZ intra_vaa_rows

    // The four accumulated mean columns occupy only the low four words.
    MOVOU X8, X0
    PSRLDQ $4, X0
    PADDW X0, X8
    MOVOU X8, X0
    PSRLDQ $2, X0
    PADDW X0, X8
    MOVL X8, CX
    ANDL $0xffff, CX
    IMULL CX, CX
    SHRL $4, CX

    // PMADDWD formed two useful dwords per band; the remaining two are zero.
    MOVOU X9, X0
    PSRLDQ $4, X0
    PADDD X0, X9
    MOVL X9, AX
    SUBL CX, AX
    MOVL AX, ret+16(FP)
    RET
