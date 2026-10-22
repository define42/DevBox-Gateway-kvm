//go:build amd64

// The widened forward DC and packed inverse/dequant Hadamard patterns follow
// Cisco OpenH264 codec/encoder/core/x86/{dct,quant}.asm. The forward gather uses
// exact word reads and both entries allow unaligned buffers.
// Copyright (c) 2009-2013, Cisco Systems. All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// A column of spatial DCs, sign-extended from four exact int16 input loads.
#define DC_COLUMN(OUT, A, B, C, D) \
    PXOR OUT, OUT; \
    PINSRW $0, A(SI), OUT; \
    PINSRW $1, B(SI), OUT; \
    PINSRW $2, C(SI), OUT; \
    PINSRW $3, D(SI), OUT; \
    PXOR X4, X4; \
    PCMPGTW OUT, X4; \
    PUNPCKLWL X4, OUT

// Four independent transforms across X0..X3. Every forward intermediate is
// int32, not a wrapping word: full signed input may exceed the native residual
// range and must still round and saturate exactly like WelsHadamardT4Dc_c.
#define DC_HADAMARD_D \
    MOVOU X0, X4; \
    PADDL X3, X4; \
    MOVOU X0, X5; \
    PSUBL X3, X5; \
    MOVOU X1, X6; \
    PADDL X2, X6; \
    MOVOU X1, X7; \
    PSUBL X2, X7; \
    MOVOU X4, X0; \
    PADDL X6, X0; \
    MOVOU X4, X2; \
    PSUBL X6, X2; \
    MOVOU X5, X1; \
    PADDL X7, X1; \
    MOVOU X5, X3; \
    PSUBL X7, X3

#define DC_TRANSPOSE_D \
    MOVOU X0, X4; \
    PUNPCKLLQ X1, X0; \
    PUNPCKHLQ X1, X4; \
    MOVOU X2, X5; \
    PUNPCKLLQ X3, X2; \
    PUNPCKHLQ X3, X5; \
    MOVOU X0, X1; \
    PUNPCKLQDQ X2, X0; \
    PUNPCKHQDQ X2, X1; \
    MOVOU X4, X3; \
    PUNPCKLQDQ X5, X4; \
    PUNPCKHQDQ X5, X3; \
    MOVOU X4, X2

TEXT ·hadamardT4DcSSE2(SB), NOSPLIT, $0-16
    MOVQ dc+0(FP), DI
    MOVQ residual+8(FP), SI
    // Spatial columns in the encoder's grouped 4x4 coefficient layout.
    DC_COLUMN(X0, 0, 64, 256, 320)
    DC_COLUMN(X1, 32, 96, 288, 352)
    DC_COLUMN(X2, 128, 192, 384, 448)
    DC_COLUMN(X3, 160, 224, 416, 480)
    DC_HADAMARD_D
    DC_TRANSPOSE_D
    DC_HADAMARD_D
    PCMPEQL X4, X4
    PSRLL $31, X4
    PADDL X4, X0
    PADDL X4, X1
    PADDL X4, X2
    PADDL X4, X3
    PSRAL $1, X0
    PSRAL $1, X1
    PSRAL $1, X2
    PSRAL $1, X3
    PACKSSLW X1, X0
    PACKSSLW X3, X2
    MOVOU X0, (DI)
    MOVOU X2, 16(DI)
    RET

// Every addition/subtraction and final product is observed modulo 2^16 by
// the scalar inverse/dequant routine. Multiplication commutes with the two
// linear Hadamard stages, including uint16 multipliers above MaxInt16.
#define DC_HADAMARD_W \
    MOVOU X0, X4; \
    PADDW X3, X4; \
    MOVOU X0, X5; \
    PSUBW X3, X5; \
    MOVOU X1, X6; \
    PADDW X2, X6; \
    MOVOU X1, X7; \
    PSUBW X2, X7; \
    MOVOU X4, X0; \
    PADDW X6, X0; \
    MOVOU X4, X2; \
    PSUBW X6, X2; \
    MOVOU X5, X1; \
    PADDW X7, X1; \
    MOVOU X5, X3; \
    PSUBW X7, X3

// Transpose the low four words of X0..X3 into their low four output words.
#define DC_TRANSPOSE_W \
    MOVOU X0, X4; \
    PUNPCKLWL X1, X4; \
    MOVOU X2, X5; \
    PUNPCKLWL X3, X5; \
    MOVOU X4, X0; \
    PUNPCKLLQ X5, X0; \
    PUNPCKHLQ X5, X4; \
    MOVOU X0, X1; \
    PSRLDQ $8, X1; \
    MOVOU X4, X2; \
    MOVOU X4, X3; \
    PSRLDQ $8, X3

TEXT ·dequantIHadamard4x4SSE2(SB), NOSPLIT, $0-16
    MOVQ residual+0(FP), DI
    MOVQ mf+8(FP), AX
    MOVL AX, X8
    PSHUFLW $0, X8, X8
    PSHUFD $0, X8, X8
    MOVOU (DI), X0
    MOVOU 16(DI), X2
    PMULLW X8, X0
    PMULLW X8, X2
    MOVOU X0, X1
    PSRLDQ $8, X1
    MOVOU X2, X3
    PSRLDQ $8, X3
    DC_HADAMARD_W
    DC_TRANSPOSE_W
    DC_HADAMARD_W
    DC_TRANSPOSE_W
    PUNPCKLQDQ X1, X0
    PUNPCKLQDQ X3, X2
    MOVOU X0, (DI)
    MOVOU X2, 16(DI)
    RET
