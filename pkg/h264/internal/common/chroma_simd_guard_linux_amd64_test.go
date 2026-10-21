//go:build linux && amd64

package common

import (
	"testing"

	"golang.org/x/sys/unix"
)

// Place the last valid byte immediately before an unreadable guard page.
func chromaGuardedBuffer(t *testing.T, size int) []byte {
	t.Helper()
	page := unix.Getpagesize()
	mapping, err := unix.Mmap(-1, 0, page*2, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	if err := unix.Mprotect(mapping[page:], unix.PROT_NONE); err != nil {
		t.Fatal(err)
	}
	return mapping[page-size : page : page]
}

func TestChromaSIMDGuardPages(t *testing.T) {
	for _, width := range []int32{2, 4, 8} {
		src := chromaGuardedBuffer(t, int((width+1)*9))
		dst := chromaGuardedBuffer(t, int(width*8))
		for i := range src {
			src[i] = byte(i*37 + 11)
		}
		for dx := int16(0); dx < 8; dx++ {
			for dy := int16(0); dy < 8; dy++ {
				McChroma_sse2(src, 0, width+1, dst, 0, width, dx, dy, width, 8)
			}
		}
	}
}
