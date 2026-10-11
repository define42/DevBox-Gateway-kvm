package machine

import (
	"encoding/binary"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/define42/gokvm/internal/iodev"
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

func TestRequestShutdownPowerButton(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		enabled bool
		acpi    bool
	}{
		{name: "before ACPI and interrupt enable"},
		{name: "before ACPI", enabled: true},
		{name: "before interrupt enable", acpi: true},
		{name: "guest ready", enabled: true, acpi: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var levels []bool
			power := &uefiPower{irq: func(high bool) error {
				levels = append(levels, high)

				return nil
			}}
			m := &Machine{uefiPower: power}
			if tc.enabled {
				if err := power.Write(0xb002, []byte{0, 1}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.acpi {
				if err := power.smi(0xb2, []byte{0xf1}); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.RequestShutdown(); err != nil {
				t.Fatal(err)
			}
			var status [2]byte
			if err := power.Read(0xb000, status[:]); err != nil {
				t.Fatal(err)
			}
			if binary.LittleEndian.Uint16(status[:]) != 1<<8 {
				t.Fatalf("power button status not latched: %x", status)
			}
			if got := len(levels) != 0; got != (tc.enabled && tc.acpi) {
				t.Fatalf("SCI before guest readiness = %v", levels)
			}
			if err := power.Write(0xb002, []byte{0, 1}); err != nil {
				t.Fatal(err)
			}
			if err := power.smi(0xb2, []byte{0xf1}); err != nil {
				t.Fatal(err)
			}
			if err := m.RequestShutdown(); err != nil {
				t.Fatal(err)
			}
			if err := power.Write(0xb000, []byte{0, 0}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(levels, []bool{true}) {
				t.Fatalf("repeated request or zero status write changed SCI: %v", levels)
			}
			if err := power.Write(0xb001, []byte{1}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(levels, []bool{true, false}) {
				t.Fatalf("power-button acknowledgement did not clear SCI: %v", levels)
			}
		})
	}
}

func TestPowerButtonDisableAndReenableSCI(t *testing.T) {
	t.Parallel()

	var levels []bool
	power := &uefiPower{status: 1 << 8, enable: 1 << 8, irq: func(high bool) error {
		levels = append(levels, high)

		return nil
	}}
	for _, command := range []byte{0xf1, 0xf0, 0xf1} {
		if err := power.smi(0xb2, []byte{command}); err != nil {
			t.Fatal(err)
		}
	}
	if err := power.Write(0xb003, []byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := power.Write(0xb003, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(levels, []bool{true, false, true, false, true}) {
		t.Fatalf("ACPI mode or event enable failed to update SCI: %v", levels)
	}
}

func TestPowerButtonIRQFailureCanRetry(t *testing.T) {
	t.Parallel()

	fail := true
	power := &uefiPower{enable: 1 << 8, control: 1, irq: func(bool) error {
		if fail {
			return io.ErrClosedPipe
		}

		return nil
	}}
	m := &Machine{uefiPower: power}
	if err := m.RequestShutdown(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("lost SCI assert error: %v", err)
	}
	fail = false
	if err := m.RequestShutdown(); err != nil || !power.sciHigh {
		t.Fatalf("failed to retry SCI assertion: %v", err)
	}
	fail = true
	if err := power.Write(0xb001, []byte{1}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("lost SCI deassert error: %v", err)
	}
	fail = false
	if err := power.Write(0xb001, []byte{1}); err != nil || power.sciHigh {
		t.Fatalf("failed to retry SCI deassertion: %v", err)
	}
}

func TestPowerButtonSharesNetworkIRQ(t *testing.T) {
	t.Parallel()

	var levels []bool
	line := &sharedIRQLine{setLevel: func(high bool) error {
		levels = append(levels, high)

		return nil
	}}
	m := &Machine{pciIRQLines: map[uint8]*sharedIRQLine{9: line}}
	m.uefiPower = &uefiPower{control: 1, enable: 1 << 8, irq: m.sharedIRQSource(9)}
	network := m.sharedIRQSource(9)
	if err := network(true); err != nil {
		t.Fatal(err)
	}
	if err := m.RequestShutdown(); err != nil {
		t.Fatal(err)
	}
	if err := m.uefiPower.Write(0xb001, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(levels, []bool{true}) {
		t.Fatalf("ACPI acknowledgement cleared an active network interrupt: %v", levels)
	}
	if err := network(false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(levels, []bool{true, false}) {
		t.Fatalf("GSI remained asserted after both sources acknowledged: %v", levels)
	}
}

func TestRequestShutdownUnsupportedOrStopped(t *testing.T) {
	t.Parallel()

	m := &Machine{}
	if err := m.RequestShutdown(); !errors.Is(err, ErrShutdownUnsupported) {
		t.Fatalf("non-UEFI shutdown error = %v", err)
	}
	m.uefiPower = &uefiPower{irq: func(bool) error {
		t.Error("stopped machine attempted IRQ injection")

		return nil
	}}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := m.RequestShutdown(); !errors.Is(err, ErrMachineStopped) {
		t.Fatalf("stopped-machine shutdown error = %v", err)
	}
}

func TestUEFIFADTFixedPowerButton(t *testing.T) {
	t.Parallel()

	fadt := uefiFADT()
	flags := binary.LittleEndian.Uint32(fadt[112:])
	if flags&(1<<4) != 0 {
		t.Fatal("FADT advertises a control-method power button instead of fixed registers")
	}
	if flags&(1<<5) == 0 {
		t.Fatal("FADT advertises an unimplemented fixed sleep button")
	}
	if binary.LittleEndian.Uint16(fadt[46:]) != 9 {
		t.Fatal("FADT SCI does not use shared GSI 9")
	}
}

func TestPowerButtonAckAfterFailedAssertion(t *testing.T) {
	t.Parallel()

	fail := true
	var levels []bool
	line := &sharedIRQLine{setLevel: func(high bool) error {
		if fail {
			return io.ErrClosedPipe
		}
		levels = append(levels, high)

		return nil
	}}
	m := &Machine{pciIRQLines: map[uint8]*sharedIRQLine{9: line}}
	m.uefiPower = &uefiPower{control: 1, enable: 1 << 8, irq: m.sharedIRQSource(9)}
	if err := m.RequestShutdown(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("SCI injection error = %v", err)
	}
	// The guest may acknowledge a latched status while polling, even if
	// injection failed. Clear the shared source's intent as well as PM1_STS.
	if err := m.uefiPower.Write(0xb001, []byte{1}); err != nil {
		t.Fatal(err)
	}
	fail = false
	network := m.sharedIRQSource(9)
	if err := network(true); err != nil {
		t.Fatal(err)
	}
	if err := network(false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(levels, []bool{true, false}) {
		t.Fatalf("failed ACPI assertion left a stale shared IRQ source: %v", levels)
	}
}
