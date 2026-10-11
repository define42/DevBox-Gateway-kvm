package machine

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/define42/gokvm/iodev"
	"github.com/define42/gokvm/kvm"
	"github.com/define42/gokvm/pci"
	"github.com/define42/gokvm/serial"
)

func TestAbsentLegacyIOPorts(t *testing.T) {
	t.Parallel()

	m := &Machine{pci: pci.New(), serial: &serial.Serial{}}
	m.initIOPortHandlers()

	// Slax's built-in advansys driver probes these ISA/VLB base addresses.
	// Its signature byte is at base+1; the signature word is at base.
	for _, base := range []uint64{0x120, 0x130, 0x140, 0x150, 0x190, 0x210, 0x230, 0x250, 0x330} {
		t.Run(fmt.Sprintf("%#x", base), func(t *testing.T) {
			t.Parallel()

			for _, port := range []uint64{base, base + 1} {
				for _, size := range []int{1, 2, 4} {
					data := bytes.Repeat([]byte{0x25}, size)
					if err := m.ioportHandlers[port][kvm.EXITIOOUT](port, data); err != nil {
						t.Fatalf("write port %#x: %v", port, err)
					}
					if err := m.ioportHandlers[port][kvm.EXITIOIN](port, data); err != nil {
						t.Fatalf("read port %#x: %v", port, err)
					}
					if want := bytes.Repeat([]byte{0xff}, size); !bytes.Equal(data, want) {
						t.Fatalf("read port %#x: got %x, want %x", port, data, want)
					}
				}
			}
		})
	}
}

func TestLegacyIOPortOverrides(t *testing.T) {
	t.Parallel()

	m := &Machine{pci: pci.New(), serial: &serial.Serial{IER: 0x5a}}
	m.AddDevice(&iodev.Noop{Port: 0x180, Psize: 1})
	m.initIOPortHandlers()

	for _, tc := range []struct {
		port uint64
		want byte
	}{
		{0x17f, 0xff},
		{0x180, 0x25}, // Explicit device overrides the absent-device fallback.
		{0x3c0, 0x25}, // VGA retains its dedicated handler.
		{serial.COM1Addr + 1, 0x5a},
	} {
		data := []byte{0x25}
		if err := m.ioportHandlers[tc.port][kvm.EXITIOIN](tc.port, data); err != nil {
			t.Fatalf("read port %#x: %v", tc.port, err)
		}
		if data[0] != tc.want {
			t.Errorf("read port %#x: got %#x, want %#x", tc.port, data[0], tc.want)
		}
	}

	if err := m.ioportHandlers[0xcf9][kvm.EXITIOOUT](0xcf9, []byte{0xe}); !errors.Is(err, ErrWriteToCF9) {
		t.Errorf("reset port: got %v, want %v", err, ErrWriteToCF9)
	}
	if err := m.ioportHandlers[0x5000][kvm.EXITIOIN](0x5000, []byte{0}); !errors.Is(err, kvm.ErrUnexpectedExitReason) {
		t.Errorf("unknown port: got %v, want %v", err, kvm.ErrUnexpectedExitReason)
	}
}

func TestUEFIAbsentVMwarePorts(t *testing.T) {
	t.Parallel()
	m := &Machine{pci: pci.New(), mem: make([]byte, 2<<20), vcpuFds: []uintptr{0}}
	if err := m.uefiDevices(); err != nil {
		t.Fatal(err)
	}
	for _, port := range []uint64{0x5658, 0x5659} {
		data := []byte{0x68, 0x58, 0x4d, 0x56}
		if err := m.ioportHandlers[port][kvm.EXITIOOUT](port, data); err != nil {
			t.Fatalf("VMware probe write: %v", err)
		}
		if err := m.ioportHandlers[port][kvm.EXITIOIN](port, data); err != nil {
			t.Fatalf("VMware probe read: %v", err)
		}
		if !bytes.Equal(data, []byte{0xff, 0xff, 0xff, 0xff}) {
			t.Fatalf("VMware probe returned %x, want absent device", data)
		}
	}
}
