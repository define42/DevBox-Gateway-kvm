package gateway

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errRDPProxyTest = errors.New("private proxy transport error")

func TestRDPProxyRetainsFirstOperationBeforeCleanup(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"front_read", "front_write", "backend_read", "backend_write"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			front, backend := newRDPProxyTestConn(), newRDPProxyTestConn()
			switch operation {
			case "front_read":
				front.read = func([]byte) (int, error) { return 0, errRDPProxyTest }
			case "backend_read":
				backend.read = func([]byte) (int, error) { return 0, errRDPProxyTest }
			case "front_write":
				backend.read = rdpProxyTestOneRead(backend, []byte("backend payload"))
				front.write = func([]byte) (int, error) { return 0, errRDPProxyTest }
			case "backend_write":
				front.read = rdpProxyTestOneRead(front, []byte("front payload"))
				backend.write = func([]byte) (int, error) { return 0, errRDPProxyTest }
			}
			result, closes := startRDPProxyTest(t, front, backend)
			err := waitRDPProxyTest(t, result)
			assertRDPProxyTestError(t, err, operation, errRDPProxyTest)
			if got := closes.Load(); got != 1 {
				t.Fatalf("close callback called %d times, want one", got)
			}
		})
	}
}

func TestRDPProxyShortWritesRetainDestination(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"front_write", "backend_write"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			front, backend := newRDPProxyTestConn(), newRDPProxyTestConn()
			source, destination := front, backend
			if operation == "front_write" {
				source, destination = backend, front
			}
			source.read = rdpProxyTestOneRead(source, []byte("payload"))
			destination.write = func(p []byte) (int, error) { return len(p) - 1, nil }
			result, _ := startRDPProxyTest(t, front, backend)
			assertRDPProxyTestError(t, waitRDPProxyTest(t, result), operation, io.ErrShortWrite)
		})
	}
}

func TestRDPProxyForwardsBytesReturnedWithEOF(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"front_read", "backend_read"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			front, backend := newRDPProxyTestConn(), newRDPProxyTestConn()
			source, destination := front, backend
			if operation == "backend_read" {
				source, destination = backend, front
			}
			payload := []byte{0, 1, 0x80, 0xff, 'e', 'n', 'd'}
			source.read = func(p []byte) (int, error) { return copy(p, payload), io.EOF }
			started := time.Now()
			result, _ := startRDPProxyTest(t, front, backend)
			assertRDPProxyTestError(t, waitRDPProxyTest(t, result), operation, io.EOF)
			finished := time.Now()
			if got := destination.output(); !bytes.Equal(got, payload) {
				t.Fatalf("bytes accompanying EOF = %x, want %x", got, payload)
			}
			// Observing EOF must not close the sockets until io.Copy has sent
			// those final bytes, and existing sliding deadlines must survive.
			assertRDPProxyTestDeadline(t, source.readDeadline, started, finished, 5*time.Minute)
			assertRDPProxyTestDeadline(t, destination.writeDeadline, started, finished, 30*time.Second)
		})
	}
}

func TestRDPProxyPreservesTimeout(t *testing.T) {
	t.Parallel()
	front, backend := newRDPProxyTestConn(), newRDPProxyTestConn()
	front.read = func([]byte) (int, error) { return 0, os.ErrDeadlineExceeded }
	result, _ := startRDPProxyTest(t, front, backend)
	err := waitRDPProxyTest(t, result)
	assertRDPProxyTestError(t, err, "front_read", os.ErrDeadlineExceeded)
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("proxy error lost timeout classification: %v", err)
	}
}

func TestRDPProxyObservesReadErrorBeforeFinalWriteError(t *testing.T) {
	t.Parallel()
	front, backend := newRDPProxyTestConn(), newRDPProxyTestConn()
	front.read = func(p []byte) (int, error) { return copy(p, "final bytes"), io.EOF }
	backend.write = func([]byte) (int, error) { return 0, errRDPProxyTest }
	result, _ := startRDPProxyTest(t, front, backend)
	// io.Copy returns the write error in this case. First-close attribution
	// must retain the EOF observed earlier when the final bytes were read.
	assertRDPProxyTestError(t, waitRDPProxyTest(t, result), "front_read", io.EOF)
}

func TestRDPProxySimultaneousExits(t *testing.T) {
	t.Parallel()
	front, backend := newRDPProxyTestConn(), newRDPProxyTestConn()
	started, release := make(chan struct{}, 2), make(chan struct{})
	read := func([]byte) (int, error) {
		started <- struct{}{}
		<-release

		return 0, io.EOF
	}
	front.read, backend.read = read, read
	result, closes := startRDPProxyTest(t, front, backend)
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("both proxy readers did not start")
		}
	}
	close(release)
	err := waitRDPProxyTest(t, result)
	var proxyErr *rdpProxyError
	if !errors.As(err, &proxyErr) || !errors.Is(err, io.EOF) ||
		(proxyErr.operation != "front_read" && proxyErr.operation != "backend_read") {
		t.Fatalf("simultaneous EOF result = %v", err)
	}
	if got := closes.Load(); got != 1 {
		t.Fatalf("simultaneous exits closed connections %d times, want one", got)
	}
}

func TestRDPProxyPreservesBidirectionalBytes(t *testing.T) {
	t.Parallel()
	client, front := net.Pipe()
	backend, worker := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	t.Cleanup(func() { _ = worker.Close() })
	for _, conn := range []net.Conn{client, worker} {
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	result, closes := startRDPProxyTest(t, front, backend)
	for _, direction := range []struct {
		source, destination net.Conn
		payload             []byte
	}{
		{source: client, destination: worker, payload: bytes.Repeat([]byte{0, 1, 0x80, 0xff}, 20000)},
		{source: worker, destination: client, payload: bytes.Repeat([]byte{0xff, 0x80, 1, 0}, 20000)},
	} {
		written := make(chan error, 1)
		go func() {
			_, err := direction.source.Write(direction.payload)
			written <- err
		}()
		got := make([]byte, len(direction.payload))
		if _, err := io.ReadFull(direction.destination, got); err != nil {
			t.Fatal(err)
		}
		if err := <-written; err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, direction.payload) {
			t.Fatal("proxy modified relayed bytes")
		}
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	assertRDPProxyTestError(t, waitRDPProxyTest(t, result), "front_read", io.EOF)
	if got := closes.Load(); got != 1 {
		t.Fatalf("close callback called %d times, want one", got)
	}
}

func startRDPProxyTest(t *testing.T, front, backend net.Conn) (<-chan error, *atomic.Int32) {
	t.Helper()
	closeBoth := func() {
		_ = front.Close()
		_ = backend.Close()
	}
	t.Cleanup(closeBoth)
	closes := &atomic.Int32{}
	result := make(chan error, 1)
	go func() {
		result <- proxyRDPConnections(front, backend, func() {
			closes.Add(1)
			closeBoth()
		})
	}()

	return result, closes
}

func waitRDPProxyTest(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not finish both copy loops")

		return nil
	}
}

func assertRDPProxyTestError(t *testing.T, err error, operation string, cause error) {
	t.Helper()
	var proxyErr *rdpProxyError
	if !errors.As(err, &proxyErr) || proxyErr.operation != operation || !errors.Is(err, cause) {
		t.Fatalf("proxy result = %v, want operation %s wrapping %v", err, operation, cause)
	}
}

func assertRDPProxyTestDeadline(t *testing.T, deadline, started, finished time.Time, duration time.Duration) {
	t.Helper()
	if deadline.Before(started.Add(duration)) || deadline.After(finished.Add(duration)) {
		t.Fatalf("proxy deadline %s outside expected interval [%s, %s]", deadline,
			started.Add(duration), finished.Add(duration))
	}
}

type rdpProxyTestConn struct {
	read          func([]byte) (int, error)
	write         func([]byte) (int, error)
	closed        chan struct{}
	closeOnce     sync.Once
	mu            sync.Mutex
	written       []byte
	readDeadline  time.Time
	writeDeadline time.Time
}

func newRDPProxyTestConn() *rdpProxyTestConn {
	return &rdpProxyTestConn{closed: make(chan struct{})}
}

func (c *rdpProxyTestConn) Read(p []byte) (int, error) {
	if c.read != nil {
		return c.read(p)
	}
	<-c.closed

	return 0, net.ErrClosed
}

func (c *rdpProxyTestConn) Write(p []byte) (int, error) {
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
	}
	if c.write != nil {
		return c.write(p)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.written = append(c.written, p...)

	return len(p), nil
}

func (c *rdpProxyTestConn) output() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	return bytes.Clone(c.written)
}

func (c *rdpProxyTestConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })

	return nil
}

func (*rdpProxyTestConn) LocalAddr() net.Addr  { return &net.TCPAddr{} }
func (*rdpProxyTestConn) RemoteAddr() net.Addr { return &net.TCPAddr{} }

func (c *rdpProxyTestConn) SetDeadline(deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline, c.writeDeadline = deadline, deadline

	return nil
}

func (c *rdpProxyTestConn) SetReadDeadline(deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = deadline

	return nil
}

func (c *rdpProxyTestConn) SetWriteDeadline(deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeDeadline = deadline

	return nil
}

func rdpProxyTestOneRead(conn *rdpProxyTestConn, data []byte) func([]byte) (int, error) {
	read := false

	return func(p []byte) (int, error) {
		if !read {
			read = true

			return copy(p, data), nil
		}
		<-conn.closed

		return 0, net.ErrClosed
	}
}
