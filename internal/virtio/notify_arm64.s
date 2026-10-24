//go:build arm64 && !purego

#include "textflag.h"

TEXT ·notificationBarrier(SB), NOSPLIT, $0-0
	DMB $0xb // ISH: both reads and writes, not the store-only ISHST barrier.
	RET

TEXT ·completionBarrier(SB), NOSPLIT, $0-0
	DMB $0xa // ISHST: data/status and used entries precede the used index.
	RET
