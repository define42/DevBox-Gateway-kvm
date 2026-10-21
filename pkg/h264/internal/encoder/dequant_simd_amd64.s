//go:build amd64

#include "textflag.h"

// Low-word multiplication preserves the scalar int16 truncation, for every
// signed coefficient and uint16 multiplier (including overflowing products).
TEXT ·dequant4x4SSE2(SB), NOSPLIT, $0-48
	MOVQ dct_base+0(FP), AX
	MOVQ mf_base+24(FP), BX
	MOVOU (BX), X0
	MOVOU 0(AX), X1
	PMULLW X0, X1
	MOVOU X1, 0(AX)
	MOVOU 16(AX), X1
	PMULLW X0, X1
	MOVOU X1, 16(AX)
	RET

TEXT ·dequantFour4x4SSE2(SB), NOSPLIT, $0-48
	MOVQ dct_base+0(FP), AX
	MOVQ mf_base+24(FP), BX
	MOVOU (BX), X0
	MOVOU 0(AX), X1
	PMULLW X0, X1
	MOVOU X1, 0(AX)
	MOVOU 16(AX), X1
	PMULLW X0, X1
	MOVOU X1, 16(AX)
	MOVOU 32(AX), X1
	PMULLW X0, X1
	MOVOU X1, 32(AX)
	MOVOU 48(AX), X1
	PMULLW X0, X1
	MOVOU X1, 48(AX)
	MOVOU 64(AX), X1
	PMULLW X0, X1
	MOVOU X1, 64(AX)
	MOVOU 80(AX), X1
	PMULLW X0, X1
	MOVOU X1, 80(AX)
	MOVOU 96(AX), X1
	PMULLW X0, X1
	MOVOU X1, 96(AX)
	MOVOU 112(AX), X1
	PMULLW X0, X1
	MOVOU X1, 112(AX)
	RET

