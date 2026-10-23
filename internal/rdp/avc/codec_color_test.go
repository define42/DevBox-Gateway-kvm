package avc

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"os/exec"
	"testing"
	"time"
)

//nolint:tagliatelle // JSON field names are defined by ffprobe.
func TestCodecColorMetadata(t *testing.T) {
	t.Parallel()
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}
	const width, height = 130, 98
	encoder, err := NewEncoder(width, height)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	packet, err := encoder.Encode(image.NewRGBA(image.Rect(0, 0, width, height)), false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, ffprobe, //nolint:gosec // Executable is from PATH; arguments are fixed.
		"-v", "error", "-f", "h264", "-show_streams", "-of", "json", "pipe:0")
	command.Stdin = bytes.NewReader(packet)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("ffprobe: %v: %s", err, stderr.Bytes())
	}
	var metadata struct {
		Streams []struct {
			Width          int    `json:"width"`
			Height         int    `json:"height"`
			ColorRange     string `json:"color_range"`
			ColorSpace     string `json:"color_space"`
			ColorTransfer  string `json:"color_transfer"`
			ColorPrimaries string `json:"color_primaries"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &metadata); err != nil {
		t.Fatal(err)
	}
	if len(metadata.Streams) != 1 {
		t.Fatalf("ffprobe returned %d streams", len(metadata.Streams))
	}
	stream := metadata.Streams[0]
	if stream.Width != width || stream.Height != height || stream.ColorRange != "pc" ||
		stream.ColorSpace != "bt709" || stream.ColorTransfer != "bt709" || stream.ColorPrimaries != "bt709" {
		t.Fatalf("unexpected H.264 metadata: %+v", stream)
	}
}
