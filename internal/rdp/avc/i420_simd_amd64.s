//go:build amd64

#include "textflag.h"

// The luma identity
//
//     54R + 183G + 18B = 256G + 54R - 73G + 18B
//
// keeps each signed intermediate within 16 bits. VPMADDUBSW forms the latter
// three terms, after which an arithmetic shift and G produce the exact scalar
// result.
DATA ·i420YCoeff<>+0(SB)/8, $0x0012b7360012b736
DATA ·i420YCoeff<>+8(SB)/8, $0x0012b7360012b736
DATA ·i420YCoeff<>+16(SB)/8, $0x0012b7360012b736
DATA ·i420YCoeff<>+24(SB)/8, $0x0012b7360012b736
GLOBL ·i420YCoeff<>(SB), RODATA|NOPTR, $32

// The same luma identity applied to B,G,R,X source bytes.
DATA ·i420BGRXYCoeff<>+0(SB)/8, $0x0036b7120036b712
DATA ·i420BGRXYCoeff<>+8(SB)/8, $0x0036b7120036b712
DATA ·i420BGRXYCoeff<>+16(SB)/8, $0x0036b7120036b712
DATA ·i420BGRXYCoeff<>+24(SB)/8, $0x0036b7120036b712
GLOBL ·i420BGRXYCoeff<>(SB), RODATA|NOPTR, $32

// Select G as words, duplicated to match VPHADDW's lane layout.
DATA ·i420GShuffle<>+0(SB)/8, $0x800d800980058001
DATA ·i420GShuffle<>+8(SB)/8, $0x800d800980058001
DATA ·i420GShuffle<>+16(SB)/8, $0x800d800980058001
DATA ·i420GShuffle<>+24(SB)/8, $0x800d800980058001
GLOBL ·i420GShuffle<>(SB), RODATA|NOPTR, $32

// Arrange each adjacent RGBA pixel pair as R0,R1,G0,G1,B0,B1,0,0.
DATA ·i420PairShuffle<>+0(SB)/8, $0x8080060205010400
DATA ·i420PairShuffle<>+8(SB)/8, $0x80800e0a0d090c08
DATA ·i420PairShuffle<>+16(SB)/8, $0x8080060205010400
DATA ·i420PairShuffle<>+24(SB)/8, $0x80800e0a0d090c08
GLOBL ·i420PairShuffle<>(SB), RODATA|NOPTR, $32

// Arrange adjacent BGRX pixels into the same R0,R1,G0,G1,B0,B1,0,0 order.
DATA ·i420BGRXPairShuffle<>+0(SB)/8, $0x8080040005010602
DATA ·i420BGRXPairShuffle<>+8(SB)/8, $0x80800c080d090e0a
DATA ·i420BGRXPairShuffle<>+16(SB)/8, $0x8080040005010602
DATA ·i420BGRXPairShuffle<>+24(SB)/8, $0x80800c080d090e0a
GLOBL ·i420BGRXPairShuffle<>(SB), RODATA|NOPTR, $32

DATA ·i420ByteOnes<>+0(SB)/8, $0x0101010101010101
DATA ·i420ByteOnes<>+8(SB)/8, $0x0101010101010101
DATA ·i420ByteOnes<>+16(SB)/8, $0x0101010101010101
DATA ·i420ByteOnes<>+24(SB)/8, $0x0101010101010101
GLOBL ·i420ByteOnes<>(SB), RODATA|NOPTR, $32

DATA ·i420UCoeff<>+0(SB)/8, $0x00000080ff9dffe3
DATA ·i420UCoeff<>+8(SB)/8, $0x00000080ff9dffe3
DATA ·i420UCoeff<>+16(SB)/8, $0x00000080ff9dffe3
DATA ·i420UCoeff<>+24(SB)/8, $0x00000080ff9dffe3
GLOBL ·i420UCoeff<>(SB), RODATA|NOPTR, $32

DATA ·i420VCoeff<>+0(SB)/8, $0x0000fff4ff8c0080
DATA ·i420VCoeff<>+8(SB)/8, $0x0000fff4ff8c0080
DATA ·i420VCoeff<>+16(SB)/8, $0x0000fff4ff8c0080
DATA ·i420VCoeff<>+24(SB)/8, $0x0000fff4ff8c0080
GLOBL ·i420VCoeff<>(SB), RODATA|NOPTR, $32

DATA ·i420ChromaBias<>+0(SB)/8, $0x0000008000000080
DATA ·i420ChromaBias<>+8(SB)/8, $0x0000008000000080
DATA ·i420ChromaBias<>+16(SB)/8, $0x0000008000000080
DATA ·i420ChromaBias<>+24(SB)/8, $0x0000008000000080
GLOBL ·i420ChromaBias<>(SB), RODATA|NOPTR, $32

// func rgb32ToI420AVX2Kernel(dstY0, dstY1, dstU, dstV, src0, src1 *byte,
//     width, redIndex int) bool
// Requires AVX2. width is a positive multiple of eight pixels.
TEXT ·rgb32ToI420AVX2Kernel(SB), NOSPLIT, $0-65
	MOVQ dstY0+0(FP), AX
	MOVQ dstY1+8(FP), BX
	MOVQ dstU+16(FP), CX
	MOVQ dstV+24(FP), DX
	MOVQ src0+32(FP), SI
	MOVQ src1+40(FP), DI
	MOVQ width+48(FP), R8

	VMOVDQU ·i420YCoeff<>(SB), Y15
	VMOVDQU ·i420PairShuffle<>(SB), Y13
	CMPQ redIndex+56(FP), $0
	JE i420AVX2CoefficientsReady
	VMOVDQU ·i420BGRXYCoeff<>(SB), Y15
	VMOVDQU ·i420BGRXPairShuffle<>(SB), Y13

i420AVX2CoefficientsReady:
	VMOVDQU ·i420GShuffle<>(SB), Y14
	VMOVDQU ·i420ByteOnes<>(SB), Y12
	VMOVDQU ·i420UCoeff<>(SB), Y11
	VMOVDQU ·i420VCoeff<>(SB), Y10
	VMOVDQU ·i420ChromaBias<>(SB), Y9

	XORQ R9, R9   // pixel offset
	XORQ R14, R14 // chroma-byte offset
	XORQ R15, R15 // accumulated destination differences

i420AVX2Loop:
	VMOVDQU (SI)(R9*4), Y0
	VMOVDQU (DI)(R9*4), Y1

	// Eight luma samples from the first row.
	VPMADDUBSW Y15, Y0, Y2
	VPHADDW Y2, Y2, Y2
	VPSHUFB Y14, Y0, Y3
	VPERMQ $0xd8, Y2, Y2
	VPERMQ $0xd8, Y3, Y3
	VPSRAW $8, Y2, Y2
	VPADDW Y3, Y2, Y2
	VPACKUSWB Y2, Y2, Y2
	VMOVQ X2, R11
	MOVQ (AX)(R9*1), R12
	XORQ R11, R12
	ORQ R12, R15
	MOVQ R11, (AX)(R9*1)

	// Eight luma samples from the second row.
	VPMADDUBSW Y15, Y1, Y2
	VPHADDW Y2, Y2, Y2
	VPSHUFB Y14, Y1, Y3
	VPERMQ $0xd8, Y2, Y2
	VPERMQ $0xd8, Y3, Y3
	VPSRAW $8, Y2, Y2
	VPADDW Y3, Y2, Y2
	VPACKUSWB Y2, Y2, Y2
	VMOVQ X2, R11
	MOVQ (BX)(R9*1), R12
	XORQ R11, R12
	ORQ R12, R15
	MOVQ R11, (BX)(R9*1)

	// Sum each 2x2 block by channel, then divide before applying the chroma
	// matrices. This ordering is required for bit-exact scalar results.
	VPSHUFB Y13, Y0, Y2
	VPSHUFB Y13, Y1, Y3
	VPMADDUBSW Y12, Y2, Y2
	VPMADDUBSW Y12, Y3, Y3
	VPADDW Y3, Y2, Y2
	VPSRLW $2, Y2, Y2

	// Four U samples.
	VPMADDWD Y11, Y2, Y3
	VPHADDD Y3, Y3, Y3
	VPERMQ $0xd8, Y3, Y3
	VPSRAD $8, Y3, Y3
	VPADDD Y9, Y3, Y3
	VPACKSSDW Y3, Y3, Y3
	VPACKUSWB Y3, Y3, Y3
	VMOVD X3, R11
	MOVL (CX)(R14*1), R12
	XORL R11, R12
	ORQ R12, R15
	MOVL R11, (CX)(R14*1)

	// Four V samples.
	VPMADDWD Y10, Y2, Y4
	VPHADDD Y4, Y4, Y4
	VPERMQ $0xd8, Y4, Y4
	VPSRAD $8, Y4, Y4
	VPADDD Y9, Y4, Y4
	VPACKSSDW Y4, Y4, Y4
	VPACKUSWB Y4, Y4, Y4
	VMOVD X4, R11
	MOVL (DX)(R14*1), R12
	XORL R11, R12
	ORQ R12, R15
	MOVL R11, (DX)(R14*1)

	ADDQ $8, R9
	ADDQ $4, R14
	CMPQ R9, R8
	JB i420AVX2Loop

	VZEROUPPER
	TESTQ R15, R15
	SETNE ret+64(FP)
	RET
