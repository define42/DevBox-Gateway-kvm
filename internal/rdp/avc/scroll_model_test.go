package avc

import (
	"fmt"
	"image"
	"math/rand/v2"
	"testing"

	openh264 "github.com/define42/DevBox-Gateway-kvm/pkg/h264"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

const (
	modelWidth, modelHeight = 640, 480
	modelTile               = 64
	modelLine               = 18
)

// modelDesktop draws a static frame around a document pane scrolled to offset.
type modelDesktop struct {
	document [][]byte // RGB rows of a tall document.
	offset   int
	status   int
	edits    map[[2]int]byte // Document pixels changed after drawing.
}

func modelPane() image.Rectangle { return image.Rect(96, 40, 608, 448) }

func newModelDesktop(rng *rand.Rand) *modelDesktop {
	d := &modelDesktop{edits: map[[2]int]byte{}}
	width := modelPane().Dx()
	for line := 0; line < 160; line++ {
		ink := [3]byte{30, 30, 40}
		switch line % 7 {
		case 2:
			ink = [3]byte{20, 70, 200} // Links exercise chroma.
		case 5:
			ink = [3]byte{170, 40, 40}
		}
		blank := line%9 == 8
		for y := range modelLine {
			row := make([]byte, width*3)
			for x := range width {
				r, g, b := byte(250), byte(250), byte(246)
				if !blank && y >= 4 && y < 15 && x >= 8 && x < width-40 && rng.IntN(3) == 0 {
					r, g, b = ink[0], ink[1], ink[2]
				}
				row[x*3], row[x*3+1], row[x*3+2] = r, g, b
			}
			d.document = append(d.document, row)
		}
	}

	return d
}

func (d *modelDesktop) draw() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, modelWidth, modelHeight))
	pane := modelPane()
	for y := range modelHeight {
		for x := range modelWidth {
			r, g, b := byte(45), byte(52), byte(63)
			switch {
			case image.Pt(x, y).In(pane):
				row := d.document[y-pane.Min.Y+d.offset]
				i := (x - pane.Min.X) * 3
				r, g, b = row[i], row[i+1], row[i+2]
				if value, ok := d.edits[[2]int{x, y - pane.Min.Y + d.offset}]; ok {
					r, g, b = value, 255-value, value/2
				}
			case x >= 608 && x < 624 && y >= 40 && y < 448:
				thumb := 40 + d.offset*(408-60)/(len(d.document)-pane.Dy())
				r, g, b = 200, 200, 205
				if y >= thumb && y < thumb+60 {
					r, g, b = 90, 90, 100
				}
			case y >= 456 && x < 64+d.status%200:
				r, g, b = byte(100+d.status), 160, 90
			case x < 96 && (x/24+y/24)%3 == 0:
				r, g, b = 70, byte(110+y/8), 160
			}
			offset := img.PixOffset(x, y)
			img.Pix[offset], img.Pix[offset+1], img.Pix[offset+2], img.Pix[offset+3] = r, g, b, 255
		}
	}

	return img
}

func (d *modelDesktop) scroll(by int) {
	d.offset = max(0, min(len(d.document)-modelPane().Dy(), d.offset+by))
}

// modelDamage reports changed 64-pixel tiles as the framebuffer does: runs of
// each tile row, merged downward when they span the same columns.
func modelDamage(previous, current *image.RGBA) []image.Rectangle {
	var rects []image.Rectangle
	for ty := 0; ty*modelTile < modelHeight; ty++ {
		for tx := 0; tx*modelTile < modelWidth; {
			if !modelTileChanged(previous, current, tx, ty) {
				tx++

				continue
			}
			start := tx
			for tx*modelTile < modelWidth && modelTileChanged(previous, current, tx, ty) {
				tx++
			}
			rect := image.Rect(start*modelTile, ty*modelTile, min(tx*modelTile, modelWidth), min((ty+1)*modelTile, modelHeight))
			merged := false
			for i := len(rects) - 1; i >= 0; i-- {
				if rects[i].Max.Y == rect.Min.Y && rects[i].Min.X == rect.Min.X && rects[i].Max.X == rect.Max.X {
					rects[i].Max.Y, merged = rect.Max.Y, true

					break
				}
			}
			if !merged {
				rects = append(rects, rect)
			}
		}
	}

	return rects
}

func modelTileChanged(previous, current *image.RGBA, tx, ty int) bool {
	for y := ty * modelTile; y < min((ty+1)*modelTile, modelHeight); y++ {
		start := current.PixOffset(tx*modelTile, y)
		end := current.PixOffset(min((tx+1)*modelTile, modelWidth), y)
		if string(previous.Pix[start:end]) != string(current.Pix[start:end]) {
			return true
		}
	}

	return false
}

// modelClient applies copies and repaints as an RDP client does.
type modelClient struct {
	decoder *openh264.Decoder
	surface *image.RGBA
}

func (c *modelClient) present(t *testing.T, data []byte, update Update) {
	t.Helper()
	var planes [3][]byte
	var info api.SBufferInfo
	if state := c.decoder.Raw().DecodeFrameNoDelay(data, int32(len(data)), &planes, &info); state != api.DsErrorFree ||
		info.IBufferStatus != 1 {
		t.Fatalf("decode: state=%d status=%d", state, info.IBufferStatus)
	}
	lumaStride := int(info.UsrData.SSystemBuffer.IStride[0])
	chromaStride := int(info.UsrData.SSystemBuffer.IStride[1])
	regions := []image.Rectangle{c.surface.Rect}
	if update.Tracked && !avcKeyframe(data) {
		for _, move := range update.Copies {
			pixels := make([]byte, 0, move.Source.Dx()*move.Source.Dy()*4)
			for y := move.Source.Min.Y; y < move.Source.Max.Y; y++ {
				pixels = append(pixels, c.surface.Pix[c.surface.PixOffset(move.Source.Min.X, y):][:move.Source.Dx()*4]...)
			}
			for y := range move.Source.Dy() {
				copy(c.surface.Pix[c.surface.PixOffset(move.Destination.X, move.Destination.Y+y):],
					pixels[y*move.Source.Dx()*4:][:move.Source.Dx()*4])
			}
		}
		if update.Regions != nil {
			regions = update.Regions
		}
	}
	for _, rect := range regions {
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				luma := float64(planes[0][y*lumaStride+x])
				u := float64(planes[1][y/2*chromaStride+x/2]) - 128
				v := float64(planes[2][y/2*chromaStride+x/2]) - 128
				offset := c.surface.PixOffset(x, y)
				c.surface.Pix[offset] = clampByte(luma + 1.5748*v)
				c.surface.Pix[offset+1] = clampByte(luma - 0.1873*u - 0.4681*v)
				c.surface.Pix[offset+2] = clampByte(luma + 1.8556*u)
				c.surface.Pix[offset+3] = 255
			}
		}
	}
}

func avcKeyframe(data []byte) bool {
	for i := 0; i+3 < len(data); i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 && data[i+3]&31 == 5 {
			return true
		}
	}

	return false
}

func clampByte(value float64) byte {
	return byte(max(0, min(255, value+0.5)))
}

// worstBlockError is the largest mean absolute RGB error of any 16x16 block.
func worstBlockError(surface, source *image.RGBA) float64 {
	worst := 0.0
	for by := 0; by < modelHeight; by += 16 {
		for bx := 0; bx < modelWidth; bx += 16 {
			total := 0
			for y := by; y < by+16; y++ {
				for x := bx; x < bx+16; x++ {
					a, b := surface.Pix[surface.PixOffset(x, y):][:3], source.Pix[source.PixOffset(x, y):][:3]
					for i := range 3 {
						total += max(int(a[i])-int(b[i]), int(b[i])-int(a[i]))
					}
				}
			}
			worst = max(worst, float64(total)/(16*16*3))
		}
	}

	return worst
}

type modelResult struct {
	worst                   float64
	bytes, scrollBytes      int
	copies, forced, repaint int
}

// runModel replays one scripted session. Scroll frames move the document while
// other frames type into it, change the status bar or stay idle.
func runModel(t *testing.T, copies, bgrx bool, threads int) modelResult {
	t.Helper()
	rng := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // Reproducible desktop.
	desktop := newModelDesktop(rng)
	encoder, err := NewEncoderWithOptions(modelWidth, modelHeight, Options{Threads: threads, SurfaceCopies: copies})
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	decoder, err := openh264.NewDecoder(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	client := &modelClient{decoder: decoder, surface: image.NewRGBA(image.Rect(0, 0, modelWidth, modelHeight))}
	steps := []string{
		"idle", "scroll 18", "scroll 17", "scroll 1", "scroll 2", "scroll 3", "type", "scroll -40",
		"scroll 9 status", "page", "type", "idle", "scroll -17", "type", "scroll 120", "scroll -1",
	}
	random := [...]string{"scroll 6", "scroll 7", "type", "status", "idle", "scroll -12", "scroll 25"}
	for range 40 {
		steps = append(steps, random[rng.IntN(len(random))])
	}
	var result modelResult
	var previous *image.RGBA
	for frame := -1; frame < len(steps); frame++ {
		step := "first"
		if frame >= 0 {
			step = steps[frame]
		}
		var by int
		scrolled := false
		switch {
		case step == "page":
			desktop.scroll(modelPane().Dy() - 20)
			scrolled = true
		case step == "type":
			for range 12 {
				at := [2]int{modelPane().Min.X + 10 + rng.IntN(400), desktop.offset + rng.IntN(modelPane().Dy())}
				desktop.edits[at] = byte(rng.IntN(256))
			}
		case step == "status":
			desktop.status += 7
		case len(step) > 7 && step[:7] == "scroll ":
			if _, err := fmt.Sscan(step[7:], &by); err != nil {
				t.Fatal(err)
			}
			desktop.scroll(by)
			if len(step) > 7 && step[len(step)-6:] == "status" {
				desktop.status += 3
			}
			scrolled = true
		}
		current := desktop.draw()
		var damage []image.Rectangle
		force := previous == nil
		if !force {
			damage = modelDamage(previous, current)
		}
		var data []byte
		if bgrx {
			pix := make([]byte, len(current.Pix))
			for i := 0; i < len(pix); i += 4 {
				pix[i], pix[i+1], pix[i+2], pix[i+3] = current.Pix[i+2], current.Pix[i+1], current.Pix[i], 0
			}
			err = encoder.PrepareBGRXDamage(pix, current.Stride, modelWidth, modelHeight, damage, force)
		} else {
			err = encoder.PrepareDamage(current, damage, force)
		}
		if err != nil {
			t.Fatal(err)
		}
		if data, err = encoder.EncodePrepared(); err != nil {
			t.Fatal(err)
		}
		update := encoder.Update()
		result.copies += len(update.Copies)
		for index, forced := range encoder.surface.forced {
			if !update.Tracked {
				break
			}
			if forced {
				result.forced++
			}
			if encoder.surface.painted[index] && encoder.surface.stale[index] {
				result.repaint++
			}
		}
		if data != nil {
			client.present(t, data, update)
			encoder.Sent(true)
			result.bytes += len(data)
			if scrolled {
				result.scrollBytes += len(data)
			}
		}
		if worst := worstBlockError(client.surface, current); worst > result.worst {
			result.worst = worst
		}
		previous = current
	}

	return result
}

// The client model must keep showing the source within codec error while
// copies replace scrolled video and stale decoded blocks are never repainted.
func TestSurfaceCopiesClientModel(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		bgrx    bool
		threads int
	}{
		{"rgba", false, 1},
		{"bgrx", true, 1},
		{"bgrx-two-slices", true, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			baseline := runModel(t, false, test.bgrx, test.threads)
			copied := runModel(t, true, test.bgrx, test.threads)
			t.Logf("worst block error %.2f without copies, %.2f with; scroll bytes %d -> %d",
				baseline.worst, copied.worst, baseline.scrollBytes, copied.scrollBytes)
			t.Logf("copies=%d forced=%d repainted stale=%d", copied.copies, copied.forced, copied.repaint)
			if copied.worst > baseline.worst*1.5+2 {
				t.Fatalf("worst block error %.2f with copies, %.2f without", copied.worst, baseline.worst)
			}
			if copied.copies == 0 || copied.forced == 0 || copied.repaint == 0 {
				t.Fatalf("copies=%d forced=%d repainted stale=%d: scenario not exercised",
					copied.copies, copied.forced, copied.repaint)
			}
			if copied.scrollBytes >= baseline.scrollBytes {
				t.Fatalf("scroll frames used %d bytes with copies, %d without", copied.scrollBytes, baseline.scrollBytes)
			}
		})
	}
}

// BenchmarkSurfaceCopies scrolls the 1080p desktop pane by eight pixels each
// frame, alternating direction, with and without copies. The caller-side
// metrics show how many copies and bytes reach the client.
func BenchmarkSurfaceCopies(b *testing.B) {
	for _, copies := range []bool{false, true} {
		b.Run(fmt.Sprintf("1080p/Copies=%t", copies), func(b *testing.B) {
			frames, regions := slicedDesktopFrames(1920, 1080, true)
			encoder, err := NewEncoderWithOptions(1920, 1080, Options{Threads: 1, SurfaceCopies: copies})
			if err != nil {
				b.Fatal(err)
			}
			defer encoder.Close()
			if _, err := encoder.Encode(frames[0], true); err != nil {
				b.Fatal(err)
			}
			encoder.Sent(true)
			var bytes, copied, frame int
			b.ReportAllocs()
			for b.Loop() {
				frame++
				data, err := encoder.EncodeDamage(frames[frame%2], regions, false)
				if err != nil || len(data) == 0 {
					b.Fatalf("encode returned %d bytes, error %v", len(data), err)
				}
				copied += len(encoder.Update().Copies)
				encoder.Sent(true)
				bytes += len(data)
			}
			b.ReportMetric(float64(bytes)/float64(b.N), "bytes/frame")
			b.ReportMetric(float64(copied)/float64(b.N), "copies/frame")
		})
	}
}
