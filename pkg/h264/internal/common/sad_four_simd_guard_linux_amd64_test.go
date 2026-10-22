//go:build linux && amd64

package common

import (
	"testing"

	"golang.org/x/sys/unix"
)

func sadGuardedBuffer(t *testing.T, size int, atEnd bool) []byte {
	t.Helper()
	page := unix.Getpagesize()
	mapping, err := unix.Mmap(-1, 0, page*3, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	for _, guard := range [][]byte{mapping[:page], mapping[2*page:]} {
		if err := unix.Mprotect(guard, unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
	}
	off := page
	if atEnd {
		off = 2*page - size
	}
	return mapping[off : off+size : off+size]
}

func TestSampleSadFourGuardPages(t *testing.T) {
	for _, tc := range sadFourSIMDCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, atEnd := range []bool{false, true} {
				stride := tc.width
				a := sadGuardedBuffer(t, tc.width*tc.height, atEnd)
				ref := sadGuardedBuffer(t, (tc.height+2)*stride, atEnd)
				for i := range a {
					a[i] = byte(i*11 + 3)
				}
				for i := range ref {
					ref[i] = byte(i*37 + 7)
				}
				var got [4]int32
				tc.batched(a, 0, int32(stride), ref, stride, int32(stride), got[:])
				want := sadFourReference(a, 0, stride, ref, stride, stride, tc.width, tc.height)
				if got != want {
					t.Fatalf("atEnd=%t: got=%v want=%v", atEnd, got, want)
				}
			}
		})
	}
}

func TestSampleSadNarrowGuardPages(t *testing.T) {
	for _, tc := range sadFourSIMDCases {
		if tc.width != 4 {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			a := sadGuardedBuffer(t, tc.width*tc.height, true)
			ref := sadGuardedBuffer(t, tc.width*tc.height, true)
			for i := range a {
				a[i] = 255
			}
			if got, want := tc.single(a, 0, 4, ref, 0, 4), int32(tc.width*tc.height*255); got != want {
				t.Fatalf("got=%d want=%d", got, want)
			}
		})
	}
}
