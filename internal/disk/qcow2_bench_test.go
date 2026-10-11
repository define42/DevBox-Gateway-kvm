package disk

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

const (
	benchmarkQCOW2Size        = 64 * 1024 * 1024
	benchmarkQCOW2ClusterSize = 64 * 1024
	benchmarkQCOW2BlockSize   = 4096
)

func BenchmarkQCOW2Open(b *testing.B) {
	path := createBenchmarkQCOW2(b)
	b.ReportAllocs()
	for b.Loop() {
		image, err := Open(path)
		if err != nil {
			b.Fatal(err)
		}
		if err := image.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQCOW2Read4K(b *testing.B) {
	image := openBenchmarkQCOW2(b)
	data := make([]byte, benchmarkQCOW2BlockSize)
	var off int64
	b.ReportAllocs()
	b.SetBytes(benchmarkQCOW2BlockSize)
	for b.Loop() {
		if n, err := image.ReadAt(data, off); err != nil || n != len(data) {
			b.Fatalf("ReadAt: got (%d, %v), want (%d, nil)", n, err, len(data))
		}
		off = nextBenchmarkQCOW2Offset(off)
	}
}

func BenchmarkQCOW2Write4K(b *testing.B) {
	for _, syncEach := range []bool{false, true} {
		name := "buffered"
		if syncEach {
			name = "sync"
		}
		b.Run(name, func(b *testing.B) {
			image := openBenchmarkQCOW2(b)
			data := bytes.Repeat([]byte{0x5a}, benchmarkQCOW2BlockSize)
			var off int64
			b.ReportAllocs()
			b.SetBytes(benchmarkQCOW2BlockSize)
			for b.Loop() {
				if n, err := image.WriteAt(data, off); err != nil || n != len(data) {
					b.Fatalf("WriteAt: got (%d, %v), want (%d, nil)", n, err, len(data))
				}
				if syncEach {
					if err := image.Sync(); err != nil {
						b.Fatal(err)
					}
				}
				off = nextBenchmarkQCOW2Offset(off)
			}
			// Drain buffered writes outside the timed loop before the next case.
			if err := image.Sync(); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// An odd stride in 4 KiB blocks visits the entire power-of-two working set,
// crossing clusters without measuring random-number generation.
func nextBenchmarkQCOW2Offset(off int64) int64 {
	return (off + benchmarkQCOW2ClusterSize + benchmarkQCOW2BlockSize) & (benchmarkQCOW2Size - 1)
}

func openBenchmarkQCOW2(b *testing.B) Image {
	b.Helper()
	image, err := Open(createBenchmarkQCOW2(b))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := image.Close(); err != nil {
			b.Error(err)
		}
	})
	// These are warm buffered-I/O benchmarks, not measurements of storage media
	// or guest virtio/KVM latency. Populate the complete host page-cache working set.
	data := make([]byte, 1024*1024)
	for off := int64(0); off < benchmarkQCOW2Size; off += int64(len(data)) {
		if n, err := image.ReadAt(data, off); err != nil || n != len(data) {
			b.Fatalf("warm ReadAt: got (%d, %v), want (%d, nil)", n, err, len(data))
		}
	}

	return image
}

// createBenchmarkQCOW2 builds a standalone version-3 image without qemu-img.
// All virtual data is allocated, so overwrite benchmarks exclude image growth.
// Clusters 0..4 contain the header, refcount table, refcount block, L1, and L2.
func createBenchmarkQCOW2(b *testing.B) string {
	b.Helper()
	const (
		metadataClusters = 5
		dataClusters     = benchmarkQCOW2Size / benchmarkQCOW2ClusterSize
	)
	metadata := make([]byte, metadataClusters*benchmarkQCOW2ClusterSize)
	copy(metadata, qcow2Magic)
	binary.BigEndian.PutUint32(metadata[4:8], qcow2Version3)
	binary.BigEndian.PutUint32(metadata[20:24], 16)
	binary.BigEndian.PutUint64(metadata[24:32], benchmarkQCOW2Size)
	binary.BigEndian.PutUint32(metadata[36:40], 1)
	binary.BigEndian.PutUint64(metadata[40:48], 3*benchmarkQCOW2ClusterSize)
	binary.BigEndian.PutUint64(metadata[48:56], benchmarkQCOW2ClusterSize)
	binary.BigEndian.PutUint32(metadata[56:60], 1)
	binary.BigEndian.PutUint32(metadata[96:100], qcow2RefcountOrder16)
	binary.BigEndian.PutUint32(metadata[100:104], qcow2HeaderV3Len)
	binary.BigEndian.PutUint64(metadata[benchmarkQCOW2ClusterSize:], 2*benchmarkQCOW2ClusterSize)
	for cluster := range metadataClusters + dataClusters {
		binary.BigEndian.PutUint16(metadata[2*benchmarkQCOW2ClusterSize+cluster*2:], 1)
	}
	binary.BigEndian.PutUint64(metadata[3*benchmarkQCOW2ClusterSize:], 4*benchmarkQCOW2ClusterSize|qcow2OFlagCopied)
	for cluster := range dataClusters {
		hostOff := uint64((metadataClusters + cluster) * benchmarkQCOW2ClusterSize)
		binary.BigEndian.PutUint64(metadata[4*benchmarkQCOW2ClusterSize+cluster*8:], hostOff|qcow2OFlagCopied)
	}
	path := filepath.Join(b.TempDir(), "allocated.qcow2")
	file, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = file.Close() })
	if _, err := file.Write(metadata); err != nil {
		b.Fatal(err)
	}
	data := bytes.Repeat([]byte{0xa5}, benchmarkQCOW2ClusterSize)
	for range dataClusters {
		if _, err := file.Write(data); err != nil {
			b.Fatal(err)
		}
	}
	if err := file.Sync(); err != nil {
		b.Fatal(err)
	}
	if err := file.Close(); err != nil {
		b.Fatal(err)
	}

	return path
}
