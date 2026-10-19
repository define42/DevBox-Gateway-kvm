//go:build amd64 && !purego

#include "textflag.h"

TEXT ·blkNotificationBarrier(SB), NOSPLIT, $0-0
	MFENCE
	RET

TEXT ·blkCompletionBarrier(SB), NOSPLIT, $0-0
	RET
