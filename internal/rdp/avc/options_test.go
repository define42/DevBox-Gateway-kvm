package avc

import (
	"errors"
	"image"
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

func TestThreadOptions(t *testing.T) {
	t.Parallel()
	for _, threads := range []int{-1, 0, MaxThreads + 1} {
		encoder, err := NewEncoderWithOptions(64, 48, Options{Threads: threads})
		if !errors.Is(err, ErrThreads) || encoder != nil {
			t.Fatalf("threads %d: encoder %v, error %v", threads, encoder, err)
		}
	}
}

func TestFrameRateOptions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		requested, want int
		wantErr         error
	}{
		{name: "default", want: FrameRate},
		{name: "minimum", requested: 1, want: 1},
		{name: "office", requested: 30, want: 30},
		{name: "maximum", requested: FrameRate, want: FrameRate},
		{name: "negative", requested: -1, wantErr: ErrFrameRate},
		{name: "above maximum", requested: FrameRate + 1, wantErr: ErrFrameRate},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoder, err := NewEncoderWithOptions(64, 48, Options{Threads: 1, FrameRate: test.requested})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewEncoderWithOptions: got %v, want %v", err, test.wantErr)
			}
			if err != nil {
				if encoder != nil {
					encoder.Close()
					t.Fatal("invalid rate returned an encoder")
				}

				return
			}
			defer encoder.Close()
			checkSoftwareFrameRate(t, encoder, test.want)
		})
	}
}

func TestHardwareFrameRateOptions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		rate        int
		backend     Backend
		hardwareErr error
		want        int
		wantErr     error
	}{
		{name: "default", backend: BackendNVENC, want: FrameRate},
		{name: "office", rate: 30, backend: BackendNVENC, want: 30},
		{name: "auto", rate: 30, backend: BackendAuto, want: 30},
		{name: "initialization fallback", rate: 30, backend: BackendAuto, hardwareErr: ErrCodec, want: 30},
		{name: "invalid", rate: FrameRate + 1, backend: BackendNVENC, wantErr: ErrFrameRate},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			probes := 0
			encoder, err := newEncoderWithNVENC(64, 48,
				Options{Threads: 1, FrameRate: test.rate, Backend: test.backend},
				func(width, height, rate int) (codecEncoder, error) {
					probes++
					if width != 64 || height != 48 || rate != test.want {
						t.Fatalf("NVENC options = %dx%d at %d FPS", width, height, rate)
					}
					if test.hardwareErr != nil {
						return nil, test.hardwareErr
					}

					return &recordingCodec{}, nil
				})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("newEncoderWithNVENC: got %v, want %v", err, test.wantErr)
			}
			if err != nil {
				if probes != 0 || encoder != nil {
					t.Fatalf("invalid rate reached codec: probes %d, encoder %v", probes, encoder)
				}

				return
			}
			defer encoder.Close()
			if probes != 1 || encoder.FrameRate() != test.want {
				t.Fatalf("probes %d, rate %d; want 1, %d", probes, encoder.FrameRate(), test.want)
			}
			if test.hardwareErr != nil {
				checkSoftwareFrameRate(t, encoder, test.want)
			}
		})
	}
}

func TestFrameRateTimestampsAndRetry(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		requested, want int
	}{
		{name: "default", want: FrameRate},
		{name: "office", requested: 30, want: 30},
		{name: "minimum", requested: 1, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorded := &recordingCodec{}
			hardware := &failingHardwareCodec{codecEncoder: recorded}
			encoder, err := newEncoderWithNVENC(16, 16,
				Options{Threads: 1, Backend: BackendNVENC, FrameRate: test.requested},
				func(int, int, int) (codecEncoder, error) { return hardware, nil })
			if err != nil {
				t.Fatal(err)
			}
			defer encoder.Close()
			img := image.NewRGBA(image.Rect(0, 0, 16, 16))
			if _, err := encoder.Encode(img, false); err != nil {
				t.Fatal(err)
			}
			if data, err := encoder.Encode(img, false); data != nil || err != nil {
				t.Fatalf("unchanged frame = %v, %v", data, err)
			}
			hardware.err = ErrCodec
			if _, err := encoder.Encode(img, true); !errors.Is(err, ErrCodec) {
				t.Fatalf("injected failure = %v", err)
			}
			hardware.err = nil
			if _, err := encoder.EncodeDamage(img, []image.Rectangle{}, false); err != nil {
				t.Fatal(err)
			}
			if _, err := encoder.Encode(img, true); err != nil {
				t.Fatal(err)
			}
			// The failed attempt advances the clock; an unchanged frame does not.
			wantTimestamps := []int64{0, 2 * 1000 / int64(test.want), 3 * 1000 / int64(test.want)}
			if !slices.Equal(recorded.timestamps, wantTimestamps) ||
				!slices.Equal(recorded.forces, []bool{false, true, true}) {
				t.Fatalf("encode calls: timestamps %v, forces %v; want %v, [false true true]",
					recorded.timestamps, recorded.forces, wantTimestamps)
			}
		})
	}
}

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

func TestEncodeMeasurements(t *testing.T) {
	t.Parallel()
	for _, measure := range []bool{false, true} {
		codec := &recordingCodec{}
		encoder := &Encoder{
			codec: codec, frameRate: FrameRate, width: 16, height: 16, i420: make([]byte, 16*16*3/2), measure: measure,
		}
		img := image.NewRGBA(image.Rect(0, 0, 16, 16))
		if _, err := encoder.Encode(img, false); err != nil {
			t.Fatal(err)
		}
		stats := encoder.LastStats()
		if measure && (stats.Conversion <= 0 || stats.Encoding <= 0) {
			t.Fatalf("measured frame stats = %+v", stats)
		}
		if !measure && stats != (EncodeStats{}) {
			t.Fatalf("disabled frame stats = %+v", stats)
		}
		if _, err := encoder.Encode(img, false); err != nil {
			t.Fatal(err)
		}
		if encoder.LastStats().Encoding != 0 {
			t.Fatal("unchanged frame retained previous encoding measurement")
		}
		encoder.Close()
		if _, err := encoder.Encode(img, false); !errors.Is(err, ErrClosed) {
			t.Fatalf("closed encoder returned %v", err)
		}
		if encoder.LastStats() != (EncodeStats{}) {
			t.Fatal("failed frame retained measurements")
		}
	}
}
