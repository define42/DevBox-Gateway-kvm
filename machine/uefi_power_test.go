package machine

import (
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/define42/gokvm/iodev"
)

func TestUEFIPowerACPIMode(t *testing.T) {
	t.Parallel()

	power := &uefiPower{timer: iodev.NewACPIPMTimer()}
	if power.IOPort() != 0xb000 || power.Size() != 0x40 {
		t.Fatalf("wrong PM register window %#x + %#x", power.IOPort(), power.Size())
	}
	for _, tc := range []struct {
		command byte
		want    uint16
	}{
		{command: 0xf1, want: 1},
		{command: 0x00, want: 1},
		{command: 0xf0, want: 0},
	} {
		if err := power.smi(0xb2, []byte{tc.command}); err != nil {
			t.Fatal(err)
		}
		data := make([]byte, 2)
		if err := power.Read(0xb004, data); err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint16(data); got != tc.want {
			t.Fatalf("SMI %#x: PM1 control %#x, want %#x", tc.command, got, tc.want)
		}
	}
}

func TestUEFIPowerShutdown(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		port uint64
		data []byte
		stop bool
	}{
		{name: "OVMF shutdown", port: 0xb004, data: []byte{0, 0x20}, stop: true},
		{name: "OS shutdown in ACPI mode", port: 0xb004, data: []byte{1, 0x20}, stop: true},
		{name: "byte high register", port: 0xb005, data: []byte{0x20}, stop: true},
		{name: "without sleep enable", port: 0xb004, data: []byte{1, 0}, stop: false},
		{name: "unsupported sleep state", port: 0xb004, data: []byte{0, 0x24}, stop: false},
		{name: "event register write", port: 0xb000, data: []byte{0, 0x20}, stop: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			power := &uefiPower{timer: iodev.NewACPIPMTimer()}
			var stopped bool
			power.stop = func() error {
				stopped = true
				// The callback must run outside the mutex: shutdown may inspect registers.

				return power.Read(0xb004, make([]byte, 2))
			}
			if err := power.Write(tc.port, tc.data); err != nil {
				t.Fatal(err)
			}
			if stopped != tc.stop {
				t.Fatalf("shutdown called = %v, want %v", stopped, tc.stop)
			}
			data := make([]byte, 2)
			if err := power.Read(0xb004, data); err != nil {
				t.Fatal(err)
			}
			if binary.LittleEndian.Uint16(data)&(1<<13) != 0 {
				t.Fatal("write-only SLP_EN trigger remained set on readback")
			}
		})
	}
}

func TestUEFIPowerPropagatesStopError(t *testing.T) {
	t.Parallel()

	want := io.ErrClosedPipe
	power := &uefiPower{stop: func() error { return want }}
	if err := power.Write(0xb004, []byte{0, 0x20}); !errors.Is(err, want) {
		t.Fatalf("shutdown error = %v, want %v", err, want)
	}
}

func TestUEFIPowerTimer(t *testing.T) {
	t.Parallel()

	timer := iodev.NewACPIPMTimer()
	timer.Start = time.Now().Add(-time.Second)
	power := &uefiPower{timer: timer}
	before := make([]byte, 4)
	if err := timer.Read(0x608, before); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4)
	if err := power.Read(0xb008, data); err != nil {
		t.Fatal(err)
	}
	after := make([]byte, 4)
	if err := timer.Read(0x608, after); err != nil {
		t.Fatal(err)
	}
	low, high := binary.LittleEndian.Uint32(before), binary.LittleEndian.Uint32(after)
	if got := binary.LittleEndian.Uint32(data); got < low || got > high {
		t.Fatalf("relocated PM timer %d outside [%d,%d]", got, low, high)
	}
}

func TestUEFIPowerEventRegisterReadback(t *testing.T) {
	t.Parallel()

	power := &uefiPower{status: 0x0521}
	if err := power.Write(0xb002, []byte{0x20, 0x04}); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4)
	if err := power.Read(0xb000, data); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(data[2:]); got != 0x0420 {
		t.Fatalf("PM1_EN = %#x, want GlobalLock/RTC enabled", got)
	}
	if got := binary.LittleEndian.Uint16(data); got != 0x0521 {
		t.Fatalf("PM1_EN write changed PM1_STS to %#x", got)
	}
	if err := power.Write(0xb001, []byte{0x01}); err != nil {
		t.Fatal(err)
	}
	if err := power.Write(0xb000, []byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := power.Read(0xb000, data); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(data); got != 0x0421 {
		t.Fatalf("PM1_STS W1C = %#x, want %#x", got, 0x0421)
	}
	if err := power.Write(0xb000, []byte{0xff, 0xff, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := power.Read(0xb000, data); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(data); got != 0 {
		t.Fatalf("cleared PM1 event registers = %#x", got)
	}
}
