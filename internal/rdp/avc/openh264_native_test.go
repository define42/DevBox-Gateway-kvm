//go:build cgo && !purego

package avc

import (
	"bytes"
	"fmt"
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
				got, err := native.(damageCodecEncoder).encodeDamage(i420, force, timestamp, unchanged)
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
