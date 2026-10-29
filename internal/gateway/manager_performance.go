package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
	"golang.org/x/sys/unix"
)

const performanceStaleAfter = 3 * telemetry.Interval

// VMPerformance is a bounded, owner-scoped view of one current VM process.
// CPUPercent counts one fully used host CPU as 100%; null means unavailable.
// Timings are interval averages, except ACKMs, which is the latest ACK sample.
// PayloadBytes excludes transport overhead; Network includes RDP transport bytes.
// All rates use the measured sample interval.
type VMPerformance struct {
	State                 string                      `json:"state"`
	SampledAt             *time.Time                  `json:"sampledAt"`
	SampleIntervalSeconds *float64                    `json:"sampleIntervalSeconds"`
	CPUPercent            *float64                    `json:"cpuPercent"`
	ResidentBytes         *uint64                     `json:"residentBytes"`
	GuestMemoryBytes      uint64                      `json:"guestMemoryBytes"`
	VCPUs                 int                         `json:"vcpus"`
	RequestedEncoder      string                      `json:"requestedEncoder"`
	RDPProfile            string                      `json:"rdpProfile"`
	Connections           uint32                      `json:"connections"`
	Encoder               string                      `json:"encoder"`
	FramesSent            uint64                      `json:"framesSent"`
	FramesPerSecond       *float64                    `json:"framesPerSecond"`
	PayloadBytes          uint64                      `json:"payloadBytes"`
	PayloadBytesPerSecond *float64                    `json:"payloadBytesPerSecond"`
	EncodeMs              *float64                    `json:"encodeMs"`
	WriteMs               *float64                    `json:"writeMs"`
	ACKMs                 *float64                    `json:"ackMs"`
	Pipeline              *VMPipelinePerformance      `json:"pipeline"`
	Network               *VMNetworkPerformance       `json:"network"`
	RDPSessions           []telemetry.RDPSessionEvent `json:"rdpSessions,omitempty"`
}

// PerformanceBackend optionally supplies private in-memory worker telemetry.
type PerformanceBackend interface {
	Performance(ctx context.Context, owner, id string) (VMPerformance, error)
}

// Performance never waits for lifecycle work or performs process/file I/O.
// Capturing the immutable launch view prevents old worker data from becoming
// associated with a replacement process, even if a restart races this request.
func (m *Manager) Performance(ctx context.Context, owner, id string) (VMPerformance, error) {
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return VMPerformance{}, err
	}
	defer finish()
	v, err := m.lookup(owner, id)
	if err != nil {
		return VMPerformance{}, err
	}
	view := v.view.Load()
	if view.record.State == "deleted" {
		return VMPerformance{}, ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return VMPerformance{}, err
	}
	result := VMPerformance{State: "stopped"}
	if view.record.State == "starting" || view.record.State == "creating" {
		result.State = "starting"
	}
	if view.run != nil {
		result.State = "unavailable"
		if view.run.performance != nil {
			result = view.run.performance.snapshot(time.Now())
		}
		if view.record.State == "starting" || view.record.State == "creating" {
			result.State = "starting"
		}
		select {
		case <-view.run.done:
			result = VMPerformance{State: "stopped"}
		default:
		}
	}
	result.GuestMemoryBytes = uint64(m.config.MemoryMiB) << 20
	result.VCPUs = m.config.CPUs
	// Retain the read-only API fields for clients that display the graphics policy.
	result.RequestedEncoder = "software"
	result.RDPProfile = "smooth"

	return result, nil
}

// A connected Unix seqpacket pair carries no path, PID, username, or caller-controlled VM identity.
// Unlike datagrams, its worker endpoint cannot reconnect to another Unix service.
// Close-on-exec prevents accidental inheritance by other workers; os/exec maps
// only the explicit child end to descriptor 4. Failure disables telemetry only.
func newPerformancePipe() (*net.UnixConn, *os.File, error) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	parent := os.NewFile(uintptr(fds[0]), "performance-parent")
	child := os.NewFile(uintptr(fds[1]), "performance-child")
	defer parent.Close()
	if err := unix.SetsockoptInt(fds[0], unix.SOL_SOCKET, unix.SO_RCVBUF, 4*telemetry.MaxMessageSize); err != nil {
		_ = child.Close()

		return nil, nil, err
	}
	// AF_UNIX packet queue memory is charged to the sender; SO_RCVBUF alone
	// does not bound it. The child exporter uses nonblocking sends and drops
	// updates when this deliberately small queue is full.
	if err := unix.SetsockoptInt(fds[1], unix.SOL_SOCKET, unix.SO_SNDBUF, 4*telemetry.MaxMessageSize); err != nil {
		_ = child.Close()

		return nil, nil, err
	}
	connection, err := net.FileConn(parent)
	if err != nil {
		_ = child.Close()

		return nil, nil, err
	}

	return connection.(*net.UnixConn), child, nil
}

type processPerformance struct {
	conn     *net.UnixConn
	done     chan struct{}
	stop     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	current  telemetry.Snapshot
	previous telemetry.Snapshot
	received time.Time
}

func newProcessPerformance(conn *net.UnixConn) *processPerformance {
	p := &processPerformance{conn: conn, done: make(chan struct{}), stop: make(chan struct{})}
	go p.read()

	return p
}

func (p *processPerformance) close() {
	p.once.Do(func() {
		close(p.stop)
		_ = p.conn.Close()
	})
	<-p.done
}

func (p *processPerformance) read() {
	defer close(p.done)
	buffer := make([]byte, telemetry.MaxMessageSize+1)
	pace := time.NewTimer(time.Hour)
	defer pace.Stop()
	for {
		n, _, flags, _, err := p.conn.ReadMsgUnix(buffer, nil)
		if err != nil {
			return
		}
		if n <= telemetry.MaxMessageSize && flags&unix.MSG_TRUNC == 0 {
			p.decode(buffer[:n], time.Now())
		}
		// Bound decoding work even if a worker sends a continuous packet flood.
		// Healthy five-second samples arrive after this pause has finished.
		pace.Reset(100 * time.Millisecond)
		select {
		case <-p.stop:
			return
		case <-pace.C:
		}
	}
}

func (p *processPerformance) decode(data []byte, received time.Time) bool {
	if len(data) == 0 || len(data) > telemetry.MaxMessageSize {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var sample telemetry.Snapshot
	if err := decoder.Decode(&sample); err != nil {
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return false
	}
	if err := sample.Validate(); err != nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.received.IsZero() && (received.Sub(p.received) < time.Second || !performanceAdvances(p.current, sample)) {
		return false
	}
	p.previous, p.current, p.received = p.current, sample, received

	return true
}

func performanceAdvances(previous, current telemetry.Snapshot) bool {
	if current.Sequence <= previous.Sequence ||
		current.ElapsedSeconds-previous.ElapsedSeconds < time.Millisecond.Seconds() {
		return false
	}
	if current.CPUSeconds != nil && previous.CPUSeconds != nil && *current.CPUSeconds < *previous.CPUSeconds {
		return false
	}
	if !current.Pipeline.MonotonicAfter(previous.Pipeline) || !networkAdvances(previous.Network, current.Network) ||
		!telemetry.RDPSessionsMonotonicAfter(previous.RDPSessions, current.RDPSessions) {
		return false
	}

	return current.FramesSent >= previous.FramesSent && current.PayloadBytes >= previous.PayloadBytes &&
		current.EncodeNanos >= previous.EncodeNanos && current.EncodeCount >= previous.EncodeCount &&
		current.WriteNanos >= previous.WriteNanos && current.WriteCount >= previous.WriteCount
}

func (p *processPerformance) snapshot(now time.Time) VMPerformance {
	p.mu.Lock()
	current, previous, received := p.current, p.previous, p.received
	p.mu.Unlock()
	if received.IsZero() {
		return VMPerformance{State: "unavailable"}
	}
	result := VMPerformance{
		State: "live", SampledAt: &received, Connections: current.Connections,
		Encoder: current.Encoder, FramesSent: current.FramesSent, PayloadBytes: current.PayloadBytes,
		Pipeline: pipelinePerformance(previous, current), Network: networkPerformance(previous, current),
		RDPSessions: cloneRDPSessionEvents(current.RDPSessions),
	}
	if now.Sub(received) > performanceStaleAfter {
		result.State = "stale"
	}
	if current.ResidentBytes != nil {
		resident := *current.ResidentBytes
		result.ResidentBytes = &resident
	}
	if previous.Sequence != 0 {
		elapsed := current.ElapsedSeconds - previous.ElapsedSeconds
		result.SampleIntervalSeconds = &elapsed
		if current.CPUSeconds != nil && previous.CPUSeconds != nil {
			cpu := 100 * (*current.CPUSeconds - *previous.CPUSeconds) / elapsed
			result.CPUPercent = &cpu
		}
		fps := float64(current.FramesSent-previous.FramesSent) / elapsed
		result.FramesPerSecond = &fps
		payloadRate := float64(current.PayloadBytes-previous.PayloadBytes) / elapsed
		result.PayloadBytesPerSecond = &payloadRate
		result.EncodeMs = performanceMean(current.EncodeNanos-previous.EncodeNanos, current.EncodeCount-previous.EncodeCount)
		result.WriteMs = performanceMean(current.WriteNanos-previous.WriteNanos, current.WriteCount-previous.WriteCount)
	}
	if current.ACKNanos != 0 {
		ack := float64(current.ACKNanos) / 1e6
		result.ACKMs = &ack
	}
	if current.Connections == 0 {
		result.Encoder = ""
		result.EncodeMs, result.WriteMs, result.ACKMs = nil, nil, nil
	}

	return result
}

func performanceMean(total, count uint64) *float64 {
	if count == 0 {
		return nil
	}
	value := float64(total) / float64(count) / 1e6

	return &value
}
