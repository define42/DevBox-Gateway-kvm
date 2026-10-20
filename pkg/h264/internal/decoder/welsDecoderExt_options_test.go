package decoder

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

func TestDecoderOptionValueTypes(t *testing.T) {
	t.Parallel()

	i32, integer, unsigned, enabled, disabled := int32(1), 1, uint32(1), true, false
	for _, tt := range []struct {
		name  string
		value any
		want  int32
	}{
		{"int32", &i32, 1}, {"int", &integer, 1}, {"uint32", &unsigned, 1},
		{"bool enabled", &enabled, 1}, {"bool disabled", &disabled, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dec := newSyntheticDecoder(t, false)
			if result := dec.SetOption(api.DECODER_OPTION_END_OF_STREAM, tt.value); result != 0 {
				t.Fatalf("SetOption(END_OF_STREAM): %d", result)
			}
			var got32 int32
			var gotInt int
			var gotUnsigned uint32
			for _, target := range []any{&got32, &gotInt, &gotUnsigned} {
				if result := dec.GetOption(api.DECODER_OPTION_END_OF_STREAM, target); result != 0 {
					t.Fatalf("GetOption(END_OF_STREAM, %T): %d", target, result)
				}
			}
			if got32 != tt.want || gotInt != int(tt.want) || gotUnsigned != uint32(tt.want) {
				t.Fatalf("EOS representations = %d/%d/%d, want %d", got32, gotInt, gotUnsigned, tt.want)
			}
		})
	}
}

func TestDecoderOptionValidation(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		option api.DECODER_OPTION
		value  any
		set    bool
	}{
		{"EOS nil", api.DECODER_OPTION_END_OF_STREAM, nil, true},
		{"concealment typed nil", api.DECODER_OPTION_ERROR_CON_IDC, (*int32)(nil), true},
		{"interval nil", api.DECODER_OPTION_STATISTICS_LOG_INTERVAL, nil, true},
		{"statistics read only", api.DECODER_OPTION_GET_STATISTICS, int32(1), true},
		{"SAR read only", api.DECODER_OPTION_GET_SAR_INFO, int32(1), true},
		{"unknown setter", api.DECODER_OPTION(999), int32(1), true},
		{"EOS non-pointer", api.DECODER_OPTION_END_OF_STREAM, int32(1), false},
		{"EOS nil destination", api.DECODER_OPTION_END_OF_STREAM, (*uint32)(nil), false},
		{"statistics wrong destination", api.DECODER_OPTION_GET_STATISTICS, new(int32), false},
		{"SAR wrong destination", api.DECODER_OPTION_GET_SAR_INFO, new(int32), false},
		{"unknown getter", api.DECODER_OPTION(999), new(int32), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dec := newSyntheticDecoder(t, false)
			var result int32
			if tt.set {
				result = dec.SetOption(tt.option, tt.value)
			} else {
				result = dec.GetOption(tt.option, tt.value)
			}
			if result != int32(api.CmInitParaError) {
				t.Fatalf("option result = %d, want CmInitParaError", result)
			}
		})
	}
}

func TestDecoderModeAndLifecycleValidation(t *testing.T) {
	t.Parallel()

	dec := newSyntheticDecoder(t, false)
	var info api.SBufferInfo
	var planes [3][]byte
	var parsed api.SParserBsInfo
	if state := dec.DecodeParser(nil, 0, &parsed); state != api.DsInvalidArgument {
		t.Fatalf("parser API in decode mode = %v, want DsInvalidArgument", state)
	}
	if result := dec.Initialize(nil); result != int32(api.CmInitParaError) {
		t.Fatalf("nil initialization = %d, want CmInitParaError", result)
	}
	if result := dec.Uninitialize(); result != 0 {
		t.Fatalf("Uninitialize: %d", result)
	}
	if state := dec.DecodeFrame2(nil, 0, &planes, &info); state != api.DsInitialOptExpected {
		t.Fatalf("uninitialized decode = %v, want DsInitialOptExpected", state)
	}
	if state := dec.DecodeParser(nil, 0, &parsed); state != api.DsInitialOptExpected {
		t.Fatalf("uninitialized parser = %v, want DsInitialOptExpected", state)
	}
	var option int32
	if result := dec.GetOption(api.DECODER_OPTION_END_OF_STREAM, &option); result != int32(api.CmInitExpected) {
		t.Fatalf("uninitialized getter = %d, want CmInitExpected", result)
	}
	if result := dec.SetOption(api.DECODER_OPTION_END_OF_STREAM, &option); result != int32(api.DsInitialOptExpected) {
		t.Fatalf("uninitialized setter = %d, want DsInitialOptExpected", result)
	}
	if result := dec.Initialize(&api.SDecodingParam{BParseOnly: true}); result != 0 {
		t.Fatalf("reinitialize as parser: %d", result)
	}
	if state := dec.DecodeFrame2(nil, 0, &planes, &info); state != api.DsInvalidArgument {
		t.Fatalf("decode API in parser mode = %v, want DsInvalidArgument", state)
	}
	concealment := int32(api.ERROR_CON_SLICE_COPY)
	if result := dec.SetOption(api.DECODER_OPTION_ERROR_CON_IDC, &concealment); result != int32(api.CmInitParaError) {
		t.Fatalf("parser concealment request = %d, want CmInitParaError", result)
	}
	if result := dec.GetOption(api.DECODER_OPTION_ERROR_CON_IDC, &concealment); result != 0 || concealment != 0 {
		t.Fatalf("parser concealment changed: result=%d, value=%d", result, concealment)
	}
}

func TestDecoderStatisticsAndTraceOptions(t *testing.T) {
	t.Parallel()

	dec := newSyntheticDecoder(t, false)
	marker := &struct{ name string }{"trace context"}
	var context any = marker
	var messages []string
	callback := api.WelsTraceCallback(func(ctx any, _ int32, message string) {
		if ctx != marker {
			t.Errorf("trace context = %v, want supplied context", ctx)
		}
		messages = append(messages, message)
	})
	level, interval := uint32(api.WELS_LOG_INFO), uint32(1)
	for _, option := range []struct {
		id    api.DECODER_OPTION
		value any
	}{
		{api.DECODER_OPTION_TRACE_CALLBACK_CONTEXT, &context},
		{api.DECODER_OPTION_TRACE_CALLBACK, &callback},
		{api.DECODER_OPTION_TRACE_LEVEL, &level},
		{api.DECODER_OPTION_STATISTICS_LOG_INTERVAL, &interval},
	} {
		if result := dec.SetOption(option.id, option.value); result != 0 {
			t.Fatalf("SetOption(%d): %d", option.id, result)
		}
	}
	stream := syntheticStream(t, "main_ip")
	var planes [3][]byte
	var info api.SBufferInfo
	pos := 0
	for pos < len(stream) {
		packet := confReadFrame(stream, &pos)
		if state := dec.DecodeFrame2(packet, int32(len(packet)), &planes, &info); state != api.DsErrorFree {
			t.Fatalf("DecodeFrame2: %v", state)
		}
	}
	if state := dec.DecodeFrame2(nil, 0, &planes, &info); state != api.DsErrorFree {
		t.Fatalf("decode final frame: %v", state)
	}
	var stats api.SDecoderStatistics
	if result := dec.GetOption(api.DECODER_OPTION_GET_STATISTICS, &stats); result != 0 {
		t.Fatalf("GetOption(GET_STATISTICS): %d", result)
	}
	if stats.UiWidth != 96 || stats.UiHeight != 64 || stats.UiDecodedFrameCount != 12 || stats.IStatisticsLogInterval != 1 {
		t.Fatalf("incorrect statistics: %+v", stats)
	}
	var sar api.SVuiSarInfo
	if result := dec.GetOption(api.DECODER_OPTION_GET_SAR_INFO, &sar); result != 0 || sar.UiSarWidth != 8 || sar.UiSarHeight != 9 {
		t.Fatalf("SAR result=%d, value=%+v, want 8:9", result, sar)
	}
	if !strings.Contains(strings.Join(messages, "\n"), "DecoderStatistics:") {
		t.Fatal("statistics interval produced no callback report")
	}
	var noCallback api.WelsTraceCallback
	if result := dec.SetOption(api.DECODER_OPTION_TRACE_CALLBACK, &noCallback); result != 0 {
		t.Fatalf("disable trace callback: %d", result)
	}
	before := len(messages)
	concealment := int32(api.ERROR_CON_SLICE_COPY)
	if result := dec.SetOption(api.DECODER_OPTION_ERROR_CON_IDC, &concealment); result != 0 || len(messages) != before {
		t.Fatal("disabled callback received an option-change report")
	}
}

func TestDecoderLegacyAPIConformance(t *testing.T) {
	t.Parallel()

	dec := newSyntheticDecoder(t, false)
	stream := syntheticStream(t, "baseline")
	digest := sha256.New()
	frames := 0
	decode := func(src []byte) {
		var planes [3][]byte
		var stride [2]int32
		var width, height int32
		if state := dec.DecodeFrame(src, int32(len(src)), &planes, &stride, &width, &height); state != api.DsErrorFree {
			t.Fatalf("DecodeFrame: %v", state)
		}
		if planes[0] == nil {
			return
		}
		if width != 62 || height != 46 {
			t.Fatalf("legacy dimensions = %dx%d, want 62x46", width, height)
		}
		var info api.SBufferInfo
		info.UsrData.SSystemBuffer = api.SSysMEMBuffer{IWidth: width, IHeight: height, IStride: stride}
		confOnDecodeFrame(digest, &planes, &info)
		frames++
	}
	pos := 0
	for pos < len(stream) {
		decode(confReadFrame(stream, &pos))
	}
	decode(nil)
	const wantHash = "1ccf6a5e38f825953f81fdf3d2cf863a3a968e339803d4d82379a57eb0be41df"
	if frames != 12 || hex.EncodeToString(digest.Sum(nil)) != wantHash {
		t.Fatalf("legacy output: frames=%d SHA-256=%x, want 12 and %s", frames, digest.Sum(nil), wantHash)
	}
}
