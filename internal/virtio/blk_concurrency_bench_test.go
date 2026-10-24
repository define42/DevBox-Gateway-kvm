package virtio

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
)

type delayedReadImage struct {
	disk.Image
	delay time.Duration
}

func (d delayedReadImage) ReadAt(p []byte, off int64) (int, error) {
	if d.delay != 0 {
		time.Sleep(d.delay)
	}

	return d.Image.ReadAt(p, off)
}

// Warm raw reads isolate scheduler overhead; the delayed case models a blocked
// backend, not measured SSD latency. Each operation drains the complete batch.
func BenchmarkBlkReadConcurrency(b *testing.B) {
	for _, delay := range []time.Duration{0, time.Millisecond} {
		for _, requests := range []int{1, 8} {
			b.Run(fmt.Sprintf("delay=%s/requests=%d", delay, requests), func(b *testing.B) {
				const blockSize = 4096
				path := filepath.Join(b.TempDir(), "disk.img")
				if err := os.WriteFile(path, make([]byte, requests*blockSize), 0o600); err != nil {
					b.Fatal(err)
				}
				backing, err := disk.OpenRaw(path)
				if err != nil {
					b.Fatal(err)
				}
				mem := make([]byte, requests*(blockSize+17))
				v := newBlk(delayedReadImage{Image: backing, delay: delay}, false, 10, benchIRQInjector{}, mem)
				b.Cleanup(func() { _ = v.Close() })
				negotiateBlkFlush(v)
				q := NewSplitQueue(QueueSize)
				for i := range requests {
					addr, head := uint64(i*(blockSize+17)), uint16(i*3)
					binary.LittleEndian.PutUint64(mem[addr+8:], uint64(i*blockSize/SectorSize))
					q.Desc[head] = SplitDesc{Addr: addr, Len: 16, Flags: descFNext, Next: head + 1}
					q.Desc[head+1] = SplitDesc{Addr: addr + 16, Len: blockSize, Flags: descFNext | descFWrite, Next: head + 2}
					q.Desc[head+2] = SplitDesc{Addr: addr + 16 + blockSize, Len: 1, Flags: descFWrite}
					q.Avail.Ring[i] = head
				}
				v.VirtQueue[blkQueue] = q
				b.ReportAllocs()
				b.SetBytes(int64(requests * blockSize))
				for b.Loop() {
					v.LastAvailIdx[blkQueue], q.Used.Idx = 0, 0
					q.Avail.Idx = uint16(requests)
					if err := v.IO(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
