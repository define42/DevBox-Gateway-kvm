package rdp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"
	"strconv"
	"testing"
)

func TestBitmapEncoderPixelLayout(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{16, 24, 32} {
		t.Run(strconv.Itoa(depth), func(t *testing.T) {
			t.Parallel()
			// A subimage exercises nonzero origin and a stride wider than its rows.
			source := image.NewRGBA(image.Rect(0, 0, 12, 8)).SubImage(image.Rect(3, 2, 6, 4)).(*image.RGBA)
			colors := []color.RGBA{
				{255, 0, 0, 255},
				{0, 255, 0, 255},
				{0, 0, 255, 255},
				{255, 255, 255, 255},
				{0, 0, 0, 255},
				{248, 128, 64, 255},
			}
			for i, value := range colors {
				source.SetRGBA(3+i%3, 2+i/3, value)
			}
			encoder, err := NewBitmapEncoder(67, 65, depth)
			if err != nil {
				t.Fatal(err)
			}
			decoded := image.NewRGBA(image.Rect(0, 0, 67, 65))
			updates := 0
			err = encoder.WriteFrame(source, false, func(packet []byte) error {
				updates++
				decodeTestBitmap(t, decoded, packet, depth)

				return nil
			})
			if err != nil || updates != 4 {
				t.Fatalf("updates=%d err=%v", updates, err)
			}
			for y := range 65 {
				for x := range 67 {
					want := colors[(y*2/65)*3+x*3/67]
					if depth == 16 {
						want.R &= 0xf8
						want.G &= 0xfc
						want.B &= 0xf8
					}
					if got := decoded.RGBAAt(x, y); got != want {
						t.Fatalf("pixel (%d,%d): got %v want %v", x, y, got, want)
					}
				}
			}
		})
	}
}

func decodeTestBitmap(t *testing.T, dst *image.RGBA, packet []byte, depth int) {
	t.Helper()
	if len(packet) < 22 || len(packet) >= 32767 || binary.LittleEndian.Uint16(packet) != 1 ||
		binary.LittleEndian.Uint16(packet[2:]) != 1 {
		t.Fatalf("invalid bitmap update header/length: %d", len(packet))
	}
	x, y := int(binary.LittleEndian.Uint16(packet[4:])), int(binary.LittleEndian.Uint16(packet[6:]))
	w, h := int(binary.LittleEndian.Uint16(packet[12:])), int(binary.LittleEndian.Uint16(packet[14:]))
	if int(binary.LittleEndian.Uint16(packet[8:])) != x+w-1 || int(binary.LittleEndian.Uint16(packet[10:])) != y+h-1 ||
		int(binary.LittleEndian.Uint16(packet[16:])) != depth || binary.LittleEndian.Uint16(packet[18:]) != 0 ||
		int(binary.LittleEndian.Uint16(packet[20:])) != len(packet)-22 {
		t.Fatal("inconsistent bitmap rectangle")
	}
	stride := ((w*depth/8 + 3) / 4) * 4
	if len(packet) != 22+stride*h {
		t.Fatal("incorrect row padding")
	}
	for row := range h {
		for col := range w {
			pixel := packet[22+row*stride+col*depth/8:]
			c := color.RGBA{A: 255}
			if depth == 16 {
				value := binary.LittleEndian.Uint16(pixel)
				c.R, c.G, c.B = uint8(value>>11)<<3, uint8(value>>5&63)<<2, uint8(value&31)<<3
			} else {
				c.R, c.G, c.B = pixel[2], pixel[1], pixel[0]
			}
			dst.SetRGBA(x+col, y+h-1-row, c)
		}
	}
}

func TestBitmapEncoderDirtyTilesAndFailedWrite(t *testing.T) {
	t.Parallel()
	encoder, err := NewBitmapEncoder(128, 128, 24)
	if err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, 128, 128))
	count := 0
	send := func([]byte) error {
		count++

		return nil
	}
	if err := encoder.WriteFrame(frame, false, send); err != nil || count != 4 {
		t.Fatalf("initial frame: updates=%d err=%v", count, err)
	}
	count = 0
	if err := encoder.WriteFrame(frame, false, send); err != nil || count != 0 {
		t.Fatalf("unchanged frame: updates=%d err=%v", count, err)
	}
	frame.SetRGBA(100, 100, color.RGBA{255, 0, 0, 255})
	failure := io.ErrClosedPipe
	if err := encoder.WriteFrame(frame, false, func([]byte) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("write error: %v", err)
	}
	if err := encoder.WriteFrame(frame, false, send); err != nil || count != 1 {
		t.Fatalf("retry changed tile: updates=%d err=%v", count, err)
	}
	count = 0
	if err := encoder.WriteFrame(frame, true, send); err != nil || count != 4 {
		t.Fatalf("forced refresh: updates=%d err=%v", count, err)
	}
}

func TestBitmapScaleRegionReference(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{16, 24, 32} {
		for _, test := range []struct {
			name    string
			source  image.Rectangle
			output  image.Point
			padding int
		}{
			{name: "same-size", source: image.Rect(0, 0, 17, 9), output: image.Pt(17, 9), padding: 0},
			{
				name: "padded-subimage", source: image.Rect(-4, 2, 13, 11),
				output: image.Pt(17, 9), padding: 5,
			},
			{name: "single-pixel", source: image.Rect(13, -7, 14, -6), output: image.Pt(1, 1), padding: 0},
			{
				name: "upscaled", source: image.Rect(-4, 2, 3, 7),
				output: image.Pt(17, 9), padding: 3,
			},
			{
				name: "downscaled", source: image.Rect(5, -3, 38, 16),
				output: image.Pt(17, 9), padding: 2,
			},
		} {
			t.Run(strconv.Itoa(depth)+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				output := image.Rectangle{Min: image.Pt(0, 0), Max: test.output}
				for _, region := range []struct {
					name string
					rect image.Rectangle
				}{
					{name: "full", rect: output},
					{name: "interior", rect: output.Inset(min(output.Dx(), output.Dy()) / 3)},
					{name: "last-pixel", rect: image.Rectangle{Min: output.Max.Sub(image.Pt(1, 1)), Max: output.Max}},
					{name: "empty-column", rect: image.Rect(0, 0, 0, output.Dy())},
					{
						name: "empty-outside",
						rect: image.Rectangle{Min: output.Max.Add(image.Pt(2, 2)), Max: output.Max.Add(image.Pt(2, 3))},
					},
				} {
					t.Run(region.name, func(t *testing.T) {
						t.Parallel()
						parent := image.NewRGBA(test.source.Inset(-test.padding))
						for index := range parent.Pix {
							parent.Pix[index] = byte(index*31 + index/7)
						}
						img := parent.SubImage(test.source).(*image.RGBA)
						// Valid images need no trailing padding after their final row.
						img.Pix = img.Pix[:(img.Rect.Dy()-1)*img.Stride+img.Rect.Dx()*4]
						sourceBefore := bytes.Clone(parent.Pix)
						encoder, err := NewBitmapEncoder(test.output.X, test.output.Y, depth)
						if err != nil {
							t.Fatal(err)
						}
						encoder.pixels = make([]byte, encoder.width*encoder.height*encoder.bytesPerPixel)
						for index := range encoder.pixels {
							encoder.pixels[index] = byte(index*17 + 0x5a)
						}
						want := referenceBitmapRegion(img, encoder, region.rect)
						encoder.scaleRegion(img, region.rect)
						if !bytes.Equal(encoder.pixels, want) {
							t.Fatal("packed pixels differ from reference or changed outside the region")
						}
						if !bytes.Equal(parent.Pix, sourceBefore) {
							t.Fatal("packing modified source pixels or padding")
						}
					})
				}
			})
		}
	}
}

// referenceBitmapRegion uses logical image coordinates and explicit wire bytes,
// independently of the production row offsets and packing helpers. Seeded bytes
// outside the region and the reserved fourth byte must remain unchanged.
func referenceBitmapRegion(img *image.RGBA, encoder *BitmapEncoder, region image.Rectangle) []byte {
	want := bytes.Clone(encoder.pixels)
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			pixel := img.RGBAAt(
				img.Rect.Min.X+x*img.Rect.Dx()/encoder.width,
				img.Rect.Min.Y+y*img.Rect.Dy()/encoder.height,
			)
			offset := (y*encoder.width + x) * encoder.bytesPerPixel
			if encoder.bytesPerPixel == 2 {
				want[offset] = pixel.B>>3 | (pixel.G&0x1c)<<3
				want[offset+1] = pixel.R&0xf8 | pixel.G>>5
			} else {
				want[offset], want[offset+1], want[offset+2] = pixel.B, pixel.G, pixel.R
			}
		}
	}

	return want
}
