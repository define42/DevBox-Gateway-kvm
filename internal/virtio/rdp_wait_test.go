package virtio

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/define42/gokvm/internal/rdp"
)

func TestRDPWaitReadyRejectsIncompleteConnections(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"TCP", "TLS"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			d := newRDPTestDisplay(t)
			conn := dialRDPTestDisplay(t, d)
			if stage == "TLS" {
				negotiateRDPTestDisplay(t, conn, 1)
				secured := tls.Client(conn, rdpTestClientTLS(t, d))
				if err := secured.Handshake(); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			if err := d.WaitReady(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("incomplete %s connection released boot wait: %v", stage, err)
			}
		})
	}
}

func TestRDPWaitReadyWakesOnClose(t *testing.T) {
	t.Parallel()
	d := newRDPTestDisplay(t)
	result := make(chan error, 1)
	go func() { result <- d.WaitReady(t.Context()) }()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("display close returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("display close did not release the readiness waiter")
	}
}

func TestRDPWaitReadyBeforeFirstGuestFrame(t *testing.T) {
	t.Parallel()
	d := newRDPTestDisplay(t)
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	// Session represents a fully activated connection. Suppression prevents
	// writes and proves readiness does not depend on publishing a guest frame.
	session := &rdp.Session{Width: 800, Height: 600, BitsPerPixel: 32}
	var suppressed atomic.Bool
	suppressed.Store(true)
	stop := make(chan struct{})
	close(stop)
	for range 2 {
		if err := d.writeFrames(conn, session, stop, nil, &suppressed); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		err := d.WaitReady(ctx)
		cancel()
		if err != nil {
			t.Fatalf("activated display without a guest frame remained blocked: %v", err)
		}
	}
	if d.seq != 0 || len(d.frame) != 0 {
		t.Fatal("test unexpectedly published a guest frame")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := d.WaitReady(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ready display ignored cancellation: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.WaitReady(t.Context()); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("ready display ignored shutdown: %v", err)
	}
}
