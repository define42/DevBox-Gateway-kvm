//nolint:err113 // The test's minimal RFB client returns contextual protocol errors.
package vmm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestTinyCoreVNCDesktopBoot is an end-to-end test: it boots TinyCore from the
// ISO with a VNC display, connects a minimal RFB client, waits for the flwm
// desktop to render, then moves the pointer and opens the desktop menu to
// confirm that guest input and the desktop session work together.
//
// It is heavy (a full guest boot) and needs /dev/kvm, so it skips unless those
// preconditions are met, matching the other KVM integration tests in this repo.
func TestTinyCoreVNCDesktopBoot(t *testing.T) { //nolint:paralleltest
	if testing.Short() {
		t.Skip("skipping TinyCore VNC boot test in short mode")
	}

	if os.Getuid() != 0 {
		t.Skip("skipping test since we are not root")
	}

	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skipf("skipping test since /dev/kvm is unavailable: %v", err)
	}

	const isoPath = "../../TinyCore-current.iso"
	if _, err := os.Stat(isoPath); err != nil {
		t.Skipf("skipping test since %s is missing: %v", isoPath, err)
	}

	// `make test` runs under `unshare --net`, where loopback starts down; VNC
	// binds/listens in Init(), so bring loopback up before creating the display.
	// On a real host it is already up and this is a harmless no-op.
	_ = exec.Command("ip", "link", "set", "lo", "up").Run()

	v := New(Config{
		Dev:     "/dev/kvm",
		ISO:     isoPath,
		VNC:     "127.0.0.1:0", // ephemeral port, read back from the listener.
		NCPUs:   1,
		MemSize: 512 << 20,
	})

	if err := v.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	t.Cleanup(func() { _ = v.Close() })

	if err := v.Setup(); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if err := v.InjectSerialIRQ(); err != nil {
		t.Logf("InjectSerialIRQ: %v", err)
	}

	for cpu := 0; cpu < v.NCPUs; cpu++ {
		c := cpu
		go func() {
			if err := v.RunInfiniteLoop(c); err != nil {
				t.Logf("RunInfiniteLoop(%d): %v", c, err)
			}
		}()
	}

	addr := v.vncDisplay.Addr()
	t.Logf("VNC listening on %s", addr)

	client, err := dialRFB(addr)
	if err != nil {
		t.Fatalf("dial VNC: %v", err)
	}
	defer func() { _ = client.Close() }()

	// 1. The guest boots and the desktop shows: wait for a full 1024x768
	//    framebuffer with real graphical content (not the blank default or the
	//    smaller text-console fallback).
	bootDeadline := time.Now().Add(testTimeBudget(t, 4*time.Minute))
	for {
		if time.Now().After(bootDeadline) {
			t.Fatalf("desktop did not render within deadline (last frame %dx%d, %d colors)",
				client.fbW, client.fbH, client.distinctColors())
		}

		if err := client.refresh(); err != nil {
			t.Fatalf("framebuffer update: %v", err)
		}

		if client.fbW >= 1024 && client.fbH >= 768 && client.distinctColors() >= 8 {
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Logf("desktop rendered: %dx%d with %d distinct colors",
		client.fbW, client.fbH, client.distinctColors())

	// 2. A single absolute move must place the guest cursor at the requested
	//    coordinates. Check a clean area away from the desktop logo and dock.
	//
	//    Xvesa opens the mouse a little after it first paints, so retry to absorb
	//    that startup race instead of relying on a fixed sleep.
	const region = 48
	moveDeadline := time.Now().Add(testTimeBudget(t, 45*time.Second))
	moved := false

	for !moved && time.Now().Before(moveDeadline) {
		if err := client.pointer(0, 700, 500); err != nil {
			t.Fatalf("move pointer away: %v", err)
		}
		time.Sleep(400 * time.Millisecond)
		_ = client.refresh()
		away := client.regionSnapshot(190, 140, region, region)

		if err := client.pointer(0, 200, 150); err != nil {
			t.Fatalf("move pointer to target: %v", err)
		}
		time.Sleep(400 * time.Millisecond)
		_ = client.refresh()
		onTarget := client.regionSnapshot(190, 140, region, region)

		appeared := diffPixels(away, onTarget, client.bpp)
		if appeared < 10 {
			continue // cursor not at the target yet; Xvesa still starting.
		}

		if err := client.pointer(0, 700, 500); err != nil {
			t.Fatalf("move pointer away again: %v", err)
		}
		time.Sleep(400 * time.Millisecond)
		_ = client.refresh()
		left := client.regionSnapshot(190, 140, region, region)

		departed := diffPixels(onTarget, left, client.bpp)
		t.Logf("cursor at (200, 150) changed %d px, leaving changed %d px", appeared, departed)

		if departed >= 10 {
			moved = true
		}
	}

	if !moved {
		t.Fatal("mouse cursor did not reach (200, 150) with a single absolute move")
	}

	// 3. A right-click must open the FLWM desktop menu, and clicking outside it
	//    must dismiss it. A wallpaper and cursor alone do not establish that
	//    the window manager is usable.
	testDesktopMenu(t, client)
}

func testDesktopMenu(t *testing.T, client *rfbClient) {
	t.Helper()

	const x, y, width, height = 200, 150, 320, 400
	background := client.regionSnapshot(x, y, width, height)
	if err := client.pointer(4, x, y); err != nil {
		t.Fatalf("press desktop menu button: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if err := client.refresh(); err != nil {
		t.Fatalf("refresh desktop menu: %v", err)
	}
	menu := client.regionSnapshot(x, y, width, height)
	if changed := diffPixels(background, menu, client.bpp); changed < 2000 {
		t.Fatalf("desktop menu did not open: only %d pixels changed", changed)
	}

	// SystemTools is generated by TinyCore's setupdesktop. A window manager
	// started without that preparation still has an otherwise empty menu.
	const submenuX, submenuY, submenuWidth, submenuHeight = 314, 170, 200, 330
	beforeSubmenu := client.regionSnapshot(submenuX, submenuY, submenuWidth, submenuHeight)
	if err := client.pointer(4, 260, 178); err != nil {
		t.Fatalf("point to SystemTools: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if err := client.refresh(); err != nil {
		t.Fatalf("refresh SystemTools submenu: %v", err)
	}
	submenu := client.regionSnapshot(submenuX, submenuY, submenuWidth, submenuHeight)
	if changed := diffPixels(beforeSubmenu, submenu, client.bpp); changed < 2000 {
		t.Fatalf("SystemTools submenu is missing: only %d pixels changed", changed)
	}

	if err := client.pointer(0, 800, 100); err != nil {
		t.Fatalf("release desktop menu button outside: %v", err)
	}
	if err := client.pointer(1, 800, 100); err != nil {
		t.Fatalf("click outside desktop menu: %v", err)
	}
	if err := client.pointer(0, 800, 100); err != nil {
		t.Fatalf("release outside desktop menu: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if err := client.refresh(); err != nil {
		t.Fatalf("refresh dismissed menu: %v", err)
	}
	dismissed := client.regionSnapshot(x, y, width, height)
	if changed := diffPixels(background, dismissed, client.bpp); changed > 100 {
		t.Fatalf("desktop menu did not close: %d pixels remain changed", changed)
	}
}

// testTimeBudget returns d, shrunk to fit before the test deadline (with a
// margin) when one is set, so the test fails with a useful message instead of
// the bare `go test` timeout.
func testTimeBudget(t *testing.T, d time.Duration) time.Duration {
	t.Helper()

	dl, ok := t.Deadline()
	if !ok {
		return d
	}

	if budget := time.Until(dl) - 30*time.Second; budget < d {
		return budget
	}

	return d
}

// rfbClient is a minimal RFB/VNC client: enough of the protocol to pull raw
// framebuffer updates and inject pointer events.
type rfbClient struct {
	conn net.Conn
	bpp  int // bytes per pixel, from the server pixel format.

	fbW int
	fbH int
	fb  []byte
}

func dialRFB(addr string) (*rfbClient, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}

	c := &rfbClient{conn: conn}
	if err := c.handshake(); err != nil {
		_ = conn.Close()

		return nil, err
	}

	// Raw is the only encoding we decode; the cursor is rendered into the
	// framebuffer by the guest, so we do not need the cursor pseudo-encoding.
	if err := c.setEncodings(0); err != nil {
		_ = conn.Close()

		return nil, err
	}

	return c, nil
}

func (c *rfbClient) Close() error { return c.conn.Close() }

func (c *rfbClient) handshake() error {
	_ = c.conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer func() { _ = c.conn.SetDeadline(time.Time{}) }()

	var serverVersion [12]byte
	if _, err := io.ReadFull(c.conn, serverVersion[:]); err != nil {
		return fmt.Errorf("read protocol version: %w", err)
	}

	if _, err := c.conn.Write([]byte("RFB 003.008\n")); err != nil {
		return fmt.Errorf("write protocol version: %w", err)
	}

	var nSec [1]byte
	if _, err := io.ReadFull(c.conn, nSec[:]); err != nil {
		return fmt.Errorf("read security count: %w", err)
	}

	if nSec[0] == 0 {
		return fmt.Errorf("server offered no security types")
	}

	secTypes := make([]byte, nSec[0])
	if _, err := io.ReadFull(c.conn, secTypes); err != nil {
		return fmt.Errorf("read security types: %w", err)
	}

	if !bytes.Contains(secTypes, []byte{1}) {
		return fmt.Errorf("server does not offer None security: %v", secTypes)
	}

	if _, err := c.conn.Write([]byte{1}); err != nil { // None
		return fmt.Errorf("select security: %w", err)
	}

	var secResult [4]byte
	if _, err := io.ReadFull(c.conn, secResult[:]); err != nil {
		return fmt.Errorf("read security result: %w", err)
	}

	if _, err := c.conn.Write([]byte{1}); err != nil { // ClientInit: shared
		return fmt.Errorf("client init: %w", err)
	}

	var serverInit [24]byte
	if _, err := io.ReadFull(c.conn, serverInit[:]); err != nil {
		return fmt.Errorf("read server init: %w", err)
	}

	c.bpp = int(serverInit[4]) / 8
	if c.bpp <= 0 {
		return fmt.Errorf("invalid bits-per-pixel %d", serverInit[4])
	}

	nameLen := binary.BigEndian.Uint32(serverInit[20:24])
	if nameLen > 0 {
		if _, err := io.CopyN(io.Discard, c.conn, int64(nameLen)); err != nil {
			return fmt.Errorf("read server name: %w", err)
		}
	}

	return nil
}

func (c *rfbClient) setEncodings(encodings ...int32) error {
	buf := make([]byte, 4+4*len(encodings))
	buf[0] = 2 // SetEncodings
	binary.BigEndian.PutUint16(buf[2:4], uint16(len(encodings)))
	for i, enc := range encodings {
		binary.BigEndian.PutUint32(buf[4+i*4:], uint32(enc))
	}

	_, err := c.conn.Write(buf)

	return err
}

// refresh requests a full framebuffer update and applies it. The gokvm server
// answers a full (non-incremental) request immediately with the current frame,
// so this never blocks waiting on guest activity.
func (c *rfbClient) refresh() error {
	if err := c.requestUpdate(false); err != nil {
		return err
	}

	return c.readUpdate()
}

func (c *rfbClient) requestUpdate(incremental bool) error {
	buf := make([]byte, 10)
	buf[0] = 3 // FramebufferUpdateRequest
	if incremental {
		buf[1] = 1
	}
	// x=0, y=0, width/height = max; the server clamps to the current frame.
	binary.BigEndian.PutUint16(buf[6:8], 0xffff)
	binary.BigEndian.PutUint16(buf[8:10], 0xffff)

	_, err := c.conn.Write(buf)

	return err
}

func (c *rfbClient) readUpdate() error {
	_ = c.conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	defer func() { _ = c.conn.SetReadDeadline(time.Time{}) }()

	var hdr [4]byte
	if _, err := io.ReadFull(c.conn, hdr[:]); err != nil {
		return fmt.Errorf("read update header: %w", err)
	}

	if hdr[0] != 0 {
		return fmt.Errorf("unexpected server message %d", hdr[0])
	}

	nRects := binary.BigEndian.Uint16(hdr[2:4])
	for i := 0; i < int(nRects); i++ {
		if err := c.readRect(); err != nil {
			return err
		}
	}

	return nil
}

func (c *rfbClient) readRect() error {
	var rh [12]byte
	if _, err := io.ReadFull(c.conn, rh[:]); err != nil {
		return fmt.Errorf("read rect header: %w", err)
	}

	x := int(binary.BigEndian.Uint16(rh[0:2]))
	y := int(binary.BigEndian.Uint16(rh[2:4]))
	w := int(binary.BigEndian.Uint16(rh[4:6]))
	h := int(binary.BigEndian.Uint16(rh[6:8]))
	enc := int32(binary.BigEndian.Uint32(rh[8:12]))

	if enc != 0 {
		return fmt.Errorf("unsupported encoding %d", enc)
	}

	raw := make([]byte, w*h*c.bpp)
	if _, err := io.ReadFull(c.conn, raw); err != nil {
		return fmt.Errorf("read raw pixels: %w", err)
	}

	c.applyRect(x, y, w, h, raw)

	return nil
}

func (c *rfbClient) applyRect(x, y, w, h int, raw []byte) {
	// Full-frame updates start at the origin; resize our buffer to match so
	// snapshots taken before and after a resize are not compared.
	if x == 0 && y == 0 && (w != c.fbW || h != c.fbH) {
		c.fbW = w
		c.fbH = h
		c.fb = make([]byte, w*h*c.bpp)
	}

	for row := 0; row < h; row++ {
		dst := ((y+row)*c.fbW + x) * c.bpp
		src := row * w * c.bpp
		if dst < 0 || dst+w*c.bpp > len(c.fb) {
			continue
		}
		copy(c.fb[dst:dst+w*c.bpp], raw[src:src+w*c.bpp])
	}
}

func (c *rfbClient) distinctColors() int {
	seen := make(map[uint32]struct{})
	for off := 0; off+4 <= len(c.fb); off += c.bpp {
		seen[binary.LittleEndian.Uint32(c.fb[off:off+4])] = struct{}{}
		if len(seen) >= 256 {
			break
		}
	}

	return len(seen)
}

// regionSnapshot returns a copy of the w x h framebuffer block at (x,y).
func (c *rfbClient) regionSnapshot(x, y, w, h int) []byte {
	out := make([]byte, 0, w*h*c.bpp)
	for row := 0; row < h; row++ {
		off := ((y+row)*c.fbW + x) * c.bpp
		if off < 0 || off+w*c.bpp > len(c.fb) {
			continue
		}
		out = append(out, c.fb[off:off+w*c.bpp]...)
	}

	return out
}

func (c *rfbClient) pointer(buttonMask uint8, x, y uint16) error {
	buf := make([]byte, 6)
	buf[0] = 5 // PointerEvent
	buf[1] = buttonMask
	binary.BigEndian.PutUint16(buf[2:4], x)
	binary.BigEndian.PutUint16(buf[4:6], y)

	_, err := c.conn.Write(buf)

	return err
}

func diffPixels(a, b []byte, bpp int) int {
	if len(a) != len(b) || bpp <= 0 {
		return 0
	}

	diff := 0
	for off := 0; off+bpp <= len(a); off += bpp {
		if !bytes.Equal(a[off:off+bpp], b[off:off+bpp]) {
			diff++
		}
	}

	return diff
}
