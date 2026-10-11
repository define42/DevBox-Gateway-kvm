//go:build integration

package vmm

import (
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNetDeskUKIDesktopBoot is opt-in because the external image and guest need
// several GiB of memory. It verifies NetDesk's own desktop-ready check, rendered
// graphics, pointer input, and keyboard-driven Chromium networking over VNC.
// A 6 GiB guest must not run alongside other heavy boots.
func TestNetDeskUKIDesktopBoot(t *testing.T) { //nolint:paralleltest
	path := os.Getenv("GOKVM_NETDESK_UKI")
	if testing.Short() || path == "" {
		t.Skip("set GOKVM_NETDESK_UKI to a NetDesk UKI and omit -short")
	}
	kvm, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("KVM unavailable: %v", err)
	}
	_ = kvm.Close()

	artifacts := t.ArtifactDir()
	serialPath := filepath.Join(artifacts, "serial.log")
	serialLog, err := os.Create(serialPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serialLog.Close() })

	v := New(Config{
		Dev: "/dev/kvm", UKI: path, NCPUs: 2, MemSize: 6 << 30,
		Network: "user", VNC: "127.0.0.1:0",
	})
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	if err := v.Setup(); err != nil {
		t.Fatal(err)
	}
	v.GetSerial().SetOutput(serialLog)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	done := make(chan error, 1)
	go func() { done <- v.BootContext(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("guest stopped: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("guest did not stop after cancellation")
		}
	})

	t.Logf("NetDesk serial log: %s", serialPath)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		serial, err := os.ReadFile(serialPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(serial), "NetDesk desktop ready") {
			break
		}
		select {
		case <-ticker.C:
		case err := <-done:
			done <- err
			t.Fatalf("guest stopped before desktop readiness: %v\n%s", err, serial)
		case <-ctx.Done():
			t.Fatalf("desktop did not become ready: %v\n%s", ctx.Err(), serial)
		}
	}

	client, err := dialRFB(v.vncDisplay.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	waitNetDeskPaint(t, client)
	saveNetDeskFrame(t, client, filepath.Join(artifacts, "desktop.png"))

	testNetDeskPointer(t, client)
	testNetDeskBrowser(t, client, done, artifacts)
	t.Log("NetDesk UKI boot, XFCE desktop, VNC input, DHCP and Chromium networking passed")
}

func waitNetDeskPaint(t *testing.T, client *rfbClient) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := client.refresh(); err != nil {
			t.Fatal(err)
		}
		colored := 0
		for i := 0; i+2 < len(client.fb); i += client.bpp {
			if client.fb[i] > 25 || client.fb[i+1] > 25 || client.fb[i+2] > 25 {
				colored++
			}
		}
		// Readiness names the desktop processes, but their first repaint may
		// still be pending. Wait for wallpaper and launchers, not a black frame.
		if client.fbW >= 800 && client.fbH >= 600 && colored > client.fbW*client.fbH/2 {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("desktop did not finish painting")
}

func testNetDeskPointer(t *testing.T, client *rfbClient) {
	t.Helper()
	// Right-clicking open desktop space must open an XFCE menu.
	before := append([]byte(nil), client.fb...)
	for _, mask := range []uint8{4, 0} {
		if err := client.pointer(mask, 600, 400); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(500 * time.Millisecond)
	if err := client.refresh(); err != nil {
		t.Fatal(err)
	}
	if diffPixels(before, client.fb, client.bpp) < 1000 {
		t.Fatal("pointer input did not open the desktop menu")
	}
	if err := client.ukiKey(0xff1b, true); err != nil { // Escape
		t.Fatal(err)
	}
	if err := client.ukiKey(0xff1b, false); err != nil {
		t.Fatal(err)
	}
}

func testNetDeskBrowser(t *testing.T, client *rfbClient, done chan error, artifacts string) {
	t.Helper()

	requested := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gokvm-netdesk-smoke" {
			select {
			case requested <- r.UserAgent():
			default:
			}
		}
		_, _ = io.WriteString(w, "<!doctype html><title>gokvm NetDesk smoke test</title><h1>NetDesk networking works</h1>")
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	// Launch Chromium from NetDesk's top panel. Retry entering the URL while
	// its first window starts, rather than depending on a desktop shortcut.
	for _, mask := range []uint8{1, 0} {
		if err := client.pointer(mask, 154, 18); err != nil {
			t.Fatal(err)
		}
	}
	url := "http://10.0.2.2:" + port + "/gokvm-netdesk-smoke"
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case ua := <-requested:
			if !strings.Contains(ua, "Chrome/") {
				t.Fatalf("request did not come from Chromium: %q", ua)
			}
			t.Logf("Chromium reached host HTTP server: %s", ua)
			time.Sleep(500 * time.Millisecond)
			if err := client.refresh(); err != nil {
				t.Fatal(err)
			}
			saveNetDeskFrame(t, client, filepath.Join(artifacts, "browser.png"))

			return
		case err := <-done:
			done <- err
			t.Fatalf("guest stopped while starting Chromium: %v", err)
		case <-deadline.C:
			_ = client.refresh()
			saveNetDeskFrame(t, client, filepath.Join(artifacts, "browser-failure.png"))
			t.Fatal("Chromium did not reach the host server through the guest network")
		case <-tick.C:
			visitNetDeskURL(t, client, url)
		}
	}
}

func visitNetDeskURL(t *testing.T, client *rfbClient, url string) {
	t.Helper()
	for _, key := range []struct {
		code uint32
		down bool
	}{{code: 0xffe3, down: true}, {code: 'l', down: true}, {code: 'l'}, {code: 0xffe3}} {
		if err := client.ukiKey(key.code, key.down); err != nil {
			t.Fatal(err)
		}
	}
	// Chromium processes the focus shortcut asynchronously from evdev input.
	time.Sleep(150 * time.Millisecond)
	if err := client.ukiType(url); err != nil {
		t.Fatal(err)
	}
	for _, down := range []bool{true, false} {
		if err := client.ukiKey(0xff0d, down); err != nil {
			t.Fatal(err)
		}
	}
}

func (c *rfbClient) ukiKey(key uint32, down bool) error {
	var data [8]byte
	data[0] = 4
	if down {
		data[1] = 1
	}
	binary.BigEndian.PutUint32(data[4:], key)
	_, err := c.conn.Write(data[:])

	return err
}

func (c *rfbClient) ukiType(text string) error {
	for _, char := range text {
		shift := char == ':'
		if shift {
			if err := c.ukiKey(0xffe1, true); err != nil {
				return err
			}
		}
		for _, down := range []bool{true, false} {
			if err := c.ukiKey(uint32(char), down); err != nil {
				return err
			}
		}
		if shift {
			if err := c.ukiKey(0xffe1, false); err != nil {
				return err
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	return nil
}

func saveNetDeskFrame(t *testing.T, client *rfbClient, path string) {
	t.Helper()
	if client.bpp != 4 {
		t.Fatalf("unexpected VNC pixel width: %d", client.bpp)
	}
	frame := image.NewRGBA(image.Rect(0, 0, client.fbW, client.fbH))
	for i := 0; i < len(client.fb); i += 4 {
		frame.Pix[i] = client.fb[i+2]
		frame.Pix[i+1] = client.fb[i+1]
		frame.Pix[i+2] = client.fb[i]
		frame.Pix[i+3] = 255
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, frame); err != nil {
		t.Fatal(err)
	}
}
