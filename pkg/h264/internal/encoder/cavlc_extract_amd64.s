//go:build amd64

#include "textflag.h"

// Acquire a SIMD nonzero mask, then compact dense blocks with PSHUFB or
// enumerate sparse coefficients inside the kernel without Go bounds checks.
// Unlike the native kernel, exact-width stores preserve every unused output
// element, the last run includes leading zeros, and end=14 masks word 15 even
// when it is nonzero. All input loads stay inside the 16-word argument.
TEXT ·cavlcExtractSSSE3(SB), NOSPLIT, $0-44
	MOVQ coeff+0(FP), SI
	MOVQ run+8(FP), DI
	MOVQ level+16(FP), BX
	MOVQ total+24(FP), DX
	MOVOU (SI), X0
	MOVOU 16(SI), X1
	PACKSSWB X1, X0
	PXOR X2, X2
	PCMPEQB X2, X0
	PMOVMSKB X0, AX
	XORL $65535, AX
	CMPL end+32(FP), $14
	JNE cavlc_extract_masked
	ANDL $32767, AX
cavlc_extract_masked:
	POPCNTL AX, R10
	MOVL R10, (DX)
	MOVL $0, ret+40(FP)
	TESTL AX, AX
	JZ cavlc_extract_done
	CMPL AX, $65535
	JE cavlc_extract_all16
	CMPL AX, $32767
	JE cavlc_extract_all15
	BSRL AX, CX
	INCL CX
	SUBL R10, CX
	MOVL CX, ret+40(FP)
	CMPL R10, $15
	JE cavlc_extract_one_zero
	JMP cavlc_extract_sparse
cavlc_extract_done:
	RET

// Fully populated blocks need only a reverse scan and zero runs.
cavlc_extract_all16:
	MOVOU (SI), X0
	MOVOU 16(SI), X1
	MOVOU cavlcExtractReverse<>(SB), X3
	PSHUFB X3, X0
	PSHUFB X3, X1
	MOVOU X1, (BX)
	MOVOU X0, 16(BX)
	MOVOU X2, (DI)
	RET
cavlc_extract_all15:
	MOVL $-1, R8
	MOVOU (SI), X0
	MOVOU 16(SI), X1
	MOVOU cavlcExtractReverse<>(SB), X3
	PSHUFB X3, X0
	PSHUFB X3, X1
	PSRLDQ $2, X1
	JMP cavlc_extract_join15

// With exactly one zero, compress its half with one shuffle. The other half
// stays reversed; one byte identifies the sole nonzero run. This avoids four
// nibble-group setups on the nearly full blocks.
cavlc_extract_one_zero:
	XORL $65535, AX
	BSFL AX, CX
	MOVL $14, R8
	SUBL CX, R8
	MOVOU (SI), X0
	MOVOU 16(SI), X1
	MOVOU cavlcExtractReverse<>(SB), X3
	PSHUFB X3, X0
	PSHUFB X3, X1
	LEAQ cavlcExtractRemoveWord<>(SB), R9
	CMPL CX, $8
	JGE cavlc_extract_zero_high
	MOVL $7, DX
	SUBL CX, DX
	SHLQ $4, DX
	PSHUFB (R9)(DX*1), X0
	MOVOU X1, (BX)
	JMP cavlc_extract_store7
cavlc_extract_zero_high:
	MOVL $15, DX
	SUBL CX, DX
	SHLQ $4, DX
	PSHUFB (R9)(DX*1), X1
cavlc_extract_join15:
	MOVOU X0, X4
	PSLLDQ $14, X4
	POR X4, X1
	MOVOU X1, (BX)
	PSRLDQ $2, X0
cavlc_extract_store7:
	MOVQ X0, 16(BX)
	PSRLDQ $8, X0
	MOVQ X0, DX
	MOVL DX, 24(BX)
	SHRQ $32, DX
	MOVW DX, 28(BX)
	MOVQ X2, (DI)
	MOVL $0, 8(DI)
	MOVW $0, 12(DI)
	MOVB $0, 14(DI)
	TESTL R8, R8
	JS cavlc_extract_done
	MOVB $1, (DI)(R8*1)
	RET

// Blocks with up to 14 nonzeros need fewer instructions when the acquired
// SIMD mask drives exact scalar loads, rather than setting up four shuffles.
// Keep this choice in the kernel: returning a mask to Go adds call/check costs.
cavlc_extract_sparse:
	BSRL AX, CX
cavlc_extract_sparse_next:
	MOVW (SI)(CX*2), DX
	MOVW DX, (BX)
	BTRL CX, AX
	MOVL CX, R8
	TESTL AX, AX
	JZ cavlc_extract_sparse_last
	BSRL AX, CX
	SUBL CX, R8
	DECL R8
	MOVB R8, (DI)
	ADDQ $2, BX
	INCQ DI
	JMP cavlc_extract_sparse_next
cavlc_extract_sparse_last:
	MOVB R8, (DI)
	RET

DATA cavlcExtractReverse<>+0(SB)/8, $0x09080b0a0d0c0f0e
DATA cavlcExtractReverse<>+8(SB)/8, $0x0100030205040706
GLOBL cavlcExtractReverse<>(SB), RODATA|NOPTR, $16

// Remove one reversed coefficient while preserving the order of seven others.
DATA cavlcExtractRemoveWord<>+0(SB)/8, $0x0908070605040302
DATA cavlcExtractRemoveWord<>+8(SB)/8, $0x80800f0e0d0c0b0a
DATA cavlcExtractRemoveWord<>+16(SB)/8, $0x0908070605040100
DATA cavlcExtractRemoveWord<>+24(SB)/8, $0x80800f0e0d0c0b0a
DATA cavlcExtractRemoveWord<>+32(SB)/8, $0x0908070603020100
DATA cavlcExtractRemoveWord<>+40(SB)/8, $0x80800f0e0d0c0b0a
DATA cavlcExtractRemoveWord<>+48(SB)/8, $0x0908050403020100
DATA cavlcExtractRemoveWord<>+56(SB)/8, $0x80800f0e0d0c0b0a
DATA cavlcExtractRemoveWord<>+64(SB)/8, $0x0706050403020100
DATA cavlcExtractRemoveWord<>+72(SB)/8, $0x80800f0e0d0c0b0a
DATA cavlcExtractRemoveWord<>+80(SB)/8, $0x0706050403020100
DATA cavlcExtractRemoveWord<>+88(SB)/8, $0x80800f0e0d0c0908
DATA cavlcExtractRemoveWord<>+96(SB)/8, $0x0706050403020100
DATA cavlcExtractRemoveWord<>+104(SB)/8, $0x80800f0e0b0a0908
DATA cavlcExtractRemoveWord<>+112(SB)/8, $0x0706050403020100
DATA cavlcExtractRemoveWord<>+120(SB)/8, $0x80800d0c0b0a0908
GLOBL cavlcExtractRemoveWord<>(SB), RODATA|NOPTR, $128
