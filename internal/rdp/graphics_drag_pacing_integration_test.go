//go:build integration && linux

package rdp

import (
	"fmt"
	"testing"
	"time"
)

// BenchmarkGraphicsDragPacingFreeRDP compares paced window dragging at 30 and
// 60 FPS with the same two-slice encoder and stable 60-FPS codec clock. It
// measures the loopback FreeRDP decode/repaint path, not the production policy
// that decides when dragging is active or a physical display's presentation.
//
// Each iteration includes encoding, transport, ACK and the remaining frame
// interval. A deadline miss means that the frame's work exceeded its budget.
// Late frames start a new budget without backfilling the missed schedule.
// CPU percentages count all process threads: 100 percent is one full CPU core.
func BenchmarkGraphicsDragPacingFreeRDP(b *testing.B) {
	for _, targetFPS := range []int{30, 60} {
		b.Run(fmt.Sprintf("WindowDrag/slices=2/fps=%d", targetFPS), func(b *testing.B) {
			client := newGraphicsACKClient(b, graphicsACKWidth, graphicsACKHeight)
			fixture := newGraphicsACKFixture(b, "WindowDrag", 2)
			if got := fixture.encoder.FrameRate(); got != 60 {
				b.Fatalf("codec frame rate = %d, want stable 60-FPS clock", got)
			}
			const warmup = 10
			for frame := range warmup {
				measureGraphicsACKFrame(b, client, fixture, frame)
			}
			interval := time.Second / time.Duration(targetFPS)
			measurements := graphicsACKMeasurements{samples: make([]time.Duration, 0, 1024)}
			frame, misses := warmup, 0
			var cpuBefore graphicsACKCPU
			var wallStarted time.Time
			var work time.Duration
			for b.Loop() {
				if frame == warmup {
					cpuBefore = client.cpuTime(b)
					wallStarted = time.Now()
				}
				started := time.Now()
				measurements.add(measureGraphicsACKFrame(b, client, fixture, frame))
				work += time.Since(started)
				if remaining := time.Until(started.Add(interval)); remaining > 0 {
					time.Sleep(remaining)
				} else {
					misses++
				}
				frame++
			}
			elapsed := time.Since(wallStarted)
			cpuAfter := client.cpuTime(b)
			cpuAfter.report(b, cpuBefore)
			measurements.validate(b)
			// Include the last frame's idle remainder, so the measured window
			// contains one complete pacing budget per frame even for short runs.
			b.ReportMetric(float64(targetFPS), "target_fps")
			b.ReportMetric(float64(b.N)/elapsed.Seconds(), "actual_fps")
			b.ReportMetric(float64(misses)/float64(b.N), "deadline_misses/frame")
			b.ReportMetric(float64(work)/float64(b.N)/float64(time.Millisecond), "frame_work_mean_ms")
			for _, process := range []struct {
				name string
				cpu  time.Duration
			}{
				{"server", cpuAfter.server - cpuBefore.server},
				{"client", cpuAfter.client - cpuBefore.client},
				{"xvfb", cpuAfter.xvfb - cpuBefore.xvfb},
				{"total", cpuAfter.server + cpuAfter.client + cpuAfter.xvfb -
					cpuBefore.server - cpuBefore.client - cpuBefore.xvfb},
			} {
				b.ReportMetric(100*float64(process.cpu)/float64(elapsed), process.name+"_cpu_pct")
			}
			mean, p50, p95 := measurements.latencies()
			b.ReportMetric(float64(mean)/float64(time.Millisecond), "ack_mean_ms")
			b.ReportMetric(float64(p50)/float64(time.Millisecond), "ack_p50_ms")
			b.ReportMetric(float64(p95)/float64(time.Millisecond), "ack_p95_ms")
			b.ReportMetric(float64(len(measurements.samples)), "ack_samples")
			b.ReportMetric(float64(measurements.raced)/float64(b.N), "ack_raced/frame")
			b.ReportMetric(float64(measurements.write)/float64(b.N)/float64(time.Millisecond), "write_mean_ms")
			b.ReportMetric(float64(measurements.writeACK)/float64(b.N)/float64(time.Millisecond), "write_ack_mean_ms")
			b.ReportMetric(float64(measurements.bytes)/float64(b.N), "bytes/frame")
			b.ReportMetric(float64(measurements.regions)/float64(b.N), "regions/frame")
		})
	}
}
