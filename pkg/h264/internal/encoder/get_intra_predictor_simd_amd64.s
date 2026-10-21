//go:build amd64

#include "textflag.h"

// All kernels require only SSE2. Go wrappers validate exact buffer spans.

TEXT ·intraPredFillSSE2(SB), NOSPLIT, $0-25
	MOVQ dst_base+0(FP), AX
	MOVQ dst_len+8(FP), CX
	MOVBLZX value+24(FP), DX
	MOVD DX, X0
	PUNPCKLBW X0, X0
	PUNPCKLWL X0, X0
	PSHUFL $0, X0, X0
fill_loop:
	MOVOU X0, (AX)
	ADDQ $16, AX
	SUBQ $16, CX
	JNZ fill_loop
	RET

// Match the scalar predictors' bottom-to-top traversal, including aliases.
TEXT ·intraPredHorizontalSSE2(SB), NOSPLIT, $0-64
	MOVQ dst_base+0(FP), AX
	MOVQ left_base+24(FP), DX
	MOVQ stride+48(FP), SI
	MOVQ size+56(FP), CX
	MOVQ CX, R8
	DECQ R8
	MOVQ R8, R9
	IMULQ SI, R9
	ADDQ R9, DX
	IMULQ CX, R8
	ADDQ R8, AX
	CMPQ CX, $16
	JE horizontal16_loop
horizontal8_loop:
	MOVBLZX (DX), BX
	MOVD BX, X0
	PUNPCKLBW X0, X0
	PUNPCKLWL X0, X0
	PSHUFL $0, X0, X0
	MOVQ X0, (AX)
	SUBQ SI, DX
	SUBQ $8, AX
	DECQ CX
	JNZ horizontal8_loop
	RET
horizontal16_loop:
	MOVBLZX (DX), BX
	MOVD BX, X0
	PUNPCKLBW X0, X0
	PUNPCKLWL X0, X0
	PSHUFL $0, X0, X0
	MOVOU X0, (AX)
	SUBQ SI, DX
	SUBQ $16, AX
	DECQ CX
	JNZ horizontal16_loop
	RET

DATA ·intraPlaneColumns+0(SB)/8, $0x0003000200010000
DATA ·intraPlaneColumns+8(SB)/8, $0x0007000600050004
GLOBL ·intraPlaneColumns(SB), RODATA|NOPTR, $16

TEXT ·intraPredPlaneSSE2(SB), NOSPLIT, $0-48
	MOVQ dst_base+0(FP), AX
	MOVL base+24(FP), BX
	MOVD BX, X0
	PUNPCKLWL X0, X0
	PSHUFL $0, X0, X0
	MOVL vertical+32(FP), BX
	MOVD BX, X1
	PUNPCKLWL X1, X1
	PSHUFL $0, X1, X1
	MOVL horizontal+28(FP), BX
	MOVD BX, X2
	PUNPCKLWL X2, X2
	PSHUFL $0, X2, X2
	MOVOU ·intraPlaneColumns(SB), X3
	PMULLW X2, X3
	PADDW X0, X3
	PSLLW $3, X2
	MOVQ size+40(FP), CX
	CMPQ CX, $16
	JE plane16_loop
plane8_loop:
	MOVOU X3, X4
	PSRAW $5, X4
	PACKUSWB X4, X4
	MOVQ X4, (AX)
	PADDW X1, X3
	ADDQ $8, AX
	DECQ CX
	JNZ plane8_loop
	RET
plane16_loop:
	MOVOU X3, X4
	MOVOU X3, X5
	PADDW X2, X5
	PSRAW $5, X4
	PSRAW $5, X5
	PACKUSWB X5, X4
	MOVOU X4, (AX)
	PADDW X1, X3
	ADDQ $16, AX
	DECQ CX
	JNZ plane16_loop
	RET

TEXT ·intraPredChromaDCSSE2(SB), NOSPLIT, $0-40
	MOVQ dst_base+0(FP), AX
	MOVQ top+24(FP), X0
	MOVQ bottom+32(FP), X1
	MOVQ X0, 0(AX)
	MOVQ X0, 8(AX)
	MOVQ X0, 16(AX)
	MOVQ X0, 24(AX)
	MOVQ X1, 32(AX)
	MOVQ X1, 40(AX)
	MOVQ X1, 48(AX)
	MOVQ X1, 56(AX)
	RET

// Compute V, H, DC SAD together. Read each encoded row once; no prediction
// scratch is written. Left neighbours are byte loads, top/source rows 16 bytes.
TEXT ·intraSad16x16SSE2(SB), NOSPLIT, $0-96
	MOVQ src_base+0(FP), AX
	MOVQ srcStride+24(FP), BX
	MOVQ top_base+32(FP), CX
	MOVQ left_base+56(FP), DX
	MOVQ refStride+80(FP), SI
	MOVQ costs+88(FP), DI
	PXOR X15, X15
	MOVOU (CX), X0
	MOVOU X0, X1
	PSADBW X15, X1
	MOVQ X1, R9
	PSRLDQ $8, X1
	MOVQ X1, R8
	ADDQ R8, R9
	MOVQ DX, R10
	MOVQ $16, R8
sad_left_sum:
	MOVBLZX (R10), R11
	ADDQ R11, R9
	ADDQ SI, R10
	DECQ R8
	JNZ sad_left_sum
	ADDQ $16, R9
	SHRQ $5, R9
	MOVD R9, X1
	PUNPCKLBW X1, X1
	PUNPCKLWL X1, X1
	PSHUFL $0, X1, X1
	PXOR X2, X2
	PXOR X3, X3
	PXOR X4, X4
	MOVQ $16, R8
sad16_loop:
	MOVOU (AX), X5
	MOVOU X5, X6
	PSADBW X0, X6
	PADDQ X6, X2
	MOVOU X5, X6
	PSADBW X1, X6
	PADDQ X6, X4
	MOVBLZX (DX), R9
	MOVD R9, X7
	PUNPCKLBW X7, X7
	PUNPCKLWL X7, X7
	PSHUFL $0, X7, X7
	PSADBW X7, X5
	PADDQ X5, X3
	ADDQ BX, AX
	ADDQ SI, DX
	DECQ R8
	JNZ sad16_loop
	MOVOU X2, X5
	PSRLDQ $8, X5
	PADDQ X5, X2
	MOVL X2, 0(DI)
	MOVOU X3, X5
	PSRLDQ $8, X5
	PADDQ X5, X3
	MOVL X3, 4(DI)
	MOVOU X4, X5
	PSRLDQ $8, X5
	PADDQ X5, X4
	MOVL X4, 8(DI)
	RET

TEXT ·intraSad8x8SSE2(SB), NOSPLIT, $0-112
	MOVQ src_base+0(FP), AX
	MOVQ srcStride+24(FP), BX
	MOVQ top_base+32(FP), CX
	MOVQ left_base+56(FP), DX
	MOVQ refStride+80(FP), SI
	MOVQ costs+104(FP), DI
	MOVQ (CX), X0
	MOVQ dcTop+88(FP), X1
	MOVQ dcBottom+96(FP), X8
	PXOR X2, X2
	PXOR X3, X3
	PXOR X4, X4
	MOVQ $8, R8
sad8_loop:
	MOVQ (AX), X5
	MOVOU X5, X6
	PSADBW X0, X6
	PADDQ X6, X2
	MOVOU X5, X6
	PSADBW X1, X6
	PADDQ X6, X4
	MOVBLZX (DX), R9
	MOVD R9, X7
	PUNPCKLBW X7, X7
	PUNPCKLWL X7, X7
	PSHUFL $0, X7, X7
	// Zero high bytes so they contribute nothing to an 8-pixel SAD.
	MOVQ X7, X7
	PSADBW X7, X5
	PADDQ X5, X3
	ADDQ BX, AX
	ADDQ SI, DX
	DECQ R8
	JZ sad8_done
	CMPQ R8, $4
	JNE sad8_loop
	MOVOU X8, X1
	JMP sad8_loop
sad8_done:
	MOVL X2, 0(DI)
	MOVL X3, 4(DI)
	MOVL X4, 8(DI)
	RET
