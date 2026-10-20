package avc

import (
	"bytes"
	"fmt"
	"image/color"
	"os/exec"
	"regexp"
	"strconv"
	"testing"
)

func TestQPBitstreamQuantizer(t *testing.T) {
	t.Parallel()
	for _, qp := range []int{MinQP, DefaultQP, MaxQP} {
		t.Run(fmt.Sprintf("QP%d", qp), func(t *testing.T) {
			t.Parallel()
			encoder, err := NewEncoderWithOptions(128, 96, Options{Threads: 1, QP: &qp})
			if err != nil {
				t.Fatal(err)
			}
			defer encoder.Close()
			checkEncodedQP(t, qpTestStream(t, encoder), qp)
		})
	}
}

func qpTestStream(t *testing.T, encoder *Encoder) []byte {
	t.Helper()
	img := qpTestImage(128, 96)
	var stream []byte
	for frame := range 3 {
		img.SetRGBA(2, 2, color.RGBA{R: byte(frame * 70), G: 90, A: 255})
		packet, err := encoder.Encode(img, frame == 2)
		if err != nil || len(packet) == 0 {
			t.Fatalf("encode frame %d: bytes=%d, error=%v", frame, len(packet), err)
		}
		stream = append(stream, packet...)
	}

	return stream
}

// An independent parser verifies actual PPS + slice QP, including the 0/51
// endpoints; inspecting requested codec parameters alone could miss clipping.
func checkEncodedQP(t *testing.T, stream []byte, want int) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	//nolint:gosec // The executable is resolved from PATH; arguments are fixed.
	command := exec.CommandContext(t.Context(), ffmpeg,
		"-hide_banner", "-loglevel", "verbose", "-f", "h264", "-i", "pipe:0",
		"-map", "0:v:0", "-c:v", "copy", "-bsf:v", "trace_headers", "-f", "null", "-")
	command.Stdin = bytes.NewReader(stream)
	var trace bytes.Buffer
	command.Stderr = &trace
	if err := command.Run(); err != nil {
		t.Fatalf("trace H.264 headers: %v: %s", err, trace.Bytes())
	}
	fields := regexp.MustCompile(`(pic_init_qp_minus26|slice_qp_delta)\s+\S+\s+=\s+(-?\d+)`)
	baseQP, slices := -1, 0
	for _, match := range fields.FindAllStringSubmatch(trace.String(), -1) {
		value, err := strconv.Atoi(match[2])
		if err != nil {
			t.Fatal(err)
		}
		if match[1] == "pic_init_qp_minus26" {
			baseQP = 26 + value
		} else {
			slices++
			if baseQP < 0 || baseQP+value != want {
				t.Fatalf("slice QP=%d+%d, want %d", baseQP, value, want)
			}
		}
	}
	if slices < 3 {
		t.Fatalf("traced only %d slices for three pictures: %s", slices, trace.Bytes())
	}
}
