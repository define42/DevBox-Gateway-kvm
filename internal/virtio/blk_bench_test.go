package virtio

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/define42/gokvm/internal/disk"
)

type benchBlockImage struct {
	disk.Image
	syncs int
}

func (d *benchBlockImage) Sync() error {
	d.syncs++

	return d.Image.Sync()
}

func BenchmarkBlkWriteBatch(b *testing.B) {
	for _, requests := range []int{1, 8} {
		b.Run(fmt.Sprintf("requests=%d", requests), func(b *testing.B) {
			const blockSize = 4096
			path := filepath.Join(b.TempDir(), "disk.img")
			if err := os.WriteFile(path, make([]byte, requests*blockSize), 0o600); err != nil {
				b.Fatal(err)
			}
			backing, err := disk.Open(path)
			if err != nil {
				b.Fatal(err)
			}
			image := &benchBlockImage{Image: backing}
			mem := make([]byte, requests*(blockSize+17))
			v := newBlk(image, false, 10, benchIRQInjector{}, mem)
			b.Cleanup(func() { _ = backing.Close() })
			q := &SplitQueue{Desc: &[QueueSize]SplitDesc{}, Avail: &SplitAvail{}, Used: &SplitUsed{}}
			for i := 0; i < requests; i++ {
				addr := uint64(i * (blockSize + 17))
				head := uint16(i * 3)
				binary.LittleEndian.PutUint32(mem[addr:], 1)
				binary.LittleEndian.PutUint64(mem[addr+8:], uint64(i*blockSize/SectorSize))
				q.Desc[head] = SplitDesc{Addr: addr, Len: 16, Flags: descFNext, Next: head + 1}
				q.Desc[head+1] = SplitDesc{Addr: addr + 16, Len: blockSize, Flags: descFNext, Next: head + 2}
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
			b.ReportMetric(float64(image.syncs)/float64(b.N), "syncs/op")
		})
	}
}
