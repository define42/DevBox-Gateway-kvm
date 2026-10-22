//go:build amd64

// The broadcast/four-store operation follows UpdateMbMv_sse2 in OpenH264's
// codec/common/x86/mb_copy.asm. Unaligned stores support every Go slice origin.
// Copyright (c) 2009-2013, Cisco Systems. All rights reserved.
// BSD-2-Clause; see LICENSE-OpenH264 in the repository root.

#include "textflag.h"

// func updateMbMvSSE2(dst *[16]SMVUnitXY, packed uint32)
// The value was copied before entry. Only dst[0:16], exactly 64 bytes, is written.
TEXT ·updateMbMvSSE2(SB), NOSPLIT, $0-12
    MOVQ dst+0(FP), AX
    MOVL packed+8(FP), X0
    PSHUFL $0, X0, X0
    MOVOU X0, 0(AX)
    MOVOU X0, 16(AX)
    MOVOU X0, 32(AX)
    MOVOU X0, 48(AX)
    RET
