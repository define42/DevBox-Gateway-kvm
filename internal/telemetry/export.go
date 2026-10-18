package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var ErrDescriptor = errors.New("telemetry requires a connected inherited Unix datagram descriptor >=3")

// Start takes ownership of fd (when >=3), including on initialization failure.
// Its exporter sends immediately and every Interval, even without RDP traffic.
// Datagram writes never block: a full/disconnected peer drops a snapshot, while
// the next heartbeat retains cumulative counters. Stop cancels, joins and closes.
func Start(ctx context.Context, fd int) (*Collector, func(), error) {
	if fd < 3 {
		return nil, nil, ErrDescriptor
	}
	file := os.NewFile(uintptr(fd), "vm-telemetry")
	if err := validateDescriptor(fd); err != nil {
		_ = file.Close()

		return nil, nil, err
	}
	unix.CloseOnExec(fd)
	collector := New()
	exportCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer file.Close()
		collector.run(exportCtx, func(data []byte) {
			// MSG_DONTWAIT is per call: no descriptor-wide flag changes are
			// needed, and failure never propagates into guest execution.
			_ = unix.Send(fd, data, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
		})
	}()
	var once sync.Once
	stop := func() {
		once.Do(cancel)
		<-done
	}

	return collector, stop, nil
}

func validateDescriptor(fd int) error {
	typeID, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDescriptor, err)
	}
	if typeID != unix.SOCK_DGRAM {
		return ErrDescriptor
	}
	peer, err := unix.Getpeername(fd)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDescriptor, err)
	}
	if _, ok := peer.(*unix.SockaddrUnix); !ok {
		return ErrDescriptor
	}

	return nil
}

func (c *Collector) run(ctx context.Context, send func([]byte)) {
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		data, err := json.Marshal(c.Snapshot())
		if err == nil && len(data) <= MaxMessageSize {
			send(data)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func processResources() (*float64, *uint64) {
	var cpu *float64
	var usage unix.Rusage
	if unix.Getrusage(unix.RUSAGE_SELF, &usage) == nil {
		seconds := float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1e6 +
			float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1e6
		cpu = &seconds
	}
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return cpu, nil
	}

	return cpu, residentBytes(data, uint64(os.Getpagesize()))
}

func residentBytes(data []byte, pageSize uint64) *uint64 {
	fields := strings.Fields(string(data))
	if len(fields) < 2 || pageSize == 0 {
		return nil
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || pages > ^uint64(0)/pageSize {
		return nil
	}
	bytes := pages * pageSize

	return &bytes
}
