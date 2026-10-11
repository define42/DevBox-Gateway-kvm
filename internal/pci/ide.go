package pci

type ideController struct{}

func (d ideController) GetDeviceHeader() DeviceHeader {
	return DeviceHeader{
		DeviceID:      0x7010, // Intel 82371SB PIIX3 IDE
		VendorID:      0x8086,
		ClassCode:     0x01, // Mass storage controller
		Subclass:      0x01, // IDE controller
		ProgIF:        0x00, // Primary/secondary channels in legacy compatibility mode.
		HeaderType:    0,
		Command:       0x1, // I/O space enable.
		InterruptLine: 14,
		InterruptPin:  1,
	}
}

func (d ideController) Read(port uint64, bytes []byte) error {
	return ErrIONotPermit
}

func (d ideController) Write(port uint64, bytes []byte) error {
	return ErrIONotPermit
}

func (d ideController) IOPort() uint64 {
	return 0
}

func (d ideController) Size() uint64 {
	return 0
}

func NewIDEController() Device {
	return &ideController{}
}
