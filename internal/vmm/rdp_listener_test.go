package vmm

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func reservedRDPFile(t *testing.T) (*os.File, string) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	file, err := listener.File()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	return file, listener.Addr().String()
}

func assertRDPPortReserved(t *testing.T, address string) {
	t.Helper()
	listener, err := net.Listen("tcp", address)
	if listener != nil {
		_ = listener.Close()
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("RDP port was not reserved: %v", err)
	}
}

func assertRDPPortReleased(t *testing.T, address string) {
	t.Helper()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("RDP listener leaked: %v", err)
	}
	_ = listener.Close()
}

func TestInheritedRDPListenerSurvivesDisplayRestart(t *testing.T) {
	t.Parallel()
	file, address := reservedRDPFile(t)
	conf := Config{RDP: address, RDPListenerFile: file}
	for range 2 {
		v := New(conf)
		display, err := v.display(nil)
		if err != nil {
			t.Fatal(err)
		}
		if v.rdpDisplay.Addr() != address {
			t.Fatalf("inherited address = %q, want %q", v.rdpDisplay.Addr(), address)
		}
		assertRDPPortReserved(t, address)
		if err := display.Close(); err != nil {
			t.Fatal(err)
		}
		// The caller's original descriptor must keep the port reserved between
		// VM instances when firmware requests a reset.
		assertRDPPortReserved(t, address)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	assertRDPPortReleased(t, address)
}

func TestInheritedRDPListenerInitializationFailureDoesNotLeak(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"encoder", "certificate", "mismatched address"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			file, address := reservedRDPFile(t)
			conf := Config{RDP: address, RDPListenerFile: file}
			switch failure {
			case "encoder":
				conf.RDPH264, conf.RDPH264Encoder = true, "invalid"
			case "certificate":
				conf.RDPCert, conf.RDPKey = "missing.crt", "missing.key"
			case "mismatched address":
				conf.RDP = "127.0.0.1:0"
			}
			if display, err := New(conf).display(nil); err == nil {
				_ = display.Close()
				t.Fatal("invalid display settings accepted")
			}
			assertRDPPortReserved(t, address)
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			assertRDPPortReleased(t, address)
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
	v := New(Config{RDP: "127.0.0.1:0", RDPListenerFile: file})
	if display, err := v.display(nil); err == nil {
		_ = display.Close()
		t.Fatal("invalid inherited listener fell back to opening a new port")
	} else if !strings.Contains(err.Error(), "adopt inherited RDP listener") {
		t.Fatalf("missing descriptor error context: %v", err)
	}
}

func TestInheritedRDPListenerRequiresAddressBeforeMachineInit(t *testing.T) {
	t.Parallel()
	file, _ := reservedRDPFile(t)
	v := New(Config{Dev: "/nonexistent-kvm", RDPListenerFile: file})
	if err := v.Init(); !errors.Is(err, errRDPListenerConfig) {
		t.Fatalf("inherited listener without address reached machine initialization: %v", err)
	}
}

func TestInheritedRDPListenerRejectsConnectedSocket(t *testing.T) {
	t.Parallel()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address, err := net.ResolveTCPAddr("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTCP("tcp4", nil, address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	file, err := conn.File()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	v := New(Config{RDP: conn.LocalAddr().String(), RDPListenerFile: file})
	adopted, err := v.inheritedRDPListener()
	if adopted != nil {
		_ = adopted.Close()
	}
	if !errors.Is(err, errRDPListenerConfig) {
		t.Fatalf("connected socket accepted as an RDP listener: %v", err)
	}
}
