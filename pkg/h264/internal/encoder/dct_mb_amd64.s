// This forward transform follows OpenH264 2.6.0's AVX2 implementation in
// codec/common/x86/dct.asm.
// Copyright (c) 2009-2013, Cisco Systems.
// All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

DATA dctMbShufflePixels<>+0(SB)/8, $0x8002800180038000
DATA dctMbShufflePixels<>+8(SB)/8, $0x8006800580078004
GLOBL dctMbShufflePixels<>(SB), RODATA|NOPTR, $16

DATA dctMbShufflePairs<>+0(SB)/8, $0x0504070601000302
DATA dctMbShufflePairs<>+8(SB)/8, $0x0d0c0f0e09080b0a
GLOBL dctMbShufflePairs<>(SB), RODATA|NOPTR, $16

DATA dctMbHorizontalSigns<>+0(SB)/8, $0xffff0001ffff0001
DATA dctMbHorizontalSigns<>+8(SB)/8, $0xffff0001ffff0001
DATA dctMbHorizontalSigns<>+16(SB)/8, $0xffff0001ffff0001
DATA dctMbHorizontalSigns<>+24(SB)/8, $0xffff0001ffff0001
GLOBL dctMbHorizontalSigns<>(SB), RODATA|NOPTR, $32

DATA dctMbHorizontalFactors<>+0(SB)/8, $0xfffeffff00020001
DATA dctMbHorizontalFactors<>+8(SB)/8, $0xfffeffff00020001
DATA dctMbHorizontalFactors<>+16(SB)/8, $0xfffeffff00020001
DATA dctMbHorizontalFactors<>+24(SB)/8, $0xfffeffff00020001
GLOBL dctMbHorizontalFactors<>(SB), RODATA|NOPTR, $32

// DCT_FOUR_T4 transforms four 4x4 blocks: AX = 64-coefficient output,
// BX/CX = first pixel block and stride, DX/SI = second pixel block and stride.
// Each 128-bit lane holds one horizontal pair; the upper lanes hold the bottom
// pair. The byte shuffle loads each row as [x0,x3,x1,x2] so each horizontal
// transform uses signs, a word multiply and two shuffles. Coefficients are
// stored row-major in TL, TR, BL, BR block order with unaligned stores.
// R10-R13 hold the shuffle, sign and factor tables (DCT_TABLES).
// Clobbers BX, DX and Y0-Y6.
#define DCT_TABLES \
	LEAQ dctMbShufflePixels<>(SB), R10; \
	LEAQ dctMbShufflePairs<>(SB), R11; \
	LEAQ dctMbHorizontalSigns<>(SB), R12; \
	LEAQ dctMbHorizontalFactors<>(SB), R13

#define DCT_FOUR_T4 \
	VBROADCASTI128 (R10), Y6; \
	VMOVQ (BX), X0; \
	VPBROADCASTQ (BX)(CX*4), Y4; \
	VPBLENDD $0xf0, Y4, Y0, Y0; \
	VPSHUFB Y6, Y0, Y0; \
	VMOVQ (DX), X4; \
	VPBROADCASTQ (DX)(SI*4), Y5; \
	VPBLENDD $0xf0, Y5, Y4, Y4; \
	VPSHUFB Y6, Y4, Y4; \
	VPSUBW Y4, Y0, Y0; \
	ADDQ CX, BX; \
	ADDQ SI, DX; \
	VMOVQ (BX), X1; \
	VPBROADCASTQ (BX)(CX*4), Y4; \
	VPBLENDD $0xf0, Y4, Y1, Y1; \
	VPSHUFB Y6, Y1, Y1; \
	VMOVQ (DX), X4; \
	VPBROADCASTQ (DX)(SI*4), Y5; \
	VPBLENDD $0xf0, Y5, Y4, Y4; \
	VPSHUFB Y6, Y4, Y4; \
	VPSUBW Y4, Y1, Y1; \
	ADDQ CX, BX; \
	ADDQ SI, DX; \
	VMOVQ (BX), X2; \
	VPBROADCASTQ (BX)(CX*4), Y4; \
	VPBLENDD $0xf0, Y4, Y2, Y2; \
	VPSHUFB Y6, Y2, Y2; \
	VMOVQ (DX), X4; \
	VPBROADCASTQ (DX)(SI*4), Y5; \
	VPBLENDD $0xf0, Y5, Y4, Y4; \
	VPSHUFB Y6, Y4, Y4; \
	VPSUBW Y4, Y2, Y2; \
	ADDQ CX, BX; \
	ADDQ SI, DX; \
	VMOVQ (BX), X3; \
	VPBROADCASTQ (BX)(CX*4), Y4; \
	VPBLENDD $0xf0, Y4, Y3, Y3; \
	VPSHUFB Y6, Y3, Y3; \
	VMOVQ (DX), X4; \
	VPBROADCASTQ (DX)(SI*4), Y5; \
	VPBLENDD $0xf0, Y5, Y4, Y4; \
	VPSHUFB Y6, Y4, Y4; \
	VPSUBW Y4, Y3, Y3; \
	VPSUBW Y3, Y0, Y5; \
	VPADDW Y3, Y0, Y0; \
	VPSUBW Y2, Y1, Y3; \
	VPADDW Y2, Y1, Y1; \
	VPSUBW Y1, Y0, Y2; \
	VPADDW Y1, Y0, Y0; \
	VPSLLW $1, Y5, Y1; \
	VPADDW Y3, Y1, Y1; \
	VPSLLW $1, Y3, Y3; \
	VPSUBW Y3, Y5, Y3; \
	VBROADCASTI128 (R11), Y6; \
	VPSIGNW (R12), Y0, Y5; \
	VPSHUFB Y6, Y0, Y0; \
	VPADDW Y5, Y0, Y0; \
	VPMULLW (R13), Y0, Y5; \
	VPSHUFD $0xb1, Y0, Y0; \
	VPADDW Y5, Y0, Y0; \
	VPSIGNW (R12), Y1, Y5; \
	VPSHUFB Y6, Y1, Y1; \
	VPADDW Y5, Y1, Y1; \
	VPMULLW (R13), Y1, Y5; \
	VPSHUFD $0xb1, Y1, Y1; \
	VPADDW Y5, Y1, Y1; \
	VPSIGNW (R12), Y2, Y5; \
	VPSHUFB Y6, Y2, Y2; \
	VPADDW Y5, Y2, Y2; \
	VPMULLW (R13), Y2, Y5; \
	VPSHUFD $0xb1, Y2, Y2; \
	VPADDW Y5, Y2, Y2; \
	VPSIGNW (R12), Y3, Y5; \
	VPSHUFB Y6, Y3, Y3; \
	VPADDW Y5, Y3, Y3; \
	VPMULLW (R13), Y3, Y5; \
	VPSHUFD $0xb1, Y3, Y3; \
	VPADDW Y5, Y3, Y3; \
	VPUNPCKLQDQ Y1, Y0, Y5; \
	VMOVDQU X5, 0(AX); \
	VEXTRACTI128 $1, Y5, 64(AX); \
	VPUNPCKHQDQ Y1, Y0, Y5; \
	VMOVDQU X5, 32(AX); \
	VEXTRACTI128 $1, Y5, 96(AX); \
	VPUNPCKLQDQ Y3, Y2, Y5; \
	VMOVDQU X5, 16(AX); \
	VEXTRACTI128 $1, Y5, 80(AX); \
	VPUNPCKHQDQ Y3, Y2, Y5; \
	VMOVDQU X5, 48(AX); \
	VEXTRACTI128 $1, Y5, 112(AX)

// func welsDctLumaMbAVX2(dct *[256]int16, enc *byte, encStride int, pred *[256]byte)
// Transforms the four 8x8 quadrants of a 16x16 macroblock residual in
// WelsDctMb order; pred has stride 16.
TEXT ·welsDctLumaMbAVX2(SB), NOSPLIT, $0-32
	MOVQ dct+0(FP), AX
	MOVQ enc+8(FP), R8
	MOVQ encStride+16(FP), CX
	MOVQ pred+24(FP), R9
	MOVQ $16, SI
	DCT_TABLES
	MOVQ R8, BX
	MOVQ R9, DX
	DCT_FOUR_T4
	ADDQ $128, AX
	LEAQ 8(R8), BX
	LEAQ 8(R9), DX
	DCT_FOUR_T4
	ADDQ $128, AX
	LEAQ (R8)(CX*8), BX
	LEAQ 128(R9), DX
	DCT_FOUR_T4
	ADDQ $128, AX
	LEAQ 8(R8)(CX*8), BX
	LEAQ 136(R9), DX
	DCT_FOUR_T4
	VZEROUPPER
	RET
