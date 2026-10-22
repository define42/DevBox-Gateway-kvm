//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"golang.org/x/sys/cpu"
)

func BenchmarkQuantScanAVX2(b *testing.B) {
	if !cpu.X86.HasAVX2 || !cpu.X86.HasSSSE3 {
		b.Skip("AVX2/SSSE3 unavailable")
	}
	for _, blocks := range []int{4, 16} {
		for _, density := range []string{"Zero", "Sparse", "Dense"} {
			for _, avx2 := range []bool{false, true} {
				b.Run(fmt.Sprintf("blocks%d/%s/AVX2%t", blocks, density, avx2), func(b *testing.B) {
					var input, dct, levels [256]int16
					var maxima [16]int16
					var masks [16]uint16
					ff, mf := g_kiQuantInterFF[20], g_kiQuantMF[20]
					for i := range blocks * 16 {
						if density == "Dense" || (density == "Sparse" && i%17 == 0) {
							input[i] = int16(i*71 - 9000)
						}
					}
					b.ReportAllocs()
					if blocks == 4 {
						if avx2 {
							for b.Loop() {
								copy(dct[:64], input[:64])
								quantScanFour4x4AVX2((*[64]int16)(dct[:]), (*[64]int16)(levels[:]), &ff, &mf, (*[4]int16)(maxima[:]), (*[4]uint16)(masks[:]), 0)
							}
						} else {
							for b.Loop() {
								copy(dct[:64], input[:64])
								quantScanFour4x4SSSE3((*[64]int16)(dct[:]), (*[64]int16)(levels[:]), &ff, &mf, (*[4]int16)(maxima[:]), (*[4]uint16)(masks[:]), 0)
							}
						}
					} else if avx2 {
						for b.Loop() {
							dct = input
							quantScanLumaAVX2(&dct, &levels, &ff, &mf, &maxima, &masks)
						}
					} else {
						for b.Loop() {
							dct = input
							quantScanLumaSSSE3(&dct, &levels, &ff, &mf, &maxima, &masks)
						}
					}
				})
			}
		}
	}
}
