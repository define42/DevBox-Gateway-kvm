package main

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"net"
	"slices"
	"sync/atomic"
	"time"
)

// These fixed presets avoid accidentally turning the isolated fixture into an
// arbitrary-size workload generator. An omitted workload retains the smoke
// geometry, including callers that construct configuration directly in tests.
type workloadSpec struct {
	name               string
	width, height, fps int
	slices             int
}

func fixtureWorkload(name string) workloadSpec {
	if name == "performance" {
		return workloadSpec{name: name, width: 1920, height: 1080, fps: 30, slices: 4}
	}

	return workloadSpec{name: "smoke", width: fixtureWidth, height: fixtureHeight, fps: 2, slices: 1}
}

// countedConnection observes bytes at the raw TCP connection, so totals include
// TLS records and all RDP channels but exclude TCP/IP headers. The reader and
// encoder run concurrently; no packet contents or key values are retained.
type countedConnection struct {
	net.Conn
	sent, received atomic.Uint64
}

func (conn *countedConnection) Read(data []byte) (int, error) {
	n, err := conn.Conn.Read(data)
	if n > 0 {
		conn.received.Add(uint64(n))
	}

	return n, err
}

func (conn *countedConnection) Write(data []byte) (int, error) {
	n, err := conn.Conn.Write(data)
	if n > 0 {
		conn.sent.Add(uint64(n))
	}

	return n, err
}

type performanceReport struct {
	Scope            string        `json:"scope"`
	DurationSeconds  float64       `json:"durationSeconds"`
	FramesPerSecond  float64       `json:"framesPerSecond"`
	FrameMS          *timingReport `json:"frameMs"`
	EncodeMS         *timingReport `json:"encodeMs"`
	ConvertMS        *timingReport `json:"convertMs"`
	BitmapPackingMS  *timingReport `json:"bitmapPackingMs"`
	WriteMS          *timingReport `json:"writeMs"`
	SkippedTicks     uint64        `json:"skippedTicks"`
	TransportSeconds float64       `json:"transportSeconds"`
	SentBytes        uint64        `json:"sentBytes"`
	ReceivedBytes    uint64        `json:"receivedBytes"`
	SentMbps         float64       `json:"sentMbps"`
	ReceivedMbps     float64       `json:"receivedMbps"`
}

// Times are wall-clock service times, not CPU time or client display latency.
// frameMs includes painting, encoding/packing and writes for each attempted
// frame; encodeMs and convertMs use the encoder's separate codec/conversion
// measurements and are null for bitmap. Bitmap writeMs sums the writes for one
// frame, excluding packing; bitmapPackingMs records the rest of WriteFrame.
// The final failed attempt can contribute timings.
type timingReport struct {
	Count     uint64  `json:"count"`
	Samples   int     `json:"samples"`
	Truncated bool    `json:"truncated"`
	Average   float64 `json:"average"`
	P50       float64 `json:"p50"`
	P95       float64 `json:"p95"`
	Maximum   float64 `json:"maximum"`
}

// A 10-minute maximum session at 30 FPS has at most 18,000 normal ticks.
// Retain a bounded number of timings even if this invariant changes later;
// count/average/maximum still cover all observations and truncation is explicit.
const maximumTimingSamples = 20000

type timingSamples struct {
	values []float64
	count  uint64
	total  float64
	max    float64
}

func (samples *timingSamples) add(duration time.Duration) {
	value := float64(duration) / float64(time.Millisecond)
	samples.count++
	samples.total += value
	samples.max = max(samples.max, value)
	if len(samples.values) < maximumTimingSamples {
		samples.values = append(samples.values, value)
	}
}

func (samples *timingSamples) report() *timingReport {
	if samples.count == 0 {
		return nil
	}
	values := slices.Clone(samples.values)
	slices.Sort(values)
	percentile := func(fraction float64) float64 {
		return values[max(0, int(math.Ceil(float64(len(values))*fraction))-1)]
	}

	return &timingReport{
		Count: samples.count, Samples: len(values), Truncated: samples.count > uint64(len(values)),
		Average: samples.total / float64(samples.count), Maximum: samples.max,
		P50: percentile(0.50), P95: percentile(0.95),
	}
}

type performanceMeasurements struct {
	frame, encode, convert, write, bitmapPacking timingSamples
	skippedTicks                                 uint64
}

func (measurements *performanceMeasurements) finish(result *report) {
	if result.Performance == nil {
		result.Performance = &performanceReport{Scope: "session_including_warmup"}
	}
	performance := result.Performance
	performance.DurationSeconds = result.SessionSeconds
	if result.SessionSeconds > 0 {
		performance.FramesPerSecond = float64(result.FramesSent) / result.SessionSeconds
	}
	performance.FrameMS = measurements.frame.report()
	performance.EncodeMS = measurements.encode.report()
	performance.ConvertMS = measurements.convert.report()
	performance.BitmapPackingMS = measurements.bitmapPacking.report()
	performance.WriteMS = measurements.write.report()
	performance.SkippedTicks = measurements.skippedTicks
}

func byteRateMbps(count uint64, seconds float64) float64 {
	if seconds <= 0 {
		return 0
	}

	return float64(count) * 8 / seconds / 1e6
}

// The workload scrolls deterministic document rows and moves a window over
// them. A sequence/input marker remains above the content. Client.cs samples
// its interiors, including when the 1080p session is scaled to a smaller
// physical runner display. No fonts, external assets or guest OS are needed.
func paintPerformanceFrame(frame *image.RGBA, input, sequence uint64) {
	fill := func(rect image.Rectangle, red, green, blue uint8) {
		draw.Draw(frame, rect.Intersect(frame.Rect), image.NewUniform(color.RGBA{R: red, G: green, B: blue, A: 255}),
			image.Point{}, draw.Src)
	}
	fill(frame.Rect, 232, 236, 242)
	// The header provides a stable high-contrast surround for the marker.
	fill(image.Rect(0, 0, 1920, 128), 80, 88, 104)
	body := image.Rect(0, 128, 1920, 1080)
	offset := int(sequence * 4 % 48)
	for row := -1; row < 22; row++ {
		top := 128 + row*48 - offset
		if row&1 == 0 {
			fill(image.Rect(0, top, 1920, top+48).Intersect(body), 248, 249, 251)
		}
		for column := range 5 {
			left := 64 + column*360
			width := 120 + ((row+1)*37+column*53)%180
			fill(image.Rect(left, top+18, left+width, top+28).Intersect(body), 48, 58, 74)
		}
	}
	left := 96 + int(sequence*7%1040)
	top := 320 + int(sequence*3%240)
	fill(image.Rect(left+8, top+8, left+648, top+368), 112, 120, 136)
	fill(image.Rect(left, top, left+640, top+360), 250, 252, 255)
	fill(image.Rect(left, top, left+640, top+40), 44, 96, 170)
	for row := range 8 {
		fill(image.Rect(left+24, top+64+row*32, left+480-row%3*64, top+74+row*32), 56, 68, 84)
	}
	paintPerformanceMarker(frame, input, sequence)
}

func paintPerformanceMarker(frame *image.RGBA, input, sequence uint64) {
	// 24-bit sequence, 8-bit input epoch, then XOR checksum; bytes and bits
	// are least-significant first. The second row is the exact complement.
	packet := [5]byte{byte(sequence), byte(sequence >> 8), byte(sequence >> 16), byte(input)}
	packet[4] = packet[0] ^ packet[1] ^ packet[2] ^ packet[3] ^ 0xa5
	for bit := range 40 {
		value := packet[bit/8] >> (bit % 8) & 1
		for row := range 2 {
			shade := (value ^ uint8(row)) * 255
			left, top := 64+bit*32, 16+row*40
			draw.Draw(frame, image.Rect(left, top, left+32, top+32),
				image.NewUniform(color.RGBA{R: shade, G: shade, B: shade, A: 255}), image.Point{}, draw.Src)
		}
	}
}
