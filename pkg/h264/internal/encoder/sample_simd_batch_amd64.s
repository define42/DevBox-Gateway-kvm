//go:build amd64

// The packed sum/difference SATD reduction follows OpenH264's AVX2
// implementation in codec/common/x86/satd_sad.asm.
// Copyright (c) 2009-2013, Cisco Systems. All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// Process eight rows at a time for 8-wide blocks and four rows for 16-wide
// blocks or a final 4-row band. Each independent group of four columns is
// a 4x4 Hadamard transform: larger blocks retain their sum-of-4x4 metric.
TEXT ·satdBlockAVX2(SB), NOSPLIT, $0-56
    MOVQ src+0(FP), AX
    MOVQ srcStride+8(FP), CX
    MOVQ ref+16(FP), BX
    MOVQ refStride+24(FP), DX
    MOVQ width+32(FP), R8
    MOVQ height+40(FP), R9
    LEAQ ret+48(FP), R13
    XORQ R15, R15
    JMP satdRowsAVX2<>(SB)

TEXT ·satdAvgBlockAVX2(SB), NOSPLIT, $0-72
    MOVQ src+0(FP), AX
    MOVQ srcStride+8(FP), CX
    MOVQ a+16(FP), BX
    MOVQ aStride+24(FP), DX
    MOVQ b+32(FP), SI
    MOVQ bStride+40(FP), DI
    MOVQ width+48(FP), R8
    MOVQ height+56(FP), R9
    LEAQ ret+64(FP), R13
    MOVQ $1, R15
    JMP satdRowsAVX2<>(SB)

// AX/CX: encoded pixels/stride, BX/DX: prediction A/stride,
// SI/DI: prediction B/stride when R15 != 0. R8 is 8 or 16;
// R9 is a positive multiple of 4. All input spans are checked in Go.
TEXT satdRowsAVX2<>(SB), NOSPLIT|NOFRAME, $0-0
    VPXOR Y14, Y14, Y14
    // Each byte pair becomes a horizontal sum in the low lane and a
    // difference in the high lane. The products fit exactly in int16.
    VMOVDQU ·satdPairSigns(SB), Y13
    MOVL $0x00010001, R10
    VMOVD R10, X15
    VPBROADCASTD X15, Y15
satd_rows:
    TESTQ R15, R15
    JNZ satd_avg_rows
    CMPQ R8, $16
    JE satd_load16
satd_load8:
    CMPQ R9, $8
    JGE satd_packed8_init
    VMOVQ (AX), X0
    VMOVQ (BX), X4
    VINSERTI128 $1, X0, Y0, Y0
    VPMADDUBSW Y13, Y0, Y0
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y0, Y0
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X1
    VMOVQ (BX), X4
    VINSERTI128 $1, X1, Y1, Y1
    VPMADDUBSW Y13, Y1, Y1
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y1, Y1
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X2
    VMOVQ (BX), X4
    VINSERTI128 $1, X2, Y2, Y2
    VPMADDUBSW Y13, Y2, Y2
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y2, Y2
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X3
    VMOVQ (BX), X4
    VINSERTI128 $1, X3, Y3, Y3
    VPMADDUBSW Y13, Y3, Y3
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y3, Y3
    ADDQ CX, AX
    ADDQ DX, BX
    JMP satd_transform

satd_load16:
    VBROADCASTI128 (AX), Y0
    VBROADCASTI128 (BX), Y4
    VPMADDUBSW Y13, Y0, Y0
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y0, Y0
    ADDQ CX, AX
    ADDQ DX, BX
    VBROADCASTI128 (AX), Y1
    VBROADCASTI128 (BX), Y4
    VPMADDUBSW Y13, Y1, Y1
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y1, Y1
    ADDQ CX, AX
    ADDQ DX, BX
    VBROADCASTI128 (AX), Y2
    VBROADCASTI128 (BX), Y4
    VPMADDUBSW Y13, Y2, Y2
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y2, Y2
    ADDQ CX, AX
    ADDQ DX, BX
    VBROADCASTI128 (AX), Y3
    VBROADCASTI128 (BX), Y4
    VPMADDUBSW Y13, Y3, Y3
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y3, Y3
    ADDQ CX, AX
    ADDQ DX, BX
    JMP satd_transform

satd_avg_rows:
    CMPQ R8, $16
    JE satd_avg_load16
satd_avg_load8:
    CMPQ R9, $8
    JGE satd_packed8_init
    VMOVQ (AX), X0
    VMOVQ (BX), X4
    VMOVQ (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VINSERTI128 $1, X0, Y0, Y0
    VPMADDUBSW Y13, Y0, Y0
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y0, Y0
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X1
    VMOVQ (BX), X4
    VMOVQ (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VINSERTI128 $1, X1, Y1, Y1
    VPMADDUBSW Y13, Y1, Y1
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y1, Y1
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X2
    VMOVQ (BX), X4
    VMOVQ (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VINSERTI128 $1, X2, Y2, Y2
    VPMADDUBSW Y13, Y2, Y2
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y2, Y2
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVQ (AX), X3
    VMOVQ (BX), X4
    VMOVQ (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VINSERTI128 $1, X3, Y3, Y3
    VPMADDUBSW Y13, Y3, Y3
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
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
    VINSERTI128 $1, X0, Y0, Y0
    VPMADDUBSW Y13, Y0, Y0
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y0, Y0
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVDQU (AX), X1
    VMOVDQU (BX), X4
    VMOVDQU (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VINSERTI128 $1, X1, Y1, Y1
    VPMADDUBSW Y13, Y1, Y1
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y1, Y1
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVDQU (AX), X2
    VMOVDQU (BX), X4
    VMOVDQU (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VINSERTI128 $1, X2, Y2, Y2
    VPMADDUBSW Y13, Y2, Y2
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y2, Y2
    ADDQ CX, AX
    ADDQ DX, BX
    VMOVDQU (AX), X3
    VMOVDQU (BX), X4
    VMOVDQU (SI), X5
    VPAVGB X5, X4, X4
    ADDQ DI, SI
    VINSERTI128 $1, X3, Y3, Y3
    VPMADDUBSW Y13, Y3, Y3
    VINSERTI128 $1, X4, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
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

    // Complete the horizontal transform without forming its last sum and
    // difference: |a+b| + |a-b| = 2*max(|a|, |b|). The factor two cancels
    // SATD normalization exactly; each 4x4 sum is even. Interleave rows so
    // both words of every dword carry a useful maximum.
    VPABSW Y0, Y0
    VPABSW Y1, Y1
    VPABSW Y2, Y2
    VPABSW Y3, Y3
    VPBLENDW $0xaa, Y1, Y0, Y4
    VPSLLD $16, Y1, Y1
    VPSRLD $16, Y0, Y0
    VPOR Y0, Y1, Y1
    VPMAXUW Y4, Y1, Y1
    VPBLENDW $0xaa, Y3, Y2, Y4
    VPSLLD $16, Y3, Y3
    VPSRLD $16, Y2, Y2
    VPOR Y2, Y3, Y3
    VPMAXUW Y4, Y3, Y3
    VPADDW Y3, Y1, Y0
    VPMADDWD Y15, Y0, Y0
    VPADDD Y0, Y14, Y14
    SUBQ $4, R9
    JNZ satd_repeat16
satd_reduce:
    VEXTRACTI128 $1, Y14, X0
    VPADDD X14, X0, X0
    VPSHUFD $0x4e, X0, X1
    VPADDD X1, X0, X0
    VPSHUFD $0xb1, X0, X1
    VPADDD X1, X0, X0
    VMOVD X0, AX
    VZEROUPPER
    MOVQ AX, (R13)
    RET

satd_repeat16:
    // An 8-wide block enters this transform only for its final four rows:
    // heights of at least eight use the packed path. Thus a nonzero row
    // count here always belongs to width 16; skip the width dispatch.
    TESTQ R15, R15
    JNZ satd_avg_load16
    JMP satd_load16

// Put row y in the low 128-bit lane and row y+4 in the high lane.
// Each row is duplicated within its lane: the first four words become
// horizontal sums and the next four become differences. Thus every lane
// carries useful data while all memory operands still read exactly 8 bytes.
satd_packed8_init:
    VMOVDQU ·satdPairSigns8(SB), Y13
satd_packed8_rows:
    TESTQ R15, R15
    JNZ satd_packed8_avg
    VPBROADCASTQ (AX), Y0
    VPBROADCASTQ (AX)(CX*4), Y4
    VPBLENDD $0xf0, Y4, Y0, Y0
    VPMADDUBSW Y13, Y0, Y0
    VPBROADCASTQ (BX), Y4
    VPBROADCASTQ (BX)(DX*4), Y5
    VPBLENDD $0xf0, Y5, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y0, Y0
    ADDQ CX, AX
    ADDQ DX, BX
    VPBROADCASTQ (AX), Y1
    VPBROADCASTQ (AX)(CX*4), Y4
    VPBLENDD $0xf0, Y4, Y1, Y1
    VPMADDUBSW Y13, Y1, Y1
    VPBROADCASTQ (BX), Y4
    VPBROADCASTQ (BX)(DX*4), Y5
    VPBLENDD $0xf0, Y5, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y1, Y1
    ADDQ CX, AX
    ADDQ DX, BX
    VPBROADCASTQ (AX), Y2
    VPBROADCASTQ (AX)(CX*4), Y4
    VPBLENDD $0xf0, Y4, Y2, Y2
    VPMADDUBSW Y13, Y2, Y2
    VPBROADCASTQ (BX), Y4
    VPBROADCASTQ (BX)(DX*4), Y5
    VPBLENDD $0xf0, Y5, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y2, Y2
    ADDQ CX, AX
    ADDQ DX, BX
    VPBROADCASTQ (AX), Y3
    VPBROADCASTQ (AX)(CX*4), Y4
    VPBLENDD $0xf0, Y4, Y3, Y3
    VPMADDUBSW Y13, Y3, Y3
    VPBROADCASTQ (BX), Y4
    VPBROADCASTQ (BX)(DX*4), Y5
    VPBLENDD $0xf0, Y5, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y3, Y3
    ADDQ CX, AX
    ADDQ DX, BX
    LEAQ (AX)(CX*4), AX
    LEAQ (BX)(DX*4), BX
    JMP satd_packed8_transform

satd_packed8_avg:
    VPBROADCASTQ (AX), Y0
    VPBROADCASTQ (AX)(CX*4), Y4
    VPBLENDD $0xf0, Y4, Y0, Y0
    VPMADDUBSW Y13, Y0, Y0
    VPBROADCASTQ (BX), Y4
    VPBROADCASTQ (BX)(DX*4), Y5
    VPBLENDD $0xf0, Y5, Y4, Y4
    VPBROADCASTQ (SI), Y5
    VPBROADCASTQ (SI)(DI*4), Y6
    VPBLENDD $0xf0, Y6, Y5, Y5
    VPAVGB Y5, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y0, Y0
    ADDQ CX, AX
    ADDQ DX, BX
    ADDQ DI, SI
    VPBROADCASTQ (AX), Y1
    VPBROADCASTQ (AX)(CX*4), Y4
    VPBLENDD $0xf0, Y4, Y1, Y1
    VPMADDUBSW Y13, Y1, Y1
    VPBROADCASTQ (BX), Y4
    VPBROADCASTQ (BX)(DX*4), Y5
    VPBLENDD $0xf0, Y5, Y4, Y4
    VPBROADCASTQ (SI), Y5
    VPBROADCASTQ (SI)(DI*4), Y6
    VPBLENDD $0xf0, Y6, Y5, Y5
    VPAVGB Y5, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y1, Y1
    ADDQ CX, AX
    ADDQ DX, BX
    ADDQ DI, SI
    VPBROADCASTQ (AX), Y2
    VPBROADCASTQ (AX)(CX*4), Y4
    VPBLENDD $0xf0, Y4, Y2, Y2
    VPMADDUBSW Y13, Y2, Y2
    VPBROADCASTQ (BX), Y4
    VPBROADCASTQ (BX)(DX*4), Y5
    VPBLENDD $0xf0, Y5, Y4, Y4
    VPBROADCASTQ (SI), Y5
    VPBROADCASTQ (SI)(DI*4), Y6
    VPBLENDD $0xf0, Y6, Y5, Y5
    VPAVGB Y5, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y2, Y2
    ADDQ CX, AX
    ADDQ DX, BX
    ADDQ DI, SI
    VPBROADCASTQ (AX), Y3
    VPBROADCASTQ (AX)(CX*4), Y4
    VPBLENDD $0xf0, Y4, Y3, Y3
    VPMADDUBSW Y13, Y3, Y3
    VPBROADCASTQ (BX), Y4
    VPBROADCASTQ (BX)(DX*4), Y5
    VPBLENDD $0xf0, Y5, Y4, Y4
    VPBROADCASTQ (SI), Y5
    VPBROADCASTQ (SI)(DI*4), Y6
    VPBLENDD $0xf0, Y6, Y5, Y5
    VPAVGB Y5, Y4, Y4
    VPMADDUBSW Y13, Y4, Y4
    VPSUBW Y4, Y3, Y3
    ADDQ CX, AX
    ADDQ DX, BX
    ADDQ DI, SI
    LEAQ (AX)(CX*4), AX
    LEAQ (BX)(DX*4), BX
    LEAQ (SI)(DI*4), SI

satd_packed8_transform:
    // The lanes contain independent 4-row bands; the same butterflies and
    // absolute-max reduction compute four independent 4x4 transforms.
    VPADDW Y2, Y0, Y4
    VPADDW Y3, Y1, Y5
    VPSUBW Y2, Y0, Y6
    VPSUBW Y3, Y1, Y7
    VPADDW Y5, Y4, Y0
    VPADDW Y7, Y6, Y1
    VPSUBW Y7, Y6, Y2
    VPSUBW Y5, Y4, Y3
    VPABSW Y0, Y0
    VPABSW Y1, Y1
    VPABSW Y2, Y2
    VPABSW Y3, Y3
    VPBLENDW $0xaa, Y1, Y0, Y4
    VPSLLD $16, Y1, Y1
    VPSRLD $16, Y0, Y0
    VPOR Y0, Y1, Y1
    VPMAXUW Y4, Y1, Y1
    VPBLENDW $0xaa, Y3, Y2, Y4
    VPSLLD $16, Y3, Y3
    VPSRLD $16, Y2, Y2
    VPOR Y2, Y3, Y3
    VPMAXUW Y4, Y3, Y3
    VPADDW Y3, Y1, Y0
    VPMADDWD Y15, Y0, Y0
    VPADDD Y0, Y14, Y14
    SUBQ $8, R9
    JZ satd_reduce
    CMPQ R9, $8
    JGE satd_packed8_rows
    // A legal 8x12 average leaves four rows. Restore the original layout
    // before the existing 4-row loader, retaining the 32-bit accumulator.
    VMOVDQU ·satdPairSigns(SB), Y13
    JMP satd_rows

// Pairwise sum coefficients in the low lane, differences in the high lane.
DATA ·satdPairSigns+0(SB)/8, $0x0101010101010101
DATA ·satdPairSigns+8(SB)/8, $0x0101010101010101
DATA ·satdPairSigns+16(SB)/8, $0xff01ff01ff01ff01
DATA ·satdPairSigns+24(SB)/8, $0xff01ff01ff01ff01
GLOBL ·satdPairSigns(SB), RODATA|NOPTR, $32

// Pairwise sums followed by differences within each independent 128-bit lane.
DATA ·satdPairSigns8+0(SB)/8, $0x0101010101010101
DATA ·satdPairSigns8+8(SB)/8, $0xff01ff01ff01ff01
DATA ·satdPairSigns8+16(SB)/8, $0x0101010101010101
DATA ·satdPairSigns8+24(SB)/8, $0xff01ff01ff01ff01
GLOBL ·satdPairSigns8(SB), RODATA|NOPTR, $32
