//go:build amd64

#include "textflag.h"

// Each source row is loaded once for the up, down, left, and right candidates.
// The wrappers validate all four reference spans, including the negative row
// offset. Narrow blocks use exact-width loads, including MOVL for four pixels.

// func sadFour16SSE2(src []byte, srcStride int, ref []byte, refStride, height int, sad *[4]int32)
TEXT ·sadFour16SSE2(SB), NOSPLIT, $0-80
    MOVQ src_base+0(FP), AX
    MOVQ srcStride+24(FP), CX
    MOVQ ref_base+32(FP), DX
    MOVQ refStride+56(FP), BX
    MOVQ height+64(FP), R9
    MOVQ sad+72(FP), R10
    MOVQ BX, R8
    NEGQ R8
    PXOR X0, X0
    PXOR X1, X1
    PXOR X2, X2
    PXOR X3, X3
sad_four_16_row:
    MOVOU (AX), X4
    MOVOU (DX)(R8*1), X5
    MOVOU (DX)(BX*1), X6
    MOVOU -1(DX), X7
    MOVOU 1(DX), X8
    PSADBW X4, X5
    PSADBW X4, X6
    PSADBW X4, X7
    PSADBW X4, X8
    PADDD X5, X0
    PADDD X6, X1
    PADDD X7, X2
    PADDD X8, X3
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ R9
    JNZ sad_four_16_row
    MOVOU X0, X4
    PSRLDQ $8, X4
    PADDD X4, X0
    MOVOU X1, X4
    PSRLDQ $8, X4
    PADDD X4, X1
    MOVOU X2, X4
    PSRLDQ $8, X4
    PADDD X4, X2
    MOVOU X3, X4
    PSRLDQ $8, X4
    PADDD X4, X3
    MOVL X0, 0(R10)
    MOVL X1, 4(R10)
    MOVL X2, 8(R10)
    MOVL X3, 12(R10)
    RET

// func sadFour8SSE2(src []byte, srcStride int, ref []byte, refStride, height int, sad *[4]int32)
TEXT ·sadFour8SSE2(SB), NOSPLIT, $0-80
    MOVQ src_base+0(FP), AX
    MOVQ srcStride+24(FP), CX
    MOVQ ref_base+32(FP), DX
    MOVQ refStride+56(FP), BX
    MOVQ height+64(FP), R9
    MOVQ sad+72(FP), R10
    MOVQ BX, R8
    NEGQ R8
    PXOR X0, X0
    PXOR X1, X1
    PXOR X2, X2
    PXOR X3, X3
sad_four_8_row:
    MOVQ (AX), X4
    MOVQ (DX)(R8*1), X5
    MOVQ (DX)(BX*1), X6
    MOVQ -1(DX), X7
    MOVQ 1(DX), X8
    PSADBW X4, X5
    PSADBW X4, X6
    PSADBW X4, X7
    PSADBW X4, X8
    PADDD X5, X0
    PADDD X6, X1
    PADDD X7, X2
    PADDD X8, X3
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ R9
    JNZ sad_four_8_row
    MOVL X0, 0(R10)
    MOVL X1, 4(R10)
    MOVL X2, 8(R10)
    MOVL X3, 12(R10)
    RET

// func sadFour4SSE2(src []byte, srcStride int, ref []byte, refStride, height int, sad *[4]int32)
TEXT ·sadFour4SSE2(SB), NOSPLIT, $0-80
    MOVQ src_base+0(FP), AX
    MOVQ srcStride+24(FP), CX
    MOVQ ref_base+32(FP), DX
    MOVQ refStride+56(FP), BX
    MOVQ height+64(FP), R9
    MOVQ sad+72(FP), R10
    MOVQ BX, R8
    NEGQ R8
    PXOR X0, X0
    PXOR X1, X1
    PXOR X2, X2
    PXOR X3, X3
sad_four_4_row:
    MOVL (AX), X4
    MOVL (DX)(R8*1), X5
    MOVL (DX)(BX*1), X6
    MOVL -1(DX), X7
    MOVL 1(DX), X8
    PSADBW X4, X5
    PSADBW X4, X6
    PSADBW X4, X7
    PSADBW X4, X8
    PADDD X5, X0
    PADDD X6, X1
    PADDD X7, X2
    PADDD X8, X3
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ R9
    JNZ sad_four_4_row
    MOVL X0, 0(R10)
    MOVL X1, 4(R10)
    MOVL X2, 8(R10)
    MOVL X3, 12(R10)
    RET
