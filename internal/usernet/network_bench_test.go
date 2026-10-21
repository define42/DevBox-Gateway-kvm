//go:build integration

package usernet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
)

// BenchmarkNetworkTCP measures established TCP connections through guestStack's
// Ethernet bridge, Network.Write/Read, netstack and the production host-socket
// forwarder. It includes the in-process guest and loopback fixture overhead;
// SetBytes counts application payload in the named direction, excluding ACKs.
func BenchmarkNetworkTCP(b *testing.B) {
	for _, direction := range []string{"upload", "download"} {
		for _, size := range []int{64 << 10, 1 << 20} {
			b.Run(fmt.Sprintf("%s/%dKiB", direction, size>>10), func(b *testing.B) {
				n := testNetwork(b)
				guest := guestStack(b, n)
				payload := bytes.Repeat([]byte{0x5a}, size)
				address := measurementTCPServer(b, func(conn net.Conn) {
					data := make([]byte, size)
					ack := []byte{1}
					for {
						//nolint:nestif // Keep the two fixture protocols together and symmetric with the timed loop.
						if direction == "upload" {
							if _, err := io.ReadFull(conn, data); err != nil {
								return
							}
							if !bytes.Equal(data, payload) {
								return
							}
							if _, err := conn.Write(ack); err != nil {
								return
							}
						} else {
							if _, err := io.ReadFull(conn, ack); err != nil {
								return
							}
							if _, err := conn.Write(payload); err != nil {
								return
							}
						}
					}
				})
				ctx, cancel := context.WithTimeout(b.Context(), 5*time.Second)
				conn, err := gonet.DialContextTCP(ctx, guest, address, ipv4.ProtocolNumber)
				cancel()
				if err != nil {
					b.Fatal(err)
				}
				defer conn.Close()
				if err := conn.SetDeadline(time.Now().Add(2 * time.Minute)); err != nil {
					b.Fatal(err)
				}
				ack := []byte{1}
				received := make([]byte, size)
				b.ReportAllocs()
				b.SetBytes(int64(size))
				for b.Loop() {
					//nolint:nestif // Keep measured socket operations inline instead of adding per-iteration callbacks.
					if direction == "upload" {
						if _, err := conn.Write(payload); err != nil {
							b.Fatal(err)
						}
						if _, err := io.ReadFull(conn, ack); err != nil {
							b.Fatal(err)
						}
					} else {
						if _, err := conn.Write(ack); err != nil {
							b.Fatal(err)
						}
						if _, err := io.ReadFull(conn, received); err != nil {
							b.Fatal(err)
						}
					}
				}
				if direction == "download" && !bytes.Equal(received, payload) {
					b.Fatal("download payload mismatch")
				}
			})
		}
	}
}

// measurementTCPServer binds only loopback. Every accepted socket and handler
// is owned by the fixture, so cancellation tests cannot strand host goroutines.
func measurementTCPServer(tb testing.TB, handle func(net.Conn)) tcpip.FullAddress {
	tb.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	var mu sync.Mutex
	connections := make(map[net.Conn]struct{})
	var workers sync.WaitGroup
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			mu.Lock()
			connections[conn] = struct{}{}
			mu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() {
					_ = conn.Close()
					mu.Lock()
					delete(connections, conn)
					mu.Unlock()
				}()
				handle(conn)
			}()
		}
	}()
	tb.Cleanup(func() {
		_ = listener.Close()
		<-accepted
		mu.Lock()
		for conn := range connections {
			_ = conn.Close()
		}
		mu.Unlock()
		done := make(chan struct{})
		go func() { workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			tb.Error("loopback fixture handlers did not stop")
		}
	})

	return tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: uint16(listener.Addr().(*net.TCPAddr).Port)}
}

func measurementEcho(tb testing.TB, conn net.Conn) {
	tb.Helper()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		tb.Fatal(err)
	}
	want := []byte("forwarding remains usable")
	if _, err := conn.Write(want); err != nil {
		tb.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		tb.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		tb.Fatal("forwarding corrupted application bytes")
	}
	if err := conn.SetDeadline(time.Time{}); err != nil && !errors.Is(err, net.ErrClosed) {
		tb.Fatal(err)
	}
}
