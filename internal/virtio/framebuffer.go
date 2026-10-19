package virtio

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"sync"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/guestmem"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

var errInvalidFramebuffer = errors.New("invalid framebuffer image or dimensions")

// ConsoleDisplay presents guest graphics and legacy framebuffer consoles
// through the same remote display.
type ConsoleDisplay interface {
	Display
	SetInput(DisplayInput)
	StartVGATextFallback(mem []byte)
	StartLinearFramebufferFallback(mem []byte, base, width, height, stride int)
}

// framebuffer holds protocol-independent display, fallback, and input state.
// The text lock serializes fallback publication with the first GPU frame and
// coordinates worker creation with shutdown. The frame lock never acquires it.
type framebuffer struct {
	mu             sync.Mutex
	width          int
	height         int
	frame          []byte
	frameShared    bool
	tileVersions   []uint64
	seq            uint64
	input          DisplayInput
	done           chan struct{}
	changed        chan struct{}
	linearInterval time.Duration

	textMu                 sync.Mutex
	graphicsActive         bool
	legacyFallbackDisabled bool
	stopped                bool
	fallbackWG             sync.WaitGroup
	shutdownOnce           sync.Once
}

func newFramebuffer() *framebuffer {
	d := &framebuffer{
		width:          displayDefaultWidth,
		height:         displayDefaultHeight,
		done:           make(chan struct{}),
		changed:        make(chan struct{}),
		linearInterval: 33 * time.Millisecond,
	}

	return d
}

// SetInput attaches an input sink for remote keyboard and pointer events.
func (d *framebuffer) SetInput(input DisplayInput) {
	d.mu.Lock()
	d.input = input
	d.mu.Unlock()
}

// GraphicsReset releases graphics ownership after the GPU loses its scanout.
// OVMF resets virtio-gpu at ExitBootServices. Keep the last graphics image
// visible until the next driver publishes a frame.
func (d *framebuffer) GraphicsReset() {
	d.textMu.Lock()
	defer d.textMu.Unlock()
	if d.stopped || !d.graphicsActive {
		return
	}
	d.graphicsActive = false
}

// Flush publishes a GPU frame, permanently replacing legacy console fallbacks.
func (d *framebuffer) Flush(width, height int, img *image.RGBA) error {
	d.textMu.Lock()
	defer d.textMu.Unlock()

	if d.stopped {
		return nil
	}

	if err := d.flush(width, height, img); err != nil {
		return err
	}

	if width > 0 && height > 0 {
		d.graphicsActive = true
		d.legacyFallbackDisabled = true
	}

	return nil
}

// flush is called with textMu held, including while publishing fallback frames.
func (d *framebuffer) flush(width, height int, img *image.RGBA) error {
	if width <= 0 || height <= 0 {
		return nil
	}

	const maxInt = int(^uint(0) >> 1)
	if img == nil || width > maxInt/4 || height > maxInt/(width*4) ||
		width > img.Rect.Dx() || height > img.Rect.Dy() {
		return errInvalidFramebuffer
	}

	rowBytes := width * 4
	if img.Stride < rowBytes || len(img.Pix) < rowBytes ||
		height-1 > (len(img.Pix)-rowBytes)/img.Stride {
		return errInvalidFramebuffer
	}

	frame := make([]byte, rowBytes*height)
	for y := 0; y < height; y++ {
		copy(frame[y*rowBytes:(y+1)*rowBytes], img.Pix[y*img.Stride:y*img.Stride+rowBytes])
	}

	d.mu.Lock()
	d.width = width
	d.height = height
	d.frame = frame
	d.frameShared = false
	d.seq++
	d.markFrameDamageLocked(nil)
	close(d.changed)
	d.changed = make(chan struct{})
	d.mu.Unlock()

	return nil
}

// StartVGATextFallback presents legacy VGA text until the GPU supplies a frame.
func (d *framebuffer) StartVGATextFallback(mem []byte) {
	const textSize = vgaTextCols * vgaTextRows * 2
	if len(mem) < vgaTextBase+textSize {
		return
	}

	d.startFallback(mem[vgaTextBase:vgaTextBase+textSize], 100*time.Millisecond,
		vgaTextBlank, renderVGAText)
}

// StartLinearFramebufferFallback presents a guest BGRX framebuffer until the
// GPU supplies a frame. Guest memory must remain valid until display closure.
func (d *framebuffer) StartLinearFramebufferFallback(mem []byte, base, width, height, stride int) {
	if base < 0 || base > len(mem) || width <= 0 || height <= 0 ||
		stride <= 0 || width > stride/4 || height > (len(mem)-base)/stride ||
		!guestmem.ValidRange(mem, uint64(base), uint64(stride*height)) {
		return
	}

	d.startFallback(mem[base:base+stride*height], d.linearInterval, framebufferBlank,
		func(frame []byte) *image.RGBA {
			return renderLinearFramebuffer(frame, width, height, stride)
		})
}

func (d *framebuffer) startFallback(
	mem []byte,
	interval time.Duration,
	isBlank func([]byte) bool,
	render func([]byte) *image.RGBA,
) {
	d.textMu.Lock()
	if d.stopped || d.legacyFallbackDisabled {
		d.textMu.Unlock()

		return
	}

	d.fallbackWG.Add(1)
	d.textMu.Unlock()

	go func() {
		defer d.fallbackWG.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var last []byte
		rendered := false
		for {
			select {
			case <-d.done:
				return
			case <-ticker.C:
			}

			if !d.refreshFallback(mem, &last, &rendered, isBlank, render) {
				return
			}
		}
	}()
}

func (d *framebuffer) refreshFallback(
	mem []byte,
	last *[]byte,
	rendered *bool,
	isBlank func([]byte) bool,
	render func([]byte) *image.RGBA,
) bool {
	d.textMu.Lock()
	defer d.textMu.Unlock()

	if d.stopped || d.legacyFallbackDisabled {
		return false
	}

	if bytes.Equal(mem, *last) {
		return true
	}

	snap := bytes.Clone(mem)
	*last = snap
	if !*rendered && isBlank(snap) {
		return true
	}

	*rendered = true
	img := render(snap)
	_ = d.flush(img.Bounds().Dx(), img.Bounds().Dy(), img)

	return true
}

// shutdown stops fallback workers and signals display shutdown. The transport
// remains responsible for closing and waiting for its connections.
func (d *framebuffer) shutdown() {
	d.shutdownOnce.Do(func() {
		d.textMu.Lock()
		d.stopped = true
		close(d.done)
		d.textMu.Unlock()

		d.fallbackWG.Wait()
	})
}

const (
	displayDefaultWidth  = 1024
	displayDefaultHeight = 768
	vgaTextCellWidth     = 7
	vgaTextCellHeight    = 13

	vgaTextBase = 0xb8000
	vgaTextCols = 80
	vgaTextRows = 25
)

type displayFrame struct {
	width  int
	height int
	pix    []byte
	seq    uint64
}

// DisplayInput receives X11 keysyms and absolute pointer events from remote displays.
type DisplayInput interface {
	KeyEvent(down bool, keysym uint32)
	PointerEvent(buttonMask uint8, x, y uint16)
}

func (d *framebuffer) sendKeyEvent(down bool, keysym uint32) {
	d.mu.Lock()
	input := d.input
	d.mu.Unlock()

	if input != nil {
		input.KeyEvent(down, keysym)
	}
}

func sendPointerEventInBounds(input DisplayInput, buttonMask uint8, x, y uint16, width, height int) {
	if bounded, ok := input.(interface {
		PointerEventInBounds(uint8, uint16, uint16, int, int)
	}); ok {
		bounded.PointerEventInBounds(buttonMask, x, y, width, height)

		return
	}
	if input != nil {
		input.PointerEvent(buttonMask, x, y)
	}
}

func (d *framebuffer) snapshot() displayFrame {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.snapshotLocked()
}

// changedFrame returns an immutable frame and the next publication notification
// atomically, so a writer cannot lose a wakeup between reading and waiting.
// Unlike snapshot, the returned pixels must never be modified by the caller.
func (d *framebuffer) changedFrame(sequence uint64, force bool) (displayFrame, bool, <-chan struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !force && d.seq == sequence {
		return displayFrame{}, false, d.changed
	}
	d.frameShared = true

	return displayFrame{width: d.width, height: d.height, pix: d.frame, seq: d.seq}, true, d.changed
}

func (d *framebuffer) snapshotLocked() displayFrame {
	frame := make([]byte, len(d.frame))
	copy(frame, d.frame)

	return displayFrame{
		width:  d.width,
		height: d.height,
		pix:    frame,
		seq:    d.seq,
	}
}

func renderVGAText(text []byte) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, vgaTextCols*vgaTextCellWidth, vgaTextRows*vgaTextCellHeight))
	drawer := font.Drawer{
		Dst:  img,
		Face: basicfont.Face7x13,
	}

	for y := 0; y < vgaTextRows; y++ {
		for x := 0; x < vgaTextCols; x++ {
			off := (y*vgaTextCols + x) * 2
			ch := text[off]
			if ch < 0x20 || ch >= 0x7f {
				ch = ' '
			}

			if ch == ' ' {
				continue
			}

			drawer.Dot = fixed.P(x*vgaTextCellWidth, y*vgaTextCellHeight+11)
			drawer.Src = image.NewUniform(vgaColor(text[off+1] & 0x0f))
			drawer.DrawString(string([]byte{ch}))
		}
	}

	return img
}

func vgaTextBlank(text []byte) bool {
	for i := 0; i+1 < len(text); i += 2 {
		ch := text[i]
		if ch != 0 && ch != ' ' {
			return false
		}
	}

	return true
}

func vgaColor(idx byte) color.Color {
	palette := [...]color.RGBA{
		{R: 0x00, G: 0x00, B: 0x00, A: 0xff},
		{R: 0x00, G: 0x00, B: 0xaa, A: 0xff},
		{R: 0x00, G: 0xaa, B: 0x00, A: 0xff},
		{R: 0x00, G: 0xaa, B: 0xaa, A: 0xff},
		{R: 0xaa, G: 0x00, B: 0x00, A: 0xff},
		{R: 0xaa, G: 0x00, B: 0xaa, A: 0xff},
		{R: 0xaa, G: 0x55, B: 0x00, A: 0xff},
		{R: 0xaa, G: 0xaa, B: 0xaa, A: 0xff},
		{R: 0x55, G: 0x55, B: 0x55, A: 0xff},
		{R: 0x55, G: 0x55, B: 0xff, A: 0xff},
		{R: 0x55, G: 0xff, B: 0x55, A: 0xff},
		{R: 0x55, G: 0xff, B: 0xff, A: 0xff},
		{R: 0xff, G: 0x55, B: 0x55, A: 0xff},
		{R: 0xff, G: 0x55, B: 0xff, A: 0xff},
		{R: 0xff, G: 0xff, B: 0x55, A: 0xff},
		{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	}

	return palette[idx&0x0f]
}

func renderLinearFramebuffer(frame []byte, width, height, stride int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			src := y*stride + x*4
			dst := img.PixOffset(x, y)
			img.Pix[dst+0] = frame[src+2]
			img.Pix[dst+1] = frame[src+1]
			img.Pix[dst+2] = frame[src+0]
			img.Pix[dst+3] = 0xff
		}
	}

	return img
}

func framebufferBlank(frame []byte) bool {
	for _, b := range frame {
		if b != 0 {
			return false
		}
	}

	return true
}
