package virtio

import (
	"bytes"
	"image"
	"io"
	"testing"
	"time"
)

func TestFramebufferSerialResumesAfterFirmwareReset(t *testing.T) {
	t.Parallel()
	d := newFramebuffer()
	d.serialInterval = time.Hour
	t.Cleanup(d.shutdown)
	if _, err := d.writeSerial([]byte("old firmware text")); err != nil {
		t.Fatal(err)
	}
	firmware := image.NewRGBA(image.Rect(0, 0, 32, 32))
	firmware.Pix[0] = 255
	if err := d.Flush(32, 32, firmware); err != nil {
		t.Fatal(err)
	}
	before := d.snapshot()
	d.GraphicsReset()
	if got := d.snapshot(); got.seq != before.seq || !bytes.Equal(got.pix, before.pix) {
		t.Fatal("reset replaced firmware graphics before fresh serial output")
	}
	if _, err := d.writeSerial([]byte("Linux boot\n")); err != nil {
		t.Fatal(err)
	}
	// Linux resets the already released device again during probe.
	d.GraphicsReset()
	if _, err := d.writeSerial([]byte("driver probe\n")); err != nil {
		t.Fatal(err)
	}
	d.refreshSerial()
	want := newVNCTextConsole(vncTextCols, vncTextRows)
	want.write([]byte("Linux boot\ndriver probe\n"))
	if !bytes.Equal(d.snapshot().pix, want.render().Pix) {
		t.Fatal("kernel serial was lost or stale firmware text was replayed")
	}
	if err := d.Flush(32, 32, firmware); err != nil {
		t.Fatal(err)
	}
	if _, err := d.writeSerial([]byte("late serial")); err != nil {
		t.Fatal(err)
	}
	if d.refreshSerial() || !bytes.Equal(d.snapshot().pix, firmware.Pix) {
		t.Fatal("late serial overwrote active OS graphics")
	}
	d.shutdown()
	d.GraphicsReset()
	if d.refreshSerial() {
		t.Fatal("reset restarted a closed display")
	}
}

func TestFramebufferSerialWorkerRestartsAfterReset(t *testing.T) {
	t.Parallel()
	d := newFramebuffer()
	d.serialInterval = time.Millisecond
	t.Cleanup(d.shutdown)
	if _, err := d.writeSerial([]byte("firmware")); err != nil {
		t.Fatal(err)
	}
	if err := d.Flush(1, 1, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		d.textMu.Lock()
		running := d.serialRunning
		d.textMu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("serial worker did not stop after graphics takeover")
		}
		time.Sleep(time.Millisecond)
	}
	d.GraphicsReset()
	if _, err := d.writeSerial([]byte("boot\n")); err != nil {
		t.Fatal(err)
	}
	frame := d.snapshot()
	_, _, changed := d.changedFrame(frame.seq, false)
	if _, err := d.writeSerial([]byte("ready")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("serial rendering worker was not restarted")
	}
	want := newVNCTextConsole(vncTextCols, vncTextRows)
	want.write([]byte("boot\nready"))
	if !bytes.Equal(d.snapshot().pix, want.render().Pix) {
		t.Fatal("restarted worker lost pending boot text")
	}
}

func TestRDPDoesNotAcceptSerialOutput(t *testing.T) {
	t.Parallel()
	if _, ok := any((*RDPDisplay)(nil)).(io.Writer); ok {
		t.Fatal("RDP exposes a serial output writer")
	}
	if _, ok := any((*VNCDisplay)(nil)).(io.Writer); !ok {
		t.Fatal("VNC lost its serial output writer")
	}
}

func TestRDPGraphicsReplacesSameSizeFallbackFrame(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	t.Cleanup(d.shutdown)
	fallback := image.NewRGBA(image.Rect(0, 0, 32, 32))
	fallback.Pix[0] = 255
	var last []byte
	rendered := false
	if !d.refreshFallback([]byte{1}, &last, &rendered, framebufferBlank, func([]byte) *image.RGBA { return fallback }) {
		t.Fatal("legacy framebuffer did not start")
	}
	img := image.NewRGBA(fallback.Bounds())
	img.Pix[len(img.Pix)-4] = 255
	// Even a small update must replace fallback pixels across the whole image.
	if err := d.FlushDamage(32, 32, img, []image.Rectangle{image.Rect(0, 0, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d.snapshot().pix, img.Pix) {
		t.Fatal("fallback pixels survived the graphics handoff")
	}
	d.GraphicsReset()
	for _, damage := range [][]image.Rectangle{{}, {image.Rect(-10, -10, -1, -1)}} {
		before := d.snapshot()
		if err := d.FlushDamage(16, 16, image.NewRGBA(image.Rect(0, 0, 16, 16)), damage); err != nil {
			t.Fatal(err)
		}
		if d.snapshot().seq != before.seq || d.textDisabled {
			t.Fatal("empty damage reclaimed graphics ownership")
		}
	}
	img.Pix[4] = 255
	if err := d.FlushDamage(32, 32, img, []image.Rectangle{image.Rect(0, 0, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d.snapshot().pix, img.Pix) {
		t.Fatal("first graphics frame after reset was not published in full")
	}
}

func TestFramebufferResetDoesNotReviveLegacyFallback(t *testing.T) {
	t.Parallel()
	d := newFramebuffer()
	t.Cleanup(d.shutdown)
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var last []byte
	rendered := false
	if !d.refreshFallback([]byte{1}, &last, &rendered, framebufferBlank, func([]byte) *image.RGBA { return img }) {
		t.Fatal("legacy fallback did not start")
	}
	d.GraphicsReset()
	if !d.serialMuted {
		t.Fatal("initial GPU reset displaced active legacy graphics")
	}
	if err := d.Flush(1, 1, img); err != nil {
		t.Fatal(err)
	}
	d.GraphicsReset()
	if _, err := d.writeSerial([]byte("kernel boot")); err != nil {
		t.Fatal(err)
	}
	before := d.snapshot()
	if d.refreshFallback([]byte{2}, &last, &rendered, framebufferBlank, func([]byte) *image.RGBA {
		t.Error("stale legacy framebuffer rendered after GPU ownership")

		return img
	}) {
		t.Fatal("legacy fallback revived across GPU reset")
	}
	if d.snapshot().seq != before.seq {
		t.Fatal("legacy fallback overwrote kernel serial")
	}
}
