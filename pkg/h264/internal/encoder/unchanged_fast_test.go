package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestUnchangedEarlyPredictorMatchesFullCache(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(42))
	mbs := make([]SMB, 9)
	costs := make([]int32, len(mbs))
	for i := range mbs {
		mbs[i] = SMB{iMbXY: int32(i), pMbList: mbs, pSadCost: &costs[i],
			sMv: make([]SMVUnitXY, 16), pRefIndex: make([]int8, 4)}
	}
	mb := &mbs[4]
	var proven, fallback int
	for range 4000 {
		mb.uiNeighborAvail = uint8(rng.Intn(16))
		for i := range mbs {
			mbs[i].uiMbType = []uint32{common.MB_TYPE_SKIP, common.MB_TYPE_16x16, common.MB_TYPE_INTRA4x4}[rng.Intn(3)]
			for j := range mbs[i].sMv {
				mbs[i].sMv[j] = SMVUnitXY{int16(rng.Intn(3) - 1), int16(rng.Intn(3) - 1)}
			}
			for j := range mbs[i].pRefIndex {
				mbs[i].pRefIndex[j] = int8(rng.Intn(3) - 1)
			}
		}
		cache := SMbCache{pEncSad: make([]int32, len(mbs)), iEncSadOff: 4}
		FillNeighborCacheInterWithoutBGD(&cache, mb, 3, nil, 4)
		var predicted SMVUnitXY
		PredSkipMv(&cache, &predicted)
		if unchangedSkipPredictorIsZero(mb, 3) {
			proven++
			if predicted != (SMVUnitXY{}) {
				t.Fatalf("early zero predictor disagrees with full predictor: availability=%x mv=%v", mb.uiNeighborAvail, predicted)
			}
		} else {
			fallback++
		}
	}
	if proven == 0 || fallback == 0 {
		t.Fatalf("missing predictor coverage: proven=%d fallback=%d", proven, fallback)
	}
}

func TestUnchangedReconstructionProvenance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*unchangedFrameState, *SPicture)
	}{
		{name: "different generation", change: func(s *unchangedFrameState, _ *SPicture) { s.pictures[0].generation++ }},
		{name: "unknown source", change: func(s *unchangedFrameState, _ *SPicture) { s.source.picture = &SPicture{} }},
		{name: "new allocation", change: func(_ *unchangedFrameState, p *SPicture) { p.pData[0] = bytes.Clone(p.pData[0]) }},
		{name: "changed origin", change: func(_ *unchangedFrameState, p *SPicture) { p.iDataOff[0]++ }},
		{name: "changed stride", change: func(_ *unchangedFrameState, p *SPicture) { p.iLineSize[1]++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			picture := AllocPicture(32, 32, true, 0)
			version := pictureVersion(picture, 7)
			state := unchangedFrameState{source: version, pictures: [2]reconstructedPictureVersion{version}}
			// Recycling reference metadata must not constitute a new pixel generation.
			picture.iFramePoc, picture.iFrameNum = 100, 200
			if state.reusableSource() != picture {
				t.Fatal("POC changes incorrectly discarded independent pixel provenance")
			}
			test.change(&state, picture)
			if state.reusableSource() != nil {
				t.Fatal("uncertain reconstruction provenance allowed copy elision")
			}
		})
	}
}

func TestUnchangedReconstructionCopyElisionEngages(t *testing.T) {
	t.Parallel()
	ctx, slice, mb := unchangedTestMacroblock()
	destination := ctx.pCurDqLayer.pDecPic
	copy(destination.pBuffer, ctx.pCurDqLayer.pRefPic.pBuffer)
	version := pictureVersion(destination, 1)
	ctx.unchanged.source, ctx.unchanged.pictures[0] = version, version
	ctx.unchanged.finalZeroSkips = []bool{false, true, false, false}
	ctx.unchanged.begin(ctx, unchangedTestSource())
	if ctx.unchanged.reuseDestination != destination {
		t.Fatal("matching destination generation did not enable reuse")
	}
	before := bytes.Clone(destination.pBuffer)
	// A successful elision does not read the reconstructed reference planes.
	// The allocation provenance and destination samples remain valid.
	slice.sMbCacheInfo.SPicData.pRefMb = [3][]byte{}
	if !tryUnchangedMacroblock(ctx, &SWelsMD{}, slice, mb) {
		t.Fatal("eligible zero-motion skip did not engage")
	}
	if !bytes.Equal(before, destination.pBuffer) {
		t.Fatal("copy elision modified the resident reconstruction")
	}
}

func TestUnchangedNonzeroFinalSkipStillCopies(t *testing.T) {
	t.Parallel()
	ctx, slice, mb := unchangedTestMacroblock()
	layer := ctx.pCurDqLayer
	version := pictureVersion(layer.pDecPic, 1)
	ctx.unchanged.source, ctx.unchanged.pictures[0] = version, version
	// A refined P_SKIP can have a nonzero final MV and a zero integer-search
	// winner in sMvList. That permits the existing hint, but not copy elision.
	layer.iMbWidth, layer.iMbHeight = 2, 2
	layer.sMbDataP = make([]SMB, 4)
	layer.sMbDataP[mb.iMbXY] = SMB{uiMbType: common.MB_TYPE_SKIP, sMv: []SMVUnitXY{{iMvX: 1}}}
	ctx.unchanged.captureFinalSkipMotion(layer, true)
	ctx.unchanged.begin(ctx, unchangedTestSource())
	if ctx.unchanged.reuseDestination == nil || ctx.unchanged.finalZeroSkips[mb.iMbXY] {
		t.Fatal("fixture did not distinguish picture provenance from final skip motion")
	}
	if !tryUnchangedMacroblock(ctx, &SWelsMD{}, slice, mb) {
		t.Fatal("additional proof incorrectly changed existing hint eligibility")
	}
	data := &slice.sMbCacheInfo.SPicData
	for plane := range 3 {
		block := 8
		if plane == 0 {
			block = 16
		}
		for y := range block {
			dst := data.iCsMbOff[plane] + y*int(layer.iCsStride[plane])
			src := data.iRefMbOff[plane] + y*int(layer.pRefPic.iLineSize[plane])
			if !bytes.Equal(data.pCsMb[plane][dst:dst+block], data.pRefMb[plane][src:src+block]) {
				t.Fatal("nonzero final skip incorrectly elided reconstruction copying")
			}
		}
	}
}

func TestUnchangedFastPathsMatchFullEncoding(t *testing.T) {
	t.Parallel()
	for _, sliceCount := range []int{1, 2, 4} {
		t.Run(fmt.Sprintf("%dSlices", sliceCount), func(t *testing.T) {
			t.Parallel()
			testUnchangedFastPathsMatchFullEncoding(t, sliceCount)
		})
	}
}

func testUnchangedFastPathsMatchFullEncoding(t *testing.T, sliceCount int) {
	t.Helper()
	const width, height = 128, 96
	codecs := [2]*CWelsH264SVCEncoder{
		newUnchangedParityCodec(t, width, height, sliceCount),
		newUnchangedParityCodec(t, width, height, sliceCount),
	}
	var cacheCalls atomic.Int32
	fastCtx := codecs[1].m_pEncContext
	fillCache := fastCtx.pFuncList.pfFillInterNeighborCache
	fastCtx.pFuncList.pfFillInterNeighborCache = func(c *SMbCache, m *SMB, w int32, flags []int8, off int) {
		cacheCalls.Add(1)
		fillCache(c, m, w, flags, off)
	}
	var previous [3][]byte
	for frame := range 24 {
		pixels := unchangedParityPixels(width, height, frame)
		mask := unchangedParityMask(width, height, previous, pixels)
		if frame == 11 {
			for _, codec := range codecs {
				var output api.SFrameBSInfo
				if codec.EncodeFrame(nil, &output) == 0 {
					t.Fatal("invalid input succeeded")
				}
				if codec.m_pEncContext.unchanged.generation != 0 {
					t.Fatal("failed encode retained reconstruction generations")
				}
			}
		}
		if frame == 15 {
			for _, codec := range codecs {
				codec.ForceIntraFrame(true, -1)
			}
		}
		if frame == 19 {
			for _, codec := range codecs {
				var param api.SEncParamExt
				if codec.GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &param) != 0 {
					t.Fatal("get encoding parameters failed")
				}
				param.SSpatialLayers[0].IDLayerQp = 24
				if codec.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &param) != 0 {
					t.Fatal("change quantizer failed")
				}
			}
		}
		var outputs [2][]byte
		cacheCalls.Store(0)
		for i, codec := range codecs {
			source := &api.SSourcePicture{IColorFormat: int32(api.VideoFormatI420), IPicWidth: width,
				IPicHeight: height, IStride: [4]int32{width, width / 2, width / 2}, UiTimeStamp: int64(frame * 16)}
			copy(source.PData[:3], pixels[:])
			if i == 1 {
				source.UnchangedMacroblocks = mask
			}
			var output api.SFrameBSInfo
			if code := codec.EncodeFrame(source, &output); code != 0 {
				t.Fatalf("frame %d codec %d: encode failed: %d", frame, i, code)
			}
			outputs[i] = unchangedFrameBytes(&output)
			ctx := codec.m_pEncContext
			if ctx.unchanged.macroblocks != nil || ctx.unchanged.reuseDestination != nil {
				t.Fatal("successful frame retained borrowed/transient unchanged state")
			}
			if frame == 15 && ctx.unchanged.source.picture != nil {
				t.Fatal("IDR retained reconstruction source provenance")
			}
			for _, slice := range ctx.pCurDqLayer.ppSliceInLayer {
				if slice != nil && (!reflect.ValueOf(slice.sMdScratch).IsZero() || !reflect.ValueOf(slice.sDssScratch).IsZero()) {
					t.Fatal("slice scratch retained frame state after encoding")
				}
			}
		}
		if !bytes.Equal(outputs[0], outputs[1]) {
			t.Fatalf("frame %d bitstream differs with hints: normal=%d bytes hinted=%d bytes",
				frame, len(outputs[0]), len(outputs[1]))
		}
		want, got := codecs[0].m_pEncContext.pCurDqLayer.pDecPic, codecs[1].m_pEncContext.pCurDqLayer.pDecPic
		if !bytes.Equal(want.pBuffer, got.pBuffer) {
			t.Fatalf("frame %d reconstruction differs with hints", frame)
		}
		if frame == 3 && cacheCalls.Load() != 0 {
			t.Fatalf("stable frame still built %d full neighbor caches", cacheCalls.Load())
		}
		previous = pixels
	}
}

func newUnchangedParityCodec(t *testing.T, width, height, sliceCount int) *CWelsH264SVCEncoder {
	t.Helper()
	codec := NewCWelsH264SVCEncoder()
	t.Cleanup(func() { codec.Uninitialize() })
	param := unchangedTestParams().SEncParamExt
	param.IPicWidth, param.IPicHeight, param.FMaxFrameRate = int32(width), int32(height), 60
	param.IMultipleThreadIdc = uint16(sliceCount)
	layer := &param.SSpatialLayers[0]
	layer.IVideoWidth, layer.IVideoHeight, layer.FFrameRate = int32(width), int32(height), 60
	layer.SSliceArgument.UiSliceNum = uint32(sliceCount)
	if sliceCount > 1 {
		layer.SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
	}
	param.BUseLoadBalancing, param.BEnableSceneChangeDetect, param.BEnableFrameSkip = false, false, false
	if code := codec.InitializeExt(&param); code != 0 {
		t.Fatalf("initialize: %d", code)
	}
	return codec
}

func unchangedFrameBytes(output *api.SFrameBSInfo) []byte {
	var data []byte
	for _, layer := range output.SLayerInfo[:output.ILayerNum] {
		size := int32(0)
		for _, length := range layer.PNalLengthInByte[:layer.INalCount] {
			size += length
		}
		data = append(data, layer.PBsBuf[:size]...)
	}
	return data
}

func unchangedParityPixels(width, height, frame int) [3][]byte {
	var pixels [3][]byte
	for plane := range pixels {
		w, h, block := width, height, 16
		if plane > 0 {
			w, h, block = width/2, height/2, 8
		}
		pixels[plane] = make([]byte, w*h)
		shift := 0
		if frame >= 8 {
			shift = block / 4
		}
		for y := range h {
			for x := range w {
				pixels[plane][y*w+x] = byte(70 + plane*25 + (((x+shift)/4+y/4)%7)*3)
			}
		}
		if frame >= 4 && frame < 8 || frame >= 20 {
			left, top := (frame%6)*block, ((frame/2)%5)*block
			for y := top; y < top+block; y++ {
				for x := left; x < left+block; x++ {
					pixels[plane][y*w+x] = byte(130 + plane*9)
				}
			}
		}
	}
	return pixels
}

func unchangedParityMask(width, height int, previous, current [3][]byte) []bool {
	mask := make([]bool, width/16*(height/16))
	if previous[0] == nil {
		return mask
	}
	for mb := range mask {
		mask[mb] = true
		for plane := range current {
			stride, block := width, 16
			if plane > 0 {
				stride, block = width/2, 8
			}
			off := (mb/(width/16))*block*stride + (mb%(width/16))*block
			for row := range block {
				start := off + row*stride
				if !bytes.Equal(previous[plane][start:start+block], current[plane][start:start+block]) {
					mask[mb] = false
				}
			}
		}
	}
	return mask
}
