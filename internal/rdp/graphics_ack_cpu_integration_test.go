//go:build integration && linux

package rdp

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// graphicsACKCPU counts scheduled CPU time across every thread in each process.
// Snapshots exclude client startup, encoder warmup and teardown. Server time
// includes conversion, encoding, transport, ACK handling and benchmark overhead.
type graphicsACKCPU struct {
	server, client, xvfb time.Duration
}

func (c *graphicsACKClient) cpuTime(tb testing.TB) graphicsACKCPU {
	tb.Helper()

	return graphicsACKCPU{
		server: graphicsACKClockTime(tb, unix.CLOCK_PROCESS_CPUTIME_ID),
		client: graphicsACKClockTime(tb, graphicsACKProcessClock(c.client.pid)),
		xvfb:   graphicsACKClockTime(tb, graphicsACKProcessClock(c.xvfb.pid)),
	}
}

// Linux make_process_cpuclock(pid, CPUCLOCK_SCHED) encodes the PID this way.
// Leaving CPUCLOCK_PERTHREAD_MASK clear counts every thread in the process.
func graphicsACKProcessClock(pid int) int32 {
	//nolint:gosec // Linux CPU clock IDs intentionally encode a PID in a signed 32-bit bit pattern.
	return int32((^uint32(pid) << 3) | 2)
}

func graphicsACKClockTime(tb testing.TB, clockID int32) time.Duration {
	tb.Helper()
	var value unix.Timespec
	if err := unix.ClockGettime(clockID, &value); err != nil {
		tb.Fatalf("read process CPU clock %d: %v", clockID, err)
	}

	return time.Duration(value.Nano())
}

func (after graphicsACKCPU) report(b *testing.B, before graphicsACKCPU) {
	b.Helper()
	server, client, xvfb := after.server-before.server, after.client-before.client, after.xvfb-before.xvfb
	if server <= 0 || client <= 0 || xvfb < 0 {
		b.Fatalf("invalid CPU deltas: server=%s client=%s Xvfb=%s", server, client, xvfb)
	}
	divisor := float64(b.N) * float64(time.Millisecond)
	b.ReportMetric(float64(server)/divisor, "server_cpu_ms/frame")
	b.ReportMetric(float64(client)/divisor, "client_cpu_ms/frame")
	b.ReportMetric(float64(xvfb)/divisor, "xvfb_cpu_ms/frame")
	b.ReportMetric(float64(server+client+xvfb)/divisor, "total_cpu_ms/frame")
}
