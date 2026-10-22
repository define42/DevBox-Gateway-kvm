//go:build linux && amd64

package encoder

import (
	"fmt"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func intraDCGuardedBuffer(t *testing.T, size int, atEnd bool) ([]byte, func()) {
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
		start = 2*page - size
	}
	return mapping[start : start+size : start+size], func() {
		t.Helper()
		if err := unix.Mprotect(mapping[page:2*page], unix.PROT_READ); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIntraDCRawGuards(t *testing.T) {
	for _, atEnd := range []bool{false, true} {
		t.Run(fmt.Sprintf("end%t", atEnd), func(t *testing.T) {
			sourceBytes, readOnly := intraDCGuardedBuffer(t, 241*2, atEnd)
			source := (*[241]int16)(unsafe.Pointer(&sourceBytes[0]))
			for i := range source {
				source[i] = int16(i*1701 - 32768)
			}
			before := *source
			readOnly()
			dcBytes, _ := intraDCGuardedBuffer(t, 32, atEnd)
			dc := (*[16]int16)(unsafe.Pointer(&dcBytes[0]))
			var wantDc [16]int16
			WelsHadamardT4Dc_c(wantDc[:], before[:])
			hadamardT4DcSSE2(dc, source)
			if *dc != wantDc || *source != before {
				t.Fatal("guarded forward output differs or input changed")
			}
			for _, mf := range []uint16{0, 1, 32768, 65535} {
				*dc = [16]int16{-32768, 32767, -1, 1, 16384, -16385, 8191, -8192, 32766, -32767, 3, -5, 7, -11, 13, -17}
				want := *dc
				WelsDequantIHadamard4x4_c(want[:], mf)
				dequantIHadamard4x4SSE2(dc, uint64(mf))
				if *dc != want {
					t.Fatalf("guarded inverse mf%d differs", mf)
				}
			}
		})
	}
}
