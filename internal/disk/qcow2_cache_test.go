package disk

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const l2CacheTestClusterBits = 18

func TestQCOW2RejectsL2TableLargerThanCache(t *testing.T) {
	t.Parallel()

	header := &qcow2Header{clusterBits: 24, headerLength: qcow2HeaderV3Len, l1Size: 1, size: 1}
	if err := validateHeaderGeometry(header); err == nil || !strings.Contains(err.Error(), "l2 table exceeds") {
		t.Fatalf("oversized L2 table geometry returned %v", err)
	}
}

func TestQCOW2L2CacheBounded(t *testing.T) {
	t.Parallel()

	path, tableCoverage, regions := createL2CacheTestQCOW2(t)
	q := openWriteTestQCOW2(t, path)
	writeL2CacheTestRegions(t, q, tableCoverage, regions)

	if got, want := len(q.l2Cache), q.l2CacheLimit; got != want {
		t.Fatalf("cached L2 tables = %d, want %d", got, want)
	}
	if got := l2CachePayloadBytes(q); got > qcow2L2CacheBytes {
		t.Fatalf("cached L2 payload = %d bytes, limit %d", got, qcow2L2CacheBytes)
	}
	evicted := regions - q.l2CacheLimit
	for index := range evicted {
		if _, cached := q.l2Cache[uint64(index)]; cached {
			t.Fatalf("oldest L2 table %d remains cached", index)
		}
	}
	for index := evicted; index < regions; index++ {
		if _, cached := q.l2Cache[uint64(index)]; !cached {
			t.Fatalf("newest L2 table %d was not retained", index)
		}
	}
}

func TestQCOW2L2CacheEvictedWriteReloads(t *testing.T) {
	t.Parallel()

	path, tableCoverage, regions := createL2CacheTestQCOW2(t)
	q := openWriteTestQCOW2(t, path)
	want := []byte("write survives L2 eviction")
	mustWriteAt(t, q, want, 17)
	for region := 1; region < regions; region++ {
		offset := int64(uint64(region) * tableCoverage)
		mustWriteAt(t, q, []byte{byte(region)}, offset)
	}
	if _, cached := q.l2Cache[0]; cached {
		t.Fatal("first L2 table remains cached after FIFO eviction")
	}
	assertReadAt(t, q, want, 17)
	if _, cached := q.l2Cache[0]; !cached {
		t.Fatal("evicted L2 table was not cached after reload")
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openWriteTestQCOW2(t, path)
	assertReadAt(t, reopened, want, 17)
}

func TestQCOW2ConcurrentReadsWithL2Eviction(t *testing.T) {
	t.Parallel()

	path, tableCoverage, regions := createL2CacheTestQCOW2(t)
	q := openWriteTestQCOW2(t, path)
	writeL2CacheTestRegions(t, q, tableCoverage, regions)
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	q = openWriteTestQCOW2(t, path)

	start := make(chan struct{})
	var readers sync.WaitGroup
	for worker := range 8 {
		readers.Go(func() {
			<-start
			for iteration := range 64 {
				region := (worker*7 + iteration) % regions
				offset := int64(uint64(region) * tableCoverage)
				var got [1]byte
				n, err := q.ReadAt(got[:], offset)
				if err != nil || n != len(got) || got[0] != byte(region+1) {
					t.Errorf("region %d read = (%x, %d, %v)", region, got, n, err)

					return
				}
			}
		})
	}
	close(start)
	readers.Wait()
	if got := l2CachePayloadBytes(q); got > qcow2L2CacheBytes {
		t.Fatalf("cached L2 payload = %d bytes, limit %d", got, qcow2L2CacheBytes)
	}
}

func createL2CacheTestQCOW2(t *testing.T) (string, uint64, int) {
	t.Helper()
	clusterSize := uint64(1) << l2CacheTestClusterBits
	cacheLimit := int(qcow2L2CacheBytes / clusterSize)
	regions := cacheLimit + 2
	tableCoverage := clusterSize * (clusterSize / 8)
	metadata := make([]byte, 4*int(clusterSize))
	copy(metadata, qcow2Magic)
	binary.BigEndian.PutUint32(metadata[4:8], qcow2Version3)
	binary.BigEndian.PutUint32(metadata[20:24], l2CacheTestClusterBits)
	binary.BigEndian.PutUint64(metadata[24:32], uint64(regions)*tableCoverage)
	binary.BigEndian.PutUint32(metadata[36:40], uint32(regions))
	binary.BigEndian.PutUint64(metadata[40:48], 3*clusterSize)
	binary.BigEndian.PutUint64(metadata[48:56], clusterSize)
	binary.BigEndian.PutUint32(metadata[56:60], 1)
	binary.BigEndian.PutUint32(metadata[96:100], qcow2RefcountOrder16)
	binary.BigEndian.PutUint32(metadata[100:104], qcow2HeaderV3Len)
	binary.BigEndian.PutUint64(metadata[clusterSize:], 2*clusterSize)
	for cluster := range 4 {
		offset := 2*int(clusterSize) + cluster*2
		binary.BigEndian.PutUint16(metadata[offset:], 1)
	}
	path := filepath.Join(t.TempDir(), "l2-cache.qcow2")
	if err := os.WriteFile(path, metadata, 0o600); err != nil {
		t.Fatal(err)
	}

	return path, tableCoverage, regions
}

func writeL2CacheTestRegions(t *testing.T, q *qcow2Image, tableCoverage uint64, regions int) {
	t.Helper()
	for region := range regions {
		offset := int64(uint64(region) * tableCoverage)
		mustWriteAt(t, q, []byte{byte(region + 1)}, offset)
	}
}

func l2CachePayloadBytes(q *qcow2Image) uint64 {
	var total uint64
	for _, table := range q.l2Cache {
		total += uint64(len(table)) * 8
	}

	return total
}
