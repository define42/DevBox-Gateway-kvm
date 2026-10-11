package machine

import (
	"bytes"
	"testing"

	"github.com/define42/gokvm/internal/pci"
	"github.com/define42/gokvm/internal/serial"
	"github.com/define42/gokvm/kvm"
)

func TestDisabledSerial(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		init func(*Machine) error
	}{
		{
			name: "direct boot devices",
			init: func(m *Machine) error {
				if err := m.initSerial(); err != nil {
					return err
				}
				m.initIOPortHandlers()

				return nil
			},
		},
		{name: "UEFI devices", init: (*Machine).uefiDevices},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := &Machine{
				pci: pci.New(), mem: make([]byte, 2<<20),
				vcpuFds: []uintptr{0}, vmFd: ^uintptr(0),
			}
			m.DisableSerial()
			if err := tc.init(m); err != nil {
				t.Fatal(err)
			}
			if m.GetSerial() != nil {
				t.Fatal("disabled UART was instantiated")
			}
			if m.GetInputChan() != nil {
				t.Fatal("disabled UART has an input channel")
			}
			if err := m.InjectSerialIRQ(); err != nil {
				t.Fatalf("disabled UART attempted an IRQ on invalid VM fd: %v", err)
			}

			for port := uint64(serial.COM1Addr); port < serial.COM1Addr+8; port++ {
				for _, size := range []int{0, 1, 2, 4} {
					data := bytes.Repeat([]byte{0x5a}, size)
					read := m.ioportHandlers[port][kvm.EXITIOIN]
					write := m.ioportHandlers[port][kvm.EXITIOOUT]
					if read == nil || write == nil {
						t.Fatalf("COM1 port %#x has no handler", port)
					}
					if err := write(port, data); err != nil {
						t.Fatalf("write COM1 port %#x: %v", port, err)
					}
					if err := read(port, data); err != nil {
						t.Fatalf("read COM1 port %#x: %v", port, err)
					}
					if want := bytes.Repeat([]byte{0xff}, size); !bytes.Equal(data, want) {
						t.Fatalf("read COM1 port %#x: got %x, want %x", port, data, want)
					}
				}
			}
		})
	}
}

func TestSerialEnabledByDefault(t *testing.T) {
	t.Parallel()

	m := &Machine{pci: pci.New()}
	if err := m.initSerial(); err != nil {
		t.Fatal(err)
	}
	if m.GetSerial() == nil || m.GetInputChan() == nil {
		t.Fatal("default UART or input channel is absent")
	}
	m.initIOPortHandlers()
	var output bytes.Buffer
	m.GetSerial().SetOutput(&output)
	if err := m.ioportHandlers[serial.COM1Addr][kvm.EXITIOOUT](serial.COM1Addr, []byte{'x'}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "x" {
		t.Fatalf("UART output = %q, want x", output.String())
	}
	m.GetInputChan() <- 'y'
	data := []byte{0}
	if err := m.ioportHandlers[serial.COM1Addr][kvm.EXITIOIN](serial.COM1Addr, data); err != nil {
		t.Fatal(err)
	}
	if data[0] != 'y' {
		t.Fatalf("UART input = %q, want y", data[0])
	}
}

func TestUninitializedSerialQueries(t *testing.T) {
	t.Parallel()

	m := &Machine{vmFd: ^uintptr(0)}
	if m.GetSerial() != nil || m.GetInputChan() != nil {
		t.Fatal("uninitialized UART is present")
	}
	if err := m.InjectSerialIRQ(); err != nil {
		t.Fatalf("uninitialized UART attempted an IRQ: %v", err)
	}
}
