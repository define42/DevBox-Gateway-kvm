package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"io"
	"net"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
)

func TestPerformanceConfigurationAndReadiness(t *testing.T) {
	t.Parallel()
	args := []string{"-mode", "h264", "-state-dir", t.TempDir(), "-output", t.TempDir()}
	config, err := parseConfiguration(args)
	if err != nil || config.workload != "smoke" || fixtureWorkload(config.workload).fps != 2 {
		t.Fatalf("default smoke changed: %+v %v", config, err)
	}
	config, err = parseConfiguration(append(args, "-workload", "performance"))
	if err != nil {
		t.Fatal(err)
	}
	if err := publishReadiness(config, []byte("fixture certificate"), 12345); err != nil {
		t.Fatal(err)
	}
	ready := awaitReadiness(t, config.stateDir)
	if ready.Workload != "performance" || ready.Width != 1920 || ready.Height != 1080 || ready.TargetFPS != 30 {
		t.Fatalf("incorrect performance contract: %+v", ready)
	}
	for _, bad := range []string{"", "PERFORMANCE", "benchmark", "smoke\n"} {
		if _, err := parseConfiguration(append(args, "-workload", bad)); !errors.Is(err, errConfiguration) {
			t.Fatalf("accepted workload %q", bad)
		}
	}
}

func TestPerformanceMarkerAndScrolling(t *testing.T) {
	t.Parallel()
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	paintPerformanceFrame(frame, 7, 0x123456)
	// An independent golden packet prevents matching renderer/decoder mistakes.
	// The final D2 is XOR(56,34,12,07,A5), not calculated by the production code.
	// Client.cs tests the same golden bytes using its independent decoder.
	want := [5]byte{0x56, 0x34, 0x12, 0x07, 0xd2}
	for bit := range 40 {
		for row := range 2 {
			shade := ((want[bit/8] >> (bit % 8) & 1) ^ byte(row)) * 255
			for _, offset := range []image.Point{{X: 5, Y: 5}, {X: 16, Y: 16}, {X: 26, Y: 26}} {
				pixel := frame.RGBAAt(64+bit*32+offset.X, 16+row*40+offset.Y)
				if pixel != (color.RGBA{R: shade, G: shade, B: shade, A: 255}) {
					t.Fatalf("marker bit=%d row=%d offset=%v: %v", bit, row, offset, pixel)
				}
			}
		}
	}
	paintPerformanceFrame(frame, 0, 0)
	first := frame.RGBAAt(100, 154)
	paintPerformanceFrame(frame, 0, 1)
	if first == frame.RGBAAt(100, 154) {
		t.Fatal("document content did not scroll outside the marker")
	}
	stable := bytes.Clone(frame.Pix)
	paintPerformanceFrame(frame, 0, 1)
	if !bytes.Equal(stable, frame.Pix) {
		t.Fatal("workload is not deterministic")
	}
}

func TestPerformanceEncoderSettings(t *testing.T) {
	t.Parallel()
	writer, err := newWorkloadFrameWriter("h264", &rdp.Session{BitsPerPixel: 32}, fixtureWorkload("performance"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.close()
	if writer.graphics == nil || writer.graphics.Threads() != 4 || writer.bitmap != nil {
		t.Fatal("performance must use the four-slice software H.264 encoder")
	}
	if !writer.graphics.MeasurementsEnabled() {
		t.Fatal("performance must enable separate conversion and encoding measurements")
	}
	smoke, err := newFrameWriter("h264", &rdp.Session{BitsPerPixel: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer smoke.close()
	if smoke.graphics.MeasurementsEnabled() {
		t.Fatal("smoke must preserve disabled encoder measurements")
	}
	// Encoding a complete workload image catches geometry and encoder setup
	// mismatches before the Windows client is involved.
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	paintPerformanceFrame(frame, 0, 0)
	packet, err := writer.graphics.Encode(frame, true)
	if err != nil || len(packet) == 0 {
		t.Fatalf("1080p frame encode failed: bytes=%d, error=%v", len(packet), err)
	}
	stats := writer.graphics.LastStats()
	// A fast phase can finish within one Windows clock tick. Configuration,
	// rather than a positive elapsed time, establishes whether timing is enabled.
	if stats.Conversion < 0 || stats.Encoding < 0 {
		t.Fatalf("negative encoder measurements: %+v", stats)
	}
}

func TestPerformanceTimingStatistics(t *testing.T) {
	t.Parallel()
	var samples timingSamples
	if samples.report() != nil {
		t.Fatal("unavailable timings must be null")
	}
	var zero timingSamples
	zero.add(0)
	if got := zero.report(); got == nil || *got != (timingReport{Count: 1, Samples: 1}) {
		t.Fatalf("zero-duration sample must remain available: %+v", got)
	}
	for _, duration := range []time.Duration{
		20 * time.Millisecond, 5 * time.Millisecond, 15 * time.Millisecond, 10 * time.Millisecond,
	} {
		samples.add(duration)
	}
	got := samples.report()
	if got.Count != 4 || got.Samples != 4 || got.Truncated || got.Average != 12.5 ||
		got.P50 != 10 || got.P95 != 20 || got.Maximum != 20 {
		t.Fatalf("incorrect nearest-rank timing summary: %+v", got)
	}
	if samples.values[0] != 20 {
		t.Fatal("summarizing altered observation order")
	}
	var bounded timingSamples
	for range maximumTimingSamples {
		bounded.add(time.Millisecond)
	}
	bounded.add(5 * time.Millisecond)
	got = bounded.report()
	if !got.Truncated || got.Count != maximumTimingSamples+1 || got.Samples != maximumTimingSamples ||
		got.Maximum != 5 || got.P95 != 1 || got.Average <= 1 {
		t.Fatalf("bounded statistics lost truncation or full-stream totals: %+v", got)
	}
}

type partialConnection struct{ net.Conn }

func (*partialConnection) Read(data []byte) (int, error)  { return copy(data, "abc"), io.EOF }
func (*partialConnection) Write(data []byte) (int, error) { return min(len(data), 2), io.ErrClosedPipe }

func TestPerformanceTransportCountsActualBytes(t *testing.T) {
	t.Parallel()
	conn := &countedConnection{Conn: &partialConnection{}}
	buffer := make([]byte, 16)
	if n, err := conn.Read(buffer); n != 3 || !errors.Is(err, io.EOF) || string(buffer[:3]) != "abc" {
		t.Fatal("counter changed read behavior")
	}
	if n, err := conn.Write(buffer); n != 2 || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("counter changed partial write behavior")
	}
	if conn.received.Load() != 3 || conn.sent.Load() != 2 {
		t.Fatal("transport totals must count transferred bytes, including partial operations")
	}
	if byteRateMbps(125000, 1) != 1 || byteRateMbps(125000, 2) != .5 || byteRateMbps(125000, 0) != 0 {
		t.Fatal("transport rate uses the wrong units or measurement duration")
	}
}

func TestPerformanceSnapshotPreservesMeasurementScope(t *testing.T) {
	t.Parallel()
	measurements := &performanceMeasurements{skippedTicks: 3}
	measurements.frame.add(8 * time.Millisecond)
	measurements.bitmapPacking.add(5 * time.Millisecond)
	measurements.write.add(2 * time.Millisecond)
	result := report{
		FramesSent: 60, SessionSeconds: 4,
		Performance: &performanceReport{Scope: "session_including_warmup", SentBytes: 1234, TransportSeconds: 5},
	}
	measurements.finish(&result)
	got := result.Performance
	if got.DurationSeconds != 4 || got.FramesPerSecond != 15 || got.FrameMS.Average != 8 ||
		got.BitmapPackingMS.Average != 5 || got.WriteMS.Average != 2 || got.EncodeMS != nil ||
		got.ConvertMS != nil || got.SentBytes != 1234 || got.TransportSeconds != 5 || got.SkippedTicks != 3 {
		t.Fatalf("incorrect bitmap metrics or mixed measurement windows: %+v", got)
	}
}
