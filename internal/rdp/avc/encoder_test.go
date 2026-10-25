package avc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"math/rand/v2"
	"testing"
)

func TestGeometry(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{0, 32}, {32, -1}, {15, 32}, {32, 17}, {4098, 32}, {32, 4098}} {
		if encoder, err := NewEncoder(size[0], size[1]); !errors.Is(err, ErrGeometry) || encoder != nil {
			t.Fatalf("NewEncoder(%v) = %v, %v", size, encoder, err)
		}
	}
}

func TestI420ColorConversion(t *testing.T) {
	t.Parallel()
	// The expected values come directly from the MS-RDPEGFX color matrix.
	for _, test := range []struct {
		name string
		rgb  color.RGBA
		yuv  [3]byte
	}{
		{"black", color.RGBA{A: 255}, [3]byte{0, 128, 128}},
		{"white", color.RGBA{R: 255, G: 255, B: 255, A: 255}, [3]byte{254, 128, 128}},
		{"red", color.RGBA{R: 255, A: 255}, [3]byte{53, 99, 255}},
		{"green", color.RGBA{G: 255, A: 255}, [3]byte{182, 29, 12}},
		{"blue", color.RGBA{B: 255, A: 255}, [3]byte{17, 255, 116}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			img := image.NewRGBA(image.Rect(0, 0, 2, 2))
			for y := range 2 {
				for x := range 2 {
					img.SetRGBA(x, y, test.rgb)
				}
			}
			got := make([]byte, 6)
			scaleI420(got, 2, 2, img)
			want := []byte{test.yuv[0], test.yuv[0], test.yuv[0], test.yuv[0], test.yuv[1], test.yuv[2]}
			if !bytes.Equal(got, want) {
				t.Fatalf("I420 = %v, want %v", got, want)
			}
		})
	}
}

func TestI420SubimageAndScaling(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(-5, -7, 5, 3))
	img.SetRGBA(-3, -5, color.RGBA{R: 255, A: 255})
	img.SetRGBA(-2, -5, color.RGBA{G: 255, A: 255})
	img.SetRGBA(-3, -4, color.RGBA{B: 255, A: 255})
	img.SetRGBA(-2, -4, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	sub := img.SubImage(image.Rect(-3, -5, -1, -3)).(*image.RGBA)
	got := make([]byte, 24)
	scaleI420(got, 4, 4, sub)
	want := []byte{
		53, 53, 182, 182, 53, 53, 182, 182,
		17, 17, 254, 254, 17, 17, 254, 254,
		99, 29, 255, 128, 255, 12, 116, 128,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("scaled I420 = %v, want %v", got, want)
	}

	got = make([]byte, 6)
	scaleI420(got, 2, 2, sub)
	if !bytes.Equal(got, []byte{53, 182, 17, 254, 128, 128}) {
		t.Fatalf("averaged chroma = %v", got)
	}
}

func TestI420MatchesReference(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                      string
		sourceWidth, sourceHeight int
		width, height             int
	}{
		{"native", 1024, 768, 1024, 768},
		{"upscale", 800, 600, 1024, 768},
		{"downscale", 1920, 1080, 1024, 768},
		{"odd-source", 17, 21, 32, 16},
		{"single-pixel", 1, 1, 16, 16},
		{"maximum-width", 31, 17, maxDimension, 16},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// A subimage exercises nonzero origins, padding between rows, and
			// unused bytes before and after the source rectangle.
			parent := image.NewRGBA(image.Rect(-7, -9, test.sourceWidth+3, test.sourceHeight+5))
			random := rand.New(rand.NewPCG(17, 31)) //nolint:gosec // Deterministic test pixels, not security-sensitive.
			for index := range parent.Pix {
				parent.Pix[index] = byte(random.Uint32())
			}
			img := parent.SubImage(image.Rect(-3, -5, test.sourceWidth-3, test.sourceHeight-5)).(*image.RGBA)
			want := referenceI420(test.width, test.height, img)
			got := make([]byte, len(want))
			scaleI420(got, test.width, test.height, img)
			if !bytes.Equal(got, want) {
				for index, value := range want {
					if got[index] != value {
						t.Fatalf("I420 byte %d = %d, want %d", index, got[index], value)
					}
				}
			}
		})
	}
}

// referenceI420 deliberately samples each output pixel independently; it is
// the scalar definition of nearest-neighbor scaling and 2x2 chroma averaging.
func referenceI420(width, height int, img *image.RGBA) []byte {
	luma := width * height
	dst := make([]byte, luma*3/2)
	for y := 0; y < height; y += 2 {
		for x := 0; x < width; x += 2 {
			var red, green, blue int
			for dy := range 2 {
				for dx := range 2 {
					pixel := img.RGBAAt(img.Rect.Min.X+(x+dx)*img.Rect.Dx()/width,
						img.Rect.Min.Y+(y+dy)*img.Rect.Dy()/height)
					r, g, b := int(pixel.R), int(pixel.G), int(pixel.B)
					dst[(y+dy)*width+x+dx] = byte((54*r + 183*g + 18*b) >> 8)
					red, green, blue = red+r, green+g, blue+b
				}
			}
			red, green, blue = red/4, green/4, blue/4
			pos := (y/2)*(width/2) + x/2
			dst[luma+pos] = byte(((-29*red - 99*green + 128*blue) >> 8) + 128)
			dst[luma+luma/4+pos] = byte(((128*red - 116*green - 12*blue) >> 8) + 128)
		}
	}

	return dst
}

type recordingCodec struct {
	forces     []bool
	timestamps []int64
	closed     int
}

func (c *recordingCodec) encode(_ []byte, force bool, timestamp int64) ([]byte, error) {
	c.forces = append(c.forces, force)
	c.timestamps = append(c.timestamps, timestamp)

	return []byte{1}, nil
}

func (c *recordingCodec) close() { c.closed++ }

func TestEncoderRefreshAndClose(t *testing.T) {
	t.Parallel()
	codec := &recordingCodec{}
	encoder := &Encoder{codec: codec, frameRate: FrameRate, width: 16, height: 16, i420: make([]byte, 16*16*3/2)}
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for _, force := range []bool{false, false, true} {
		if _, err := encoder.Encode(img, force); err != nil {
			t.Fatal(err)
		}
	}
	if len(codec.forces) != 2 || codec.forces[0] || !codec.forces[1] || codec.timestamps[1] != 1000/FrameRate {
		t.Fatalf("encode calls: force=%v timestamp=%v", codec.forces, codec.timestamps)
	}

	img.SetRGBA(0, 0, color.RGBA{R: 255})
	if _, err := encoder.Encode(img, false); err != nil || len(codec.forces) != 3 {
		t.Fatalf("changed frame = %v, %d calls", err, len(codec.forces))
	}
	encoder.Close()
	encoder.Close()
	if codec.closed != 1 {
		t.Fatalf("codec close called %d times", codec.closed)
	}
	if _, err := encoder.Encode(img, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("Encode after Close = %v", err)
	}
}

func TestRejectMalformedRGBA(t *testing.T) {
	t.Parallel()
	codec := &recordingCodec{}
	encoder := &Encoder{codec: codec, frameRate: FrameRate, width: 16, height: 16, i420: make([]byte, 16*16*3/2)}
	defer encoder.Close()
	for _, img := range []*image.RGBA{
		nil,
		{},
		{Pix: make([]byte, 4), Stride: 8, Rect: image.Rect(0, 0, 2, 2)},
		{Pix: make([]byte, 16), Stride: 0, Rect: image.Rect(0, 0, 2, 2)},
		{Pix: make([]byte, 16), Stride: -1, Rect: image.Rect(0, 0, 2, 2)},
		{Pix: make([]byte, 16), Stride: 8, Rect: image.Rect(0, 0, 2, 3)},
	} {
		if _, err := encoder.Encode(img, false); !errors.Is(err, ErrGeometry) {
			t.Fatalf("malformed RGBA returned %v", err)
		}
	}
	if len(codec.forces) != 0 {
		t.Fatal("malformed image reached codec")
	}
}

func TestEncoderCloseReleasesStorage(t *testing.T) {
	t.Parallel()
	encoder := &Encoder{
		codec: &recordingCodec{}, i420: make([]byte, 32*32*3/2), unchanged: make([]bool, 4), prepared: true,
	}
	encoder.Close()
	encoder.Close()
	if encoder.i420 != nil || encoder.unchanged != nil || encoder.prepared {
		t.Fatal("closed encoder retained picture storage or prepared state")
	}
}
