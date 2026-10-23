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

// The 32-pixel kernel below avoids horizontal adds. Luma reuses the identity
// above: VPMADDUBSW and VPMADDWD form 54R-73G+18B per pixel, and the G byte
// masked in place is exactly 256G. Their sum, 54R+183G+18B, lies in [0,65025],
// so it packs to unsigned words without saturation before the final shift.
DATA ·i420GMask<>+0(SB)/8, $0x0000ff000000ff00
DATA ·i420GMask<>+8(SB)/8, $0x0000ff000000ff00
DATA ·i420GMask<>+16(SB)/8, $0x0000ff000000ff00
DATA ·i420GMask<>+24(SB)/8, $0x0000ff000000ff00
GLOBL ·i420GMask<>(SB), RODATA|NOPTR, $32

DATA ·i420WordOnes<>+0(SB)/8, $0x0001000100010001
DATA ·i420WordOnes<>+8(SB)/8, $0x0001000100010001
DATA ·i420WordOnes<>+16(SB)/8, $0x0001000100010001
DATA ·i420WordOnes<>+24(SB)/8, $0x0001000100010001
GLOBL ·i420WordOnes<>(SB), RODATA|NOPTR, $32

// Two in-lane packs leave four-pixel groups in 0,2,4,6,1,3,5,7 order.
DATA ·i420LumaOrder<>+0(SB)/8, $0x0000000400000000
DATA ·i420LumaOrder<>+8(SB)/8, $0x0000000500000001
DATA ·i420LumaOrder<>+16(SB)/8, $0x0000000600000002
DATA ·i420LumaOrder<>+24(SB)/8, $0x0000000700000003
GLOBL ·i420LumaOrder<>(SB), RODATA|NOPTR, $32

// Negated chroma coefficients fit signed bytes: 29R+99G-128B and
// -128R+116G+12B. Each word pair stays within [-32640,32640]; VPMADDWD with
// -1 restores the scalar numerator, which still fits a signed word.
DATA ·i420UByteCoeff<>+0(SB)/8, $0x0080631d0080631d
DATA ·i420UByteCoeff<>+8(SB)/8, $0x0080631d0080631d
DATA ·i420UByteCoeff<>+16(SB)/8, $0x0080631d0080631d
DATA ·i420UByteCoeff<>+24(SB)/8, $0x0080631d0080631d
GLOBL ·i420UByteCoeff<>(SB), RODATA|NOPTR, $32

DATA ·i420VByteCoeff<>+0(SB)/8, $0x000c7480000c7480
DATA ·i420VByteCoeff<>+8(SB)/8, $0x000c7480000c7480
DATA ·i420VByteCoeff<>+16(SB)/8, $0x000c7480000c7480
DATA ·i420VByteCoeff<>+24(SB)/8, $0x000c7480000c7480
GLOBL ·i420VByteCoeff<>(SB), RODATA|NOPTR, $32

// Adding 128 to a signed byte in [-128,127] is an exclusive or with 0x80.
DATA ·i420ByteBias<>+0(SB)/8, $0x8080808080808080
DATA ·i420ByteBias<>+8(SB)/8, $0x8080808080808080
DATA ·i420ByteBias<>+16(SB)/8, $0x8080808080808080
DATA ·i420ByteBias<>+24(SB)/8, $0x8080808080808080
GLOBL ·i420ByteBias<>(SB), RODATA|NOPTR, $32

// After VPERMQ, each lane holds samples 0,1,4,5,8,9,12,13,2,3,6,7,10,11,14,15.
DATA ·i420ChromaOrder<>+0(SB)/8, $0x0b0a030209080100
DATA ·i420ChromaOrder<>+8(SB)/8, $0x0f0e07060d0c0504
DATA ·i420ChromaOrder<>+16(SB)/8, $0x0b0a030209080100
DATA ·i420ChromaOrder<>+24(SB)/8, $0x0f0e07060d0c0504
GLOBL ·i420ChromaOrder<>(SB), RODATA|NOPTR, $32

// I420_WIDE_LUMA converts 32 pixels of one source row, stores 32 luma bytes
// and accumulates destination differences in Y10. Clobbers Y0-Y4.
#define I420_WIDE_LUMA(src, dst) \
	VMOVDQU (src)(R9*4), Y0; \
	VMOVDQU 32(src)(R9*4), Y1; \
	VPMADDUBSW Y15, Y0, Y2; \
	VPMADDUBSW Y15, Y1, Y3; \
	VPMADDWD Y11, Y2, Y2; \
	VPMADDWD Y11, Y3, Y3; \
	VPAND Y14, Y0, Y0; \
	VPAND Y14, Y1, Y1; \
	VPADDD Y0, Y2, Y2; \
	VPADDD Y1, Y3, Y3; \
	VPACKUSDW Y3, Y2, Y2; \
	VMOVDQU 64(src)(R9*4), Y0; \
	VMOVDQU 96(src)(R9*4), Y1; \
	VPMADDUBSW Y15, Y0, Y3; \
	VPMADDUBSW Y15, Y1, Y4; \
	VPMADDWD Y11, Y3, Y3; \
	VPMADDWD Y11, Y4, Y4; \
	VPAND Y14, Y0, Y0; \
	VPAND Y14, Y1, Y1; \
	VPADDD Y0, Y3, Y3; \
	VPADDD Y1, Y4, Y4; \
	VPACKUSDW Y4, Y3, Y3; \
	VPSRLW $8, Y2, Y2; \
	VPSRLW $8, Y3, Y3; \
	VPACKUSWB Y3, Y2, Y2; \
	VPERMD Y2, Y9, Y2; \
	VPXOR (dst)(R9*1), Y2, Y3; \
	VPOR Y3, Y10, Y10; \
	VMOVDQU Y2, (dst)(R9*1)

// I420_WIDE_AVERAGE leaves the floor-averaged R,G,B,0 words of four 2x2
// blocks, read from eight pixels of both rows at offset, in out. Clobbers Y0-Y1.
#define I420_WIDE_AVERAGE(offset, out) \
	VMOVDQU offset(SI)(R9*4), Y0; \
	VMOVDQU offset(DI)(R9*4), Y1; \
	VPSHUFB Y13, Y0, Y0; \
	VPSHUFB Y13, Y1, Y1; \
	VPMADDUBSW Y12, Y0, Y0; \
	VPMADDUBSW Y12, Y1, Y1; \
	VPADDW Y1, Y0, out; \
	VPSRLW $2, out, out

// func rgb32ToI420AVX2Wide(dstY0, dstY1, dstU, dstV, src0, src1 *byte,
//     width, redIndex int) bool
// Requires AVX2. width is a positive multiple of 32 pixels.
TEXT ·rgb32ToI420AVX2Wide(SB), NOSPLIT, $0-65
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
	JE i420WideCoefficientsReady
	VMOVDQU ·i420BGRXYCoeff<>(SB), Y15
	VMOVDQU ·i420BGRXPairShuffle<>(SB), Y13

i420WideCoefficientsReady:
	VMOVDQU ·i420GMask<>(SB), Y14
	VMOVDQU ·i420ByteOnes<>(SB), Y12
	VMOVDQU ·i420WordOnes<>(SB), Y11
	VMOVDQU ·i420LumaOrder<>(SB), Y9
	VMOVDQU ·i420UByteCoeff<>(SB), Y6
	VMOVDQU ·i420VByteCoeff<>(SB), Y7
	VPCMPEQD Y8, Y8, Y8 // Signed word -1.
	VPXOR Y10, Y10, Y10 // Accumulated destination differences.

	XORQ R9, R9   // Pixel offset.
	XORQ R14, R14 // Chroma-byte offset.

i420WideLoop:
	I420_WIDE_LUMA(SI, AX)
	I420_WIDE_LUMA(DI, BX)

	// Samples 0-3, 4-7, 8-11 and 12-15. Packing keeps lanes, so the bytes are
	// R,G,B,0 for samples 0,1,4,5|2,3,6,7 and 8,9,12,13|10,11,14,15.
	I420_WIDE_AVERAGE(0, Y2)
	I420_WIDE_AVERAGE(32, Y3)
	I420_WIDE_AVERAGE(64, Y4)
	I420_WIDE_AVERAGE(96, Y5)
	VPACKUSWB Y3, Y2, Y2
	VPACKUSWB Y5, Y4, Y4

	VPMADDUBSW Y6, Y2, Y0
	VPMADDUBSW Y6, Y4, Y1
	VPMADDWD Y8, Y0, Y0
	VPMADDWD Y8, Y1, Y1
	VPACKSSDW Y1, Y0, Y0

	VPMADDUBSW Y7, Y2, Y1
	VPMADDUBSW Y7, Y4, Y3
	VPMADDWD Y8, Y1, Y1
	VPMADDWD Y8, Y3, Y3
	VPACKSSDW Y3, Y1, Y1

	// Arithmetic shifts match the scalar >> 8; signed packing is then exact.
	VPSRAW $8, Y0, Y0
	VPSRAW $8, Y1, Y1
	VPACKSSWB Y1, Y0, Y0
	VPXOR ·i420ByteBias<>(SB), Y0, Y0
	VPERMQ $0xd8, Y0, Y0
	VPSHUFB ·i420ChromaOrder<>(SB), Y0, Y0

	// The low lane is U and the high lane is V.
	VMOVDQU (CX)(R14*1), X1
	VINSERTI128 $1, (DX)(R14*1), Y1, Y1
	VPXOR Y0, Y1, Y1
	VPOR Y1, Y10, Y10
	VMOVDQU X0, (CX)(R14*1)
	VEXTRACTI128 $1, Y0, (DX)(R14*1)

	ADDQ $32, R9
	ADDQ $16, R14
	CMPQ R9, R8
	JB i420WideLoop

	VPTEST Y10, Y10
	SETNE ret+64(FP)
	VZEROUPPER
	RET
