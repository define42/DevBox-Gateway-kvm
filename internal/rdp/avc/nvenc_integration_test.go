//go:build nvenc && cgo && integration

package avc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"testing"
)

// TestNVENCHardware exercises the actual GPU, including non-macroblock-aligned
// dimensions and independent decoding of a forced refresh. Opt in explicitly so
// native builds can still run their unit tests on machines without NVIDIA GPUs.
//
//nolint:paralleltest // GPU tests share limited hardware encoder sessions.
func TestNVENCHardware(t *testing.T) {
	if os.Getenv("GOKVM_TEST_NVENC") != "1" {
		t.Skip("set GOKVM_TEST_NVENC=1 to test the NVIDIA GPU")
	}
	for _, dimensions := range [][2]int{{1280, 720}, {642, 362}} {
		t.Run(fmt.Sprintf("%dx%d", dimensions[0], dimensions[1]), func(t *testing.T) {
			testNVENCRoundTrip(t, dimensions[0], dimensions[1])
		})
	}
}

func testNVENCRoundTrip(t *testing.T, width, height int) {
	t.Helper()
	codec, err := newNVENCEncoder(width, height)
	if err != nil {
		t.Fatal(err)
	}
	defer codec.close()
	if _, err := codec.encode(nil, false, 0); !errors.Is(err, ErrCodec) {
		t.Fatalf("malformed frame returned %v, want ErrCodec", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	colors := []color.RGBA{
		{R: 230, G: 30, B: 40, A: 255},
		{R: 20, G: 220, B: 40, A: 255},
		{R: 30, G: 40, B: 230, A: 255},
		{R: 240, G: 240, B: 240, A: 255},
	}
	var packets, expected [][]byte
	var firstCopy []byte
	for index := range 3 {
		for y := range height {
			for x := range width {
				value := colors[x*len(colors)/width]
				if x >= width/4 && x < width/2 && y >= height/4 && y < height/2 {
					value.R += byte(index * 15)
				}
				img.SetRGBA(x, y, value)
			}
		}
		i420 := make([]byte, width*height*3/2)
		scaleI420(i420, width, height, img)
		packet, err := codec.encode(i420, index == 2, int64(index*1000/FrameRate))
		if err != nil {
			t.Fatalf("encode frame %d: %v", index, err)
		}
		if len(packet) == 0 || !bytes.HasPrefix(packet, []byte{0, 0, 0, 1}) {
			t.Fatalf("frame %d lacks Annex-B data", index)
		}
		if index != 1 && (!hasNAL(packet, 7) || !hasNAL(packet, 8) || !hasNAL(packet, 5)) {
			t.Fatalf("frame %d lacks SPS/PPS/IDR NAL units", index)
		}
		if index == 1 && (!hasNAL(packet, 1) || hasNAL(packet, 5)) {
			t.Fatal("interframe did not produce a P-frame")
		}
		if index == 0 {
			firstCopy = bytes.Clone(packet)
		}
		packets = append(packets, packet)
		expected = append(expected, i420)
	}
	codec.close()
	if !bytes.Equal(packets[0], firstCopy) {
		t.Fatal("encoding or closing mutated an earlier access unit")
	}
	if _, err := codec.encode(expected[0], false, 100); !errors.Is(err, ErrCodec) {
		t.Fatalf("encode after close returned %v, want ErrCodec", err)
	}

	stream := bytes.Join(packets, nil)
	checkNVENCMetadata(t, stream, width, height)
	checkNVENCDecoded(t, stream, expected)
	// Refreshes must decode with a fresh client that has no preceding frames.
	checkNVENCDecoded(t, packets[2], expected[2:])
}

//nolint:tagliatelle // JSON field names are defined by ffprobe.
func checkNVENCMetadata(t *testing.T, stream []byte, width, height int) {
	t.Helper()
	probe := runNVENCDecoder(t, "ffprobe", stream,
		"-v", "error", "-f", "h264", "-show_streams", "-of", "json", "pipe:0")
	var metadata struct {
		Streams []struct {
			Profile        string `json:"profile"`
			Width          int    `json:"width"`
			Height         int    `json:"height"`
			HasBFrames     int    `json:"has_b_frames"`
			ColorRange     string `json:"color_range"`
			ColorSpace     string `json:"color_space"`
			ColorTransfer  string `json:"color_transfer"`
			ColorPrimaries string `json:"color_primaries"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(probe, &metadata); err != nil {
		t.Fatalf("parse ffprobe output: %v", err)
	}
	if len(metadata.Streams) != 1 {
		t.Fatalf("ffprobe returned %d streams", len(metadata.Streams))
	}
	s := metadata.Streams[0]
	if (s.Profile != "Constrained Baseline" && s.Profile != "Baseline") || s.Width != width ||
		s.Height != height || s.HasBFrames != 0 || s.ColorRange != "pc" || s.ColorSpace != "bt709" ||
		s.ColorTransfer != "bt709" || s.ColorPrimaries != "bt709" {
		t.Fatalf("unexpected H.264 metadata: %+v", s)
	}
}

func checkNVENCDecoded(t *testing.T, stream []byte, expected [][]byte) {
	t.Helper()
	decoded := runNVENCDecoder(t, "ffmpeg", stream,
		"-hide_banner", "-loglevel", "error", "-f", "h264", "-i", "pipe:0",
		"-map", "0:v:0", "-f", "rawvideo", "-pix_fmt", "yuvj420p", "pipe:1")
	frameSize := len(expected[0])
	if len(decoded) != len(expected)*frameSize {
		t.Fatalf("decoded %d bytes, want %d", len(decoded), len(expected)*frameSize)
	}
	for frame, want := range expected {
		var difference int
		for index, value := range want {
			got := int(decoded[frame*frameSize+index])
			difference += max(int(value)-got, got-int(value))
		}
		if average := float64(difference) / float64(frameSize); average > 3 {
			t.Fatalf("frame %d average sample error = %f", frame, average)
		}
	}
}

func runNVENCDecoder(t *testing.T, name string, input []byte, args ...string) []byte {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("hardware integration test requires %s: %v", name, err)
	}
	//nolint:gosec // Test decoder resolved from PATH with fixed arguments.
	command := exec.CommandContext(t.Context(), path, args...)
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("%s: %v: %s", name, err, stderr.Bytes())
	}

	return output
}
