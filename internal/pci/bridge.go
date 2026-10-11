package pci

import "errors"

var ErrIONotPermit = errors.New("IO is not permitted for PCI bridge")

type bridge struct{}

func (br bridge) GetDeviceHeader() DeviceHeader {
	return DeviceHeader{
		DeviceID:   0x1237,
		VendorID:   0x8086,
		ClassCode:  0x06, // Bridge device
		Subclass:   0x00, // Host bridge
		HeaderType: 0,
		// SeaBIOS uses the northbridge subsystem IDs to detect QEMU-style
		// PC hardware during early platform setup.
		SubsystemVendorID: 0x1af4,
		SubsystemID:       0x1100,
		InterruptLine:     0,
		InterruptPin:      0,
		BAR:               [6]uint32{},
		Command:           0,
	}
}

func (br bridge) Read(port uint64, bytes []byte) error {
	return ErrIONotPermit
}

func (br bridge) Write(port uint64, bytes []byte) error {
	return ErrIONotPermit
}

func (br bridge) IOPort() uint64 {
	return 0
}

func (br bridge) Size() uint64 {
	return 0
}

func NewBridge() Device {
	return &bridge{}
}
