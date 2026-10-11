// Port of test/api/encoder_test.cpp and test/api/BaseEncoderTest.cpp.
//
// TestEncoderOutputCompareOutput encodes each entry of kFileParamArray and
// compares the SHA1 hash of the produced bitstream with the reference hash of
// the C++ encoder.

package encoder

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/decoder"
)

// resDir is the repository res/ directory relative to this package.
const resDir = "../../../.."

// encTestRecover turns a panic inside a (sub)test into a test failure so that
// the remaining tests still run.
func encTestRecover(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Fatalf("panic: %v\n%s", r, debug.Stack())
	}
}

// ---------------------------------------------------------------------------
// BaseEncoderTest

type baseEncoderTest struct {
	t        *testing.T
	encoder_ api.ISVCEncoder
}

// SetUp is BaseEncoderTest::SetUp.
func (b *baseEncoderTest) SetUp(t *testing.T) {
	b.t = t
	rv := WelsCreateSVCEncoder(&b.encoder_)
	if rv != 0 {
		t.Fatalf("WelsCreateSVCEncoder returned %d", rv)
	}
	if b.encoder_ == nil {
		t.Fatalf("WelsCreateSVCEncoder returned a nil encoder")
	}

	uiTraceLevel := uint32(api.WELS_LOG_ERROR)
	b.encoder_.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &uiTraceLevel)
}

// TearDown is BaseEncoderTest::TearDown.
func (b *baseEncoderTest) TearDown() {
	if b.encoder_ != nil {
		b.encoder_.Uninitialize()
		WelsDestroySVCEncoder(b.encoder_)
	}
}

// initWithParam is the static InitWithParam of BaseEncoderTest.cpp.
func initWithParam(encoder api.ISVCEncoder, pEncParamExt *api.SEncParamExt) int32 {
	eSliceMode := pEncParamExt.SSpatialLayers[0].SSliceArgument.UiSliceMode
	bBaseParamFlag := api.SM_SINGLE_SLICE == eSliceMode && !pEncParamExt.BEnableDenoise &&
		pEncParamExt.ISpatialLayerNum == 1 && !pEncParamExt.BIsLosslessLink &&
		!pEncParamExt.BEnableLongTermReference && pEncParamExt.IEntropyCodingModeFlag == 0
	if bBaseParamFlag {
		var param api.SEncParamBase

		param.IUsageType = pEncParamExt.IUsageType
		param.FMaxFrameRate = pEncParamExt.FMaxFrameRate
		param.IPicWidth = pEncParamExt.IPicWidth
		param.IPicHeight = pEncParamExt.IPicHeight
		param.ITargetBitrate = 5000000
		return encoder.Initialize(&param)
	}

	var param api.SEncParamExt
	encoder.GetDefaultParams(&param)

	param.IUsageType = pEncParamExt.IUsageType
	param.FMaxFrameRate = pEncParamExt.FMaxFrameRate
	param.IPicWidth = pEncParamExt.IPicWidth
	param.IPicHeight = pEncParamExt.IPicHeight
	param.ITargetBitrate = 5000000
	param.BEnableDenoise = pEncParamExt.BEnableDenoise
	param.ISpatialLayerNum = pEncParamExt.ISpatialLayerNum
	param.BIsLosslessLink = pEncParamExt.BIsLosslessLink
	param.BEnableLongTermReference = pEncParamExt.BEnableLongTermReference
	if pEncParamExt.IEntropyCodingModeFlag != 0 {
		param.IEntropyCodingModeFlag = 1
	} else {
		param.IEntropyCodingModeFlag = 0
	}
	if eSliceMode != api.SM_SINGLE_SLICE &&
		eSliceMode != api.SM_SIZELIMITED_SLICE { //SM_SIZELIMITED_SLICE don't support multi-thread now
		param.IMultipleThreadIdc = 2
	}

	for i := int32(0); i < param.ISpatialLayerNum; i++ {
		param.SSpatialLayers[i].IVideoWidth = pEncParamExt.IPicWidth >> (param.ISpatialLayerNum - 1 - i)
		param.SSpatialLayers[i].IVideoHeight = pEncParamExt.IPicHeight >> (param.ISpatialLayerNum - 1 - i)
		param.SSpatialLayers[i].FFrameRate = pEncParamExt.FMaxFrameRate
		param.SSpatialLayers[i].ISpatialBitrate = param.ITargetBitrate

		param.SSpatialLayers[i].SSliceArgument.UiSliceMode = eSliceMode
		if eSliceMode == api.SM_SIZELIMITED_SLICE {
			param.SSpatialLayers[i].SSliceArgument.UiSliceSizeConstraint = 600
			param.UiMaxNalSize = 1500
			param.IMultipleThreadIdc = 4
			param.BUseLoadBalancing = false
		}
		if eSliceMode == api.SM_FIXEDSLCNUM_SLICE {
			param.SSpatialLayers[i].SSliceArgument.UiSliceNum = 4
			param.IMultipleThreadIdc = 4
			param.BUseLoadBalancing = false
		}
		if param.IEntropyCodingModeFlag != 0 {
			param.SSpatialLayers[i].UiProfileIdc = api.PRO_MAIN
		}
	}
	param.ITargetBitrate *= param.ISpatialLayerNum
	return encoder.InitializeExt(&param)
}

// EncodeStream is BaseEncoderTest::EncodeStream; in reads one frame per
// call (InputStream::read) and onEncodeFrame is the Callback (may be nil).
func (b *baseEncoderTest) EncodeStream(in io.Reader, pEncParamExt *api.SEncParamExt, onEncodeFrame func(*api.SFrameBSInfo)) {
	t := b.t
	t.Helper()
	if pEncParamExt == nil {
		t.Fatalf("pEncParamExt is nil")
	}

	rv := initWithParam(b.encoder_, pEncParamExt)
	if rv != int32(api.CmResultSuccess) {
		t.Fatalf("InitWithParam returned %d", rv)
	}

	// I420: 1(Y) + 1/4(U) + 1/4(V)
	frameSize := int(pEncParamExt.IPicWidth * pEncParamExt.IPicHeight * 3 / 2)

	buf := make([]byte, frameSize)

	var info api.SFrameBSInfo

	var pic api.SSourcePicture
	pic.IPicWidth = pEncParamExt.IPicWidth
	pic.IPicHeight = pEncParamExt.IPicHeight
	pic.IColorFormat = int32(api.VideoFormatI420)
	pic.IStride[0] = pic.IPicWidth
	pic.IStride[1] = pic.IPicWidth >> 1
	pic.IStride[2] = pic.IStride[1]
	lumaSize := int(pEncParamExt.IPicWidth * pEncParamExt.IPicHeight)
	pic.PData[0] = buf
	pic.PData[1] = buf[lumaSize:]
	pic.PData[2] = buf[lumaSize+(lumaSize>>2):]
	for {
		n, _ := io.ReadFull(in, buf)
		if n != frameSize {
			break
		}
		rv = b.encoder_.EncodeFrame(&pic, &info)
		if rv != int32(api.CmResultSuccess) {
			t.Fatalf("EncodeFrame returned %d", rv)
		}
		if info.EFrameType != api.VideoFrameTypeSkip && onEncodeFrame != nil {
			onEncodeFrame(&info)
		}
	}
}

// EncodeFile is BaseEncoderTest::EncodeFile.
func (b *baseEncoderTest) EncodeFile(fileName string, pEncParamExt *api.SEncParamExt, onEncodeFrame func(*api.SFrameBSInfo)) {
	t := b.t
	t.Helper()
	f, err := os.Open(filepath.Join(resDir, fileName))
	if err != nil {
		t.Skipf("OpenH264 conformance data is unavailable: %v", err)
	}
	defer f.Close()
	if pEncParamExt == nil {
		t.Fatalf("pEncParamExt is nil")
	}
	b.EncodeStream(f, pEncParamExt, onEncodeFrame)
}

// ---------------------------------------------------------------------------
// encoder_test.cpp

func generatePattern(yBuf []uint8, yStride int, uBuf []uint8, uStride int, vBuf []uint8, vStride int, width, height int) {
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < yStride {
				yBuf[y*yStride+x] = uint8((x*4 + y*4) % 256)
			}
		}
	}
	for y := 0; y < (height >> 1); y++ {
		for x := 0; x < (width >> 1); x++ {
			if x < uStride {
				uBuf[y*uStride+x] = uint8((x * 8) % 256)
			}
			if x < vStride {
				vBuf[y*vStride+x] = uint8((y * 8) % 256)
			}
		}
	}
}

func calculatePlanePsnr(ref []uint8, refStride int, test []uint8, testStride int, width, height int) float64 {
	mse := 0.0
	compareWidth := min(width, min(refStride, testStride))
	for y := 0; y < height; y++ {
		for x := 0; x < compareWidth; x++ {
			diff := float64(ref[y*refStride+x]) - float64(test[y*testStride+x])
			mse += diff * diff
		}
	}
	mse /= float64(compareWidth * height)
	if mse == 0 {
		return 99.0
	}
	return 10.0 * math.Log10((255.0*255.0)/mse)
}

// updateHashFromFrame is the static UpdateHashFromFrame.
func updateHashFromFrame(info *api.SFrameBSInfo, ctx io.Writer) {
	for i := int32(0); i < info.ILayerNum; i++ {
		layerInfo := &info.SLayerInfo[i]
		layerSize := int32(0)
		for j := int32(0); j < layerInfo.INalCount; j++ {
			layerSize += layerInfo.PNalLengthInByte[j]
		}
		ctx.Write(layerInfo.PBsBuf[:layerSize])
	}
}

// frameBsLength sums the NAL sizes of all layers of info.
func frameBsLength(info *api.SFrameBSInfo) int32 {
	l := int32(0)
	for i := int32(0); i < info.ILayerNum; i++ {
		layerInfo := &info.SLayerInfo[i]
		for j := int32(0); j < layerInfo.INalCount; j++ {
			l += layerInfo.PNalLengthInByte[j]
		}
	}
	return l
}

func TestEncoderInitTestJustInit(t *testing.T) {
	defer encTestRecover(t)
	var b baseEncoderTest
	b.SetUp(t)
	defer b.TearDown()
}

type encodeFileParam struct {
	pkcFileName string
	pkcHashStr  [2]string
	eUsageType  api.EUsageType
	iWidth      int32
	iHeight     int32
	fFrameRate  float32
	eSliceMode  api.SliceModeEnum
	bDenoise    bool
	iLayerNum   int32
	bLossless   bool
	bEnableLtr  bool
	bCabac      bool
}

func encFileParamToParamExt(pEncFileParam *encodeFileParam, pEnxParamExt *api.SEncParamExt) {
	pEnxParamExt.IUsageType = pEncFileParam.eUsageType
	pEnxParamExt.IPicWidth = pEncFileParam.iWidth
	pEnxParamExt.IPicHeight = pEncFileParam.iHeight
	pEnxParamExt.FMaxFrameRate = pEncFileParam.fFrameRate
	pEnxParamExt.ISpatialLayerNum = pEncFileParam.iLayerNum

	pEnxParamExt.BEnableDenoise = pEncFileParam.bDenoise
	pEnxParamExt.BIsLosslessLink = pEncFileParam.bLossless
	pEnxParamExt.BEnableLongTermReference = pEncFileParam.bEnableLtr
	if pEncFileParam.bCabac {
		pEnxParamExt.IEntropyCodingModeFlag = 1
	} else {
		pEnxParamExt.IEntropyCodingModeFlag = 0
	}

	for i := int32(0); i < pEnxParamExt.ISpatialLayerNum; i++ {
		pEnxParamExt.SSpatialLayers[i].SSliceArgument.UiSliceMode = pEncFileParam.eSliceMode
	}
}

var kFileParamArray = []encodeFileParam{
	{
		"res/CiscoVT2people_320x192_12fps.yuv",
		[2]string{"672a52fb6b6e6d52b5b3f3480d13d44e88481fb9"}, api.CAMERA_VIDEO_REAL_TIME, 320, 192, 12.0, api.SM_SINGLE_SLICE, false, 1, false, false, false,
	},
	{
		"res/CiscoVT2people_160x96_6fps.yuv",
		[2]string{"08ade1853e4e49d50be675393780e75519586143"}, api.CAMERA_VIDEO_REAL_TIME, 160, 96, 6.0, api.SM_SINGLE_SLICE, false, 1, false, false, false,
	},
	{
		"res/Static_152_100.yuv",
		[2]string{"e60f12e3c24500d4306d812b0811d3c21855dd1c"}, api.CAMERA_VIDEO_REAL_TIME, 152, 100, 6.0, api.SM_SINGLE_SLICE, false, 1, false, false, false,
	},
	{
		"res/CiscoVT2people_320x192_12fps.yuv",
		[2]string{"266de2d059a00ad2f28304e7eb378543ea7d85ab"}, api.CAMERA_VIDEO_REAL_TIME, 320, 192, 12.0, api.SM_RASTER_SLICE, false, 1, false, false, false, // One slice per MB row
	},
	{
		"res/CiscoVT2people_320x192_12fps.yuv",
		[2]string{"913e49c787a0abdb378e9bc55bcffc27da89b965"}, api.CAMERA_VIDEO_REAL_TIME, 320, 192, 12.0, api.SM_SINGLE_SLICE, true, 1, false, false, false,
	},
	{
		"res/CiscoVT2people_320x192_12fps.yuv",
		// Allow for different output depending on whether averaging is done
		// vertically or horizontally first when downsampling.
		[2]string{"e626f7efa3d54da15794407c15b7c694f7ddd383", "eb4adc831563ce4f02f2942f52c992da760b4113"},
		api.CAMERA_VIDEO_REAL_TIME, 320, 192, 12.0, api.SM_SINGLE_SLICE, false, 2, false, false, false,
	},
	{
		"res/Cisco_Absolute_Power_1280x720_30fps.yuv",
		[2]string{"53f5681a0c2b7068f4edc94538d6133a657df25d"}, api.CAMERA_VIDEO_REAL_TIME, 1280, 720, 30.0, api.SM_SIZELIMITED_SLICE, false, 1, false, false, false,
	},
	{
		"res/Cisco_Absolute_Power_1280x720_30fps.yuv",
		// Allow for different output depending on whether averaging is done
		// vertically or horizontally first when downsampling.
		[2]string{"0d4bf6a3b6f09d6de7bbce6daf8002c614ee6241", "a32db3cfa66568e231d1f580d239d6468d26ce9a"},
		api.CAMERA_VIDEO_REAL_TIME, 1280, 720, 30.0, api.SM_SINGLE_SLICE, false, 4, false, false, false,
	},

	// the following values may be adjusted for times since we start tuning the strategy
	{
		"res/CiscoVT2people_320x192_12fps.yuv",
		[2]string{"fd57470eebb9b334e8edcb8b47f7fb5b5868f111"}, api.SCREEN_CONTENT_REAL_TIME, 320, 192, 12.0, api.SM_SINGLE_SLICE, false, 1, false, false, false,
	},
	{
		"res/CiscoVT2people_160x96_6fps.yuv",
		[2]string{"5f63e723c3ec82fad186b48fcbcfb54730ce3b26"}, api.SCREEN_CONTENT_REAL_TIME, 160, 96, 6.0, api.SM_SINGLE_SLICE, false, 1, false, false, false,
	},
	{
		"res/Static_152_100.yuv",
		[2]string{"e77a5b0ffb48753556e617544616fb06a049e9be"}, api.SCREEN_CONTENT_REAL_TIME, 152, 100, 6.0, api.SM_SINGLE_SLICE, false, 1, false, false, false,
	},
	{
		"res/Cisco_Absolute_Power_1280x720_30fps.yuv",
		[2]string{"f01e41426ca49932a8f1f67ad59a1700a3fa7fee"}, api.SCREEN_CONTENT_REAL_TIME, 1280, 720, 30.0, api.SM_SIZELIMITED_SLICE, false, 1, false, false, false,
	},
	//for different strategy
	{
		"res/Cisco_Absolute_Power_1280x720_30fps.yuv",
		[2]string{"4684962979bc306e35de93bed58cc84938abcdee"}, api.SCREEN_CONTENT_REAL_TIME, 1280, 720, 30.0, api.SM_SIZELIMITED_SLICE, false, 1, true, true, false,
	},
	{
		"res/CiscoVT2people_320x192_12fps.yuv",
		[2]string{"d31a72395a4ca760c5b86a06901a2557e0373e76"}, api.CAMERA_VIDEO_REAL_TIME, 320, 192, 12.0, api.SM_SINGLE_SLICE, false, 1, false, false, true, //turn on cabac
	},

	{
		"res/Cisco_Absolute_Power_1280x720_30fps.yuv",
		[2]string{"8bef37fa5965d5e650c1170d938423269f7406ac"}, api.CAMERA_VIDEO_REAL_TIME, 1280, 720, 30.0, api.SM_SIZELIMITED_SLICE, false, 1, false, false, true,
	},

	{
		"res/Cisco_Absolute_Power_1280x720_30fps.yuv",
		[2]string{"c5cb4a6f55c10485aa90f8b237fcb8697ba70d43"}, api.CAMERA_VIDEO_REAL_TIME, 1280, 720, 30.0, api.SM_FIXEDSLCNUM_SLICE, false, 1, false, false, true,
	},
}

// compareHashAnyOf is CompareHashAnyOf of test/utils/HashFunctions.h.
func compareHashAnyOf(t *testing.T, digest []byte, hashStr []string) {
	t.Helper()
	hashStrCmp := hex.EncodeToString(digest)
	for i := 0; i < len(hashStr) && hashStr[i] != ""; i++ {
		if hashStr[i] == hashStrCmp {
			return
		}
	}
	// No match found. Compare to first hash so as to produce a grepable failure.
	t.Errorf("hash mismatch: expected %s, got %s", hashStr[0], hashStrCmp)
}

// TestEncoderOutputCompareOutput is EncodeFile/EncoderOutputTest.CompareOutput.
func TestEncoderOutputCompareOutput(t *testing.T) {
	for idx := range kFileParamArray {
		p := kFileParamArray[idx]
		name := fmt.Sprintf("%02d_%s", idx, filepath.Base(p.pkcFileName))
		t.Run(name, func(t *testing.T) {
			defer encTestRecover(t)
			var b baseEncoderTest
			b.SetUp(t)
			defer b.TearDown()
			ctx := sha1.New()

			var EnxParamExt api.SEncParamExt
			encFileParamToParamExt(&p, &EnxParamExt)

			b.EncodeFile(p.pkcFileName, &EnxParamExt, func(info *api.SFrameBSInfo) {
				updateHashFromFrame(info, ctx)
			})
			digest := ctx.Sum(nil)
			if !t.Failed() {
				compareHashAnyOf(t, digest, p.pkcHashStr[:])
			}
		})
	}
}

// randomInputStream is RandomInputStream of encoder_test.cpp.
type randomInputStream struct {
	max_frames_ int
	count_      int
	rng         *rand.Rand
}

func newRandomInputStream(max_frames int) *randomInputStream {
	return &randomInputStream{max_frames_: max_frames, rng: rand.New(rand.NewSource(12345))}
}

func (r *randomInputStream) read(p []byte) int {
	if r.count_ >= r.max_frames_ {
		return 0
	}
	for i := range p {
		p[i] = byte(r.rng.Int31() % 256)
	}
	r.count_++
	return len(p)
}

func setupI420Pic(pic *api.SSourcePicture, buf []byte, width, height int32) {
	*pic = api.SSourcePicture{}
	pic.IPicWidth = width
	pic.IPicHeight = height
	pic.IColorFormat = int32(api.VideoFormatI420)
	pic.IStride[0] = pic.IPicWidth
	pic.IStride[1] = pic.IPicWidth >> 1
	pic.IStride[2] = pic.IStride[1]
	lumaSize := int(width * height)
	pic.PData[0] = buf
	pic.PData[1] = buf[lumaSize:]
	pic.PData[2] = buf[lumaSize+(lumaSize>>2):]
}

// This test uses random noise input and a very low QP to intentionally create
// poor compression (less than 1:1 ratio). This produces very large slices
// and tests the encoder's ability to handle inflated buffers correctly without
// insufficient memory errors.
func TestEncoderInitTestVeryLargeSlices(t *testing.T) {
	defer encTestRecover(t)
	var b baseEncoderTest
	b.SetUp(t)
	defer b.TearDown()
	encoder_ := b.encoder_

	var param api.SEncParamExt
	encoder_.GetDefaultParams(&param)

	param.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	param.IPicWidth = 1280
	param.IPicHeight = 720
	param.FMaxFrameRate = 30.0
	param.ISpatialLayerNum = 1
	param.IMultipleThreadIdc = 4
	param.IRCMode = api.RC_OFF_MODE

	param.SSpatialLayers[0].IVideoWidth = param.IPicWidth
	param.SSpatialLayers[0].IVideoHeight = param.IPicHeight
	param.SSpatialLayers[0].FFrameRate = param.FMaxFrameRate
	param.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
	param.SSpatialLayers[0].SSliceArgument.UiSliceNum = 4
	param.SSpatialLayers[0].IDLayerQp = 12
	param.IMinQp = 0
	param.IMaxQp = 51

	rv := encoder_.InitializeExt(&param)
	if rv != 0 {
		t.Fatalf("InitializeExt returned %d", rv)
	}

	stream := newRandomInputStream(1)

	frameSize := int(param.IPicWidth * param.IPicHeight * 3 / 2)
	buf := make([]byte, frameSize)

	var info api.SFrameBSInfo
	var pic api.SSourcePicture
	setupI420Pic(&pic, buf, param.IPicWidth, param.IPicHeight)

	for stream.read(buf) == frameSize {
		rv = encoder_.EncodeFrame(&pic, &info)
		if rv != 0 {
			t.Fatalf("EncodeFrame returned %d", rv)
		}
	}
}

// This test verifies that the encoder correctly handles screen content
// sequences with large vertical scrolling motion vectors.
func TestEncoderInitTestScreenContentScrollMotionVectorBounds(t *testing.T) {
	defer encTestRecover(t)
	var b baseEncoderTest
	b.SetUp(t)
	defer b.TearDown()
	encoder_ := b.encoder_

	var param api.SEncParamExt
	encoder_.GetDefaultParams(&param)

	param.IUsageType = api.SCREEN_CONTENT_REAL_TIME
	param.IPicWidth = 640
	param.IPicHeight = 1800
	param.FMaxFrameRate = 30.0
	param.ISpatialLayerNum = 1
	param.IRCMode = api.RC_OFF_MODE

	param.SSpatialLayers[0].IVideoWidth = param.IPicWidth
	param.SSpatialLayers[0].IVideoHeight = param.IPicHeight
	param.SSpatialLayers[0].FFrameRate = param.FMaxFrameRate
	param.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
	param.SSpatialLayers[0].IDLayerQp = 51
	param.IMinQp = 51
	param.IMaxQp = 51

	rv := encoder_.InitializeExt(&param)
	if rv != 0 {
		t.Fatalf("InitializeExt returned %d", rv)
	}

	var info api.SFrameBSInfo

	width := int(param.IPicWidth)
	height := int(param.IPicHeight)
	frameSize := width * height * 3 / 2
	frame0 := make([]uint8, frameSize)
	frame1 := make([]uint8, frameSize)
	for i := range frame0 {
		frame0[i] = 128
		frame1[i] = 128
	}

	// Fill frame0 luma with pseudo-random patterns to ensure CheckLine qualifies
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			frame0[y*width+x] = uint8((x*29 + y*43 + 17) % 251)
		}
	}

	// Frame 1: shift vertically by -512 pixels (content moving downward by 512)
	scroll_mv := -512
	for y := 0; y < height; y++ {
		if y+scroll_mv >= 0 && y+scroll_mv < height {
			copy(frame1[y*width:y*width+width], frame0[(y+scroll_mv)*width:])
		} else {
			for x := 0; x < width; x++ {
				frame1[y*width+x] = uint8((x*53 + y*71 + 101) & 0xFF)
			}
		}
	}

	// Modify macroblock (1, 32) at pixel rows 512..527 and cols 16..31 in frame1.
	for y := 512; y < 528; y++ {
		for x := 16; x < 32; x++ {
			frame1[y*width+x] ^= 0xFF
		}
	}

	var pic api.SSourcePicture
	setupI420Pic(&pic, frame0, int32(width), int32(height))

	// Encode Frame 0 (Base pattern)
	rv = encoder_.EncodeFrame(&pic, &info)
	if rv != 0 {
		t.Fatalf("EncodeFrame returned %d", rv)
	}
	pic.UiTimeStamp += 33

	// Encode Frame 1 (Scrolled pattern with modified MB)
	pic.PData[0] = frame1
	pic.PData[1] = frame1[width*height:]
	pic.PData[2] = frame1[width*height+(width*height>>2):]
	rv = encoder_.EncodeFrame(&pic, &info)
	if rv != 0 {
		t.Fatalf("EncodeFrame returned %d", rv)
	}
}

// This test verifies dynamic slice adjustment when encoding frames with extreme
// aspect ratios (very wide resolution) in RC_OFF_MODE using multiple
// slices/threads.
func TestEncoderInitTestDynamicAdjustSlicingExtremeAspectRatio(t *testing.T) {
	defer encTestRecover(t)
	var b baseEncoderTest
	b.SetUp(t)
	defer b.TearDown()
	encoder_ := b.encoder_
	rng := rand.New(rand.NewSource(1))

	var param api.SEncParamExt
	encoder_.GetDefaultParams(&param)

	param.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	param.IPicWidth = 32752
	param.IPicHeight = 32
	param.FMaxFrameRate = 30.0
	param.ISpatialLayerNum = 1
	param.IMultipleThreadIdc = 4
	param.IRCMode = api.RC_OFF_MODE
	param.BUseLoadBalancing = true
	param.IComplexityMode = api.HIGH_COMPLEXITY
	param.IMinQp = 0
	param.IMaxQp = 51

	param.SSpatialLayers[0].IVideoWidth = param.IPicWidth
	param.SSpatialLayers[0].IVideoHeight = param.IPicHeight
	param.SSpatialLayers[0].FFrameRate = param.FMaxFrameRate
	param.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
	param.SSpatialLayers[0].SSliceArgument.UiSliceNum = 4
	param.SSpatialLayers[0].IDLayerQp = 24

	rv := encoder_.InitializeExt(&param)
	if rv != 0 {
		t.Fatalf("InitializeExt returned %d", rv)
	}

	frameSize := int(param.IPicWidth * param.IPicHeight * 3 / 2)
	buf := make([]byte, frameSize)

	var info api.SFrameBSInfo
	var pic api.SSourcePicture
	setupI420Pic(&pic, buf, param.IPicWidth, param.IPicHeight)

	for i := 0; i < 10; i++ {
		for idx := 0; idx < frameSize; idx++ {
			buf[idx] = byte(rng.Int31() % 256)
		}
		for y := 0; y < 16; y++ {
			clear(pic.PData[0][y*int(pic.IStride[0]) : y*int(pic.IStride[0])+16368])
		}
		for y := 0; y < 8; y++ {
			clear(pic.PData[1][y*int(pic.IStride[1]) : y*int(pic.IStride[1])+8184])
			clear(pic.PData[2][y*int(pic.IStride[2]) : y*int(pic.IStride[2])+8184])
		}
		rv = encoder_.EncodeFrame(&pic, &info)
		if rv != 0 {
			t.Fatalf("EncodeFrame returned %d", rv)
		}
	}
}

// newTestDecoder creates and initializes a decoder the way encoder_test.cpp
// does.
func newTestDecoder(t *testing.T) api.ISVCDecoder {
	t.Helper()
	var dec api.ISVCDecoder
	rv := decoder.WelsCreateDecoder(&dec)
	if rv != 0 || dec == nil {
		t.Fatalf("WelsCreateDecoder returned %d", rv)
	}

	var decParam api.SDecodingParam
	decParam.UiTargetDqLayer = math.MaxUint8
	decParam.EEcActiveIdc = api.ERROR_CON_SLICE_COPY
	decParam.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_DEFAULT

	rv = dec.Initialize(&decParam)
	if rv != 0 {
		t.Fatalf("decoder Initialize returned %d", rv)
	}
	return dec
}

// decodeOne decodes one access unit (flushing if necessary) and returns the
// output planes and buffer info.
func decodeOne(t *testing.T, dec api.ISVCDecoder, bs []byte, length int32) ([3][]byte, api.SBufferInfo) {
	t.Helper()
	var pData [3][]byte
	var dstBufInfo api.SBufferInfo

	rv := dec.DecodeFrame2(bs, length, &pData, &dstBufInfo)
	if rv != 0 {
		t.Fatalf("DecodeFrame2 returned %d", rv)
	}

	if dstBufInfo.IBufferStatus == 0 {
		rv = dec.DecodeFrame2(nil, 0, &pData, &dstBufInfo)
		if rv != 0 {
			t.Fatalf("DecodeFrame2 (flush) returned %d", rv)
		}
	}

	if dstBufInfo.IBufferStatus != 1 {
		t.Fatalf("iBufferStatus = %d, want 1", dstBufInfo.IBufferStatus)
	}
	return pData, dstBufInfo
}

// This test verifies that the encoder correctly handles frames with distinct,
// asymmetric strides for the U and V chroma planes.
func TestEncoderInitTestCustomChromaPlaneStrides(t *testing.T) {
	defer encTestRecover(t)
	var b baseEncoderTest
	b.SetUp(t)
	defer b.TearDown()
	encoder_ := b.encoder_

	var param api.SEncParamExt
	encoder_.GetDefaultParams(&param)

	param.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	param.IPicWidth = 64
	param.IPicHeight = 64
	param.FMaxFrameRate = 30.0
	param.ISpatialLayerNum = 1
	param.IRCMode = api.RC_OFF_MODE

	param.SSpatialLayers[0].IVideoWidth = param.IPicWidth
	param.SSpatialLayers[0].IVideoHeight = param.IPicHeight
	param.SSpatialLayers[0].FFrameRate = param.FMaxFrameRate
	param.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
	param.SSpatialLayers[0].IDLayerQp = 0

	rv := encoder_.InitializeExt(&param)
	if rv != 0 {
		t.Fatalf("InitializeExt returned %d", rv)
	}

	// Initialize a decoder to verify correctness of the output
	dec := newTestDecoder(t)
	defer decoder.WelsDestroyDecoder(dec)

	var info api.SFrameBSInfo

	// Configure distinct strides: U stride is much larger than V stride.
	strideY := 64
	strideU := 4096
	strideV := 32

	h := int(param.IPicHeight)
	bufY := make([]uint8, strideY*h)
	bufU := make([]uint8, strideU*(h>>1))
	bufV := make([]uint8, strideV*(h>>1))
	generatePattern(bufY, strideY, bufU, strideU, bufV, strideV, int(param.IPicWidth), h)

	var pic api.SSourcePicture
	pic.IPicWidth = param.IPicWidth
	pic.IPicHeight = param.IPicHeight
	pic.IColorFormat = int32(api.VideoFormatI420)
	pic.IStride[0] = int32(strideY)
	pic.IStride[1] = int32(strideU)
	pic.IStride[2] = int32(strideV)
	pic.PData[0] = bufY
	pic.PData[1] = bufU
	pic.PData[2] = bufV

	rv = encoder_.EncodeFrame(&pic, &info)
	if rv != 0 {
		t.Fatalf("EncodeFrame returned %d", rv)
	}

	// Calculate total bitstream size
	length := frameBsLength(&info)
	if length <= 0 {
		t.Fatalf("bitstream length %d", length)
	}

	// Decode the encoded frame
	pData, dstBufInfo := decodeOne(t, dec, info.SLayerInfo[0].PBsBuf, length)

	// Verify that the decoded YUV content matches our original pattern (high PSNR)
	decodedWidthU := int(dstBufInfo.UsrData.SSystemBuffer.IWidth >> 1)
	decodedHeightU := int(dstBufInfo.UsrData.SSystemBuffer.IHeight >> 1)
	strideDecY := int(dstBufInfo.UsrData.SSystemBuffer.IStride[0])
	strideDecChroma := int(dstBufInfo.UsrData.SSystemBuffer.IStride[1])

	if pData[0] == nil || pData[1] == nil || pData[2] == nil {
		t.Fatalf("decoder returned nil planes")
	}

	psnrY := calculatePlanePsnr(bufY, strideY, pData[0], strideDecY, int(param.IPicWidth), h)
	psnrU := calculatePlanePsnr(bufU, strideU, pData[1], strideDecChroma, decodedWidthU, decodedHeightU)
	psnrV := calculatePlanePsnr(bufV, strideV, pData[2], strideDecChroma, decodedWidthU, decodedHeightU)

	// With lossless QP=0, PSNR should be extremely high (effectively identical)
	if psnrY <= 40.0 || psnrU <= 40.0 || psnrV <= 40.0 {
		t.Errorf("PSNR too low: Y=%f U=%f V=%f", psnrY, psnrU, psnrV)
	}
}

// This test verifies that the encoder safely returns early when the input
// source picture has U or V strides that are smaller than the required
// width/2.
func TestEncoderInitTestCustomChromaPlaneStridesInvalidSrc(t *testing.T) {
	defer encTestRecover(t)
	var b baseEncoderTest
	b.SetUp(t)
	defer b.TearDown()
	encoder_ := b.encoder_

	var param api.SEncParamExt
	encoder_.GetDefaultParams(&param)

	param.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	param.IPicWidth = 64
	param.IPicHeight = 64
	param.FMaxFrameRate = 30.0
	param.ISpatialLayerNum = 1
	param.IRCMode = api.RC_OFF_MODE

	param.SSpatialLayers[0].IVideoWidth = param.IPicWidth
	param.SSpatialLayers[0].IVideoHeight = param.IPicHeight
	param.SSpatialLayers[0].FFrameRate = param.FMaxFrameRate
	param.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
	param.SSpatialLayers[0].IDLayerQp = 0

	rv := encoder_.InitializeExt(&param)
	if rv != 0 {
		t.Fatalf("InitializeExt returned %d", rv)
	}

	var info api.SFrameBSInfo

	strideY := 64
	strideU := 16 // Invalid: must be >= 32
	strideV := 32

	h := int(param.IPicHeight)
	fill := func(n int, v uint8) []uint8 {
		s := make([]uint8, n)
		for i := range s {
			s[i] = v
		}
		return s
	}
	bufY := fill(strideY*h, 128)
	bufU := fill(strideU*(h>>1), 100)
	bufV := fill(strideV*(h>>1), 200)

	var pic api.SSourcePicture
	pic.IPicWidth = param.IPicWidth
	pic.IPicHeight = param.IPicHeight
	pic.IColorFormat = int32(api.VideoFormatI420)
	pic.IStride[0] = int32(strideY)
	pic.IStride[1] = int32(strideU)
	pic.IStride[2] = int32(strideV)
	pic.PData[0] = bufY
	pic.PData[1] = bufU
	pic.PData[2] = bufV

	// This should reject the frame and return cmUnsupportedData
	rv = encoder_.EncodeFrame(&pic, &info)
	if rv != int32(api.CmUnsupportedData) {
		t.Fatalf("EncodeFrame returned %d, want cmUnsupportedData", rv)
	}
}

// A source picture is allowed to exceed the per-frame macroblock limit as long
// as the spatial layer it is coded into does not.
func TestEncoderInitTestSourceLargerThanMaxMbsPerFrame(t *testing.T) {
	defer encTestRecover(t)
	var b baseEncoderTest
	b.SetUp(t)
	defer b.TearDown()
	encoder_ := b.encoder_

	const kMaxPixelsPerFrame = 36864 * 256
	const kLayerWidth = 2048
	const kLayerHeight = 1280

	var param api.SEncParamExt
	encoder_.GetDefaultParams(&param)

	param.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	param.IPicWidth = 4096
	param.IPicHeight = 2560
	param.FMaxFrameRate = 30.0
	param.ISpatialLayerNum = 1
	param.IRCMode = api.RC_OFF_MODE

	if !(param.IPicWidth*param.IPicHeight > kMaxPixelsPerFrame) || !(kLayerWidth*kLayerHeight <= kMaxPixelsPerFrame) {
		t.Fatalf("bad test geometry")
	}

	param.SSpatialLayers[0].IVideoWidth = kLayerWidth
	param.SSpatialLayers[0].IVideoHeight = kLayerHeight
	param.SSpatialLayers[0].FFrameRate = param.FMaxFrameRate
	param.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
	param.SSpatialLayers[0].IDLayerQp = 26

	rv := encoder_.InitializeExt(&param)
	if rv != 0 {
		t.Fatalf("InitializeExt returned %d", rv)
	}

	strideY := int(param.IPicWidth)
	strideUV := int(param.IPicWidth >> 1)
	h := int(param.IPicHeight)
	bufY := make([]uint8, strideY*h)
	bufU := make([]uint8, strideUV*(h>>1))
	bufV := make([]uint8, strideUV*(h>>1))
	generatePattern(bufY, strideY, bufU, strideUV, bufV, strideUV, int(param.IPicWidth), h)

	var pic api.SSourcePicture
	pic.IPicWidth = param.IPicWidth
	pic.IPicHeight = param.IPicHeight
	pic.IColorFormat = int32(api.VideoFormatI420)
	pic.IStride[0] = int32(strideY)
	pic.IStride[1] = int32(strideUV)
	pic.IStride[2] = int32(strideUV)
	pic.PData[0] = bufY
	pic.PData[1] = bufU
	pic.PData[2] = bufV

	var info api.SFrameBSInfo
	rv = encoder_.EncodeFrame(&pic, &info)
	if rv != 0 {
		t.Fatalf("EncodeFrame returned %d", rv)
	}
	if info.EFrameType != api.VideoFrameTypeIDR {
		t.Fatalf("eFrameType = %d, want IDR", info.EFrameType)
	}

	length := frameBsLength(&info)
	if length <= 0 {
		t.Fatalf("bitstream length %d", length)
	}

	// The coded frame must carry the layer geometry, not the source geometry.
	dec := newTestDecoder(t)
	defer decoder.WelsDestroyDecoder(dec)
	_, dstBufInfo := decodeOne(t, dec, info.SLayerInfo[0].PBsBuf, length)
	if dstBufInfo.UsrData.SSystemBuffer.IWidth != kLayerWidth {
		t.Errorf("decoded width %d, want %d", dstBufInfo.UsrData.SSystemBuffer.IWidth, kLayerWidth)
	}
	if dstBufInfo.UsrData.SSystemBuffer.IHeight != kLayerHeight {
		t.Errorf("decoded height %d, want %d", dstBufInfo.UsrData.SSystemBuffer.IHeight, kLayerHeight)
	}
}

// SSourcePicture.bPsnrY/U/V asks for the PSNR of a single frame and the result
// is handed back in SLayerBSInfo.rPsnr.
func TestEncoderInitTestPerFramePsnr(t *testing.T) {
	defer encTestRecover(t)
	var b baseEncoderTest
	b.SetUp(t)
	defer b.TearDown()
	encoder_ := b.encoder_

	var param api.SEncParamExt
	encoder_.GetDefaultParams(&param)

	param.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	param.IPicWidth = 176
	param.IPicHeight = 144
	param.FMaxFrameRate = 30.0
	param.ISpatialLayerNum = 1
	param.IRCMode = api.RC_OFF_MODE

	param.SSpatialLayers[0].IVideoWidth = param.IPicWidth
	param.SSpatialLayers[0].IVideoHeight = param.IPicHeight
	param.SSpatialLayers[0].FFrameRate = param.FMaxFrameRate
	param.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
	param.SSpatialLayers[0].IDLayerQp = 0

	rv := encoder_.InitializeExt(&param)
	if rv != 0 {
		t.Fatalf("InitializeExt returned %d", rv)
	}

	strideY := int(param.IPicWidth)
	strideU := int(param.IPicWidth >> 1)
	strideV := int(param.IPicWidth >> 1)
	h := int(param.IPicHeight)
	bufY := make([]uint8, strideY*h)
	bufU := make([]uint8, strideU*(h>>1))
	bufV := make([]uint8, strideV*(h>>1))
	generatePattern(bufY, strideY, bufU, strideU, bufV, strideV, int(param.IPicWidth), h)

	var pic api.SSourcePicture
	pic.IPicWidth = param.IPicWidth
	pic.IPicHeight = param.IPicHeight
	pic.IColorFormat = int32(api.VideoFormatI420)
	pic.IStride[0] = int32(strideY)
	pic.IStride[1] = int32(strideU)
	pic.IStride[2] = int32(strideV)
	pic.PData[0] = bufY
	pic.PData[1] = bufU
	pic.PData[2] = bufV

	var info api.SFrameBSInfo

	// nothing was asked for, so nothing is reported
	info = api.SFrameBSInfo{}
	rv = encoder_.EncodeFrame(&pic, &info)
	if rv != 0 || info.ILayerNum <= 0 {
		t.Fatalf("EncodeFrame returned %d, iLayerNum %d", rv, info.ILayerNum)
	}
	for i := 0; i < 3; i++ {
		if info.SLayerInfo[0].RPsnr[i] != 0 {
			t.Errorf("rPsnr[%d] = %f, want 0", i, info.SLayerInfo[0].RPsnr[i])
		}
	}

	// ask for a single component, the other two stay untouched
	info = api.SFrameBSInfo{}
	pic.BPsnrY = true
	rv = encoder_.EncodeFrame(&pic, &info)
	if rv != 0 || info.ILayerNum <= 0 {
		t.Fatalf("EncodeFrame returned %d, iLayerNum %d", rv, info.ILayerNum)
	}
	if v := info.SLayerInfo[0].RPsnr[0]; !(v > 30.0 && v <= 99.99) {
		t.Errorf("rPsnr[0] = %f", v)
	}
	if info.SLayerInfo[0].RPsnr[1] != 0 || info.SLayerInfo[0].RPsnr[2] != 0 {
		t.Errorf("rPsnr[1..2] = %f %f, want 0", info.SLayerInfo[0].RPsnr[1], info.SLayerInfo[0].RPsnr[2])
	}

	// ask for all three of them
	info = api.SFrameBSInfo{}
	pic.BPsnrU = true
	pic.BPsnrV = true
	rv = encoder_.EncodeFrame(&pic, &info)
	if rv != 0 || info.ILayerNum <= 0 {
		t.Fatalf("EncodeFrame returned %d, iLayerNum %d", rv, info.ILayerNum)
	}
	for i := 0; i < 3; i++ {
		if v := info.SLayerInfo[0].RPsnr[i]; !(v > 30.0 && v <= 99.99) {
			t.Errorf("component %d: rPsnr = %f", i, v)
		}
	}
}
