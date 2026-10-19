//go:build integration

package vmm

import (
	"context"
	"errors"
	"image"
	"image/draw"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

// TestNetDeskUKIDesktopBoot is opt-in because the external image and guest need
// several GiB of memory. It verifies NetDesk's own desktop-ready check, rendered
// graphics, pointer input, and keyboard-driven Chromium networking using an
// in-memory display and the guest's virtio input devices.
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
		Network: "user",
	})
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	desktop := &netDeskDesktop{input: v.AddVirtioInput()}
	if err := v.AddGPUDisplay(desktop); err != nil {
		t.Fatal(err)
	}
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

	waitNetDeskPaint(t, desktop)

	testNetDeskPointer(t, desktop)
	testNetDeskBrowser(t, desktop, done)
	t.Log("NetDesk UKI boot, XFCE desktop, virtio input, DHCP and Chromium networking passed")
}

func waitNetDeskPaint(t *testing.T, desktop *netDeskDesktop) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		frame := desktop.snapshot()
		colored := 0
		for i := 0; i+2 < len(frame.Pix); i += 4 {
			if frame.Pix[i] > 25 || frame.Pix[i+1] > 25 || frame.Pix[i+2] > 25 {
				colored++
			}
		}
		// Readiness names the desktop processes, but their first repaint may
		// still be pending. Wait for wallpaper and launchers, not a black frame.
		width, height := frame.Bounds().Dx(), frame.Bounds().Dy()
		if width >= 800 && height >= 600 && colored > width*height/2 {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("desktop did not finish painting")
}

func testNetDeskPointer(t *testing.T, desktop *netDeskDesktop) {
	t.Helper()
	// Right-clicking open desktop space must open an XFCE menu.
	before := desktop.snapshot()
	for _, mask := range []uint8{4, 0} {
		desktop.pointer(mask, 600, 400)
	}
	time.Sleep(500 * time.Millisecond)
	if changedNetDeskPixels(before, desktop.snapshot()) < 1000 {
		t.Fatal("pointer input did not open the desktop menu")
	}
	desktop.input.KeyEvent(true, 0xff1b) // Escape
	desktop.input.KeyEvent(false, 0xff1b)
}

func testNetDeskBrowser(t *testing.T, desktop *netDeskDesktop, done chan error) {
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
		desktop.pointer(mask, 154, 18)
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

			return
		case err := <-done:
			done <- err
			t.Fatalf("guest stopped while starting Chromium: %v", err)
		case <-deadline.C:
			t.Fatal("Chromium did not reach the host server through the guest network")
		case <-tick.C:
			visitNetDeskURL(desktop, url)
		}
	}
}

func visitNetDeskURL(desktop *netDeskDesktop, url string) {
	for _, key := range []struct {
		code uint32
		down bool
	}{{code: 0xffe3, down: true}, {code: 'l', down: true}, {code: 'l'}, {code: 0xffe3}} {
		desktop.input.KeyEvent(key.down, key.code)
	}
	// Chromium processes the focus shortcut asynchronously from evdev input.
	time.Sleep(150 * time.Millisecond)
	desktop.typeText(url)
	for _, down := range []bool{true, false} {
		desktop.input.KeyEvent(down, 0xff0d)
	}
}

type netDeskDesktop struct {
	mu    sync.Mutex
	frame *image.RGBA
	input virtio.DisplayInput
}

func (d *netDeskDesktop) Flush(width, height int, frame *image.RGBA) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.frame == nil || d.frame.Bounds() != image.Rect(0, 0, width, height) {
		d.frame = image.NewRGBA(image.Rect(0, 0, width, height))
	}
	draw.Draw(d.frame, d.frame.Bounds(), frame, frame.Bounds().Min, draw.Src)

	return nil
}

func (d *netDeskDesktop) Close() error { return nil }

func (d *netDeskDesktop) snapshot() *image.RGBA {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.frame == nil {
		return image.NewRGBA(image.Rectangle{})
	}
	frame := image.NewRGBA(d.frame.Bounds())
	draw.Draw(frame, frame.Bounds(), d.frame, d.frame.Bounds().Min, draw.Src)

	return frame
}

func (d *netDeskDesktop) pointer(buttons uint8, x, y uint16) {
	if bounded, ok := d.input.(interface {
		PointerEventInBounds(uint8, uint16, uint16, int, int)
	}); ok {
		bounds := d.snapshot().Bounds()
		bounded.PointerEventInBounds(buttons, x, y, bounds.Dx(), bounds.Dy())

		return
	}
	d.input.PointerEvent(buttons, x, y)
}

func (d *netDeskDesktop) typeText(text string) {
	for _, char := range text {
		shift := char == ':'
		if shift {
			d.input.KeyEvent(true, 0xffe1)
		}
		for _, down := range []bool{true, false} {
			d.input.KeyEvent(down, uint32(char))
		}
		if shift {
			d.input.KeyEvent(false, 0xffe1)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func changedNetDeskPixels(before, after *image.RGBA) int {
	if before.Bounds() != after.Bounds() {
		return 0
	}
	changed := 0
	for y := before.Bounds().Min.Y; y < before.Bounds().Max.Y; y++ {
		for x := before.Bounds().Min.X; x < before.Bounds().Max.X; x++ {
			if before.RGBAAt(x, y) != after.RGBAAt(x, y) {
				changed++
			}
		}
	}

	return changed
}
