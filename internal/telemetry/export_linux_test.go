//go:build linux && amd64

package telemetry

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Existing Unix descriptors remain usable after worker seccomp blocks socket().
// They must not provide an alternate route to pathname services outside Landlock.
func TestTelemetryEndpointCannotRedirect(t *testing.T) {
	t.Parallel()
	peer, child := seqpacketPair(t)
	defer unix.Close(child)
	duplicate, err := unix.FcntlInt(uintptr(child), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(duplicate)
	target, err := unix.Socket(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(target)
	address := &unix.SockaddrUnix{Name: filepath.Join(t.TempDir(), "target")}
	if err := unix.Bind(target, address); err != nil {
		t.Fatal(err)
	}
	if err := unix.Listen(target, 1); err != nil {
		t.Fatal(err)
	}
	for _, fd := range []int{child, duplicate} {
		if err := unix.Connect(fd, address); !errors.Is(err, unix.EISCONN) {
			t.Fatalf("connected endpoint redirected: %v", err)
		}
		// Linux ignores the supplied destination on a connected seqpacket
		// send: the data must still arrive at the original private peer.
		message := []byte("fixed peer")
		if err := unix.Sendto(fd, message, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL, address); err != nil {
			t.Fatal(err)
		}
		var data [32]byte
		n, _, err := unix.Recvfrom(peer, data[:], unix.MSG_DONTWAIT)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data[:n], message) {
			t.Fatalf("sendto changed destination: received=%q err=%v", data[:n], err)
		}
		// AF_UNSPEC disconnect can detach a datagram socket. It must not
		// detach our endpoint and permit a subsequent reconnect elsewhere.
		unspecified := unix.RawSockaddr{Family: unix.AF_UNSPEC}
		_, _, errno := unix.Syscall(unix.SYS_CONNECT, uintptr(fd),
			uintptr(unsafe.Pointer(&unspecified)), unsafe.Sizeof(unspecified))
		if errno == 0 {
			t.Fatal("seqpacket endpoint accepted AF_UNSPEC disconnect")
		}
		if err := unix.Connect(fd, address); !errors.Is(err, unix.EISCONN) {
			t.Fatalf("endpoint redirected after AF_UNSPEC: %v", err)
		}
	}
	if err := unix.Shutdown(child, unix.SHUT_RDWR); err != nil {
		t.Fatal(err)
	}
	for _, fd := range []int{child, duplicate} {
		if err := unix.Connect(fd, address); !errors.Is(err, unix.EISCONN) {
			t.Fatalf("endpoint redirected after shutdown: %v", err)
		}
		err := unix.Sendto(fd, []byte("blocked"), unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL, address)
		if !errors.Is(err, unix.EPIPE) {
			t.Fatalf("shut down endpoint sent to another service: %v", err)
		}
	}
	accepted, _, err := unix.Accept4(target, unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC)
	if accepted >= 0 {
		_ = unix.Close(accepted)
	}
	if !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("unrelated Unix service received a connection: %v", err)
	}
}
