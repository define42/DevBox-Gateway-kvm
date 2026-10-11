package virtio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"syscall"
	"testing"

	"github.com/define42/gokvm/internal/guestmem"
)

// sparseGuestMemory reserves a guest-physical address space but commits only
// the pages touched by the test, so exercising high RAM needs very little RAM.
func sparseGuestMemory(t *testing.T) []byte {
	t.Helper()
	mem, err := syscall.Mmap(-1, 0, int(guestmem.HighRAMStart+65536), syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS|syscall.MAP_NORESERVE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Munmap(mem); err != nil {
			t.Error(err)
		}
	})

	return mem
}

func TestModernQueueGuestRAM(t *testing.T) {
	t.Parallel()
	mem := sparseGuestMemory(t)
	for _, tc := range []struct {
		name string
		addr uint64
		want bool
	}{
		{"low RAM", 0x1000, true},
		{"high RAM", guestmem.HighRAMStart + 0x1000, true},
		{"MMIO hole", guestmem.MMIOStart, false},
		{"cross MMIO boundary", guestmem.MMIOStart - 16, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, part := range []string{"descriptor", "available", "used"} {
				q := queueState{size: QueueSize, enable: 1, desc: 0x1000, driver: 0x3000, device: 0x4000}
				switch part {
				case "descriptor":
					q.desc = tc.addr
				case "available":
					q.driver = tc.addr
				case "used":
					q.device = tc.addr
				}
				tr := &ModernTransport{Mem: mem, queues: []queueState{q}}
				if got := tr.mapQueue(0); (got != nil) != tc.want {
					t.Fatalf("%s queue at %#x accepted=%v, want %v", part, tc.addr, got != nil, tc.want)
				}
			}
		})
	}
}

func TestDeviceDMAGuestRAM(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		addr uint64
		want bool
	}{
		{"low RAM", 0x1000, true},
		{"high RAM", guestmem.HighRAMStart + 0x1000, true},
		{"MMIO hole", guestmem.MMIOStart, false},
		{"cross MMIO boundary", guestmem.MMIOStart - 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mem := sparseGuestMemory(t)
			q := &SplitQueue{Desc: new([QueueSize]SplitDesc), Avail: new(SplitAvail), Used: new(SplitUsed)}
			q.Desc[0] = SplitDesc{Addr: tc.addr, Len: 16, Flags: descFWrite}
			tr := &ModernTransport{Mem: mem}

			n := &Net{ModernTransport: tr}
			_, _, err := n.rxBuffers(q, 0)
			if (err == nil) != tc.want || err != nil && !errors.Is(err, ErrNetDesc) {
				t.Fatalf("network RX validation: %v", err)
			}

			packet := new(bytes.Buffer)
			n.tap = packet
			n.ModernTransport = NewModernTransport(n, mem, func() error { return nil })
			n.VirtQueue[netTxQueue] = q
			q.Desc[0].Flags = 0
			q.Avail.Idx = 1
			err = n.Tx()
			if (err == nil) != tc.want || err != nil && !errors.Is(err, ErrNetDesc) {
				t.Fatalf("network TX validation: %v", err)
			}
			if tc.want && !bytes.Equal(packet.Bytes(), mem[tc.addr+netHdrLen:tc.addr+16]) {
				t.Fatal("network TX did not read the addressed RAM")
			}
			if !tc.want && packet.Len() != 0 {
				t.Fatal("network TX transmitted bytes from the MMIO hole")
			}
			q.Desc[0].Flags = descFWrite

			s := &Sound{ModernTransport: tr}
			_, _, _, _, ok := s.chain(q, 0, 16)
			if ok != tc.want {
				t.Fatalf("sound DMA accepted=%v, want %v", ok, tc.want)
			}

			g := &GPU{ModernTransport: tr, resources: map[uint32]*gpuResource{1: {}}}
			_, writable := g.collectChain(q, 0)
			if (len(writable) == 1) != tc.want {
				t.Fatal("GPU accepted invalid response buffer or rejected RAM")
			}
			q.Desc[0].Flags = 0
			readable, _ := g.collectChain(q, 0)
			if (len(readable) == 16) != tc.want {
				t.Fatal("GPU accepted invalid request buffer or rejected RAM")
			}

			// Check the whole block chain, with the data buffer at the tested address.
			q.Desc[0] = SplitDesc{Addr: 0x100, Len: 16, Flags: descFNext, Next: 1}
			q.Desc[1] = SplitDesc{Addr: tc.addr, Len: 16, Flags: descFNext, Next: 2}
			q.Desc[2] = SplitDesc{Addr: 0x200, Len: 1, Flags: descFWrite}
			b := &Blk{ModernTransport: tr}
			err = b.snapshotRequest(q, QueueSize, 0, new(blkPendingRequest))
			if (err == nil) != tc.want || err != nil && !errors.Is(err, ErrBlkDesc) {
				t.Fatalf("block DMA validation: %v", err)
			}

			q.Desc[0] = SplitDesc{Addr: tc.addr, Len: 16, Flags: descFWrite}
			d := &InputDevice{ModernTransport: tr}
			before := bytes.Clone(mem[tc.addr : tc.addr+16])
			_, ok = d.writeEvent(q, 0, inputEvent{typ: evKey, code: keyA, value: 1})
			if ok != tc.want {
				t.Fatalf("input DMA accepted=%v, want %v", ok, tc.want)
			}
			if !tc.want && !bytes.Equal(mem[tc.addr:tc.addr+16], before) {
				t.Fatal("input DMA modified the MMIO hole")
			}

			req := make([]byte, gpuCtrlHdrLen+8+16)
			binary.LittleEndian.PutUint32(req[gpuCtrlHdrLen:], 1)
			binary.LittleEndian.PutUint32(req[gpuCtrlHdrLen+4:], 1)
			binary.LittleEndian.PutUint64(req[gpuCtrlHdrLen+8:], tc.addr)
			binary.LittleEndian.PutUint32(req[gpuCtrlHdrLen+16:], 16)
			response := g.cmdResourceAttachBacking(req)
			if got := binary.LittleEndian.Uint32(response); (got == gpuRespOKNoData) != tc.want {
				t.Fatalf("GPU backing validation response %#x", got)
			}
			if tc.want {
				var copied [16]byte
				g.backingRead(g.resources[1], 0, copied[:])
				if !bytes.Equal(copied[:], mem[tc.addr:tc.addr+16]) {
					t.Fatal("GPU backing read did not access the addressed RAM")
				}
			}
		})
	}
}
