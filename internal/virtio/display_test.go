package virtio_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

// TestPNGDisplayFlush checks the default backend writes a decodable PNG whose
// pixels round-trip.
func TestPNGDisplayFlush(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "fb.png")

	d := virtio.NewPNGDisplay(path)
	defer d.Close()

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3] = 0x10, 0x20, 0x30, 0xff

	if err := d.Flush(2, 2, img); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer f.Close()

	got, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}

	if b := got.Bounds(); b.Dx() != 2 || b.Dy() != 2 {
		t.Fatalf("decoded dims: got %dx%d want 2x2", b.Dx(), b.Dy())
	}

	// RGBA() returns 16-bit channels; compare the high byte.
	r, g, b, a := got.At(0, 0).RGBA()
	if uint8(r>>8) != 0x10 || uint8(g>>8) != 0x20 || uint8(b>>8) != 0x30 || uint8(a>>8) != 0xff {
		t.Fatalf("pixel(0,0): got (%d,%d,%d,%d) want (16,32,48,255)", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestVNCDisplayHandshakeAndRawFramebuffer(t *testing.T) {
	t.Parallel()

	d, err := virtio.NewVNCDisplay("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	input := &mockVNCInput{events: make(chan vncInputEvent, 3)}
	d.SetInput(input)

	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3] = 0xff, 0, 0, 0xff
	img.Pix[4], img.Pix[5], img.Pix[6], img.Pix[7] = 0, 0xff, 0, 0xff

	if err := d.Flush(2, 1, img); err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", d.Addr())
	if err != nil {
		if errors.Is(err, syscall.ENETUNREACH) {
			t.Skipf("loopback is unreachable in this network namespace: %v", err)
		}

		t.Fatal(err)
	}
	defer conn.Close()

	version := readN(t, conn, 12)
	if string(version) != "RFB 003.008\n" {
		t.Fatalf("version: got %q", version)
	}

	writeAll(t, conn, version)

	securityTypes := readN(t, conn, 2)
	if !bytes.Equal(securityTypes, []byte{1, 1}) {
		t.Fatalf("security types: got %v, want [1 1]", securityTypes)
	}

	writeAll(t, conn, []byte{1})

	securityResult := readN(t, conn, 4)
	if binary.BigEndian.Uint32(securityResult) != 0 {
		t.Fatalf("security result: got %v, want OK", securityResult)
	}

	writeAll(t, conn, []byte{1}) // ClientInit: shared flag.

	serverInit := readN(t, conn, 24)
	if w := binary.BigEndian.Uint16(serverInit[0:2]); w != 2 {
		t.Fatalf("server width: got %d, want 2", w)
	}

	if h := binary.BigEndian.Uint16(serverInit[2:4]); h != 1 {
		t.Fatalf("server height: got %d, want 1", h)
	}

	nameLen := binary.BigEndian.Uint32(serverInit[20:24])
	name := readN(t, conn, int(nameLen))
	if string(name) != "gokvm" {
		t.Fatalf("server name: got %q, want gokvm", name)
	}

	request := []byte{
		3,    // FramebufferUpdateRequest
		0,    // incremental = false
		0, 0, // x
		0, 0, // y
		0, 2, // width
		0, 1, // height
	}
	writeAll(t, conn, request)

	header := readN(t, conn, 4)
	if !bytes.Equal(header, []byte{0, 0, 0, 1}) {
		t.Fatalf("framebuffer update header: got %v, want one rectangle", header)
	}

	rect := readN(t, conn, 12)
	if x := binary.BigEndian.Uint16(rect[0:2]); x != 0 {
		t.Fatalf("rect x: got %d, want 0", x)
	}

	if w := binary.BigEndian.Uint16(rect[4:6]); w != 2 {
		t.Fatalf("rect width: got %d, want 2", w)
	}

	if enc := binary.BigEndian.Uint32(rect[8:12]); enc != 0 {
		t.Fatalf("rect encoding: got %d, want raw", enc)
	}

	pixels := readN(t, conn, 8)
	want := []byte{
		0, 0, 0xff, 0, // red in default little-endian true-color format.
		0, 0xff, 0, 0, // green
	}
	if !bytes.Equal(pixels, want) {
		t.Fatalf("pixels: got %v, want %v", pixels, want)
	}

	writeAll(t, conn, []byte{
		4,    // KeyEvent
		1,    // down
		0, 0, // padding
		0, 0, 0, 'a',
	})
	writeAll(t, conn, []byte{
		4,    // KeyEvent
		0,    // up
		0, 0, // padding
		0, 0, 0, 'a',
	})
	writeAll(t, conn, []byte{
		5,     // PointerEvent
		0x05,  // left + right buttons
		0, 12, // x
		0, 34, // y
	})

	first := readInputEvent(t, input.events)
	if first.kind != "key" || !first.down || first.keysym != 'a' {
		t.Fatalf("first input event: got %+v", first)
	}

	second := readInputEvent(t, input.events)
	if second.kind != "key" || second.down || second.keysym != 'a' {
		t.Fatalf("second input event: got %+v", second)
	}

	third := readInputEvent(t, input.events)
	if third.kind != "pointer" || third.buttonMask != 0x05 || third.x != 12 || third.y != 34 {
		t.Fatalf("third input event: got %+v", third)
	}
}

func TestVNCDisplayInputWhileIncrementalUpdatePending(t *testing.T) {
	t.Parallel()

	d, err := virtio.NewVNCDisplay("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	input := &mockVNCInput{events: make(chan vncInputEvent, 1)}
	d.SetInput(input)

	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	if err := d.Flush(2, 1, img); err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", d.Addr())
	if err != nil {
		if errors.Is(err, syscall.ENETUNREACH) {
			t.Skipf("loopback is unreachable in this network namespace: %v", err)
		}

		t.Fatal(err)
	}
	defer conn.Close()

	version := readN(t, conn, 12)
	writeAll(t, conn, version)
	readN(t, conn, 2)
	writeAll(t, conn, []byte{1})
	readN(t, conn, 4)
	writeAll(t, conn, []byte{1})
	serverInit := readN(t, conn, 24)
	nameLen := binary.BigEndian.Uint32(serverInit[20:24])
	readN(t, conn, int(nameLen))

	writeAll(t, conn, []byte{
		3,    // FramebufferUpdateRequest
		0,    // incremental = false
		0, 0, // x
		0, 0, // y
		0, 2, // width
		0, 1, // height
	})
	readN(t, conn, 4)
	readN(t, conn, 12)
	readN(t, conn, 8)

	writeAll(t, conn, []byte{
		3,    // FramebufferUpdateRequest
		1,    // incremental = true, no newer frame is available.
		0, 0, // x
		0, 0, // y
		0, 2, // width
		0, 1, // height
	})
	writeAll(t, conn, []byte{
		5,     // PointerEvent
		0x01,  // left button
		0, 21, // x
		0, 43, // y
	})

	event := readInputEvent(t, input.events)
	if event.kind != "pointer" || event.buttonMask != 0x01 || event.x != 21 || event.y != 43 {
		t.Fatalf("input event: got %+v", event)
	}
}

func TestVNCDisplayCursorPseudoEncoding(t *testing.T) {
	t.Parallel()

	d, err := virtio.NewVNCDisplay("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := d.Flush(1, 1, img); err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", d.Addr())
	if err != nil {
		if errors.Is(err, syscall.ENETUNREACH) {
			t.Skipf("loopback is unreachable in this network namespace: %v", err)
		}

		t.Fatal(err)
	}
	defer conn.Close()

	version := readN(t, conn, 12)
	writeAll(t, conn, version)
	readN(t, conn, 2)
	writeAll(t, conn, []byte{1})
	readN(t, conn, 4)
	writeAll(t, conn, []byte{1})
	serverInit := readN(t, conn, 24)
	nameLen := binary.BigEndian.Uint32(serverInit[20:24])
	readN(t, conn, int(nameLen))

	writeAll(t, conn, []byte{
		2,    // SetEncodings
		0,    // padding
		0, 1, // one encoding
		0xff, 0xff, // Cursor pseudo-encoding (-239).
		0xff, 0x11,
	})
	writeAll(t, conn, []byte{
		3,    // FramebufferUpdateRequest
		0,    // incremental = false
		0, 0, // x
		0, 0, // y
		0, 1, // width
		0, 1, // height
	})

	header := readN(t, conn, 4)
	if rects := binary.BigEndian.Uint16(header[2:4]); rects != 2 {
		t.Fatalf("rectangles: got %d, want 2", rects)
	}

	rawRect := readN(t, conn, 12)
	if enc := binary.BigEndian.Uint32(rawRect[8:12]); enc != 0 {
		t.Fatalf("raw rect encoding: got 0x%x, want 0", enc)
	}
	readN(t, conn, 4)

	cursorRect := readN(t, conn, 12)
	if enc := binary.BigEndian.Uint32(cursorRect[8:12]); enc != 0xffffff11 {
		t.Fatalf("cursor rect encoding: got 0x%x, want 0xffffff11", enc)
	}
	if w := binary.BigEndian.Uint16(cursorRect[4:6]); w != 1 {
		t.Fatalf("cursor width: got %d, want 1", w)
	}
	if h := binary.BigEndian.Uint16(cursorRect[6:8]); h != 1 {
		t.Fatalf("cursor height: got %d, want 1", h)
	}

	payload := readN(t, conn, 1*1*4+1)
	if payload[4] != 0 {
		t.Fatalf("cursor mask: got 0x%x, want transparent", payload[4])
	}
}

func TestVNCDisplaySerialFallback(t *testing.T) {
	t.Parallel()

	d, err := virtio.NewVNCDisplay("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := d.Write([]byte("TinyCore booting\n")); err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", d.Addr())
	if err != nil {
		if errors.Is(err, syscall.ENETUNREACH) {
			t.Skipf("loopback is unreachable in this network namespace: %v", err)
		}

		t.Fatal(err)
	}
	defer conn.Close()

	version := readN(t, conn, 12)
	writeAll(t, conn, version)
	readN(t, conn, 2) // security types
	writeAll(t, conn, []byte{1})
	readN(t, conn, 4) // security result
	writeAll(t, conn, []byte{1})
	serverInit := readN(t, conn, 24)
	width := binary.BigEndian.Uint16(serverInit[0:2])
	height := binary.BigEndian.Uint16(serverInit[2:4])
	nameLen := binary.BigEndian.Uint32(serverInit[20:24])
	readN(t, conn, int(nameLen))

	writeAll(t, conn, []byte{
		3,    // FramebufferUpdateRequest
		0,    // incremental = false
		0, 0, // x
		0, 0, // y
		byte(width >> 8), byte(width),
		byte(height >> 8), byte(height),
	})

	header := readN(t, conn, 4)
	if binary.BigEndian.Uint16(header[2:4]) != 1 {
		t.Fatalf("rectangles: got %v, want one rectangle", header)
	}

	readN(t, conn, 12)
	pixels := readN(t, conn, int(width)*int(height)*4)
	if bytes.Count(pixels, []byte{0}) == len(pixels) {
		t.Fatal("serial fallback frame is blank")
	}
}

func TestVNCDisplayVGATextFallback(t *testing.T) {
	t.Parallel()

	d, err := virtio.NewVNCDisplay("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	mem := make([]byte, 0xc0000)
	copy(mem[0xb8000:], []byte{'T', 0x0f, 'C', 0x0f})
	d.StartVGATextFallback(mem)

	conn, err := net.Dial("tcp", d.Addr())
	if err != nil {
		if errors.Is(err, syscall.ENETUNREACH) {
			t.Skipf("loopback is unreachable in this network namespace: %v", err)
		}

		t.Fatal(err)
	}
	defer conn.Close()

	version := readN(t, conn, 12)
	writeAll(t, conn, version)
	readN(t, conn, 2)
	writeAll(t, conn, []byte{1})
	readN(t, conn, 4)
	writeAll(t, conn, []byte{1})
	serverInit := readN(t, conn, 24)
	width := binary.BigEndian.Uint16(serverInit[0:2])
	height := binary.BigEndian.Uint16(serverInit[2:4])
	nameLen := binary.BigEndian.Uint32(serverInit[20:24])
	readN(t, conn, int(nameLen))

	deadline := time.After(2 * time.Second)
	for {
		writeAll(t, conn, []byte{
			3,    // FramebufferUpdateRequest
			0,    // incremental = false
			0, 0, // x
			0, 0, // y
			byte(width >> 8), byte(width),
			byte(height >> 8), byte(height),
		})

		header := readN(t, conn, 4)
		rects := binary.BigEndian.Uint16(header[2:4])
		if rects == 0 {
			continue
		}

		rect := readN(t, conn, 12)
		rectWidth := binary.BigEndian.Uint16(rect[4:6])
		rectHeight := binary.BigEndian.Uint16(rect[6:8])
		pixels := readN(t, conn, int(rectWidth)*int(rectHeight)*4)
		if bytes.Count(pixels, []byte{0}) != len(pixels) {
			return
		}

		select {
		case <-deadline:
			t.Fatal("VGA fallback frame stayed blank")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestVNCDisplayLinearFramebufferFallback(t *testing.T) {
	t.Parallel()

	d, err := virtio.NewVNCDisplay("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	mem := make([]byte, 0x1000)
	copy(mem[0x100:], []byte{0x10, 0x20, 0x30, 0x00, 0x40, 0x50, 0x60, 0x00})
	d.StartLinearFramebufferFallback(mem, 0x100, 2, 1, 8)

	conn, err := net.Dial("tcp", d.Addr())
	if err != nil {
		if errors.Is(err, syscall.ENETUNREACH) {
			t.Skipf("loopback is unreachable in this network namespace: %v", err)
		}

		t.Fatal(err)
	}
	defer conn.Close()

	version := readN(t, conn, 12)
	writeAll(t, conn, version)
	readN(t, conn, 2)
	writeAll(t, conn, []byte{1})
	readN(t, conn, 4)
	writeAll(t, conn, []byte{1})
	serverInit := readN(t, conn, 24)
	width := binary.BigEndian.Uint16(serverInit[0:2])
	height := binary.BigEndian.Uint16(serverInit[2:4])
	nameLen := binary.BigEndian.Uint32(serverInit[20:24])
	readN(t, conn, int(nameLen))

	deadline := time.After(2 * time.Second)
	for {
		writeAll(t, conn, []byte{
			3,    // FramebufferUpdateRequest
			0,    // incremental = false
			0, 0, // x
			0, 0, // y
			byte(width >> 8), byte(width),
			byte(height >> 8), byte(height),
		})

		header := readN(t, conn, 4)
		rects := binary.BigEndian.Uint16(header[2:4])
		if rects == 0 {
			continue
		}

		rect := readN(t, conn, 12)
		rectWidth := binary.BigEndian.Uint16(rect[4:6])
		rectHeight := binary.BigEndian.Uint16(rect[6:8])
		pixels := readN(t, conn, int(rectWidth)*int(rectHeight)*4)
		if bytes.Count(pixels, []byte{0}) != len(pixels) {
			return
		}

		select {
		case <-deadline:
			t.Fatal("linear framebuffer fallback did not render BGRX pixels")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

type vncInputEvent struct {
	kind       string
	down       bool
	keysym     uint32
	buttonMask uint8
	x          uint16
	y          uint16
}

type mockVNCInput struct {
	events chan vncInputEvent
}

func (m *mockVNCInput) KeyEvent(down bool, keysym uint32) {
	m.events <- vncInputEvent{kind: "key", down: down, keysym: keysym}
}

func (m *mockVNCInput) PointerEvent(buttonMask uint8, x, y uint16) {
	m.events <- vncInputEvent{kind: "pointer", buttonMask: buttonMask, x: x, y: y}
}

func readInputEvent(t *testing.T, events <-chan vncInputEvent) vncInputEvent {
	t.Helper()

	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for VNC input event")
	}

	return vncInputEvent{}
}

func readN(t *testing.T, r io.Reader, n int) []byte {
	t.Helper()

	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		t.Fatal(err)
	}

	return b
}

func writeAll(t *testing.T, w io.Writer, b []byte) {
	t.Helper()

	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
}
