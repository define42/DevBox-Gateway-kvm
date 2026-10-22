// This forward transform follows OpenH264 2.6.0's AVX2 implementation in
// codec/common/x86/dct.asm.
// Copyright (c) 2009-2013, Cisco Systems.
// All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

DATA skipDctQuantShufflePixels<>+0(SB)/8, $0x8002800180038000
DATA skipDctQuantShufflePixels<>+8(SB)/8, $0x8006800580078004
GLOBL skipDctQuantShufflePixels<>(SB), RODATA|NOPTR, $16

DATA skipDctQuantShufflePairs<>+0(SB)/8, $0x0504070601000302
DATA skipDctQuantShufflePairs<>+8(SB)/8, $0x0d0c0f0e09080b0a
GLOBL skipDctQuantShufflePairs<>(SB), RODATA|NOPTR, $16

DATA skipDctQuantHorizontalSigns<>+0(SB)/8, $0xffff0001ffff0001
DATA skipDctQuantHorizontalSigns<>+8(SB)/8, $0xffff0001ffff0001
DATA skipDctQuantHorizontalSigns<>+16(SB)/8, $0xffff0001ffff0001
DATA skipDctQuantHorizontalSigns<>+24(SB)/8, $0xffff0001ffff0001
GLOBL skipDctQuantHorizontalSigns<>(SB), RODATA|NOPTR, $32

DATA skipDctQuantHorizontalFactors<>+0(SB)/8, $0xfffeffff00020001
DATA skipDctQuantHorizontalFactors<>+8(SB)/8, $0xfffeffff00020001
DATA skipDctQuantHorizontalFactors<>+16(SB)/8, $0xfffeffff00020001
DATA skipDctQuantHorizontalFactors<>+24(SB)/8, $0xfffeffff00020001
GLOBL skipDctQuantHorizontalFactors<>(SB), RODATA|NOPTR, $32

// func skipDctQuant8x8AVX2(dct *[64]int16, pixel1 *byte, stride1 int,
//     pixel2 *byte, stride2 int, ff, mf *[8]int16, maxima *[4]int16)
// Each 128-bit lane holds one horizontal pair; the upper lanes hold the
// bottom pair. Eight-byte loads touch exactly the validated pixel columns.
TEXT ·skipDctQuant8x8AVX2(SB), NOSPLIT, $0-64
	MOVQ dct+0(FP), AX
	MOVQ pixel1+8(FP), BX
	MOVQ stride1+16(FP), CX
	MOVQ pixel2+24(FP), DX
	MOVQ stride2+32(FP), SI
	VBROADCASTI128 skipDctQuantShufflePixels<>(SB), Y6

	VMOVQ (BX), X0
	VPBROADCASTQ (BX)(CX*4), Y4
	VPBLENDD $0xf0, Y4, Y0, Y0
	VPSHUFB Y6, Y0, Y0
	VMOVQ (DX), X4
	VPBROADCASTQ (DX)(SI*4), Y5
	VPBLENDD $0xf0, Y5, Y4, Y4
	VPSHUFB Y6, Y4, Y4
	VPSUBW Y4, Y0, Y0
	ADDQ CX, BX
	ADDQ SI, DX
	VMOVQ (BX), X1
	VPBROADCASTQ (BX)(CX*4), Y4
	VPBLENDD $0xf0, Y4, Y1, Y1
	VPSHUFB Y6, Y1, Y1
	VMOVQ (DX), X4
	VPBROADCASTQ (DX)(SI*4), Y5
	VPBLENDD $0xf0, Y5, Y4, Y4
	VPSHUFB Y6, Y4, Y4
	VPSUBW Y4, Y1, Y1
	ADDQ CX, BX
	ADDQ SI, DX
	VMOVQ (BX), X2
	VPBROADCASTQ (BX)(CX*4), Y4
	VPBLENDD $0xf0, Y4, Y2, Y2
	VPSHUFB Y6, Y2, Y2
	VMOVQ (DX), X4
	VPBROADCASTQ (DX)(SI*4), Y5
	VPBLENDD $0xf0, Y5, Y4, Y4
	VPSHUFB Y6, Y4, Y4
	VPSUBW Y4, Y2, Y2
	ADDQ CX, BX
	ADDQ SI, DX
	VMOVQ (BX), X3
	VPBROADCASTQ (BX)(CX*4), Y4
	VPBLENDD $0xf0, Y4, Y3, Y3
	VPSHUFB Y6, Y3, Y3
	VMOVQ (DX), X4
	VPBROADCASTQ (DX)(SI*4), Y5
	VPBLENDD $0xf0, Y5, Y4, Y4
	VPSHUFB Y6, Y4, Y4
	VPSUBW Y4, Y3, Y3

	// Vertical 4-point transform of all four blocks together.
	VPSUBW Y3, Y0, Y5
	VPADDW Y3, Y0, Y0
	VPSUBW Y2, Y1, Y3
	VPADDW Y2, Y1, Y1
	VPSUBW Y1, Y0, Y2
	VPADDW Y1, Y0, Y0
	VPSLLW $1, Y5, Y1
	VPADDW Y3, Y1, Y1
	VPSLLW $1, Y3, Y3
	VPSUBW Y3, Y5, Y3

	// The byte shuffle loaded each row as [x0,x3,x1,x2], allowing each
	// horizontal transform to use signs, a word multiply and two shuffles.
	VBROADCASTI128 skipDctQuantShufflePairs<>(SB), Y6
	VPSIGNW skipDctQuantHorizontalSigns<>(SB), Y0, Y5
	VPSHUFB Y6, Y0, Y0
	VPADDW Y5, Y0, Y0
	VPMULLW skipDctQuantHorizontalFactors<>(SB), Y0, Y5
	VPSHUFD $0xb1, Y0, Y0
	VPADDW Y5, Y0, Y0
	VPSIGNW skipDctQuantHorizontalSigns<>(SB), Y1, Y5
	VPSHUFB Y6, Y1, Y1
	VPADDW Y5, Y1, Y1
	VPMULLW skipDctQuantHorizontalFactors<>(SB), Y1, Y5
	VPSHUFD $0xb1, Y1, Y1
	VPADDW Y5, Y1, Y1
	VPSIGNW skipDctQuantHorizontalSigns<>(SB), Y2, Y5
	VPSHUFB Y6, Y2, Y2
	VPADDW Y5, Y2, Y2
	VPMULLW skipDctQuantHorizontalFactors<>(SB), Y2, Y5
	VPSHUFD $0xb1, Y2, Y2
	VPADDW Y5, Y2, Y2
	VPSIGNW skipDctQuantHorizontalSigns<>(SB), Y3, Y5
	VPSHUFB Y6, Y3, Y3
	VPADDW Y5, Y3, Y3
	VPMULLW skipDctQuantHorizontalFactors<>(SB), Y3, Y5
	VPSHUFD $0xb1, Y3, Y3
	VPADDW Y5, Y3, Y3

	// Keep each block's first/last eight coefficients in the corresponding
	// 128-bit lane. Upper lanes are the bottom pair of blocks.
	VPUNPCKLQDQ Y1, Y0, Y4
	VPUNPCKHQDQ Y1, Y0, Y5
	VPUNPCKLQDQ Y3, Y2, Y6
	VPUNPCKHQDQ Y3, Y2, Y7
	MOVQ ff+40(FP), BX
	MOVQ mf+48(FP), CX
	MOVQ maxima+56(FP), DI
	VBROADCASTI128 (BX), Y8
	VBROADCASTI128 (CX), Y9
	VPXOR Y10, Y10, Y10
	VPCMPEQW Y11, Y11, Y11
	VPSRLW $1, Y11, Y11

	// Match quantFour4x4MaxAVX2 for every FF/MF bit pattern: unsigned
	// saturation/multiply, signed maxima clamped at zero, and positive
	// sign for zero coefficients that rounding makes nonzero.
	VPABSW Y4, Y0
	VPOR Y11, Y4, Y4
	VPADDUSW Y8, Y0, Y0
	VPMULHUW Y9, Y0, Y0
	VPSIGNW Y4, Y0, Y4
	VPMAXSW Y10, Y0, Y12
	VPABSW Y6, Y0
	VPOR Y11, Y6, Y6
	VPADDUSW Y8, Y0, Y0
	VPMULHUW Y9, Y0, Y0
	VPSIGNW Y6, Y0, Y6
	VPMAXSW Y0, Y12, Y12

	VPABSW Y5, Y0
	VPOR Y11, Y5, Y5
	VPADDUSW Y8, Y0, Y0
	VPMULHUW Y9, Y0, Y0
	VPSIGNW Y5, Y0, Y5
	VPMAXSW Y10, Y0, Y13
	VPABSW Y7, Y0
	VPOR Y11, Y7, Y7
	VPADDUSW Y8, Y0, Y0
	VPMULHUW Y9, Y0, Y0
	VPSIGNW Y7, Y0, Y7
	VPMAXSW Y0, Y13, Y13

	VMOVDQU X4, 0(AX)
	VEXTRACTI128 $1, Y4, 64(AX)
	VMOVDQU X5, 32(AX)
	VEXTRACTI128 $1, Y5, 96(AX)
	VMOVDQU X6, 16(AX)
	VEXTRACTI128 $1, Y6, 80(AX)
	VMOVDQU X7, 48(AX)
	VEXTRACTI128 $1, Y7, 112(AX)

	VPSRLDQ $8, Y12, Y0
	VPMAXSW Y0, Y12, Y12
	VPSRLDQ $4, Y12, Y0
	VPMAXSW Y0, Y12, Y12
	VPSRLDQ $2, Y12, Y0
	VPMAXSW Y0, Y12, Y12
	VPSRLDQ $8, Y13, Y0
	VPMAXSW Y0, Y13, Y13
	VPSRLDQ $4, Y13, Y0
	VPMAXSW Y0, Y13, Y13
	VPSRLDQ $2, Y13, Y0
	VPMAXSW Y0, Y13, Y13
	VPEXTRW $0, X12, R8
	VPEXTRW $0, X13, R9
	SHLL $16, R9
	ORL R9, R8
	MOVL R8, (DI)
	VEXTRACTI128 $1, Y12, X0
	VEXTRACTI128 $1, Y13, X1
	VPEXTRW $0, X0, R8
	VPEXTRW $0, X1, R9
	SHLL $16, R9
	ORL R9, R8
	MOVL R8, 4(DI)
	VZEROUPPER
	RET
