//go:build amd64

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

func initSampleHalfSatdPairSIMD(samples *SSampleDealingFunc, flags uint32) {
	const required = common.WELS_CPU_SSE41 | common.WELS_CPU_SSSE3 | common.WELS_CPU_AVX2
	samples.bSampleHalfSatdPair = flags&required == required
}

// This is Go-specific fusion: native OpenH264 calls SATD separately for each
// half-pixel candidate. Only encoded horizontal pair sums/differences are shared;
// each prediction still gets the original residual butterflies and reduction.
//
//go:noescape
func satdTwoAVX2(enc *byte, encStride int, first, second *byte, predStride, width, height int) (firstCost, secondCost int32)

// Validate after each filter, never across both axes: filters and aliased
// scratch buffers can change inputs. The vertical union has height+1 rows;
// the horizontal union has width+1 visible bytes per row. No input is written,
// so read-only aliases are valid. Failed checks return to the original callbacks
// in their original order, including any MVD lookup between the two scores.
func sampleHalfSatdPair(enc []byte, encOff int, encStride int32, prediction []byte,
	secondOff int, width, height int32) (int32, int32, bool) {
	if (width != 8 && width != 16) || (height != 8 && height != 16) ||
		(secondOff != 1 && secondOff != ME_REFINE_BUF_STRIDE) || encStride < 0 || encOff < 0 {
		return 0, 0, false
	}
	encSpan := int64(height-1)*int64(encStride) + int64(width)
	predSpan := int64(height-1)*ME_REFINE_BUF_STRIDE + int64(width) + int64(secondOff)
	if int64(encOff) > int64(len(enc))-encSpan || predSpan > int64(len(prediction)) {
		return 0, 0, false
	}
	first, second := satdTwoAVX2(&enc[encOff], int(encStride), &prediction[0], &prediction[secondOff],
		ME_REFINE_BUF_STRIDE, int(width), int(height))
	return first, second, true
}
