package decoder

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

// These synthetic clips and independent reference hashes are checked in, so
// full decoding remains tested when the optional upstream res/ corpus is absent.
func TestDecoderSyntheticConformance(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name          string
		width, height int32
		frames        int
		profile       int32
		hash          string
	}{
		{
			name: "baseline", width: 62, height: 46, frames: 12, profile: 66,
			hash: "1ccf6a5e38f825953f81fdf3d2cf863a3a968e339803d4d82379a57eb0be41df",
		},
		{
			name: "main_ip", width: 96, height: 64, frames: 12, profile: 77,
			hash: "1673939b91dbc86f1d05363b743631727e0ea8d2ec8f6f7138300ac6a3782267",
		},
		{
			name: "high_ip", width: 96, height: 64, frames: 12, profile: 100,
			hash: "4f7d7efbbf087ac71a3746b6aa24a904777f8ae6b9de280543c4e4318dd0f910",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stream := syntheticStream(t, tt.name)
			dec := newSyntheticDecoder(t, false)
			digest := sha256.New()
			frames := 0
			onFrame := func(data *[3][]byte, info *api.SBufferInfo) {
				if info.IBufferStatus == 0 {
					return
				}
				if info.IBufferStatus != 1 {
					t.Fatalf("buffer status = %d, want 0 or 1", info.IBufferStatus)
				}
				buffer := info.UsrData.SSystemBuffer
				if buffer.IWidth != tt.width || buffer.IHeight != tt.height {
					t.Fatalf("frame %d dimensions = %dx%d, want %dx%d",
						frames, buffer.IWidth, buffer.IHeight, tt.width, tt.height)
				}
				confOnDecodeFrame(digest, data, info)
				frames++
			}
			decode := func(src []byte) {
				var data [3][]byte
				var info api.SBufferInfo
				if state := dec.DecodeFrame2(src, int32(len(src)), &data, &info); state != api.DsErrorFree {
					t.Fatalf("DecodeFrame2 after %d frames: state %#x", frames, state)
				}
				onFrame(&data, &info)
			}
			pos := 0
			for pos < len(stream) {
				decode(confReadFrame(stream, &pos))
			}
			eos := int32(1)
			if result := dec.SetOption(api.DECODER_OPTION_END_OF_STREAM, &eos); result != 0 {
				t.Fatalf("SetOption(END_OF_STREAM): %d", result)
			}
			decode(nil)
			var remaining int32
			if result := dec.GetOption(api.DECODER_OPTION_NUM_OF_FRAMES_REMAINING_IN_BUFFER, &remaining); result != 0 {
				t.Fatalf("GetOption(NUM_OF_FRAMES_REMAINING): %d", result)
			}
			if remaining < 0 || remaining > 12 {
				t.Fatalf("buffered frames = %d, outside [0,12]", remaining)
			}
			for range remaining {
				var data [3][]byte
				var info api.SBufferInfo
				if state := dec.FlushFrame(&data, &info); state != api.DsErrorFree {
					t.Fatalf("FlushFrame: state %#x", state)
				}
				if info.IBufferStatus != 1 {
					t.Fatalf("FlushFrame buffer status = %d, want 1", info.IBufferStatus)
				}
				onFrame(&data, &info)
			}
			if frames != tt.frames {
				t.Errorf("decoded frames = %d, want %d", frames, tt.frames)
			}
			if got := hex.EncodeToString(digest.Sum(nil)); got != tt.hash {
				t.Errorf("decoded I420 SHA-256 = %s, want %s", got, tt.hash)
			}
			var profile int32
			if result := dec.GetOption(api.DECODER_OPTION_PROFILE, &profile); result != 0 {
				t.Fatalf("GetOption(PROFILE): %d", result)
			}
			if profile != tt.profile {
				t.Errorf("decoded profile = %d, want %d", profile, tt.profile)
			}
		})
	}
}

func TestDecoderSyntheticParser(t *testing.T) {
	t.Parallel()

	stream := syntheticStream(t, "baseline")
	dec := newSyntheticDecoder(t, true)
	frames := 0
	parse := func(src []byte) {
		var info api.SParserBsInfo
		if state := dec.DecodeParser(src, int32(len(src)), &info); state != api.DsErrorFree {
			t.Fatalf("DecodeParser after %d frames: state %#x", frames, state)
		}
		if info.INalNum == 0 {
			return
		}
		if info.ISpsWidthInPixel != 62 || info.ISpsHeightInPixel != 46 {
			t.Fatalf("parsed dimensions = %dx%d, want 62x46", info.ISpsWidthInPixel, info.ISpsHeightInPixel)
		}
		if info.INalNum < 0 || int(info.INalNum) > len(info.PNalLenInByte) {
			t.Fatalf("NAL count %d exceeds length array %d", info.INalNum, len(info.PNalLenInByte))
		}
		offset, slices := 0, 0
		for _, size := range info.PNalLenInByte[:info.INalNum] {
			if size < 5 || int(size) > len(info.PDstBuff)-offset {
				t.Fatalf("invalid NAL size %d at offset %d in buffer %d", size, offset, len(info.PDstBuff))
			}
			nal := info.PDstBuff[offset : offset+int(size)]
			if !bytes.HasPrefix(nal, []byte{0, 0, 0, 1}) {
				t.Fatalf("parsed NAL lacks Annex-B prefix: %x", nal[:5])
			}
			if kind := nal[4] & 0x1f; kind == 1 || kind == 5 {
				slices++
				if (frames == 0) != (kind == 5) {
					t.Fatalf("frame %d slice type = %d; only first frame should be IDR", frames, kind)
				}
			}
			offset += int(size)
		}
		if slices != 2 {
			t.Fatalf("frame %d has %d slices, want 2", frames, slices)
		}
		frames++
	}
	pos := 0
	for pos < len(stream) {
		parse(confReadFrame(stream, &pos))
	}
	parse(nil)
	if frames != 12 {
		t.Errorf("parsed frames = %d, want 12", frames)
	}
}

func syntheticStream(t *testing.T, name string) []byte {
	t.Helper()

	encoded, err := os.ReadFile(filepath.Join("testdata", name+".h264.b64"))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func newSyntheticDecoder(t *testing.T, parseOnly bool) api.ISVCDecoder {
	t.Helper()

	var dec api.ISVCDecoder
	if result := WelsCreateDecoder(&dec); result != 0 || dec == nil {
		t.Fatalf("WelsCreateDecoder: result %d, decoder %v", result, dec)
	}
	t.Cleanup(func() {
		if result := dec.Uninitialize(); result != 0 {
			t.Errorf("Uninitialize: %d", result)
		}
		WelsDestroyDecoder(dec)
	})
	param := api.SDecodingParam{UiTargetDqLayer: 0xff, EEcActiveIdc: api.ERROR_CON_DISABLE, BParseOnly: parseOnly}
	param.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_AVC
	if result := dec.Initialize(&param); result != 0 {
		t.Fatalf("Initialize: %d", result)
	}

	return dec
}
