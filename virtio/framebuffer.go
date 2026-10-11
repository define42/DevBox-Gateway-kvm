package virtio

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"sync"
	"time"

	"github.com/define42/gokvm/internal/guestmem"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

var errInvalidFramebuffer = errors.New("invalid framebuffer image or dimensions")

// ConsoleDisplay presents guest graphics and legacy framebuffer consoles
// through the same remote display.
type ConsoleDisplay interface {
	Display
	SetInput(VNCInput)
	StartVGATextFallback(mem []byte)
	StartLinearFramebufferFallback(mem []byte, base, width, height, stride int)
}

// framebuffer holds protocol-independent display, fallback, and input state.
// The text lock serializes fallback publication with the first GPU frame and
// coordinates worker creation with shutdown. The frame lock never acquires it.
type framebuffer struct {
	mu             sync.Mutex
	cond           *sync.Cond
	width          int
	height         int
	frame          []byte
	frameShared    bool
	tileVersions   []uint64
	seq            uint64
	input          VNCInput
	done           chan struct{}
	changed        chan struct{}
	linearInterval time.Duration
	serialInterval time.Duration

	textMu                 sync.Mutex
	textConsole            *vncTextConsole
	textDirty              bool
	serialRunning          bool
	textDisabled           bool
	legacyFallbackDisabled bool
	serialMuted            bool
	stopped                bool
	fallbackWG             sync.WaitGroup
	shutdownOnce           sync.Once
}

func newFramebuffer() *framebuffer {
	d := &framebuffer{
		width:          vncDefaultWidth,
		height:         vncDefaultHeight,
		done:           make(chan struct{}),
		changed:        make(chan struct{}),
		linearInterval: 33 * time.Millisecond,
		serialInterval: 33 * time.Millisecond,
	}
	d.cond = sync.NewCond(&d.mu)

	return d
}

// SetInput attaches an input sink for remote keyboard and pointer events.
func (d *framebuffer) SetInput(input VNCInput) {
	d.mu.Lock()
	d.input = input
	d.mu.Unlock()
}

// GraphicsReset releases graphics ownership after the GPU loses its scanout.
// OVMF resets virtio-gpu at ExitBootServices. VNC may then show fresh serial
// output until the next driver publishes a frame. Keep the last graphics image
// visible until new output arrives.
func (d *framebuffer) GraphicsReset() {
	d.textMu.Lock()
	defer d.textMu.Unlock()
	if d.stopped || !d.textDisabled {
		return
	}
	d.textDisabled = false
	d.serialMuted = false
	d.textConsole = nil
	d.textDirty = false
}

// Flush publishes a GPU frame, replacing console fallbacks until a GPU reset.
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
		d.textDisabled = true
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
	d.cond.Broadcast()
	d.mu.Unlock()

	return nil
}

// writeSerial mirrors serial bytes for VNC until a graphical or VGA console takes over.
// After the first frame, rendering is batched so byte-at-a-time UART writes do
// not stall the guest copying a complete framebuffer for every character.
func (d *framebuffer) writeSerial(p []byte) (int, error) {
	d.textMu.Lock()
	defer d.textMu.Unlock()

	if len(p) == 0 || d.stopped || d.textDisabled || d.serialMuted {
		return len(p), nil
	}

	first := d.textConsole == nil
	if first {
		d.textConsole = newVNCTextConsole(vncTextCols, vncTextRows)
	}

	d.textConsole.write(p)
	if !d.serialRunning {
		d.serialRunning = true
		d.fallbackWG.Add(1)
		go d.renderSerialFrames()
	}
	if !first {
		d.textDirty = true

		return len(p), nil
	}

	img := d.textConsole.render()
	if err := d.flush(img.Bounds().Dx(), img.Bounds().Dy(), img); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (d *framebuffer) renderSerialFrames() {
	defer d.fallbackWG.Done()
	ticker := time.NewTicker(d.serialInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			d.textMu.Lock()
			active := d.refreshSerialLocked()
			if !active {
				d.serialRunning = false
			}
			d.textMu.Unlock()
			if !active {
				return
			}
		}
	}
}

func (d *framebuffer) refreshSerial() bool {
	d.textMu.Lock()
	defer d.textMu.Unlock()

	return d.refreshSerialLocked()
}

func (d *framebuffer) refreshSerialLocked() bool {
	if d.stopped || d.textDisabled || d.serialMuted {
		return false
	}
	if d.textDirty {
		img := d.textConsole.render()
		_ = d.flush(img.Bounds().Dx(), img.Bounds().Dy(), img)
		d.textDirty = false
	}

	return true
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
	d.serialMuted = true
	img := render(snap)
	_ = d.flush(img.Bounds().Dx(), img.Bounds().Dy(), img)

	return true
}

// shutdown stops fallback workers and wakes all protocol frame waiters. Each
// transport remains responsible for closing and waiting for its connections.
func (d *framebuffer) shutdown() {
	d.shutdownOnce.Do(func() {
		d.textMu.Lock()
		d.stopped = true
		close(d.done)
		d.textMu.Unlock()

		d.wakeFrameWaiters()
		d.fallbackWG.Wait()
	})
}

const (
	vncDefaultWidth  = 1024
	vncDefaultHeight = 768
	vncTextCols      = 100
	vncTextRows      = 40
	vncTextCellW     = 7
	vncTextCellH     = 13

	vgaTextBase = 0xb8000
	vgaTextCols = 80
	vgaTextRows = 25
)

type vncFrame struct {
	width  int
	height int
	pix    []byte
	seq    uint64
}

// VNCInput receives X11 keysyms and RFB-style pointer events from remote displays.
type VNCInput interface {
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

func (d *framebuffer) sendPointerEvent(buttonMask uint8, x, y uint16) {
	d.mu.Lock()
	input := d.input
	width, height := d.width, d.height
	d.mu.Unlock()

	sendPointerEventInBounds(input, buttonMask, x, y, width, height)
}

func sendPointerEventInBounds(input VNCInput, buttonMask uint8, x, y uint16, width, height int) {
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

func (d *framebuffer) snapshot() vncFrame {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.snapshotLocked()
}

// changedFrame returns an immutable frame and the next publication notification
// atomically, so a writer cannot lose a wakeup between reading and waiting.
// Unlike snapshot, the returned pixels must never be modified by the caller.
func (d *framebuffer) changedFrame(sequence uint64, force bool) (vncFrame, bool, <-chan struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !force && d.seq == sequence {
		return vncFrame{}, false, d.changed
	}
	d.frameShared = true

	return vncFrame{width: d.width, height: d.height, pix: d.frame, seq: d.seq}, true, d.changed
}

func (d *framebuffer) frameForRequestUntil(incremental bool, lastSeq uint64, done <-chan struct{}) (vncFrame, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if incremental {
		for d.seq == lastSeq {
			if d.frameWaitCanceled(done) {
				return d.snapshotLocked(), false
			}

			d.cond.Wait()
		}
	}

	return d.snapshotLocked(), true
}

func (d *framebuffer) frameWaitCanceled(done <-chan struct{}) bool {
	select {
	case <-d.done:
		return true
	default:
	}

	if done != nil {
		select {
		case <-done:
			return true
		default:
		}
	}

	return false
}

func (d *framebuffer) wakeFrameWaiters() {
	d.mu.Lock()
	d.cond.Broadcast()
	d.mu.Unlock()
}

func (d *framebuffer) snapshotLocked() vncFrame {
	frame := make([]byte, len(d.frame))
	copy(frame, d.frame)

	return vncFrame{
		width:  d.width,
		height: d.height,
		pix:    frame,
		seq:    d.seq,
	}
}

type vncTextConsole struct {
	cols int
	rows int

	cells [][]rune
	row   int
	col   int

	escape []byte
}

func newVNCTextConsole(cols, rows int) *vncTextConsole {
	c := &vncTextConsole{
		cols:  cols,
		rows:  rows,
		cells: make([][]rune, rows),
	}
	for y := range c.cells {
		c.cells[y] = make([]rune, cols)
		for x := range c.cells[y] {
			c.cells[y][x] = ' '
		}
	}

	return c
}

func (c *vncTextConsole) write(p []byte) {
	for _, b := range p {
		c.putByte(b)
	}
}

func (c *vncTextConsole) putByte(b byte) {
	if len(c.escape) > 0 {
		c.putEscapeByte(b)

		return
	}

	switch b {
	case 0x1b:
		c.escape = []byte{b}
	case '\r':
		c.col = 0
	case '\n':
		c.newline()
	case '\b':
		if c.col > 0 {
			c.col--
		}
	case '\t':
		for {
			c.putRune(' ')
			if c.col%8 == 0 {
				break
			}
		}
	default:
		if b >= 0x20 && b < 0x7f {
			c.putRune(rune(b))
		}
	}
}

func (c *vncTextConsole) putEscapeByte(b byte) {
	c.escape = append(c.escape, b)
	if len(c.escape) == 2 && b != '[' {
		c.escape = nil

		return
	}

	if len(c.escape) < 3 {
		return
	}

	if b < 0x40 || b > 0x7e {
		return
	}

	c.handleCSI(string(c.escape[2:len(c.escape)-1]), b)
	c.escape = nil
}

func (c *vncTextConsole) handleCSI(params string, final byte) {
	switch final {
	case 'H', 'f':
		c.row, c.col = 0, 0
	case 'J':
		if params == "" || params == "2" {
			c.clear()
		}
	case 'K':
		for x := c.col; x < c.cols; x++ {
			c.cells[c.row][x] = ' '
		}
	case 'm', 'h', 'l':
		// Styling and terminal mode toggles are ignored by the fallback.
	default:
	}
}

func (c *vncTextConsole) putRune(r rune) {
	if c.col >= c.cols {
		c.newline()
	}

	c.cells[c.row][c.col] = r
	c.col++
}

func (c *vncTextConsole) newline() {
	c.col = 0
	c.row++
	if c.row < c.rows {
		return
	}

	copy(c.cells, c.cells[1:])
	c.cells[c.rows-1] = make([]rune, c.cols)
	for x := range c.cells[c.rows-1] {
		c.cells[c.rows-1][x] = ' '
	}

	c.row = c.rows - 1
}

func (c *vncTextConsole) clear() {
	for y := range c.cells {
		for x := range c.cells[y] {
			c.cells[y][x] = ' '
		}
	}

	c.row, c.col = 0, 0
}

func (c *vncTextConsole) render() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, c.cols*vncTextCellW, c.rows*vncTextCellH))
	drawer := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.RGBA{R: 0xe8, G: 0xea, B: 0xed, A: 0xff}),
		Face: basicfont.Face7x13,
	}

	for y, row := range c.cells {
		drawer.Dot = fixed.P(0, y*vncTextCellH+11)
		drawer.DrawString(string(row))
	}

	return img
}

func renderVGAText(text []byte) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, vgaTextCols*vncTextCellW, vgaTextRows*vncTextCellH))
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

			drawer.Dot = fixed.P(x*vncTextCellW, y*vncTextCellH+11)
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
