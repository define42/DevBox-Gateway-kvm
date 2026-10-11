package iodev

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestCMOSReportsConfiguredMemoryBelow4G(t *testing.T) {
	t.Parallel()

	cmos := NewCMOS(512*mib, 0)

	if got, want := binary.LittleEndian.Uint16(cmos.Data[0x34:0x36]), uint16(0x1f00); got != want {
		t.Fatalf("CMOS 0x34 memory = %#x, want %#x", got, want)
	}
}

func TestToBCD(t *testing.T) {
	t.Parallel()

	if got, want := toBCD(42), uint8(0x42); got != want {
		t.Fatalf("toBCD(42) = %#x, want %#x", got, want)
	}
}

func TestCMOSClockRegisters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		register byte
		bcd      byte
		binary   byte
	}{
		{name: "seconds", register: 0x00, bcd: 0x58, binary: 58},
		{name: "minutes", register: 0x02, bcd: 0x35, binary: 35},
		{name: "hours", register: 0x04, bcd: 0x14, binary: 14},
		{name: "weekday Sunday is one", register: 0x06, bcd: 1, binary: 1},
		{name: "day", register: 0x07, bcd: 0x11, binary: 11},
		{name: "month", register: 0x08, bcd: 0x10, binary: 10},
		{name: "year", register: 0x09, bcd: 0x26, binary: 26},
		{name: "century", register: 0x32, bcd: 0x20, binary: 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmos := NewCMOS(512*mib, 0)
			cmos.now = func() time.Time { return time.Date(2026, time.October, 11, 14, 35, 58, 0, time.UTC) }
			for _, mode := range []struct {
				control byte
				want    byte
			}{
				{control: 0x02, want: tc.bcd},
				{control: 0x06, want: tc.binary},
			} {
				writeCMOSRegister(t, cmos, 0x0b, mode.control)
				for _, mask := range []byte{0, 0x80} {
					if got := readCMOSRegister(t, cmos, tc.register|mask); got != mode.want {
						t.Fatalf("register %#x mode %#x = %#x, want %#x", tc.register|mask, mode.control, got, mode.want)
					}
				}
			}
		})
	}
}

func TestCMOS12HourClock(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		hour int
		want byte
	}{
		{name: "midnight", hour: 0, want: 0x12},
		{name: "morning", hour: 11, want: 0x11},
		{name: "noon", hour: 12, want: 0x92},
		{name: "afternoon", hour: 14, want: 0x82},
		{name: "night", hour: 23, want: 0x91},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmos := NewCMOS(512*mib, 0)
			cmos.now = func() time.Time { return time.Date(2026, time.October, 11, tc.hour, 0, 0, 0, time.UTC) }
			writeCMOSRegister(t, cmos, 0x0b, 0)
			if got := readCMOSRegister(t, cmos, 4); got != tc.want {
				t.Fatalf("hour %d encoded as %#x, want %#x", tc.hour, got, tc.want)
			}
		})
	}
}

func readCMOSRegister(t *testing.T, cmos *CMOS, index byte) byte {
	t.Helper()
	if err := cmos.Write(0x70, []byte{index}); err != nil {
		t.Fatal(err)
	}
	data := []byte{0}
	if err := cmos.Read(0x71, data); err != nil {
		t.Fatal(err)
	}

	return data[0]
}

func writeCMOSRegister(t *testing.T, cmos *CMOS, index, value byte) {
	t.Helper()
	if err := cmos.Write(0x70, []byte{index}); err != nil {
		t.Fatal(err)
	}
	if err := cmos.Write(0x71, []byte{value}); err != nil {
		t.Fatal(err)
	}
}
