//go:build nvenc && cgo && integration

package avc

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"os"
	"testing"
)

//nolint:paralleltest // GPU tests share limited hardware encoder sessions.
func TestNVENCHardwarePublicEncoder(t *testing.T) {
	if os.Getenv("GOKVM_TEST_NVENC") != "1" {
		t.Skip("set GOKVM_TEST_NVENC=1 to test the NVIDIA GPU")
	}
	for _, backend := range []Backend{BackendNVENC, BackendAuto} {
		t.Run(string(backend), func(t *testing.T) {
			const width, height = 640, 360
			options := Options{Backend: backend, Threads: 4, FrameRate: 30}
			encoder, err := NewEncoderWithOptions(width, height, options)
			if err != nil {
				t.Fatal(err)
			}
			defer encoder.Close()
			checkActiveNVENC(t, encoder)

			img := image.NewRGBA(image.Rect(0, 0, width, height))
			draw.Draw(img, img.Rect, image.NewUniform(color.RGBA{B: 220, A: 255}), image.Point{}, draw.Src)
			var packets, expected [][]byte
			encode := func(regions []image.Rectangle, force bool) {
				t.Helper()
				packet, err := encoder.EncodeDamage(img, regions, force)
				if err != nil {
					t.Fatal(err)
				}
				if len(packets) == 0 || force {
					if !hasNAL(packet, 7) || !hasNAL(packet, 8) || !hasNAL(packet, 5) {
						t.Fatal("initial or forced access unit lacks SPS/PPS/IDR")
					}
				} else if !hasNAL(packet, 1) || hasNAL(packet, 5) {
					t.Fatal("partial update did not produce a P-frame")
				}
				packets = append(packets, packet)
				expected = append(expected, referenceI420(width, height, img))
				checkActiveNVENC(t, encoder)
			}
			encode(nil, false)
			if data, err := encoder.Encode(img, false); err != nil || data != nil {
				t.Fatalf("unchanged image returned %d bytes, %v", len(data), err)
			}

			first := image.Rect(16, 16, width/2, height/2)
			draw.Draw(img, first, image.NewUniform(color.RGBA{R: 220, A: 255}), image.Point{}, draw.Src)
			encode([]image.Rectangle{first}, false)
			second := image.Rect(width/2, height/2, width-16, height-16)
			draw.Draw(img, second, image.NewUniform(color.RGBA{G: 220, A: 255}), image.Point{}, draw.Src)
			encode([]image.Rectangle{second}, false)
			if data, err := encoder.EncodeDamage(img, []image.Rectangle{}, false); err != nil || data != nil {
				t.Fatalf("empty damage returned %d bytes, %v", len(data), err)
			}
			// An unchanged image must still produce an independent refresh.
			encode([]image.Rectangle{}, true)
			encoder.Close()
			checkNVENCDecoded(t, bytes.Join(packets, nil), expected)
			checkNVENCDecoded(t, packets[3], expected[3:])

			// Session resize closes the old codec and starts a new stream. Its
			// first frame must contain fresh parameter sets even without force.
			resized, err := NewEncoderWithOptions(642, 362, options)
			if err != nil {
				t.Fatal(err)
			}
			defer resized.Close()
			checkActiveNVENC(t, resized)
			packet, err := resized.Encode(img, false)
			if err != nil {
				t.Fatal(err)
			}
			if !hasNAL(packet, 7) || !hasNAL(packet, 8) || !hasNAL(packet, 5) {
				t.Fatal("resized stream lacks SPS/PPS/IDR")
			}
			checkNVENCDecoded(t, packet, [][]byte{referenceI420(642, 362, img)})
			checkActiveNVENC(t, resized)
		})
	}
}

func checkActiveNVENC(t *testing.T, encoder *Encoder) {
	t.Helper()
	if encoder.FrameRate() != 30 {
		t.Fatalf("frame rate = %d, want 30", encoder.FrameRate())
	}
	if encoder.Backend() != BackendNVENC || encoder.Threads() != 0 || encoder.FallbackReason() != nil {
		t.Fatalf("backend=%s threads=%d fallback=%v, want nvenc with no fallback",
			encoder.Backend(), encoder.Threads(), encoder.FallbackReason())
	}
}
