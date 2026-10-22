//go:build linux && amd64

package processing

import (
	"fmt"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Each row has a separate accessible page between inaccessible pages. Testing
// at both edges checks the exact read/write extent in either direction; source
// pages are read-only so accidental input stores fault too.
func vaaAVX2GuardedRows(t *testing.T, height, rowBytes int, atEnd, readOnly bool, seed int) ([]byte, int) {
	t.Helper()
	page := unix.Getpagesize()
	if height <= 0 || rowBytes <= 0 || rowBytes > page {
		t.Fatal("invalid guarded row shape")
	}
	mapping, err := unix.Mmap(-1, 0, (2*height+1)*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	start := page
	if atEnd {
		start += page - rowBytes
	}
	for y := range height {
		for x := range rowBytes {
			mapping[start+2*y*page+x] = byte(y*73 + x*157 + seed)
		}
	}
	for p := range 2*height + 1 {
		prot := unix.PROT_NONE
		if p&1 != 0 {
			prot = unix.PROT_READ
			if !readOnly {
				prot |= unix.PROT_WRITE
			}
		}
		if err := unix.Mprotect(mapping[p*page:(p+1)*page], prot); err != nil {
			t.Fatal(err)
		}
	}
	end := start + 2*(height-1)*page + rowBytes
	return mapping[start:end:end], 2 * page
}

func TestVAAAVX2ExactRowGuards(t *testing.T) {
	requireVAAAVX2(t)
	// No macroblocks means no input/output pointer dereference.
	if got := vaaSadRowAVX2(nil, nil, 0, nil, 0); got != 0 {
		t.Fatalf("empty raw row: %d", got)
	}
	for _, width := range []int{16, 32, 48, 80} {
		for _, height := range []int{16, 32} {
			for _, atEnd := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/end%t", width, height, atEnd), func(t *testing.T) {
					cur, stride := vaaAVX2GuardedRows(t, height, width, atEnd, true, 11)
					ref, _ := vaaAVX2GuardedRows(t, height, width, atEnd, true, 53)
					count := width / 16 * (height / 16)
					sadBytes, _ := vaaAVX2GuardedRows(t, 1, count*16, atEnd, false, 97)
					frameBytes, _ := vaaAVX2GuardedRows(t, 1, 4, atEnd, false, 29)
					sad := unsafe.Slice((*[4]int32)(unsafe.Pointer(&sadBytes[0])), count)
					frame := (*int32)(unsafe.Pointer(&frameBytes[0]))
					want := make([][4]int32, count)
					var wantFrame int32
					VAACalcSad_c(cur, 0, ref, 0, int32(width), int32(height), int32(stride), &wantFrame, want)
					VAACalcSad_avx2(cur, 0, ref, 0, int32(width), int32(height), int32(stride), frame, sad)
					if *frame != wantFrame || !slices.Equal(sad, want) {
						t.Fatal("checked guarded SAD differs")
					}
					var total int32
					for row := range height / 16 {
						offset, output := row*16*stride, row*(width/16)
						total += vaaSadRowAVX2(&cur[offset], &ref[offset], stride, &sad[output], width/16)
					}
					if total != wantFrame || !slices.Equal(sad, want) {
						t.Fatal("raw guarded SAD differs")
					}
				})
			}
		}
	}
}
