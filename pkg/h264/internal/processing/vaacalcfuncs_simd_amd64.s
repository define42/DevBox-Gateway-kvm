#include "textflag.h"

// Keep the two PSADBW lanes and the top/bottom accumulators separate to
// emit TL, TR, BL, BR in the same order as the scalar implementation.
TEXT ·vaaSadRowSSE2(SB), NOSPLIT, $0-84
	MOVQ cur_base+0(FP), AX
	MOVQ ref_base+24(FP), BX
	MOVQ stride+48(FP), CX
	MOVQ sad_base+56(FP), DI
	MOVQ sad_len+64(FP), R8
	XORL R10, R10
	TESTQ R8, R8
	JE done
block:
	MOVQ AX, SI
	MOVQ BX, DX
	PXOR X0, X0
	PXOR X1, X1
	MOVQ $8, R9
top:
	MOVOU (SI), X2
	MOVOU (DX), X3
	PSADBW X3, X2
	PADDQ X2, X0
	ADDQ CX, SI
	ADDQ CX, DX
	DECQ R9
	JNZ top
	MOVQ $8, R9
bottom:
	MOVOU (SI), X2
	MOVOU (DX), X3
	PSADBW X3, X2
	PADDQ X2, X1
	ADDQ CX, SI
	ADDQ CX, DX
	DECQ R9
	JNZ bottom
	MOVL X0, R11
	MOVL R11, 0(DI)
	ADDL R11, R10
	PSRLDQ $8, X0
	MOVL X0, R11
	MOVL R11, 4(DI)
	ADDL R11, R10
	MOVL X1, R11
	MOVL R11, 8(DI)
	ADDL R11, R10
	PSRLDQ $8, X1
	MOVL X1, R11
	MOVL R11, 12(DI)
	ADDL R11, R10
	ADDQ $16, AX
	ADDQ $16, BX
	ADDQ $16, DI
	DECQ R8
	JNZ block
done:
	MOVL R10, ret+80(FP)
	RET
