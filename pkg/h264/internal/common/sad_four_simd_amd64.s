//go:build amd64

// The paired-row SAD schedule follows OpenH264's SSE2 implementation in
// codec/common/x86/satd_sad.asm.
// Copyright (c) 2009-2013, Cisco Systems. All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// The wrappers validate every input span before entering assembly. Inputs may
// overlap or have zero/overlapping strides; all accesses are read-only and all
// four output stores remain deferred until the complete block is consumed.

// func sadFour16SSE2(src *byte, srcStride int, ref *byte, refStride, height int, sad *[4]int32)
TEXT ·sadFour16SSE2(SB), NOSPLIT, $0-48
    MOVQ src+0(FP), AX
    MOVQ srcStride+8(FP), CX
    MOVQ ref+16(FP), DX
    MOVQ refStride+24(FP), BX
    MOVQ height+32(FP), R9
    MOVQ sad+40(FP), R10
    MOVQ BX, R8
    NEGQ R8
    PXOR X0, X0
    PXOR X1, X1
    PXOR X2, X2
    PXOR X3, X3
sad_four_16_row:
    MOVOU (AX), X4
    MOVOU (DX)(R8*1), X5
    MOVOU (DX)(BX*1), X6
    MOVOU -1(DX), X7
    MOVOU 1(DX), X8
    PSADBW X4, X5
    PSADBW X4, X6
    PSADBW X4, X7
    PSADBW X4, X8
    PADDD X5, X0
    PADDD X6, X1
    PADDD X7, X2
    PADDD X8, X3
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ R9
    JNZ sad_four_16_row
    MOVOU X0, X4
    PSRLDQ $8, X4
    PADDD X4, X0
    MOVOU X1, X4
    PSRLDQ $8, X4
    PADDD X4, X1
    MOVOU X2, X4
    PSRLDQ $8, X4
    PADDD X4, X2
    MOVOU X3, X4
    PSRLDQ $8, X4
    PADDD X4, X3
    MOVL X0, 0(R10)
    MOVL X1, 4(R10)
    MOVL X2, 8(R10)
    MOVL X3, 12(R10)
    RET

// func sadFour8SSE2(src *byte, srcStride int, ref *byte, refStride, height int, sad *[4]int32)
TEXT ·sadFour8SSE2(SB), NOSPLIT, $0-48
    MOVQ src+0(FP), AX
    MOVQ srcStride+8(FP), CX
    MOVQ ref+16(FP), DX
    MOVQ refStride+24(FP), BX
    MOVQ height+32(FP), R9
    MOVQ sad+40(FP), R10
    MOVQ BX, R8
    NEGQ R8
    PXOR X0, X0
    PXOR X1, X1
    PXOR X2, X2
    PXOR X3, X3

    // Each register holds two exact eight-byte rows. The downward reference
    // pair is also the next source pair's upward reference, and stays live
    // across iterations without another load.
    MOVQ (AX), X4
    MOVHPS (AX)(CX*1), X4
    MOVQ (DX)(R8*1), X5
    MOVHPS (DX), X5
    PSADBW X4, X5
    PADDD X5, X0
sad_four_8_pair:
    MOVQ -1(DX), X6
    MOVHPS -1(DX)(BX*1), X6
    MOVQ 1(DX), X7
    MOVHPS 1(DX)(BX*1), X7
    PSADBW X4, X6
    PSADBW X4, X7
    PADDD X6, X2
    PADDD X7, X3
    MOVQ (DX)(BX*1), X5
    MOVHPS (DX)(BX*2), X5
    PSADBW X5, X4
    PADDD X4, X1
    SUBQ $2, R9
    JZ sad_four_8_reduce
    LEAQ (AX)(CX*2), AX
    LEAQ (DX)(BX*2), DX
    MOVQ (AX), X4
    MOVHPS (AX)(CX*1), X4
    PSADBW X4, X5
    PADDD X5, X0
    JMP sad_four_8_pair
sad_four_8_reduce:
    MOVOU X0, X4
    PSRLDQ $8, X4
    PADDD X4, X0
    MOVOU X1, X4
    PSRLDQ $8, X4
    PADDD X4, X1
    MOVOU X2, X4
    PSRLDQ $8, X4
    PADDD X4, X2
    MOVOU X3, X4
    PSRLDQ $8, X4
    PADDD X4, X3
    MOVL X0, 0(R10)
    MOVL X1, 4(R10)
    MOVL X2, 8(R10)
    MOVL X3, 12(R10)
    RET

// func sadFour4SSE2(src *byte, srcStride int, ref *byte, refStride, height int, sad *[4]int32)
TEXT ·sadFour4SSE2(SB), NOSPLIT, $0-48
    MOVQ src+0(FP), AX
    MOVQ srcStride+8(FP), CX
    MOVQ ref+16(FP), DX
    MOVQ refStride+24(FP), BX
    MOVQ height+32(FP), R9
    MOVQ sad+40(FP), R10
    MOVQ BX, R8
    NEGQ R8
    PXOR X0, X0
    PXOR X1, X1
    PXOR X2, X2
    PXOR X3, X3
sad_four_4_row:
    MOVL (AX), X4
    MOVL (DX)(R8*1), X5
    MOVL (DX)(BX*1), X6
    MOVL -1(DX), X7
    MOVL 1(DX), X8
    PSADBW X4, X5
    PSADBW X4, X6
    PSADBW X4, X7
    PSADBW X4, X8
    PADDD X5, X0
    PADDD X6, X1
    PADDD X7, X2
    PADDD X8, X3
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ R9
    JNZ sad_four_4_row
    MOVL X0, 0(R10)
    MOVL X1, 4(R10)
    MOVL X2, 8(R10)
    MOVL X3, 12(R10)
    RET
