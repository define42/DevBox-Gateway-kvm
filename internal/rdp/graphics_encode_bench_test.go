//go:build integration && linux

package rdp

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// BenchmarkGraphicsEncodeDamage isolates conversion and encoding for the same
// pictures as the real-client ACK benchmark, excluding client and network work.
func BenchmarkGraphicsEncodeDamage(b *testing.B) {
	for _, workload := range []string{"Scroll", "WindowDrag"} {
		for _, slices := range []int{1, 2, 4} {
			b.Run(fmt.Sprintf("%s/slices=%d", workload, slices), func(b *testing.B) {
				fixture := newGraphicsACKFixture(b, workload, slices)
				const warmup = 10
				for frame := range warmup {
					fixture.encode(b, frame)
				}
				frame := warmup
				var cpuBefore time.Duration
				var bytes int64
				b.ReportAllocs()
				for b.Loop() {
					if frame == warmup {
						cpuBefore = graphicsACKClockTime(b, unix.CLOCK_PROCESS_CPUTIME_ID)
					}
					bytes += int64(len(fixture.encode(b, frame).data))
					frame++
				}
				cpuAfter := graphicsACKClockTime(b, unix.CLOCK_PROCESS_CPUTIME_ID)
				b.ReportMetric(float64(cpuAfter-cpuBefore)/float64(b.N)/float64(time.Millisecond), "server_cpu_ms/frame")
				b.ReportMetric(float64(bytes)/float64(b.N), "bytes/frame")
			})
		}
	}
}
