package vmm

import (
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func reservedRDPFile(tb testing.TB) (*os.File, string) {
	tb.Helper()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: "@gokvm-test-" + rand.Text(), Net: "unix"})
	if err != nil {
		tb.Fatal(err)
	}
	defer listener.Close()
	file, err := listener.File()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = file.Close() })

	return file, listener.Addr().String()
}

func gatewayRDPConfig(tb testing.TB) Config {
	tb.Helper()

	return withGatewayRDP(tb, Config{})
}

func withGatewayRDP(tb testing.TB, config Config) Config {
	tb.Helper()
	file, _ := reservedRDPFile(tb)
	config.RDPListenerFile = file
	config.RDPGatewayPID = os.Getppid()
	config.RDPGatewayUID = uint32(os.Geteuid())

	return config
}

func assertRDPSocketReserved(tb testing.TB, address string) {
	tb.Helper()
	listener, err := net.Listen("unix", address)
	if listener != nil {
		_ = listener.Close()
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		tb.Fatalf("RDP socket was not reserved: %v", err)
	}
}

func assertRDPSocketReleased(tb testing.TB, address string) {
	tb.Helper()
	listener, err := net.Listen("unix", address)
	if err != nil {
		tb.Fatalf("RDP listener leaked: %v", err)
	}
	_ = listener.Close()
}

func TestInheritedRDPListenerSurvivesDisplayRestart(t *testing.T) {
	t.Parallel()
	conf := gatewayRDPConfig(t)
	var address string
	for range 2 {
		v := New(conf)
		display, err := v.display(nil)
		if err != nil {
			t.Fatal(err)
		}
		if address != "" && v.rdpDisplay.Addr() != address {
			t.Fatalf("inherited address changed: %q, want %q", v.rdpDisplay.Addr(), address)
		}
		address = v.rdpDisplay.Addr()
		assertRDPSocketReserved(t, address)
		if err := display.Close(); err != nil {
			t.Fatal(err)
		}
		// The caller retains the original socket between firmware boots.
		assertRDPSocketReserved(t, address)
	}
	if err := conf.RDPListenerFile.Close(); err != nil {
		t.Fatal(err)
	}
	assertRDPSocketReleased(t, address)
}

func TestInheritedRDPListenerInitializationFailureDoesNotLeak(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"encoder", "certificate", "missing gateway identity"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			file, address := reservedRDPFile(t)
			conf := Config{RDPListenerFile: file, RDPGatewayPID: os.Getppid(), RDPGatewayUID: uint32(os.Geteuid())}
			switch failure {
			case "encoder":
				conf.RDPH264, conf.RDPH264Encoder = true, "invalid"
			case "certificate":
				conf.RDPCert, conf.RDPKey = "missing.crt", "missing.key"
			case "missing gateway identity":
				conf.RDPGatewayPID = 0
			}
			if display, err := New(conf).display(nil); err == nil {
				_ = display.Close()
				t.Fatal("invalid display settings accepted")
			}
			assertRDPSocketReserved(t, address)
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			assertRDPSocketReleased(t, address)
		})
	}
}

func TestInheritedRDPListenerRejectsNonSocket(t *testing.T) {
	t.Parallel()
	file, err := os.Create(filepath.Join(t.TempDir(), "regular-file"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	v := New(Config{RDPListenerFile: file, RDPGatewayPID: os.Getppid()})
	if display, err := v.display(nil); err == nil {
		_ = display.Close()
		t.Fatal("invalid inherited listener fell back to opening a new port")
	} else if !strings.Contains(err.Error(), "adopt inherited RDP listener") {
		t.Fatalf("missing descriptor error context: %v", err)
	}
}

func TestInheritedRDPListenerRequiresGatewayBeforeMachineInit(t *testing.T) {
	t.Parallel()
	file, _ := reservedRDPFile(t)
	v := New(Config{Dev: "/nonexistent-kvm", RDPListenerFile: file})
	if err := v.Init(); !errors.Is(err, errRDPListenerConfig) {
		t.Fatalf("inherited listener without gateway identity reached machine initialization: %v", err)
	}
}

func TestInheritedRDPListenerRejectsTCPAndPathname(t *testing.T) {
	t.Parallel()
	for _, network := range []string{"tcp", "unix", "unixpacket"} {
		t.Run(network, func(t *testing.T) {
			t.Parallel()
			address := "127.0.0.1:0"
			if network == "unix" {
				address = filepath.Join(t.TempDir(), "rdp.sock")
			}
			if network == "unixpacket" {
				address = "@gokvm-test-" + rand.Text()
			}
			listener, err := net.Listen(network, address)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			var file *os.File
			switch listener := listener.(type) {
			case *net.TCPListener:
				file, err = listener.File()
			case *net.UnixListener:
				file, err = listener.File()
			}
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			adopted, err := New(Config{RDPListenerFile: file}).inheritedRDPListener()
			if adopted != nil {
				_ = adopted.Close()
			}
			if !errors.Is(err, errRDPListenerConfig) {
				t.Fatalf("unsafe inherited listener accepted: %v", err)
			}
		})
	}
}

func TestInheritedRDPListenerRejectsConnectedSocket(t *testing.T) {
	t.Parallel()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: "@gokvm-test-" + rand.Text(), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	conn, err := net.DialUnix("unix", nil, listener.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	file, err := conn.File()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	adopted, err := New(Config{RDPListenerFile: file}).inheritedRDPListener()
	if adopted != nil {
		_ = adopted.Close()
	}
	if !errors.Is(err, errRDPListenerConfig) {
		t.Fatalf("connected socket accepted as an RDP listener: %v", err)
	}
}
