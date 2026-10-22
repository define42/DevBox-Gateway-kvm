package encoder

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"
)

func TestCavlcBitWriterFlushParity(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(264, 32))
	for left := int32(1); left <= 32; left++ {
		for n := int32(0); n <= 32; n++ {
			for trial := 0; trial < 16; trial++ {
				var gotBuffer, wantBuffer [16]byte
				bits, value := rng.Uint32(), int32(rng.Uint32())
				got := sCavlcBs{pBuf: gotBuffer[:], pBufPtr: 4, uiCurBits: bits, iLeftBits: left}
				want := cavlcWriterReference{sCavlcBs{pBuf: wantBuffer[:], pBufPtr: 4, uiCurBits: bits, iLeftBits: left}}
				got.write(n, value)
				want.write(n, value)
				if got.pBufPtr != want.pBufPtr || got.uiCurBits != want.uiCurBits || got.iLeftBits != want.iLeftBits || !bytes.Equal(gotBuffer[:], wantBuffer[:]) {
					t.Fatalf("left=%d n=%d value=%x: writer state differs", left, n, value)
				}
			}
		}
	}
}

func BenchmarkCavlcBitWriter(b *testing.B) {
	for _, dense := range []bool{false, true} {
		var lengths [128]int32
		for i := range lengths {
			lengths[i] = int32(1 + i%7)
			if dense {
				lengths[i] = int32(5 + i%15)
			}
		}
		for _, prior := range []bool{true, false} {
			b.Run(fmt.Sprintf("dense=%t/prior=%t", dense, prior), func(b *testing.B) {
				var buffer [512]byte
				b.ReportAllocs()
				for b.Loop() {
					if prior {
						writer := cavlcWriterReference{sCavlcBs{pBuf: buffer[:], iLeftBits: 32}}
						for _, n := range lengths {
							writer.write(n, 1)
						}
					} else {
						writer := sCavlcBs{pBuf: buffer[:], iLeftBits: 32}
						for _, n := range lengths {
							writer.write(n, 1)
						}
					}
				}
			})
		}
	}
}
