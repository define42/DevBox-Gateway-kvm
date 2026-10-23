package avc

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"os/exec"
	"testing"
	"time"
)

const (
	damageInteropWidth  = 130
	damageInteropHeight = 98
)

// TestEncoderDamageFFmpegCompatibility exercises retained-picture encoding with
// an independent decoder. Per-block bounds catch a lost small update that a
// whole-picture average could hide. Like TestFFmpegCompatibility, this runs in
// ordinary CI when FFmpeg is installed and needs no external service or device.
func TestEncoderDamageFFmpegCompatibility(t *testing.T) {
	t.Parallel()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	for _, test := range []struct {
		name                      string
		threads                   int
		sourceWidth, sourceHeight int
	}{
		{"native-one-slice", 1, 130, 98},
		{"native-four-slices", 4, 130, 98},
		{"upscale-one-slice", 1, 67, 51},
		{"upscale-four-slices", 4, 67, 51},
		{"downscale-one-slice", 1, 201, 133},
		{"downscale-four-slices", 4, 201, 133},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stream, expected := damageInteropFrames(t, test.threads, test.sourceWidth, test.sourceHeight)
			decoded := decodeDamageInteropStream(t, ffmpeg, stream)
			frameSize := damageInteropWidth * damageInteropHeight * 3 / 2
			if len(decoded) != len(expected)*frameSize {
				t.Fatalf("decoded %d bytes, want %d frames of %d bytes", len(decoded), len(expected), frameSize)
			}
			for index, want := range expected {
				got := decoded[index*frameSize : (index+1)*frameSize]
				checkDamageInteropFrame(t, index, got, want)
			}
		})
	}
}

func damageInteropFrames(t *testing.T, threads, width, height int) ([]byte, [][]byte) {
	t.Helper()
	encoder, err := NewEncoderWithOptions(damageInteropWidth, damageInteropHeight, Options{
		Threads: threads, Backend: BackendSoftware,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	if want := expectedSoftwareSlices(threads); encoder.Threads() != want {
		t.Fatalf("effective slices = %d, want %d", encoder.Threads(), want)
	}

	// A subimage also exercises nonzero origins and padding in the RGBA stride.
	parent := image.NewRGBA(image.Rect(-7, -9, width+5, height+7))
	img := parent.SubImage(image.Rect(-3, -5, width-3, height-5)).(*image.RGBA)
	base := image.NewRGBA(img.Rect)
	draw.Draw(base, base.Rect, &image.Uniform{C: color.RGBA{48, 64, 80, 255}}, image.Point{}, draw.Src)
	chromaRect := image.Rect(width-25, height-19, width-8, height-6).Add(img.Rect.Min)
	chromaColors := [2]color.RGBA{{96, 120, 96, 255}, {96, 108, 218, 255}}
	draw.Draw(base, chromaRect, &image.Uniform{C: chromaColors[0]}, image.Point{}, draw.Src)
	draw.Draw(img, img.Rect, base, base.Rect.Min, draw.Src)

	var stream bytes.Buffer
	var expected [][]byte
	appendDamageInteropFrame(t, encoder, img, nil, false, &stream, &expected)
	previous := image.Rectangle{}
	for index := range 48 {
		if !previous.Empty() {
			draw.Draw(img, previous, base, previous.Min, draw.Src)
		}
		// Odd edges and changing positions cross macroblock and slice boundaries.
		x, y := 17+(index*13)%(width-39), 17+(index*7)%(height-35)
		moving := image.Rect(x, y, x+11, y+9).Add(img.Rect.Min)
		draw.Draw(img, moving, &image.Uniform{C: color.RGBA{220, 40, 48, 255}}, image.Point{}, draw.Src)
		// These RGB colors have identical BT.709 integer luma but differ in U.
		// A luma-only unchanged-block test must not discard this update.
		draw.Draw(img, chromaRect, &image.Uniform{C: chromaColors[(index+1)%2]}, image.Point{}, draw.Src)
		edge := image.Rect(width-3, height-3, width, height).Add(img.Rect.Min)
		shade := byte(100 + 100*(index%2))
		draw.Draw(img, edge, &image.Uniform{C: color.RGBA{shade, shade, shade, 255}}, image.Point{}, draw.Src)
		regions := []image.Rectangle{previous, moving, chromaRect, edge}
		appendDamageInteropFrame(t, encoder, img, regions, false, &stream, &expected)
		previous = moving
	}

	// A full refresh must recover an intentionally unreported change, and the
	// next sparse frame must reference that refreshed picture, not the old one.
	refresh := image.Rect(0, 0, 16, 16).Add(img.Rect.Min)
	draw.Draw(img, refresh, &image.Uniform{C: color.RGBA{200, 200, 200, 255}}, image.Point{}, draw.Src)
	appendDamageInteropFrame(t, encoder, img, []image.Rectangle{}, true, &stream, &expected)
	draw.Draw(img, previous, base, previous.Min, draw.Src)
	draw.Draw(img, chromaRect, &image.Uniform{C: chromaColors[1]}, image.Point{}, draw.Src)
	appendDamageInteropFrame(t, encoder, img, []image.Rectangle{previous, chromaRect}, false, &stream, &expected)

	return stream.Bytes(), expected
}

func appendDamageInteropFrame(
	t *testing.T, encoder *Encoder, img *image.RGBA, regions []image.Rectangle, force bool,
	stream *bytes.Buffer, expected *[][]byte,
) {
	t.Helper()
	data, err := encoder.EncodeDamage(img, regions, force)
	if err != nil {
		t.Fatalf("encode frame %d: %v", len(*expected), err)
	}
	if len(data) == 0 {
		t.Fatalf("frame %d lost an update", len(*expected))
	}
	if force && !hasNAL(data, 5) {
		t.Fatal("forced refresh did not produce an IDR")
	}
	stream.Write(data)
	*expected = append(*expected, referenceI420(damageInteropWidth, damageInteropHeight, img))
}

func decodeDamageInteropStream(t *testing.T, ffmpeg string, stream []byte) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg, //nolint:gosec // Executable is from PATH; arguments are fixed.
		"-hide_banner", "-loglevel", "error", "-threads", "1", "-f", "h264", "-i", "pipe:0",
		"-map", "0:v:0", "-threads", "1", "-f", "rawvideo", "-pix_fmt", "yuvj420p", "pipe:1")
	command.Stdin = bytes.NewReader(stream)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	decoded, err := command.Output()
	if err != nil {
		t.Fatalf("ffmpeg decode: %v: %s", err, stderr.Bytes())
	}
	if stderr.Len() != 0 {
		t.Fatalf("ffmpeg reported a decode error: %s", stderr.Bytes())
	}

	return decoded
}

func checkDamageInteropFrame(t *testing.T, frame int, got, want []byte) {
	t.Helper()
	luma := damageInteropWidth * damageInteropHeight
	for _, plane := range []struct {
		name                  string
		offset, width, height int
	}{
		{"Y", 0, damageInteropWidth, damageInteropHeight},
		{"U", luma, damageInteropWidth / 2, damageInteropHeight / 2},
		{"V", luma + luma/4, damageInteropWidth / 2, damageInteropHeight / 2},
	} {
		for top := 0; top < plane.height; top += 8 {
			for left := 0; left < plane.width; left += 8 {
				block := image.Rect(left, top, min(left+8, plane.width), min(top+8, plane.height))
				checkDamageInteropBlock(t, frame, plane.name, plane.offset, plane.width, block, got, want)
			}
		}
	}
}

func checkDamageInteropBlock(
	t *testing.T, frame int, plane string, offset, stride int, block image.Rectangle, got, want []byte,
) {
	t.Helper()
	var total, largest int
	for y := block.Min.Y; y < block.Max.Y; y++ {
		for x := block.Min.X; x < block.Max.X; x++ {
			pos := offset + y*stride + x
			difference := max(int(got[pos])-int(want[pos]), int(want[pos])-int(got[pos]))
			total += difference
			largest = max(largest, difference)
		}
	}
	average := float64(total) / float64(block.Dx()*block.Dy())
	if average > 8 || largest > 32 {
		t.Fatalf("frame %d %s block %v: mean error %.2f, maximum %d; missing or ghosted damage",
			frame, plane, block, average, largest)
	}
}
