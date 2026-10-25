package virtio

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
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
			backing, err := disk.OpenRaw(path)
			if err != nil {
				b.Fatal(err)
			}
			image := &benchBlockImage{Image: backing}
			mem := make([]byte, requests*(blockSize+17))
			v := newBlk(image, false, 10, benchIRQInjector{}, mem)
			b.Cleanup(func() { _ = backing.Close() })
			q := NewSplitQueue(QueueSize)
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

// BenchmarkBlkWriteFlushGroup submits writes individually, as a QD1 guest
// would, and makes every 32-write group durable in both cache modes.
func BenchmarkBlkWriteFlushGroup(b *testing.B) {
	for _, writeback := range []bool{false, true} {
		name := "writethrough"
		if writeback {
			name = "writeback"
		}
		b.Run(name, func(b *testing.B) {
			const blockSize, groupSize = 4096, 32
			path := filepath.Join(b.TempDir(), "disk.img")
			if err := os.WriteFile(path, make([]byte, groupSize*blockSize), 0o600); err != nil {
				b.Fatal(err)
			}
			backing, err := disk.OpenRaw(path)
			if err != nil {
				b.Fatal(err)
			}
			image := &benchBlockImage{Image: backing}
			mem := make([]byte, blockSize+64)
			v := newBlk(image, false, 10, benchIRQInjector{}, mem)
			b.Cleanup(func() { _ = v.Close() })
			if writeback {
				writeCfg(v.ModernTransport, 12, blkFeatureFlush, 4)
				writeCfg(v.ModernTransport, 20, 0x08, 1)
			}
			q := NewSplitQueue(QueueSize)
			binary.LittleEndian.PutUint32(mem, 1)
			binary.LittleEndian.PutUint32(mem[blockSize+32:], 4)
			q.Desc[0] = SplitDesc{Addr: 0, Len: 16, Flags: descFNext, Next: 1}
			q.Desc[1] = SplitDesc{Addr: 16, Len: blockSize, Flags: descFNext, Next: 2}
			q.Desc[2] = SplitDesc{Addr: blockSize + 16, Len: 1, Flags: descFWrite}
			q.Desc[3] = SplitDesc{Addr: blockSize + 32, Len: 16, Flags: descFNext, Next: 4}
			q.Desc[4] = SplitDesc{Addr: blockSize + 48, Len: 1, Flags: descFWrite}
			v.VirtQueue[blkQueue] = q
			submit := func(head uint16) {
				q.Avail.Ring[q.Avail.Idx%QueueSize] = head
				q.Avail.Idx++
				if err := v.IO(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.SetBytes(groupSize * blockSize)
			for b.Loop() {
				for i := range groupSize {
					binary.LittleEndian.PutUint64(mem[8:], uint64(i*blockSize/SectorSize))
					submit(0)
				}
				if writeback {
					submit(3)
				}
			}
			b.ReportMetric(float64(image.syncs)/float64(b.N), "syncs/op")
		})
	}
}

// BenchmarkBlkSegmentedRead submits one maximum-size read into scattered 4 KiB
// guest pages, as Linux readahead does, from a warm, fully allocated QCOW2
// image, with and without the image's vectored reads.
func BenchmarkBlkSegmentedRead(b *testing.B) {
	for _, vectored := range []bool{false, true} {
		name := "per_segment"
		if vectored {
			name = "vectored"
		}
		b.Run(name, func(b *testing.B) {
			const pageSize, diskSize = 4096, 4 << 20
			image, err := disk.CreateQCOW2(filepath.Join(b.TempDir(), "disk.qcow2"), diskSize)
			if err != nil {
				b.Fatal(err)
			}
			fill := make([]byte, 1<<20)
			for off := int64(0); off < diskSize; off += int64(len(fill)) {
				if _, err := image.WriteAt(fill, off); err != nil {
					b.Fatal(err)
				}
			}
			backing := image
			if !vectored {
				backing = struct{ disk.Image }{image} // Hides ReadvAt.
			}
			mem := make([]byte, 0x2000+2*blkMaxSegments*pageSize)
			v := newBlk(backing, false, 10, benchIRQInjector{}, mem)
			b.Cleanup(func() { _ = v.Close() })
			q := NewSplitQueue(QueueSize)
			v.VirtQueue[blkQueue] = q
			segments := make([]blkTestSegment, blkMaxSegments)
			for i := range segments {
				segments[i] = blkTestSegment{addr: 0x2000 + uint64(2*i*pageSize), len: pageSize}
			}
			ids := make([]uint16, blkMaxSegments+2)
			for i := range ids {
				ids[i] = uint16(i)
			}
			b.ReportAllocs()
			b.SetBytes(blkMaxSegments * pageSize)
			for b.Loop() {
				addSegmentedRequest(q, mem, ids, 0, 0, 0x100, 0x200, segments)
				if err := v.IO(); err != nil || mem[0x200] != 0 {
					b.Fatalf("read: IO=%v status=%d", err, mem[0x200])
				}
			}
		})
	}
}
