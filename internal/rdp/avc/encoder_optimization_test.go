package avc

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"testing"
)

// These bundled pure-Go codec digests were captured before the second optimization pass. They cover
// complete access units, including refreshes, slice boundaries and reused input.
func TestEncoderOptimizationBitstreamParity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                      string
		width, height, qp, slices int
		want                      string
	}{
		{"66x50/QP0/1Slices", 66, 50, 0, 1, "ec6889135fd25f91fe279d93e88ccbb141eb49877dffb63278f3714cbba21554"},
		{"66x50/QP0/4Slices", 66, 50, 0, 4, "ec6889135fd25f91fe279d93e88ccbb141eb49877dffb63278f3714cbba21554"},
		{"66x50/QP20/1Slices", 66, 50, 20, 1, "a70743c630ad8522da3b2e52d0884221bd2bb5899eb6988739301a6382776dc6"},
		{"66x50/QP20/4Slices", 66, 50, 20, 4, "a70743c630ad8522da3b2e52d0884221bd2bb5899eb6988739301a6382776dc6"},
		{"66x50/QP51/1Slices", 66, 50, 51, 1, "17949d5ea6dc8f7ef979f2b283db32033fa983ee759f4b239b5cfc60134852a0"},
		{"66x50/QP51/4Slices", 66, 50, 51, 4, "17949d5ea6dc8f7ef979f2b283db32033fa983ee759f4b239b5cfc60134852a0"},
		{"320x192/QP0/1Slices", 320, 192, 0, 1, "598c8a066745f7c71de4ded78ae6f7b47f1c6a671030dc5077c98410959e1d25"},
		{"320x192/QP0/4Slices", 320, 192, 0, 4, "823551f03791b76fc43d512ac6d36be47e2d9c191f42ae9de847bf6350374cc9"},
		{"320x192/QP20/1Slices", 320, 192, 20, 1, "b8a008d7d9d1e4b844bd83ca5d8954bd97f0764c35013f36d2cddbc9c472e85c"},
		{"320x192/QP20/4Slices", 320, 192, 20, 4, "e937b6bb0bf2fcc05bf1f2bdfc36c499ebf8221448584b8e68bd0ef68b420cbd"},
		{"320x192/QP51/1Slices", 320, 192, 51, 1, "9240b72be7b43d30a304cc74a8b15594d643a03c9721b02971c3263d702045c1"},
		{"320x192/QP51/4Slices", 320, 192, 51, 4, "56498a7c06cac9bb77f7292c603b8b3adf3ab8d330de7725862492f7b4f6f51f"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoder, err := NewEncoderWithOptions(test.width, test.height,
				Options{Threads: test.slices, QP: &test.qp})
			if err != nil {
				t.Fatal(err)
			}
			defer encoder.Close()
			img := image.NewRGBA(image.Rect(5, -3, 5+test.width, -3+test.height))
			for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
				for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
					img.SetRGBA(x, y, color.RGBA{R: byte(x*7 + y*3), G: byte(x*2 - y*5), B: byte(x + y*11), A: 255})
				}
			}
			digest := sha256.New()
			var first, firstCopy []byte
			for frame := range 48 {
				x := img.Rect.Min.X + (frame*7)%(test.width-12)
				y := img.Rect.Min.Y + (frame*11)%(test.height-12)
				region := image.Rect(x, y, x+9, y+7)
				regions := []image.Rectangle{region}
				if frame%11 != 0 || frame == 0 {
					for py := region.Min.Y; py < region.Max.Y; py++ {
						for px := region.Min.X; px < region.Max.X; px++ {
							img.SetRGBA(px, py, color.RGBA{R: byte(frame*19 + px), G: byte(frame*7 + py), B: byte(frame * 37), A: 255})
						}
					}
				}
				if frame%9 == 8 {
					region = image.Rect(img.Rect.Min.X+2, img.Rect.Min.Y+2, img.Rect.Max.X-2, img.Rect.Max.Y-2)
					for py := region.Min.Y; py < region.Max.Y; py++ {
						off := img.PixOffset(region.Min.X, py)
						span := (region.Dx() - 1) * 4
						copy(img.Pix[off:off+span], img.Pix[off+4:off+4+span])
					}
					regions = []image.Rectangle{region}
				}
				if frame%13 == 0 {
					regions = nil
				}
				packet, err := encoder.EncodeDamage(img, regions, frame == 17 || frame == 39)
				if err != nil {
					t.Fatalf("frame %d: %v", frame, err)
				}
				if frame == 0 {
					first = packet
					firstCopy = bytes.Clone(packet)
				}
				var length [4]byte
				binary.BigEndian.PutUint32(length[:], uint32(len(packet)))
				_, _ = digest.Write(length[:])
				_, _ = digest.Write(packet)
			}
			if !bytes.Equal(first, firstCopy) {
				t.Fatal("later frames modified previously returned output")
			}
			got := fmt.Sprintf("%x", digest.Sum(nil))
			if got != test.want {
				t.Fatalf("bitstream digest %s, want %s", got, test.want)
			}
		})
	}
}
