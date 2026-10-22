//go:build amd64

// Ported from Cisco OpenH264 codec/encoder/core/x86/quant.asm.
// Copyright (c) 2009-2013, Cisco Systems.
// BSD-2-Clause; see LICENSE-OpenH264.

#include "textflag.h"

// Quantization follows OpenH264's valid coefficient domain: ff and mf are
// non-negative int16 values. PADDUSW is exact because abs(dct)+ff <= 65535.

// func quant4x4SSE2(dct []int16, ff []int16, mf []int16)
TEXT ·quant4x4SSE2(SB), NOSPLIT, $0-72
	MOVQ dct_base+0(FP), AX
	MOVQ ff_base+24(FP), BX
	MOVQ mf_base+48(FP), CX
	MOVOU (BX), X2
	MOVOU (CX), X3
	MOVQ $2, DX

quant4x4_loop:
	MOVOU (AX), X0
	PXOR X1, X1
	PCMPGTW X0, X1
	PXOR X1, X0
	PSUBW X1, X0
	PADDUSW X2, X0
	PMULHUW X3, X0
	PXOR X1, X0
	PSUBW X1, X0
	MOVOU X0, (AX)
	ADDQ $16, AX
	DECQ DX
	JNZ quant4x4_loop
	RET

// func quant4x4DcSSE2(dct []int16, ff int64, mf int64)
TEXT ·quant4x4DcSSE2(SB), NOSPLIT, $0-40
	MOVQ ff+24(FP), AX
	MOVD AX, X2
	PSHUFLW $0, X2, X2
	PSHUFD $0, X2, X2
	MOVQ mf+32(FP), AX
	MOVD AX, X3
	PSHUFLW $0, X3, X3
	PSHUFD $0, X3, X3
	MOVQ dct_base+0(FP), AX
	MOVQ $2, DX

quant_dc_loop:
	MOVOU (AX), X0
	PXOR X1, X1
	PCMPGTW X0, X1
	PXOR X1, X0
	PSUBW X1, X0
	PADDUSW X2, X0
	PMULHUW X3, X0
	PXOR X1, X0
	PSUBW X1, X0
	MOVOU X0, (AX)
	ADDQ $16, AX
	DECQ DX
	JNZ quant_dc_loop
	RET

// func quantFour4x4SSE2(dct []int16, ff []int16, mf []int16)
TEXT ·quantFour4x4SSE2(SB), NOSPLIT, $0-72
	MOVQ dct_base+0(FP), AX
	MOVQ ff_base+24(FP), BX
	MOVQ mf_base+48(FP), CX
	MOVOU (BX), X2
	MOVOU (CX), X3
	MOVQ $8, DX

quant_four_loop:
	MOVOU (AX), X0
	PXOR X1, X1
	PCMPGTW X0, X1
	PXOR X1, X0
	PSUBW X1, X0
	PADDUSW X2, X0
	PMULHUW X3, X0
	PXOR X1, X0
	PSUBW X1, X0
	MOVOU X0, (AX)
	ADDQ $16, AX
	DECQ DX
	JNZ quant_four_loop
	RET

// func quantFour4x4MaxSSE2(dct []int16, ff []int16, mf []int16, max []int16)
TEXT ·quantFour4x4MaxSSE2(SB), NOSPLIT, $0-96
	MOVQ dct_base+0(FP), AX
	MOVQ ff_base+24(FP), BX
	MOVQ mf_base+48(FP), CX
	MOVQ max_base+72(FP), DI
	MOVOU (BX), X2
	MOVOU (CX), X3
	MOVQ $4, R8

quant_max_block:
	PXOR X4, X4

	MOVOU (AX), X0
	PXOR X1, X1
	PCMPGTW X0, X1
	PXOR X1, X0
	PSUBW X1, X0
	PADDUSW X2, X0
	PMULHUW X3, X0
	PMAXSW X0, X4
	PXOR X1, X0
	PSUBW X1, X0
	MOVOU X0, (AX)

	MOVOU 16(AX), X0
	PXOR X1, X1
	PCMPGTW X0, X1
	PXOR X1, X0
	PSUBW X1, X0
	PADDUSW X2, X0
	PMULHUW X3, X0
	PMAXSW X0, X4
	PXOR X1, X0
	PSUBW X1, X0
	MOVOU X0, 16(AX)

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

	ADDQ $32, AX
	ADDQ $2, DI
	DECQ R8
	JNZ quant_max_block
	RET

// Quantize two blocks per iteration, then place each block's two halves in
// the same 128-bit lane for paired maximum reduction. Unlike native's valid-
// table-only reduction, clamp signed magnitudes at zero to retain all existing
// SSE2 FF/MF bit patterns. The OR sign operand also preserves rounded dct=0.
// func quantFour4x4MaxAVX2(dct *[64]int16, ff, mf *[8]int16, maxima *[4]int16)
TEXT ·quantFour4x4MaxAVX2(SB), NOSPLIT, $0-32
	MOVQ dct+0(FP), AX
	MOVQ ff+8(FP), BX
	MOVQ mf+16(FP), CX
	MOVQ maxima+24(FP), DI
	VBROADCASTI128 (BX), Y8
	VBROADCASTI128 (CX), Y9
	VPXOR Y10, Y10, Y10
	VPCMPEQW Y11, Y11, Y11
	VPSRLW $1, Y11, Y11
	MOVL $2, R8

quant_max_pair:
	VMOVDQU (AX), Y0
	VMOVDQU 32(AX), Y1
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
	VMOVDQU Y0, (AX)
	VMOVDQU Y1, 32(AX)

	VPERM2I128 $0x20, Y3, Y2, Y4
	VPERM2I128 $0x31, Y3, Y2, Y5
	VPMAXSW Y10, Y4, Y4
	VPMAXSW Y5, Y4, Y4
	VPSRLDQ $8, Y4, Y5
	VPMAXSW Y5, Y4, Y4
	VPSRLDQ $4, Y4, Y5
	VPMAXSW Y5, Y4, Y4
	VPSRLDQ $2, Y4, Y5
	VPMAXSW Y5, Y4, Y4
	VPEXTRW $0, X4, R9
	VEXTRACTI128 $1, Y4, X5
	VPEXTRW $0, X5, R10
	SHLL $16, R10
	ORL R10, R9
	MOVL R9, (DI)

	ADDQ $64, AX
	ADDQ $4, DI
	DECL R8
	JNZ quant_max_pair
	VZEROUPPER
	RET
