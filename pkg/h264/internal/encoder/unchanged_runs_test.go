package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"sync/atomic"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Runs, caller-goroutine slices and border-only padding replace per-block work
// with its known effects. Whatever they skip, the decoder must reproduce the
// encoder's reconstruction, and the reference must stay fully padded.
func TestUnchangedFastPathsDecodeToReconstruction(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{320, 240}, {176, 144}, {48, 32}, {16, 64}} {
		for _, sliceCount := range []int{1, 2, 4} {
			if sliceCount > size[1]/16 {
				continue
			}
			t.Run(fmt.Sprintf("%dx%d/%dSlices", size[0], size[1], sliceCount), func(t *testing.T) {
				t.Parallel()
				testUnchangedFastPathsDecodeToReconstruction(t, size[0], size[1], sliceCount)
			})
		}
	}
}

func testUnchangedFastPathsDecodeToReconstruction(t *testing.T, width, height, sliceCount int) {
	t.Helper()
	codec := newUnchangedParityCodec(t, width, height, sliceCount)
	dec := newTestDecoder(t)
	t.Cleanup(func() { dec.Uninitialize() })
	ctx := codec.m_pEncContext
	var macroblockInits, fullExpansions atomic.Int32
	rcInit := ctx.pFuncList.pfRc.pfWelsRcMbInit
	ctx.pFuncList.pfRc.pfWelsRcMbInit = func(c *sWelsEncCtx, mb *SMB, slice *SSlice) {
		macroblockInits.Add(1)
		rcInit(c, mb, slice)
	}
	expandLuma := ctx.pFuncList.sExpandPicFunc.PfExpandLumaPicture
	ctx.pFuncList.sExpandPicFunc.PfExpandLumaPicture = func(dst []uint8, off int, stride, w, h int32) {
		fullExpansions.Add(1)
		expandLuma(dst, off, stride, w, h)
	}

	rng := rand.New(rand.NewSource(int64(width*131 + height*7 + sliceCount)))
	planes := unchangedRunsBackground(width, height)
	var previous [3][]byte
	macroblocks := int32(width / 16 * (height / 16))
	var runFrames, borderOnlyFrames int
	for frame := range 48 {
		current := unchangedRunsNextFrame(rng, planes, width, height, frame)
		switch {
		case frame == 30:
			var output api.SFrameBSInfo
			if codec.EncodeFrame(nil, &output) == 0 {
				t.Fatal("invalid input succeeded")
			}
		case frame%17 == 16:
			codec.ForceIntraFrame(true, -1)
		}
		source := &api.SSourcePicture{IColorFormat: int32(api.VideoFormatI420), IPicWidth: int32(width),
			IPicHeight: int32(height), IStride: [4]int32{int32(width), int32(width / 2), int32(width / 2)},
			UiTimeStamp: int64(frame * 16)}
		copy(source.PData[:3], current[:])
		source.UnchangedMacroblocks = unchangedParityMask(width, height, previous, current)
		macroblockInits.Store(0)
		fullExpansions.Store(0)
		var output api.SFrameBSInfo
		if code := codec.EncodeFrame(source, &output); code != 0 {
			t.Fatalf("frame %d: encode failed: %d", frame, code)
		}
		if macroblockInits.Load() < macroblocks {
			runFrames++
		}
		if fullExpansions.Load() == 0 {
			borderOnlyFrames++
		}

		bitstream := unchangedFrameBytes(&output)
		decoded, info := decodeOne(t, dec, bitstream, int32(len(bitstream)))
		reconstruction := ctx.pCurDqLayer.pDecPic
		strides := [3]int{
			int(info.UsrData.SSystemBuffer.IStride[0]),
			int(info.UsrData.SSystemBuffer.IStride[1]), int(info.UsrData.SSystemBuffer.IStride[1]),
		}
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
			}
		}

		padded := bytes.Clone(reconstruction.pBuffer)
		var expanded [3][]byte
		for plane := range expanded {
			expanded[plane] = padded
		}
		common.ExpandReferencingPicture(expanded[:], reconstruction.iDataOff[:], reconstruction.iWidthInPixel,
			reconstruction.iHeightInPixel, reconstruction.iLineSize[:], common.ExpandPictureLuma_c,
			[2]common.PExpandPictureFunc{common.ExpandPictureChroma_c, common.ExpandPictureChroma_c})
		if !bytes.Equal(padded, reconstruction.pBuffer) {
			t.Fatalf("frame %d: reference padding differs from a full expansion", frame)
		}
		previous = current
	}
	if runFrames == 0 {
		t.Fatal("no frame coded an unchanged run")
	}
	if borderOnlyFrames == 0 && width >= 32 && height >= 32 {
		t.Fatal("no frame padded only its border")
	}
}

func unchangedRunsBackground(width, height int) [3][]byte {
	var planes [3][]byte
	for plane := range planes {
		w, h := width, height
		if plane > 0 {
			w, h = width/2, height/2
		}
		planes[plane] = make([]byte, w*h)
		for y := range h {
			for x := range w {
				planes[plane][y*w+x] = byte(70 + plane*25 + ((x/4+y/4)%7)*3)
			}
		}
	}

	return planes
}

// unchangedRunsNextFrame mutates planes in place: static frames, small
// rectangles that favor borders and corners, short scrolls and noise.
func unchangedRunsNextFrame(rng *rand.Rand, planes [3][]byte, width, height, frame int) [3][]byte {
	switch op := rng.Intn(10); {
	case frame == 0 || op < 3:
	case op < 7:
		for range 1 + rng.Intn(3) {
			bw, bh := 2+rng.Intn(20), 2+rng.Intn(20)
			x0, y0 := rng.Intn(width), rng.Intn(height)
			switch rng.Intn(5) {
			case 0:
				x0 = 0
			case 1:
				x0 = width - bw
			case 2:
				y0 = 0
			case 3:
				y0 = height - bh
			}
			value := byte(rng.Intn(256))
			for plane := range planes {
				shift, w, h := 0, width, height
				if plane > 0 {
					shift, w, h = 1, width/2, height/2
				}
				for y := max(0, y0>>shift); y < min(h, (y0+bh)>>shift); y++ {
					for x := max(0, x0>>shift); x < min(w, (x0+bw)>>shift); x++ {
						planes[plane][y*w+x] = value + byte(plane*40+x)
					}
				}
			}
		}
	case op < 9:
		rows := 1 + rng.Intn(6)
		for plane := range planes {
			w := width
			if plane > 0 {
				w = width / 2
			}
			copy(planes[plane], planes[plane][rows*w:])
		}
	default:
		for plane := range planes {
			for i := range planes[plane] {
				planes[plane][i] = byte(rng.Intn(256))
			}
		}
	}

	return [3][]byte{bytes.Clone(planes[0]), bytes.Clone(planes[1]), bytes.Clone(planes[2])}
}
