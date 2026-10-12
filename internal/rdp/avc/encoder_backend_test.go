package avc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"testing"

	openh264 "github.com/define42/DevBox-Gateway-kvm/pkg/h264"
)

func TestEncoderBackendSelection(t *testing.T) {
	t.Parallel()
	unavailable := fmt.Errorf("%w: hardware unavailable", ErrCodec)
	for _, test := range []struct {
		name        string
		backend     Backend
		hardwareErr error
		want        Backend
		wantErr     error
		probes      int
		fallback    bool
	}{
		{name: "default software", want: BackendSoftware},
		{name: "explicit software", backend: BackendSoftware, want: BackendSoftware},
		{name: "invalid", backend: "unknown", wantErr: ErrBackend},
		{name: "explicit hardware", backend: BackendNVENC, want: BackendNVENC, probes: 1},
		{name: "hardware failure", backend: BackendNVENC, hardwareErr: unavailable, wantErr: unavailable, probes: 1},
		{name: "auto hardware", backend: BackendAuto, want: BackendNVENC, probes: 1},
		{
			name: "auto fallback", backend: BackendAuto, hardwareErr: unavailable,
			want: BackendSoftware, probes: 1, fallback: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			codec := &recordingCodec{}
			var probes int
			encoder, err := newEncoderWithNVENC(640, 480, Options{Threads: 2, Backend: test.backend},
				func(int, int, int) (codecEncoder, error) {
					probes++
					if test.hardwareErr != nil {
						return nil, test.hardwareErr
					}

					return codec, nil
				})
			if !errors.Is(err, test.wantErr) || probes != test.probes {
				t.Fatalf("error = %v, probes = %d; want %v, %d", err, probes, test.wantErr, test.probes)
			}
			if err != nil {
				if encoder != nil {
					t.Fatal("failed construction returned an encoder")
				}

				return
			}
			defer encoder.Close()
			if encoder.Backend() != test.want || (encoder.FallbackReason() != nil) != test.fallback {
				t.Fatalf("backend = %s, fallback = %v", encoder.Backend(), encoder.FallbackReason())
			}
			if test.fallback && !errors.Is(encoder.FallbackReason(), unavailable) {
				t.Fatalf("fallback lost hardware error: %v", encoder.FallbackReason())
			}
			wantThreads := 2
			if test.want == BackendNVENC {
				wantThreads = 0
			}
			if encoder.Threads() != wantThreads {
				t.Fatalf("threads = %d, want %d", encoder.Threads(), wantThreads)
			}
			encoder.Close()
			if test.want == BackendNVENC && codec.closed != 1 {
				t.Fatalf("hardware closed %d times", codec.closed)
			}
		})
	}
}

type failingHardwareCodec struct {
	codecEncoder
	err    error
	closed int
}

func (c *failingHardwareCodec) encode(pixels []byte, force bool, timestamp int64) ([]byte, error) {
	if c.err != nil {
		return nil, c.err
	}

	return c.codecEncoder.encode(pixels, force, timestamp)
}

func (c *failingHardwareCodec) close() {
	c.closed++
	c.codecEncoder.close()
}

func TestEncoderBackendFailure(t *testing.T) {
	t.Parallel()
	for _, backend := range []Backend{BackendAuto, BackendNVENC} {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			// A real codec supplies the initial stream; the wrapper injects a
			// device failure without requiring a GPU in normal test runs.
			const frameRate = 30
			codec, _, err := newCodecEncoder(64, 48, 1, frameRate)
			if err != nil {
				t.Fatal(err)
			}
			hardware := &failingHardwareCodec{codecEncoder: codec}
			encoder, err := newEncoderWithNVENC(64, 48, Options{Threads: 2, Backend: backend, FrameRate: frameRate},
				func(int, int, int) (codecEncoder, error) { return hardware, nil })
			if err != nil {
				hardware.close()
				t.Fatal(err)
			}
			defer encoder.Close()
			img := image.NewRGBA(image.Rect(0, 0, 64, 48))
			fillColorBars(img, 0)
			first, err := encoder.Encode(img, false)
			if err != nil {
				t.Fatal(err)
			}
			firstCopy := bytes.Clone(first)
			// Preserve a previously sent change outside the failed frame's damage.
			img.SetRGBA(2, 2, color.RGBA{R: 255, A: 255})
			if _, err := encoder.EncodeDamage(img, []image.Rectangle{image.Rect(2, 2, 3, 3)}, false); err != nil {
				t.Fatal(err)
			}
			img.SetRGBA(50, 30, color.RGBA{G: 255, A: 255})
			hardware.err = fmt.Errorf("%w: device lost", ErrCodec)
			data, err := encoder.EncodeDamage(img, []image.Rectangle{image.Rect(50, 30, 51, 31)}, false)
			if backend == BackendNVENC {
				if !errors.Is(err, hardware.err) || encoder.Backend() != BackendNVENC || hardware.closed != 0 {
					t.Fatalf("strict backend hid failure: %v, backend %s, closes %d", err, encoder.Backend(), hardware.closed)
				}
				if encoder.FallbackReason() != nil {
					t.Fatal("strict backend reported automatic fallback")
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if encoder.Backend() != BackendSoftware ||
				!errors.Is(encoder.FallbackReason(), hardware.err) || hardware.closed != 1 {
				t.Fatalf("failed recovery: backend %s, reason %v, closes %d",
					encoder.Backend(), encoder.FallbackReason(), hardware.closed)
			}
			// Small pictures reduce the requested two slices to one.
			if encoder.Threads() != 1 {
				t.Fatalf("fallback software slices = %d, want 1", encoder.Threads())
			}
			if !hasNAL(data, 7) || !hasNAL(data, 8) || !hasNAL(data, 5) {
				t.Fatal("fallback did not return an independent SPS/PPS/IDR frame")
			}
			if !bytes.Equal(first, firstCopy) {
				t.Fatal("fallback invalidated a previously returned frame")
			}
			checkSoftwareFrameRate(t, encoder, frameRate)
			if got, want := encoder.codec.(*openH264Encoder).frame.Timestamp, int64(2*1000/frameRate); got != want {
				t.Fatalf("fallback timestamp = %d, want %d", got, want)
			}
			checkSoftwareFallbackImage(t, data, img)
			if data, err := encoder.EncodeDamage(img, []image.Rectangle{}, false); data != nil || err != nil {
				t.Fatalf("recovery did not clear retry state: %v, %v", data, err)
			}
			encoder.Close()
			if hardware.closed != 1 {
				t.Fatal("failed hardware was closed twice")
			}
		})
	}
}

func checkSoftwareFallbackImage(t *testing.T, data []byte, img *image.RGBA) {
	t.Helper()
	decoder, err := openh264.NewDecoder(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	frame, err := decoder.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if frame == nil {
		frames, err := decoder.Flush()
		if err != nil || len(frames) != 1 {
			t.Fatalf("independent fallback decode: frames %d, error %v", len(frames), err)
		}
		frame = frames[0]
	}
	pixels, want := appendFrameI420(nil, frame), referenceI420(64, 48, img)
	if len(pixels) != len(want) {
		t.Fatalf("fallback decoded %d bytes, want %d", len(pixels), len(want))
	}
	var difference int
	for i, value := range want {
		difference += max(int(value)-int(pixels[i]), int(pixels[i])-int(value))
	}
	if average := float64(difference) / float64(len(want)); average > 3 {
		t.Fatalf("fallback lost retained image: average sample error %f", average)
	}
}
