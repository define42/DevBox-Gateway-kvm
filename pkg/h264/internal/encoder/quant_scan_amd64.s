//go:build amd64

// The AVX2 quantization arithmetic follows Cisco OpenH264,
// codec/encoder/core/x86/quant.asm.
// Copyright (c) 2009-2013, Cisco Systems.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// The four blocks share FF/MF and zigzag vectors. Quantized raster coefficients,
// scanned entropy levels, maxima, and masks are all produced in the same pass.
TEXT ·quantScanFour4x4SSSE3(SB), NOSPLIT, $0-52
	MOVQ dct+0(FP), AX
	MOVQ levels+8(FP), BX
	MOVQ ff+16(FP), CX
	MOVQ mf+24(FP), DX
	MOVQ maxima+32(FP), DI
	MOVQ masks+40(FP), SI
	MOVL ac+48(FP), R9
	MOVQ $4, R8
	JMP quantScanBlocksSSSE3<>(SB)

// Completed luma encoding shares the exact same block loop, with one table
// snapshot and one Go/assembly boundary for sixteen blocks instead of four.
TEXT ·quantScanLumaSSSE3(SB), NOSPLIT, $0-48
	MOVQ dct+0(FP), AX
	MOVQ levels+8(FP), BX
	MOVQ ff+16(FP), CX
	MOVQ mf+24(FP), DX
	MOVQ maxima+32(FP), DI
	MOVQ masks+40(FP), SI
	XORL R9, R9
	MOVQ $16, R8
	JMP quantScanBlocksSSSE3<>(SB)

// AX/BX raster/scanned coefficients; CX/DX FF/MF; DI/SI maxima/masks;
// R8 block count; R9 AC-only flag. Entries validate all spans and alias rules.
TEXT quantScanBlocksSSSE3<>(SB), NOSPLIT|NOFRAME, $0-0
	SHLQ $6, R9
	LEAQ quantScanShuffle<>(SB), R10
	ADDQ R9, R10
	MOVOU (CX), X8
	MOVOU (DX), X9
	PXOR X10, X10
	MOVOU (R10), X12
	MOVOU 16(R10), X13
	MOVOU 32(R10), X14
	MOVOU 48(R10), X15
quant_scan_block:
	MOVOU (AX), X0
	MOVOU X10, X2
	PCMPGTW X0, X2
	PXOR X2, X0
	PSUBW X2, X0
	PADDUSW X8, X0
	PMULHUW X9, X0
	MOVOU X10, X4
	PMAXSW X0, X4
	PXOR X2, X0
	PSUBW X2, X0
	MOVOU X0, (AX)

	MOVOU 16(AX), X1
	MOVOU X10, X3
	PCMPGTW X1, X3
	PXOR X3, X1
	PSUBW X3, X1
	PADDUSW X8, X1
	PMULHUW X9, X1
	PMAXSW X1, X4
	PXOR X3, X1
	PSUBW X3, X1
	MOVOU X1, 16(AX)

	MOVOU X4, X5
	PSRLDQ $8, X5
	PMAXSW X5, X4
	MOVOU X4, X5
	PSRLDQ $4, X5
	PMAXSW X5, X4
	MOVOU X4, X5
	PSRLDQ $2, X5
	PMAXSW X5, X4
	PEXTRW $0, X4, R9
	MOVW R9, (DI)
	TESTL R9, R9
	JZ quant_scan_zero

	MOVOU X0, X2
	MOVOU X1, X3
	PSHUFB X12, X2
	PSHUFB X13, X3
	POR X3, X2
	MOVOU X0, X4
	MOVOU X1, X5
	PSHUFB X14, X4
	PSHUFB X15, X5
	POR X5, X4
	MOVOU X2, (BX)
	MOVOU X4, 16(BX)
	PCMPEQW X10, X2
	PCMPEQW X10, X4
	PACKSSWB X4, X2
	PMOVMSKB X2, R9
	XORL $65535, R9
	MOVW R9, (SI)
	JMP quant_scan_next
quant_scan_zero:
	MOVOU X10, (BX)
	MOVOU X10, 16(BX)
	MOVW $0, (SI)
quant_scan_next:
	ADDQ $32, AX
	ADDQ $32, BX
	ADDQ $2, DI
	ADDQ $2, SI
	DECQ R8
	JNZ quant_scan_block
	RET

DATA quantScanShuffle<>+0(SB)/8, $0x8080090803020100
DATA quantScanShuffle<>+8(SB)/8, $0x0d0c070605040b0a
DATA quantScanShuffle<>+16(SB)/8, $0x0100808080808080
DATA quantScanShuffle<>+24(SB)/8, $0x8080808080808080
DATA quantScanShuffle<>+32(SB)/8, $0x8080808080808080
DATA quantScanShuffle<>+40(SB)/8, $0x8080808080800f0e
DATA quantScanShuffle<>+48(SB)/8, $0x05040b0a09080302
DATA quantScanShuffle<>+56(SB)/8, $0x0f0e0d0c07068080
DATA quantScanShuffle<>+64(SB)/8, $0x0b0a808009080302
DATA quantScanShuffle<>+72(SB)/8, $0x80800d0c07060504
DATA quantScanShuffle<>+80(SB)/8, $0x8080010080808080
DATA quantScanShuffle<>+88(SB)/8, $0x0302808080808080
DATA quantScanShuffle<>+96(SB)/8, $0x0f0e808080808080
DATA quantScanShuffle<>+104(SB)/8, $0x8080808080808080
DATA quantScanShuffle<>+112(SB)/8, $0x808005040b0a0908
DATA quantScanShuffle<>+120(SB)/8, $0x80800f0e0d0c0706
GLOBL quantScanShuffle<>(SB), RODATA|NOPTR, $128

// Both entry points process independent blocks in the two 128-bit lanes.
// Tables are snapshotted once, exactly as in the SSSE3 entries.
TEXT ·quantScanFour4x4AVX2(SB), NOSPLIT, $0-52
	MOVQ dct+0(FP), AX
	MOVQ levels+8(FP), BX
	MOVQ ff+16(FP), CX
	MOVQ mf+24(FP), DX
	MOVQ maxima+32(FP), DI
	MOVQ masks+40(FP), SI
	MOVL ac+48(FP), R9
	MOVL $2, R8
	JMP quantScanBlockPairsAVX2<>(SB)

TEXT ·quantScanLumaAVX2(SB), NOSPLIT, $0-48
	MOVQ dct+0(FP), AX
	MOVQ levels+8(FP), BX
	MOVQ ff+16(FP), CX
	MOVQ mf+24(FP), DX
	MOVQ maxima+32(FP), DI
	MOVQ masks+40(FP), SI
	XORL R9, R9
	MOVL $8, R8
	JMP quantScanBlockPairsAVX2<>(SB)

// Preserve saturating unsigned FF addition, unsigned high multiplication and
// signed maxima clamped at zero. The sign operand is nonzero even for dct=0:
// rounding may produce a nonzero magnitude that must retain a positive sign.
TEXT quantScanBlockPairsAVX2<>(SB), NOSPLIT|NOFRAME, $0-0
	SHLQ $6, R9
	LEAQ quantScanShuffle<>(SB), R10
	ADDQ R9, R10
	VBROADCASTI128 (CX), Y8
	VBROADCASTI128 (DX), Y9
	VPXOR Y10, Y10, Y10
	VPCMPEQW Y11, Y11, Y11
	VPSRLW $1, Y11, Y11
	VBROADCASTI128 (R10), Y12
	VBROADCASTI128 16(R10), Y13
	VBROADCASTI128 32(R10), Y14
	VBROADCASTI128 48(R10), Y15
	VBROADCASTI128 quantScanBroadcastMax<>(SB), Y7
quant_scan_pair:
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

	VPMAXSW Y10, Y2, Y4
	VPMAXSW Y3, Y4, Y4
	VPSRLDQ $8, Y4, Y5
	VPMAXSW Y5, Y4, Y4
	VPSRLDQ $4, Y4, Y5
	VPMAXSW Y5, Y4, Y4
	VPSRLDQ $2, Y4, Y5
	VPMAXSW Y5, Y4, Y4
	VPEXTRW $0, X4, R9
	VEXTRACTI128 $1, Y4, X5
	VPEXTRW $0, X5, R11
	SHLL $16, R11
	ORL R11, R9
	MOVL R9, (DI)
	TESTL R9, R9
	JZ quant_scan_pair_zero

	// A zero signed maximum clears its entire scanned block, including when
	// out-of-table unsigned magnitudes have their sign bit set. Valid tables
	// also take this path for pairs containing one empty block.
	VPSHUFB Y7, Y4, Y6
	VPCMPEQW Y10, Y6, Y6
	VPANDN Y0, Y6, Y0
	VPANDN Y1, Y6, Y1
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
	JMP quant_scan_pair_next
quant_scan_pair_zero:
	VMOVDQU Y10, (BX)
	VMOVDQU Y10, 32(BX)
	MOVL $0, (SI)
quant_scan_pair_next:
	ADDQ $64, AX
	ADDQ $64, BX
	ADDQ $4, DI
	ADDQ $4, SI
	DECL R8
	JNZ quant_scan_pair
	VZEROUPPER
	RET

DATA quantScanBroadcastMax<>+0(SB)/8, $0x0100010001000100
DATA quantScanBroadcastMax<>+8(SB)/8, $0x0100010001000100
GLOBL quantScanBroadcastMax<>(SB), RODATA|NOPTR, $16
