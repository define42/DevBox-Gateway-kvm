//go:build amd64

// Packed quantization follows Cisco OpenH264 codec/encoder/core/x86/quant.asm.
// Copyright (c) 2009-2013, Cisco Systems.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// I16 AC always scans all sixteen quantized blocks. There is no signed maximum
// reduction or zero shortcut: out-of-table magnitudes retain every bit exactly
// as WelsQuantFour4x4_sse2 followed by coefficientScanMask(ac=true).
// The checked entry excludes all output/table overlaps across the full spans.
// func quantScanIntraLumaAVX2(dct, levels *[256]int16, ff, mf *[8]int16, masks *[16]uint16)
TEXT ·quantScanIntraLumaAVX2(SB), NOSPLIT, $0-40
	MOVQ dct+0(FP), AX
	MOVQ levels+8(FP), BX
	MOVQ ff+16(FP), CX
	MOVQ mf+24(FP), DX
	MOVQ masks+32(FP), SI
	MOVL $8, R8
	VBROADCASTI128 (CX), Y8
	VBROADCASTI128 (DX), Y9
	VPXOR Y10, Y10, Y10
	VPCMPEQW Y11, Y11, Y11
	VPSRLW $1, Y11, Y11
	LEAQ intraACScanShuffle<>(SB), R10
	VBROADCASTI128 (R10), Y12
	VBROADCASTI128 16(R10), Y13
	VBROADCASTI128 32(R10), Y14
	VBROADCASTI128 48(R10), Y15
intra_ac_quant_scan_pair:
	VMOVDQU (AX), X0
	VINSERTI128 $1, 32(AX), Y0, Y0
	VMOVDQU 16(AX), X1
	VINSERTI128 $1, 48(AX), Y1, Y1
	VPABSW Y0, Y2
	VPOR Y11, Y0, Y0
	VPADDUSW Y8, Y2, Y2
	VPMULHUW Y9, Y2, Y2
	VPSIGNW Y0, Y2, Y0
	VPABSW Y1, Y3
	VPOR Y11, Y1, Y1
	VPADDUSW Y8, Y3, Y3
	VPMULHUW Y9, Y3, Y3
	VPSIGNW Y1, Y3, Y1
	VMOVDQU X0, (AX)
	VEXTRACTI128 $1, Y0, 32(AX)
	VMOVDQU X1, 16(AX)
	VEXTRACTI128 $1, Y1, 48(AX)

	VPSHUFB Y12, Y0, Y2
	VPSHUFB Y13, Y1, Y3
	VPOR Y3, Y2, Y2
	VPSHUFB Y14, Y0, Y4
	VPSHUFB Y15, Y1, Y5
	VPOR Y5, Y4, Y4
	VMOVDQU X2, (BX)
	VEXTRACTI128 $1, Y2, 32(BX)
	VMOVDQU X4, 16(BX)
	VEXTRACTI128 $1, Y4, 48(BX)
	VPCMPEQW Y10, Y2, Y2
	VPCMPEQW Y10, Y4, Y4
	VPACKSSWB Y4, Y2, Y2
	VPMOVMSKB Y2, R9
	XORL $-1, R9
	MOVL R9, (SI)
	ADDQ $64, AX
	ADDQ $64, BX
	ADDQ $4, SI
	DECL R8
	JNZ intra_ac_quant_scan_pair
	VZEROUPPER
	RET

// AC zigzag omits coefficient zero and explicitly writes the trailing zero.
DATA intraACScanShuffle<>+0(SB)/8, $0x0b0a808009080302
DATA intraACScanShuffle<>+8(SB)/8, $0x80800d0c07060504
DATA intraACScanShuffle<>+16(SB)/8, $0x8080010080808080
DATA intraACScanShuffle<>+24(SB)/8, $0x0302808080808080
DATA intraACScanShuffle<>+32(SB)/8, $0x0f0e808080808080
DATA intraACScanShuffle<>+40(SB)/8, $0x8080808080808080
DATA intraACScanShuffle<>+48(SB)/8, $0x808005040b0a0908
DATA intraACScanShuffle<>+56(SB)/8, $0x80800f0e0d0c0706
GLOBL intraACScanShuffle<>(SB), RODATA|NOPTR, $64
