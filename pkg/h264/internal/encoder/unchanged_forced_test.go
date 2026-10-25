package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Forced macroblocks repeat the previously decoded block whatever their
// source, unless their motion predictor rules out a zero-motion skip. Either
// way the decoder must reproduce the encoder's reconstruction everywhere.
func TestForcedMacroblocksRepeatReference(t *testing.T) {
	t.Parallel()
	for _, sliceCount := range []int{1, 2} {
		t.Run(fmt.Sprintf("%dSlices", sliceCount), func(t *testing.T) {
			t.Parallel()
			const width, height = 128, 96
			codec := newUnchangedParityCodec(t, width, height, sliceCount)
			dec := newTestDecoder(t)
			t.Cleanup(func() { dec.Uninitialize() })
			rng := rand.New(rand.NewSource(int64(sliceCount)))
			planes := unchangedRunsBackground(width, height)
			var previous, previousDecoded [3][]byte
			repeated, coded := 0, 0
			for frame := range 32 {
				current := unchangedRunsNextFrame(rng, planes, width, height, frame)
				mask := unchangedParityMask(width, height, previous, current)
				var forced []bool
				if frame >= 2 && frame%4 != 0 {
					forced = make([]bool, len(mask))
					for mb := range forced {
						if rng.Intn(3) == 0 {
							forced[mb], mask[mb] = true, false
						}
					}
				}
				source := &api.SSourcePicture{IColorFormat: int32(api.VideoFormatI420), IPicWidth: width,
					IPicHeight: height, IStride: [4]int32{width, width / 2, width / 2}, UiTimeStamp: int64(frame * 16),
					UnchangedMacroblocks: mask, ForcedMacroblocks: forced}
				copy(source.PData[:3], current[:])
				var output api.SFrameBSInfo
				if code := codec.EncodeFrame(source, &output); code != 0 {
					t.Fatalf("frame %d: encode failed: %d", frame, code)
				}
				bitstream := unchangedFrameBytes(&output)
				decoded, info := decodeOne(t, dec, bitstream, int32(len(bitstream)))
				strides := [3]int{int(info.UsrData.SSystemBuffer.IStride[0]),
					int(info.UsrData.SSystemBuffer.IStride[1]), int(info.UsrData.SSystemBuffer.IStride[1])}
				reconstruction := codec.m_pEncContext.pCurDqLayer.pDecPic
				var picture [3][]byte
				for plane := range 3 {
					w, h := width, height
					if plane > 0 {
						w, h = width/2, height/2
					}
					for y := range h {
						got := decoded[plane][y*strides[plane]:][:w]
						want := reconstruction.pData[plane][reconstruction.iDataOff[plane]+y*int(reconstruction.iLineSize[plane]):][:w]
						if !bytes.Equal(got, want) {
							t.Fatalf("frame %d: decoded plane %d row %d differs from the encoder reconstruction", frame, plane, y)
						}
						picture[plane] = append(picture[plane], got...)
					}
				}
				for mb, force := range forced {
					if !force {
						continue
					}
					if macroblockEqual(picture, previousDecoded, width, mb) {
						repeated++
					} else {
						coded++
					}
				}
				previous, previousDecoded = current, picture
			}
			t.Logf("forced macroblocks: %d repeated, %d coded normally", repeated, coded)
			if repeated == 0 || repeated < coded {
				t.Fatalf("forced macroblocks repeated %d times and fell back %d times", repeated, coded)
			}
		})
	}
}

// macroblockEqual compares one macroblock of two tightly packed I420 pictures.
func macroblockEqual(a, b [3][]byte, width, mb int) bool {
	columns := width / 16
	for plane := range 3 {
		stride, block := width, 16
		if plane > 0 {
			stride, block = width/2, 8
		}
		off := (mb/columns)*block*stride + (mb%columns)*block
		for y := range block {
			if !bytes.Equal(a[plane][off+y*stride:][:block], b[plane][off+y*stride:][:block]) {
				return false
			}
		}
	}

	return true
}

// A forced shift codes interior blocks of a moved area as P_SKIPs carrying the
// motion: the decoder moves its previous picture, exactly in luma, as a client
// copying the area would.
func TestForcedMacroblocksFollowShift(t *testing.T) {
	t.Parallel()
	for _, shift := range []int{6, 7, -5, -16, 1} {
		t.Run(fmt.Sprintf("shift%d", shift), func(t *testing.T) {
			t.Parallel()
			const width, height = 128, 128
			codec := newUnchangedParityCodec(t, width, height, 1)
			dec := newTestDecoder(t)
			t.Cleanup(func() { dec.Uninitialize() })
			rng := rand.New(rand.NewSource(int64(shift + 100)))
			var tall [3][]byte
			for plane := range tall {
				w, h := width, 4*height
				if plane > 0 {
					w, h = width/2, 2*height
				}
				tall[plane] = make([]byte, w*h)
				for i := range tall[plane] {
					tall[plane][i] = byte(60 + rng.Intn(140))
				}
			}
			offset := 2 * height
			view := func() [3][]byte {
				var planes [3][]byte
				for plane := range planes {
					w, h, o := width, height, offset
					if plane > 0 {
						w, h, o = width/2, height/2, offset/2
					}
					planes[plane] = append([]byte(nil), tall[plane][o*w:(o+h)*w]...)
				}
				return planes
			}
			var previous, previousDecoded [3][]byte
			moved := 0
			for frame := range 8 {
				mask := make([]bool, width/16*(height/16))
				var forced []bool
				var shifts []int16
				if frame >= 2 {
					offset += shift
					forced = make([]bool, len(mask))
					shifts = make([]int16, len(mask))
					for mb := range forced {
						forced[mb], shifts[mb] = true, int16(shift)
					}
				} else if frame == 1 {
					for mb := range mask {
						mask[mb] = true
					}
				}
				current := view()
				source := &api.SSourcePicture{IColorFormat: int32(api.VideoFormatI420), IPicWidth: width,
					IPicHeight: height, IStride: [4]int32{width, width / 2, width / 2}, UiTimeStamp: int64(frame * 16),
					UnchangedMacroblocks: mask, ForcedMacroblocks: forced, ForcedShift: shifts}
				copy(source.PData[:3], current[:])
				var output api.SFrameBSInfo
				if code := codec.EncodeFrame(source, &output); code != 0 {
					t.Fatalf("frame %d: encode failed: %d", frame, code)
				}
				bitstream := unchangedFrameBytes(&output)
				decoded, info := decodeOne(t, dec, bitstream, int32(len(bitstream)))
				strides := [3]int{int(info.UsrData.SSystemBuffer.IStride[0]),
					int(info.UsrData.SSystemBuffer.IStride[1]), int(info.UsrData.SSystemBuffer.IStride[1])}
				reconstruction := codec.m_pEncContext.pCurDqLayer.pDecPic
				var picture [3][]byte
				for plane := range 3 {
					w, h := width, height
					if plane > 0 {
						w, h = width/2, height/2
					}
					for y := range h {
						got := decoded[plane][y*strides[plane]:][:w]
						want := reconstruction.pData[plane][reconstruction.iDataOff[plane]+y*int(reconstruction.iLineSize[plane]):][:w]
						if !bytes.Equal(got, want) {
							t.Fatalf("frame %d: decoded plane %d row %d differs from the encoder reconstruction", frame, plane, y)
						}
						picture[plane] = append(picture[plane], got...)
					}
				}
				if forced != nil {
					// Interior blocks, away from the predictor's zero-motion edges
					// and from rows the shift brings in from outside the picture.
					for mbY := 1; mbY < height/16-1; mbY++ {
						for mbX := 1; mbX < width/16; mbX++ {
							y0 := mbY * 16
							if y0+shift < 0 || y0+16+shift > height {
								continue
							}
							equal := true
							for y := y0; y < y0+16 && equal; y++ {
								equal = bytes.Equal(picture[0][y*width+mbX*16:][:16], previousDecoded[0][(y+shift)*width+mbX*16:][:16])
							}
							if equal {
								moved++
							}
						}
					}
				}
				previous, previousDecoded = current, picture
			}
			_ = previous
			if moved == 0 {
				t.Fatal("no forced block repeated its shifted reference")
			}
		})
	}
}

// Unsupported shifts must fall back before either forced path reads motion
// compensation buffers, including values that would wrap in quarter-pel form.
func TestForcedMotionRejectsOutOfRangeShifts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		shift      int16
		rangeLimit int32
	}{
		{-32768, 64}, {-600, 64}, {-api.MaxForcedShift - 1, 64},
		{api.MaxForcedShift + 1, 64}, {600, 64}, {32767, 64},
		{-33, 32}, {33, 32},
	} {
		t.Run(fmt.Sprintf("shift%d/range%d", test.shift, test.rangeLimit), func(t *testing.T) {
			t.Parallel()
			ctx, slice, original := unchangedTestMacroblock()
			ctx.iMvRange = test.rangeLimit
			layer := ctx.pCurDqLayer
			const columns, rows, index = 3, 96, 40*3 + 1
			layer.iMbWidth, layer.iMbHeight = columns, rows
			blocks := make([]SMB, columns*rows)
			blocks[index] = *original
			mb := &blocks[index]
			mb.iMbXY, mb.iMbX, mb.iMbY = index, 1, 40
			mb.pMbList = blocks
			mb.uiNeighborAvail = LEFT_MB_POS | TOP_MB_POS
			layer.sMbDataP = blocks
			ctx.unchanged.macroblocks = make([]bool, len(blocks))
			ctx.unchanged.forced = make([]bool, len(blocks))
			ctx.unchanged.forcedShift = make([]int16, len(blocks))
			ctx.unchanged.forced[index] = true
			ctx.unchanged.forcedShift[index] = test.shift
			// Matching neighbors otherwise permit a forced run. The untouched
			// reconstruction buffers deliberately remain those of the small
			// fixture: a rejected hint must never access its shifted samples.
			mv := SMVUnitXY{iMvY: int16(4 * int32(test.shift))}
			for _, neighbor := range []int{index - 1, index - columns} {
				blocks[neighbor].uiMbType = common.MB_TYPE_SKIP
				blocks[neighbor].pRefIndex = make([]int8, 4)
				blocks[neighbor].sMv = make([]SMVUnitXY, 16)
				fillUnchanged(blocks[neighbor].sMv, mv)
			}
			var md SWelsMD
			if tryForcedMotionMacroblock(ctx, &md, slice, mb) {
				t.Fatal("out-of-range shift encoded a forced macroblock")
			}
			if count, next := encodeForcedMotionRun(ctx, &md, slice, index, 1); count != 0 || next != index {
				t.Fatalf("out-of-range shift encoded a run: count=%d next=%d", count, next)
			}
		})
	}
}

func TestForcedShiftLimitFitsEveryLevel(t *testing.T) {
	t.Parallel()
	for _, level := range common.G_ksLevelLimits {
		param := unchangedTestParams()
		param.SSpatialLayers[0].UiLevelIdc = level.UiLevelIdc
		var motionRange, differenceRange int32
		GetMvMvdRange(param, &motionRange, &differenceRange)
		ctx := &sWelsEncCtx{iMvRange: motionRange}
		for _, shift := range []int16{-api.MaxForcedShift, api.MaxForcedShift} {
			if !validForcedShift(ctx, shift) {
				t.Fatalf("level %d rejects public shift limit %d with motion range %d", level.UiLevelIdc, shift, motionRange)
			}
		}
	}
}

// An invalid public hint must produce the same bitstream and reconstruction
// as normal source encoding, including after the reference has become a P frame.
func TestOversizedForcedShiftsFallBackToSource(t *testing.T) {
	t.Parallel()
	for _, shift := range []int16{-32768, -600, -api.MaxForcedShift - 1, api.MaxForcedShift + 1, 600, 32767} {
		t.Run(fmt.Sprintf("shift%d", shift), func(t *testing.T) {
			t.Parallel()
			const width, height = 128, 1536
			codecs := [2]*CWelsH264SVCEncoder{
				newUnchangedParityCodec(t, width, height, 1),
				newUnchangedParityCodec(t, width, height, 1),
			}
			const count = width / 16 * (height / 16)
			unchanged, forced, shifts := make([]bool, count), make([]bool, count), make([]int16, count)
			for index := range forced {
				forced[index], shifts[index] = true, shift
			}
			for frame := range 4 {
				pixels := unchangedParityPixels(width, height, frame+4)
				var streams [2][]byte
				for i, codec := range codecs {
					source := &api.SSourcePicture{IColorFormat: int32(api.VideoFormatI420),
						IPicWidth: width, IPicHeight: height, IStride: [4]int32{width, width / 2, width / 2},
						UiTimeStamp: int64(frame * 16), UnchangedMacroblocks: unchanged}
					copy(source.PData[:3], pixels[:])
					if i == 1 && frame > 0 {
						source.ForcedMacroblocks, source.ForcedShift = forced, shifts
					}
					var output api.SFrameBSInfo
					if code := codec.EncodeFrame(source, &output); code != 0 {
						t.Fatalf("frame %d codec %d: encode failed: %d", frame, i, code)
					}
					streams[i] = unchangedFrameBytes(&output)
				}
				if !bytes.Equal(streams[0], streams[1]) {
					t.Fatalf("frame %d: oversized shift changed the bitstream", frame)
				}
				want, got := codecs[0].m_pEncContext.pCurDqLayer.pDecPic, codecs[1].m_pEncContext.pCurDqLayer.pDecPic
				if !bytes.Equal(want.pBuffer, got.pBuffer) {
					t.Fatalf("frame %d: oversized shift changed the reconstruction", frame)
				}
			}
		})
	}
}
