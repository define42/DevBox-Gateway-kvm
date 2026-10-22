package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Benchmark complete sequences of residual blocks, keeping pending bits between
// blocks as the frame writer does. The same benchmark file runs on the unpacked
// baseline; it does not depend on the table's representation.
func BenchmarkCavlcLevelWriter(b *testing.B) {
	patterns := []struct {
		name   string
		values [16]int16
	}{
		{"small", [16]int16{2, -2, 3, -3, 6, -6, 12, -12, 24, -24, 48, -48, 2, -4, 8, -16}},
		{"boundary", [16]int16{63, -64, 64, -65, 62, -63, 65, -66, 63, -64, 64, -65, 62, -63, 65, -66}},
		{"mixed", [16]int16{2, -3, 4, -7, 12, -13, 24, -25, 48, -49, 63, -64, 64, -65, 511, -1023}},
	}
	for _, end := range []int32{14, 15} {
		for _, density := range []struct {
			name   string
			stride int32
		}{{"medium", 2}, {"dense", 1}} {
			for _, pattern := range patterns {
				b.Run(fmt.Sprintf("end%d/%s/%s", end, density.name, pattern.name), func(b *testing.B) {
					var blocks [16][16]int16
					for block := range blocks {
						for index := int32(0); index <= end; index += density.stride {
							blocks[block][index] = pattern.values[(int(index)+block)%len(pattern.values)]
						}
						for i := 0; i < block%4; i++ {
							blocks[block][int(end)-i] = int16(1 - 2*(i&1))
						}
					}
					var functions SWelsFuncPtrList
					var buffer [4096]byte
					var bits common.SBitStringAux
					property := int32(LUMA_4x4)
					if end == 14 {
						property = int32(LUMA_AC)
					}
					common.InitBits(&bits, buffer[:], 0, int32(len(buffer)))
					for block := range blocks {
						if result := WriteBlockResidualCavlc(&functions, blocks[block][:], end, 1, property, int8(block), &bits); result != ENC_RETURN_SUCCESS {
							b.Fatalf("fixture block%d: result%d", block, result)
						}
					}
					b.ReportAllocs()
					for b.Loop() {
						common.InitBits(&bits, buffer[:], 0, int32(len(buffer)))
						for block := range blocks {
							WriteBlockResidualCavlc(&functions, blocks[block][:], end, 1, property, int8(block), &bits)
						}
					}
					b.ReportMetric(float64(len(blocks)), "blocks/op")
				})
			}
		}
	}
}
