package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/sys/unix"
)

func TestExporterIdleHeartbeatAndDroppedSnapshot(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		collector := New()
		packets := make(chan []byte, 4)
		done := make(chan struct{})
		go func() {
			defer close(done)
			collector.run(ctx, func(data []byte) { packets <- data })
		}()
		first := decodeSnapshot(t, <-packets)
		if first.Sequence != 1 || first.Connections != 0 || first.ElapsedSeconds != 0 {
			t.Fatalf("initial idle snapshot: %+v", first)
		}
		connection := collector.Connect()
		connection.Sent(100)
		connection.Close()
		time.Sleep(Interval)
		synctest.Wait()
		// Discard one datagram as if the receiver queue was full. Cumulative
		// counters must survive independently of exporter delivery success.
		<-packets
		time.Sleep(Interval)
		synctest.Wait()
		third := decodeSnapshot(t, <-packets)
		if third.Sequence != 3 || third.ElapsedSeconds != (2*Interval).Seconds() ||
			third.FramesSent != 1 || third.PayloadBytes != 100 || third.Connections != 0 {
			t.Fatalf("idle heartbeat lost counters after drop: %+v", third)
		}
		cancel()
		<-done
	})
}

func TestExporterDatagramAndShutdown(t *testing.T) {
	t.Parallel()
	peer, child := datagramPair(t)
	collector, stop, err := Start(t.Context(), child)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	ready := []unix.PollFd{{Fd: int32(peer), Events: unix.POLLIN}}
	if count, err := unix.Poll(ready, 2000); err != nil || count != 1 {
		t.Fatalf("initial datagram not available: count=%d err=%v", count, err)
	}
	data := make([]byte, MaxMessageSize+1)
	count, _, err := unix.Recvfrom(peer, data, unix.MSG_DONTWAIT)
	if err != nil {
		t.Fatal(err)
	}
	decodeSnapshot(t, data[:count])
	stop()
	stop()
	// The collector's lifetime and counters do not depend on IPC availability.
	connection := collector.Connect()
	connection.Sent(123)
	connection.Close()
	if collector.Snapshot().PayloadBytes != 123 {
		t.Fatal("stopped exporter disabled process counters")
	}
}

func TestExporterFullSocketDoesNotBlockShutdown(t *testing.T) {
	t.Parallel()
	_, child := datagramPair(t)
	if err := unix.SetsockoptInt(child, unix.SOL_SOCKET, unix.SO_SNDBUF, MaxMessageSize); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, MaxMessageSize)
	full := false
	for range 1024 {
		err := unix.Send(child, data, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
		if errors.Is(err, unix.EAGAIN) {
			full = true

			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !full {
		t.Fatal("could not fill bounded datagram queue")
	}
	collector, stop, err := Start(t.Context(), child)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	deadline := time.After(2 * time.Second)
	for collector.sequence.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("exporter did not start a snapshot")
		default:
			runtime.Gosched()
		}
	}
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("full telemetry socket blocked shutdown")
	}
}

func TestExporterDisconnectedSocket(t *testing.T) {
	t.Parallel()
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, stop, err := Start(t.Context(), pair[1])
	if err != nil {
		_ = unix.Close(pair[0])
		t.Fatal(err)
	}
	defer stop()
	if err := unix.Close(pair[0]); err != nil {
		t.Fatal(err)
	}
	stop()
}

func TestStartRejectsMalformedDescriptors(t *testing.T) {
	t.Parallel()
	for _, fd := range []int{-1, 0, 1, 2} {
		t.Run(strconv.Itoa(fd), func(t *testing.T) {
			t.Parallel()
			collector, stop, err := Start(t.Context(), fd)
			if !errors.Is(err, ErrDescriptor) || collector != nil || stop != nil {
				t.Fatalf("reserved fd accepted: collector=%v stop=%v err=%v", collector, stop != nil, err)
			}
		})
	}
	t.Run("stream socket", func(t *testing.T) {
		t.Parallel()
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		_, _, err = Start(t.Context(), pair[1])
		if !errors.Is(err, ErrDescriptor) {
			t.Fatalf("stream descriptor accepted: %v", err)
		}
	})
	t.Run("unconnected datagram", func(t *testing.T) {
		t.Parallel()
		fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = Start(t.Context(), fd)
		if !errors.Is(err, ErrDescriptor) {
			t.Fatalf("unconnected descriptor accepted: %v", err)
		}
	})
	t.Run("regular descriptor", func(t *testing.T) {
		t.Parallel()
		fd, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = Start(t.Context(), fd)
		if !errors.Is(err, ErrDescriptor) {
			t.Fatalf("non-socket descriptor accepted: %v", err)
		}
	})
}

func TestResidentBytes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		data string
		page uint64
		want uint64
		ok   bool
	}{
		{name: "resident not virtual", data: "1000 2 3 4", page: 4096, want: 8192, ok: true},
		{name: "zero resident", data: "1000 0", page: 4096, ok: true},
		{name: "empty", page: 4096},
		{name: "short", data: "1000", page: 4096},
		{name: "negative", data: "1000 -1", page: 4096},
		{name: "invalid", data: "1000 nope", page: 4096},
		{name: "overflow", data: "1000 18446744073709551615", page: 4096},
		{name: "invalid page size", data: "1000 2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := residentBytes([]byte(test.data), test.page)
			if (got != nil) != test.ok || got != nil && *got != test.want {
				t.Fatalf("residentBytes = %v, want %d (valid=%v)", got, test.want, test.ok)
			}
		})
	}
}

func datagramPair(t *testing.T) (int, int) {
	t.Helper()
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(pair[0]) })

	return pair[0], pair[1]
}

func decodeSnapshot(t *testing.T, data []byte) Snapshot {
	t.Helper()
	if len(data) > MaxMessageSize {
		t.Fatalf("oversized telemetry payload: %d", len(data))
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("invalid exported snapshot: %v", err)
	}

	return snapshot
}
