package avc

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

func checkSoftwareFrameRate(t *testing.T, encoder *Encoder, want int) {
	t.Helper()
	codec, ok := encoder.codec.(*openH264Encoder)
	if !ok {
		t.Fatalf("codec type = %T, want software", encoder.codec)
	}
	var effective api.SEncParamExt
	if rv := codec.codec.Raw().GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &effective); rv != 0 {
		t.Fatalf("GetOption returned %d", rv)
	}
	if encoder.FrameRate() != want || effective.FMaxFrameRate != float32(want) ||
		effective.SSpatialLayers[0].FFrameRate != float32(want) {
		t.Fatalf("frame rates: encoder %d, codec %v, layer %v; want %d",
			encoder.FrameRate(), effective.FMaxFrameRate, effective.SSpatialLayers[0].FFrameRate, want)
	}
}

func checkSoftwareQP(t *testing.T, encoder *Encoder, want int) {
	t.Helper()
	codec, ok := encoder.codec.(*openH264Encoder)
	if !ok {
		t.Fatalf("codec type = %T, want software", encoder.codec)
	}
	var effective api.SEncParamExt
	var statistics api.SEncoderStatistics
	if rv := codec.codec.Raw().GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &effective); rv != 0 {
		t.Fatalf("read effective settings: %d", rv)
	}
	if rv := codec.codec.Raw().GetOption(api.ENCODER_OPTION_GET_STATISTICS, &statistics); rv != 0 {
		t.Fatalf("read encoded-frame statistics: %d", rv)
	}
	if encoder.QP() != want || effective.SSpatialLayers[0].IDLayerQp != int32(want) ||
		statistics.UiAverageFrameQP != uint32(want) || statistics.UiInputFrameCount == 0 {
		t.Fatalf("QP: configured=%d layer=%d actual=%d frames=%d, want %d",
			encoder.QP(), effective.SSpatialLayers[0].IDLayerQp, statistics.UiAverageFrameQP,
			statistics.UiInputFrameCount, want)
	}
}

func checkSoftwarePolicy(t *testing.T, encoder *Encoder) {
	t.Helper()
	codec, ok := encoder.codec.(*openH264Encoder)
	if !ok {
		t.Fatalf("codec type = %T", encoder.codec)
	}
	var effective api.SEncParamExt
	if rv := codec.codec.Raw().GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &effective); rv != 0 {
		t.Fatalf("GetOption returned %d", rv)
	}
	if got, want := effective.IMultipleThreadIdc, uint16(encoder.Threads()); got != want {
		t.Fatalf("codec workers = %d, slices = %d", got, want)
	}
	if got := effective.UiIntraPeriod; got != codecIntraPeriod {
		t.Fatalf("codec intra period = %d, want %d", got, codecIntraPeriod)
	}
	if got := effective.ILoopFilterDisableIdc; got != codecLoopFilterDisableIDC {
		t.Fatalf("codec loop-filter disable IDC = %d, want %d", got, codecLoopFilterDisableIDC)
	}
}

// Pictures used with this helper are large enough to retain every requested slice.
func expectedSoftwareSlices(requested int) int { return requested }
