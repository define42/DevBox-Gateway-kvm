//go:build amd64

// The packed sum/difference SATD reduction follows OpenH264's AVX2
// implementation in codec/common/x86/satd_sad.asm. Scoring both half-pixel
// predictions in one call is additional Go-specific fusion.
// Copyright (c) 2009-2013, Cisco Systems. All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

#define PAIR_ENC16(Y) \
    VBROADCASTI128 (AX), Y; \
    VPMADDUBSW ·satdPairSigns(SB), Y, Y; \
    ADDQ CX, AX

#define PAIR_ROW16(ENC, OUT, PTR) \
    VBROADCASTI128 (PTR), OUT; \
    VPMADDUBSW ·satdPairSigns(SB), OUT, OUT; \
    VPSUBW OUT, ENC, OUT; \
    ADDQ DX, PTR

// Rows y and y+4 occupy independent lanes, exactly as the retained 8-row SATD.
#define PAIR_ENC8(Y) \
    VPBROADCASTQ (AX), Y; \
    VPBROADCASTQ (AX)(CX*4), Y4; \
    VPBLENDD $0xf0, Y4, Y, Y; \
    VPMADDUBSW ·satdPairSigns8(SB), Y, Y; \
    ADDQ CX, AX

#define PAIR_ROW8(ENC, OUT, PTR) \
    VPBROADCASTQ (PTR), OUT; \
    VPBROADCASTQ (PTR)(DX*4), Y4; \
    VPBLENDD $0xf0, Y4, OUT, OUT; \
    VPMADDUBSW ·satdPairSigns8(SB), OUT, OUT; \
    VPSUBW OUT, ENC, OUT; \
    ADDQ DX, PTR

// Y8-Y11 cache encoded horizontal pairs; Y12/Y13 accumulate independent costs.
// Y0-Y7 are scratch. Inputs are read-only, including aliased predictions.
#define PAIR_TRANSFORM(ACC) \
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

// Tracked-pointer ABI, no local descriptors/output pointers and no R14 use.
// Width and height are each 8 or 16. Every load is exactly width bytes.
TEXT ·satdTwoAVX2(SB), NOSPLIT, $0-64
    MOVQ enc+0(FP), AX
    MOVQ encStride+8(FP), CX
    MOVQ first+16(FP), BX
    MOVQ second+24(FP), SI
    MOVQ predStride+32(FP), DX
    MOVQ width+40(FP), R8
    MOVQ height+48(FP), R9
    VPXOR Y12, Y12, Y12
    VPXOR Y13, Y13, Y13
    CMPQ R8, $16
    JNE pair_rows8
pair_rows16:
    PAIR_ENC16(Y8)
    PAIR_ENC16(Y9)
    PAIR_ENC16(Y10)
    PAIR_ENC16(Y11)
    PAIR_ROW16(Y8, Y0, BX)
    PAIR_ROW16(Y9, Y1, BX)
    PAIR_ROW16(Y10, Y2, BX)
    PAIR_ROW16(Y11, Y3, BX)
    PAIR_TRANSFORM(Y12)
    PAIR_ROW16(Y8, Y0, SI)
    PAIR_ROW16(Y9, Y1, SI)
    PAIR_ROW16(Y10, Y2, SI)
    PAIR_ROW16(Y11, Y3, SI)
    PAIR_TRANSFORM(Y13)
    SUBQ $4, R9
    JNZ pair_rows16
    JMP pair_reduce
pair_rows8:
    PAIR_ENC8(Y8)
    PAIR_ENC8(Y9)
    PAIR_ENC8(Y10)
    PAIR_ENC8(Y11)
    LEAQ (AX)(CX*4), AX
    PAIR_ROW8(Y8, Y0, BX)
    PAIR_ROW8(Y9, Y1, BX)
    PAIR_ROW8(Y10, Y2, BX)
    PAIR_ROW8(Y11, Y3, BX)
    LEAQ (BX)(DX*4), BX
    PAIR_TRANSFORM(Y12)
    PAIR_ROW8(Y8, Y0, SI)
    PAIR_ROW8(Y9, Y1, SI)
    PAIR_ROW8(Y10, Y2, SI)
    PAIR_ROW8(Y11, Y3, SI)
    LEAQ (SI)(DX*4), SI
    PAIR_TRANSFORM(Y13)
    SUBQ $8, R9
    JNZ pair_rows8
pair_reduce:
    VEXTRACTI128 $1, Y12, X0
    VPADDD X12, X0, X0
    VPSHUFD $0x4e, X0, X1
    VPADDD X1, X0, X0
    VPSHUFD $0xb1, X0, X1
    VPADDD X1, X0, X0
    VMOVQ X0, AX
    MOVL AX, firstCost+56(FP)
    VEXTRACTI128 $1, Y13, X0
    VPADDD X13, X0, X0
    VPSHUFD $0x4e, X0, X1
    VPADDD X1, X0, X0
    VPSHUFD $0xb1, X0, X1
    VPADDD X1, X0, X0
    VMOVQ X0, AX
    MOVL AX, secondCost+60(FP)
    VZEROUPPER
    RET
