// This forward transform follows OpenH264 2.6.0's AVX2 implementation in
// codec/common/x86/dct.asm.
// Copyright (c) 2009-2013, Cisco Systems.
// All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

DATA dctShufflePixels<>+0(SB)/8, $0x8002800180038000
DATA dctShufflePixels<>+8(SB)/8, $0x8006800580078004
GLOBL dctShufflePixels<>(SB), RODATA|NOPTR, $16

DATA dctShufflePairs<>+0(SB)/8, $0x0504070601000302
DATA dctShufflePairs<>+8(SB)/8, $0x0d0c0f0e09080b0a
GLOBL dctShufflePairs<>(SB), RODATA|NOPTR, $16

DATA dctHorizontalSigns<>+0(SB)/8, $0xffff0001ffff0001
DATA dctHorizontalSigns<>+8(SB)/8, $0xffff0001ffff0001
DATA dctHorizontalSigns<>+16(SB)/8, $0xffff0001ffff0001
DATA dctHorizontalSigns<>+24(SB)/8, $0xffff0001ffff0001
GLOBL dctHorizontalSigns<>(SB), RODATA|NOPTR, $32

DATA dctHorizontalFactors<>+0(SB)/8, $0xfffeffff00020001
DATA dctHorizontalFactors<>+8(SB)/8, $0xfffeffff00020001
DATA dctHorizontalFactors<>+16(SB)/8, $0xfffeffff00020001
DATA dctHorizontalFactors<>+24(SB)/8, $0xfffeffff00020001
GLOBL dctHorizontalFactors<>(SB), RODATA|NOPTR, $32

// func welsDctFourT4AVX2(dct *[64]int16, pixel1 *byte, stride1 int,
//     pixel2 *byte, stride2 int)
// Each 128-bit lane holds one horizontal pair; the upper lanes hold the
// bottom pair. Eight-byte loads touch exactly the validated pixel columns.
TEXT ·welsDctFourT4AVX2(SB), NOSPLIT, $0-40
	MOVQ dct+0(FP), AX
	MOVQ pixel1+8(FP), BX
	MOVQ stride1+16(FP), CX
	MOVQ pixel2+24(FP), DX
	MOVQ stride2+32(FP), SI
	VBROADCASTI128 dctShufflePixels<>(SB), Y6

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
	VBROADCASTI128 dctShufflePairs<>(SB), Y6
	VPSIGNW dctHorizontalSigns<>(SB), Y0, Y5
	VPSHUFB Y6, Y0, Y0
	VPADDW Y5, Y0, Y0
	VPMULLW dctHorizontalFactors<>(SB), Y0, Y5
	VPSHUFD $0xb1, Y0, Y0
	VPADDW Y5, Y0, Y0
	VPSIGNW dctHorizontalSigns<>(SB), Y1, Y5
	VPSHUFB Y6, Y1, Y1
	VPADDW Y5, Y1, Y1
	VPMULLW dctHorizontalFactors<>(SB), Y1, Y5
	VPSHUFD $0xb1, Y1, Y1
	VPADDW Y5, Y1, Y1
	VPSIGNW dctHorizontalSigns<>(SB), Y2, Y5
	VPSHUFB Y6, Y2, Y2
	VPADDW Y5, Y2, Y2
	VPMULLW dctHorizontalFactors<>(SB), Y2, Y5
	VPSHUFD $0xb1, Y2, Y2
	VPADDW Y5, Y2, Y2
	VPSIGNW dctHorizontalSigns<>(SB), Y3, Y5
	VPSHUFB Y6, Y3, Y3
	VPADDW Y5, Y3, Y3
	VPMULLW dctHorizontalFactors<>(SB), Y3, Y5
	VPSHUFD $0xb1, Y3, Y3
	VPADDW Y5, Y3, Y3

	// Store row-major coefficients in TL, TR, BL, BR block order. Go does
	// not guarantee vector alignment, so all output stores are unaligned.
	VPUNPCKLQDQ Y1, Y0, Y5
	VMOVDQU X5, 0(AX)
	VEXTRACTI128 $1, Y5, 64(AX)
	VPUNPCKHQDQ Y1, Y0, Y5
	VMOVDQU X5, 32(AX)
	VEXTRACTI128 $1, Y5, 96(AX)
	VPUNPCKLQDQ Y3, Y2, Y5
	VMOVDQU X5, 16(AX)
	VEXTRACTI128 $1, Y5, 80(AX)
	VPUNPCKHQDQ Y3, Y2, Y5
	VMOVDQU X5, 48(AX)
	VEXTRACTI128 $1, Y5, 112(AX)
	VZEROUPPER
	RET
