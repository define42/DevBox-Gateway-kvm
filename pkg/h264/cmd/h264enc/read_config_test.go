package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/console"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/encoder"
)

func configFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.cfg")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func configReader(t *testing.T, contents string) *console.CReadConfig {
	t.Helper()
	r := console.NewCReadConfig(configFile(t, contents))
	t.Cleanup(r.Close)
	return r
}

func checkFields(t *testing.T, value any, want map[string]any) {
	t.Helper()
	v := reflect.ValueOf(value)
	for name, expected := range want {
		got := v.FieldByName(name).Interface()
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("%s = %v, want %v", name, got, expected)
		}
	}
}

func TestSPSPPSIDStrategy(t *testing.T) {
	t.Parallel()
	for value, want := range map[int32]api.EParameterSetStrategy{
		0: api.CONSTANT_ID, 1: api.INCREASING_ID, 2: api.SPS_LISTING,
		3: api.SPS_LISTING_AND_PPS_INCREASING, 6: api.SPS_PPS_LISTING, -1: api.CONSTANT_ID, 4: api.CONSTANT_ID,
	} {
		t.Run(fmt.Sprintf("strategy %d", value), func(t *testing.T) {
			t.Parallel()
			if got := spsPpsIdStrategy(value); got != want {
				t.Errorf("strategy(%d) = %v, want %v", value, got, want)
			}
		})
	}
}

func TestParseLayerConfig(t *testing.T) {
	t.Parallel()
	r := configReader(t, `# layer settings
FrameWidth 640
FrameHeight 360
FrameRateOut 29.5
ReconFile reconstructed.yuv
ProfileIdc 66
FRExt 1
SpatialBitrate 512
MaxSpatialBitrate 768
InitialQP 24
SliceMode 2
SliceSize 1000
SliceNum 2
SlicesAssign0 20
SlicesAssign1 30
SlicesAssign-1 999
SlicesAssign999 999
UnknownOption ignored
`)
	p := api.SEncParamExt{IRCMode: api.RC_BITRATE_MODE, ITargetBitrate: 1_000_000}
	var files SFilesSet
	if got := ParseLayerConfig(r, 1, &p, &files); got != 0 {
		t.Fatalf("ParseLayerConfig = %d", got)
	}
	want := api.SSpatialLayerConfig{
		IVideoWidth: 640, IVideoHeight: 360, FFrameRate: 29.5, UiProfileIdc: api.PRO_BASELINE,
		ISpatialBitrate: 512_000, IMaxSpatialBitrate: 768_000, IDLayerQp: 24,
		SSliceArgument: api.SSliceArgument{UiSliceMode: api.SM_RASTER_SLICE, UiSliceSizeConstraint: 1000, UiSliceNum: 2},
	}
	want.SSliceArgument.UiSliceMbNum[0], want.SSliceArgument.UiSliceMbNum[1] = 20, 30
	if !reflect.DeepEqual(p.SSpatialLayers[1], want) || files.sRecFileName[1] != "reconstructed.yuv" {
		t.Fatalf("parsed layer = %+v, file=%q, want %+v", p.SSpatialLayers[1], files.sRecFileName[1], want)
	}
	if p.SSpatialLayers[0] != (api.SSpatialLayerConfig{}) {
		t.Fatal("parsing layer 1 modified layer 0")
	}
}

func TestParseLayerConfigInvalid(t *testing.T) {
	t.Parallel()
	for _, contents := range []string{
		"ReconFile " + strings.Repeat("x", common.MAX_FNAME_LEN),
		"SpatialBitrate 0", "SpatialBitrate 1001", "MaxSpatialBitrate -1",
		"SpatialBitrate 500\nMaxSpatialBitrate 499",
	} {
		t.Run(contents[:min(len(contents), 40)], func(t *testing.T) {
			t.Parallel()
			p := api.SEncParamExt{IRCMode: api.RC_BITRATE_MODE, ITargetBitrate: 1_000_000}
			if got := ParseLayerConfig(configReader(t, contents), 0, &p, &SFilesSet{}); got != -1 {
				t.Fatalf("invalid layer config returned %d, want -1", got)
			}
		})
	}
	if got := ParseLayerConfig(console.NewCReadConfig(""), 0, &api.SEncParamExt{}, &SFilesSet{}); got != 1 {
		t.Fatalf("missing config returned %d, want 1", got)
	}
	p := api.SEncParamExt{IRCMode: api.RC_OFF_MODE}
	if got := ParseLayerConfig(configReader(t, "SpatialBitrate 0\nMaxSpatialBitrate 0"), 0, &p, &SFilesSet{}); got != 0 {
		t.Fatalf("disabled rate control rejected zero bitrates: %d", got)
	}
}

func TestParseConfig(t *testing.T) {
	t.Parallel()
	layerPath := configFile(t, "FrameWidth 320\nFrameHeight 180\nSpatialBitrate 256\n")
	r := configReader(t, `# main settings
UsageType 1
SimulcastAVC 1
SourceWidth 640
SourceHeight 360
InputFile input.yuv
InputFile
OutputFile output.264
MaxFrameRate 29.5
FramesToBeEncoded 17
TemporalLayerNum 2
IntraPeriod 32
MaxNalSize 1400
SpsPpsIDStrategy 6
EnableMultiBsFile 1
EnableScalableSEI 1
EnableFrameCropping 1
EntropyCodingModeFlag 2
ComplexityMode 2
LoopFilterDisableIDC 1
LoopFilterAlphaC0Offset -9
LoopFilterBetaOffset 9
MultipleThreadIdc 999
UseLoadBalancing 1
RCMode 1
TargetBitrate 1000
MaxOverallBitrate 2000
MaxQp 40
MinQp 10
EnableDenoise 1
EnableSceneChangeDetection 1
EnableBackgroundDetection 1
EnableAdaptiveQuantization 1
EnableFrameSkip 1
EnableLongTermReference 1
LongTermReferenceNumber 2
LtrMarkPeriod 30
LosslessLink 1
NumLayers 2
PrefixNALAddingCtrl 9
UnknownSetting ignored
LayerCfg `+layerPath+"\n")
	var picture api.SSourcePicture
	var p api.SEncParamExt
	var files SFilesSet
	if got := ParseConfig(r, &picture, &p, &files); got != 0 {
		t.Fatalf("ParseConfig = %d", got)
	}
	checkFields(t, p, map[string]any{
		"IUsageType": api.SCREEN_CONTENT_REAL_TIME, "BSimulcastAVC": true,
		"FMaxFrameRate": float32(29.5), "ITemporalLayerNum": int32(2), "UiIntraPeriod": uint32(32),
		"UiMaxNalSize": uint32(1400), "ESpsPpsIdStrategy": api.SPS_PPS_LISTING,
		"BEnableSSEI": true, "BEnableFrameCroppingFlag": true, "IEntropyCodingModeFlag": int32(1),
		"IComplexityMode": api.HIGH_COMPLEXITY, "ILoopFilterDisableIdc": int32(1),
		"ILoopFilterAlphaC0Offset": int32(-6), "ILoopFilterBetaOffset": int32(6),
		"IMultipleThreadIdc": uint16(encoder.MAX_THREADS_NUM), "BUseLoadBalancing": true,
		"IRCMode": api.RC_BITRATE_MODE, "ITargetBitrate": int32(1_000_000), "IMaxBitrate": int32(2_000_000),
		"IMaxQp": int32(40), "IMinQp": int32(10), "BEnableDenoise": true,
		"BEnableSceneChangeDetect": true, "BEnableBackgroundDetection": true,
		"BEnableAdaptiveQuant": true, "BEnableFrameSkip": true, "BEnableLongTermReference": true,
		"ILTRRefNum": int32(2), "ILtrMarkPeriod": uint32(30), "BIsLosslessLink": true,
		"ISpatialLayerNum": int32(1), "BPrefixNalAddingCtrl": true,
	})
	if picture.IPicWidth != 640 || picture.IPicHeight != 360 || files.strSeqFile != "input.yuv" ||
		files.strBsFile != "output.264" || files.uiFrameToBeCoded != 17 || !files.bEnableMultiBsFile ||
		files.strLayerCfgFile[0] != layerPath || p.SSpatialLayers[0].IVideoWidth != 320 {
		t.Fatalf("incorrect source, files or layer settings: picture=%+v files=%+v layer=%+v", picture, files, p.SSpatialLayers[0])
	}
}

func TestParseConfigValidation(t *testing.T) {
	t.Parallel()
	for _, contents := range []string{
		"LoopFilterDisableIDC -1", "LoopFilterDisableIDC 7", "TargetBitrate 0",
		"MaxOverallBitrate -1", "NumLayers 0", "NumLayers 5",
		"NumLayers 1\nLayerCfg " + configFile(t, "SpatialBitrate -1"),
	} {
		t.Run(contents[:min(len(contents), 40)], func(t *testing.T) {
			t.Parallel()
			if got := ParseConfig(configReader(t, contents), &api.SSourcePicture{}, &api.SEncParamExt{}, &SFilesSet{}); got != 1 {
				t.Fatalf("invalid config returned %d, want 1", got)
			}
		})
	}
	for _, offset := range []int{-10, 3, 10} {
		t.Run(fmt.Sprintf("offset %d", offset), func(t *testing.T) {
			t.Parallel()
			var p api.SEncParamExt
			contents := fmt.Sprintf("LoopFilterAlphaC0Offset %d\nLoopFilterBetaOffset %d\nPrefixNALAddingCtrl -1\nEntropyCodingModeFlag 0", offset, offset)
			if got := ParseConfig(configReader(t, contents), &api.SSourcePicture{}, &p, &SFilesSet{}); got != 0 {
				t.Fatalf("valid config returned %d", got)
			}
			want := int32(max(-6, min(6, offset)))
			if p.ILoopFilterAlphaC0Offset != want || p.ILoopFilterBetaOffset != want || p.BPrefixNalAddingCtrl || p.IEntropyCodingModeFlag != 0 {
				t.Fatalf("incorrect clamping: %+v", p)
			}
		})
	}
}
