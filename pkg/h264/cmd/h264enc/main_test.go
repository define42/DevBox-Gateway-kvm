package main

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/decoder"
)

// Tests using captureOutput run serially because stdout is package-global.
func captureOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := stdout
	var output bytes.Buffer
	stdout = bufio.NewWriter(&output)
	t.Cleanup(func() { _ = stdout.Flush(); stdout = previous })
	return &output
}

func TestParseCommandLine(t *testing.T) {
	// Parsing -trace modifies the package-global trace level.
	previous := g_LevelSetting
	t.Cleanup(func() { g_LevelSetting = previous })
	var picture api.SSourcePicture
	var p api.SEncParamExt
	var files SFilesSet
	args := strings.Fields(`-bf output.264 -org input.yuv -sw 640 -sh 360 -utype 1 -savc 1
-frms 17 -frin 29.5 -numtl 2 -mfile 1 -iper 32 -nalsize 1400 -spsid 3 -cabac 1 -complexity 2
-denois 1 -scene 1 -bgd 1 -aq 1 -fs 1 -fixrc 1 -idrBitrateRatio 400 -ltr 1 -ltrnum 2 -ltrper 30
-threadIdc 4 -loadbalancing 1 -deblockIdc 1 -alphaOffset -3 -betaOffset 3 -rc 1 -trace 0
-tarb 1000 -maxbrTotal 2000 -maxqp 40 -minqp 10 -numl 1 -dprofile 0 66 -drec 0 reconstructed.yuv
-dw 0 320 -dh 0 180 -frout 0 15.5 -lqp 0 24 -ltarb 0 256 -lmaxb 0 512
-slcmd 0 2 -slcsize 0 1000 -slcnum 0 2 -slcmbnum 0 20 -unknown ignored`)
	if got := ParseCommandLine(args, &picture, &p, &files); got != 0 {
		t.Fatalf("ParseCommandLine = %d", got)
	}
	checkFields(t, p, map[string]any{
		"IUsageType": api.SCREEN_CONTENT_REAL_TIME, "BSimulcastAVC": true,
		"FMaxFrameRate": float32(29.5), "ITemporalLayerNum": int32(2), "UiIntraPeriod": uint32(32),
		"UiMaxNalSize": uint32(1400), "ESpsPpsIdStrategy": api.SPS_LISTING_AND_PPS_INCREASING,
		"IEntropyCodingModeFlag": int32(1), "IComplexityMode": api.HIGH_COMPLEXITY,
		"BEnableDenoise": true, "BEnableSceneChangeDetect": true, "BEnableBackgroundDetection": true,
		"BEnableAdaptiveQuant": true, "BEnableFrameSkip": true, "BFixRCOverShoot": true,
		"IIdrBitrateRatio": int32(400), "BEnableLongTermReference": true, "ILTRRefNum": int32(2),
		"ILtrMarkPeriod": uint32(30), "IMultipleThreadIdc": uint16(4), "BUseLoadBalancing": true,
		"ILoopFilterDisableIdc": int32(1), "ILoopFilterAlphaC0Offset": int32(-3), "ILoopFilterBetaOffset": int32(3),
		"IRCMode": api.RC_BITRATE_MODE, "ITargetBitrate": int32(1_000_000), "IMaxBitrate": int32(2_000_000),
		"IMaxQp": int32(40), "IMinQp": int32(10), "ISpatialLayerNum": int32(1),
	})
	wantLayer := api.SSpatialLayerConfig{IVideoWidth: 320, IVideoHeight: 180, FFrameRate: 15.5,
		UiProfileIdc: api.PRO_BASELINE, IDLayerQp: 24, ISpatialBitrate: 256_000, IMaxSpatialBitrate: 512_000,
		SSliceArgument: api.SSliceArgument{UiSliceMode: api.SM_RASTER_SLICE, UiSliceSizeConstraint: 1000, UiSliceNum: 2}}
	wantLayer.SSliceArgument.UiSliceMbNum[0] = 20
	if !reflect.DeepEqual(p.SSpatialLayers[0], wantLayer) {
		t.Errorf("layer = %+v, want %+v", p.SSpatialLayers[0], wantLayer)
	}
	if picture.IPicWidth != 640 || picture.IPicHeight != 360 || files.strSeqFile != "input.yuv" ||
		files.strBsFile != "output.264" || files.uiFrameToBeCoded != 17 || !files.bEnableMultiBsFile ||
		files.sRecFileName[0] != "reconstructed.yuv" || g_LevelSetting != 0 {
		t.Fatalf("incorrect source/file/trace settings: picture=%+v files=%+v trace=%d", picture, files, g_LevelSetting)
	}
}

func TestParseCommandLineLayerValidation(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  api.SliceModeEnum
	}{
		{"0", api.SM_SINGLE_SLICE}, {"1", api.SM_FIXEDSLCNUM_SLICE}, {"2", api.SM_RASTER_SLICE},
		{"3", api.SM_SIZELIMITED_SLICE}, {"99", api.SM_RESERVED},
	} {
		t.Run("slice mode "+tt.value, func(t *testing.T) {
			var p api.SEncParamExt
			if got := ParseCommandLine([]string{"-slcmd", "0", tt.value}, &api.SSourcePicture{}, &p, &SFilesSet{}); got != 0 || p.SSpatialLayers[0].SSliceArgument.UiSliceMode != tt.want {
				t.Fatalf("slice mode %s: result=%d mode=%v, want %v", tt.value, got, p.SSpatialLayers[0].SSliceArgument.UiSliceMode, tt.want)
			}
		})
	}
	var p api.SEncParamExt
	var files SFilesSet
	args := strings.Fields("-dw -1 99 -lqp 999 30 -drec 999 ignored -lconfig 999 ignored -sw")
	if got := ParseCommandLine(args, &api.SSourcePicture{}, &p, &files); got != 0 || p != (api.SEncParamExt{}) || files != (SFilesSet{}) {
		t.Fatalf("invalid layer indices or missing values changed settings: result=%d param=%+v files=%+v", got, p, files)
	}
	path := configFile(t, "FrameWidth 48\nFrameHeight 32\n")
	if got := ParseCommandLine([]string{"-lconfig", "1", path}, &api.SSourcePicture{}, &p, &files); got != 0 || p.SSpatialLayers[1].IVideoWidth != 48 || files.strLayerCfgFile[1] != path {
		t.Fatalf("layer config not applied: result=%d layer=%+v files=%+v", got, p.SSpatialLayers[1], files)
	}
	for _, args := range [][]string{
		{"-drec", "0", strings.Repeat("x", common.MAX_FNAME_LEN)},
		{"-lconfig", "0", configFile(t, "SpatialBitrate -1")},
	} {
		t.Run("invalid "+args[0], func(t *testing.T) {
			if got := ParseCommandLine(args, &api.SSourcePicture{}, &api.SEncParamExt{}, &SFilesSet{}); got != 1 {
				t.Fatalf("invalid options %q returned %d", args, got)
			}
		})
	}
}

func TestFillSpecificParameters(t *testing.T) {
	var p api.SEncParamExt
	if got := FillSpecificParameters(&p); got != 0 {
		t.Fatalf("FillSpecificParameters = %d", got)
	}
	if p.IPicWidth != 1280 || p.IPicHeight != 720 || p.FMaxFrameRate != 30 || p.ISpatialLayerNum != 4 ||
		p.ITemporalLayerNum != 3 || p.ITargetBitrate != 2_500_000 || !p.BEnableFrameSkip || !p.BFixRCOverShoot {
		t.Fatalf("unexpected default parameters: %+v", p)
	}
	for i, want := range []struct {
		width, height, bitrate int32
		rate                   float32
	}{
		{160, 90, 64_000, 7.5}, {320, 180, 160_000, 15}, {640, 360, 512_000, 30}, {1280, 720, 1_500_000, 30},
	} {
		got := p.SSpatialLayers[i]
		if got.IVideoWidth != want.width || got.IVideoHeight != want.height || got.ISpatialBitrate != want.bitrate || got.FFrameRate != want.rate || got.SSliceArgument.UiSliceMode != api.SM_SINGLE_SLICE {
			t.Errorf("layer %d defaults = %+v", i, got)
		}
	}
}

func TestHelpAndFloatFormatting(t *testing.T) {
	output := captureOutput(t)
	PrintHelp()
	if err := stdout.Flush(); err != nil {
		t.Fatal(err)
	}
	for _, option := range []string{"Encoder Usage", "-org", "-bf", "-lconfig", "-trace"} {
		if !strings.Contains(output.String(), option) {
			t.Errorf("help omits %s", option)
		}
	}
	for _, tt := range []struct {
		value float64
		want  string
	}{
		{1.25, "1.250000"}, {math.Inf(1), "inf"}, {math.Inf(-1), "-inf"},
		{math.NaN(), "nan"}, {math.Copysign(math.NaN(), -1), "-nan"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			if got := cFloat(tt.value); got != tt.want {
				t.Errorf("cFloat(%g) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func newEncoder(t *testing.T) api.ISVCEncoder {
	t.Helper()
	var enc api.ISVCEncoder
	if got := CreateSVCEncHandle(&enc); got != 0 || enc == nil {
		t.Fatalf("create encoder = %d, %v", got, enc)
	}
	t.Cleanup(func() { DestroySVCEncHandle(enc) })
	return enc
}

func TestProcessEncodingRoundTrip(t *testing.T) {
	captureOutput(t)
	dir := t.TempDir()
	source, output := filepath.Join(dir, "input.yuv"), filepath.Join(dir, "output.264")
	frame := bytes.Repeat([]byte{128}, 32*32*3/2)
	if err := os.WriteFile(source, append(bytes.Clone(frame), frame...), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"h264enc", "-org", source, "-bf", output, "-sw", "32", "-sh", "32", "-numl", "1", "-numtl", "1", "-dw", "0", "32", "-dh", "0", "32", "-frout", "0", "30", "-frin", "30", "-rc", "-1", "-iper", "1", "-frms", "1"}
	if got := ProcessEncoding(newEncoder(t), args, false); got != 0 {
		t.Fatalf("ProcessEncoding = %d", got)
	}
	bitstream, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(bitstream) == 0 {
		t.Fatal("encoder produced no bitstream")
	}
	var dec api.ISVCDecoder
	if got := decoder.WelsCreateDecoder(&dec); got != 0 {
		t.Fatalf("create decoder = %d", got)
	}
	t.Cleanup(func() { decoder.WelsDestroyDecoder(dec) })
	if got := dec.Initialize(&api.SDecodingParam{}); got != 0 {
		t.Fatalf("initialize decoder = %d", got)
	}
	var planes [3][]byte
	var info api.SBufferInfo
	if got := dec.DecodeFrameNoDelay(bitstream, int32(len(bitstream)), &planes, &info); got != api.DsErrorFree {
		t.Fatalf("decode = %v", got)
	}
	if info.IBufferStatus != 1 || info.UsrData.SSystemBuffer.IWidth != 32 || info.UsrData.SSystemBuffer.IHeight != 32 {
		t.Fatalf("decoded dimensions/status = %+v", info)
	}
	for plane, data := range planes {
		if len(data) == 0 || data[0] < 126 || data[0] > 130 {
			t.Fatalf("decoded plane %d sample = %v, want neutral gray", plane, data[:min(1, len(data))])
		}
	}
}

// scriptedEncoder isolates command file routing from codec compression. Every
// frame is still checked against the source layout passed to EncodeFrame.
type scriptedEncoder struct {
	api.ISVCEncoder
	initResult, dumpResult, frameResult int32
	params                              api.SEncParamExt
	frames                              int
	frameType                           api.EVideoFrameType
	layers                              []api.SLayerBSInfo
}

func (e *scriptedEncoder) GetDefaultParams(*api.SEncParamExt) int32 { return 0 }
func (e *scriptedEncoder) InitializeExt(p *api.SEncParamExt) int32 {
	e.params = *p
	return e.initResult
}
func (e *scriptedEncoder) SetOption(option api.ENCODER_OPTION, _ any) int32 {
	if option == api.ENCODER_OPTION_DUMP_FILE {
		return e.dumpResult
	}
	return 0
}
func (e *scriptedEncoder) EncodeFrame(p *api.SSourcePicture, info *api.SFrameBSInfo) int32 {
	e.frames++
	if p.IPicWidth != 32 || p.IPicHeight != 32 || p.IStride[0] != 32 || len(p.PData[0]) != 1536 {
		panic("incorrect I420 source layout")
	}
	info.EFrameType = e.frameType
	info.ILayerNum = int32(len(e.layers))
	copy(info.SLayerInfo[:], e.layers)
	return e.frameResult
}

func TestProcessEncodingFiles(t *testing.T) {
	captureOutput(t)
	for _, extension := range []string{".264", ""} {
		t.Run("multiple streams"+extension, func(t *testing.T) {
			dir := t.TempDir()
			source, output := filepath.Join(dir, "input.yuv"), filepath.Join(dir, "output"+extension)
			if err := os.WriteFile(source, make([]byte, 1536), 0o600); err != nil {
				t.Fatal(err)
			}
			enc := &scriptedEncoder{frameType: api.VideoFrameTypeIDR}
			for _, data := range [][]byte{{0, 0, 0, 1, 7, 42}, {0, 0, 0, 1, 5, 43}, {0, 0, 0, 1, 20, 44}} {
				layer := api.SLayerBSInfo{PBsBuf: data, INalCount: 1, PNalLengthInByte: []int32{int32(len(data))}}
				if data[4] == 20 {
					layer.UiSpatialId = 1
				}
				enc.layers = append(enc.layers, layer)
			}
			args := []string{"h264enc", "-org", source, "-bf", output, "-sw", "32", "-sh", "32", "-numl", "2", "-mfile", "1"}
			if got := ProcessEncoding(enc, args, false); got != 0 || enc.frames != 1 {
				t.Fatalf("encoding result=%d frames=%d", got, enc.frames)
			}
			for i := range 2 {
				data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("output_layer%d%s", i, extension)))
				if err != nil {
					t.Fatal(err)
				}
				want := append(bytes.Clone(enc.layers[0].PBsBuf), enc.layers[i+1].PBsBuf...)
				if !bytes.Equal(data, want) {
					t.Fatalf("layer %d stream=%x, want %x", i, data, want)
				}
			}
		})
	}
	for _, tt := range []struct {
		name  string
		enc   scriptedEncoder
		short bool
	}{
		{"skipped", scriptedEncoder{frameType: api.VideoFrameTypeSkip}, false},
		{"codec failure", scriptedEncoder{frameResult: 1}, false},
		{"truncated input", scriptedEncoder{}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			source, output := filepath.Join(dir, "input.yuv"), filepath.Join(dir, "out.264")
			size := 1536
			if tt.short {
				size--
			}
			if err := os.WriteFile(source, make([]byte, size), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"h264enc", "-org", source, "-bf", output, "-sw", "32", "-sh", "32", "-numl", "1", "-frms", "2"}
			if got := ProcessEncoding(&tt.enc, args, false); got != 0 {
				t.Fatalf("result=%d", got)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			wantFrames := 1
			if tt.short {
				wantFrames = 0
			}
			if len(data) != 0 || tt.enc.frames != wantFrames {
				t.Fatalf("bytes=%d frames=%d, want 0 and %d", len(data), tt.enc.frames, wantFrames)
			}
		})
	}
}

func TestProcessEncodingErrors(t *testing.T) {
	captureOutput(t)
	DestroySVCEncHandle(nil)
	if got := ProcessEncoding(nil, nil, false); got != 1 {
		t.Fatalf("nil encoder result=%d", got)
	}
	dir := t.TempDir()
	for _, tt := range []struct {
		name   string
		args   []string
		config bool
		enc    scriptedEncoder
	}{
		{"missing config", []string{"h264enc", filepath.Join(dir, "missing.cfg")}, true, scriptedEncoder{}},
		{"invalid config", []string{"h264enc", configFile(t, "NumLayers 0")}, true, scriptedEncoder{}},
		{"invalid arguments", []string{"h264enc", "-drec", "0", strings.Repeat("x", common.MAX_FNAME_LEN)}, false, scriptedEncoder{}},
		{"initialization", []string{"h264enc"}, false, scriptedEncoder{initResult: 1}},
		{"reconstruction", []string{"h264enc", "-drec", "0", "unused"}, false, scriptedEncoder{dumpResult: 1}},
		{"missing output", []string{"h264enc"}, false, scriptedEncoder{}},
		{"invalid output", []string{"h264enc", "-bf", dir}, false, scriptedEncoder{}},
		{"invalid multilayer output", []string{"h264enc", "-bf", filepath.Join(dir, "missing", "out.264"), "-mfile", "1"}, false, scriptedEncoder{}},
		{"missing source", []string{"h264enc", "-bf", filepath.Join(dir, "out.264"), "-org", filepath.Join(dir, "missing.yuv")}, false, scriptedEncoder{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProcessEncoding(&tt.enc, tt.args, tt.config); got != 1 {
				t.Fatalf("result=%d, want 1", got)
			}
		})
	}
}
