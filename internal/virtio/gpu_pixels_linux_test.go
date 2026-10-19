//go:build linux

package virtio

import (
	"bytes"
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

func TestBGRToRGBARowProtectedPages(t *testing.T) {
	t.Parallel()
	edges := []struct {
		name  string
		atEnd bool
	}{
		{name: "start"},
		{name: "end", atEnd: true},
	}
	for _, pixels := range []int{0, 1, 7, 8, 9, 15, 16, 17, 31, 32, 33, 63, 64, 65} {
		for _, alpha := range []uint32{0, 0xff000000} {
			for _, srcEdge := range edges {
				for _, dstEdge := range edges {
					name := fmt.Sprintf("pixels=%d/alpha=%x/src=%s/dst=%s", pixels, alpha, srcEdge.name, dstEdge.name)
					t.Run(name, func(t *testing.T) {
						t.Parallel()
						source, dest := newGPUProtectedPixelPage(t), newGPUProtectedPixelPage(t)
						for i := range source {
							source[i] = byte(i*37 + i/4 + 11)
							dest[i] = 0xa5
						}
						n := pixels * gpuBytesPerPixel
						srcStart, dstStart := 0, 0
						if srcEdge.atEnd {
							srcStart = len(source) - n
						}
						if dstEdge.atEnd {
							dstStart = len(dest) - n
						}
						want := bytes.Clone(dest)
						for x := range pixels {
							s, d := srcStart+x*4, dstStart+x*4
							want[d], want[d+1], want[d+2] = source[s+2], source[s+1], source[s]
							want[d+3] = source[s+3] | byte(alpha>>24)
						}
						if err := unix.Mprotect(source, unix.PROT_READ); err != nil {
							t.Fatalf("protect source page: %v", err)
						}
						bgrToRGBARow(dest[dstStart:dstStart+n:dstStart+n], source[srcStart:srcStart+n:srcStart+n], alpha)
						if !bytes.Equal(dest, want) {
							t.Fatal("converted pixels or bytes outside the row differ")
						}
					})
				}
			}
		}
	}
}

// newGPUProtectedPixelPage places one readable/writable page between two
// inaccessible pages. Unlike byte sentinels, these guards detect extra reads.
func newGPUProtectedPixelPage(t *testing.T) []byte {
	t.Helper()
	pageSize := unix.Getpagesize()
	mapping, err := unix.Mmap(
		-1,
		0,
		3*pageSize,
		unix.PROT_NONE,
		unix.MAP_PRIVATE|unix.MAP_ANON,
	)
	if err != nil {
		t.Fatalf("map protected pixel pages: %v", err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Errorf("unmap protected pixel pages: %v", err)
		}
	})
	page := mapping[pageSize : 2*pageSize : 2*pageSize]
	if err := unix.Mprotect(page, unix.PROT_READ|unix.PROT_WRITE); err != nil {
		t.Fatalf("make pixel page writable: %v", err)
	}

	return page
}
