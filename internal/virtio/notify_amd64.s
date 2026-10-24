//go:build amd64 && !purego

#include "textflag.h"

TEXT ·notificationBarrier(SB), NOSPLIT, $0-0
	MFENCE
	RET

TEXT ·completionBarrier(SB), NOSPLIT, $0-0
	RET
