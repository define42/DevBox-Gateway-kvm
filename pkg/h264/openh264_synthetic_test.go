package openh264

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

// These sequences are generated in memory so round-trip, rate-control, and
// screen-content coverage does not depend on the optional upstream res/ corpus.
func TestSyntheticEncodeDecode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		svc    bool
		change func(*api.SEncParamExt)
	}{
		{name: "camera_bitrate", change: func(p *api.SEncParamExt) { p.IRCMode = api.RC_BITRATE_MODE }},
		{name: "camera_quality", change: func(p *api.SEncParamExt) { p.IRCMode = api.RC_QUALITY_MODE }},
		{name: "camera_buffer", change: func(p *api.SEncParamExt) { p.IRCMode = api.RC_BUFFERBASED_MODE }},
		{name: "camera_timestamp", change: func(p *api.SEncParamExt) { p.IRCMode = api.RC_TIMESTAMP_MODE }},
		{name: "camera_cabac", change: func(p *api.SEncParamExt) {
			p.IEntropyCodingModeFlag = 1
			p.SSpatialLayers[0].UiProfileIdc = api.PRO_MAIN
		}},
		{name: "camera_temporal", change: func(p *api.SEncParamExt) { p.ITemporalLayerNum = 3 }},
		{name: "camera_denoise", change: func(p *api.SEncParamExt) { p.BEnableDenoise = true }},
		{name: "camera_fixed_slices", change: func(p *api.SEncParamExt) {
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
			p.SSpatialLayers[0].SSliceArgument.UiSliceNum = 3
		}},
		{name: "camera_size_limited_slices", change: func(p *api.SEncParamExt) {
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SIZELIMITED_SLICE
			p.SSpatialLayers[0].SSliceArgument.UiSliceSizeConstraint = 600
		}},
		{name: "camera_cabac_slices", change: func(p *api.SEncParamExt) {
			p.IEntropyCodingModeFlag = 1
			p.SSpatialLayers[0].UiProfileIdc = api.PRO_MAIN
			p.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
			p.SSpatialLayers[0].SSliceArgument.UiSliceNum = 3
		}},
		{name: "camera_spatial_layers", svc: true, change: func(p *api.SEncParamExt) {
			p.ISpatialLayerNum = 2
			p.SSpatialLayers[1] = p.SSpatialLayers[0]
			p.SSpatialLayers[0].IVideoWidth /= 2
			p.SSpatialLayers[0].IVideoHeight /= 2
			p.SSpatialLayers[0].ISpatialBitrate /= 2
			p.ITargetBitrate += p.SSpatialLayers[0].ISpatialBitrate
		}},
		{name: "camera_long_term", change: func(p *api.SEncParamExt) {
			p.BEnableLongTermReference = true
			p.ILTRRefNum = 1
			p.ILtrMarkPeriod = 2
		}},
		{name: "screen", change: func(p *api.SEncParamExt) { p.IUsageType = api.SCREEN_CONTENT_REAL_TIME }},
		{name: "screen_long_term", change: func(p *api.SEncParamExt) {
			p.IUsageType = api.SCREEN_CONTENT_REAL_TIME
			p.BEnableLongTermReference = true
			p.ILTRRefNum = 1
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const width, height, frames = 128, 96, 24
			params, err := DefaultEncoderParams(width, height, 2_000_000, 30)
			if err != nil {
				t.Fatal(err)
			}
			params.IMultipleThreadIdc = 1
			params.BEnableFrameSkip = false
			params.IRCMode = api.RC_OFF_MODE
			tc.change(params)
			enc, err := NewEncoder(params)
			if err != nil {
				t.Fatal(err)
			}
			defer enc.Close()
			var decParams *api.SDecodingParam
			if tc.svc {
				decParams = &api.SDecodingParam{UiTargetDqLayer: 255}
				decParams.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_SVC
			}
			dec, err := NewDecoder(decParams)
			if err != nil {
				t.Fatal(err)
			}
			defer dec.Close()
			inputs := make([]*Frame, 0, frames)
			outputs := make([]*Frame, 0, frames)
			for i := range frames {
				frame := syntheticFrame(width, height, i)
				inputs = append(inputs, frame)
				if i == 16 {
					if err := enc.ForceIntraFrame(); err != nil {
						t.Fatal(err)
					}
				}
				data, kind, err := enc.Encode(frame)
				if err != nil || len(data) == 0 {
					t.Fatalf("encode frame %d: %d bytes, type %v, %v", i, len(data), kind, err)
				}
				if (i == 0 || i == 16) && kind != api.VideoFrameTypeIDR {
					t.Fatalf("frame %d type = %v, want IDR", i, kind)
				}
				decoded, err := dec.Decode(data)
				if err != nil {
					t.Fatalf("decode frame %d: %v", i, err)
				}
				if decoded != nil {
					outputs = append(outputs, decoded)
				}
			}
			remaining, err := dec.Flush()
			if err != nil {
				t.Fatal(err)
			}
			outputs = append(outputs, remaining...)
			if len(outputs) != frames {
				t.Fatalf("decoded %d frames, want %d", len(outputs), frames)
			}
			for i, got := range outputs {
				if got.Width != width || got.Height != height {
					t.Fatalf("frame %d: %dx%d, want %dx%d", i, got.Width, got.Height, width, height)
				}
				for plane, data := range [][]byte{got.Y, got.U, got.V} {
					want := [][]byte{inputs[i].Y, inputs[i].U, inputs[i].V}[plane]
					if quality := psnr(want, data); quality < 30 {
						t.Errorf("frame %d plane %d: PSNR %.2f dB, want at least 30", i, plane, quality)
					}
				}
			}
		})
	}
}

func syntheticFrame(width, height, index int) *Frame {
	frame := NewFrame(width, height)
	frame.Timestamp = int64(index * 33)
	for y := range height {
		for x := range width {
			value := 40 + (x+y)%80
			// Repeated desktop backgrounds plus a moving window and a scene cut.
			if x >= (index%8)*8 && x < (index%8)*8+24 && y >= 24 && y < 48 {
				value = 200
			}
			if index >= 12 {
				value = 240 - value
			}
			frame.Y[y*width+x] = byte(value)
		}
	}
	for i := range frame.U {
		frame.U[i], frame.V[i] = 100, 150
	}

	return frame
}
