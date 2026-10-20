package iodev_test

import (
	"sync"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/iodev"
)

func TestPS2KeyboardFloodPreservesRelease(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	for range 20 * 1024 {
		dev.KeyEvent(true, 'a')
	}
	dev.KeyEvent(false, 'a')
	data := drainBoundedPS2(t, dev, false)
	if len(data) < 2 || data[0] != 0x1e || data[len(data)-1] != 0x9e {
		t.Fatalf("flood lost make/break sequence: %x", data)
	}
}

func TestPS2ConcurrentBoundedQueue(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	writePS2Mouse(t, dev, 0xf4)
	_ = readPS2(t, dev, 0x60)
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 2000 {
			dev.KeyEvent(true, 0xff53)
			dev.KeyEvent(false, 0xff53)
		}
	})
	workers.Go(func() {
		for i := range 2000 {
			dev.PointerEvent(uint8(i%8), uint16(i*17), uint16(i*11))
		}
	})
	workers.Go(func() {
		for range 2000 {
			if err := dev.Write(0x64, []byte{0x60}); err != nil {
				t.Error(err)
			}
			if err := dev.Write(0x60, []byte{0x47}); err != nil {
				t.Error(err)
			}
		}
	})
	workers.Go(func() {
		for range 2000 {
			if err := dev.Write(0x64, []byte{0x20}); err != nil {
				t.Error(err)
			}
		}
	})
	workers.Go(func() {
		for range 2000 {
			var data [1]byte
			if err := dev.Read(0x60, data[:]); err != nil {
				t.Error(err)
			}
			if err := dev.Read(0x64, data[:]); err != nil {
				t.Error(err)
			}
		}
	})
	workers.Wait()
	for n := 0; readPS2(t, dev, 0x64)&1 != 0; n++ {
		if n >= 1028 {
			t.Fatal("concurrent producers exceeded the PS/2 backlog bound")
		}
		_ = readPS2(t, dev, 0x60)
	}
}

func drainBoundedPS2(t *testing.T, dev *iodev.PS2Controller, aux bool) []byte {
	t.Helper()
	var data []byte
	for readPS2(t, dev, 0x64)&1 != 0 {
		// The queue holds at most 1024 bytes, plus one deferred mouse packet.
		if len(data) >= 1028 {
			t.Fatal("PS/2 input backlog exceeded its fixed bound")
		}
		if got := readPS2(t, dev, 0x64)&0x20 != 0; got != aux {
			t.Fatalf("auxiliary status = %v, want %v", got, aux)
		}
		data = append(data, readPS2(t, dev, 0x60))
	}

	return data
}
