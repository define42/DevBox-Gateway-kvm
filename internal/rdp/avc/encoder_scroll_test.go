package avc

import (
	"fmt"
	"image"
	"os/exec"
	"testing"
)

// Large scrolls used to force out-of-range motion into the next ordinary
// macroblock's cost lookup. QP 0 exposes the resulting negative table index.
func TestEncoderLargeScrollRepaints(t *testing.T) {
	t.Parallel()
	for _, threads := range []int{1, 2} {
		t.Run(fmt.Sprintf("slices%d", threads), func(t *testing.T) {
			t.Parallel()
			const width, height = 128, 1536
			qp := 0
			encoder, err := NewEncoderWithOptions(width, height, Options{
				Threads: threads, SurfaceCopies: true, QP: &qp,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer encoder.Close()
			var stream []byte
			var expected [][]byte
			for frame, offset := range []int{0, 0, 600, 600, 0} {
				img := image.NewRGBA(image.Rect(0, 0, width, height))
				for y := range height {
					for x := range width {
						value := byte(((x*53 + (y+offset)*131) * ((y+offset)*77 + 13)) >> 5)
						pixel := img.Pix[img.PixOffset(x, y):][:4]
						pixel[0], pixel[1], pixel[2], pixel[3] = value, value, value, 255
					}
				}
				img.Pix[0] = byte(frame * 20) // Keep stationary frames in the reference chain.
				data, err := encoder.EncodeDamage(img, []image.Rectangle{img.Rect}, false)
				if err != nil || len(data) == 0 {
					t.Fatalf("frame %d: empty or failed encoding: %v", frame, err)
				}
				update := encoder.Update()
				if len(update.Copies) != 0 {
					t.Fatalf("frame %d: oversized scroll emitted copies: %v", frame, update.Copies)
				}
				if update.Tracked && update.Regions != nil {
					t.Fatalf("frame %d: full damage was not repainted: %+v", frame, update)
				}
				encoder.Sent(true)
				stream = append(stream, data...)
				expected = append(expected, referenceI420(width, height, img))
			}

			// The crash regression above runs even without FFmpeg. When available,
			// independently verify that fallback paints the current source pixels.
			t.Run("ffmpeg", func(t *testing.T) {
				t.Parallel()
				ffmpeg, err := exec.LookPath("ffmpeg")
				if err != nil {
					t.Skip("ffmpeg is not installed")
				}
				decoded := decodeDamageInteropStream(t, ffmpeg, stream)
				frameSize := width * height * 3 / 2
				if len(decoded) != len(expected)*frameSize {
					t.Fatalf("decoded %d bytes, want %d", len(decoded), len(expected)*frameSize)
				}
				for frame, want := range expected {
					got := decoded[frame*frameSize : (frame+1)*frameSize]
					for y := 0; y < height; y += 16 {
						for x := 0; x < width; x += 16 {
							block := image.Rect(x, y, x+16, y+16)
							checkDamageInteropBlock(t, frame, "Y", 0, width, block, got, want)
						}
					}
				}
			})
		})
	}
}
