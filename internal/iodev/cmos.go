package iodev

import (
	"sync"
	"time"
)

const (
	indexMask   = uint8(0x7F)
	indexOffset = uint64(0x70)
	dataOffset  = uint64(0x71)
	dataLen     = uint64(128)

	kib = 1024
	mib = 1024 * kib
)

type CMOS struct {
	mu    sync.Mutex
	now   func() time.Time
	Index uint8
	Data  []uint8
}

func NewCMOS(memBelow4G, memAbove4G uint64) *CMOS {
	cmos := &CMOS{
		now:   time.Now,
		Index: 0,
		Data:  make([]uint8, dataLen),
	}

	// RTC register B: BCD values, 24-hour clock, interrupts disabled.
	cmos.Data[0x0b] = 0x02
	cmos.setUint16(0x15, 640) // Conventional memory in KiB.
	extendedKiB := saturatingSub(memBelow4G, 1*mib) / kib
	cmos.setUint16(0x17, uint16(clampMax(extendedKiB, 0xffff)))
	cmos.setUint16(0x30, uint16(clampMax(extendedKiB, 0xffff)))

	// Extended memory above 16MiB in 64KiB chunks. SeaBIOS uses this to
	// decide where top-of-RAM scratch space lives.
	extMem64K := uint16(clampMax(saturatingSub(memBelow4G, 16*mib)/(64*kib), 0xffff))
	cmos.setUint16(0x34, extMem64K)

	above4G64K := clampMax(memAbove4G/(64*kib), 0xffffff)
	cmos.Data[0x5b] = uint8(above4G64K)
	cmos.Data[0x5c] = uint8(above4G64K >> 8)
	cmos.Data[0x5d] = uint8(above4G64K >> 16)

	return cmos
}

func (c *CMOS) setUint16(index uint8, v uint16) {
	c.Data[index] = uint8(v)
	c.Data[index+1] = uint8(v >> 8)
}

func clampMax(v, max uint64) uint64 {
	if v > max {
		return max
	}

	return v
}

func saturatingSub(v, sub uint64) uint64 {
	if v < sub {
		return 0
	}

	return v - sub
}

func (c *CMOS) Read(base uint64, data []byte) error {
	if len(data) != 1 {
		return errDataLenInvalid
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	var d uint8

	// Reading CMOS RAM also requires two steps:
	// 1. OUT to port hex 70 with the CMOS address that is to be read from.
	// 2. IN from port hex 71, and the data read is returned in the AL register.
	// Ref: http://bitsavers.trailing-edge.com/pdf/ibm/pc/at/1502494_PC_AT_Technical_Reference_Mar84.pdf
	switch base {
	case indexOffset:
		data[0] = c.Index
	case dataOffset:
		dt := c.now()
		secs := dt.Second()
		min := dt.Minute()
		hour := dt.Hour()
		weekd := dt.Weekday()
		day := dt.Day()
		month := dt.Month()
		year := dt.Year()

		encode := toBCD
		if c.Data[0x0b]&0x04 != 0 {
			encode = func(value uint8) uint8 { return value }
		}

		// Bit 7 controls NMI masking; it is not part of the CMOS address.
		switch c.Index & indexMask {
		case 0x00:
			d = encode(uint8(secs))
		case 0x02:
			d = encode(uint8(min))
		case 0x04:
			if c.Data[0x0b]&0x02 == 0 {
				d = encode(uint8((hour+11)%12 + 1))
				if hour >= 12 {
					d |= 0x80
				}
			} else {
				d = encode(uint8(hour))
			}
		case 0x06:
			d = encode(uint8(weekd) + 1)
		case 0x07:
			d = encode(uint8(day))
		case 0x08:
			d = encode(uint8(month))
		case 0x09:
			d = encode(uint8(year % 100))
		case 0x0A:
			d = 1<<5 | 0<<7 // 32kHz Clock and we assume no update in progress
		case 0x0D:
			d = 1 << 7
		case 0x32:
			d = encode(uint8(year / 100))
		default:
			d = c.Data[c.Index&indexMask]
		}

		data[0] = d
	}

	return nil
}

func (c *CMOS) Write(base uint64, data []byte) error {
	if len(data) != 1 {
		return errDataLenInvalid
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Writing to CMOS RAM involves two steps:
	// 1. OUT to port hex 70 with the CMOS address that will be written to.
	// 2. OUT to port hex 71 with the data to be written.
	// Ref: http://bitsavers.trailing-edge.com/pdf/ibm/pc/at/1502494_PC_AT_Technical_Reference_Mar84.pdf
	switch base {
	case indexOffset:
		c.Index = data[0]
	case dataOffset:
		if c.Index == 0x8F && data[0] == 0 {
			// CMOS reset - we ignore for now
		} else {
			c.Data[c.Index&indexMask] = data[0]
		}
	}

	return nil
}

func toBCD(v uint8) uint8 {
	return ((v / 10) << 4) | (v % 10)
}

func (c *CMOS) IOPort() uint64 {
	return 0x70
}

func (c *CMOS) Size() uint64 {
	return 0x2
}
