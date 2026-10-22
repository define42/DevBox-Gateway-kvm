package encoder

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Exercise the benchmark's complete I4 path through production callbacks and
// compare every mutable buffer against scalar transforms, for all QPs.
func TestI4ReconstructionTransformParity(t *testing.T) {
	for _, strides := range [][2]int32{{16, 16}, {23, 29}, {1920, 1984}} {
		for _, content := range []string{"Zero", "Sparse", "Dense"} {
			t.Run(fmt.Sprintf("%s/Stride%d_%d", content, strides[0], strides[1]), func(t *testing.T) {
				for qp := uint8(0); qp <= 51; qp++ {
					got := newIntraReconstructionFixture(strides[0], strides[1], qp, content, common.WelsCPUFeatureDetect(nil))
					want := newIntraReconstructionFixture(strides[0], strides[1], qp, content, 0)
					got.ctx.uiTemporalId, want.ctx.uiTemporalId = qp%2, qp%2
					for block := uint8(0); block < 16; block++ {
						WelsEncRecI4x4Y(&got.ctx, &got.mb, &got.cache, block)
						WelsEncRecI4x4Y(&want.ctx, &want.mb, &want.cache, block)
						if got.mb.uiCbp != want.mb.uiCbp || got.nonzero != want.nonzero || got.coefficients != want.coefficients || got.dct != want.dct ||
							got.prediction != want.prediction || !bytes.Equal(got.source, want.source) || !bytes.Equal(got.reconstructed, want.reconstructed) {
							t.Fatalf("QP=%d block=%d: reconstruction state differs from scalar", qp, block)
						}
					}
					if content == "Zero" && got.mb.uiCbp != 0x30 {
						t.Fatalf("QP=%d: zero residual unexpectedly set luma CBP", qp)
					}
					if content == "Dense" && got.mb.uiCbp&15 == 0 {
						t.Fatalf("QP=%d: dense fixture did not exercise IDCT", qp)
					}
				}
			})
		}
	}
}
