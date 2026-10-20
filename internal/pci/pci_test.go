package pci_test

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
)

func TestSizeToBits(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		input    uint64
		expected uint32
	}{
		{
			name:     "Success",
			input:    0x100,
			expected: 0xffffff00,
		},
		{
			name:     "Fail",
			input:    0x0,
			expected: 0x0,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.expected != pci.SizeToBits(tt.input) {
				t.Fatalf("expected: %v, actual: %v", tt.expected, tt.input)
			}
		})
	}
}

func TestBytesToNum(t *testing.T) {
	t.Parallel()

	expected := uint64(0x12345678)
	actual := pci.BytesToNum([]byte{0x78, 0x56, 0x34, 0x12})

	if expected != actual {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}
}

func TestNumToBytes8(t *testing.T) {
	t.Parallel()

	expected := []byte{0x12}
	actual := pci.NumToBytes(uint8(0x12))

	if !bytes.Equal(actual, expected) {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}
}

func TestNumToBytes16(t *testing.T) {
	t.Parallel()

	expected := []byte{0x34, 0x12}
	actual := pci.NumToBytes(uint16(0x1234))

	if !bytes.Equal(actual, expected) {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}
}

func TestNumToBytes32(t *testing.T) {
	t.Parallel()

	expected := []byte{0x78, 0x56, 0x34, 0x12}
	actual := pci.NumToBytes(uint32(0x12345678))

	if !bytes.Equal(actual, expected) {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}
}

func TestNumToBytes64(t *testing.T) {
	t.Parallel()

	expected := []byte{0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12}
	actual := pci.NumToBytes(uint64(0x1234567812345678))

	if !bytes.Equal(actual, expected) {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}
}

func TestNumToBytesInvalid(t *testing.T) {
	t.Parallel()

	actual := pci.NumToBytes(-1)
	expected := []byte{}

	if !bytes.Equal(actual, expected) {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}
}

func TestProbingBAR0(t *testing.T) {
	t.Parallel()

	br := pci.NewBridge()
	expected := pci.SizeToBits(br.Size())

	p := pci.New(br)
	_ = p.PciConfAddrOut(0x0, pci.NumToBytes(uint32(0x80000010)))   // offset 0x10 for BAR0 with enable bit 0x80
	_ = p.PciConfDataOut(0xCFC, pci.NumToBytes(uint32(0xffffffff))) // all 1-bits for probing size of BAR0
	_ = p.PciConfAddrIn(0xCF8, pci.NumToBytes(uint32(0x80000010)))  // random call to PciConfAddrIn

	bytes := make([]byte, 4)
	_ = p.PciConfDataIn(0xCFC, bytes)
	actual := uint32(pci.BytesToNum(bytes))

	if expected != actual {
		t.Fatalf("expected: 0x%x, actual: 0x%x", expected, actual)
	}
}

func TestPciConfDataOutNonBARWriteReadsBack(t *testing.T) {
	t.Parallel()

	p := pci.New(pci.NewBridge())

	_ = p.PciConfAddrOut(0x0, pci.NumToBytes(uint32(0x80000058)))
	_ = p.PciConfDataOut(0xcfd, []byte{0x30, 0x33, 0x33})

	data := make([]byte, 4)
	if err := p.PciConfDataIn(0xcfc, data); err != nil {
		t.Fatal(err)
	}

	if got, want := data, []byte{0x00, 0x30, 0x33, 0x33}; !bytes.Equal(got, want) {
		t.Fatalf("config readback: got %#v, want %#v", got, want)
	}
}

func TestBytes(t *testing.T) {
	t.Parallel()

	dh := pci.DeviceHeader{
		DeviceID:      1,
		VendorID:      1,
		HeaderType:    1,
		SubsystemID:   1,
		Command:       1,
		BAR:           [6]uint32{},
		InterruptPin:  1,
		InterruptLine: 1,
	}

	b, err := dh.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	if b[0] != byte(dh.VendorID) {
		t.Fatalf("invalid vendor id")
	}
}

func TestPciConfAddrInOut(t *testing.T) {
	t.Parallel()

	p := pci.New(pci.NewBridge())

	for _, tt := range []struct {
		name string
		port uint64
		data []byte
		exp  error
	}{
		{
			name: "Success",
			port: 0x0,
			data: make([]byte, 4),
			exp:  nil,
		},
		{
			name: "Fail_DataLength",
			port: 0x0,
			data: make([]byte, 3),
			exp:  nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := p.PciConfAddrIn(tt.port, tt.data); !errors.Is(err, tt.exp) {
				t.Fatalf("%s failed: %v", tt.name, err)
			}

			if err := p.PciConfAddrOut(tt.port, tt.data); !errors.Is(err, tt.exp) {
				t.Fatalf("%s failed: %v", tt.name, err)
			}
		})
	}
}

func TestPciConfDataInOut(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		pci  *pci.PCI
		port uint64
		data []byte
		exp  error
	}{
		{
			name: "Success_1",
			pci:  pci.New(),
			port: 0xCFC,
			data: make([]byte, 4),
			exp:  nil,
		},
		{
			name: "Success_2",
			pci:  &pci.PCI{},
			port: 0xCFC,
			data: make([]byte, 4),
			exp:  nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.pci.PciConfDataIn(tt.port, tt.data); !errors.Is(err, tt.exp) {
				t.Fatalf("%s failed: %v", tt.name, err)
			}

			if err := tt.pci.PciConfDataOut(tt.port, tt.data); !errors.Is(err, tt.exp) {
				t.Fatalf("%s failed: %v", tt.name, err)
			}
		})
	}
}

func TestPciConfDataInAbsentDeviceReturnsAllOnes(t *testing.T) {
	t.Parallel()

	p := pci.New(pci.NewBridge())
	_ = p.PciConfAddrOut(0x0, pci.NumToBytes(uint32(0x80000800))) // bus 0, slot 1, function 0

	data := make([]byte, 4)
	if err := p.PciConfDataIn(0xCFC, data); err != nil {
		t.Fatal(err)
	}

	if got, want := pci.BytesToNum(data), uint64(0xffffffff); got != want {
		t.Fatalf("absent config read: got %#x, want %#x", got, want)
	}
}

func TestPciConfDataOutPartialBARWriteUpdatesMMIODecode(t *testing.T) {
	t.Parallel()

	dev := &testMMIODevice{}
	p := pci.New(dev)

	_ = p.PciConfAddrOut(0x0, pci.NumToBytes(uint32(0x80000010)))
	_ = p.PciConfDataOut(0xcfc, []byte{0x00, 0x00})
	_ = p.PciConfDataOut(0xcfe, []byte{0x34, 0x12})

	data := make([]byte, 4)
	if err := p.PciConfDataIn(0xcfc, data); err != nil {
		t.Fatal(err)
	}

	if got, want := pci.BytesToNum(data), uint64(0x12340000); got != want {
		t.Fatalf("BAR readback: got %#x, want %#x", got, want)
	}

	if _, off, ok := p.LookupMMIO(0x12340020); !ok || off != 0x20 {
		t.Fatalf("LookupMMIO: ok=%v off=%#x, want true/0x20", ok, off)
	}
}

type testMMIODevice struct{}

func (d *testMMIODevice) GetDeviceHeader() pci.DeviceHeader {
	return pci.DeviceHeader{
		VendorID:            0x1af4,
		DeviceID:            0x1042,
		Status:              0x10,
		CapabilitiesPointer: 0x40,
		BAR:                 [6]uint32{0xd0000000},
	}
}

func (d *testMMIODevice) Read(uint64, []byte) error { return nil }

func (d *testMMIODevice) Write(uint64, []byte) error { return nil }

func (d *testMMIODevice) IOPort() uint64 { return 0 }

func (d *testMMIODevice) Size() uint64 { return 0 }

func (d *testMMIODevice) Capabilities() []byte { return nil }

func (d *testMMIODevice) MMIOBARIndex() int { return 0 }

func (d *testMMIODevice) MMIOSize() uint64 { return 0x1000 }

func (d *testMMIODevice) MMIO(uint64, []byte, bool) {}

func TestPCIConcurrentBARProgrammingAndMMIO(t *testing.T) {
	t.Parallel()

	p := pci.New()
	dev := &reentrantMMIODevice{bus: p}
	p.AddDevices(dev)
	_ = p.PciConfAddrOut(0xcf8, pci.NumToBytes(uint32(0x80000010)))
	bases := [2]uint32{0xd0000000, 0xd1000000}
	var workers sync.WaitGroup
	workers.Go(func() {
		for i := range 1000 {
			_ = p.PciConfDataOut(0xcfc, pci.NumToBytes(bases[i%len(bases)]))
		}
	})
	workers.Go(func() {
		var value [4]byte
		for range 1000 {
			_ = p.PciConfDataIn(0xcfc, value[:])
			got := uint32(pci.BytesToNum(value[:]))
			if got != bases[0] && got != bases[1] {
				t.Errorf("torn BAR read: %#x", got)

				return
			}
		}
	})
	for range 2 {
		workers.Go(func() {
			for range 1000 {
				for _, base := range bases {
					device, off, ok := p.LookupMMIO(uint64(base) + 0x20)
					if !ok {
						continue
					}
					if device != dev || off != 0x20 {
						t.Errorf("MMIO dispatch: device=%v offset=%#x", device, off)

						return
					}
					device.MMIO(off, []byte{1}, true)
				}
			}
		})
	}
	workers.Wait()
	if dev.accesses.Load() == 0 {
		t.Fatal("no MMIO access reached the device")
	}

	_ = p.PciConfDataOut(0xcfc, pci.NumToBytes(bases[1]))
	if _, off, ok := p.LookupMMIO(uint64(bases[1]) + 0x20); !ok || off != 0x20 {
		t.Fatalf("final BAR mapping: found=%v offset=%#x", ok, off)
	}
	if _, _, ok := p.LookupMMIO(uint64(bases[0]) + 0x20); ok {
		t.Fatal("old BAR mapping still decodes")
	}
}

func TestPCIConcurrentConfigurationAndRegistration(t *testing.T) {
	t.Parallel()

	p := pci.New(&testMMIODevice{})
	_ = p.PciConfAddrOut(0xcf8, pci.NumToBytes(uint32(0x80000058)))
	var workers sync.WaitGroup
	workers.Go(func() {
		for i := range 1000 {
			_ = p.PciConfAddrOut(0xcf8, pci.NumToBytes(uint32(0x80000058+4*(i%2))))
		}
	})
	workers.Go(func() {
		for i := range 1000 {
			_ = p.PciConfDataOut(0xcfc, pci.NumToBytes(uint32(i)))
		}
	})
	workers.Go(func() {
		var value [4]byte
		for range 1000 {
			_ = p.PciConfAddrIn(0xcf8, value[:])
			_ = p.PciConfDataIn(0xcfc, value[:])
			if _, _, ok := p.LookupMMIO(0xd0000020); !ok {
				t.Error("registration lost an existing MMIO mapping")

				return
			}
			devices := p.Devices()
			devices[0] = nil // The returned slice must not alias the registry.
		}
	})
	workers.Go(func() {
		for range 31 {
			p.AddDevices(pci.NewBridge())
		}
	})
	workers.Wait()
	if devices := p.Devices(); len(devices) != 32 || devices[0] == nil {
		t.Fatalf("device registry corrupted: %v", devices)
	}
	_ = p.PciConfAddrOut(0xcf8, pci.NumToBytes(uint32(0x80000058)))
	_ = p.PciConfDataOut(0xcfc, []byte{1, 2, 3, 4})
	var value [4]byte
	_ = p.PciConfDataIn(0xcfc, value[:])
	if value != [4]byte{1, 2, 3, 4} {
		t.Fatalf("configuration override lost: %v", value)
	}
}

func TestPCIRegistrationCallbacksCanReadBus(t *testing.T) {
	t.Parallel()

	p := pci.New()
	done := make(chan struct{})
	go func() {
		p.AddDevices(&reentrantMMIODevice{bus: p})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("registration held the bus lock during a device callback")
	}
}

type reentrantMMIODevice struct {
	testMMIODevice
	bus      *pci.PCI
	accesses atomic.Uint32
}

func (d *reentrantMMIODevice) GetDeviceHeader() pci.DeviceHeader {
	_ = d.bus.Devices()

	return d.testMMIODevice.GetDeviceHeader()
}

func (d *reentrantMMIODevice) Capabilities() []byte {
	_ = d.bus.Devices()

	return nil
}

func (d *reentrantMMIODevice) MMIO(_ uint64, _ []byte, _ bool) {
	var value [4]byte
	_ = d.bus.PciConfAddrIn(0xcf8, value[:])
	d.accesses.Add(1)
}

func TestPCISparseFunctionsKeepIndependentBARs(t *testing.T) {
	t.Parallel()

	p := pci.New(pci.NewBridge())
	first, second := &testMMIODevice{}, &testMMIODevice{}
	if err := p.AddDeviceAt(5, 3, second); err != nil {
		t.Fatal(err)
	}
	if err := p.AddDeviceAt(5, 0, first); err != nil {
		t.Fatal(err)
	}
	p.AddDevices(pci.NewBridge())
	if got := readPCIConfig(t, p, 5, 0, 0x0c); got&0x00800000 == 0 {
		t.Fatal("late function zero does not advertise multiple functions")
	}
	if got := readPCIConfig(t, p, 6, 0, 0); got != 0x12378086 {
		t.Fatalf("sequential registration did not continue after explicit slot: %#x", got)
	}
	barOffset := 0x10 + 4*first.MMIOBARIndex()
	writePCIConfig(t, p, 5, 0, barOffset, 0xd1000000)
	writePCIConfig(t, p, 5, 3, barOffset, 0xd2000000)
	for _, tc := range []struct {
		name string
		addr uint64
		dev  pci.CapsAndMMIO
	}{
		{name: "function zero", addr: 0xd1000020, dev: first},
		{name: "function three", addr: 0xd2000020, dev: second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev, offset, found := p.LookupMMIO(tc.addr)
			if !found || dev != tc.dev || offset != 0x20 {
				t.Fatalf("MMIO lookup: device=%v offset=%#x found=%v", dev, offset, found)
			}
		})
	}
	writePCIConfig(t, p, 5, 3, barOffset, 0xffffffff)
	if got := readPCIConfig(t, p, 5, 0, barOffset); got != 0xd1000000 {
		t.Fatalf("function-three BAR probe altered function zero: %#x", got)
	}
	if got := readPCIConfig(t, p, 5, 3, barOffset); got != pci.SizeToBits(second.MMIOSize()) {
		t.Fatalf("wrong function-three BAR probe result: %#x", got)
	}
}

func TestPCIAddDeviceAtValidation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		slot     int
		function int
		dev      pci.Device
		want     error
	}{
		{name: "negative slot", slot: -1, dev: pci.NewBridge(), want: pci.ErrDeviceAddress},
		{name: "large slot", slot: 32, dev: pci.NewBridge(), want: pci.ErrDeviceAddress},
		{name: "negative function", function: -1, dev: pci.NewBridge(), want: pci.ErrDeviceAddress},
		{name: "large function", function: 8, dev: pci.NewBridge(), want: pci.ErrDeviceAddress},
		{name: "nil device", want: pci.ErrNilDevice},
		{name: "occupied function", dev: pci.NewBridge(), want: pci.ErrDeviceOccupied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := pci.New(pci.NewBridge())
			if err := p.AddDeviceAt(tc.slot, tc.function, tc.dev); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if len(p.Devices()) != 1 {
				t.Fatal("failed registration changed the registry")
			}
		})
	}
}
