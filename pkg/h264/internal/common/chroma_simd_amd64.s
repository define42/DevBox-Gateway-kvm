#include "textflag.h"

// All weights are nonnegative and sum to 64, so the rounded accumulator is
// at most 255*64+32. Packed signed words cannot overflow before the shift.
TEXT ·mcChromaSSE2(SB), NOSPLIT, $0-88
	MOVQ src_base+0(FP), AX
	MOVQ srcStride+24(FP), CX
	MOVQ dst_base+32(FP), BX
	MOVQ dstStride+56(FP), DX
	MOVQ width+64(FP), SI
	MOVQ height+72(FP), DI
	MOVQ weights+80(FP), X4
	PSHUFLW $0x55, X4, X5
	PSHUFLW $0xaa, X4, X6
	PSHUFLW $0xff, X4, X7
	PSHUFLW $0, X4, X4
	PSHUFL $0, X4, X4
	PSHUFL $0, X5, X5
	PSHUFL $0, X6, X6
	PSHUFL $0, X7, X7
	PXOR X8, X8
	MOVL $0x00200020, R9
	MOVL R9, X9
	PSHUFL $0, X9, X9
chroma_row:
	CMPQ SI, $8
	JE chroma_load8
	CMPQ SI, $4
	JE chroma_load4
	MOVWLZX (AX), R9
	MOVL R9, X0
	MOVWLZX 1(AX), R9
	MOVL R9, X1
	MOVWLZX (AX)(CX*1), R9
	MOVL R9, X2
	MOVWLZX 1(AX)(CX*1), R9
	MOVL R9, X3
	JMP chroma_filter
chroma_load4:
	MOVL (AX), X0
	MOVL 1(AX), X1
	MOVL (AX)(CX*1), X2
	MOVL 1(AX)(CX*1), X3
	JMP chroma_filter
chroma_load8:
	MOVQ (AX), X0
	MOVQ 1(AX), X1
	MOVQ (AX)(CX*1), X2
	MOVQ 1(AX)(CX*1), X3
chroma_filter:
	PUNPCKLBW X8, X0
	PUNPCKLBW X8, X1
	PUNPCKLBW X8, X2
	PUNPCKLBW X8, X3
	PMULLW X4, X0
	PMULLW X5, X1
	PMULLW X6, X2
	PMULLW X7, X3
	PADDW X1, X0
	PADDW X2, X0
	PADDW X3, X0
	PADDW X9, X0
	PSRLW $6, X0
	PACKUSWB X0, X0
	CMPQ SI, $8
	JE chroma_store8
	CMPQ SI, $4
	JE chroma_store4
	MOVL X0, R9
	MOVW R9, (BX)
	JMP chroma_next
chroma_store4:
	MOVL X0, (BX)
	JMP chroma_next
chroma_store8:
	MOVQ X0, (BX)
chroma_next:
	ADDQ CX, AX
	ADDQ DX, BX
	DECQ DI
	JNZ chroma_row
	RET
