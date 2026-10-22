package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type intraDCFixture struct {
	base       *intraReconstructionFixture
	prediction [256]byte
}

func newIntraDCFixture(encStride, recStride int32, qp uint8, content string) *intraDCFixture {
	f := &intraDCFixture{base: newIntraReconstructionFixture(encStride, recStride, qp, content, common.WelsCPUFeatureDetect(nil))}
	f.base.mb.uiMbType = common.MB_TYPE_INTRA16x16
	for i := range f.prediction {
		f.prediction[i] = 128
	}
	f.base.cache.pMemPredLuma = f.prediction[:]
	if content == "DC" {
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				f.base.source[3+y*int(encStride)+x] = 144
			}
		}
	}
	return f
}

var intraDCCallerBenchmarkSink uint8

// Baseline-compatible complete I16 entry: sixteen DCTs, DC Hadamard/quant/scan,
// AC quantization and bookkeeping, inverse DC and reconstruction/copy branches.
// Fixture and source setup are outside the timed loop. Use alongside raw DC
// timings so a faster transform is not mistaken for a faster production caller.
func BenchmarkI16Reconstruction(b *testing.B) {
	for _, strides := range [][2]int32{{64, 64}, {1920, 1984}} {
		for _, qp := range []uint8{8, 20, 32} {
			for _, content := range []string{"Zero", "DC", "Sparse", "Dense"} {
				b.Run(fmt.Sprintf("%s/QP%d/Stride%d_%d", content, qp, strides[0], strides[1]), func(b *testing.B) {
					f := newIntraDCFixture(strides[0], strides[1], qp, content)
					b.ReportAllocs()
					for b.Loop() {
						f.base.mb.uiCbp = 0x30
						WelsEncRecI16x16Y(&f.base.ctx, &f.base.mb, &f.base.cache)
					}
					b.ReportMetric(1, "macroblocks/op")
					intraDCCallerBenchmarkSink = f.base.mb.uiCbp
				})
			}
		}
	}
}
