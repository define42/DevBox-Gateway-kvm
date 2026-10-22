//go:build amd64

package encoder

import (
	"math/rand/v2"
	"testing"

	"golang.org/x/sys/cpu"
)

func TestDctLumaMbFastParity(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	rng := rand.New(rand.NewPCG(256, 16))
	for test := 0; test < 5000; test++ {
		stride := 16 + rng.IntN(48)
		encOff, predOff := rng.IntN(64), rng.IntN(64)
		enc := make([]byte, encOff+15*stride+16)
		pred := make([]byte, predOff+256)
		for i := range enc {
			enc[i] = byte(rng.Uint32())
		}
		for i := range pred {
			pred[i] = byte(rng.Uint32())
		}
		var got, want [256]int16
		WelsDctFourT4_c(want[:], enc, encOff, int32(stride), pred, predOff, 16)
		WelsDctFourT4_c(want[64:], enc, encOff+8, int32(stride), pred, predOff+8, 16)
		WelsDctFourT4_c(want[128:], enc, encOff+8*stride, int32(stride), pred, predOff+128, 16)
		WelsDctFourT4_c(want[192:], enc, encOff+8*stride+8, int32(stride), pred, predOff+136, 16)
		if !dctLumaMbFast(got[:], enc, encOff, int32(stride), pred, predOff) {
			t.Fatal("valid macroblock rejected")
		}
		if got != want {
			t.Fatalf("test %d stride %d: coefficients differ", test, stride)
		}
	}
	var res [256]int16
	buf := make([]byte, 16*16)
	for _, c := range []struct {
		off, stride, predOff int
	}{{1, 16, 0}, {0, -16, 0}, {0, 16, 1}, {-1, 16, 0}} {
		if dctLumaMbFast(res[:], buf, c.off, int32(c.stride), buf, c.predOff) {
			t.Fatalf("%+v accepted", c)
		}
	}
	if dctLumaMbFast(res[:255], buf, 0, 16, buf, 0) {
		t.Fatal("short output accepted")
	}
}
