package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func cavlcExtractFixtures() []struct {
	name  string
	coeff [16]int16
} {
	return []struct {
		name  string
		coeff [16]int16
	}{
		{name: "zero"},
		{name: "dc", coeff: [16]int16{7}},
		{name: "sparse-low", coeff: [16]int16{7, 0, -2, 1}},
		{name: "sparse-wide", coeff: [16]int16{7, 0, -2, 0, 0, 1, 0, 0, 0, -1, 0, 0, 0, 0, 0, 1}},
		{name: "medium", coeff: [16]int16{4, 0, 1, 0, 9, 0, 2, 0, -5, 0, 1, 0, 2, 0, -1}},
		{name: "nearly-dense", coeff: [16]int16{4, -2, 1, 0, 9, -3, 2, 0, -5, 7, 1, 0, 2, 3, -1}},
		{name: "one-zero", coeff: [16]int16{4, -2, 1, -1, 9, -3, 2, 0, -5, 7, 1, 1, 2, 3, -1, 1}},
		{name: "dense", coeff: [16]int16{4, -2, 1, -1, 9, -3, 2, 1, -5, 7, 1, 1, 2, 3, -1, 1}},
	}
}

func BenchmarkCavlcExtraction(b *testing.B) {
	for _, end := range []int32{3, 14, 15} {
		for _, fixture := range cavlcExtractFixtures() {
			for _, scalar := range []bool{true, false} {
				b.Run(fmt.Sprintf("end%d/%s/scalar=%t", end, fixture.name, scalar), func(b *testing.B) {
					var levels [16]int16
					var runs [16]uint8
					var count int32
					b.ReportAllocs()
					if scalar {
						for b.Loop() {
							CavlcParamCal_c(fixture.coeff[:], runs[:], levels[:], &count, end)
						}
					} else {
						for b.Loop() {
							cavlcExtract(fixture.coeff[:], &runs, &levels, &count, end)
						}
					}
				})
			}
		}
	}
}

func BenchmarkCavlcExtractionResidual(b *testing.B) {
	for _, end := range []int32{3, 14, 15} {
		for _, fixture := range cavlcExtractFixtures() {
			for _, scalar := range []bool{true, false} {
				b.Run(fmt.Sprintf("end%d/%s/scalar=%t", end, fixture.name, scalar), func(b *testing.B) {
					var functions SWelsFuncPtrList
					var buffer [512]byte
					var bits common.SBitStringAux
					property, nc := int32(LUMA_4x4), int8(3)
					if end == 3 {
						property, nc = int32(CHROMA_DC), CHROMA_DC_NC_OFFSET
					}
					b.ReportAllocs()
					if scalar {
						for b.Loop() {
							common.InitBits(&bits, buffer[:], 0, int32(len(buffer)))
							writeBlockResidualCavlcScalarExtraction(&functions, fixture.coeff[:], end, 1, property, nc, &bits)
						}
					} else {
						for b.Loop() {
							common.InitBits(&bits, buffer[:], 0, int32(len(buffer)))
							WriteBlockResidualCavlc(&functions, fixture.coeff[:], end, 1, property, nc, &bits)
						}
					}
				})
			}
		}
	}
}
