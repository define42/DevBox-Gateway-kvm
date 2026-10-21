package encoder

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

func sourceCopyFixture(width, height int32) (*CWelsPreProcess, *api.SSourcePicture) {
	param := unchangedTestParams()
	param.IPicWidth, param.IPicHeight = width, height
	param.sDependencyLayers[0].iActualWidth, param.sDependencyLayers[0].iActualHeight = width, height
	param.SUsedPicRect = SUsedPicRect{iWidth: width, iHeight: height}
	ctx := &sWelsEncCtx{pSvcParam: param}
	p := &CWelsPreProcess{m_pEncCtx: ctx}
	ctx.pVpp = p
	p.vaaUnchanged.enabled = true
	src := &api.SSourcePicture{IColorFormat: int32(api.VideoFormatI420), IPicWidth: width, IPicHeight: height}
	for plane := range 3 {
		w, h := int(width), int(height)
		if plane != 0 {
			w, h = w/2, h/2
		}
		stride := w + 7
		src.IStride[plane] = int32(stride)
		// Deliberately omit padding after the final row.
		src.PData[plane] = make([]byte, (h-1)*stride+w)
		for i := range src.PData[plane] {
			src.PData[plane][i] = byte(i*13 + plane*59)
		}
	}
	return p, src
}

func sourceCopyFrame(t *testing.T, p *CWelsPreProcess, src *api.SSourcePicture, dst *SPicture, mask []bool, frame int32) bool {
	t.Helper()
	p.vaaUnchanged.mask = mask
	if code := p.WelsMoveMemoryWrapper(p.m_pEncCtx.pSvcParam, dst, src, src.IPicWidth, src.IPicHeight); code != 0 {
		t.Fatalf("source copy: %d", code)
	}
	partial := p.sourceCopy.partial
	dst.iFramePoc = frame * 2
	p.vaaUnchanged.reference, p.vaaUnchanged.poc = dst, dst.iFramePoc
	p.m_pLastSpatialPicture[0][0] = dst
	p.sourceCopy.finish(p, &api.SFrameBSInfo{EFrameType: api.VideoFrameTypeP})
	p.vaaUnchanged.mask = nil
	return partial
}

func TestSourceCopyAccumulatesDamageAcrossResidentBuffers(t *testing.T) {
	t.Parallel()
	p, src := sourceCopyFixture(66, 50)
	var pictures [3]*SPicture
	for i := range pictures {
		pictures[i] = AllocPicture(80, 64, false, 0)
	}
	want := AllocPicture(80, 64, false, 0)
	// Three buffers and uneven reuse intervals exercise more than N-1 damage.
	order := []int{0, 1, 2, 0, 2, 1, 0, 1, 2, 0}
	partials := 0
	for frame, index := range order {
		mask := make([]bool, 20)
		for i := range mask {
			mask[i] = true
		}
		if frame > 0 {
			mb := (frame * 7) % len(mask)
			mask[mb] = false
			plane := frame % 3
			block := 16
			if plane != 0 {
				block = 8
			}
			off := (mb/5)*block*int(src.IStride[plane]) + (mb%5)*block
			src.PData[plane][off] ^= byte(frame + 43)
		}
		dst := pictures[index]
		if sourceCopyFrame(t, p, src, dst, mask, int32(frame)) {
			partials++
		}
		WelsMoveMemory_c(want.pData[0], want.iDataOff[0], want.pData[1], want.iDataOff[1], want.pData[2], want.iDataOff[2],
			want.iLineSize[0], want.iLineSize[1], want.iLineSize[2], src.PData[0], 0, src.PData[1], 0, src.PData[2], 0,
			src.IStride[0], src.IStride[1], src.IStride[2], src.IPicWidth, src.IPicHeight)
		for plane := range 3 {
			if !bytes.Equal(dst.pData[plane], want.pData[plane]) {
				t.Fatalf("frame %d plane %d lost earlier damage or wrote outside source", frame, plane)
			}
		}
		// Subsequent calls must not read storage borrowed from this frame.
		clear(mask)
	}
	if partials != len(order)-3 {
		t.Fatalf("partial frames=%d, want %d", partials, len(order)-3)
	}
}

func TestSourceCopyFallsBackOnUncertainGeneration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*CWelsPreProcess, *api.SSourcePicture, *SPicture, *[]bool)
	}{
		{name: "missing mask", change: func(_ *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture, m *[]bool) { *m = nil }},
		{name: "short mask", change: func(_ *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture, m *[]bool) { *m = (*m)[:1] }},
		{name: "oversized mask", change: func(_ *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture, m *[]bool) { *m = append(*m, true) }},
		{name: "previous source replaced", change: func(p *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture, _ *[]bool) {
			p.m_pLastSpatialPicture[0][0] = nil
		}},
		{name: "previous source recycled", change: func(p *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture, _ *[]bool) {
			p.vaaUnchanged.reference.iFramePoc++
		}},
		{name: "QP change", change: func(p *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture, _ *[]bool) {
			p.m_pEncCtx.pSvcParam.SSpatialLayers[0].IDLayerQp++
		}},
		{name: "slice count change", change: func(p *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture, _ *[]bool) {
			p.m_pEncCtx.pSvcParam.SSpatialLayers[0].SSliceArgument.UiSliceNum++
		}},
		{name: "forced IDR", change: func(p *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture, _ *[]bool) {
			p.m_pEncCtx.pSvcParam.sDependencyLayers[0].bEncCurFrmAsIdrFlag = true
		}},
		{name: "VAA disabled", change: func(p *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture, _ *[]bool) {
			p.vaaUnchanged.enabled = false
		}},
		{name: "denoise enabled", change: func(p *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture, _ *[]bool) {
			p.m_pEncCtx.pSvcParam.BEnableDenoise = true
		}},
		{name: "source replaced", change: func(_ *CWelsPreProcess, _ *api.SSourcePicture, d *SPicture, _ *[]bool) {
			d.pData[0] = bytes.Clone(d.pData[0])
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p, src := sourceCopyFixture(32, 32)
			a, b := AllocPicture(32, 32, false, 0), AllocPicture(32, 32, false, 0)
			mask := []bool{true, true, true, true}
			sourceCopyFrame(t, p, src, a, mask, 0)
			sourceCopyFrame(t, p, src, b, mask, 1)
			test.change(p, src, a, &mask)
			// Deliberately stale hints must not conceal this change after invalidation.
			src.PData[0][0] ^= 99
			if sourceCopyFrame(t, p, src, a, mask, 2) {
				t.Fatal("uncertain generation used a partial copy")
			}
			if a.pData[0][a.iDataOff[0]] != src.PData[0][0] {
				t.Fatal("fallback did not refresh source")
			}
		})
	}
}

func TestSourceCopyRejectsUnsupportedGeometry(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*CWelsPreProcess, *api.SSourcePicture, *SPicture)
	}{
		{name: "crop left", change: func(p *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture) {
			p.m_pEncCtx.pSvcParam.SUsedPicRect.iLeft = 2
		}},
		{name: "crop top", change: func(p *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture) {
			p.m_pEncCtx.pSvcParam.SUsedPicRect.iTop = 2
		}},
		{name: "rescale", change: func(p *CWelsPreProcess, _ *api.SSourcePicture, _ *SPicture) {
			p.m_sScaledPicture.pScaledInputPicture = &SPicture{}
		}},
		{name: "odd source", change: func(_ *CWelsPreProcess, s *api.SSourcePicture, _ *SPicture) { s.IPicWidth-- }},
		{name: "wrong source format", change: func(_ *CWelsPreProcess, s *api.SSourcePicture, _ *SPicture) { s.IColorFormat = 0 }},
		{name: "short source plane", change: func(_ *CWelsPreProcess, s *api.SSourcePicture, _ *SPicture) { s.PData[2] = s.PData[2][:1] }},
		{name: "short destination plane", change: func(_ *CWelsPreProcess, _ *api.SSourcePicture, d *SPicture) { d.pData[2] = d.pData[2][:1] }},
		{name: "short source stride", change: func(_ *CWelsPreProcess, s *api.SSourcePicture, _ *SPicture) { s.IStride[2] = 1 }},
		{name: "short destination stride", change: func(_ *CWelsPreProcess, _ *api.SSourcePicture, d *SPicture) { d.iLineSize[2] = 1 }},
		{name: "negative source stride", change: func(_ *CWelsPreProcess, s *api.SSourcePicture, _ *SPicture) { s.IStride[0] = -32 }},
		{name: "negative destination offset", change: func(_ *CWelsPreProcess, _ *api.SSourcePicture, d *SPicture) { d.iDataOff[0] = -1 }},
		{name: "destination geometry", change: func(_ *CWelsPreProcess, _ *api.SSourcePicture, d *SPicture) { d.iWidthInPixel = 16 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p, src := sourceCopyFixture(32, 32)
			a, b := AllocPicture(32, 32, false, 0), AllocPicture(32, 32, false, 0)
			mask := []bool{true, true, true, true}
			sourceCopyFrame(t, p, src, a, mask, 0)
			sourceCopyFrame(t, p, src, b, mask, 1)
			p.vaaUnchanged.mask = mask
			test.change(p, src, a)
			if p.tryCopySource(p.m_pEncCtx.pSvcParam, a, src, 32, 32) || p.sourceCopy.reference != nil {
				t.Fatal("unsupported geometry retained source residency")
			}
		})
	}
}

func TestSourceCopyInvalidationRetainsOwnedMaskCapacity(t *testing.T) {
	t.Parallel()
	p, src := sourceCopyFixture(32, 32)
	a, b := AllocPicture(32, 32, false, 0), AllocPicture(32, 32, false, 0)
	mask := []bool{true, true, true, true}
	sourceCopyFrame(t, p, src, a, mask, 0)
	sourceCopyFrame(t, p, src, b, mask, 1)
	var storage [3]*bool
	for i := range storage {
		if len(p.sourceCopy.buffers[i].dirty) != 0 {
			storage[i] = &p.sourceCopy.buffers[i].dirty[0]
		}
	}
	p.vaaUnchanged.reference = nil // Failed, skipped, or rejected encode.
	p.sourceCopy.finish(p, nil)
	if p.sourceCopy.reference != nil || p.sourceCopy.current != nil {
		t.Fatal("failed encode retained generation")
	}
	for i, previous := range storage {
		buffer := &p.sourceCopy.buffers[i]
		if buffer.valid || buffer.picture != nil || previous != nil && previous != &buffer.dirty[0] {
			t.Fatal("invalidation retained content or discarded reusable mask allocation")
		}
	}
	if sourceCopyFrame(t, p, src, a, mask, 2) {
		t.Fatal("failure did not force full source copy")
	}
}

func TestSourceCopyMatchesFullEncode(t *testing.T) {
	t.Parallel()
	for _, slices := range []uint32{1, 4} {
		for _, qp := range []int32{0, 20, 51} {
			t.Run(fmt.Sprintf("slices%d/QP%d", slices, qp), func(t *testing.T) {
				t.Parallel()
				const width, height = 66, 50
				var codecs [2]*CWelsH264SVCEncoder
				for i := range codecs {
					codec := NewCWelsH264SVCEncoder()
					codecs[i] = codec
					t.Cleanup(func() { codec.Uninitialize() })
					param := unchangedTestParams().SEncParamExt
					param.IPicWidth, param.IPicHeight = width, height
					param.FMaxFrameRate = 60
					layer := &param.SSpatialLayers[0]
					layer.IVideoWidth, layer.IVideoHeight, layer.FFrameRate, layer.IDLayerQp = width, height, 60, qp
					layer.SSliceArgument.UiSliceNum = slices
					if slices > 1 {
						layer.SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
					}
					param.IMultipleThreadIdc = uint16(slices)
					param.BUseLoadBalancing, param.BEnableSceneChangeDetect, param.BEnableFrameSkip = false, false, false
					if code := codec.InitializeExt(&param); code != 0 {
						t.Fatalf("initialize: %d", code)
					}
				}
				_, source := sourceCopyFixture(width, height)
				partialFrames := 0
				seen := map[*SPicture]bool{}
				for frame := range 22 {
					mask := make([]bool, 20)
					for i := range mask {
						mask[i] = true
					}
					if frame > 0 {
						mb, plane := (frame*7)%len(mask), frame%3
						mask[mb] = false
						block := 16
						if plane != 0 {
							block = 8
						}
						off := (mb/5)*block*int(source.IStride[plane]) + (mb%5)*block
						source.PData[plane][off] ^= byte(frame*11 + 3)
					}
					if frame == 8 {
						for _, codec := range codecs {
							var output api.SFrameBSInfo
							if codec.EncodeFrame(nil, &output) == 0 {
								t.Fatal("nil source accepted")
							}
						}
						// Failed calls invalidate hints even when callers mistakenly
						// supply stale all-unchanged information on recovery.
						for i := range mask {
							mask[i] = true
						}
					}
					if frame == 13 {
						for _, codec := range codecs {
							codec.ForceIntraFrame(true, -1)
						}
					}
					if frame == 17 {
						mask = nil
					}
					source.UnchangedMacroblocks, source.UiTimeStamp = mask, int64(frame*16)
					var outputs [2][]byte
					for i, codec := range codecs {
						if i == 0 {
							// Keep VAA/reconstruction hints identical, changing only
							// whether original input copying may reuse resident bytes.
							codec.m_pEncContext.pVpp.sourceCopy.invalidate()
						}
						var output api.SFrameBSInfo
						if code := codec.EncodeFrame(source, &output); code != 0 {
							t.Fatalf("frame %d encoder %d: %d", frame, i, code)
						}
						for layer := int32(0); layer < output.ILayerNum; layer++ {
							info := &output.SLayerInfo[layer]
							size := int32(0)
							for nal := int32(0); nal < info.INalCount; nal++ {
								size += info.PNalLengthInByte[nal]
							}
							outputs[i] = append(outputs[i], info.PBsBuf[:size]...)
						}
					}
					if !bytes.Equal(outputs[0], outputs[1]) {
						t.Fatalf("frame %d bitstream differs with resident input reuse", frame)
					}
					p := codecs[1].m_pEncContext.pVpp
					if p.sourceCopy.partial {
						partialFrames++
					}
					if p.sourceCopy.reference != nil {
						seen[p.sourceCopy.reference] = true
					}
					want, got := codecs[0].m_pEncContext.pEncPic, codecs[1].m_pEncContext.pEncPic
					for plane := range 3 {
						if !bytes.Equal(want.pData[plane], got.pData[plane]) {
							t.Fatalf("frame %d plane %d source/padding differ", frame, plane)
						}
					}
					clear(mask)
				}
				if partialFrames < 10 || len(seen) != 2 {
					t.Fatalf("partial frames=%d resident buffers=%d", partialFrames, len(seen))
				}
			})
		}
	}
}

func TestSourceCopyMatchesFullAfterResize(t *testing.T) {
	t.Parallel()
	for _, sliceCount := range []int{1, 4} {
		t.Run(fmt.Sprintf("%dSlices", sliceCount), func(t *testing.T) {
			t.Parallel()
			codecs := [2]*CWelsH264SVCEncoder{
				newUnchangedParityCodec(t, 66, 50, sliceCount),
				newUnchangedParityCodec(t, 66, 50, sliceCount),
			}
			for phase, size := range [][2]int32{{66, 50}, {98, 66}, {66, 50}} {
				width, height := size[0], size[1]
				for _, codec := range codecs {
					var param api.SEncParamExt
					if code := codec.GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &param); code != 0 {
						t.Fatalf("get parameters: %d", code)
					}
					param.IPicWidth, param.IPicHeight = width, height
					param.SSpatialLayers[0].IVideoWidth, param.SSpatialLayers[0].IVideoHeight = width, height
					previous := codec.m_pEncContext
					if code := codec.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &param); code != 0 {
						t.Fatalf("phase %d resize: %d", phase, code)
					}
					if phase > 0 && (codec.m_pEncContext == previous || codec.m_pEncContext.pVpp.sourceCopy.reference != nil) {
						t.Fatal("resize retained the old context or original-source generation")
					}
				}
				_, source := sourceCopyFixture(width, height)
				for plane := range 3 {
					for i := range source.PData[plane] {
						source.PData[plane][i] ^= byte(phase * 37)
					}
				}
				cols, rows := int((width+15)/16), int((height+15)/16)
				partialFrames := 0
				for frame := range 7 {
					mask := make([]bool, cols*rows)
					for i := range mask {
						mask[i] = true
					}
					if frame > 0 {
						mb, plane := (frame*7)%len(mask), frame%3
						mask[mb] = false
						block := 16
						if plane != 0 {
							block = 8
						}
						off := (mb/cols)*block*int(source.IStride[plane]) + (mb%cols)*block
						source.PData[plane][off] ^= byte(frame*11 + 3)
					}
					source.UnchangedMacroblocks, source.UiTimeStamp = mask, int64((phase*7+frame)*16)
					var outputs [2][]byte
					for i, codec := range codecs {
						if i == 0 {
							codec.m_pEncContext.pVpp.sourceCopy.invalidate()
						}
						var output api.SFrameBSInfo
						if code := codec.EncodeFrame(source, &output); code != 0 {
							t.Fatalf("phase %d frame %d encoder %d: %d", phase, frame, i, code)
						}
						if frame == 0 && output.EFrameType != api.VideoFrameTypeIDR {
							t.Fatalf("phase %d first frame was not IDR", phase)
						}
						outputs[i] = unchangedFrameBytes(&output)
					}
					if !bytes.Equal(outputs[0], outputs[1]) {
						t.Fatalf("phase %d frame %d resized bitstream differs", phase, frame)
					}
					want, got := codecs[0].m_pEncContext.pEncPic, codecs[1].m_pEncContext.pEncPic
					for plane := range 3 {
						if !bytes.Equal(want.pData[plane], got.pData[plane]) {
							t.Fatalf("phase %d frame %d resized source/padding differ", phase, frame)
						}
					}
					if codecs[1].m_pEncContext.pVpp.sourceCopy.partial {
						partialFrames++
					}
					clear(mask)
				}
				if partialFrames < 3 {
					t.Fatalf("phase %d partial copies did not resume after resize: %d", phase, partialFrames)
				}
			}
		})
	}
}
