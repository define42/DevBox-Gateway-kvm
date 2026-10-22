//go:build amd64

#include "textflag.h"

// Pair signed words before multiplying, widening each pair sum to int32.
// Three pairs implement [1,-5,20,20,-5,1] over the complete int16 domain.
// |sum| <= 52*32768, so neither PMADDWD nor the dword additions overflow.
#define CENTER_PAIR_SUM(A, B, T0, T1, M01, M23, M45, C01, C23, C45, ROUND) \
    VPSHUFB M01, A, T0; \
    VPSHUFB M23, A, T1; \
    VPSHUFB M45, B, B; \
    VPMADDWD C01, T0, T0; \
    VPMADDWD C23, T1, T1; \
    VPMADDWD C45, B, B; \
    VPADDD T1, T0, A; \
    VPADDD B, A, A; \
    VPADDD ROUND, A, A; \
    VPSRAD $10, A, A

// Two overlapping 16-byte reads cover exactly nine input words for four
// outputs. The second load starts one word after the first.
#define CENTER_PAIR4(A, B, OUT) \
    VMOVDQU A(DX), X0; \
    VMOVDQU B(DX), X1; \
    CENTER_PAIR_SUM(X0, X1, X2, X3, X8, X9, X10, X11, X12, X13, X7); \
    VPACKSSDW X0, X0, X0; \
    VPACKUSWB X0, X0, X0; \
    VMOVD X0, OUT(AX)

// Each 128-bit lane produces four adjacent outputs. The upper lane starts
// four words later, so the four reads cover exactly thirteen input words.
#define CENTER_PAIR8(A, B, C, D, OUT) \
    VMOVDQU A(DX), X0; \
    VINSERTI128 $1, B(DX), Y0, Y0; \
    VMOVDQU C(DX), X1; \
    VINSERTI128 $1, D(DX), Y1, Y1; \
    CENTER_PAIR_SUM(Y0, Y1, Y2, Y3, Y8, Y9, Y10, Y11, Y12, Y13, Y7); \
    VEXTRACTI128 $1, Y0, X1; \
    VPACKSSDW X1, X0, X0; \
    VPACKUSWB X0, X0, X0; \
    VMOVQ X0, OUT(AX)

// The final vector overlaps preceding outputs instead of computing a scalar
// tail. Preserve the original group/store order and exact width+5 input span.
TEXT ·mcHorRawRefineAVX2(SB), NOSPLIT, $0-48
    MOVQ dst+0(FP), AX
    MOVQ dstStride+8(FP), CX
    MOVQ src+16(FP), DX
    MOVQ srcStride+24(FP), BX
    SHLQ $1, BX
    MOVQ width+32(FP), SI
    MOVQ height+40(FP), DI
    VPCMPEQD Y7, Y7, Y7
    VPSRLD $31, Y7, Y7
    VPSLLD $9, Y7, Y7
    VBROADCASTI128 ·mcCenterPair01Mask(SB), Y8
    VBROADCASTI128 ·mcCenterPair23Mask(SB), Y9
    VBROADCASTI128 ·mcCenterPair45Mask(SB), Y10
    VPBROADCASTD ·mcCenterPair01Coeff(SB), Y11
    VPBROADCASTD ·mcCenterPair23Coeff(SB), Y12
    VPBROADCASTD ·mcCenterPair45Coeff(SB), Y13
    CMPQ SI, $5
    JE center_refine5
    CMPQ SI, $9
    JE center_refine9
center_refine17:
    CENTER_PAIR8(0, 8, 2, 10, 0)
    CENTER_PAIR8(16, 24, 18, 26, 8)
    CENTER_PAIR4(26, 28, 13)
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ DI
    JNZ center_refine17
    VZEROUPPER
    RET

center_refine9:
    CENTER_PAIR8(0, 8, 2, 10, 0)
    CENTER_PAIR4(10, 12, 5)
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ DI
    JNZ center_refine9
    VZEROUPPER
    RET

center_refine5:
    CENTER_PAIR4(0, 2, 0)
    CENTER_PAIR4(2, 4, 1)
    ADDQ CX, AX
    ADDQ BX, DX
    DECQ DI
    JNZ center_refine5
    VZEROUPPER
    RET

// Word pairs from the first load: [0,1,1,2,2,3,3,4] and
// [2,3,3,4,4,5,5,6]. Pairs [3,4,4,5,5,6,6,7] from the second
// load correspond to absolute input positions [4,5,5,6,6,7,7,8].
DATA ·mcCenterPair01Mask+0(SB)/8, $0x0504030203020100
DATA ·mcCenterPair01Mask+8(SB)/8, $0x0908070607060504
GLOBL ·mcCenterPair01Mask(SB), RODATA|NOPTR, $16
DATA ·mcCenterPair23Mask+0(SB)/8, $0x0908070607060504
DATA ·mcCenterPair23Mask+8(SB)/8, $0x0d0c0b0a0b0a0908
GLOBL ·mcCenterPair23Mask(SB), RODATA|NOPTR, $16
DATA ·mcCenterPair45Mask+0(SB)/8, $0x0b0a090809080706
DATA ·mcCenterPair45Mask+8(SB)/8, $0x0f0e0d0c0d0c0b0a
GLOBL ·mcCenterPair45Mask(SB), RODATA|NOPTR, $16
DATA ·mcCenterPair01Coeff+0(SB)/4, $0xfffb0001
GLOBL ·mcCenterPair01Coeff(SB), RODATA|NOPTR, $4
DATA ·mcCenterPair23Coeff+0(SB)/4, $0x00140014
GLOBL ·mcCenterPair23Coeff(SB), RODATA|NOPTR, $4
DATA ·mcCenterPair45Coeff+0(SB)/4, $0x0001fffb
GLOBL ·mcCenterPair45Coeff(SB), RODATA|NOPTR, $4
