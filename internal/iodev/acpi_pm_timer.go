package iodev

import (
	"encoding/binary"
	"time"
)

type ACPIPMTimer struct {
	Start time.Time
}

const (
	pmTimerFreqHz  uint64 = 3_579_545
	nanosPerSecond uint64 = 1_000_000_000
)

func NewACPIPMTimer() *ACPIPMTimer {
	return &ACPIPMTimer{
		Start: time.Now(),
	}
}

func (a *ACPIPMTimer) Read(base uint64, data []byte) error {
	if len(data) != 4 {
		return errDataLenInvalid
	}

	binary.LittleEndian.PutUint32(data, pmTimerTicks(time.Since(a.Start)))

	return nil
}

// Split seconds from nanoseconds before multiplication. Multiplying the whole
// duration by the timer frequency overflows int64 after about 43 minutes.
func pmTimerTicks(elapsed time.Duration) uint32 {
	if elapsed <= 0 {
		return 0
	}
	seconds := uint64(elapsed / time.Second)
	nanos := uint64(elapsed % time.Second)

	return uint32(seconds*pmTimerFreqHz + nanos*pmTimerFreqHz/nanosPerSecond)
}

func (a *ACPIPMTimer) Write(base uint64, data []byte) error {
	return nil
}

func (a *ACPIPMTimer) IOPort() uint64 {
	return 0x608
}

func (a *ACPIPMTimer) Size() uint64 {
	return 0x4
}
