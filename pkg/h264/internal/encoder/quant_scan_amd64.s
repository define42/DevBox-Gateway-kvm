//go:build amd64

#include "textflag.h"

// The four blocks share FF/MF and zigzag vectors. Quantized raster coefficients,
// scanned entropy levels, maxima, and masks are all produced in the same pass.
TEXT ·quantScanFour4x4SSSE3(SB), NOSPLIT, $0-52
	MOVQ dct+0(FP), AX
	MOVQ levels+8(FP), BX
	MOVQ ff+16(FP), CX
	MOVQ mf+24(FP), DX
	MOVQ maxima+32(FP), DI
	MOVQ masks+40(FP), SI
	MOVL ac+48(FP), R9
	MOVQ $4, R8
	JMP quantScanBlocksSSSE3<>(SB)

// Completed luma encoding shares the exact same block loop, with one table
// snapshot and one Go/assembly boundary for sixteen blocks instead of four.
TEXT ·quantScanLumaSSSE3(SB), NOSPLIT, $0-48
	MOVQ dct+0(FP), AX
	MOVQ levels+8(FP), BX
	MOVQ ff+16(FP), CX
	MOVQ mf+24(FP), DX
	MOVQ maxima+32(FP), DI
	MOVQ masks+40(FP), SI
	XORL R9, R9
	MOVQ $16, R8
	JMP quantScanBlocksSSSE3<>(SB)

// AX/BX raster/scanned coefficients; CX/DX FF/MF; DI/SI maxima/masks;
// R8 block count; R9 AC-only flag. Entries validate all spans and alias rules.
TEXT quantScanBlocksSSSE3<>(SB), NOSPLIT|NOFRAME, $0-0
	SHLQ $6, R9
	LEAQ quantScanShuffle<>(SB), R10
	ADDQ R9, R10
	MOVOU (CX), X8
	MOVOU (DX), X9
	PXOR X10, X10
	MOVOU (R10), X12
	MOVOU 16(R10), X13
	MOVOU 32(R10), X14
	MOVOU 48(R10), X15
quant_scan_block:
	MOVOU (AX), X0
	MOVOU X10, X2
	PCMPGTW X0, X2
	PXOR X2, X0
	PSUBW X2, X0
	PADDUSW X8, X0
	PMULHUW X9, X0
	MOVOU X10, X4
	PMAXSW X0, X4
	PXOR X2, X0
	PSUBW X2, X0
	MOVOU X0, (AX)

	MOVOU 16(AX), X1
	MOVOU X10, X3
	PCMPGTW X1, X3
	PXOR X3, X1
	PSUBW X3, X1
	PADDUSW X8, X1
	PMULHUW X9, X1
	PMAXSW X1, X4
	PXOR X3, X1
	PSUBW X3, X1
	MOVOU X1, 16(AX)

	MOVOU X4, X5
	PSRLDQ $8, X5
	PMAXSW X5, X4
	MOVOU X4, X5
	PSRLDQ $4, X5
	PMAXSW X5, X4
	MOVOU X4, X5
	PSRLDQ $2, X5
	PMAXSW X5, X4
	PEXTRW $0, X4, R9
	MOVW R9, (DI)
	TESTL R9, R9
	JZ quant_scan_zero

	MOVOU X0, X2
	MOVOU X1, X3
	PSHUFB X12, X2
	PSHUFB X13, X3
	POR X3, X2
	MOVOU X0, X4
	MOVOU X1, X5
	PSHUFB X14, X4
	PSHUFB X15, X5
	POR X5, X4
	MOVOU X2, (BX)
	MOVOU X4, 16(BX)
	PCMPEQW X10, X2
	PCMPEQW X10, X4
	PACKSSWB X4, X2
	PMOVMSKB X2, R9
	XORL $65535, R9
	MOVW R9, (SI)
	JMP quant_scan_next
quant_scan_zero:
	MOVOU X10, (BX)
	MOVOU X10, 16(BX)
	MOVW $0, (SI)
quant_scan_next:
	ADDQ $32, AX
	ADDQ $32, BX
	ADDQ $2, DI
	ADDQ $2, SI
	DECQ R8
	JNZ quant_scan_block
	RET

DATA quantScanShuffle<>+0(SB)/8, $0x8080090803020100
DATA quantScanShuffle<>+8(SB)/8, $0x0d0c070605040b0a
DATA quantScanShuffle<>+16(SB)/8, $0x0100808080808080
DATA quantScanShuffle<>+24(SB)/8, $0x8080808080808080
DATA quantScanShuffle<>+32(SB)/8, $0x8080808080808080
DATA quantScanShuffle<>+40(SB)/8, $0x8080808080800f0e
DATA quantScanShuffle<>+48(SB)/8, $0x05040b0a09080302
DATA quantScanShuffle<>+56(SB)/8, $0x0f0e0d0c07068080
DATA quantScanShuffle<>+64(SB)/8, $0x0b0a808009080302
DATA quantScanShuffle<>+72(SB)/8, $0x80800d0c07060504
DATA quantScanShuffle<>+80(SB)/8, $0x8080010080808080
DATA quantScanShuffle<>+88(SB)/8, $0x0302808080808080
DATA quantScanShuffle<>+96(SB)/8, $0x0f0e808080808080
DATA quantScanShuffle<>+104(SB)/8, $0x8080808080808080
DATA quantScanShuffle<>+112(SB)/8, $0x808005040b0a0908
DATA quantScanShuffle<>+120(SB)/8, $0x80800f0e0d0c0706
GLOBL quantScanShuffle<>(SB), RODATA|NOPTR, $128
