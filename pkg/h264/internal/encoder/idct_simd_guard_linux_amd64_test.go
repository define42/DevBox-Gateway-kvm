//go:build linux && amd64

package encoder

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Place the last valid byte immediately before an unreadable guard page.
func idctGuardedBuffer(t *testing.T, size int) []byte {
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

func TestIDctSIMDGuardPages(t *testing.T) {
	for _, tc := range []struct {
		size, coeffs int
		fn           PIDctFunc
	}{
		{4, 16, WelsIDctT4Rec_sse2}, {8, 64, WelsIDctFourT4Rec_sse2}, {16, 16, WelsIDctRecI16x16Dc_sse2},
	} {
		pred := idctGuardedBuffer(t, (tc.size-1)*19+tc.size)
		rec := idctGuardedBuffer(t, (tc.size-1)*23+tc.size)
		coeffBytes := idctGuardedBuffer(t, tc.coeffs*2)
		coeff := unsafe.Slice((*int16)(unsafe.Pointer(&coeffBytes[0])), tc.coeffs)
		for i := range pred {
			pred[i] = 128
		}
		for i := range coeff {
			coeff[i] = int16(i*701 - 20000)
		}
		tc.fn(rec, 0, 23, pred, 0, 19, coeff)
	}
}
