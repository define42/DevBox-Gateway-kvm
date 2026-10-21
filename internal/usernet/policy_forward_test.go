package usernet

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
)

func TestPolicyExplicitHostTCPAndUDP(t *testing.T) {
	t.Parallel()
	n, err := newNetworkWithPolicy(nil, "allowlist", "127.0.0.1/32")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := n.Close(); err != nil {
			t.Error(err)
		}
	})
	guest := guestStack(t, n)
	t.Run("TCP", func(t *testing.T) {
		t.Parallel()
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		done := make(chan struct{})
		go func() {
			defer close(done)
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			_, _ = io.Copy(conn, conn)
		}()
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		address := tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: uint16(listener.Addr().(*net.TCPAddr).Port)}
		conn, err := gonet.DialContextTCP(ctx, guest, address, ipv4.ProtocolNumber)
		if err != nil {
			t.Fatal(err)
		}
		exchange(t, conn)
		_ = conn.Close()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("explicitly allowed TCP relay did not close")
		}
	})
	t.Run("UDP", func(t *testing.T) {
		t.Parallel()
		host, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer host.Close()
		go func() {
			data := make([]byte, 1600)
			count, address, readErr := host.ReadFrom(data)
			if readErr == nil {
				_, _ = host.WriteTo(data[:count], address)
			}
		}()
		address := tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: uint16(host.LocalAddr().(*net.UDPAddr).Port)}
		conn, err := gonet.DialUDP(guest, nil, &address, ipv4.ProtocolNumber)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		exchange(t, conn)
	})
}
