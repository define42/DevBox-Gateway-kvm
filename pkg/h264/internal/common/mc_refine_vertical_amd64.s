//go:build amd64

// Vertical six-tap filtering follows the adjacent-row reuse technique in
// OpenH264's codec/common/x86/mc_luma.asm.
// Copyright (c) 2009-2013, Cisco Systems. BSD-2-Clause; see
// LICENSE-OpenH264 in the repository root.
//
// Five adjacent-row pairs stay in Y0..Y4. Each output multiplies three pairs
// by [1,-5], [20,20] and [-5,1]. Advancing a row replaces only the oldest pair,
// so each source row is loaded once per vector-width pass. Pair sums are in
// [-1275,10200] and the complete sum in [-2550,10710], fitting signed words.
//
// Lower/upper 128-bit lanes represent columns 0..7/8..15. Narrow row loads
// never cross the requested width. The ring is unrolled five times to rotate
// pair registers without copying all five vectors on every row.
#include "textflag.h"

#define MC_VER_LOAD4(X, Y) VMOVD (DX), X
#define MC_VER_LOAD5(X, Y) VMOVD (DX), X; VPINSRB $4, 4(DX), X, X
#define MC_VER_LOAD8(X, Y) VMOVQ (DX), X
#define MC_VER_LOAD9(X, Y) VMOVQ (DX), X; VMOVD 5(DX), X7; VPSRLDQ $3, X7, X7; VINSERTI128 $1, X7, Y, Y
#define MC_VER_LOAD10(X, Y) VMOVQ (DX), X; VMOVD 6(DX), X7; VPSRLDQ $2, X7, X7; VINSERTI128 $1, X7, Y, Y
#define MC_VER_LOAD13(X, Y) VMOVQ (DX), X; VMOVD 8(DX), X7; VPINSRB $4, 12(DX), X7, X7; VINSERTI128 $1, X7, Y, Y
#define MC_VER_LOAD14(X, Y) VMOVQ (DX), X; VMOVD 8(DX), X7; VPINSRW $2, 12(DX), X7, X7; VINSERTI128 $1, X7, Y, Y
#define MC_VER_LOAD16(X, Y) VMOVDQU (DX), X; VPSRLDQ $8, X, X7; VINSERTI128 $1, X7, Y, Y

#define MC_VER_SUM(A, B, C) \
	VPMADDUBSW Y12, A, Y8; \
	VPMADDUBSW Y13, B, Y9; \
	VPMADDUBSW Y14, C, Y10; \
	VPADDW Y9, Y8, Y8; \
	VPADDW Y10, Y8, Y8

#define MC_VER_CLIP \
	VPADDW Y15, Y8, Y8; \
	VPSRAW $5, Y8, Y8; \
	VEXTRACTI128 $1, Y8, X9; \
	VPACKUSWB X9, X8, X8

#define MC_VER_STORE4 MC_VER_CLIP; VMOVD X8, (AX)
#define MC_VER_STORE5 MC_VER_CLIP; VMOVD X8, (AX); VPEXTRB $4, X8, R11; MOVB R11, 4(AX)
#define MC_VER_STORE8 MC_VER_CLIP; VMOVQ X8, (AX)
#define MC_VER_STORE9 MC_VER_CLIP; VMOVQ X8, (AX); VPEXTRB $8, X8, R11; MOVB R11, 8(AX)
#define MC_VER_STORE10 MC_VER_CLIP; VMOVQ X8, (AX); VPEXTRW $4, X8, R11; MOVW R11, 8(AX)
#define MC_VER_STORE13 MC_VER_CLIP; VMOVQ X8, (AX); VPSRLDQ $8, X8, X9; VMOVD X9, 8(AX); VPEXTRB $12, X8, R11; MOVB R11, 12(AX)
#define MC_VER_STORE14 MC_VER_CLIP; VMOVQ X8, (AX); VPSRLDQ $8, X8, X9; VMOVD X9, 8(AX); VPEXTRW $6, X8, R11; MOVW R11, 12(AX)
#define MC_VER_STORE16 MC_VER_CLIP; VMOVDQU X8, (AX)

#define MC_VER_RAW_STORE4 VMOVQ X8, (AX)
#define MC_VER_RAW_STORE5 VMOVQ X8, (AX); VPEXTRW $4, X8, R11; MOVW R11, 8(AX)
#define MC_VER_RAW_STORE8 VMOVDQU X8, (AX)
#define MC_VER_RAW_STORE9 VMOVDQU X8, (AX); VEXTRACTI128 $1, Y8, X9; VPEXTRW $0, X9, R11; MOVW R11, 16(AX)
#define MC_VER_RAW_STORE10 VMOVDQU X8, (AX); VEXTRACTI128 $1, Y8, X9; VMOVD X9, 16(AX)
#define MC_VER_RAW_STORE13 VMOVDQU X8, (AX); VEXTRACTI128 $1, Y8, X9; VMOVQ X9, 16(AX); VPEXTRW $4, X9, R11; MOVW R11, 24(AX)
#define MC_VER_RAW_STORE14 VMOVDQU X8, (AX); VEXTRACTI128 $1, Y8, X9; VMOVQ X9, 16(AX); VPSRLDQ $8, X9, X9; VMOVD X9, 24(AX)
#define MC_VER_RAW_STORE16 VMOVDQU Y8, (AX)

// Load six rows once, building the five adjacent-row pairs. DX then points
// at the next unread source row and Y6 holds the newest complete source row.
#define MC_VER_INIT(LOAD) \
	LOAD(X5, Y5); ADDQ BX, DX; \
	LOAD(X6, Y6); VPUNPCKLBW Y6, Y5, Y0; ADDQ BX, DX; \
	LOAD(X5, Y5); VPUNPCKLBW Y5, Y6, Y1; ADDQ BX, DX; \
	LOAD(X6, Y6); VPUNPCKLBW Y6, Y5, Y2; ADDQ BX, DX; \
	LOAD(X5, Y5); VPUNPCKLBW Y5, Y6, Y3; ADDQ BX, DX; \
	LOAD(X6, Y6); VPUNPCKLBW Y6, Y5, Y4; ADDQ BX, DX

// Check for completion before loading another row: arbitrary positive heights
// consume exactly height+5 input rows, including the one-row height case.
#define MC_VER_STEP(A, B, C, LOAD, X, Y, PREV, NEXT, STORE, DONE) \
	MC_VER_SUM(A, B, C); STORE; DECQ R10; JE DONE; ADDQ CX, AX; \
	LOAD(X, Y); VPUNPCKLBW Y, PREV, NEXT; ADDQ BX, DX

// func mcVerReuseAVX2(dst *byte, dstStride int, src *byte,
//     srcStride, width, height int)
TEXT ·mcVerReuseAVX2(SB), NOSPLIT, $0-48
	MOVQ dst+0(FP), AX
	MOVQ dstStride+8(FP), CX
	MOVQ src+16(FP), DX
	MOVQ srcStride+24(FP), BX
	MOVQ width+32(FP), SI
	MOVQ height+40(FP), DI
	MOVQ AX, R12
	MOVQ DX, R13
	MOVQ DI, R10
	SUBQ BX, DX
	SUBQ BX, DX
	VPBROADCASTD ·mcPair01Coeff(SB), Y12
	VPBROADCASTD ·mcPair23Coeff(SB), Y13
	VPBROADCASTD ·mcPair45Coeff(SB), Y14
	VPCMPEQW Y15, Y15, Y15
	VPSRLW $15, Y15, Y15
	VPSLLW $4, Y15, Y15
	CMPQ SI, $16
	JE mcverreuse_w16
	CMPQ SI, $8
	JE mcverreuse_w8
	CMPQ SI, $9
	JE mcverreuse_w9
	CMPQ SI, $4
	JE mcverreuse_w4
	CMPQ SI, $5
	JE mcverreuse_w5
	CMPQ SI, $10
	JE mcverreuse_w10
	CMPQ SI, $13
	JE mcverreuse_w13
	CMPQ SI, $14
	JE mcverreuse_w14
	// Wider supported blocks use 16 columns, followed by the final eight.
	JMP mcverreuse_w16

mcverreuse_w4:
	MC_VER_INIT(MC_VER_LOAD4)
mcverreuse_w4_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD4, X5, Y5, Y6, Y0, MC_VER_STORE4, mcverreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD4, X6, Y6, Y5, Y1, MC_VER_STORE4, mcverreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD4, X5, Y5, Y6, Y2, MC_VER_STORE4, mcverreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD4, X6, Y6, Y5, Y3, MC_VER_STORE4, mcverreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD4, X5, Y5, Y6, Y4, MC_VER_STORE4, mcverreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverreuse_w4_loop

mcverreuse_w5:
	MC_VER_INIT(MC_VER_LOAD5)
mcverreuse_w5_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD5, X5, Y5, Y6, Y0, MC_VER_STORE5, mcverreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD5, X6, Y6, Y5, Y1, MC_VER_STORE5, mcverreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD5, X5, Y5, Y6, Y2, MC_VER_STORE5, mcverreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD5, X6, Y6, Y5, Y3, MC_VER_STORE5, mcverreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD5, X5, Y5, Y6, Y4, MC_VER_STORE5, mcverreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverreuse_w5_loop

mcverreuse_w8:
	MC_VER_INIT(MC_VER_LOAD8)
mcverreuse_w8_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD8, X5, Y5, Y6, Y0, MC_VER_STORE8, mcverreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD8, X6, Y6, Y5, Y1, MC_VER_STORE8, mcverreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD8, X5, Y5, Y6, Y2, MC_VER_STORE8, mcverreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD8, X6, Y6, Y5, Y3, MC_VER_STORE8, mcverreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD8, X5, Y5, Y6, Y4, MC_VER_STORE8, mcverreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverreuse_w8_loop

mcverreuse_w9:
	MC_VER_INIT(MC_VER_LOAD9)
mcverreuse_w9_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD9, X5, Y5, Y6, Y0, MC_VER_STORE9, mcverreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD9, X6, Y6, Y5, Y1, MC_VER_STORE9, mcverreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD9, X5, Y5, Y6, Y2, MC_VER_STORE9, mcverreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD9, X6, Y6, Y5, Y3, MC_VER_STORE9, mcverreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD9, X5, Y5, Y6, Y4, MC_VER_STORE9, mcverreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverreuse_w9_loop

mcverreuse_w10:
	MC_VER_INIT(MC_VER_LOAD10)
mcverreuse_w10_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD10, X5, Y5, Y6, Y0, MC_VER_STORE10, mcverreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD10, X6, Y6, Y5, Y1, MC_VER_STORE10, mcverreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD10, X5, Y5, Y6, Y2, MC_VER_STORE10, mcverreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD10, X6, Y6, Y5, Y3, MC_VER_STORE10, mcverreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD10, X5, Y5, Y6, Y4, MC_VER_STORE10, mcverreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverreuse_w10_loop

mcverreuse_w13:
	MC_VER_INIT(MC_VER_LOAD13)
mcverreuse_w13_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD13, X5, Y5, Y6, Y0, MC_VER_STORE13, mcverreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD13, X6, Y6, Y5, Y1, MC_VER_STORE13, mcverreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD13, X5, Y5, Y6, Y2, MC_VER_STORE13, mcverreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD13, X6, Y6, Y5, Y3, MC_VER_STORE13, mcverreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD13, X5, Y5, Y6, Y4, MC_VER_STORE13, mcverreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverreuse_w13_loop

mcverreuse_w14:
	MC_VER_INIT(MC_VER_LOAD14)
mcverreuse_w14_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD14, X5, Y5, Y6, Y0, MC_VER_STORE14, mcverreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD14, X6, Y6, Y5, Y1, MC_VER_STORE14, mcverreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD14, X5, Y5, Y6, Y2, MC_VER_STORE14, mcverreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD14, X6, Y6, Y5, Y3, MC_VER_STORE14, mcverreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD14, X5, Y5, Y6, Y4, MC_VER_STORE14, mcverreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverreuse_w14_loop

mcverreuse_w16:
	MC_VER_INIT(MC_VER_LOAD16)
mcverreuse_w16_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD16, X5, Y5, Y6, Y0, MC_VER_STORE16, mcverreuse_wide_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD16, X6, Y6, Y5, Y1, MC_VER_STORE16, mcverreuse_wide_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD16, X5, Y5, Y6, Y2, MC_VER_STORE16, mcverreuse_wide_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD16, X6, Y6, Y5, Y3, MC_VER_STORE16, mcverreuse_wide_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD16, X5, Y5, Y6, Y4, MC_VER_STORE16, mcverreuse_wide_done)
	VMOVDQA Y5, Y6
	JMP mcverreuse_w16_loop

mcverreuse_wide_done:
	CMPQ SI, $16
	JE mcverreuse_done
	SUBQ $8, SI
	LEAQ (R12)(SI*1), AX
	LEAQ (R13)(SI*1), DX
	SUBQ BX, DX
	SUBQ BX, DX
	MOVQ DI, R10
	JMP mcverreuse_w8

mcverreuse_done:
	VZEROUPPER
	RET

// func mcVerRawReuseAVX2(dst *int16, dstStride int, src *byte,
//     srcStride, width, height int)
TEXT ·mcVerRawReuseAVX2(SB), NOSPLIT, $0-48
	MOVQ dst+0(FP), AX
	MOVQ dstStride+8(FP), CX
	MOVQ src+16(FP), DX
	MOVQ srcStride+24(FP), BX
	MOVQ width+32(FP), SI
	MOVQ height+40(FP), DI
	SHLQ $1, CX
	MOVQ AX, R12
	MOVQ DX, R13
	MOVQ DI, R10
	SUBQ BX, DX
	SUBQ BX, DX
	VPBROADCASTD ·mcPair01Coeff(SB), Y12
	VPBROADCASTD ·mcPair23Coeff(SB), Y13
	VPBROADCASTD ·mcPair45Coeff(SB), Y14
	CMPQ SI, $16
	JE mcverrawreuse_w16
	CMPQ SI, $8
	JE mcverrawreuse_w8
	CMPQ SI, $9
	JE mcverrawreuse_w9
	CMPQ SI, $4
	JE mcverrawreuse_w4
	CMPQ SI, $5
	JE mcverrawreuse_w5
	CMPQ SI, $10
	JE mcverrawreuse_w10
	CMPQ SI, $13
	JE mcverrawreuse_w13
	CMPQ SI, $14
	JE mcverrawreuse_w14
	// Wider supported blocks use 16 columns, followed by the final eight.
	JMP mcverrawreuse_w16

mcverrawreuse_w4:
	MC_VER_INIT(MC_VER_LOAD4)
mcverrawreuse_w4_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD4, X5, Y5, Y6, Y0, MC_VER_RAW_STORE4, mcverrawreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD4, X6, Y6, Y5, Y1, MC_VER_RAW_STORE4, mcverrawreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD4, X5, Y5, Y6, Y2, MC_VER_RAW_STORE4, mcverrawreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD4, X6, Y6, Y5, Y3, MC_VER_RAW_STORE4, mcverrawreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD4, X5, Y5, Y6, Y4, MC_VER_RAW_STORE4, mcverrawreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverrawreuse_w4_loop

mcverrawreuse_w5:
	MC_VER_INIT(MC_VER_LOAD5)
mcverrawreuse_w5_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD5, X5, Y5, Y6, Y0, MC_VER_RAW_STORE5, mcverrawreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD5, X6, Y6, Y5, Y1, MC_VER_RAW_STORE5, mcverrawreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD5, X5, Y5, Y6, Y2, MC_VER_RAW_STORE5, mcverrawreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD5, X6, Y6, Y5, Y3, MC_VER_RAW_STORE5, mcverrawreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD5, X5, Y5, Y6, Y4, MC_VER_RAW_STORE5, mcverrawreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverrawreuse_w5_loop

mcverrawreuse_w8:
	MC_VER_INIT(MC_VER_LOAD8)
mcverrawreuse_w8_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD8, X5, Y5, Y6, Y0, MC_VER_RAW_STORE8, mcverrawreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD8, X6, Y6, Y5, Y1, MC_VER_RAW_STORE8, mcverrawreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD8, X5, Y5, Y6, Y2, MC_VER_RAW_STORE8, mcverrawreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD8, X6, Y6, Y5, Y3, MC_VER_RAW_STORE8, mcverrawreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD8, X5, Y5, Y6, Y4, MC_VER_RAW_STORE8, mcverrawreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverrawreuse_w8_loop

mcverrawreuse_w9:
	MC_VER_INIT(MC_VER_LOAD9)
mcverrawreuse_w9_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD9, X5, Y5, Y6, Y0, MC_VER_RAW_STORE9, mcverrawreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD9, X6, Y6, Y5, Y1, MC_VER_RAW_STORE9, mcverrawreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD9, X5, Y5, Y6, Y2, MC_VER_RAW_STORE9, mcverrawreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD9, X6, Y6, Y5, Y3, MC_VER_RAW_STORE9, mcverrawreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD9, X5, Y5, Y6, Y4, MC_VER_RAW_STORE9, mcverrawreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverrawreuse_w9_loop

mcverrawreuse_w10:
	MC_VER_INIT(MC_VER_LOAD10)
mcverrawreuse_w10_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD10, X5, Y5, Y6, Y0, MC_VER_RAW_STORE10, mcverrawreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD10, X6, Y6, Y5, Y1, MC_VER_RAW_STORE10, mcverrawreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD10, X5, Y5, Y6, Y2, MC_VER_RAW_STORE10, mcverrawreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD10, X6, Y6, Y5, Y3, MC_VER_RAW_STORE10, mcverrawreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD10, X5, Y5, Y6, Y4, MC_VER_RAW_STORE10, mcverrawreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverrawreuse_w10_loop

mcverrawreuse_w13:
	MC_VER_INIT(MC_VER_LOAD13)
mcverrawreuse_w13_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD13, X5, Y5, Y6, Y0, MC_VER_RAW_STORE13, mcverrawreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD13, X6, Y6, Y5, Y1, MC_VER_RAW_STORE13, mcverrawreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD13, X5, Y5, Y6, Y2, MC_VER_RAW_STORE13, mcverrawreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD13, X6, Y6, Y5, Y3, MC_VER_RAW_STORE13, mcverrawreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD13, X5, Y5, Y6, Y4, MC_VER_RAW_STORE13, mcverrawreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverrawreuse_w13_loop

mcverrawreuse_w14:
	MC_VER_INIT(MC_VER_LOAD14)
mcverrawreuse_w14_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD14, X5, Y5, Y6, Y0, MC_VER_RAW_STORE14, mcverrawreuse_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD14, X6, Y6, Y5, Y1, MC_VER_RAW_STORE14, mcverrawreuse_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD14, X5, Y5, Y6, Y2, MC_VER_RAW_STORE14, mcverrawreuse_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD14, X6, Y6, Y5, Y3, MC_VER_RAW_STORE14, mcverrawreuse_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD14, X5, Y5, Y6, Y4, MC_VER_RAW_STORE14, mcverrawreuse_done)
	VMOVDQA Y5, Y6
	JMP mcverrawreuse_w14_loop

mcverrawreuse_w16:
	MC_VER_INIT(MC_VER_LOAD16)
mcverrawreuse_w16_loop:
	MC_VER_STEP(Y0, Y2, Y4, MC_VER_LOAD16, X5, Y5, Y6, Y0, MC_VER_RAW_STORE16, mcverrawreuse_wide_done)
	MC_VER_STEP(Y1, Y3, Y0, MC_VER_LOAD16, X6, Y6, Y5, Y1, MC_VER_RAW_STORE16, mcverrawreuse_wide_done)
	MC_VER_STEP(Y2, Y4, Y1, MC_VER_LOAD16, X5, Y5, Y6, Y2, MC_VER_RAW_STORE16, mcverrawreuse_wide_done)
	MC_VER_STEP(Y3, Y0, Y2, MC_VER_LOAD16, X6, Y6, Y5, Y3, MC_VER_RAW_STORE16, mcverrawreuse_wide_done)
	MC_VER_STEP(Y4, Y1, Y3, MC_VER_LOAD16, X5, Y5, Y6, Y4, MC_VER_RAW_STORE16, mcverrawreuse_wide_done)
	VMOVDQA Y5, Y6
	JMP mcverrawreuse_w16_loop

mcverrawreuse_wide_done:
	CMPQ SI, $16
	JE mcverrawreuse_done
	SUBQ $8, SI
	LEAQ (R12)(SI*2), AX
	LEAQ (R13)(SI*1), DX
	SUBQ BX, DX
	SUBQ BX, DX
	MOVQ DI, R10
	JMP mcverrawreuse_w8

mcverrawreuse_done:
	VZEROUPPER
	RET


