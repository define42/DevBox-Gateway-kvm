//go:build amd64

// Horizontal six-tap filtering for the extra column required by fractional
// motion refinement. Each AVX2 lane handles a separate row. Unlike the padded
// native kernels, exact-width loads preserve the checked Go buffer contract.
#include "textflag.h"

// Filter eight samples in each 128-bit lane with [1,-5,20,20,-5,1].
// The existing shared masks address byte pairs within each source row.
#define REFINE_HOR8(src) \
	VPSHUFB Y8, src, Y1; \
	VPSHUFB Y9, src, Y2; \
	VPSHUFB Y10, src, Y3; \
	VPMADDUBSW Y11, Y1, Y1; \
	VPMADDUBSW Y12, Y2, Y2; \
	VPMADDUBSW Y13, Y3, Y3; \
	VPADDW Y1, Y2, src; \
	VPADDW Y3, src, src; \
	VPADDW Y15, src, src; \
	VPSRAW $5, src, src

// Filter the ninth sample in each lane. The last six source bytes occupy
// positions 8..13, and the coefficient mask ignores all remaining bytes.
#define REFINE_HOR_EXTRA(src) \
	VPSRLDQ $8, src, Y5; \
	VPMADDUBSW Y14, Y5, Y5; \
	VPHADDW Y5, Y5, Y5; \
	VPHADDW Y5, Y5, Y5; \
	VPADDW Y15, Y5, Y5; \
	VPSRAW $5, Y5, Y5; \
	VPACKUSWB Y5, Y5, Y5

// func mcHorRefineAVX2(dst *byte, dstStride int, src *byte,
//     srcStride, width, height int)
TEXT ·mcHorRefineAVX2(SB), NOSPLIT, $0-48
	MOVQ dst+0(FP), AX
	MOVQ dstStride+8(FP), CX
	MOVQ src+16(FP), DX
	MOVQ srcStride+24(FP), BX
	MOVQ width+32(FP), SI
	MOVQ height+40(FP), DI
	VBROADCASTI128 ·mcPair01Mask(SB), Y8
	VBROADCASTI128 ·mcPair23Mask(SB), Y9
	VBROADCASTI128 ·mcPair45Mask(SB), Y10
	VPBROADCASTD ·mcPair01Coeff(SB), Y11
	VPBROADCASTD ·mcPair23Coeff(SB), Y12
	VPBROADCASTD ·mcPair45Coeff(SB), Y13
	VPBROADCASTQ ·mcRefineExtraCoeff(SB), Y14
	VPCMPEQW Y15, Y15, Y15
	VPSRLW $15, Y15, Y15
	VPSLLW $4, Y15, Y15
	CMPQ SI, $5
	JE mchorrefine5
	CMPQ SI, $9
	JE mchorrefine9
	JMP mchorrefine17

mchorrefine5:
	// Duplicate the last source row into the upper lane for an odd height.
	// The upper output is then discarded, so neither span needs padding.
	MOVQ DX, R9
	CMPQ DI, $2
	JB mchorrefine5_load
	ADDQ BX, R9
mchorrefine5_load:
	VMOVQ -2(DX), X0
	VPINSRW $4, 6(DX), X0, X0
	VMOVQ -2(R9), X4
	VPINSRW $4, 6(R9), X4, X4
	VINSERTI128 $1, X4, Y0, Y0
	REFINE_HOR8(Y0)
	VPACKUSWB Y0, Y0, Y0
	// Overlapping four-byte stores write exactly the five output bytes.
	VMOVD X0, (AX)
	VPSRLDQ $1, X0, X1
	VMOVD X1, 1(AX)
	CMPQ DI, $2
	JB mchorrefine_done
	VEXTRACTI128 $1, Y0, X0
	VMOVD X0, (AX)(CX*1)
	VPSRLDQ $1, X0, X1
	VMOVD X1, 1(AX)(CX*1)
	LEAQ (DX)(BX*2), DX
	LEAQ (AX)(CX*2), AX
	SUBQ $2, DI
	JNZ mchorrefine5
	JMP mchorrefine_done

mchorrefine9:
	MOVQ DX, R9
	CMPQ DI, $2
	JB mchorrefine9_load
	ADDQ BX, R9
mchorrefine9_load:
	// Two overlapping eight-byte loads cover fourteen input bytes exactly.
	VMOVQ -2(DX), X0
	VMOVQ 4(DX), X4
	VPSLLDQ $6, X4, X4
	VPOR X4, X0, X0
	VMOVQ -2(R9), X1
	VMOVQ 4(R9), X4
	VPSLLDQ $6, X4, X4
	VPOR X4, X1, X1
	VINSERTI128 $1, X1, Y0, Y0
	REFINE_HOR_EXTRA(Y0)
	REFINE_HOR8(Y0)
	VPACKUSWB Y0, Y0, Y0
	VMOVQ X0, (AX)
	VMOVD X5, R8
	MOVB R8, 8(AX)
	CMPQ DI, $2
	JB mchorrefine_done
	VEXTRACTI128 $1, Y0, X0
	VEXTRACTI128 $1, Y5, X5
	VMOVQ X0, (AX)(CX*1)
	VMOVD X5, R8
	MOVB R8, 8(AX)(CX*1)
	LEAQ (DX)(BX*2), DX
	LEAQ (AX)(CX*2), AX
	SUBQ $2, DI
	JNZ mchorrefine9
	JMP mchorrefine_done

mchorrefine17:
	MOVQ DX, R9
	CMPQ DI, $2
	JB mchorrefine17_load
	ADDQ BX, R9
mchorrefine17_load:
	VMOVDQU -2(DX), X0
	VMOVDQU -2(R9), X1
	VINSERTI128 $1, X1, Y0, Y0
	// The shifted second load starts at output eight and ends at its halo.
	VMOVDQU 4(DX), X4
	VPSRLDQ $2, X4, X4
	VMOVDQU 4(R9), X1
	VPSRLDQ $2, X1, X1
	VINSERTI128 $1, X1, Y4, Y4
	REFINE_HOR_EXTRA(Y4)
	REFINE_HOR8(Y0)
	REFINE_HOR8(Y4)
	VPACKUSWB Y4, Y0, Y0
	VMOVDQU X0, (AX)
	VMOVD X5, R8
	MOVB R8, 16(AX)
	CMPQ DI, $2
	JB mchorrefine_done
	VEXTRACTI128 $1, Y0, X0
	VEXTRACTI128 $1, Y5, X5
	VMOVDQU X0, (AX)(CX*1)
	VMOVD X5, R8
	MOVB R8, 16(AX)(CX*1)
	LEAQ (DX)(BX*2), DX
	LEAQ (AX)(CX*2), AX
	SUBQ $2, DI
	JNZ mchorrefine17

mchorrefine_done:
	VZEROUPPER
	RET

// Only the first six bytes in each group contribute to the extra output.
DATA ·mcRefineExtraCoeff+0(SB)/8, $0x000001fb1414fb01
GLOBL ·mcRefineExtraCoeff(SB), RODATA|NOPTR, $8
