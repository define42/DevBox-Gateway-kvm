//go:build amd64

// Six-tap interpolation uses the H.264 coefficients [1, -5, 20, 20, -5, 1].
// Horizontal byte inputs use packed multiply-adds; vertical inputs widen
// sixteen samples at once. The second pass widens eight signed intermediates.
// Narrow tails never read beyond the halo.
#include "textflag.h"
// func mcHorVer20AVX2(dst []byte, dstStride int, src []byte,
//     srcStride, width, height int)
// Requires AVX2. Source points at the first output sample, including its halo.
TEXT ·mcHorVer20AVX2(SB), NOSPLIT, $0-80
	MOVQ dst_base+0(FP), AX
	MOVQ dstStride+24(FP), CX
	MOVQ src_base+32(FP), DX
	MOVQ srcStride+56(FP), BX
	MOVQ width+64(FP), SI
	MOVQ height+72(FP), DI
	VPCMPEQW Y7, Y7, Y7
	VPSRLW $15, Y7, Y7
	VPSLLW $4, Y7, Y7

	// Three byte-pair dot products implement all six taps without widening
	// six separate vectors. Each pair is in [-1275, 10200], so PMADDUBSW
	// saturation cannot occur. Exact-width input loads preserve the halo.
	VBROADCASTI128 ·mcPair01Mask(SB), Y8
	VBROADCASTI128 ·mcPair23Mask(SB), Y9
	VBROADCASTI128 ·mcPair45Mask(SB), Y10
	VPBROADCASTD ·mcPair01Coeff(SB), Y11
	VPBROADCASTD ·mcPair23Coeff(SB), Y12
	VPBROADCASTD ·mcPair45Coeff(SB), Y13

mchorver20avx2_row:
	MOVQ SI, R8

mchorver20avx2_16:
	CMPQ R8, $16
	JB mchorver20avx2_8
	VMOVDQU -2(DX), X0
	VMOVDQU 3(DX), X1
	// The second eight outputs need only thirteen bytes. Shift an overlapping
	// exact-width load instead of reading three bytes beyond the right halo.
	VPSRLDQ $3, X1, X1
	VINSERTI128 $1, X1, Y0, Y0
	VPSHUFB Y8, Y0, Y1
	VPSHUFB Y9, Y0, Y2
	VPSHUFB Y10, Y0, Y3
	VPMADDUBSW Y11, Y1, Y1
	VPMADDUBSW Y12, Y2, Y2
	VPMADDUBSW Y13, Y3, Y3
	VPADDW Y1, Y2, Y0
	VPADDW Y3, Y0, Y0
	VPADDW Y7, Y0, Y0
	VPSRAW $5, Y0, Y0
	VEXTRACTI128 $1, Y0, X1
	VPACKUSWB X1, X0, X0
	VMOVDQU X0, (AX)
	ADDQ $16, AX
	ADDQ $16, DX
	SUBQ $16, R8
	JMP mchorver20avx2_16

mchorver20avx2_8:
	CMPQ R8, $8
	JB mchorver20avx2_4
	VPMOVZXBW -2(DX), X0
	VPMOVZXBW 3(DX), X1
	VPMOVZXBW -1(DX), X2
	VPMOVZXBW 2(DX), X3
	VPMOVZXBW 0(DX), X4
	VPMOVZXBW 1(DX), X5
	VPADDW X1, X0, X0
	VPADDW X3, X2, X2
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X5
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X4
	VPSLLW $2, X2, X3
	VPADDW X2, X3, X3
	VPADDW X4, X0, X0
	VPSUBW X3, X0, X0
	VPADDW X7, X0, X0
	VPSRAW $5, X0, X0
	VPACKUSWB X0, X0, X0
	VMOVQ X0, (AX)
	ADDQ $8, AX
	ADDQ $8, DX
	SUBQ $8, R8
	JMP mchorver20avx2_8

mchorver20avx2_4:
	CMPQ R8, $4
	JB mchorver20avx2_tail
	MOVL -2(DX), R9
	VMOVD R9, X0
	VPMOVZXBW X0, X0
	MOVL 3(DX), R9
	VMOVD R9, X1
	VPMOVZXBW X1, X1
	MOVL -1(DX), R9
	VMOVD R9, X2
	VPMOVZXBW X2, X2
	MOVL 2(DX), R9
	VMOVD R9, X3
	VPMOVZXBW X3, X3
	MOVL 0(DX), R9
	VMOVD R9, X4
	VPMOVZXBW X4, X4
	MOVL 1(DX), R9
	VMOVD R9, X5
	VPMOVZXBW X5, X5
	VPADDW X1, X0, X0
	VPADDW X3, X2, X2
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X5
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X4
	VPSLLW $2, X2, X3
	VPADDW X2, X3, X3
	VPADDW X4, X0, X0
	VPSUBW X3, X0, X0
	VPADDW X7, X0, X0
	VPSRAW $5, X0, X0
	VPACKUSWB X0, X0, X0
	VMOVD X0, R9
	MOVL R9, (AX)
	ADDQ $4, AX
	ADDQ $4, DX
	SUBQ $4, R8
	JMP mchorver20avx2_4

mchorver20avx2_tail:
	TESTQ R8, R8
	JE mchorver20avx2_row_done
	MOVBLZX -2(DX), R9
	MOVBLZX 3(DX), R11
	ADDQ R11, R9
	MOVBLZX -1(DX), R11
	MOVBLZX 2(DX), R13
	ADDQ R13, R11
	IMULQ $5, R11
	SUBQ R11, R9
	MOVBLZX 0(DX), R11
	MOVBLZX 1(DX), R13
	ADDQ R13, R11
	IMULQ $20, R11
	ADDQ R11, R9
	ADDQ $16, R9
	SARQ $5, R9
	CMPQ R9, $0
	JGE mchorver20avx2_tail_high
	XORQ R9, R9
	JMP mchorver20avx2_tail_store
mchorver20avx2_tail_high:
	CMPQ R9, $255
	JLE mchorver20avx2_tail_store
	MOVQ $255, R9
mchorver20avx2_tail_store:
	MOVB R9, (AX)
	INCQ AX
	INCQ DX
	DECQ R8
	JMP mchorver20avx2_tail

mchorver20avx2_row_done:
	ADDQ CX, AX
	SUBQ SI, AX
	ADDQ BX, DX
	SUBQ SI, DX
	DECQ DI
	JNZ mchorver20avx2_row
	VZEROUPPER
	RET

// func mcHorVer02AVX2(dst []byte, dstStride int, src []byte,
//     srcStride, width, height int)
// Requires AVX2. Source points at the first output sample, including its halo.
TEXT ·mcHorVer02AVX2(SB), NOSPLIT, $0-80
	MOVQ dst_base+0(FP), AX
	MOVQ dstStride+24(FP), CX
	MOVQ src_base+32(FP), DX
	MOVQ srcStride+56(FP), BX
	MOVQ width+64(FP), SI
	MOVQ height+72(FP), DI
	LEAQ (BX)(BX*1), R10
	LEAQ (BX)(BX*2), R12
	VPCMPEQW Y7, Y7, Y7
	VPSRLW $15, Y7, Y7
	VPSLLW $4, Y7, Y7

mchorver02avx2_row:
	MOVQ SI, R8

mchorver02avx2_16:
	CMPQ R8, $16
	JB mchorver02avx2_8
	MOVQ DX, R11
	SUBQ R10, R11
	VPMOVZXBW (R11), Y0
	LEAQ (DX)(R12*1), R11
	VPMOVZXBW (R11), Y1
	MOVQ DX, R11
	SUBQ BX, R11
	VPMOVZXBW (R11), Y2
	LEAQ (DX)(R10*1), R11
	VPMOVZXBW (R11), Y3
	VPMOVZXBW (DX), Y4
	VPMOVZXBW (DX)(BX*1), Y5
	VPADDW Y1, Y0, Y0
	VPADDW Y3, Y2, Y2
	VPADDW Y5, Y4, Y4
	VPSLLW $2, Y4, Y5
	VPADDW Y5, Y4, Y4
	VPSLLW $2, Y4, Y4
	VPSLLW $2, Y2, Y3
	VPADDW Y2, Y3, Y3
	VPADDW Y4, Y0, Y0
	VPSUBW Y3, Y0, Y0
	VPADDW Y7, Y0, Y0
	VPSRAW $5, Y0, Y0
	VEXTRACTI128 $1, Y0, X1
	VPACKUSWB X1, X0, X0
	VMOVDQU X0, (AX)
	ADDQ $16, AX
	ADDQ $16, DX
	SUBQ $16, R8
	JMP mchorver02avx2_16

mchorver02avx2_8:
	CMPQ R8, $8
	JB mchorver02avx2_4
	MOVQ DX, R11
	SUBQ R10, R11
	VPMOVZXBW (R11), X0
	LEAQ (DX)(R12*1), R11
	VPMOVZXBW (R11), X1
	MOVQ DX, R11
	SUBQ BX, R11
	VPMOVZXBW (R11), X2
	LEAQ (DX)(R10*1), R11
	VPMOVZXBW (R11), X3
	VPMOVZXBW (DX), X4
	VPMOVZXBW (DX)(BX*1), X5
	VPADDW X1, X0, X0
	VPADDW X3, X2, X2
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X5
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X4
	VPSLLW $2, X2, X3
	VPADDW X2, X3, X3
	VPADDW X4, X0, X0
	VPSUBW X3, X0, X0
	VPADDW X7, X0, X0
	VPSRAW $5, X0, X0
	VPACKUSWB X0, X0, X0
	VMOVQ X0, (AX)
	ADDQ $8, AX
	ADDQ $8, DX
	SUBQ $8, R8
	JMP mchorver02avx2_8

mchorver02avx2_4:
	CMPQ R8, $4
	JB mchorver02avx2_tail
	MOVQ DX, R11
	SUBQ R10, R11
	MOVL (R11), R9
	VMOVD R9, X0
	VPMOVZXBW X0, X0
	LEAQ (DX)(R12*1), R11
	MOVL (R11), R9
	VMOVD R9, X1
	VPMOVZXBW X1, X1
	MOVQ DX, R11
	SUBQ BX, R11
	MOVL (R11), R9
	VMOVD R9, X2
	VPMOVZXBW X2, X2
	LEAQ (DX)(R10*1), R11
	MOVL (R11), R9
	VMOVD R9, X3
	VPMOVZXBW X3, X3
	MOVL (DX), R9
	VMOVD R9, X4
	VPMOVZXBW X4, X4
	MOVL (DX)(BX*1), R9
	VMOVD R9, X5
	VPMOVZXBW X5, X5
	VPADDW X1, X0, X0
	VPADDW X3, X2, X2
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X5
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X4
	VPSLLW $2, X2, X3
	VPADDW X2, X3, X3
	VPADDW X4, X0, X0
	VPSUBW X3, X0, X0
	VPADDW X7, X0, X0
	VPSRAW $5, X0, X0
	VPACKUSWB X0, X0, X0
	VMOVD X0, R9
	MOVL R9, (AX)
	ADDQ $4, AX
	ADDQ $4, DX
	SUBQ $4, R8
	JMP mchorver02avx2_4

mchorver02avx2_tail:
	TESTQ R8, R8
	JE mchorver02avx2_row_done
	MOVQ DX, R11
	SUBQ R10, R11
	MOVBLZX (R11), R9
	MOVBLZX (DX)(R12*1), R13
	ADDQ R13, R9
	MOVQ DX, R11
	SUBQ BX, R11
	MOVBLZX (R11), R13
	MOVBLZX (DX)(R10*1), R11
	ADDQ R11, R13
	IMULQ $5, R13
	SUBQ R13, R9
	MOVBLZX (DX), R13
	MOVBLZX (DX)(BX*1), R11
	ADDQ R11, R13
	IMULQ $20, R13
	ADDQ R13, R9
	ADDQ $16, R9
	SARQ $5, R9
	CMPQ R9, $0
	JGE mchorver02avx2_tail_high
	XORQ R9, R9
	JMP mchorver02avx2_tail_store
mchorver02avx2_tail_high:
	CMPQ R9, $255
	JLE mchorver02avx2_tail_store
	MOVQ $255, R9
mchorver02avx2_tail_store:
	MOVB R9, (AX)
	INCQ AX
	INCQ DX
	DECQ R8
	JMP mchorver02avx2_tail

mchorver02avx2_row_done:
	ADDQ CX, AX
	SUBQ SI, AX
	ADDQ BX, DX
	SUBQ SI, DX
	DECQ DI
	JNZ mchorver02avx2_row
	VZEROUPPER
	RET

// func mcVerticalRawAVX2(dst []int16, dstStride int, src []byte,
//     srcStride, width, height int)
// Requires AVX2. Source points at the first output sample, including its halo.
TEXT ·mcVerticalRawAVX2(SB), NOSPLIT, $0-80
	MOVQ dst_base+0(FP), AX
	MOVQ dstStride+24(FP), CX
	MOVQ src_base+32(FP), DX
	MOVQ srcStride+56(FP), BX
	MOVQ width+64(FP), SI
	MOVQ height+72(FP), DI
	SHLQ $1, CX
	LEAQ (BX)(BX*1), R10
	LEAQ (BX)(BX*2), R12

mcverticalrawavx2_row:
	MOVQ SI, R8

mcverticalrawavx2_16:
	CMPQ R8, $16
	JB mcverticalrawavx2_8
	MOVQ DX, R11
	SUBQ R10, R11
	VPMOVZXBW (R11), Y0
	LEAQ (DX)(R12*1), R11
	VPMOVZXBW (R11), Y1
	MOVQ DX, R11
	SUBQ BX, R11
	VPMOVZXBW (R11), Y2
	LEAQ (DX)(R10*1), R11
	VPMOVZXBW (R11), Y3
	VPMOVZXBW (DX), Y4
	VPMOVZXBW (DX)(BX*1), Y5
	VPADDW Y1, Y0, Y0
	VPADDW Y3, Y2, Y2
	VPADDW Y5, Y4, Y4
	VPSLLW $2, Y4, Y5
	VPADDW Y5, Y4, Y4
	VPSLLW $2, Y4, Y4
	VPSLLW $2, Y2, Y3
	VPADDW Y2, Y3, Y3
	VPADDW Y4, Y0, Y0
	VPSUBW Y3, Y0, Y0
	VMOVDQU Y0, (AX)
	ADDQ $32, AX
	ADDQ $16, DX
	SUBQ $16, R8
	JMP mcverticalrawavx2_16

mcverticalrawavx2_8:
	CMPQ R8, $8
	JB mcverticalrawavx2_4
	MOVQ DX, R11
	SUBQ R10, R11
	VPMOVZXBW (R11), X0
	LEAQ (DX)(R12*1), R11
	VPMOVZXBW (R11), X1
	MOVQ DX, R11
	SUBQ BX, R11
	VPMOVZXBW (R11), X2
	LEAQ (DX)(R10*1), R11
	VPMOVZXBW (R11), X3
	VPMOVZXBW (DX), X4
	VPMOVZXBW (DX)(BX*1), X5
	VPADDW X1, X0, X0
	VPADDW X3, X2, X2
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X5
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X4
	VPSLLW $2, X2, X3
	VPADDW X2, X3, X3
	VPADDW X4, X0, X0
	VPSUBW X3, X0, X0
	VMOVDQU X0, (AX)
	ADDQ $16, AX
	ADDQ $8, DX
	SUBQ $8, R8
	JMP mcverticalrawavx2_8

mcverticalrawavx2_4:
	CMPQ R8, $4
	JB mcverticalrawavx2_tail
	MOVQ DX, R11
	SUBQ R10, R11
	MOVL (R11), R9
	VMOVD R9, X0
	VPMOVZXBW X0, X0
	LEAQ (DX)(R12*1), R11
	MOVL (R11), R9
	VMOVD R9, X1
	VPMOVZXBW X1, X1
	MOVQ DX, R11
	SUBQ BX, R11
	MOVL (R11), R9
	VMOVD R9, X2
	VPMOVZXBW X2, X2
	LEAQ (DX)(R10*1), R11
	MOVL (R11), R9
	VMOVD R9, X3
	VPMOVZXBW X3, X3
	MOVL (DX), R9
	VMOVD R9, X4
	VPMOVZXBW X4, X4
	MOVL (DX)(BX*1), R9
	VMOVD R9, X5
	VPMOVZXBW X5, X5
	VPADDW X1, X0, X0
	VPADDW X3, X2, X2
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X5
	VPADDW X5, X4, X4
	VPSLLW $2, X4, X4
	VPSLLW $2, X2, X3
	VPADDW X2, X3, X3
	VPADDW X4, X0, X0
	VPSUBW X3, X0, X0
	VMOVQ X0, (AX)
	ADDQ $8, AX
	ADDQ $4, DX
	SUBQ $4, R8
	JMP mcverticalrawavx2_4

mcverticalrawavx2_tail:
	TESTQ R8, R8
	JE mcverticalrawavx2_row_done
	MOVQ DX, R11
	SUBQ R10, R11
	MOVBLZX (R11), R9
	MOVBLZX (DX)(R12*1), R13
	ADDQ R13, R9
	MOVQ DX, R11
	SUBQ BX, R11
	MOVBLZX (R11), R13
	MOVBLZX (DX)(R10*1), R11
	ADDQ R11, R13
	IMULQ $5, R13
	SUBQ R13, R9
	MOVBLZX (DX), R13
	MOVBLZX (DX)(BX*1), R11
	ADDQ R11, R13
	IMULQ $20, R13
	ADDQ R13, R9
	MOVW R9, (AX)
	ADDQ $2, AX
	INCQ DX
	DECQ R8
	JMP mcverticalrawavx2_tail

mcverticalrawavx2_row_done:
	ADDQ CX, AX
	SUBQ SI, AX
	SUBQ SI, AX
	ADDQ BX, DX
	SUBQ SI, DX
	DECQ DI
	JNZ mcverticalrawavx2_row
	VZEROUPPER
	RET

// func mcHorizontalRawToU8AVX2(dst []byte, dstStride int, src []int16,
//     srcStride, width, height int)
// Requires AVX2. Signed intermediate values are widened before arithmetic.
TEXT ·mcHorizontalRawToU8AVX2(SB), NOSPLIT, $0-80
	MOVQ dst_base+0(FP), AX
	MOVQ dstStride+24(FP), CX
	MOVQ src_base+32(FP), DX
	MOVQ srcStride+56(FP), BX
	SHLQ $1, BX
	MOVQ width+64(FP), SI
	MOVQ height+72(FP), DI
	VPCMPEQD Y7, Y7, Y7
	VPSRLD $31, Y7, Y7
	VPSLLD $9, Y7, Y7

mchorizontalrawtou8avx2_row:
	MOVQ SI, R8

mchorizontalrawtou8avx2_8:
	CMPQ R8, $8
	JB mchorizontalrawtou8avx2_4
	VPMOVSXWD 0(DX), Y0
	VPMOVSXWD 10(DX), Y1
	VPMOVSXWD 2(DX), Y2
	VPMOVSXWD 8(DX), Y3
	VPMOVSXWD 4(DX), Y4
	VPMOVSXWD 6(DX), Y5
	VPADDD Y1, Y0, Y0
	VPADDD Y3, Y2, Y2
	VPADDD Y5, Y4, Y4
	VPSLLD $2, Y4, Y5
	VPADDD Y5, Y4, Y4
	VPSLLD $2, Y4, Y4
	VPSLLD $2, Y2, Y3
	VPADDD Y2, Y3, Y3
	VPADDD Y4, Y0, Y0
	VPSUBD Y3, Y0, Y0
	VPADDD Y7, Y0, Y0
	VPSRAD $10, Y0, Y0
	VEXTRACTI128 $1, Y0, X1
	VPACKSSDW X1, X0, X0
	VPACKUSWB X0, X0, X0
	VMOVQ X0, (AX)
	ADDQ $8, AX
	ADDQ $16, DX
	SUBQ $8, R8
	JMP mchorizontalrawtou8avx2_8

mchorizontalrawtou8avx2_4:
	CMPQ R8, $4
	JB mchorizontalrawtou8avx2_tail
	VPMOVSXWD 0(DX), X0
	VPMOVSXWD 10(DX), X1
	VPMOVSXWD 2(DX), X2
	VPMOVSXWD 8(DX), X3
	VPMOVSXWD 4(DX), X4
	VPMOVSXWD 6(DX), X5
	VPADDD X1, X0, X0
	VPADDD X3, X2, X2
	VPADDD X5, X4, X4
	VPSLLD $2, X4, X5
	VPADDD X5, X4, X4
	VPSLLD $2, X4, X4
	VPSLLD $2, X2, X3
	VPADDD X2, X3, X3
	VPADDD X4, X0, X0
	VPSUBD X3, X0, X0
	VPADDD X7, X0, X0
	VPSRAD $10, X0, X0
	VPACKSSDW X0, X0, X0
	VPACKUSWB X0, X0, X0
	VMOVD X0, R9
	MOVL R9, (AX)
	ADDQ $4, AX
	ADDQ $8, DX
	SUBQ $4, R8
	JMP mchorizontalrawtou8avx2_4

mchorizontalrawtou8avx2_tail:
	TESTQ R8, R8
	JE mchorizontalrawtou8avx2_row_done
	MOVWQSX 0(DX), R9
	MOVWQSX 10(DX), R10
	ADDQ R10, R9
	MOVWQSX 2(DX), R11
	MOVWQSX 8(DX), R10
	ADDQ R10, R11
	IMULQ $5, R11
	MOVWQSX 4(DX), R12
	MOVWQSX 6(DX), R10
	ADDQ R10, R12
	IMULQ $20, R12
	ADDQ R12, R9
	SUBQ R11, R9
	ADDQ $512, R9
	SARQ $10, R9
	CMPQ R9, $0
	JGE mchorizontalrawtou8avx2_tail_high
	XORQ R9, R9
	JMP mchorizontalrawtou8avx2_tail_store
mchorizontalrawtou8avx2_tail_high:
	CMPQ R9, $255
	JLE mchorizontalrawtou8avx2_tail_store
	MOVQ $255, R9
mchorizontalrawtou8avx2_tail_store:
	MOVB R9, (AX)
	INCQ AX
	ADDQ $2, DX
	DECQ R8
	JMP mchorizontalrawtou8avx2_tail

mchorizontalrawtou8avx2_row_done:
	ADDQ CX, AX
	SUBQ SI, AX
	ADDQ BX, DX
	SUBQ SI, DX
	SUBQ SI, DX
	DECQ DI
	JNZ mchorizontalrawtou8avx2_row
	VZEROUPPER
	RET

// Shuffle each horizontal tap pair into the unsigned byte operand of
// VPMADDUBSW. Each 128-bit lane generates eight consecutive output samples.
DATA ·mcPair01Mask+0(SB)/8, $0x0403030202010100
DATA ·mcPair01Mask+8(SB)/8, $0x0807070606050504
GLOBL ·mcPair01Mask(SB), RODATA|NOPTR, $16
DATA ·mcPair23Mask+0(SB)/8, $0x0605050404030302
DATA ·mcPair23Mask+8(SB)/8, $0x0a09090808070706
GLOBL ·mcPair23Mask(SB), RODATA|NOPTR, $16
DATA ·mcPair45Mask+0(SB)/8, $0x0807070606050504
DATA ·mcPair45Mask+8(SB)/8, $0x0c0b0b0a0a090908
GLOBL ·mcPair45Mask(SB), RODATA|NOPTR, $16
DATA ·mcPair01Coeff+0(SB)/4, $0xfb01fb01
GLOBL ·mcPair01Coeff(SB), RODATA|NOPTR, $4
DATA ·mcPair23Coeff+0(SB)/4, $0x14141414
GLOBL ·mcPair23Coeff(SB), RODATA|NOPTR, $4
DATA ·mcPair45Coeff+0(SB)/4, $0x01fb01fb
GLOBL ·mcPair45Coeff(SB), RODATA|NOPTR, $4
