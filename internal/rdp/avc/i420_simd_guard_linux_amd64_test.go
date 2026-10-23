//go:build linux && amd64

package avc

import (
	"testing"

	"golang.org/x/sys/cpu"
	"golang.org/x/sys/unix"
)

// Place the last valid byte immediately before an unreadable guard page.
func i420GuardedBuffer(t *testing.T, size int) []byte {
	t.Helper()
	page := unix.Getpagesize()
	pages := (size+page-1)/page + 1
	mapping, err := unix.Mmap(-1, 0, pages*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	guard := (pages - 1) * page
	if err := unix.Mprotect(mapping[guard:], unix.PROT_NONE); err != nil {
		t.Fatal(err)
	}

	return mapping[guard-size : guard : guard]
}

func TestRGB32ToI420AVX2GuardPages(t *testing.T) {
	t.Parallel()
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 is unavailable")
	}

	for _, format := range i420AVX2Formats() {
		for _, width := range []int{8, 30, 32, 34, 62, 64, 96, 130} {
			src0, src1 := i420GuardedBuffer(t, width*4), i420GuardedBuffer(t, width*4)
			y0, y1 := i420GuardedBuffer(t, width), i420GuardedBuffer(t, width)
			u, v := i420GuardedBuffer(t, width/2), i420GuardedBuffer(t, width/2)
			for i := range src0 {
				src0[i], src1[i] = byte(i*37+11), byte(i*53+7)
			}
			if _, ok := format.convert(y0, y1, u, v, src0, src1); !ok {
				t.Fatalf("%s width %d: valid input was rejected on an AVX2 CPU", format.name, width)
			}
			wantY0, wantY1, wantU, wantV := referenceRGB32I420Rows(src0, src1, width, format.bgrx)
			checkI420SIMDPlanes(t, y0, y1, u, v, wantY0, wantY1, wantU, wantV)
		}
	}
}
