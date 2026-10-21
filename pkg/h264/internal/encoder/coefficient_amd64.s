//go:build amd64

#include "textflag.h"

// PCMPEQW and PACKSSWB produce one sign bit for each zero coefficient.
TEXT ·coefficientMaskSSE2(SB), NOSPLIT, $0-28
	MOVQ levels_base+0(FP), AX
	MOVOU (AX), X0
	MOVOU 16(AX), X1
	PXOR X2, X2
	PCMPEQW X2, X0
	PCMPEQW X2, X1
	PACKSSWB X1, X0
	PMOVMSKB X0, AX
	XORL $65535, AX
	MOVL AX, ret+24(FP)
	RET

// Load, zigzag, store, and extract nonzero metadata without rereading levels.
TEXT ·coefficientScanMaskSSSE3(SB), NOSPLIT, $0-60
	MOVQ levels_base+0(FP), AX
	MOVQ dct_base+24(FP), BX
	MOVL ac+48(FP), CX
	SHLQ $6, CX
	LEAQ coefficientScanShuffle<>(SB), DX
	ADDQ CX, DX
	MOVOU (BX), X0
	MOVOU 16(BX), X1
	MOVO X0, X2
	MOVO X1, X3
	PSHUFB (DX), X2
	PSHUFB 16(DX), X3
	POR X3, X2
	MOVO X0, X4
	MOVO X1, X5
	PSHUFB 32(DX), X4
	PSHUFB 48(DX), X5
	POR X5, X4
	MOVOU X2, (AX)
	MOVOU X4, 16(AX)
	PXOR X0, X0
	PCMPEQW X0, X2
	PCMPEQW X0, X4
	PACKSSWB X4, X2
	PMOVMSKB X2, AX
	XORL $65535, AX
	MOVL AX, ret+56(FP)
	RET

DATA coefficientScanShuffle<>+0(SB)/8, $0x8080090803020100
DATA coefficientScanShuffle<>+8(SB)/8, $0x0d0c070605040b0a
DATA coefficientScanShuffle<>+16(SB)/8, $0x0100808080808080
DATA coefficientScanShuffle<>+24(SB)/8, $0x8080808080808080
DATA coefficientScanShuffle<>+32(SB)/8, $0x8080808080808080
DATA coefficientScanShuffle<>+40(SB)/8, $0x8080808080800f0e
DATA coefficientScanShuffle<>+48(SB)/8, $0x05040b0a09080302
DATA coefficientScanShuffle<>+56(SB)/8, $0x0f0e0d0c07068080
DATA coefficientScanShuffle<>+64(SB)/8, $0x0b0a808009080302
DATA coefficientScanShuffle<>+72(SB)/8, $0x80800d0c07060504
DATA coefficientScanShuffle<>+80(SB)/8, $0x8080010080808080
DATA coefficientScanShuffle<>+88(SB)/8, $0x0302808080808080
DATA coefficientScanShuffle<>+96(SB)/8, $0x0f0e808080808080
DATA coefficientScanShuffle<>+104(SB)/8, $0x8080808080808080
DATA coefficientScanShuffle<>+112(SB)/8, $0x808005040b0a0908
DATA coefficientScanShuffle<>+120(SB)/8, $0x80800f0e0d0c0706
GLOBL coefficientScanShuffle<>(SB), RODATA|NOPTR, $128
