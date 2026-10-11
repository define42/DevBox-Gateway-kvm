package machine

import (
	"errors"
	"io"
	"slices"
	"sync"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

func TestSharedIRQLineAcknowledgesEachSource(t *testing.T) {
	t.Parallel()

	levels := []bool{}
	line := &sharedIRQLine{setLevel: func(high bool) error {
		levels = append(levels, high)

		return nil
	}}
	first, second := line.source(), line.source()
	for _, source := range []func(bool) error{first, first, second} {
		if err := source(true); err != nil {
			t.Fatal(err)
		}
	}
	if err := first(false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(levels, []bool{true}) {
		t.Fatalf("acknowledging first source cleared the shared line: %v", levels)
	}
	if err := second(false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(levels, []bool{true, false}) {
		t.Fatalf("last acknowledgement did not deassert INTx: %v", levels)
	}
}

func TestSharedIRQLineRetriesFailedAssertion(t *testing.T) {
	t.Parallel()

	var calls int
	line := &sharedIRQLine{setLevel: func(bool) error {
		calls++
		if calls == 1 {
			return io.ErrUnexpectedEOF
		}

		return nil
	}}
	source := line.source()
	if err := source(true); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("assertion error = %v", err)
	}
	if err := source(true); err != nil {
		t.Fatal(err)
	}
	if err := source(false); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || line.asserted != 0 || line.high {
		t.Fatalf("failed assertion corrupted line state: calls=%d asserted=%d high=%v", calls, line.asserted, line.high)
	}
}

func TestSharedIRQLineConcurrentSources(t *testing.T) {
	t.Parallel()

	var high bool
	line := &sharedIRQLine{setLevel: func(level bool) error {
		high = level

		return nil
	}}
	var workers sync.WaitGroup
	for range 4 {
		source := line.source()
		workers.Go(func() {
			for range 1000 {
				if err := source(true); err != nil {
					t.Error(err)
				}
				if err := source(false); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
	if high || line.asserted != 0 {
		t.Fatalf("concurrent sources left line pending: high=%v asserted=%d", high, line.asserted)
	}
}

func TestMachineConnectsUEFISharedIRQ(t *testing.T) {
	t.Parallel()

	levels := []bool{}
	line := &sharedIRQLine{setLevel: func(high bool) error {
		levels = append(levels, high)

		return nil
	}}
	m := &Machine{uefiIRQ: true, pciIRQLines: map[uint8]*sharedIRQLine{10: line}}
	first := virtio.NewInputKeyboard(10, func() error {
		t.Error("old injection used")

		return nil
	}, nil)
	second := virtio.NewInputKeyboard(10, func() error {
		t.Error("old injection used")

		return nil
	}, nil)
	m.connectVirtioIRQ(first.ModernTransport, 10)
	m.connectVirtioIRQ(second.ModernTransport, 10)
	if err := first.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := second.Interrupt(); err != nil {
		t.Fatal(err)
	}
	first.MMIO(0x1000, make([]byte, 1), false)
	if !slices.Equal(levels, []bool{true}) {
		t.Fatalf("first acknowledgement cleared another device: %v", levels)
	}
	second.MMIO(0x1000, make([]byte, 1), false)
	if !slices.Equal(levels, []bool{true, false}) {
		t.Fatalf("shared line did not clear: %v", levels)
	}
}

func TestMachinePreservesDirectBootIRQ(t *testing.T) {
	t.Parallel()

	m := &Machine{}
	var injected int
	device := virtio.NewInputKeyboard(10, func() error {
		injected++

		return nil
	}, nil)
	m.connectVirtioIRQ(device.ModernTransport, 10)
	if err := device.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if injected != 1 || len(m.pciIRQLines) != 0 {
		t.Fatal("direct boot interrupt routing was replaced")
	}
}
