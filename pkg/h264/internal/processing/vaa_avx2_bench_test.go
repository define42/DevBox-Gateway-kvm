package processing

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var vaaActiveBenchmarkSink int32

// Copy this file alone to frozen round9: it only uses existing public strategy
// entries, so it measures the dispatch actually selected on each tree. Set and
// Process are timed, including borrowed-mask setup/release, validation and all
// row commits. Construction, pixels and result allocation are outside timing.
func BenchmarkVAAActiveCaller(b *testing.B) {
	for _, size := range []struct{ width, height, stride int32 }{
		{1920, 1088, 1984}, {1280, 720, 1344}, {1904, 1088, 1984}, {48, 32, 64}, {16, 16, 32},
	} {
		for _, mode := range []string{"FullNoise", "FullEqual", "AllChangedMask", "AlternatingMask", "SparseOddRun", "AllUnchangedMask"} {
			b.Run(fmt.Sprintf("%dx%d/stride%d/%s", size.width, size.height, size.stride, mode), func(b *testing.B) {
				f := newVAAActiveBenchmarkFixture(size.width, size.height, size.stride, mode)
				b.SetBytes(int64(size.width) * int64(size.height))
				b.ReportAllocs()
				for b.Loop() {
					f.calc.Set(0, &f.param)
					f.calc.Process(0, &f.current, &f.reference)
				}
				vaaActiveBenchmarkSink = f.result.IFrameSad + f.result.PSad8x8[len(f.result.PSad8x8)-1][3]
			})
		}
	}
}

type vaaActiveBenchmarkFixture struct {
	calc               *CVAACalculation
	param              SVAACalcParam
	result             SVAACalcResult
	current, reference SPixMap
}

func newVAAActiveBenchmarkFixture(width, height, stride int32, mode string) *vaaActiveBenchmarkFixture {
	f := new(vaaActiveBenchmarkFixture)
	f.calc = NewCVAACalculation(int32(common.WelsCPUFeatureDetect(nil)))
	count := int(width>>4) * int(height>>4)
	f.result.PSad8x8 = make([][4]int32, count)
	f.param.PCalcResult = &f.result
	const offset = 13
	span := int(stride)*int(height-1) + int(width)
	cur, ref := make([]byte, offset+span), make([]byte, offset+span)
	for i := range cur {
		cur[i] = byte(i*71 + 17)
		ref[i] = byte(i*37 + 91)
	}
	if mode == "FullEqual" {
		copy(ref, cur)
	}
	if mode != "FullNoise" && mode != "FullEqual" {
		f.param.UnchangedMacroblocks = make([]bool, count)
		mbWidth := int(width >> 4)
		for mb := range count {
			equal := false
			switch mode {
			case "AlternatingMask":
				equal = mb%2 == 0
			case "SparseOddRun":
				equal = mb/mbWidth != int(height>>5) || mb%mbWidth < mbWidth/2 || mb%mbWidth >= mbWidth/2+3
			case "AllUnchangedMask":
				equal = true
			}
			f.param.UnchangedMacroblocks[mb] = equal
			if equal {
				for y := range 16 {
					start := offset + (mb/mbWidth*16+y)*int(stride) + mb%mbWidth*16
					copy(ref[start:start+16], cur[start:start+16])
				}
			}
		}
	}
	f.current = SPixMap{PPixel: [3][]uint8{cur}, IPixelOff: [3]int{offset}, IStride: [3]int32{stride}, SRect: SRect{IRectWidth: width, IRectHeight: height}}
	f.reference = f.current
	f.reference.PPixel[0] = ref
	return f
}
