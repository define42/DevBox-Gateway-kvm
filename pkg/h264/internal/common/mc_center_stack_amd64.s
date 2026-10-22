//go:build amd64

// The first pass uses OpenH264's adjacent-row reuse technique, as in
// mc_refine_vertical_amd64.s. Copyright (c) 2009-2013, Cisco Systems.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.
#include "textflag.h"

// Both passes are in this no-call leaf. Compact rows contain 22 int16 taps:
// at most 17*22*2 = 748 bytes, inside the 752-byte local frame. No pointer is
// stored in that scratch, and no uninitialized padding is read. NOSPLIT is
// below the amd64 stack budget; do not enlarge this frame or add calls.
#define MC_VER_LOAD9(X, Y) VMOVQ (DX), X; VMOVD 5(DX), X7; VPSRLDQ $3, X7, X7; VINSERTI128 $1, X7, Y, Y

#define MC_VER_LOAD13(X, Y) VMOVQ (DX), X; VMOVD 8(DX), X7; VPINSRB $4, 12(DX), X7, X7; VINSERTI128 $1, X7, Y, Y

#define MC_VER_RAW_STORE9 VMOVDQU X8, (AX); VEXTRACTI128 $1, Y8, X9; VPEXTRW $0, X9, R11; MOVW R11, 16(AX)

#define MC_VER_RAW_STORE13 VMOVDQU X8, (AX); VEXTRACTI128 $1, Y8, X9; VMOVQ X9, 16(AX); VPEXTRW $4, X9, R11; MOVW R11, 24(AX)

#define MC_VER_LOAD8(X, Y) VMOVQ (DX), X

#define MC_VER_LOAD10(X, Y) VMOVQ (DX), X; VMOVD 6(DX), X7; VPSRLDQ $2, X7, X7; VINSERTI128 $1, X7, Y, Y

#define MC_VER_LOAD14(X, Y) VMOVQ (DX), X; VMOVD 8(DX), X7; VPINSRW $2, 12(DX), X7, X7; VINSERTI128 $1, X7, Y, Y

#define MC_VER_LOAD16(X, Y) VMOVDQU (DX), X; VPSRLDQ $8, X, X7; VINSERTI128 $1, X7, Y, Y

#define MC_VER_SUM(A, B, C) \
	VPMADDUBSW Y12, A, Y8; \
	VPMADDUBSW Y13, B, Y9; \
	VPMADDUBSW Y14, C, Y10; \
	VPADDW Y9, Y8, Y8; \
	VPADDW Y10, Y8, Y8

#define MC_VER_RAW_STORE8 VMOVDQU X8, (AX)

#define MC_VER_RAW_STORE10 VMOVDQU X8, (AX); VEXTRACTI128 $1, Y8, X9; VMOVD X9, 16(AX)

#define MC_VER_RAW_STORE14 VMOVDQU X8, (AX); VEXTRACTI128 $1, Y8, X9; VMOVQ X9, 16(AX); VPSRLDQ $8, X9, X9; VMOVD X9, 24(AX)

#define MC_VER_RAW_STORE16 VMOVDQU Y8, (AX)

#define MC_VER_INIT(LOAD) \
	LOAD(X5, Y5); ADDQ BX, DX; \
	LOAD(X6, Y6); VPUNPCKLBW Y6, Y5, Y0; ADDQ BX, DX; \
	LOAD(X5, Y5); VPUNPCKLBW Y5, Y6, Y1; ADDQ BX, DX; \
	LOAD(X6, Y6); VPUNPCKLBW Y6, Y5, Y2; ADDQ BX, DX; \
	LOAD(X5, Y5); VPUNPCKLBW Y5, Y6, Y3; ADDQ BX, DX; \
	LOAD(X6, Y6); VPUNPCKLBW Y6, Y5, Y4; ADDQ BX, DX

#define MC_VER_STEP(A, B, C, LOAD, X, Y, PREV, NEXT, STORE, DONE) \
	MC_VER_SUM(A, B, C); STORE; DECQ R10; JE DONE; ADDQ CX, AX; \
	LOAD(X, Y); VPUNPCKLBW Y, PREV, NEXT; ADDQ BX, DX

#define CENTER_PAIR_SUM(A, B, T0, T1, M01, M23, M45, C01, C23, C45, ROUND) \
    VPSHUFB M01, A, T0; \
    VPSHUFB M23, A, T1; \
    VPSHUFB M45, B, B; \
    VPMADDWD C01, T0, T0; \
    VPMADDWD C23, T1, T1; \
    VPMADDWD C45, B, B; \
    VPADDD T1, T0, A; \
    VPADDD B, A, A; \
    VPADDD ROUND, A, A; \
    VPSRAD $10, A, A

// Two overlapping 16-byte reads cover exactly nine input words for four
// outputs. The second load starts one word after the first.
#define CENTER_PAIR4(A, B, OUT) \
    VMOVDQU A(DX), X0; \
    VMOVDQU B(DX), X1; \
    CENTER_PAIR_SUM(X0, X1, X2, X3, X8, X9, X10, X11, X12, X13, X7); \
    VPACKSSDW X0, X0, X0; \
    VPACKUSWB X0, X0, X0; \
    VMOVD X0, OUT(AX)

// Each 128-bit lane produces four adjacent outputs. The upper lane starts
// four words later, so the four reads cover exactly thirteen input words.
#define CENTER_PAIR8(A, B, C, D, OUT) \
    VMOVDQU A(DX), X0; \
    VINSERTI128 $1, B(DX), Y0, Y0; \
    VMOVDQU C(DX), X1; \
    VINSERTI128 $1, D(DX), Y1, Y1; \
    CENTER_PAIR_SUM(Y0, Y1, Y2, Y3, Y8, Y9, Y10, Y11, Y12, Y13, Y7); \
    VEXTRACTI128 $1, Y0, X1; \
    VPACKSSDW X1, X0, X0; \
    VPACKUSWB X0, X0, X0; \
    VMOVQ X0, OUT(AX)


TEXT ·mcCenterRefineAVX2(SB), NOSPLIT, $752-48
    LEAQ taps-752(SP), AX
    MOVQ $44, CX
    MOVQ src+16(FP), DX
    MOVQ srcStride+24(FP), BX
    MOVQ width+32(FP), SI
    ADDQ $5, SI
    MOVQ height+40(FP), DI
    MOVQ AX, R12
    MOVQ DX, R13
    MOVQ DI, R10
    SUBQ BX, DX
    SUBQ BX, DX
    VPBROADCASTD ·mcPair01Coeff(SB), Y12
    VPBROADCASTD ·mcPair23Coeff(SB), Y13
    VPBROADCASTD ·mcPair45Coeff(SB), Y14
    CMPQ SI, $9
    JE centerstack_raw9
    CMPQ SI, $10
    JE centerstack_raw10
    CMPQ SI, $13
    JE centerstack_raw13
    CMPQ SI, $14
    JE centerstack_raw14
    JMP centerstack_raw16

centerstack_raw9:
    MC_VER_INIT(MC_VER_LOAD9)
centerstack_raw9_loop:
    MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD9, X5, Y5, Y6, Y0, MC_VER_RAW_STORE9, centerstack_horizontal)
    MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD9, X6, Y6, Y5, Y1, MC_VER_RAW_STORE9, centerstack_horizontal)
    MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD9, X5, Y5, Y6, Y2, MC_VER_RAW_STORE9, centerstack_horizontal)
    MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD9, X6, Y6, Y5, Y3, MC_VER_RAW_STORE9, centerstack_horizontal)
    MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD9, X5, Y5, Y6, Y4, MC_VER_RAW_STORE9, centerstack_horizontal)
    VMOVDQA Y5, Y6
    JMP centerstack_raw9_loop

centerstack_raw13:
    MC_VER_INIT(MC_VER_LOAD13)
centerstack_raw13_loop:
    MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD13, X5, Y5, Y6, Y0, MC_VER_RAW_STORE13, centerstack_horizontal)
    MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD13, X6, Y6, Y5, Y1, MC_VER_RAW_STORE13, centerstack_horizontal)
    MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD13, X5, Y5, Y6, Y2, MC_VER_RAW_STORE13, centerstack_horizontal)
    MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD13, X6, Y6, Y5, Y3, MC_VER_RAW_STORE13, centerstack_horizontal)
    MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD13, X5, Y5, Y6, Y4, MC_VER_RAW_STORE13, centerstack_horizontal)
    VMOVDQA Y5, Y6
    JMP centerstack_raw13_loop

centerstack_raw10:
    MC_VER_INIT(MC_VER_LOAD10)
centerstack_raw10_loop:
    MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD10, X5, Y5, Y6, Y0, MC_VER_RAW_STORE10, centerstack_horizontal)
    MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD10, X6, Y6, Y5, Y1, MC_VER_RAW_STORE10, centerstack_horizontal)
    MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD10, X5, Y5, Y6, Y2, MC_VER_RAW_STORE10, centerstack_horizontal)
    MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD10, X6, Y6, Y5, Y3, MC_VER_RAW_STORE10, centerstack_horizontal)
    MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD10, X5, Y5, Y6, Y4, MC_VER_RAW_STORE10, centerstack_horizontal)
    VMOVDQA Y5, Y6
    JMP centerstack_raw10_loop

centerstack_raw14:
    MC_VER_INIT(MC_VER_LOAD14)
centerstack_raw14_loop:
    MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD14, X5, Y5, Y6, Y0, MC_VER_RAW_STORE14, centerstack_horizontal)
    MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD14, X6, Y6, Y5, Y1, MC_VER_RAW_STORE14, centerstack_horizontal)
    MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD14, X5, Y5, Y6, Y2, MC_VER_RAW_STORE14, centerstack_horizontal)
    MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD14, X6, Y6, Y5, Y3, MC_VER_RAW_STORE14, centerstack_horizontal)
    MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD14, X5, Y5, Y6, Y4, MC_VER_RAW_STORE14, centerstack_horizontal)
    VMOVDQA Y5, Y6
    JMP centerstack_raw14_loop

centerstack_raw16:
    MC_VER_INIT(MC_VER_LOAD16)
centerstack_raw16_loop:
    MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD16, X5, Y5, Y6, Y0, MC_VER_RAW_STORE16, centerstack_raw_wide_done)
    MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD16, X6, Y6, Y5, Y1, MC_VER_RAW_STORE16, centerstack_raw_wide_done)
    MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD16, X5, Y5, Y6, Y2, MC_VER_RAW_STORE16, centerstack_raw_wide_done)
    MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD16, X6, Y6, Y5, Y3, MC_VER_RAW_STORE16, centerstack_raw_wide_done)
    MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD16, X5, Y5, Y6, Y4, MC_VER_RAW_STORE16, centerstack_raw_wide_done)
    VMOVDQA Y5, Y6
    JMP centerstack_raw16_loop

centerstack_raw8:
    MC_VER_INIT(MC_VER_LOAD8)
centerstack_raw8_loop:
    MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD8, X5, Y5, Y6, Y0, MC_VER_RAW_STORE8, centerstack_horizontal)
    MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD8, X6, Y6, Y5, Y1, MC_VER_RAW_STORE8, centerstack_horizontal)
    MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD8, X5, Y5, Y6, Y2, MC_VER_RAW_STORE8, centerstack_horizontal)
    MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD8, X6, Y6, Y5, Y3, MC_VER_RAW_STORE8, centerstack_horizontal)
    MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD8, X5, Y5, Y6, Y4, MC_VER_RAW_STORE8, centerstack_horizontal)
    VMOVDQA Y5, Y6
    JMP centerstack_raw8_loop

centerstack_raw_wide_done:
    // Widths 16/17 need 21/22 taps. The final eight start at tap13/14,
    // exactly as the previous standalone first pass did.
    SUBQ $8, SI
    LEAQ (R12)(SI*2), AX
    LEAQ (R13)(SI*1), DX
    SUBQ BX, DX
    SUBQ BX, DX
    MOVQ DI, R10
    JMP centerstack_raw8

centerstack_horizontal:
    MOVQ dst+0(FP), AX
    MOVQ dstStride+8(FP), CX
    LEAQ taps-752(SP), DX
    MOVQ $44, BX
    MOVQ width+32(FP), SI
    MOVQ height+40(FP), DI
    VPCMPEQD Y7, Y7, Y7
    VPSRLD $31, Y7, Y7
    VPSLLD $9, Y7, Y7
    VBROADCASTI128 ·mcCenterPair01Mask(SB), Y8
    VBROADCASTI128 ·mcCenterPair23Mask(SB), Y9
    VBROADCASTI128 ·mcCenterPair45Mask(SB), Y10
    VPBROADCASTD ·mcCenterPair01Coeff(SB), Y11
    VPBROADCASTD ·mcCenterPair23Coeff(SB), Y12
    VPBROADCASTD ·mcCenterPair45Coeff(SB), Y13
    CMPQ SI, $4
    JE centerstack_output4
    CMPQ SI, $5
    JE centerstack_output5
    CMPQ SI, $8
    JE centerstack_output8
    CMPQ SI, $9
    JE centerstack_output9
    CMPQ SI, $16
    JE centerstack_output16
centerstack_output17:
    CENTER_PAIR8(0, 8, 2, 10, 0)
    CENTER_PAIR8(16, 24, 18, 26, 8)
    CENTER_PAIR4(26, 28, 13)
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ DI
    JNZ centerstack_output17
    VZEROUPPER
    RET

centerstack_output9:
    CENTER_PAIR8(0, 8, 2, 10, 0)
    CENTER_PAIR4(10, 12, 5)
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ DI
    JNZ centerstack_output9
    VZEROUPPER
    RET

centerstack_output5:
    CENTER_PAIR4(0, 2, 0)
    CENTER_PAIR4(2, 4, 1)
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ DI
    JNZ centerstack_output5
    VZEROUPPER
    RET


centerstack_output4:
    CENTER_PAIR4(0, 2, 0)
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ DI
    JNZ centerstack_output4
    VZEROUPPER
    RET

centerstack_output8:
    CENTER_PAIR8(0, 8, 2, 10, 0)
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ DI
    JNZ centerstack_output8
    VZEROUPPER
    RET

centerstack_output16:
    CENTER_PAIR8(0, 8, 2, 10, 0)
    CENTER_PAIR8(16, 24, 18, 26, 8)
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ DI
    JNZ centerstack_output16
    VZEROUPPER
    RET
