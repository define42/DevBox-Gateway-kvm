//go:build cgo && !purego

package avc

import (
	"bytes"
	"fmt"
	"image"
	"testing"

	openh264 "github.com/define42/DevBox-Gateway-kvm/pkg/h264"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

// The native backend must produce exactly the pure-Go port's stream for the
// same pictures, including slices, forced IDRs, extreme quantizers and the
// unchanged-macroblock hints that the patched library shares with the port.
func TestNativeOpenH264MatchesPureGo(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		width, height, slices, qp int
		hinted                    bool
	}{
		{66, 50, 1, 0, false},
		{66, 50, 4, 20, false},
		{320, 192, 1, 0, false},
		{320, 192, 4, 20, false},
		{320, 192, 4, 51, false},
		{640, 360, 3, 32, false},
		{66, 50, 1, 0, true},
		{66, 50, 4, 20, true},
		{320, 192, 1, 26, true},
		{320, 192, 4, 51, true},
		{640, 360, 3, 32, true},
	} {
		name := fmt.Sprintf("%dx%d/%dSlices/QP%d/Hinted=%t", test.width, test.height, test.slices, test.qp, test.hinted)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := codecConfig{intraPeriod: codecIntraPeriod, loopFilterDisableIDC: codecLoopFilterDisableIDC}
			native, slices, err := newCodecEncoderWithConfig(test.width, test.height, test.slices, FrameRate, test.qp, config)
			if err != nil {
				t.Fatal(err)
			}
			defer native.close()
			param, err := pureGoEncoderParams(test.width, test.height, slices, FrameRate, test.qp, config)
			if err != nil {
				t.Fatal(err)
			}
			pure, err := openh264.NewEncoder(param)
			if err != nil {
				t.Fatal(err)
			}
			defer pure.Close()

			seed := uint32(test.width*131 + test.qp) // deterministic noise
			luma := test.width * test.height
			i420 := make([]byte, luma*3/2)
			for i := range i420 {
				i420[i] = byte(i*7 + i/test.width*3)
			}
			cols, rows := (test.width+15)/16, (test.height+15)/16
			for frame := range 40 {
				// Move a noisy block so frames mix static and changed areas;
				// some frames change nothing, some change chroma only.
				x, y := (frame*9)%(test.width-16), (frame*5)%(test.height-16)
				unchanged := make([]bool, cols*rows)
				for i := range unchanged {
					unchanged[i] = true
				}
				if frame%7 != 3 {
					for row := y; row < y+16; row++ {
						for col := x; col < x+16; col++ {
							seed = seed*1664525 + 1013904223
							if frame%5 == 2 {
								i420[luma+row/2*(test.width/2)+col/2] = byte(seed >> 24)
							} else {
								i420[row*test.width+col] = byte(seed >> 24)
							}
						}
					}
					for row := y / 16; row <= (y+15)/16; row++ {
						for col := x / 16; col <= (x+15)/16; col++ {
							unchanged[row*cols+col] = false
						}
					}
				}
				if !test.hinted || frame%13 == 6 {
					unchanged = nil
				}
				force := frame == 0 || frame == 11
				timestamp := int64(frame * 1000 / FrameRate)
				got, err := native.(damageCodecEncoder).encodeDamage(i420, force, timestamp, damageHints{unchanged: unchanged})
				if err != nil {
					t.Fatalf("frame %d: native: %v", frame, err)
				}
				if force {
					if err := pure.ForceIntraFrame(); err != nil {
						t.Fatal(err)
					}
				}
				want, frameType, err := pure.Encode(&openh264.Frame{
					Width: test.width, Height: test.height, Timestamp: timestamp,
					Y: i420[:luma], U: i420[luma : luma+luma/4], V: i420[luma+luma/4:],
					UnchangedMacroblocks: unchanged,
				})
				if err != nil || frameType == api.VideoFrameTypeSkip {
					t.Fatalf("frame %d: pure Go: type %d, %v", frame, frameType, err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("frame %d: native %d bytes differ from pure Go %d bytes", frame, len(got), len(want))
				}
			}
		})
	}
}

func TestNativeOpenH264Settings(t *testing.T) {
	t.Parallel()
	codec, slices, err := newCodecEncoder(320, 192, 4, FrameRate, 27)
	if err != nil {
		t.Fatal(err)
	}
	defer codec.close()
	native := codec.(softwareCodec)
	if _, err := native.encode(make([]byte, 320*192*3/2), true, 33); err != nil {
		t.Fatal(err)
	}
	settings, err := native.settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.slices != slices || settings.threads != 4 || settings.layerQP != 27 || settings.averageFrameQP != 27 ||
		settings.inputFrames != 1 || settings.lastTimestamp != 33 || settings.maxFrameRate != FrameRate {
		t.Fatalf("settings = %+v, slices %d", settings, slices)
	}
	if _, err := native.encode(make([]byte, 10), false, 0); err == nil {
		t.Fatal("short input accepted")
	}
	native.close()
}

// Motion hints change the coded stream, so both backends must apply them
// identically. Frames scroll a textured band; verified blocks get the scroll
// vector, and some blocks get deliberately wrong hints.
func TestNativeOpenH264MotionHintsMatchPureGo(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ width, height, slices, qp int }{
		{320, 192, 1, 0}, {320, 192, 4, 26}, {320, 192, 1, 51}, {640, 360, 3, 32}, {66, 50, 1, 20},
	} {
		t.Run(fmt.Sprintf("%dx%d/%dSlices/QP%d", test.width, test.height, test.slices, test.qp), func(t *testing.T) {
			t.Parallel()
			config := codecConfig{intraPeriod: codecIntraPeriod, loopFilterDisableIDC: codecLoopFilterDisableIDC}
			native, slices, err := newCodecEncoderWithConfig(test.width, test.height, test.slices, FrameRate, test.qp, config)
			if err != nil {
				t.Fatal(err)
			}
			defer native.close()
			param, err := pureGoEncoderParams(test.width, test.height, slices, FrameRate, test.qp, config)
			if err != nil {
				t.Fatal(err)
			}
			pure, err := openh264.NewEncoder(param)
			if err != nil {
				t.Fatal(err)
			}
			defer pure.Close()
			plain, _, err := newCodecEncoderWithConfig(test.width, test.height, test.slices, FrameRate, test.qp, config)
			if err != nil {
				t.Fatal(err)
			}
			defer plain.close()

			width, height := test.width, test.height
			luma := width * height
			cols, rows := (width+15)/16, (height+15)/16
			hintedFrames, plainBytes, hintedBytes := 0, 0, 0
			seed := uint32(width*7 + test.qp)
			// Low-amplitude noise over a gradient: structured enough that QP0
			// frames fit the encoder's bitstream buffer.
			random := func() byte {
				seed = seed*1664525 + 1013904223

				return byte(seed>>24)&0x0f + byte(seed>>12)&0x70
			}
			i420 := make([]byte, luma*3/2)
			for i := range i420 {
				i420[i] = byte(i/width*3) + random()&0x1f
			}
			previous := make([]byte, len(i420))
			band := image.Rect(0, 16, width, height-height/4)
			for frame := range 30 {
				copy(previous, i420)
				dy := []int{8, 16, -8, 6, 2, 24}[frame%6]
				scrollTestBand(i420, width, height, band, dy, frame%5 == 4, random)
				unchanged, motion := testMotionHints(previous, i420, width, height, dy)
				if frame%4 == 1 { // deliberately wrong and out-of-range hints
					motion[(frame*7)%(cols*rows)] = api.MacroblockMotion{X: 4, Y: -2}
					motion[(frame*11)%(cols*rows)] = api.MacroblockMotion{X: 0, Y: 30000}
				}
				hints := damageHints{unchanged: unchanged, motion: motion}
				if frame%9 == 5 {
					hints = damageHints{}
				}
				force := frame == 0 || frame == 17
				timestamp := int64(frame * 1000 / FrameRate)
				got, err := native.(damageCodecEncoder).encodeDamage(i420, force, timestamp, hints)
				if err != nil {
					t.Fatalf("frame %d: native: %v", frame, err)
				}
				if force {
					if err := pure.ForceIntraFrame(); err != nil {
						t.Fatal(err)
					}
				}
				want, _, err := pure.Encode(&openh264.Frame{
					Width: width, Height: height, Timestamp: timestamp,
					Y: i420[:luma], U: i420[luma : luma+luma/4], V: i420[luma+luma/4:],
					UnchangedMacroblocks: hints.unchanged, MacroblockMotion: hints.motion,
				})
				if err != nil {
					t.Fatalf("frame %d: pure Go: %v", frame, err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("frame %d: native %d bytes differ from pure Go %d bytes", frame, len(got), len(want))
				}
				unhinted, err := plain.encode(i420, force, timestamp)
				if err != nil {
					t.Fatalf("frame %d: unhinted: %v", frame, err)
				}
				if !bytes.Equal(got, unhinted) {
					hintedFrames++
				}
				plainBytes += len(unhinted)
				hintedBytes += len(got)
			}
			// The plain encoder's references diverge after the first hinted
			// frame, so this only shows that the hint path ran.
			if hintedFrames == 0 {
				t.Fatal("motion hints never changed the coded stream")
			}
			t.Logf("%d hinted frames, %d bytes with hints, %d without", hintedFrames, hintedBytes, plainBytes)
		})
	}
}

// scrollTestBand moves the band's rows by dy (new row y shows old row y+dy)
// and fills the uncovered rows with fresh texture. chromaOnly also tints a
// chroma block below the band.
func scrollTestBand(i420 []byte, width, height int, band image.Rectangle, dy int, chromaOnly bool, random func() byte) {
	luma := width * height
	planes := []struct{ offset, stride, scale int }{{0, width, 1}, {luma, width / 2, 2}, {luma + luma/4, width / 2, 2}}
	for _, plane := range planes {
		top, bottom := band.Min.Y/plane.scale, band.Max.Y/plane.scale
		shift := dy / plane.scale
		old := append([]byte(nil), i420[plane.offset+top*plane.stride:plane.offset+bottom*plane.stride]...)
		for y := top; y < bottom; y++ {
			row := i420[plane.offset+y*plane.stride : plane.offset+(y+1)*plane.stride]
			if source := y + shift; source >= top && source < bottom {
				copy(row, old[(source-top)*plane.stride:(source-top+1)*plane.stride])
			} else {
				for x := range row {
					row[x] = random()
				}
			}
		}
	}
	if chromaOnly {
		i420[luma+(height/2-2)*(width/2)+3] ^= 0x55
	}
}

// testMotionHints marks blocks equal to the previous picture as unchanged and
// blocks equal to it displaced by (0, dy) with that motion.
func testMotionHints(previous, current []byte, width, height, dy int) ([]bool, []api.MacroblockMotion) {
	cols, rows := (width+15)/16, (height+15)/16
	unchanged := make([]bool, cols*rows)
	motion := make([]api.MacroblockMotion, cols*rows)
	for my := range rows {
		for mx := range cols {
			switch {
			case testBlockMatches(previous, current, width, height, mx, my, 0):
				unchanged[my*cols+mx] = true
			case testBlockMatches(previous, current, width, height, mx, my, dy):
				motion[my*cols+mx] = api.MacroblockMotion{Y: int16(dy)}
			}
		}
	}

	return unchanged, motion
}

func testBlockMatches(previous, current []byte, width, height, mx, my, dy int) bool {
	luma := width * height
	planes := []struct{ offset, stride, w, h, scale int }{
		{0, width, width, height, 1},
		{luma, width / 2, width / 2, height / 2, 2},
		{luma + luma/4, width / 2, width / 2, height / 2, 2},
	}
	for _, plane := range planes {
		size := 16 / plane.scale
		shift := dy / plane.scale
		if dy%plane.scale != 0 {
			return false
		}
		for y := my * size; y < (my+1)*size && y < plane.h; y++ {
			source := y + shift
			if source < 0 || source >= plane.h {
				return false
			}
			for x := mx * size; x < (mx+1)*size && x < plane.w; x++ {
				if current[plane.offset+y*plane.stride+x] != previous[plane.offset+source*plane.stride+x] {
					return false
				}
			}
		}
	}

	return true
}
