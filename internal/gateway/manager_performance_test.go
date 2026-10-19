package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
	"golang.org/x/sys/unix"
)

func performanceTestSample() telemetry.Snapshot {
	cpu, resident := 1.0, uint64(64<<20)

	return telemetry.Snapshot{
		Version: telemetry.Version, Sequence: 1, ElapsedSeconds: 1,
		CPUSeconds: &cpu, ResidentBytes: &resident, Connections: 1, Encoder: "software",
		FramesSent: 10, PayloadBytes: 100, EncodeNanos: 20_000_000, EncodeCount: 10,
		WriteNanos: 10_000_000, WriteCount: 10, ACKNanos: 5_000_000,
	}
}

func performanceTestJSON(t *testing.T, sample telemetry.Snapshot) []byte {
	t.Helper()
	data, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func TestProcessPerformanceSamplingAndPayload(t *testing.T) {
	t.Parallel()
	p := &processPerformance{}
	now := time.Now()
	first := performanceTestSample()
	if !p.decode(performanceTestJSON(t, first), now) {
		t.Fatal("first sample rejected")
	}
	initial := p.snapshot(now)
	if initial.SampleIntervalSeconds != nil || initial.PayloadBytesPerSecond != nil || initial.PayloadBytes != 100 {
		t.Fatalf("first sample invented an interval or payload rate: %+v", initial)
	}
	second := first
	second.Sequence, second.ElapsedSeconds, second.PayloadBytes = 4, 11, 1100
	// A delayed/lost datagram must not make receipt time or the nominal five-second
	// export period the denominator. The child's measured interval is ten seconds.
	if !p.decode(performanceTestJSON(t, second), now.Add(20*time.Second)) {
		t.Fatal("advancing sample rejected")
	}
	got := p.snapshot(now.Add(20 * time.Second))
	if got.SampleIntervalSeconds == nil || *got.SampleIntervalSeconds != 10 {
		t.Fatalf("sample interval = %v, want 10", got.SampleIntervalSeconds)
	}
	if got.PayloadBytes != 1100 || got.PayloadBytesPerSecond == nil || *got.PayloadBytesPerSecond != 100 {
		t.Fatalf("payload counters/rate = %+v, want 1100 bytes and 100 bytes/second", got)
	}
	*got.SampleIntervalSeconds, *got.PayloadBytesPerSecond = 0, 0
	again := p.snapshot(now.Add(20 * time.Second))
	if *again.SampleIntervalSeconds != 10 || *again.PayloadBytesPerSecond != 100 {
		t.Fatal("returned rate pointers alias retained data")
	}
	idle := second
	idle.Sequence, idle.ElapsedSeconds = 5, 16
	idle.Connections, idle.Encoder, idle.ACKNanos = 0, "", 0
	if !p.decode(performanceTestJSON(t, idle), now.Add(25*time.Second)) {
		t.Fatal("idle sample rejected")
	}
	got = p.snapshot(now.Add(41 * time.Second))
	if got.State != "stale" || got.PayloadBytes != 1100 ||
		got.PayloadBytesPerSecond == nil || *got.PayloadBytesPerSecond != 0 {
		t.Fatalf("idle/stale sample lost payload history or invented traffic: %+v", got)
	}
	if got.SampleIntervalSeconds == nil || *got.SampleIntervalSeconds != 5 {
		t.Fatalf("idle sample interval = %v, want 5", got.SampleIntervalSeconds)
	}
}

func TestProcessPerformanceDeltasAndReconnect(t *testing.T) {
	t.Parallel()
	p := &processPerformance{}
	now := time.Now()
	first := performanceTestSample()
	if !p.decode(performanceTestJSON(t, first), now) {
		t.Fatal("first sample rejected")
	}
	initial := p.snapshot(now)
	if initial.CPUPercent != nil || initial.FramesPerSecond != nil || initial.EncodeMs != nil ||
		initial.WriteMs != nil || initial.ResidentBytes == nil || initial.FramesSent != 10 {
		t.Fatalf("first sample must not invent rate baselines: %+v", initial)
	}
	second := first
	cpu := 11.0
	second.Sequence, second.ElapsedSeconds, second.CPUSeconds = 3, 6, &cpu // A lost datagram is harmless.
	second.FramesSent, second.PayloadBytes = 20, 300
	second.EncodeNanos, second.EncodeCount = 50_000_000, 20
	second.WriteNanos, second.WriteCount = 30_000_000, 20
	if !p.decode(performanceTestJSON(t, second), now.Add(5*time.Second)) {
		t.Fatal("valid advancing sample rejected")
	}
	got := p.snapshot(now.Add(5 * time.Second))
	for name, value := range map[string]struct {
		got  *float64
		want float64
	}{
		"cpu": {got.CPUPercent, 200}, "fps": {got.FramesPerSecond, 2},
		"encode": {got.EncodeMs, 3}, "write": {got.WriteMs, 2}, "ack": {got.ACKMs, 5},
	} {
		if value.got == nil || *value.got != value.want {
			t.Errorf("%s = %v, want %v", name, value.got, value.want)
		}
	}
	*got.ResidentBytes = 0
	if *p.snapshot(now).ResidentBytes == 0 {
		t.Fatal("returned resource pointer aliases retained data")
	}
	idle := second
	idle.Sequence, idle.ElapsedSeconds = 4, 11
	idle.Connections, idle.Encoder, idle.ACKNanos = 0, "", 0
	if !p.decode(performanceTestJSON(t, idle), now.Add(10*time.Second)) {
		t.Fatal("idle sample rejected")
	}
	got = p.snapshot(now.Add(10 * time.Second))
	if got.Encoder != "" || got.EncodeMs != nil || got.WriteMs != nil || got.ACKMs != nil || got.FramesSent != 20 {
		t.Fatalf("disconnected gauges were not cleared: %+v", got)
	}
	reconnected := idle
	reconnected.Sequence, reconnected.ElapsedSeconds = 5, 16
	reconnected.Connections, reconnected.Encoder, reconnected.FramesSent = 1, "nvenc", 25
	if !p.decode(performanceTestJSON(t, reconnected), now.Add(15*time.Second)) {
		t.Fatal("reconnected sample rejected")
	}
	got = p.snapshot(now.Add(15 * time.Second))
	if got.Encoder != "nvenc" || got.FramesPerSecond == nil || *got.FramesPerSecond != 1 || got.ACKMs != nil {
		t.Fatalf("reconnect used incorrect counters or stale ACK: %+v", got)
	}
	if got := p.snapshot(now.Add(31 * time.Second)); got.State != "stale" || got.SampledAt == nil {
		t.Fatalf("sample did not become stale: %+v", got)
	}
}

func TestProcessPerformanceRejectsMalformedSamples(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		change  func(*telemetry.Snapshot)
		data    string
		arrival time.Duration
	}{
		{name: "unknown field", data: `{"version":1,"sequence":2,"elapsedSeconds":6,"owner":"bob"}`},
		{name: "trailing object", data: `{"version":1,"sequence":2,"elapsedSeconds":6}{}`},
		{name: "oversize", data: strings.Repeat(" ", telemetry.MaxMessageSize+1)},
		{name: "bad JSON", data: "{"},
		{name: "nonfinite CPU", data: `{"version":1,"sequence":2,"elapsedSeconds":6,"cpuSeconds":1e309}`},
		{name: "bad version", change: func(s *telemetry.Snapshot) { s.Version = 9 }},
		{name: "repeated sequence", change: func(s *telemetry.Snapshot) { s.Sequence = 1 }},
		{name: "elapsed rollback", change: func(s *telemetry.Snapshot) { s.ElapsedSeconds = 0 }},
		{name: "tiny elapsed delta", change: func(s *telemetry.Snapshot) { s.ElapsedSeconds = math.Nextafter(1, 2) }},
		{name: "frame rollback", change: func(s *telemetry.Snapshot) { s.FramesSent = 9 }},
		{name: "payload rollback", change: func(s *telemetry.Snapshot) { s.PayloadBytes = 99 }},
		{name: "CPU rollback", change: func(s *telemetry.Snapshot) { value := 0.5; s.CPUSeconds = &value }},
		{name: "counter rollback", change: func(s *telemetry.Snapshot) { s.EncodeCount = 9 }},
		{name: "bad encoder", change: func(s *telemetry.Snapshot) { s.Encoder = "<script>" }},
		{name: "flood", arrival: 100 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p := &processPerformance{}
			now := time.Now()
			first := performanceTestSample()
			if !p.decode(performanceTestJSON(t, first), now) {
				t.Fatal("first sample rejected")
			}
			next := first
			next.Sequence, next.ElapsedSeconds = 2, 6
			if test.change != nil {
				test.change(&next)
			}
			data := []byte(test.data)
			if test.data == "" {
				data = performanceTestJSON(t, next)
			}
			arrival := 5 * time.Second
			if test.arrival != 0 {
				arrival = test.arrival
			}
			if p.decode(data, now.Add(arrival)) {
				t.Fatal("invalid sample accepted")
			}
			got := p.snapshot(now)
			if got.SampledAt == nil || !got.SampledAt.Equal(now) || got.FramesSent != 10 {
				t.Fatalf("invalid sample replaced latest values: %+v", got)
			}
			if _, err := json.Marshal(got); err != nil {
				t.Fatalf("nonfinite derived result: %v", err)
			}
		})
	}
}

func TestProcessPerformanceSocketAndShutdown(t *testing.T) {
	t.Parallel()
	conn, child, err := newPerformancePipe()
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	p := newProcessPerformance(conn)
	t.Cleanup(p.close)
	for _, data := range [][]byte{
		[]byte("{"), []byte(strings.Repeat(" ", telemetry.MaxMessageSize+100)),
		performanceTestJSON(t, performanceTestSample()),
	} {
		if err := unix.Send(int(child.Fd()), data, unix.MSG_DONTWAIT); err != nil {
			t.Fatal(err)
		}
	}
	waitPerformance(t, p, "live")
	var closers sync.WaitGroup
	for range 4 {
		closers.Go(p.close)
	}
	closers.Wait()
	select {
	case <-p.done:
	default:
		t.Fatal("close did not join receiver")
	}
}

func waitPerformance(t *testing.T, p *processPerformance, state string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for p.snapshot(time.Now()).State != state {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s telemetry", state)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPerformanceSocketQueueBounded(t *testing.T) {
	t.Parallel()
	conn, child, err := newPerformancePipe()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer child.Close()
	data := make([]byte, telemetry.MaxMessageSize)
	for range 1024 {
		err := unix.Send(int(child.Fd()), data, unix.MSG_DONTWAIT)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("unread telemetry queue did not apply bounded backpressure")
}

func TestManagerPerformanceOwnershipAndLaunchIsolation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	record := VM{ID: strings.Repeat("a", 26), Owner: "alice", State: "stopped"}
	v := newManagedVM(record)
	m := &Manager{ctx: ctx, config: ManagerConfig{MemoryMiB: 1024}, vms: map[string]*managedVM{record.ID: v}}
	if _, err := m.Performance(t.Context(), "bob", record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign telemetry visible: %v", err)
	}
	if got, err := m.Performance(t.Context(), "alice", record.ID); err != nil || got.State != "stopped" ||
		got.GuestMemoryBytes != 1<<30 || got.CPUPercent != nil {
		t.Fatalf("stopped state: %+v, %v", got, err)
	}
	v.record.State = "starting"
	v.publish()
	if got, _ := m.Performance(t.Context(), "alice", record.ID); got.State != "starting" {
		t.Fatalf("prelaunch starting state = %s", got.State)
	}
	v.record.State = "creating"
	v.publish()
	if got, _ := m.Performance(t.Context(), "alice", record.ID); got.State != "starting" || got.SampledAt != nil {
		t.Fatalf("provisioning state = %+v", got)
	}
	old := &processPerformance{}
	if !old.decode(performanceTestJSON(t, performanceTestSample()), time.Now()) {
		t.Fatal("sample rejected")
	}
	v.record.State, v.run = "running", &managedProcess{done: make(chan struct{}), performance: old}
	v.publish()
	if got, _ := m.Performance(t.Context(), "alice", record.ID); got.State != "live" || got.FramesSent != 10 {
		t.Fatalf("active process missing data: %+v", got)
	}
	close(v.run.done)
	if got, _ := m.Performance(t.Context(), "alice", record.ID); got.State != "stopped" || got.SampledAt != nil {
		t.Fatalf("exited process still appears live: %+v", got)
	}
	v.run = &managedProcess{done: make(chan struct{}), performance: &processPerformance{}}
	v.publish()
	if got, _ := m.Performance(t.Context(), "alice", record.ID); got.State != "unavailable" || got.FramesSent != 0 ||
		got.SampledAt != nil || got.CPUPercent != nil {
		t.Fatalf("new launch inherited old data: %+v", got)
	}
	canceled, stop := context.WithCancel(t.Context())
	stop()
	if _, err := m.Performance(canceled, "alice", record.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}

func TestManagerPerformanceConfiguration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		state  string
		run    bool
		sample bool
		exited bool
		want   string
	}{
		{name: "stopped", state: "stopped", want: "stopped"},
		{name: "creating", state: "creating", want: "starting"},
		{name: "starting", state: "starting", run: true, want: "starting"},
		{name: "unavailable", state: "running", run: true, want: "unavailable"},
		{name: "live", state: "running", run: true, sample: true, want: "live"},
		{name: "exited", state: "running", run: true, sample: true, exited: true, want: "stopped"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			record := VM{ID: strings.Repeat("a", 26), Owner: "alice", State: test.state}
			v := newManagedVM(record)
			if test.run {
				v.run = &managedProcess{done: make(chan struct{}), performance: &processPerformance{}}
				if test.sample && !v.run.performance.decode(performanceTestJSON(t, performanceTestSample()), time.Now()) {
					t.Fatal("sample rejected")
				}
				if test.exited {
					close(v.run.done)
				}
				v.publish()
			}
			m := &Manager{
				ctx: t.Context(), vms: map[string]*managedVM{record.ID: v},
				config: ManagerConfig{CPUs: 2, MemoryMiB: 4096, Encoder: "auto", RDPProfile: "office"},
			}
			got, err := m.Performance(t.Context(), "alice", record.ID)
			if err != nil || got.State != test.want {
				t.Fatalf("performance state = %+v, %v; want %s", got, err, test.want)
			}
			if got.VCPUs != 2 || got.GuestMemoryBytes != 4<<30 ||
				got.RequestedEncoder != "auto" || got.RDPProfile != "office" {
				t.Fatalf("configuration missing or replaced by observed backend: %+v", got)
			}
			if test.want == "live" && got.Encoder != "software" {
				t.Fatalf("observed encoder = %q, want software independently of requested auto", got.Encoder)
			}
		})
	}
}

func TestManagerPerformanceInheritedSocketLifecycle(t *testing.T) {
	t.Parallel()
	config := managerTestConfig(t, "graceful")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nGORACE=atexit_sleep_ms=0 GOKVM_PERFORMANCE_HELPER=1 exec " + quote(os.Args[0]) +
		" -test.run='^TestPerformanceHelperProcess$' -- \"$@\"\n"
	//nolint:gosec // Private executable test fixture.
	if err := os.WriteFile(config.Binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	m.readResident = func(int) (uint64, error) { return 0, nil }
	m.readMemory = func() (hostMemory, error) { return hostMemory{Total: 64 << 30, Available: 32 << 30}, nil }
	vm := createManagerVM(t, m, "alice", "telemetry")
	var previous *processPerformance
	for range 2 {
		if err := m.Start(t.Context(), "alice", vm.ID); err != nil {
			t.Fatal(err)
		}
		v, err := m.lookup("alice", vm.ID)
		if err != nil {
			t.Fatal(err)
		}
		p := v.view.Load().run.performance
		if p == nil || p == previous {
			t.Fatal("missing or reused launch telemetry receiver")
		}
		waitPerformance(t, p, "live")
		if got, err := m.Performance(t.Context(), "alice", vm.ID); err != nil || got.FramesSent != 10 {
			t.Fatalf("inherited telemetry not visible: %+v, %v", got, err)
		}
		if err := m.Stop(t.Context(), "alice", vm.ID); err != nil {
			t.Fatal(err)
		}
		select {
		case <-p.done:
		default:
			t.Fatal("process exited without joining telemetry reader")
		}
		previous = p
	}
	if err := m.Start(t.Context(), "alice", vm.ID); err != nil {
		t.Fatal(err)
	}
	v, err := m.lookup("alice", vm.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := v.view.Load().run.performance
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
	default:
		t.Fatal("manager close did not join receiver")
	}
}

func TestPerformanceHelperProcess(t *testing.T) { //nolint:paralleltest // Standalone signal-driven helper process.
	if os.Getenv("GOKVM_PERFORMANCE_HELPER") == "" {
		return
	}
	args := make(map[string]string)
	for i, arg := range os.Args {
		if strings.HasPrefix(arg, "-") && i+1 < len(os.Args) {
			args[arg] = os.Args[i+1]
		}
	}
	if args["-telemetry-fd"] != "4" || args["-rdp-fd"] != "3" {
		t.Fatal("missing inherited descriptors")
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGTERM)
	if err := managerHelperListen(args, "graceful"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Send(4, performanceTestJSON(t, performanceTestSample()), unix.MSG_DONTWAIT); err != nil {
		t.Fatal(err)
	}
	// Confirm the helper never receives guest/output log paths for metrics.
	if _, err := os.Stat(filepath.Join(filepath.Dir(args["-d"]), "performance.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(fmt.Errorf("unexpected performance persistence: %w", err))
	}
	<-signals
	os.Exit(0)
}
