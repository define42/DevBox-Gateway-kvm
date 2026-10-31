package avc

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"os/exec"
	"testing"
)

const (
	bootTransitionWidth  = 1920
	bootTransitionHeight = 1080
)

//nolint:paralleltest,tparallel // Sequential subtests bound full-HD codec and reference-picture memory.
func TestEncoderBootToDesktopTransition(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		width, height int
	}{
		{"native-desktop", 1920, 1080},
		{"scaled-desktop", 1366, 768},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoder, err := NewEncoderWithOptions(bootTransitionWidth, bootTransitionHeight, Options{Threads: 4})
			if err != nil {
				t.Fatal(err)
			}
			defer encoder.Close()

			var stream bytes.Buffer
			var expected [][]byte
			boot := bootTransitionImage(1024, 768, 0)
			appendBootTransitionFrame(t, encoder, boot, false, nil, &stream, &expected)

			// Cover RGBA boot capture followed by a BGRX guest desktop. The coded
			// output remains full HD when the source format and geometry change.
			desktop := bootTransitionImage(test.width, test.height, 1)
			// Even an empty damage list must rebuild the retained I420 image
			// when the source geometry changes beneath an existing encoder.
			appendBootTransitionFrame(t, encoder, desktop, true, []image.Rectangle{}, &stream, &expected)

			regions := []image.Rectangle{
				image.Rect(test.width/3-3, test.height/4-3, test.width/3+39, test.height/4+35),
				image.Rect(test.width-17, test.height-13, test.width, test.height),
			}
			for _, rect := range regions {
				draw.Draw(desktop, rect, &image.Uniform{C: color.RGBA{225, 41, 173, 255}}, image.Point{}, draw.Src)
			}
			appendBootTransitionFrame(t, encoder, desktop, true, regions, &stream, &expected)
			encoder.Close()

			// Reconnecting starts directly from the already-running desktop.
			// Its first picture must be complete even with partial source damage.
			reconnected, err := NewEncoderWithOptions(bootTransitionWidth, bootTransitionHeight, Options{Threads: 4})
			if err != nil {
				t.Fatal(err)
			}
			defer reconnected.Close()
			var reconnectStream bytes.Buffer
			var reconnectExpected [][]byte
			appendBootTransitionFrame(t, reconnected, desktop, true, regions, &reconnectStream, &reconnectExpected)
			if !bytes.Equal(expected[len(expected)-1], reconnectExpected[0]) {
				t.Fatal("reconnected encoder converted a different desktop")
			}
			reconnected.Close()

			t.Run("ffmpeg", func(t *testing.T) {
				ffmpeg, err := exec.LookPath("ffmpeg")
				if err != nil {
					t.Skip("ffmpeg is not installed")
				}
				checkBootTransitionStream(t, ffmpeg, stream.Bytes(), expected)
				checkBootTransitionStream(t, ffmpeg, reconnectStream.Bytes(), reconnectExpected)
			})
		})
	}
}

func bootTransitionImage(width, height, phase int) *image.RGBA {
	// Keep at least one padding cache line, including at aligned widths.
	stride := (width*4 + 127) &^ 63
	img := &image.RGBA{
		Pix:    bytes.Repeat([]byte{0xa5}, (height-1)*stride+width*4),
		Stride: stride,
		Rect:   image.Rect(0, 0, width, height),
	}
	for y := range height {
		for x := range width {
			pixel := img.Pix[y*stride+x*4:][:4]
			pixel[0] = byte(16 + x*191/width)
			pixel[1] = byte(24 + y*167/height)
			pixel[2] = byte(32 + ((x/64+y/32+phase)%4)*48)
			pixel[3] = 255
		}
	}

	return img
}

func appendBootTransitionFrame(
	t *testing.T, encoder *Encoder, img *image.RGBA, bgrx bool, regions []image.Rectangle,
	stream *bytes.Buffer, expected *[][]byte,
) {
	t.Helper()
	var err error
	if bgrx {
		err = encoder.PrepareBGRXDamage(
			packedBGRXForPreparation(img), img.Stride, img.Rect.Dx(), img.Rect.Dy(), regions, false,
		)
	} else {
		err = encoder.PrepareDamage(img, regions, false)
	}
	if err != nil {
		t.Fatalf("prepare frame %d: %v", len(*expected), err)
	}
	want := referenceI420(bootTransitionWidth, bootTransitionHeight, img)
	if !bytes.Equal(encoder.i420, want) {
		for index, value := range want {
			if encoder.i420[index] != value {
				t.Fatalf("frame %d I420 byte %d = %d, want %d", len(*expected), index, encoder.i420[index], value)
			}
		}
	}
	data, err := encoder.EncodePrepared()
	if err != nil || len(data) == 0 {
		t.Fatalf("encode frame %d: empty=%t, error=%v", len(*expected), len(data) == 0, err)
	}
	stream.Write(data)
	*expected = append(*expected, want)
}

func checkBootTransitionStream(t *testing.T, ffmpeg string, stream []byte, expected [][]byte) {
	t.Helper()
	decoded := decodeDamageInteropStream(t, ffmpeg, stream)
	frameSize := bootTransitionWidth * bootTransitionHeight * 3 / 2
	if len(decoded) != len(expected)*frameSize {
		t.Fatalf("decoded %d bytes, want %d frames of %d bytes", len(decoded), len(expected), frameSize)
	}
	for frame, want := range expected {
		got := decoded[frame*frameSize : (frame+1)*frameSize]
		var total int
		for index, value := range want {
			total += max(int(got[index])-int(value), int(value)-int(got[index]))
		}
		if average := float64(total) / float64(frameSize); average > 3 {
			t.Fatalf("frame %d mean sample error = %.3f", frame, average)
		}
		// Check local luma as well: a whole-frame mean can hide a missing
		// small damage region or a bad strip at a slice or picture boundary.
		for top := 0; top < bootTransitionHeight; top += 16 {
			for left := 0; left < bootTransitionWidth; left += 16 {
				block := image.Rect(left, top, min(left+16, bootTransitionWidth), min(top+16, bootTransitionHeight))
				checkDamageInteropBlock(t, frame, "Y", 0, bootTransitionWidth, block, got, want)
			}
		}
	}
}
