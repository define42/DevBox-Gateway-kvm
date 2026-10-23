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

func TestEncoderQPValidation(t *testing.T) {
	t.Parallel()
	for _, requested := range []int{MinQP - 1, MinQP, DefaultQP, MaxQP, MaxQP + 1} {
		t.Run(fmt.Sprintf("QP%d", requested), func(t *testing.T) {
			t.Parallel()
			for _, backend := range []Backend{BackendSoftware, BackendAuto} {
				encoder, err := NewEncoderWithOptions(64, 48,
					Options{Threads: 1, Backend: backend, QP: &requested})
				if requested < MinQP || requested > MaxQP {
					if !errors.Is(err, ErrQP) || encoder != nil {
						t.Fatalf("invalid QP for %s: encoder=%v error=%v", backend, encoder, err)
					}

					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if encoder.QP() != requested || encoder.Backend() != BackendSoftware {
					t.Fatalf("backend %s: QP=%d selected=%s", backend, encoder.QP(), encoder.Backend())
				}
				packet, err := encoder.Encode(qpTestImage(64, 48), false)
				if err != nil || !hasNAL(packet, 5) {
					encoder.Close()
					t.Fatalf("initial frame: bytes=%d error=%v", len(packet), err)
				}
				checkSoftwareQP(t, encoder, requested)
				encoder.Close()
			}
		})
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
