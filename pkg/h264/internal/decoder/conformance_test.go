// Decoder conformance test: port of test/api/decoder_test.cpp
// (DecoderOutputTest.CompareOutput over kFileParamArray) together with the
// decoding loop of test/api/BaseDecoderTest.cpp.

package decoder

import (
	"crypto/sha1"
	"encoding/hex"
	"hash"
	"os"
	"path/filepath"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

type confFileParam struct {
	fileName string
	hashStr  string
}

// kFileParamArray (test/api/decoder_test.cpp); file names are relative to
// the repository root.
var confFileParamArray = []confFileParam{
	{"res/Adobe_PDF_sample_a_1024x768_50Frms.264", "9aa9a4d9598eb3e1093311826844f37c43e4c521"},
	{"res/BA1_FT_C.264", "418d152fb85709b6f172799dcb239038df437cfa"},
	{"res/BA1_Sony_D.jsv", "d94b5ceed5686a03ea682b53d415dee999d27eb6"},
	{"res/BAMQ1_JVC_C.264", "613cf662c23e5d9e1d7da7fe880a3c427411d171"},
	{"res/BAMQ2_JVC_C.264", "11bcf3713f520e606a8326d37e00e5fd6c9fd4a0"},
	{"res/BA_MW_D.264", "afd7a9765961ca241bb4bdf344b31397bec7465a"},
	{"res/BANM_MW_D.264", "92d924a857a1a7d7d9b224eaa3887830f15dee7f"},
	{"res/BASQP1_Sony_C.jsv", "3986c8c9d2876d2f0748b925101b152c6ec8b811"},
	{"res/CI1_FT_B.264", "cbfec15e17a504678b19a1191992131c92a1ac26"},
	{"res/CI_MW_D.264", "289f29a103c8d95adf2909c646466904be8b06d7"},
	{"res/CVFC1_Sony_C.jsv", "4641abd7419a5580b97f16e83fd1d566339229d0"},
	{"res/CVPCMNL1_SVA_C.264", "c2b0d964de727c64b9fccb58f63b567c82bda95a"},
	{"res/LS_SVA_D.264", "72118f4d1674cf14e58bed7e67cb3aeed3df62b9"},
	{"res/MIDR_MW_D.264", "9467030f4786f75644bf06a7fc809c36d1959827"},
	{"res/MPS_MW_A.264", "67f1cfbef0e8025ed60dedccf8d9558d0636be5f"},
	{"res/MR1_BT_A.h264", "6e585f8359667a16b03e5f49a06f5ceae8d991e0"},
	{"res/MR1_MW_A.264", "d9e2bf34e9314dcc171ddaea2c5015d0421479f2"},
	{"res/MR2_MW_A.264", "628b1d4eff04c2d277f7144e23484957dad63cbe"},
	{"res/MR2_TANDBERG_E.264", "74d618bc7d9d41998edf4c85d51aa06111db6609"},
	{"res/NL1_Sony_D.jsv", "e401e30669938443c2f02522fd4d5aa1382931a0"},
	{"res/NLMQ1_JVC_C.264", "f3265c6ddf8db1b2bf604d8a2954f75532e28cda"},
	{"res/NLMQ2_JVC_C.264", "350ae86ef9ba09390d63a09b7f9ff54184109ca8"},
	{"res/NRF_MW_E.264", "20732198c04cd2591350a361e4510892f6eed3f0"},
	{"res/QCIF_2P_I_allIPCM.264", "8724c0866ebdba7ebb7209a0c0c3ae3ae38a0240"},
	{"res/SVA_BA1_B.264", "c4543b24823b16c424c673616c36c7f537089b2d"},
	{"res/SVA_BA2_D.264", "98ff2d67860462d8d8bcc9352097c06cc401d97e"},
	{"res/SVA_Base_B.264", "91f514d81cd33de9f6fbf5dbefdb189cc2e7ecf4"},
	{"res/SVA_CL1_E.264", "4fe09ab6cdc965ea10a20f1d6dd38aca954412bb"},
	{"res/SVA_FM1_E.264", "fad08c4ff7cf2307b6579853d0f4652fc26645d3"},
	{"res/SVA_NL1_B.264", "6d63f72a0c0d833b1db0ba438afff3b4180fb3e6"},
	{"res/SVA_NL2_E.264", "70453ef8097c94dd190d6d2d1d5cb83c67e66238"},
	{"res/SarVui.264", "98ff2d67860462d8d8bcc9352097c06cc401d97e"},
	{"res/Static.264", "91dd4a7a796805b2cd015cae8fd630d96c663f42"},
	{"res/Zhling_1280x720.264", "ad99f5eaa2d73ae3840e7da67313de8cfc866ce6"},
	{"res/sps_subsetsps_bothVUI.264", "d3a47032eb5dcc1963343a68e9bea12435bf1e4c"},
	{"res/test_cif_I_CABAC_PCM.264", "95fdf21470d3bbcf95505abb2164042063a79d98"},
	{"res/test_cif_I_CABAC_slice.264", "19121bc67f2b13fb8f030504fc0827e1ac6d0fdb"},
	{"res/test_cif_P_CABAC_slice.264", "521bbd0ba2422369b724c7054545cf107a56f959"},
	{"res/test_qcif_cabac.264", "587d1d05943f3cd416bf69469975fdee05361e69"},
	{"res/test_scalinglist_jm.264", "992a25b4ec98db4a16d61c097e614eb16afe3478"},
	{"res/test_vd_1d.264", "5827d2338b79ff82cd091c707823e466197281d3"},
	{"res/test_vd_rc.264", "eea02e97bfec89d0418593a8abaaf55d02eaa1ca"},
	{"res/Cisco_Men_whisper_640x320_CABAC_Bframe_9.264", "2b349c1bc806b6e0412008747b2463d77b576476"},
	{"res/Cisco_Men_whisper_640x320_CAVLC_Bframe_9.264", "e5b76ff7e2f44e9b33906f8a4039d0d2bdb1580b"},
	{"res/Cisco_Adobe_PDF_sample_a_1024x768_CAVLC_Bframe_9.264", "9d758d9e6f4dead0d7b361f3ddf2ee009d0ea190"},
	{"res/VID_1280x544_cabac_temporal_direct.264", "02299df3b9d83300d244b36601699859c57fe905"},
	{"res/VID_1280x720_cabac_temporal_direct.264", "0ef0818cb23445d209b8a7632c13f1c7e820cc27"},
	{"res/VID_1920x1080_cabac_temporal_direct.264", "ad2b1d1456919693e38a1e3e8cd9c21699688cec"},
	{"res/VID_1280x544_cavlc_temporal_direct.264", "71a12ff2b548b765a34c11f39eef1faa19b38d59"},
	{"res/VID_1280x720_cavlc_temporal_direct.264", "f39cecb32ba20ca4f3b3a385db9ef46ba340e41f"},
	{"res/VID_1920x1080_cavlc_temporal_direct.264", "6aae2d569a1ebbe5ae20e2dfc5e709cc05ab1a21"},
}

// confRepoRoot is the repository root relative to this package directory.
const confRepoRoot = "../../../.."

// confReadFrame ports the static ReadFrame of BaseDecoderTest.cpp on an
// in-memory file: it returns the bytes up to (excluding) the next
// 00 00 00 01 start code that is not within the first 4 bytes, and leaves
// *pos at that start code (the C code seeks back 4 bytes). A zero-length
// result means end of file.
func confReadFrame(file []byte, pos *int) []byte {
	// start code of a frame is {0, 0, 0, 1}
	zeroCount := 0
	start := *pos
	length := 0

	for {
		if *pos >= len(file) { // end of file
			return file[start:*pos]
		}
		b := file[*pos]
		*pos++
		length++

		if length <= 4 {
			continue
		}

		if zeroCount < 3 {
			if b != 0 {
				zeroCount = 0
			} else {
				zeroCount++
			}
		} else {
			if b == 1 {
				*pos -= 4
				return file[start:*pos]
			} else if b == 0 {
				zeroCount = 3
			} else {
				zeroCount = 0
			}
		}
	}
}

// confUpdateHashFromPlane ports UpdateHashFromPlane of decoder_test.cpp.
func confUpdateHashFromPlane(ctx hash.Hash, plane []byte, width, height, stride int) {
	off := 0
	for i := 0; i < height; i++ {
		ctx.Write(plane[off : off+width])
		off += stride
	}
}

// confOnDecodeFrame ports DecoderOutputTest::onDecodeFrame together with the
// Frame construction of BaseDecoderTest::DecodeFrame / FlushFrame.
func confOnDecodeFrame(ctx hash.Hash, data *[3][]byte, bufInfo *api.SBufferInfo) {
	sb := &bufInfo.UsrData.SSystemBuffer
	w := int(sb.IWidth)
	h := int(sb.IHeight)
	confUpdateHashFromPlane(ctx, data[0], w, h, int(sb.IStride[0]))
	confUpdateHashFromPlane(ctx, data[1], w/2, h/2, int(sb.IStride[1]))
	confUpdateHashFromPlane(ctx, data[2], w/2, h/2, int(sb.IStride[1]))
}

// confDecodeFrame ports BaseDecoderTest::DecodeFrame.
func confDecodeFrame(t *testing.T, dec api.ISVCDecoder, src []byte, ctx hash.Hash) {
	t.Helper()
	var data [3][]byte
	var bufInfo api.SBufferInfo

	rv := dec.DecodeFrame2(src, int32(len(src)), &data, &bufInfo)
	if rv != api.DsErrorFree {
		t.Fatalf("DecodeFrame2 returned %d", rv)
	}

	if bufInfo.IBufferStatus == 1 {
		confOnDecodeFrame(ctx, &data, &bufInfo)
	}
}

// confFlushFrame ports BaseDecoderTest::FlushFrame.
func confFlushFrame(t *testing.T, dec api.ISVCDecoder, ctx hash.Hash) {
	t.Helper()
	var data [3][]byte
	var bufInfo api.SBufferInfo

	rv := dec.FlushFrame(&data, &bufInfo)
	if rv != api.DsErrorFree {
		t.Fatalf("FlushFrame returned %d", rv)
	}

	if bufInfo.IBufferStatus == 1 {
		confOnDecodeFrame(ctx, &data, &bufInfo)
	}
}

// confDecodeFile ports BaseDecoderTest::SetUp + DecodeFile + TearDown.
func confDecodeFile(t *testing.T, fileName string, ctx hash.Hash) {
	t.Helper()
	file, err := os.ReadFile(fileName)
	if err != nil {
		t.Skipf("OpenH264 conformance data is unavailable: %v", err)
	}

	// SetUp
	var dec api.ISVCDecoder
	if rv := WelsCreateDecoder(&dec); rv != 0 || dec == nil {
		t.Fatalf("WelsCreateDecoder: %d", rv)
	}
	defer func() {
		// TearDown
		dec.Uninitialize()
		WelsDestroyDecoder(dec)
	}()

	var decParam api.SDecodingParam
	decParam.UiTargetDqLayer = 0xff // UCHAR_MAX
	decParam.EEcActiveIdc = api.ERROR_CON_SLICE_COPY
	decParam.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_DEFAULT

	if rv := dec.Initialize(&decParam); rv != 0 {
		t.Fatalf("Initialize: %d", rv)
	}

	// DecodeFile
	pos := 0
	for {
		buf := confReadFrame(file, &pos)
		if len(buf) == 0 {
			break
		}
		// BufferedData owns a private copy of the frame.
		frame := append([]byte(nil), buf...)
		confDecodeFrame(t, dec, frame, ctx)
	}

	iEndOfStreamFlag := int32(1)
	dec.SetOption(api.DECODER_OPTION_END_OF_STREAM, &iEndOfStreamFlag)

	// Get pending last frame
	confDecodeFrame(t, dec, nil, ctx)
	// Flush out last frames in decoder buffer
	var numOfFramesInBuffer int32
	dec.GetOption(api.DECODER_OPTION_NUM_OF_FRAMES_REMAINING_IN_BUFFER, &numOfFramesInBuffer)
	for i := int32(0); i < numOfFramesInBuffer; i++ {
		confFlushFrame(t, dec, ctx)
	}
}

// TEST_P (DecoderOutputTest, CompareOutput) instantiated with kFileParamArray.
func TestDecoderConformance(t *testing.T) {
	for _, p := range confFileParamArray {
		p := p
		t.Run(filepath.Base(p.fileName), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic while decoding %s: %v", p.fileName, r)
				}
			}()
			ctx := sha1.New()
			confDecodeFile(t, filepath.Join(confRepoRoot, p.fileName), ctx)
			got := hex.EncodeToString(ctx.Sum(nil))
			if got != p.hashStr {
				t.Errorf("%s: SHA1 mismatch: got %s, want %s", p.fileName, got, p.hashStr)
			}
		})
	}
}

// TEST_F (DecoderInitTest, JustInit)
func TestDecoderInitJustInit(t *testing.T) {
	var dec api.ISVCDecoder
	if rv := WelsCreateDecoder(&dec); rv != 0 || dec == nil {
		t.Fatalf("WelsCreateDecoder: %d", rv)
	}
	var decParam api.SDecodingParam
	decParam.UiTargetDqLayer = 0xff
	decParam.EEcActiveIdc = api.ERROR_CON_SLICE_COPY
	decParam.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_DEFAULT
	if rv := dec.Initialize(&decParam); rv != 0 {
		t.Errorf("Initialize: %d", rv)
	}
	dec.Uninitialize()
	WelsDestroyDecoder(dec)
}
