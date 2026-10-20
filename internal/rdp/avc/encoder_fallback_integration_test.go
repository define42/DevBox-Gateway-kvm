//go:build nvenc && cgo && integration

package avc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"testing"
)

// TestNVENCHardwareFallback keeps one decoder alive across a real NVENC stream
// and its software replacement, exercising the parameter-set transition.
//
//nolint:paralleltest // GPU tests serialize access to limited hardware sessions.
func TestNVENCHardwareFallback(t *testing.T) {
	if os.Getenv("DEVBOX_KVM_TEST_NVENC") != "1" {
		t.Skip("set DEVBOX_KVM_TEST_NVENC=1 to test the NVIDIA GPU")
	}
	const width, height, frameRate = 640, 360, 30
	codec, err := newNVENCEncoder(width, height, frameRate)
	if err != nil {
		t.Fatal(err)
	}
	hardware := &failingHardwareCodec{codecEncoder: codec}
	encoder, err := newEncoderWithNVENC(width, height, Options{Backend: BackendAuto, Threads: 1, FrameRate: frameRate},
		func(int, int, int) (codecEncoder, error) { return hardware, nil })
	if err != nil {
		hardware.close()
		t.Fatal(err)
	}
	defer encoder.Close()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Rect, image.NewUniform(color.RGBA{B: 220, A: 255}), image.Point{}, draw.Src)
	var packets, expected [][]byte
	for index := range 3 {
		var damage []image.Rectangle
		if index > 0 {
			region := image.Rect(index*80, index*40, index*80+64, index*40+64)
			draw.Draw(img, region, image.NewUniform(color.RGBA{R: 220, G: 150, A: 255}), image.Point{}, draw.Src)
			damage = []image.Rectangle{region}
		}
		if index == 2 {
			hardware.err = fmt.Errorf("%w: injected device loss after native frames", ErrCodec)
		}
		packet, err := encoder.EncodeDamage(img, damage, false)
		if err != nil {
			t.Fatal(err)
		}
		packets = append(packets, packet)
		expected = append(expected, referenceI420(width, height, img))
	}
	if encoder.Backend() != BackendSoftware || !errors.Is(encoder.FallbackReason(), hardware.err) || hardware.closed != 1 {
		t.Fatalf("fallback: backend=%s reason=%v native closes=%d",
			encoder.Backend(), encoder.FallbackReason(), hardware.closed)
	}
	if !hasNAL(packets[2], 7) || !hasNAL(packets[2], 8) || !hasNAL(packets[2], 5) {
		t.Fatal("software replacement lacks SPS/PPS/IDR")
	}
	checkSoftwareFrameRate(t, encoder, frameRate)
	checkNVENCDecoded(t, bytes.Join(packets, nil), expected)
}
