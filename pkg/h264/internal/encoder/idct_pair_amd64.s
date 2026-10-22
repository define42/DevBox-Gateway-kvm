//go:build amd64

// The four-block horizontal transform and coefficient layout below follow
// OpenH264 2.6.0, codec/common/x86/dct.asm.
// Copyright (c) 2009-2013, Cisco Systems.
// All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// Reconstruct two horizontally adjacent 4x4 blocks per kernel call. The
// horizontal transform retains int16 wrapping; AVX2 widens all eight columns
// before the vertical transform, preserving the scalar's full int16 domain.
TEXT ·idctFourT4RecAVX2(SB), NOSPLIT, $0-88
	MOVQ rec_base+0(FP), R9
	MOVQ recStride+24(FP), CX
	MOVQ pred_base+32(FP), R10
	MOVQ predStride+56(FP), SI
	MOVQ coeff_base+64(FP), R8
	MOVQ R9, BX
	MOVQ R10, DX
	MOVQ R8, AX
	CALL idctTwoT4RecAVX2Kernel<>(SB)
	LEAQ (R9)(CX*4), BX
	LEAQ (R10)(SI*4), DX
	LEAQ 64(R8), AX
	CALL idctTwoT4RecAVX2Kernel<>(SB)
	RET

// AX coefficients, BX output, CX output stride, DX prediction, SI prediction
// stride. R8-R10 survive. Each XMM contains one row from each independent block.
TEXT idctTwoT4RecAVX2Kernel<>(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ 0(AX), X0
	MOVQ 32(AX), X4
	PUNPCKLQDQ X4, X0
	MOVQ 8(AX), X1
	MOVQ 40(AX), X4
	PUNPCKLQDQ X4, X1
	MOVQ 16(AX), X2
	MOVQ 48(AX), X4
	PUNPCKLQDQ X4, X2
	MOVQ 24(AX), X3
	MOVQ 56(AX), X4
	PUNPCKLQDQ X4, X3
	// Transpose each 4x4 matrix so packed word arithmetic runs horizontally.
	MOVOU X0, X4
	PUNPCKLWL X1, X4
	MOVOU X0, X6
	PUNPCKHWL X1, X6
	MOVOU X2, X5
	PUNPCKLWL X3, X5
	MOVOU X2, X7
	PUNPCKHWL X3, X7
	MOVOU X4, X8
	PUNPCKLLQ X5, X8
	PUNPCKHLQ X5, X4
	MOVOU X6, X9
	PUNPCKLLQ X7, X9
	PUNPCKHLQ X7, X6
	MOVOU X8, X0
	PUNPCKLQDQ X9, X0
	MOVOU X8, X1
	PUNPCKHQDQ X9, X1
	MOVOU X4, X2
	PUNPCKLQDQ X6, X2
	MOVOU X4, X3
	PUNPCKHQDQ X6, X3
	MOVOU X0, X4
	PADDW X2, X4
	PSUBW X2, X0
	MOVOU X3, X5
	PSRAW $1, X5
	PADDW X1, X5
	PSRAW $1, X1
	PSUBW X3, X1
	MOVOU X0, X2
	PSUBW X1, X2
	PADDW X0, X1
	MOVOU X4, X3
	PSUBW X5, X3
	MOVOU X4, X0
	PADDW X5, X0
	// Restore rows, then sign-extend every horizontal result to a dword.
	MOVOU X0, X4
	PUNPCKLWL X1, X4
	MOVOU X0, X6
	PUNPCKHWL X1, X6
	MOVOU X2, X5
	PUNPCKLWL X3, X5
	MOVOU X2, X7
	PUNPCKHWL X3, X7
	MOVOU X4, X8
	PUNPCKLLQ X5, X8
	PUNPCKHLQ X5, X4
	MOVOU X6, X9
	PUNPCKLLQ X7, X9
	PUNPCKHLQ X7, X6
	MOVOU X8, X0
	PUNPCKLQDQ X9, X0
	MOVOU X8, X1
	PUNPCKHQDQ X9, X1
	MOVOU X4, X2
	PUNPCKLQDQ X6, X2
	MOVOU X4, X3
	PUNPCKHQDQ X6, X3
	VPMOVSXWD X0, Y0
	VPMOVSXWD X1, Y1
	VPMOVSXWD X2, Y2
	VPMOVSXWD X3, Y3
	VPADDD Y2, Y0, Y4
	VPSUBD Y2, Y0, Y0
	VPSRAD $1, Y3, Y5
	VPADDD Y1, Y5, Y5
	VPSRAD $1, Y1, Y1
	VPSUBD Y3, Y1, Y1
	VPSUBD Y1, Y0, Y2
	VPADDD Y0, Y1, Y1
	VPSUBD Y5, Y4, Y3
	VPADDD Y5, Y4, Y0
	MOVL $32, R11
	VMOVD R11, X7
	VPBROADCASTD X7, Y7
	VPADDD Y7, Y0, Y0
	VPSRAD $6, Y0, Y0
	VPMOVZXBD (DX), Y4
	VPADDD Y4, Y0, Y0
	VEXTRACTI128 $1, Y0, X4
	VPACKSSDW X4, X0, X0
	VPACKUSWB X0, X0, X0
	VMOVQ X0, (BX)
	ADDQ CX, BX
	ADDQ SI, DX
	VPADDD Y7, Y1, Y1
	VPSRAD $6, Y1, Y1
	VPMOVZXBD (DX), Y4
	VPADDD Y4, Y1, Y1
	VEXTRACTI128 $1, Y1, X4
	VPACKSSDW X4, X1, X1
	VPACKUSWB X1, X1, X1
	VMOVQ X1, (BX)
	ADDQ CX, BX
	ADDQ SI, DX
	VPADDD Y7, Y2, Y2
	VPSRAD $6, Y2, Y2
	VPMOVZXBD (DX), Y4
	VPADDD Y4, Y2, Y2
	VEXTRACTI128 $1, Y2, X4
	VPACKSSDW X4, X2, X2
	VPACKUSWB X2, X2, X2
	VMOVQ X2, (BX)
	ADDQ CX, BX
	ADDQ SI, DX
	VPADDD Y7, Y3, Y3
	VPSRAD $6, Y3, Y3
	VPMOVZXBD (DX), Y4
	VPADDD Y4, Y3, Y3
	VEXTRACTI128 $1, Y3, X4
	VPACKSSDW X4, X3, X3
	VPACKUSWB X3, X3, X3
	VMOVQ X3, (BX)
	VZEROUPPER
	RET

// Load all four blocks once and transform their rows together. Horizontal word
// arithmetic wraps exactly as the scalar int16 temporary. The vertical pass
// widens BEFORE any add/subtract, retaining full-int16 scalar results even when
// native's word-only vertical arithmetic would overflow. All pixel accesses
// are exactly eight bytes; coefficient loads cover exactly 128 bytes.
TEXT ·idctFourT4RecPackedAVX2(SB), NOSPLIT, $0-40
	MOVQ rec+0(FP), BX
	MOVQ recStride+8(FP), CX
	MOVQ pred+16(FP), DX
	MOVQ predStride+24(FP), SI
	MOVQ coeff+32(FP), AX

	// Each YMM holds one row from TL/TR in its low lane and BL/BR in
	// its high lane. Adjacent coefficient rows share one unaligned load.
	VMOVDQU 0(AX), X1
	VINSERTI128 $1, 64(AX), Y1, Y1
	VMOVDQU 32(AX), X4
	VINSERTI128 $1, 96(AX), Y4, Y4
	VPUNPCKLQDQ Y4, Y1, Y0
	VPUNPCKHQDQ Y4, Y1, Y1
	VMOVDQU 16(AX), X3
	VINSERTI128 $1, 80(AX), Y3, Y3
	VMOVDQU 48(AX), X4
	VINSERTI128 $1, 112(AX), Y4, Y4
	VPUNPCKLQDQ Y4, Y3, Y2
	VPUNPCKHQDQ Y4, Y3, Y3

	VBROADCASTI128 idctFourShufflePairs<>(SB), Y12
	VBROADCASTI128 idctFourShuffleColumns<>(SB), Y13
	VBROADCASTI128 idctFourSignsPairs<>(SB), Y14
	VBROADCASTI128 idctFourSignsAlternating<>(SB), Y15

	// Four horizontal transforms: [x0,x1,x2,x3] -> [y0,y3,y1,y2].
	VPSRAW $1, Y0, Y4
	VPBLENDW $0xaa, Y4, Y0, Y4
	VPSIGNW Y14, Y0, Y0
	VPSHUFD $0xb1, Y4, Y4
	VPADDW Y4, Y0, Y0
	VPSHUFB Y12, Y0, Y4
	VPSIGNW Y15, Y0, Y0
	VPADDW Y4, Y0, Y0
	VPSHUFB Y13, Y0, Y0

	// Four horizontal transforms: [x0,x1,x2,x3] -> [y0,y3,y1,y2].
	VPSRAW $1, Y1, Y4
	VPBLENDW $0xaa, Y4, Y1, Y4
	VPSIGNW Y14, Y1, Y1
	VPSHUFD $0xb1, Y4, Y4
	VPADDW Y4, Y1, Y1
	VPSHUFB Y12, Y1, Y4
	VPSIGNW Y15, Y1, Y1
	VPADDW Y4, Y1, Y1
	VPSHUFB Y13, Y1, Y1

	// Four horizontal transforms: [x0,x1,x2,x3] -> [y0,y3,y1,y2].
	VPSRAW $1, Y2, Y4
	VPBLENDW $0xaa, Y4, Y2, Y4
	VPSIGNW Y14, Y2, Y2
	VPSHUFD $0xb1, Y4, Y4
	VPADDW Y4, Y2, Y2
	VPSHUFB Y12, Y2, Y4
	VPSIGNW Y15, Y2, Y2
	VPADDW Y4, Y2, Y2
	VPSHUFB Y13, Y2, Y2

	// Four horizontal transforms: [x0,x1,x2,x3] -> [y0,y3,y1,y2].
	VPSRAW $1, Y3, Y4
	VPBLENDW $0xaa, Y4, Y3, Y4
	VPSIGNW Y14, Y3, Y3
	VPSHUFD $0xb1, Y4, Y4
	VPADDW Y4, Y3, Y3
	VPSHUFB Y12, Y3, Y4
	VPSIGNW Y15, Y3, Y3
	VPADDW Y4, Y3, Y3
	VPSHUFB Y13, Y3, Y3

	MOVL $32, AX
	VMOVD AX, X15
	VPBROADCASTD X15, Y15
	MOVL $2, AX
idct_four_vertical:
	VPMOVSXWD X0, Y4
	VPMOVSXWD X1, Y5
	VPMOVSXWD X2, Y6
	VPMOVSXWD X3, Y7
	VPADDD Y6, Y4, Y8
	VPSUBD Y6, Y4, Y4
	VPSRAD $1, Y7, Y9
	VPADDD Y5, Y9, Y9
	VPSRAD $1, Y5, Y5
	VPSUBD Y7, Y5, Y5
	VPSUBD Y5, Y4, Y6
	VPADDD Y4, Y5, Y5
	VPSUBD Y9, Y8, Y7
	VPADDD Y9, Y8, Y4

	VPADDD Y15, Y4, Y4
	VPSRAD $6, Y4, Y4
	VPMOVZXBD (DX), Y8
	VPADDD Y8, Y4, Y4
	VEXTRACTI128 $1, Y4, X8
	VPACKSSDW X8, X4, X4
	VPACKUSWB X4, X4, X4
	VMOVQ X4, (BX)
	ADDQ CX, BX
	ADDQ SI, DX

	VPADDD Y15, Y5, Y5
	VPSRAD $6, Y5, Y5
	VPMOVZXBD (DX), Y8
	VPADDD Y8, Y5, Y5
	VEXTRACTI128 $1, Y5, X8
	VPACKSSDW X8, X5, X5
	VPACKUSWB X5, X5, X5
	VMOVQ X5, (BX)
	ADDQ CX, BX
	ADDQ SI, DX

	VPADDD Y15, Y6, Y6
	VPSRAD $6, Y6, Y6
	VPMOVZXBD (DX), Y8
	VPADDD Y8, Y6, Y6
	VEXTRACTI128 $1, Y6, X8
	VPACKSSDW X8, X6, X6
	VPACKUSWB X6, X6, X6
	VMOVQ X6, (BX)
	ADDQ CX, BX
	ADDQ SI, DX

	VPADDD Y15, Y7, Y7
	VPSRAD $6, Y7, Y7
	VPMOVZXBD (DX), Y8
	VPADDD Y8, Y7, Y7
	VEXTRACTI128 $1, Y7, X8
	VPACKSSDW X8, X7, X7
	VPACKUSWB X7, X7, X7
	VMOVQ X7, (BX)
	ADDQ CX, BX
	ADDQ SI, DX

	DECL AX
	JZ idct_four_done
	VEXTRACTI128 $1, Y0, X0
	VEXTRACTI128 $1, Y1, X1
	VEXTRACTI128 $1, Y2, X2
	VEXTRACTI128 $1, Y3, X3
	JMP idct_four_vertical
idct_four_done:
	VZEROUPPER
	RET

// Swap adjacent words within each four-word block.
DATA idctFourShufflePairs<>+0(SB)/8, $0x0504070601000302
DATA idctFourShufflePairs<>+8(SB)/8, $0x0d0c0f0e09080b0a
GLOBL idctFourShufflePairs<>(SB), RODATA|NOPTR, $16

// Restore [y0,y3,y1,y2] to pixel column order.
DATA idctFourShuffleColumns<>+0(SB)/8, $0x0302070605040100
DATA idctFourShuffleColumns<>+8(SB)/8, $0x0b0a0f0e0d0c0908
GLOBL idctFourShuffleColumns<>(SB), RODATA|NOPTR, $16

DATA idctFourSignsPairs<>+0(SB)/8, $0xffffffff00010001
DATA idctFourSignsPairs<>+8(SB)/8, $0xffffffff00010001
GLOBL idctFourSignsPairs<>(SB), RODATA|NOPTR, $16

DATA idctFourSignsAlternating<>+0(SB)/8, $0xffff0001ffff0001
DATA idctFourSignsAlternating<>+8(SB)/8, $0xffff0001ffff0001
GLOBL idctFourSignsAlternating<>(SB), RODATA|NOPTR, $16
