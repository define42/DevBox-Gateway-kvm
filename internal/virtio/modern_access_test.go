package virtio

import (
	"bytes"
	"fmt"
	"testing"
)

func TestModernCommonRejectsInvalidWrites(t *testing.T) {
	t.Parallel()

	// The common configuration uses exact-width writes. Queue addresses are
	// programmed using two independent 32-bit accesses.
	writable := map[uint64]int{
		0: 4, 8: 4, 12: 4, 16: 2, 20: 1, 22: 2, 24: 2, 26: 2, 28: 2,
		32: 4, 36: 4, 40: 4, 44: 4, 48: 4, 52: 4,
	}
	for off := uint64(0); off <= commonCfgLen; off++ {
		for _, width := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 16, 64} {
			if validWidth, ok := writable[off]; ok && width == validWidth {
				continue
			}
			t.Run(fmt.Sprintf("offset_%02x_width_%d", off, width), func(t *testing.T) {
				t.Parallel()
				dev := &resettingModernDev{mockModernDev: mockModernDev{numQ: 2}}
				injected := 0
				tr := newTestTransport(dev, make([]byte, 0x10000), &injected)
				writeCfg(tr, 0, 1, 4)
				writeCfg(tr, 8, 1, 4)
				writeCfg(tr, 12, 1, 4)
				writeCfg(tr, 16, 0x1234, 2)
				writeCfg(tr, 20, 0x0f, 1)
				writeCfg(tr, 22, 1, 2)
				writeCfg(tr, 24, 16, 2)
				writeCfg(tr, 26, 0x5678, 2)
				writeCfg(tr, 32, 0x1000, 4)
				writeCfg(tr, 40, 0x2000, 4)
				writeCfg(tr, 48, 0x3000, 4)
				if err := tr.Interrupt(); err != nil {
					t.Fatal(err)
				}
				var before, after [commonCfgLen]byte
				tr.MMIO(commonCfgOffset, before[:], false)
				// Zero can trigger reset; one can enable a queue. Neither may
				// have side effects when the access itself is invalid.
				for _, value := range []uint64{0, 1} {
					writeCfg(tr, off, value, width)
					tr.MMIO(commonCfgOffset, after[:], false)
					if !bytes.Equal(before[:], after[:]) {
						t.Fatalf("invalid write of %d changed configuration: before=%x after=%x", value, before, after)
					}
					if dev.resets != 0 || len(dev.ready) != 0 {
						t.Fatalf("invalid write invoked callbacks: resets=%d queues=%v", dev.resets, dev.ready)
					}
				}
				var isr [1]byte
				tr.MMIO(isrCfgOffset, isr[:], false)
				if isr[0] != 1 || injected != 1 {
					t.Fatalf("invalid write changed interrupt state: ISR=%d injections=%d", isr[0], injected)
				}
			})
		}
	}
}

func TestModernQueueAddressHalves(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		off  uint64
	}{
		{name: "descriptor", off: 32},
		{name: "available", off: 40},
		{name: "used", off: 48},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := &mockModernDev{numQ: 1}
			injected := 0
			tr := newTestTransport(dev, nil, &injected)
			writeCfg(tr, tc.off, 0x89abcdef, 4)
			writeCfg(tr, tc.off+4, 0x12345678, 4)
			if got := readCfg(tr, tc.off, 8); got != 0x1234567889abcdef {
				t.Fatalf("address = %#x, want %#x", got, uint64(0x1234567889abcdef))
			}
			writeCfg(tr, tc.off, 0xfedcba98, 4)
			if got := readCfg(tr, tc.off, 8); got != 0x12345678fedcba98 {
				t.Fatalf("low write changed high half: %#x", got)
			}
			writeCfg(tr, tc.off+4, 0x76543210, 4)
			if got := readCfg(tr, tc.off, 8); got != 0x76543210fedcba98 {
				t.Fatalf("high write changed low half: %#x", got)
			}
		})
	}
}
