package avc

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"testing"

	openh264 "github.com/define42/DevBox-Gateway-kvm/pkg/h264"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

//nolint:paralleltest,tparallel // Sequential subtests bound codec reference-picture memory.
func TestOpenH264SlicesRoundTrip(t *testing.T) {
	t.Parallel()
	// Sequential subtests bound codec reference-picture memory. Each stream
	// changes resolution through a fresh encoder, just as an RDP resize does.
	for _, threads := range []int{1, 2, 4, MaxThreads} {
		t.Run(fmt.Sprint(threads), func(t *testing.T) {
			var frames []decodedReference
			for _, size := range [][2]int{{16, 16}, {64, 48}, {200, 200}, {320, 240}, {1024, 768}} {
				frames = append(frames, slicedTestFrames(t, threads, size[0], size[1])...)
			}
			decodeSlicedFrames(t, threads, frames)
		})
	}
}

func TestOpenH264ProductionPolicy(t *testing.T) {
	t.Parallel()

	encoder, err := NewEncoderWithOptions(320, 240, Options{Threads: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()

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

	decoder, err := openh264.NewDecoder(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	decoded := 0
	for index := range 152 {
		x, y := 2*(index%160), 2*((index/160)%120)
		value := color.RGBA{R: byte(index + 1), G: byte(index*17 + 1), B: byte(index*31 + 1), A: 255}
		region := image.Rect(x, y, x+2, y+2)
		draw.Draw(img, region, &image.Uniform{C: value}, image.Point{}, draw.Src)
		data, encodeErr := encoder.EncodeDamage(img, []image.Rectangle{region}, false)
		if encodeErr != nil {
			t.Fatalf("encode frame %d: %v", index, encodeErr)
		}
		if got, want := hasNAL(data, 5), index == 0; got != want {
			t.Fatalf("frame %d IDR = %t, want %t", index, got, want)
		}
		picture, decodeErr := decoder.Decode(data)
		if decodeErr != nil {
			t.Fatalf("decode frame %d: %v", index, decodeErr)
		}
		if picture != nil {
			decoded++
		}
	}
	data, err := encoder.Encode(img, true)
	if err != nil {
		t.Fatalf("encode forced IDR: %v", err)
	}
	if !hasNAL(data, 5) {
		t.Fatal("forced refresh did not produce an IDR")
	}
	picture, err := decoder.Decode(data)
	if err != nil {
		t.Fatalf("decode forced IDR: %v", err)
	}
	if picture != nil {
		decoded++
	}
	rest, err := decoder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	decoded += len(rest)
	if decoded != 153 {
		t.Fatalf("decoded %d frames, want 153", decoded)
	}
}

func TestOpenH264ParallelSlicesMatchSequential(t *testing.T) {
	t.Parallel()

	const width, height, slices = 320, 240, 4
	sequential := newFixedSliceTestEncoder(t, width, height, slices, 1)
	defer sequential.Close()
	parallel := newFixedSliceTestEncoder(t, width, height, slices, slices)
	defer parallel.Close()

	images, _ := slicedDesktopFrames(width, height, true)
	for index := range 6 {
		raw := referenceI420(width, height, images[index%len(images)])
		luma := width * height
		chroma := luma / 4
		frame := &openh264.Frame{
			Width: width, Height: height, Timestamp: int64(index * 1000 / FrameRate),
			Y: raw[:luma], U: raw[luma : luma+chroma], V: raw[luma+chroma:],
		}
		want, wantType, err := sequential.Encode(frame)
		if err != nil {
			t.Fatalf("sequential frame %d: %v", index, err)
		}
		got, gotType, err := parallel.Encode(frame)
		if err != nil {
			t.Fatalf("parallel frame %d: %v", index, err)
		}
		if gotType != wantType {
			t.Fatalf("frame %d type = %d, want %d", index, gotType, wantType)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("frame %d parallel output differs: got %d bytes, want %d", index, len(got), len(want))
		}
	}
}

func newFixedSliceTestEncoder(t *testing.T, width, height, slices, workers int) *openh264.Encoder {
	t.Helper()
	param, err := openh264.DefaultEncoderParams(width, height, api.UNSPECIFIED_BIT_RATE, FrameRate)
	if err != nil {
		t.Fatal(err)
	}
	param.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	param.IRCMode = api.RC_OFF_MODE
	param.IMultipleThreadIdc = uint16(workers)
	param.ILoopFilterDisableIdc = 2
	param.BUseLoadBalancing = false
	param.BEnableFrameSkip = false
	param.BEnableBackgroundDetection = false
	param.BEnableAdaptiveQuant = false
	param.IEntropyCodingModeFlag = 0
	layer := &param.SSpatialLayers[0]
	layer.IDLayerQp = DefaultQP
	layer.UiProfileIdc = api.PRO_BASELINE
	layer.SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
	layer.SSliceArgument.UiSliceNum = uint32(slices)

	encoder, err := openh264.NewEncoder(param)
	if err != nil {
		t.Fatal(err)
	}

	return encoder
}

type decodedReference struct {
	width, height int
	data          []byte
	pixels        []byte
}

func decodeSlicedFrames(t *testing.T, threads int, frames []decodedReference) {
	t.Helper()
	decoder, err := openh264.NewDecoder(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	var decoded []*openh264.Frame
	for index, frame := range frames {
		picture, err := decoder.Decode(frame.data)
		if err != nil {
			t.Fatalf("decode %d-slice frame %d: %v", threads, index, err)
		}
		if picture != nil {
			decoded = append(decoded, picture)
		}
	}
	rest, err := decoder.Flush()
	if err != nil {
		t.Fatalf("flush %d-slice stream: %v", threads, err)
	}
	decoded = append(decoded, rest...)
	if len(decoded) != len(frames) {
		t.Fatalf("decode %d-slice stream returned %d pictures, want %d", threads, len(decoded), len(frames))
	}
	for index, picture := range decoded {
		frame := frames[index]
		if picture.Width != frame.width || picture.Height != frame.height {
			t.Fatalf("frame %d dimensions %dx%d, want %dx%d",
				index, picture.Width, picture.Height, frame.width, frame.height)
		}
		decoded := appendFrameI420(nil, picture)
		if len(decoded) != len(frame.pixels) {
			t.Fatalf("frame %d decoded length = %d, want %d", index, len(decoded), len(frame.pixels))
		}
		var difference int
		for i, value := range frame.pixels {
			got := int(decoded[i])
			difference += max(int(value)-got, got-int(value))
		}
		if average := float64(difference) / float64(len(frame.pixels)); average > 3 {
			t.Fatalf("frame %d mean sample error %.3f", index, average)
		}
	}
}

func slicedTestFrames(t *testing.T, threads, width, height int) []decodedReference {
	t.Helper()
	encoder, err := NewEncoderWithOptions(width, height, Options{Threads: threads, Measure: true})
	if err != nil {
		t.Fatalf("initialize %dx%d/%d slices: %v", width, height, threads, err)
	}
	defer encoder.Close()
	actual := encoder.Threads()
	if actual < 1 || actual > threads {
		t.Fatalf("%dx%d requested %d slices, initialized %d", width, height, threads, actual)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Rect, image.NewUniform(color.RGBA{R: 210, G: 225, B: 230, A: 255}), image.Point{}, draw.Src)
	patch := image.Rect(width/4, height/4, width/2, height/2)
	var frames []decodedReference
	for index := range 3 {
		shade := color.RGBA{R: 40, G: 70, B: byte(100 + index*30), A: 255}
		draw.Draw(img, patch, image.NewUniform(shade), image.Point{}, draw.Src)
		data, err := encoder.EncodeDamage(img, []image.Rectangle{patch}, index == 2)
		if err != nil {
			t.Fatal(err)
		}
		kind := byte(5)
		if index == 1 {
			kind = 1
		}
		if slices := countNAL(data, kind); slices < actual {
			t.Fatalf("%dx%d frame %d: got %d slices, want at least %d", width, height, index, slices, actual)
		}
		if index != 1 && (!hasNAL(data, 7) || !hasNAL(data, 8)) {
			t.Fatalf("frame %d lacks parameter sets", index)
		}
		stats := encoder.LastStats()
		if stats.Conversion <= 0 || stats.Encoding <= 0 {
			t.Fatalf("missing measurements: %+v", stats)
		}
		frames = append(frames, decodedReference{
			width: width, height: height, data: bytes.Clone(data),
			pixels: referenceI420(width, height, img),
		})
	}

	return frames
}

func countNAL(data []byte, kind byte) int {
	count := 0
	for i := 0; i+4 < len(data); i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 && data[i+3]&31 == kind {
			count++
		}
	}

	return count
}
