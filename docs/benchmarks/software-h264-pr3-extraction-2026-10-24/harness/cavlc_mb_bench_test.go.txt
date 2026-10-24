package encoder

import (
	"math/rand/v2"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// This harness also compiles on the pre-batching baseline. Each operation
// encodes one complete macroblock, cycling through 16 deterministic fixtures.
// The nonzero cache matches the coefficients, including known-zero AC blocks.
func BenchmarkCavlcMacroblock(b *testing.B) {
	for _, kind := range []struct {
		name   string
		mbType Mb_Type
	}{{"inter", common.MB_TYPE_16x16}, {"intra16", common.MB_TYPE_INTRA16x16}} {
		for _, density := range []string{"dense", "mixed", "sparse", "zero"} {
			b.Run(kind.name+"/"+density, func(b *testing.B) {
				rng := rand.New(rand.NewPCG(264, 65536))
				var dcts [16]SDCTCoeff
				var caches [16]SMbCache
				mb := SMB{uiMbType: kind.mbType, uiCbp: 0x2f}
				fill := func(coeff []int16, block int) int8 {
					var count int8
					for i := range coeff {
						if density == "zero" || density == "sparse" && (block%3 == 0 || rng.IntN(8) != 0) ||
							density == "mixed" && (block%5 == 0 || rng.IntN(3) == 0) {
							continue
						}
						coeff[i] = int16(1 + rng.IntN(12))
						if rng.IntN(2) != 0 {
							coeff[i] = -coeff[i]
						}
						count++
					}
					return count
				}
				for fixture := range dcts {
					dct, cache := &dcts[fixture], &caches[fixture]
					cache.pDct = dct
					for i := range cache.iNonZeroCoeffCount {
						cache.iNonZeroCoeffCount[i] = -1
					}
					lumaCount := 16
					if kind.mbType == common.MB_TYPE_INTRA16x16 {
						lumaCount = 15
					}
					for block := 0; block < 16; block++ {
						count := fill(dct.iLumaBlock[block*16:block*16+lumaCount], block+fixture)
						cache.iNonZeroCoeffCount[common.G_kuiCache48CountScan4Idx[block]] = count
					}
					fill(dct.iLumaI16x16Dc[:], fixture)
					for plane := 0; plane < 2; plane++ {
						fill(dct.iChromaDc[plane][:], fixture+plane)
						for block := 0; block < 4; block++ {
							offset := (plane*4 + block) * 16
							count := fill(dct.iChromaBlock[offset:offset+15], fixture+plane*4+block)
							idx := 24*plane + int(common.G_kuiCache48CountScan4Idx[16+block])
							cache.iNonZeroCoeffCount[idx] = count
						}
					}
				}
				var functions SWelsFuncPtrList
				InitCoeffFunc(&functions, 0, 0)
				var buffer [4096]byte
				var bits common.SBitStringAux
				for fixture := range caches {
					common.InitBits(&bits, buffer[:], 0, int32(len(buffer)))
					if result := WelsWriteMbResidual(&functions, &caches[fixture], &mb, &bits); result != ENC_RETURN_SUCCESS {
						b.Fatalf("fixture%d: result%d", fixture, result)
					}
				}
				b.ReportAllocs()
				fixture := 0
				for b.Loop() {
					common.InitBits(&bits, buffer[:], 0, int32(len(buffer)))
					common.BsWriteBits(&bits, 7, 0x5b)
					WelsWriteMbResidual(&functions, &caches[fixture&15], &mb, &bits)
					fixture++
				}
			})
		}
	}
}
