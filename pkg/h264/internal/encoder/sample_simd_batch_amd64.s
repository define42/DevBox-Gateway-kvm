//go:build amd64

#include "textflag.h"

// Process four rows at a time. Each independent group of four columns is
// a 4x4 Hadamard transform: larger blocks retain their sum-of-4x4 metric.
TEXT ·satdBlockAVX2(SB), NOSPLIT, $0-88
    MOVQ src_base+0(FP), AX
    MOVQ srcStride+24(FP), CX
    MOVQ ref_base+32(FP), BX
    MOVQ refStride+56(FP), DX
    MOVQ width+64(FP), R8
    MOVQ height+72(FP), R9
    LEAQ ret+80(FP), R13
    XORQ R15, R15
    JMP satdRowsAVX2<>(SB)

TEXT ·satdAvgBlockAVX2(SB), NOSPLIT, $0-120
    MOVQ src_base+0(FP), AX
    MOVQ srcStride+24(FP), CX
    MOVQ a_base+32(FP), BX
    MOVQ aStride+56(FP), DX
    MOVQ b_base+64(FP), SI
    MOVQ bStride+88(FP), DI
    MOVQ width+96(FP), R8
    MOVQ height+104(FP), R9
    LEAQ ret+112(FP), R13
    MOVQ $1, R15
    JMP satdRowsAVX2<>(SB)

// AX/CX: encoded pixels/stride, BX/DX: prediction A/stride,
// SI/DI: prediction B/stride when R15 != 0. R8 is 8 or 16;
// R9 is a positive multiple of 4. All input spans are checked in Go.
TEXT satdRowsAVX2<>(SB), NOSPLIT|NOFRAME, $0-0
    VPXOR Y14, Y14, Y14
    MOVL $0x00010001, R10
    VMOVD R10, X15
    VPBROADCASTD X15, Y15
satd_rows:
    TESTQ R15, R15
    JNZ satd_avg_rows
    CMPQ R8, $16
    JE satd_load16
    JMP satd_load8
satd_avg_rows:
    CMPQ R8, $16
    JE satd_avg_load16
    JMP satd_avg_load8

satd_load8:
    VMOVQ (AX), X0
    VMOVQ (BX), X4
    VPMOVZXBW X0, Y0
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y0, Y0
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X1
    VMOVQ (BX), X4
    VPMOVZXBW X1, Y1
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y1, Y1
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X2
    VMOVQ (BX), X4
    VPMOVZXBW X2, Y2
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y2, Y2
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X3
    VMOVQ (BX), X4
    VPMOVZXBW X3, Y3
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y3, Y3
    ADDQ CX, AX
    ADDQ DX, BX
    JMP satd_transform

satd_load16:
    VMOVDQU (AX), X0
    VMOVDQU (BX), X4
    VPMOVZXBW X0, Y0
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y0, Y0
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVDQU (AX), X1
    VMOVDQU (BX), X4
    VPMOVZXBW X1, Y1
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y1, Y1
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVDQU (AX), X2
    VMOVDQU (BX), X4
    VPMOVZXBW X2, Y2
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y2, Y2
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVDQU (AX), X3
    VMOVDQU (BX), X4
    VPMOVZXBW X3, Y3
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y3, Y3
    ADDQ CX, AX
    ADDQ DX, BX
    JMP satd_transform

satd_avg_load8:
    VMOVQ (AX), X0
    VMOVQ (BX), X4
    VMOVQ (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VPMOVZXBW X0, Y0
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y0, Y0
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X1
    VMOVQ (BX), X4
    VMOVQ (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VPMOVZXBW X1, Y1
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y1, Y1
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X2
    VMOVQ (BX), X4
    VMOVQ (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VPMOVZXBW X2, Y2
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y2, Y2
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X3
    VMOVQ (BX), X4
    VMOVQ (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VPMOVZXBW X3, Y3
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y3, Y3
    ADDQ CX, AX
    ADDQ DX, BX
    JMP satd_transform

satd_avg_load16:
    VMOVDQU (AX), X0
    VMOVDQU (BX), X4
    VMOVDQU (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VPMOVZXBW X0, Y0
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y0, Y0
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVDQU (AX), X1
    VMOVDQU (BX), X4
    VMOVDQU (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VPMOVZXBW X1, Y1
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y1, Y1
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVDQU (AX), X2
    VMOVDQU (BX), X4
    VMOVDQU (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VPMOVZXBW X2, Y2
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y2, Y2
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVDQU (AX), X3
    VMOVDQU (BX), X4
    VMOVDQU (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VPMOVZXBW X3, Y3
    VPMOVZXBW X4, Y4
    VPSUBW Y4, Y3, Y3
    ADDQ CX, AX
    ADDQ DX, BX
    JMP satd_transform

satd_transform:
    // Vertical butterflies, simultaneously for all columns.
    VPADDW Y2, Y0, Y4
    VPADDW Y3, Y1, Y5
    VPSUBW Y2, Y0, Y6
    VPSUBW Y3, Y1, Y7
    VPADDW Y5, Y4, Y0
    VPADDW Y7, Y6, Y1
    VPSUBW Y7, Y6, Y2
    VPSUBW Y5, Y4, Y3

    // Horizontal butterflies within each four-word group. Signs/order of
    // the AC coefficients may differ, but their absolute values are exact.
    VPSHUFLW $0xb1, Y0, Y4
    VPSHUFHW $0xb1, Y4, Y4
    VPADDW Y4, Y0, Y5
    VPSUBW Y4, Y0, Y6
    VPBLENDW $0xaa, Y6, Y5, Y0
    VPSHUFD $0xb1, Y0, Y4
    VPADDW Y4, Y0, Y5
    VPSUBW Y4, Y0, Y6
    VPBLENDW $0xcc, Y6, Y5, Y0
    VPABSW Y0, Y0

    // Horizontal butterflies within each four-word group. Signs/order of
    // the AC coefficients may differ, but their absolute values are exact.
    VPSHUFLW $0xb1, Y1, Y4
    VPSHUFHW $0xb1, Y4, Y4
    VPADDW Y4, Y1, Y5
    VPSUBW Y4, Y1, Y6
    VPBLENDW $0xaa, Y6, Y5, Y1
    VPSHUFD $0xb1, Y1, Y4
    VPADDW Y4, Y1, Y5
    VPSUBW Y4, Y1, Y6
    VPBLENDW $0xcc, Y6, Y5, Y1
    VPABSW Y1, Y1

    // Horizontal butterflies within each four-word group. Signs/order of
    // the AC coefficients may differ, but their absolute values are exact.
    VPSHUFLW $0xb1, Y2, Y4
    VPSHUFHW $0xb1, Y4, Y4
    VPADDW Y4, Y2, Y5
    VPSUBW Y4, Y2, Y6
    VPBLENDW $0xaa, Y6, Y5, Y2
    VPSHUFD $0xb1, Y2, Y4
    VPADDW Y4, Y2, Y5
    VPSUBW Y4, Y2, Y6
    VPBLENDW $0xcc, Y6, Y5, Y2
    VPABSW Y2, Y2

    // Horizontal butterflies within each four-word group. Signs/order of
    // the AC coefficients may differ, but their absolute values are exact.
    VPSHUFLW $0xb1, Y3, Y4
    VPSHUFHW $0xb1, Y4, Y4
    VPADDW Y4, Y3, Y5
    VPSUBW Y4, Y3, Y6
    VPBLENDW $0xaa, Y6, Y5, Y3
    VPSHUFD $0xb1, Y3, Y4
    VPADDW Y4, Y3, Y5
    VPSUBW Y4, Y3, Y6
    VPBLENDW $0xcc, Y6, Y5, Y3
    VPABSW Y3, Y3

    VPADDW Y1, Y0, Y0
    VPADDW Y3, Y2, Y2
    VPADDW Y2, Y0, Y0
    VPMADDWD Y15, Y0, Y0
    VPHADDD Y0, Y0, Y0
    // Normalize each 4x4 independently with (sum + 1) >> 1. Each lane
    // contains two duplicated block sums; only the first two are reduced.
    VPSRLD $16, Y15, Y4
    VPADDD Y4, Y0, Y0
    VPSRLD $1, Y0, Y0
    VPADDD Y0, Y14, Y14
    SUBQ $4, R9
    JNZ satd_rows
    VEXTRACTI128 $1, Y14, X0
    VPADDD X14, X0, X0
    VPSHUFD $0xb1, X0, X1
    VPADDD X1, X0, X0
    VMOVD X0, AX
    VZEROUPPER
    MOVQ AX, (R13)
    RET
