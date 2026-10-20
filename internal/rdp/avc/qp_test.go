package avc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"testing"

	openh264 "github.com/define42/DevBox-Gateway-kvm/pkg/h264"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

func TestResolveQP(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		qp   *int
		want int
		err  error
	}{
		{name: "default", want: DefaultQP},
		{name: "minimum", qp: qpPointer(MinQP), want: MinQP},
		{name: "explicit default", qp: qpPointer(DefaultQP), want: DefaultQP},
		{name: "maximum", qp: qpPointer(MaxQP), want: MaxQP},
		{name: "negative", qp: qpPointer(MinQP - 1), err: ErrQP},
		{name: "above maximum", qp: qpPointer(MaxQP + 1), err: ErrQP},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveQP(test.qp)
			if got != test.want || !errors.Is(err, test.err) {
				t.Fatalf("ResolveQP = %d, %v; want %d, %v", got, err, test.want, test.err)
			}
		})
	}
}

func TestEncoderQPOptionsAndEffectiveFrames(t *testing.T) {
	t.Parallel()
	for _, requested := range []*int{nil, qpPointer(MinQP), qpPointer(DefaultQP), qpPointer(MaxQP)} {
		want, err := ResolveQP(requested)
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("QP%d", want)
		if requested == nil {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			encoder, err := NewEncoderWithOptions(64, 48, Options{Threads: 1, QP: requested})
			if err != nil {
				t.Fatal(err)
			}
			defer encoder.Close()
			if requested != nil {
				// The encoder must not retain a mutable configuration pointer.
				*requested = MaxQP + 1
			}
			img := qpTestImage(64, 48)
			for frame := range 3 {
				img.SetRGBA(2, 2, color.RGBA{R: byte(100 + frame*20), A: 255})
				packet, err := encoder.Encode(img, frame == 2)
				if err != nil || len(packet) == 0 {
					t.Fatalf("frame %d: bytes=%d, error=%v", frame, len(packet), err)
				}
				checkSoftwareQP(t, encoder, want)
			}
		})
	}
}

func TestHardwareQPOptionsAndValidation(t *testing.T) {
	t.Parallel()
	for _, requested := range []int{MinQP - 1, MinQP, DefaultQP, MaxQP, MaxQP + 1} {
		t.Run(fmt.Sprintf("QP%d", requested), func(t *testing.T) {
			t.Parallel()
			for _, backend := range []Backend{BackendSoftware, BackendNVENC, BackendAuto} {
				probes := 0
				encoder, err := newEncoderWithNVENC(64, 48,
					Options{Threads: 1, Backend: backend, QP: &requested},
					func(width, height, rate, qp int) (codecEncoder, error) {
						probes++
						if width != 64 || height != 48 || rate != FrameRate || qp != requested {
							t.Fatalf("hardware options: %dx%d, %d FPS, QP %d", width, height, rate, qp)
						}

						return &recordingCodec{}, nil
					})
				if requested < MinQP || requested > MaxQP {
					if !errors.Is(err, ErrQP) || probes != 0 || encoder != nil {
						t.Fatalf("invalid QP reached backend %s: encoder=%v probes=%d error=%v", backend, encoder, probes, err)
					}

					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if encoder.QP() != requested || backend != BackendSoftware && probes != 1 {
					t.Fatalf("backend %s: QP=%d probes=%d", backend, encoder.QP(), probes)
				}
				encoder.Close()
			}
		})
	}
}

func TestEncoderQPSurvivesFallback(t *testing.T) {
	t.Parallel()
	for _, qp := range []int{MinQP, 32, MaxQP} {
		for _, failAtInitialization := range []bool{false, true} {
			t.Run(fmt.Sprintf("QP%d/initFailure=%t", qp, failAtInitialization), func(t *testing.T) {
				t.Parallel()
				hardware := &failingHardwareCodec{codecEncoder: &recordingCodec{}}
				encoder, err := newEncoderWithNVENC(64, 48,
					Options{Threads: 1, Backend: BackendAuto, QP: &qp},
					func(_, _, _, gotQP int) (codecEncoder, error) {
						if gotQP != qp {
							t.Fatalf("hardware QP=%d, want %d", gotQP, qp)
						}
						if failAtInitialization {
							return nil, ErrCodec
						}

						return hardware, nil
					})
				if err != nil {
					t.Fatal(err)
				}
				defer encoder.Close()
				img := qpTestImage(64, 48)
				if _, err := encoder.Encode(img, false); err != nil {
					t.Fatal(err)
				}
				hardware.err = ErrCodec
				packet, err := encoder.Encode(img, true)
				if err != nil || !hasNAL(packet, 5) || encoder.Backend() != BackendSoftware {
					t.Fatalf("fallback frame: error=%v backend=%s bytes=%d", err, encoder.Backend(), len(packet))
				}
				checkSoftwareQP(t, encoder, qp)
			})
		}
	}
}

func TestEncoderQPChangesCompressionAndQuality(t *testing.T) {
	t.Parallel()
	img := qpTestImage(128, 96)
	var previousSize int
	var previousError uint64
	for _, qp := range []int{MinQP, DefaultQP, MaxQP} {
		encoder, err := NewEncoderWithOptions(128, 96, Options{Threads: 1, QP: &qp})
		if err != nil {
			t.Fatal(err)
		}
		packet, err := encoder.Encode(img, false)
		encoder.Close()
		if err != nil {
			t.Fatal(err)
		}
		decoded := decodeQPPackets(t, [][]byte{packet})
		want := referenceI420(128, 96, img)
		if len(decoded) != len(want) {
			t.Fatalf("QP%d: decoded=%d, expected=%d bytes", qp, len(decoded), len(want))
		}
		var squaredError uint64
		for index, value := range want {
			difference := int(value) - int(decoded[index])
			squaredError += uint64(difference * difference)
		}
		t.Logf("QP%d: %d bytes, squared sample error=%d", qp, len(packet), squaredError)
		if qp != MinQP && (len(packet) >= previousSize || squaredError <= previousError) {
			t.Fatalf("QP%d did not reduce bytes and increase quantization error", qp)
		}
		previousSize, previousError = len(packet), squaredError
	}
}

func TestQPDamageHintsPreserveDecodedFrames(t *testing.T) {
	t.Parallel()
	for _, qp := range []int{MinQP, DefaultQP, MaxQP} {
		t.Run(fmt.Sprintf("QP%d", qp), func(t *testing.T) {
			t.Parallel()
			var outputs [2][]byte
			for mode := range outputs {
				encoder, err := NewEncoderWithOptions(128, 96, Options{Threads: 1, QP: &qp})
				if err != nil {
					t.Fatal(err)
				}
				defer encoder.Close()
				img := qpTestImage(128, 96)
				var packets [][]byte
				for frame := range 6 {
					region := image.Rect(16, 16, 32, 32)
					for y := region.Min.Y; y < region.Max.Y; y++ {
						for x := region.Min.X; x < region.Max.X; x++ {
							img.SetRGBA(x, y, color.RGBA{R: byte(frame * 35), G: 70, B: 120, A: 255})
						}
					}
					var regions []image.Rectangle
					if mode == 1 {
						regions = []image.Rectangle{region}
					}
					packet, err := encoder.EncodeDamage(img, regions, false)
					if err != nil || len(packet) == 0 {
						t.Fatalf("frame %d: bytes=%d, error=%v", frame, len(packet), err)
					}
					checkSoftwareQP(t, encoder, qp)
					packets = append(packets, packet)
				}
				outputs[mode] = decodeQPPackets(t, packets)
			}
			if !bytes.Equal(outputs[0], outputs[1]) {
				t.Fatal("damage hints changed decoded pixels at configured QP")
			}
		})
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

func decodeQPPackets(t *testing.T, packets [][]byte) []byte {
	t.Helper()
	decoder, err := openh264.NewDecoder(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	var decoded []byte
	for _, packet := range packets {
		frame, err := decoder.Decode(packet)
		if err != nil {
			t.Fatal(err)
		}
		if frame != nil {
			decoded = appendFrameI420(decoded, frame)
		}
	}
	frames, err := decoder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range frames {
		decoded = appendFrameI420(decoded, frame)
	}

	return decoded
}

func qpTestImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetRGBA(x, y, color.RGBA{R: byte(x*13 + y*7), G: byte(x*3 + y*17), B: byte(x*y + 40), A: 255})
		}
	}

	return img
}

func qpPointer(value int) *int { return &value }
