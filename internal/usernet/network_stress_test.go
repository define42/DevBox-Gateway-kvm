//go:build integration

package usernet

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
)

// These opt-in tests exercise real forwarding and local sockets for a sustained
// interval. They deliberately do not run in parallel with throughput benchmarks.
//
//nolint:paralleltest // Measures sustained load and process resources without concurrent test fixtures.
func TestNetworkMalformedFloodRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("sustained network integration test")
	}
	duration := networkStressDuration(t)
	n := testNetwork(t)
	guest := guestStack(t, n)
	address := measurementTCPServer(t, func(conn net.Conn) { _, _ = io.Copy(conn, conn) })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	conn, err := gonet.DialContextTCP(ctx, guest, address, ipv4.ProtocolNumber)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	measurementEcho(t, conn)

	// Invalid sizes, truncated IPv4 headers, impossible IPv4 total length and
	// invalid ARP headers exercise both the NIC drop path and netstack parsing.
	frames := [][]byte{make([]byte, 13), make([]byte, MTU+15), make([]byte, 14), make([]byte, 64), make([]byte, 64)}
	frames[2][12], frames[2][13] = 8, 0
	frames[3][12], frames[3][13], frames[3][14] = 8, 0, 0x45
	frames[3][16], frames[3][17] = 0xff, 0xff
	frames[4][12], frames[4][13] = 8, 6
	var attempts atomic.Uint64
	floodCtx, stop := context.WithTimeout(t.Context(), duration)
	defer stop()
	floodDone := make(chan struct{})
	go func() {
		defer close(floodDone)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-floodCtx.Done():
				return
			case <-ticker.C:
				for i := 0; i < 128; i++ {
					_, _ = n.Write(frames[i%len(frames)])
					attempts.Add(1)
				}
			}
		}
	}()
	started := time.Now()
	usage := measureNetworkStressUsage()
	probes := 0
	probeTicker := time.NewTicker(50 * time.Millisecond)
	defer probeTicker.Stop()
	for running := true; running; {
		select {
		case <-floodDone:
			running = false
		case <-probeTicker.C:
			measurementEcho(t, conn)
			probes++
			usage.include(measureNetworkStressUsage())
		}
	}
	measurementEcho(t, conn)
	if probes == 0 || attempts.Load() == 0 {
		t.Fatal("stress traffic did not run")
	}
	t.Logf("duration=%s malformed_frames=%d successful_probes=%d peak_heap_bytes=%d peak_goroutines=%d peak_fds=%d",
		time.Since(started).Round(time.Millisecond), attempts.Load(), probes, usage.heap, usage.goroutines, usage.fds)
}

//nolint:paralleltest // Measures saturation and process resources without concurrent test fixtures.
func TestNetworkConnectionSaturationRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("sustained network integration test")
	}
	duration := networkStressDuration(t)
	n := testNetwork(t)
	guest := guestStack(t, n)
	address := measurementTCPServer(t, func(conn net.Conn) { _, _ = io.Copy(conn, conn) })
	clients := make([]*gonet.TCPConn, 0, maxConnections)
	defer func() {
		for _, conn := range clients {
			_ = conn.Close()
		}
	}()
	for i := 0; i < maxConnections; i++ {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		conn, err := gonet.DialContextTCP(ctx, guest, address, ipv4.ProtocolNumber)
		cancel()
		if err != nil {
			t.Fatalf("fill connection slot %d: %v", i, err)
		}
		clients = append(clients, conn)
	}
	started := time.Now()
	usage := measureNetworkStressUsage()
	nextSample := started
	deadline := started.Add(duration)
	rejected := 0
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		conn, err := gonet.DialContextTCP(ctx, guest, address, ipv4.ProtocolNumber)
		cancel()
		if err == nil {
			_ = conn.Close()
			t.Fatal("forwarder admitted a connection above its configured capacity")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("saturated connection timed out instead of being rejected")
		}
		rejected++
		if time.Now().After(nextSample) {
			usage.include(measureNetworkStressUsage())
			nextSample = time.Now().Add(100 * time.Millisecond)
		}
		if rejected%32 == 0 {
			measurementEcho(t, clients[0])
			runtime.Gosched()
		}
	}
	for _, conn := range clients {
		_ = conn.Close()
	}
	clients = nil
	// A visible successful connection after the holders close proves capacity
	// recovery without asserting internal scheduling or implementation counters.
	var recovered *gonet.TCPConn
	recoveryDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(recoveryDeadline) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		conn, err := gonet.DialContextTCP(ctx, guest, address, ipv4.ProtocolNumber)
		cancel()
		if err == nil {
			recovered = conn

			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if recovered == nil {
		t.Fatal("forwarding capacity did not recover after closing saturated clients")
	}
	defer recovered.Close()
	measurementEcho(t, recovered)

	closed := make(chan error, 1)
	closeStart := time.Now()
	go func() { closed <- n.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("network cancellation stalled with an active forwarded connection")
	}
	if _, err := n.Write(make([]byte, 64)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Write after cancellation: %v", err)
	}
	t.Logf("duration=%s held_connections=%d rejected_attempts=%d close_latency=%s "+
		"peak_heap_bytes=%d peak_goroutines=%d peak_fds=%d",
		time.Since(started).Round(time.Millisecond), maxConnections, rejected, time.Since(closeStart).Round(time.Microsecond),
		usage.heap, usage.goroutines, usage.fds)
}

func networkStressDuration(t *testing.T) time.Duration {
	t.Helper()
	value := os.Getenv("DEVBOX_USERNET_STRESS_DURATION")
	if value == "" {
		return 10 * time.Second
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < time.Second || duration > 5*time.Minute {
		t.Fatal("DEVBOX_USERNET_STRESS_DURATION must be between 1s and 5m")
	}

	return duration
}

// Usage covers the entire test process, including the synthetic guest and local
// server, and is diagnostic rather than a host-independent acceptance threshold.
type networkStressUsage struct {
	heap       uint64
	goroutines int
	fds        int
}

func measureNetworkStressUsage() networkStressUsage {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	fds, _ := os.ReadDir("/proc/self/fd")

	return networkStressUsage{heap: memory.HeapAlloc, goroutines: runtime.NumGoroutine(), fds: len(fds)}
}

func (peak *networkStressUsage) include(sample networkStressUsage) {
	peak.heap = max(peak.heap, sample.heap)
	peak.goroutines = max(peak.goroutines, sample.goroutines)
	peak.fds = max(peak.fds, sample.fds)
}
