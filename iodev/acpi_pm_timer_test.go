package iodev

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestACPIPMTimerTicks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		want    uint32
	}{
		{name: "start", elapsed: 0, want: 0},
		{name: "one second", elapsed: time.Second, want: 3_579_545},
		{name: "fractional second", elapsed: time.Second / 2, want: 1_789_772},
		{name: "beyond former 43 minute overflow", elapsed: 45 * time.Minute, want: uint32(9_664_771_500 % (1 << 32))},
		{name: "one day", elapsed: 24 * time.Hour, want: uint32(309_272_688_000 % (1 << 32))},
		{name: "future start", elapsed: -time.Second, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := pmTimerTicks(tc.elapsed); got != tc.want {
				t.Fatalf("ticks after %s = %d, want %d", tc.elapsed, got, tc.want)
			}
		})
	}
}

func TestACPIPMTimerRead(t *testing.T) {
	t.Parallel()

	timer := NewACPIPMTimer()
	timer.Start = time.Now().Add(-time.Second)
	before := pmTimerTicks(time.Since(timer.Start))
	data := make([]byte, 4)
	if err := timer.Read(0x608, data); err != nil {
		t.Fatal(err)
	}
	after := pmTimerTicks(time.Since(timer.Start))
	if got := binary.LittleEndian.Uint32(data); got < before || got > after {
		t.Fatalf("counter %d outside measured interval [%d,%d]", got, before, after)
	}
	if err := timer.Read(0x608, make([]byte, 2)); err == nil {
		t.Fatal("short timer read accepted")
	}
}
