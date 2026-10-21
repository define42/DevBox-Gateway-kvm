package encoder

import "testing"

func BenchmarkReconstructedBlockCopy(b *testing.B) {
	for _, block := range []struct {
		name string
		size int
		copy func([]byte, int, int32, []byte, int, int32)
		rows func([]byte, int, int32, []byte, int, int32)
	}{
		{"16x16", 16, copyReconstructed16x16, benchmarkCopyRows16},
		{"8x8", 8, copyReconstructed8x8, benchmarkCopyRows8},
	} {
		for _, method := range []struct {
			name string
			copy func([]byte, int, int32, []byte, int, int32)
		}{{"Rows", block.rows}, {"Block", block.copy}} {
			b.Run(block.name+"/"+method.name, func(b *testing.B) {
				src, dst := make([]byte, 1984*block.size), make([]byte, 1952*block.size)
				for i := range src {
					src[i] = byte(i)
				}
				b.SetBytes(int64(block.size * block.size))
				b.ReportAllocs()
				for b.Loop() {
					method.copy(dst, 7, 1952, src, 13, 1984)
				}
			})
		}
	}
}

func benchmarkCopyRows16(dst []byte, d int, ds int32, src []byte, s int, ss int32) {
	for range 16 {
		copy(dst[d:d+16], src[s:s+16])
		d += int(ds)
		s += int(ss)
	}
}

func benchmarkCopyRows8(dst []byte, d int, ds int32, src []byte, s int, ss int32) {
	for range 8 {
		copy(dst[d:d+8], src[s:s+8])
		d += int(ds)
		s += int(ss)
	}
}
