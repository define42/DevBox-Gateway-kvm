//go:build amd64

package encoder

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"golang.org/x/sys/cpu"
)

func TestQuantScanAVX2MatchesSSSE3(t *testing.T) {
	if !cpu.X86.HasAVX2 || !cpu.X86.HasSSSE3 {
		t.Skip("AVX2/SSSE3 unavailable")
	}
	rng := rand.New(rand.NewPCG(264, 216))
	edges := [...]int16{-32768, -32767, -1, 0, 1, 16383, 16384, 32766, 32767}
	for _, blocks := range []int{4, 16} {
		for _, ac := range []bool{false, true} {
			if blocks == 16 && ac {
				continue
			}
			t.Run(fmt.Sprintf("blocks%d/AC%t", blocks, ac), func(t *testing.T) {
				for fixture := range 1024 {
					var gotDCT, gotLevels [260]int16
					var gotMax [20]int16
					var gotMask [20]uint16
					var ff, mf [8]int16
					for i := range ff {
						switch fixture % 4 {
						case 0:
							ff[i], mf[i] = g_kiQuantInterFF[fixture%52][i], g_kiQuantMF[fixture%52][i]
						case 1:
							ff[i], mf[i] = int16(rng.Uint32()), int16(rng.Uint32())
						case 2:
							ff[i], mf[i] = edges[(fixture+i)%len(edges)], edges[(fixture/4+i)%len(edges)]
						case 3:
							// One lane can have a zero signed maximum despite
							// nonzero raster values while the other is populated.
							ff[i], mf[i] = 20000, -1
						}
					}
					for i := range gotDCT {
						gotDCT[i], gotLevels[i] = -12345, 23456
					}
					for i := range blocks * 16 {
						gotDCT[i+2] = int16(fixture*64 + i)
						if fixture%4 == 3 {
							gotDCT[i+2] = 0
							if (i/16+fixture/4)%2 == 0 {
								gotDCT[i+2] = 32767
							}
						}
					}
					for i := range gotMax {
						gotMax[i], gotMask[i] = -23456, 0xbeef
					}
					wantDCT, wantLevels, wantMax, wantMask := gotDCT, gotLevels, gotMax, gotMask
					if blocks == 4 {
						acFlag := int32(0)
						if ac {
							acFlag = 1
						}
						quantScanFour4x4SSSE3((*[64]int16)(wantDCT[2:66]), (*[64]int16)(wantLevels[2:66]), &ff, &mf, (*[4]int16)(wantMax[2:6]), (*[4]uint16)(wantMask[2:6]), acFlag)
						quantScanFour4x4AVX2((*[64]int16)(gotDCT[2:66]), (*[64]int16)(gotLevels[2:66]), &ff, &mf, (*[4]int16)(gotMax[2:6]), (*[4]uint16)(gotMask[2:6]), acFlag)
					} else {
						quantScanLumaSSSE3((*[256]int16)(wantDCT[2:258]), (*[256]int16)(wantLevels[2:258]), &ff, &mf, (*[16]int16)(wantMax[2:18]), (*[16]uint16)(wantMask[2:18]))
						quantScanLumaAVX2((*[256]int16)(gotDCT[2:258]), (*[256]int16)(gotLevels[2:258]), &ff, &mf, (*[16]int16)(gotMax[2:18]), (*[16]uint16)(gotMask[2:18]))
					}
					if gotDCT != wantDCT || gotLevels != wantLevels || gotMax != wantMax || gotMask != wantMask {
						t.Fatalf("fixture %d: raster, levels, maxima, masks or canaries differ", fixture)
					}
				}
			})
		}
	}
}
