//go:build amd64

// The packed sum/difference SATD reduction follows OpenH264's AVX2
// implementation in codec/common/x86/satd_sad.asm.
// Copyright (c) 2009-2013, Cisco Systems. All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// Y8-Y11 retain the encoded horizontal pair sums/differences for one band.
// Y12-Y15 retain four independent dword accumulators across all bands.
// Y0-Y7 are scratch, so no source rows or transformed source pairs need to
// be loaded again between candidates. All loads are exactly 8 or 16 bytes.
#define FOUR_CANDIDATE(A, B, AS, BS) \
    MOVQ A(R8), BX; \
    MOVQ B(R8), SI; \
    MOVQ AS(R8), DX; \
    MOVQ BS(R8), DI; \
    MOVQ R11, R12; \
    IMULQ DX, R12; \
    ADDQ R12, BX; \
    MOVQ R11, R12; \
    IMULQ DI, R12; \
    ADDQ R12, SI

#define FOUR_ENC16(Y) \
    VBROADCASTI128 (AX), Y; \
    VPMADDUBSW ·satdPairSigns(SB), Y, Y; \
    ADDQ CX, AX

#define FOUR_ROW16(ENC, OUT) \
    VBROADCASTI128 (BX), OUT; \
    VBROADCASTI128 (SI), Y4; \
    VPAVGB Y4, OUT, OUT; \
    VPMADDUBSW ·satdPairSigns(SB), OUT, OUT; \
    VPSUBW OUT, ENC, OUT; \
    ADDQ DX, BX; \
    ADDQ DI, SI

// Rows y and y+4 occupy independent lanes; each eight-byte row is
// duplicated inside its lane to produce both horizontal sums/differences.
#define FOUR_ENC8(Y) \
    VPBROADCASTQ (AX), Y; \
    VPBROADCASTQ (AX)(CX*4), Y4; \
    VPBLENDD $0xf0, Y4, Y, Y; \
    VPMADDUBSW ·satdPairSigns8(SB), Y, Y; \
    ADDQ CX, AX

#define FOUR_ROW8(ENC, OUT) \
    VPBROADCASTQ (BX), OUT; \
    VPBROADCASTQ (BX)(DX*4), Y4; \
    VPBLENDD $0xf0, Y4, OUT, OUT; \
    VPBROADCASTQ (SI), Y4; \
    VPBROADCASTQ (SI)(DI*4), Y5; \
    VPBLENDD $0xf0, Y5, Y4, Y4; \
    VPAVGB Y4, OUT, OUT; \
    VPMADDUBSW ·satdPairSigns8(SB), OUT, OUT; \
    VPSUBW OUT, ENC, OUT; \
    ADDQ DX, BX; \
    ADDQ DI, SI

// A final four-row band has a zero high lane. VEX XMM writes clear the
// high lane; do not broadcast these rows into both lanes and count twice.
#define FOUR_ENC8LOW(X, Y) \
    VMOVQ (AX), X; \
    VPUNPCKLQDQ X, X, X; \
    VPMADDUBSW ·satdPairSigns8(SB), Y, Y; \
    ADDQ CX, AX

#define FOUR_ROW8LOW(ENC, X, Y) \
    VMOVQ (BX), X; \
    VMOVQ (SI), X4; \
    VPAVGB X4, X, X; \
    VPUNPCKLQDQ X, X, X; \
    VPMADDUBSW ·satdPairSigns8(SB), Y, Y; \
    VPSUBW Y, ENC, Y; \
    ADDQ DX, BX; \
    ADDQ DI, SI

// Same 4x4 butterflies and absolute-max normalization as satdRowsAVX2.
// Maxima and their pair sums fit signed words for all byte inputs; only
// cross-band accumulation needs dwords. The four candidates never share
// an accumulator, including when their input buffers overlap.
#define FOUR_TRANSFORM(ACC) \
    VPADDW Y2, Y0, Y4; \
    VPADDW Y3, Y1, Y5; \
    VPSUBW Y2, Y0, Y6; \
    VPSUBW Y3, Y1, Y7; \
    VPADDW Y5, Y4, Y0; \
    VPADDW Y7, Y6, Y1; \
    VPSUBW Y7, Y6, Y2; \
    VPSUBW Y5, Y4, Y3; \
    VPABSW Y0, Y0; \
    VPABSW Y1, Y1; \
    VPABSW Y2, Y2; \
    VPABSW Y3, Y3; \
    VPBLENDW $0xaa, Y1, Y0, Y4; \
    VPSLLD $16, Y1, Y1; \
    VPSRLD $16, Y0, Y0; \
    VPOR Y0, Y1, Y1; \
    VPMAXUW Y4, Y1, Y1; \
    VPBLENDW $0xaa, Y3, Y2, Y4; \
    VPSLLD $16, Y3, Y3; \
    VPSRLD $16, Y2, Y2; \
    VPOR Y2, Y3, Y3; \
    VPMAXUW Y4, Y3, Y3; \
    VPADDW Y3, Y1, Y0; \
    VPMADDWD ·satdFourOnes(SB), Y0, Y0; \
    VPADDD Y0, ACC, ACC

#define FOUR_REDUCE(Y, X, OFFSET) \
    VEXTRACTI128 $1, Y, X0; \
    VPADDD X, X0, X0; \
    VPSHUFD $0x4e, X0, X1; \
    VPADDD X1, X0, X0; \
    VPSHUFD $0xb1, X0, X1; \
    VPADDD X1, X0, X0; \
    VMOVD X0, OFFSET(AX)

// Typed-pointer ABI: enc, stride, *[4]candidate, width, height, *[4]int32.
// Candidate records contain a,b pointers and a,b strides at offsets 0,8,16,24.
// The Go caller has checked all spans and all output/input disjointness.
// This leaf does not retain pointers, write input memory or touch Go's R14.
TEXT ·satdAvgFourAVX2(SB), NOSPLIT, $0-48
    MOVQ enc+0(FP), AX
    MOVQ encStride+8(FP), CX
    MOVQ candidates+16(FP), R8
    MOVQ width+24(FP), R10
    MOVQ height+32(FP), R9
    XORQ R11, R11
    VPXOR Y12, Y12, Y12
    VPXOR Y13, Y13, Y13
    VPXOR Y14, Y14, Y14
    VPXOR Y15, Y15, Y15
    CMPQ R10, $16
    JNE four_rows8

four_rows16:
    FOUR_ENC16(Y8)
    FOUR_ENC16(Y9)
    FOUR_ENC16(Y10)
    FOUR_ENC16(Y11)
    FOUR_CANDIDATE(0, 8, 16, 24)
    FOUR_ROW16(Y8, Y0)
    FOUR_ROW16(Y9, Y1)
    FOUR_ROW16(Y10, Y2)
    FOUR_ROW16(Y11, Y3)
    FOUR_TRANSFORM(Y12)
    FOUR_CANDIDATE(32, 40, 48, 56)
    FOUR_ROW16(Y8, Y0)
    FOUR_ROW16(Y9, Y1)
    FOUR_ROW16(Y10, Y2)
    FOUR_ROW16(Y11, Y3)
    FOUR_TRANSFORM(Y13)
    FOUR_CANDIDATE(64, 72, 80, 88)
    FOUR_ROW16(Y8, Y0)
    FOUR_ROW16(Y9, Y1)
    FOUR_ROW16(Y10, Y2)
    FOUR_ROW16(Y11, Y3)
    FOUR_TRANSFORM(Y14)
    FOUR_CANDIDATE(96, 104, 112, 120)
    FOUR_ROW16(Y8, Y0)
    FOUR_ROW16(Y9, Y1)
    FOUR_ROW16(Y10, Y2)
    FOUR_ROW16(Y11, Y3)
    FOUR_TRANSFORM(Y15)
    ADDQ $4, R11
    SUBQ $4, R9
    JNZ four_rows16
    JMP four_reduce

four_rows8:
    CMPQ R9, $8
    JL four_rows8low
    FOUR_ENC8(Y8)
    FOUR_ENC8(Y9)
    FOUR_ENC8(Y10)
    FOUR_ENC8(Y11)
    LEAQ (AX)(CX*4), AX
    FOUR_CANDIDATE(0, 8, 16, 24)
    FOUR_ROW8(Y8, Y0)
    FOUR_ROW8(Y9, Y1)
    FOUR_ROW8(Y10, Y2)
    FOUR_ROW8(Y11, Y3)
    FOUR_TRANSFORM(Y12)
    FOUR_CANDIDATE(32, 40, 48, 56)
    FOUR_ROW8(Y8, Y0)
    FOUR_ROW8(Y9, Y1)
    FOUR_ROW8(Y10, Y2)
    FOUR_ROW8(Y11, Y3)
    FOUR_TRANSFORM(Y13)
    FOUR_CANDIDATE(64, 72, 80, 88)
    FOUR_ROW8(Y8, Y0)
    FOUR_ROW8(Y9, Y1)
    FOUR_ROW8(Y10, Y2)
    FOUR_ROW8(Y11, Y3)
    FOUR_TRANSFORM(Y14)
    FOUR_CANDIDATE(96, 104, 112, 120)
    FOUR_ROW8(Y8, Y0)
    FOUR_ROW8(Y9, Y1)
    FOUR_ROW8(Y10, Y2)
    FOUR_ROW8(Y11, Y3)
    FOUR_TRANSFORM(Y15)
    ADDQ $8, R11
    SUBQ $8, R9
    JNZ four_rows8
    JMP four_reduce

four_rows8low:
    FOUR_ENC8LOW(X8, Y8)
    FOUR_ENC8LOW(X9, Y9)
    FOUR_ENC8LOW(X10, Y10)
    FOUR_ENC8LOW(X11, Y11)
    FOUR_CANDIDATE(0, 8, 16, 24)
    FOUR_ROW8LOW(Y8, X0, Y0)
    FOUR_ROW8LOW(Y9, X1, Y1)
    FOUR_ROW8LOW(Y10, X2, Y2)
    FOUR_ROW8LOW(Y11, X3, Y3)
    FOUR_TRANSFORM(Y12)
    FOUR_CANDIDATE(32, 40, 48, 56)
    FOUR_ROW8LOW(Y8, X0, Y0)
    FOUR_ROW8LOW(Y9, X1, Y1)
    FOUR_ROW8LOW(Y10, X2, Y2)
    FOUR_ROW8LOW(Y11, X3, Y3)
    FOUR_TRANSFORM(Y13)
    FOUR_CANDIDATE(64, 72, 80, 88)
    FOUR_ROW8LOW(Y8, X0, Y0)
    FOUR_ROW8LOW(Y9, X1, Y1)
    FOUR_ROW8LOW(Y10, X2, Y2)
    FOUR_ROW8LOW(Y11, X3, Y3)
    FOUR_TRANSFORM(Y14)
    FOUR_CANDIDATE(96, 104, 112, 120)
    FOUR_ROW8LOW(Y8, X0, Y0)
    FOUR_ROW8LOW(Y9, X1, Y1)
    FOUR_ROW8LOW(Y10, X2, Y2)
    FOUR_ROW8LOW(Y11, X3, Y3)
    FOUR_TRANSFORM(Y15)

four_reduce:
    MOVQ costs+40(FP), AX
    FOUR_REDUCE(Y12, X12, 0)
    FOUR_REDUCE(Y13, X13, 4)
    FOUR_REDUCE(Y14, X14, 8)
    FOUR_REDUCE(Y15, X15, 12)
    VZEROUPPER
    RET

DATA ·satdFourOnes+0(SB)/8, $0x0001000100010001
DATA ·satdFourOnes+8(SB)/8, $0x0001000100010001
DATA ·satdFourOnes+16(SB)/8, $0x0001000100010001
DATA ·satdFourOnes+24(SB)/8, $0x0001000100010001
GLOBL ·satdFourOnes(SB), RODATA|NOPTR, $32
