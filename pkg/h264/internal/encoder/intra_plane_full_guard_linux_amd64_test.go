//go:build linux && amd64

package encoder

import (
	"bytes"
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

func planeFullGuardedReference(t *testing.T, size int, layout string) ([]byte, int, int32) {
	t.Helper()
	page := unix.Getpagesize()
	stride := 3 * page
	pages := 3*(size+1) + 2
	mapping, err := unix.Mmap(-1, 0, pages*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	corner := page
	switch layout {
	case "TopEnd":
		corner = 2*page - size - 1
	case "LeftEnd":
		corner = 2*page - 1
	}
	ref := mapping[corner : corner+size*stride+1 : corner+size*stride+1]
	for i := 0; i <= size; i++ {
		ref[i] = byte(i*73 + 11)
		ref[i*stride] = byte(i*137 + 19)
	}
	readable := make([]bool, pages)
	for i := 0; i <= size; i++ {
		readable[1+3*i] = true
	}
	if layout == "LeftEnd" {
		readable[2] = true
	} // exact top bytes cross the first boundary
	if layout == "UnusedCenter" {
		readable[1+3*(size/2)] = false
	}
	for p := 0; p < pages; p++ {
		protection := unix.PROT_NONE
		if readable[p] {
			protection = unix.PROT_READ
		}
		if err := unix.Mprotect(mapping[p*page:(p+1)*page], protection); err != nil {
			t.Fatal(err)
		}
	}
	return ref, stride + 1, int32(stride)
}

func planeFullGuardedOutput(t *testing.T, length int, atEnd bool) []byte {
	t.Helper()
	page := unix.Getpagesize()
	mapping, err := unix.Mmap(-1, 0, 3*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	if err := unix.Mprotect(mapping[:page], unix.PROT_NONE); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mprotect(mapping[2*page:], unix.PROT_NONE); err != nil {
		t.Fatal(err)
	}
	start := page
	if atEnd {
		start = 2*page - length
	}
	return mapping[start : start+length : start+length]
}

func TestIntraPlaneFullExactReferenceGuards(t *testing.T) {
	for _, size := range []int{8, 16} {
		for _, layout := range []string{"TopStart", "TopEnd", "LeftEnd", "UnusedCenter"} {
			for _, outputAtEnd := range []bool{false, true} {
				t.Run(fmt.Sprintf("size%d/%s/outputEnd%t", size, layout, outputAtEnd), func(t *testing.T) {
					ref, off, stride := planeFullGuardedReference(t, size, layout)
					want := make([]byte, size*size)
					planeFullReference(want, 0, ref, off, stride, size)
					got := planeFullGuardedOutput(t, size*size, outputAtEnd)
					planeFullPredict(size)(got, 0, ref, off, stride)
					if !bytes.Equal(got, want) {
						t.Fatal("guarded plane prediction differs")
					}
				})
			}
		}
	}
}
