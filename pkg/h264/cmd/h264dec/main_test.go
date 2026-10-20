package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/encoder"
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

func testFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBitReading(t *testing.T) {
	t.Parallel()
	bits := []byte{0b10110110, 0b01000000}
	var position int32
	if got := readBit(bits, &position); got != 1 || position != 1 {
		t.Fatalf("first bit=%d position=%d", got, position)
	}
	if got := readBits(bits, 8, &position); got != 0b01101100 || position != 9 {
		t.Fatalf("cross-byte bits=%b position=%d", got, position)
	}
	for _, tt := range []struct {
		encoded byte
		want    int32
	}{{0x80, 0}, {0x40, 1}, {0x60, 2}, {0x20, 3}, {0x10, 7}} {
		t.Run(fmt.Sprintf("Exp-Golomb %d", tt.want), func(t *testing.T) {
			t.Parallel()
			var bitPosition int32
			if got := bsGetUe([]byte{tt.encoded}, &bitPosition); got != tt.want {
				t.Errorf("UE(%08b)=%d, want %d", tt.encoded, got, tt.want)
			}
			if got := readFirstMbInSlice([]byte{0x65, tt.encoded}); got != tt.want {
				t.Errorf("first MB=%d, want %d", got, tt.want)
			}
		})
	}
}

func nal(kind byte, shortStart bool) []byte {
	data := []byte{0, 0, 0, 1, kind, 0x80, 0x55, 0x55}
	if shortStart {
		return data[1:]
	}
	return data
}

func TestReadPicture(t *testing.T) {
	t.Parallel()
	for _, shortStart := range []bool{false, true} {
		for _, tt := range []struct {
			name  string
			kinds []byte
			units int
		}{
			{"IDR to P", []byte{5, 1}, 1}, {"P to IDR", []byte{1, 5}, 1}, {"P to P", []byte{1, 1}, 1},
			{"IDR to IDR", []byte{5, 5}, 1}, {"picture to SPS", []byte{5, 7}, 1},
			{"second SPS", []byte{7, 7}, 1}, {"picture to PPS", []byte{1, 8}, 1},
			{"access delimiter", []byte{9, 5, 9, 1}, 2}, {"parameter sets and picture", []byte{7, 8, 5}, 3},
			{"unknown NAL", []byte{6, 5}, 2},
		} {
			t.Run(fmt.Sprintf("%s/short=%v", tt.name, shortStart), func(t *testing.T) {
				t.Parallel()
				var data []byte
				for _, kind := range tt.kinds {
					data = append(data, nal(kind, shortStart)...)
				}
				var sps, count int32
				got := readPicture(data, int32(len(data)), 0, &sps, &count)
				want := tt.units * len(nal(5, shortStart))
				if got != int32(want) {
					t.Fatalf("access unit size=%d, want %d", got, want)
				}
				if tt.name == "parameter sets and picture" {
					wantOffset := int32(4)
					if shortStart {
						wantOffset = 3
					}
					if sps != wantOffset || count != 4 {
						t.Fatalf("SPS offset=%d length=%d, want %d and 4", sps, count, wantOffset)
					}
				}
			})
		}
	}
	var sps, count int32
	if got := readPicture([]byte{0, 0, 1}, 3, 0, &sps, &count); got != 3 {
		t.Fatalf("short input=%d, want 3", got)
	}
	data := append(nal(1, false), nal(1, false)...)
	data[13] = 0x40 // first_mb_in_slice=1, so this is another slice of the same picture.
	if got := readPicture(data, int32(len(data)), 0, &sps, &count); got != int32(len(data)) {
		t.Fatalf("multi-slice picture split at %d", got)
	}
}

func TestWriteCroppedI420(t *testing.T) {
	t.Parallel()
	planes := [3][]byte{
		{1, 2, 3, 4, 99, 99, 5, 6, 7, 8, 99, 99, 9, 10, 11, 12, 99, 99, 13, 14, 15, 16, 99, 99},
		{17, 18, 99, 19, 20, 99}, {21, 22, 99, 23, 24, 99},
	}
	info := api.SBufferInfo{PDst: planes}
	info.UsrData.SSystemBuffer = api.SSysMEMBuffer{IWidth: 4, IHeight: 4, IStride: [2]int32{6, 3}}
	var output bytes.Buffer
	w := bufio.NewWriter(&output)
	if got := (cUtils{}).Process(planes, &info, w); got != 0 {
		t.Fatalf("Process=%d", got)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := make([]byte, 24)
	for i := range want {
		want[i] = byte(i + 1)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("cropped I420=%v, want %v", output.Bytes(), want)
	}
	for _, tt := range []struct {
		name   string
		planes [3][]byte
		info   *api.SBufferInfo
		writer *bufio.Writer
	}{
		{"nil writer", planes, &info, nil}, {"nil info", planes, nil, w}, {"nil planes", [3][]byte{}, &info, w},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := (cUtils{}).Process(tt.planes, tt.info, tt.writer); got != 0 {
				t.Errorf("empty output Process=%d", got)
			}
		})
	}
	if output.Len() != 24 {
		t.Fatal("invalid inputs wrote extra pixels")
	}
}

func tinyFrame() api.SBufferInfo {
	info := api.SBufferInfo{IBufferStatus: 1, PDst: [3][]byte{{1, 2, 3, 4}, {5}, {6}}}
	info.UsrData.SSystemBuffer = api.SSysMEMBuffer{IWidth: 2, IHeight: 2, IStride: [2]int32{2, 1}}
	return info
}

func TestOutputFrame(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yuvPath, optionsPath := filepath.Join(dir, "frame.yuv"), filepath.Join(dir, "options.bin")
	yuv, options := createOutFile(yuvPath), createOutFile(optionsPath)
	if yuv == nil || options == nil {
		t.Fatal("could not create output files")
	}
	t.Cleanup(yuv.close)
	t.Cleanup(options.close)
	var frames, width, height, lastWidth, lastHeight int32
	info := tinyFrame()
	for range 2 {
		outputFrame(cUtils{}, &info, yuv, options, &frames, &width, &height, &lastWidth, &lastHeight)
	}
	if frames != 2 || width != 2 || height != 2 || lastWidth != 2 || lastHeight != 2 {
		t.Fatalf("output state: frames=%d dimensions=%dx%d last=%dx%d", frames, width, height, lastWidth, lastHeight)
	}
	if err := yuv.w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := options.w.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, yuvPath); !bytes.Equal(got, []byte{1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6}) {
		t.Fatalf("I420 output=%v", got)
	}
	got := readFile(t, optionsPath)
	if len(got) != 12 || binary.LittleEndian.Uint32(got[:4]) != 0 || binary.LittleEndian.Uint32(got[4:8]) != 2 || binary.LittleEndian.Uint32(got[8:]) != 2 {
		t.Fatalf("resolution records=%x", got)
	}
	var absent *outFile
	absent.close()
	if absent.writer() != nil {
		t.Fatal("nil output has a writer")
	}
	if createOutFile(dir) != nil {
		t.Fatal("directory accepted as output file")
	}
}

// scriptedDecoder supplies deterministic decoded pictures to exercise the
// command's access-unit framing, flush and legacy API paths independently.
type scriptedDecoder struct {
	api.ISVCDecoder
	threads, remaining int32
	packets            [][]byte
	flushes            int
	endOfStream        bool
	legacyPending      bool
}

func (d *scriptedDecoder) SetOption(option api.DECODER_OPTION, value any) int32 {
	if option == api.DECODER_OPTION_END_OF_STREAM {
		d.endOfStream = *value.(*int32) != 0
	}
	return 0
}
func (d *scriptedDecoder) GetOption(option api.DECODER_OPTION, value any) int32 {
	switch option {
	case api.DECODER_OPTION_NUM_OF_THREADS:
		*value.(*int32) = d.threads
	case api.DECODER_OPTION_NUM_OF_FRAMES_REMAINING_IN_BUFFER:
		*value.(*int32) = d.remaining
	}
	return 0
}
func (d *scriptedDecoder) DecodeFrameNoDelay(data []byte, size int32, _ *[3][]byte, info *api.SBufferInfo) api.DECODING_STATE {
	d.packets = append(d.packets, bytes.Clone(data[:size]))
	*info = tinyFrame()
	return api.DsErrorFree
}
func (d *scriptedDecoder) DecodeFrame2(data []byte, size int32, _ *[3][]byte, info *api.SBufferInfo) api.DECODING_STATE {
	if size > 0 {
		d.packets = append(d.packets, bytes.Clone(data[:size]))
		d.legacyPending = true
	} else if d.legacyPending {
		*info = tinyFrame()
		d.legacyPending = false
	}
	return api.DsErrorFree
}
func (d *scriptedDecoder) FlushFrame(_ *[3][]byte, info *api.SBufferInfo) api.DECODING_STATE {
	d.flushes++
	if d.flushes%2 == 1 {
		*info = tinyFrame()
	}
	d.remaining--
	return api.DsErrorFree
}

func TestFlushFrames(t *testing.T) {
	t.Parallel()
	dec := &scriptedDecoder{remaining: 2}
	var elapsed int64
	var timestamp uint64 = 42
	var frames, width, height, lastWidth, lastHeight int32
	FlushFrames(dec, &elapsed, nil, nil, &frames, &timestamp, &width, &height, &lastWidth, &lastHeight)
	if dec.flushes != 2 || dec.remaining != 0 || frames != 1 || width != 2 || height != 2 || timestamp != 42 || elapsed < 0 {
		t.Fatalf("flush state: decoder=%+v frames=%d dimensions=%dx%d elapsed=%d", dec, frames, width, height, elapsed)
	}
}

func encodedFrames(t *testing.T) [][]byte {
	t.Helper()
	var enc api.ISVCEncoder
	if got := encoder.WelsCreateSVCEncoder(&enc); got != 0 {
		t.Fatalf("create encoder=%d", got)
	}
	t.Cleanup(func() { encoder.WelsDestroySVCEncoder(enc) })
	params := api.SEncParamBase{IPicWidth: 32, IPicHeight: 32, ITargetBitrate: 100_000, FMaxFrameRate: 30, IRCMode: api.RC_OFF_MODE}
	if got := enc.Initialize(&params); got != 0 {
		t.Fatalf("initialize encoder=%d", got)
	}
	yuv := bytes.Repeat([]byte{128}, 32*32*3/2)
	picture := api.SSourcePicture{IColorFormat: int32(api.VideoFormatI420), IPicWidth: 32, IPicHeight: 32, IStride: [4]int32{32, 16, 16}, PData: [4][]byte{yuv, yuv[1024:], yuv[1280:]}}
	var frames [][]byte
	for i := range 2 {
		picture.UiTimeStamp = int64(i * 33)
		var info api.SFrameBSInfo
		if got := enc.EncodeFrame(&picture, &info); got != 0 {
			t.Fatalf("encode frame %d=%d", i, got)
		}
		var frame []byte
		for _, layer := range info.SLayerInfo[:info.ILayerNum] {
			var size int32
			for _, n := range layer.PNalLengthInByte[:layer.INalCount] {
				size += n
			}
			frame = append(frame, layer.PBsBuf[:size]...)
		}
		if len(frame) == 0 {
			t.Fatalf("frame %d has no bitstream", i)
		}
		frames = append(frames, frame)
	}
	return frames
}

func TestDecoderCommandRoundTrip(t *testing.T) {
	captureOutput(t)
	frames := encodedFrames(t)
	input := testFile(t, "input.264", bytes.Join(frames, nil))
	for _, mode := range []string{"normal", "legacy", "length", "config", "quiet"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			output, options := filepath.Join(dir, "output.yuv"), filepath.Join(dir, "options.bin")
			args := []string{"h264dec", input, output, "-options", options, "-trace", "0", "-ec", "0"}
			switch mode {
			case "legacy":
				args = append(args, "-legacy")
			case "length":
				var lengths []byte
				for _, frame := range frames {
					var record [16]byte
					binary.LittleEndian.PutUint32(record[8:12], uint32(len(frame)))
					lengths = append(lengths, record[:]...)
				}
				args = append(args, "-length", testFile(t, "lengths.bin", lengths))
			case "config":
				config := fmt.Sprintf("# decoder settings\nInputFile %s\nOutputFile %s\nRestructionFile unused\nTargetDQID 255\nErrorConcealmentIdc 0\nCPULoad 50\nVideoBitstreamType 0\nUnknown ignored\n", input, output)
				args = []string{"h264dec", testFile(t, "decoder.cfg", []byte(config))}
			case "quiet":
				args = []string{"h264dec", input}
			}
			if got := decMain(args); got != 0 {
				t.Fatalf("decMain=%d", got)
			}
			if mode == "quiet" {
				return
			}
			data := readFile(t, output)
			if len(data) != 2*32*32*3/2 {
				t.Fatalf("decoded bytes=%d, want 3072", len(data))
			}
			for i, value := range data {
				if value < 126 || value > 130 {
					t.Fatalf("decoded byte %d=%d, want neutral gray", i, value)
				}
			}
			if mode != "config" {
				record := readFile(t, options)
				if len(record) != 12 || binary.LittleEndian.Uint32(record[4:8]) != 32 || binary.LittleEndian.Uint32(record[8:12]) != 32 {
					t.Fatalf("resolution records=%x", record)
				}
			}
		})
	}
}

func TestDecoderCommandInvalidArguments(t *testing.T) {
	output := captureOutput(t)
	for _, tt := range []struct {
		args    []string
		message string
	}{
		{[]string{"h264dec"}, "usage"}, {[]string{"h264dec", "unknown"}, "No input file"},
		{[]string{"h264dec", filepath.Join(t.TempDir(), "missing.cfg")}, "not exist"},
		{[]string{"h264dec", testFile(t, "empty.cfg", nil)}, "No output file"},
		{[]string{"h264dec", testFile(t, "no-input.cfg", []byte("OutputFile unused"))}, "No input file"},
		{[]string{"h264dec", "input.264", ""}, "No output file"},
		{[]string{"h264dec", "input.264", "output.yuv", "-options"}, "options file not specified"},
		{[]string{"h264dec", "input.264", "output.yuv", "-trace"}, "trace level not specified"},
		{[]string{"h264dec", "input.264", "output.yuv", "-length"}, "lenght file not specified"},
	} {
		t.Run(tt.message, func(t *testing.T) {
			output.Reset()
			if got := decMain(tt.args); got != 1 {
				t.Fatalf("decMain=%d, want 1", got)
			}
			if err := stdout.Flush(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), tt.message) {
				t.Fatalf("diagnostic=%q, want %q", output.String(), tt.message)
			}
		})
	}
}

func TestDecodeInstanceFileErrors(t *testing.T) {
	captureOutput(t)
	var width, height int32
	H264DecodeInstance(nil, "", "", &width, &height, "", "", 0, false)
	for _, tt := range []struct{ name, input, output, options, length string }{
		{name: "empty input"},
		{name: "missing input", input: filepath.Join(t.TempDir(), "missing.264")},
		{name: "short input", input: testFile(t, "short.264", []byte{0, 0, 0, 1})},
		{name: "unwritable outputs", input: testFile(t, "input.264", nal(5, false)), output: t.TempDir(), options: t.TempDir(), length: filepath.Join(t.TempDir(), "missing.bin")},
		{name: "truncated lengths", input: testFile(t, "input.264", nal(5, false)), length: testFile(t, "length.bin", []byte{1})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dec := &scriptedDecoder{threads: 1}
			H264DecodeInstance(dec, tt.input, tt.output, &width, &height, tt.options, tt.length, 0, false)
			wantPackets := 0
			if tt.name == "unwritable outputs" {
				wantPackets = 1
			}
			if len(dec.packets) != wantPackets {
				t.Fatalf("decoded packets=%d, want %d", len(dec.packets), wantPackets)
			}
		})
	}
}

func TestDecodeInstanceFraming(t *testing.T) {
	captureOutput(t)
	for _, tt := range []struct {
		name    string
		threads int32
		legacy  bool
		length  int
	}{
		{"access units", 1, false, 0}, {"NAL units", 0, false, 0}, {"legacy", 1, true, 0}, {"oversized length", 1, false, 999},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := append(nal(5, false), nal(1, false)...)
			input := testFile(t, "input.264", data)
			lengthFile := ""
			if tt.length != 0 {
				var record [16]byte
				binary.LittleEndian.PutUint32(record[8:12], uint32(tt.length))
				lengthFile = testFile(t, "length.bin", record[:])
			}
			dec := &scriptedDecoder{threads: tt.threads}
			var width, height int32
			H264DecodeInstance(dec, input, "", &width, &height, "", lengthFile, 0, tt.legacy)
			wantPackets := 2
			if tt.length != 0 {
				wantPackets = 1
			}
			if !dec.endOfStream || len(dec.packets) != wantPackets || width != 2 || height != 2 {
				t.Fatalf("decode state: EOS=%v packets=%d dimensions=%dx%d", dec.endOfStream, len(dec.packets), width, height)
			}
			wantData := data
			if tt.threads == 0 {
				wantData = bytes.ReplaceAll(data, []byte{0, 0, 0, 1}, []byte{0, 0, 1})
			}
			if !bytes.Equal(bytes.Join(dec.packets, nil), wantData) {
				t.Fatalf("framing changed NAL payloads: %x, want %x", dec.packets, wantData)
			}
		})
	}
}

func TestCFloat(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		value float64
		want  string
	}{
		{1.25, "1.250000"}, {math.Inf(1), "inf"}, {math.Inf(-1), "-inf"}, {math.NaN(), "nan"}, {math.Copysign(math.NaN(), -1), "-nan"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := cFloat(tt.value); got != tt.want {
				t.Errorf("cFloat(%g)=%q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestDecodeInstanceSequenceChange(t *testing.T) {
	captureOutput(t)
	first := bytes.Join([][]byte{nal(7, false), nal(8, false), nal(5, false)}, nil)
	secondSPS := append(nal(7, false), 0x55)
	second := bytes.Join([][]byte{secondSPS, nal(8, false), nal(5, false)}, nil)
	input := testFile(t, "input.264", append(bytes.Clone(first), second...))
	dec := &scriptedDecoder{threads: 1, remaining: 1}
	var width, height int32
	H264DecodeInstance(dec, input, "", &width, &height, "", "", 0, false)
	if dec.flushes != 1 || !reflect.DeepEqual(dec.packets, [][]byte{first, second}) {
		t.Fatalf("sequence change: flushes=%d packets=%x", dec.flushes, dec.packets)
	}
}
