// Port of test/encoder/EncUT_EncoderExt.cpp and
// test/encoder/EncUT_InterfaceTest.cpp.
//
// C rand() is replaced by a seeded math/rand generator (rand31 returns a
// value in [0, RAND_MAX=2^31-1] like glibc's rand()).

package encoder

import (
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// ---------------------------------------------------------------------------
// EncUT_InterfaceTest.cpp

func TestEncInterfaceCallTestBaseParameterVerify(t *testing.T) {
	defer encTestRecover(t)
	var b baseEncoderTest
	b.SetUp(t)
	defer b.TearDown()
	encoder_ := b.encoder_

	uiTraceLevel := int32(api.WELS_LOG_QUIET)
	encoder_.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &uiTraceLevel)

	var baseparam api.SEncParamBase

	baseparam.IPicWidth = 0
	baseparam.IPicHeight = 7896

	ret := encoder_.Initialize(&baseparam)
	if ret != int32(api.CmInitParaError) {
		t.Errorf("Initialize returned %d, want cmInitParaError", ret)
	}

	uiTraceLevel = api.WELS_LOG_ERROR
	encoder_.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &uiTraceLevel)
}

func TestEncInterfaceCallTestSetOptionLTR(t *testing.T) {
	defer encTestRecover(t)
	var b baseEncoderTest
	b.SetUp(t)
	defer b.TearDown()
	encoder_ := b.encoder_
	rng := rand.New(rand.NewSource(1))

	iTotalFrameNum := 100
	iFrameNum := 0
	width := int32(320)
	height := int32(192)

	var baseparam api.SEncParamBase
	baseparam.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	baseparam.FMaxFrameRate = 12
	baseparam.IPicWidth = width
	baseparam.IPicHeight = height
	baseparam.ITargetBitrate = 5000000
	encoder_.Initialize(&baseparam)

	frameSize := int(width * height * 3 / 2)
	buf := make([]byte, frameSize)

	var info api.SFrameBSInfo
	var pic api.SSourcePicture
	setupI420Pic(&pic, buf, width, height)

	var config api.SLTRConfig
	config.BEnableLongTermReference = true
	config.ILTRRefNum = rng.Int31() % 4
	encoder_.SetOption(api.ENCODER_OPTION_LTR, &config)
	for {
		f, err := os.Open(filepath.Join(resDir, "res/CiscoVT2people_320x192_12fps.yuv"))
		if err != nil {
			t.Skipf("OpenH264 conformance data is unavailable: %v", err)
		}
		for {
			n, _ := io.ReadFull(f, buf)
			if n != frameSize {
				break
			}
			ret := encoder_.EncodeFrame(&pic, &info)
			if ret != int32(api.CmResultSuccess) {
				f.Close()
				t.Fatalf("EncodeFrame returned %d", ret)
			}
			if info.EFrameType != api.VideoFrameTypeSkip {
				iFrameNum++
			}
		}
		f.Close()
		if !(iFrameNum < iTotalFrameNum) {
			break
		}
	}
}

// ---------------------------------------------------------------------------
// EncUT_EncoderExt.cpp

const (
	encUT_MB_SIZE         = 16
	encUT_MAX_WIDTH       = 3840
	encUT_MAX_HEIGHT      = 2160
	encUT_MEM_VARY_SIZE   = 512
	encUT_IMAGE_VARY_SIZE = 512
	encUT_TEST_FRAMES     = 30

	encUT_NAL_HEADER_BYTES   = 4
	encUT_NAL_TYPE           = 0x0F
	encUT_SPS_NAL_TYPE       = 7
	encUT_PPS_NAL_TYPE       = 8
	encUT_SUBSETSPS_NAL_TYPE = 15
)

func encUT_VALID_SIZE(iSize int32) int32 {
	if iSize > 16 {
		return iSize
	}
	return 16
}

func encUT_GET_NAL_TYPE(pNalStart []uint8) int32 {
	return int32(pNalStart[encUT_NAL_HEADER_BYTES] & encUT_NAL_TYPE)
}

func encUT_IS_PARASET(iNalType int32) bool {
	return (iNalType == encUT_SPS_NAL_TYPE) || (iNalType == encUT_PPS_NAL_TYPE) || (iNalType == encUT_SUBSETSPS_NAL_TYPE)
}

type encoderInterfaceTest struct {
	t         *testing.T
	pPtrEnc   api.ISVCEncoder
	pParamExt *api.SEncParamExt
	pSrcPic   *api.SSourcePicture
	pOption   *api.SEncParamExt
	pYUV      []uint8
	sFbi      api.SFrameBSInfo

	m_iWidth      int32
	m_iHeight     int32
	m_iPicResSize int32

	rng *rand.Rand
}

// rand31 is C rand().
func (e *encoderInterfaceTest) rand31() int32 {
	return e.rng.Int31()
}

func newEncoderInterfaceTest(t *testing.T) *encoderInterfaceTest {
	e := &encoderInterfaceTest{t: t, rng: rand.New(rand.NewSource(1))}
	rv := WelsCreateSVCEncoder(&e.pPtrEnc)
	if rv != 0 || e.pPtrEnc == nil {
		t.Fatalf("WelsCreateSVCEncoder returned %d", rv)
	}

	uiTraceLevel := uint32(api.WELS_LOG_ERROR)
	e.pPtrEnc.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &uiTraceLevel)

	e.pParamExt = &api.SEncParamExt{}
	e.pSrcPic = &api.SSourcePicture{}
	e.pOption = &api.SEncParamExt{}

	e.m_iWidth = encUT_MAX_WIDTH
	e.m_iHeight = encUT_MAX_HEIGHT
	e.m_iPicResSize = e.m_iWidth * e.m_iHeight * 3 >> 1
	e.pYUV = make([]uint8, e.m_iPicResSize)
	return e
}

func (e *encoderInterfaceTest) TearDown() {
	if e.pPtrEnc != nil {
		WelsDestroySVCEncoder(e.pPtrEnc)
		e.pPtrEnc = nil
	}
}

func (e *encoderInterfaceTest) expectEq(got, want int32, what string) {
	e.t.Helper()
	if got != want {
		e.t.Errorf("%s: got %d, want %d", what, got, want)
	}
}

func (e *encoderInterfaceTest) PrepareOneSrcFrame() {
	e.pSrcPic.IColorFormat = int32(api.VideoFormatI420)
	e.pSrcPic.UiTimeStamp = 0
	e.pSrcPic.IPicWidth = e.pParamExt.IPicWidth
	e.pSrcPic.IPicHeight = e.pParamExt.IPicHeight
	e.pSrcPic.IPicWidth = encUT_VALID_SIZE(e.pParamExt.IPicWidth)
	e.pSrcPic.IPicHeight = encUT_VALID_SIZE(e.pParamExt.IPicHeight)
	e.m_iWidth = e.pSrcPic.IPicWidth
	e.m_iHeight = e.pSrcPic.IPicHeight
	e.m_iPicResSize = e.m_iWidth * e.m_iHeight * 3 >> 1

	e.pYUV[0] = uint8(e.rand31() % 256)
	for i := int32(1); i < e.m_iPicResSize; i++ {
		if (i % 256) == 0 {
			e.pYUV[i] = uint8(e.rand31() % 256)
		} else {
			e.pYUV[i] = uint8((int32(e.pYUV[i-1]) + (e.rand31() % 3) - 1) & 0xff)
		}
	}
	e.pSrcPic.IStride[0] = e.m_iWidth
	e.pSrcPic.IStride[1] = e.pSrcPic.IStride[0] >> 1
	e.pSrcPic.IStride[2] = e.pSrcPic.IStride[1]

	e.pSrcPic.PData[0] = e.pYUV
	e.pSrcPic.PData[1] = e.pYUV[e.m_iWidth*e.m_iHeight:]
	e.pSrcPic.PData[2] = e.pYUV[e.m_iWidth*e.m_iHeight+(e.m_iWidth*e.m_iHeight>>2):]

	e.sFbi = api.SFrameBSInfo{}
}

func (e *encoderInterfaceTest) EncodeOneFrame(pEncParamBase *api.SEncParamBase) {
	e.t.Helper()
	iResult := e.pPtrEnc.Initialize(pEncParamBase)
	e.expectEq(iResult, int32(api.CmResultSuccess), "Initialize")
	e.PrepareOneSrcFrame()
	iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
	e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
	e.expectEq(int32(e.sFbi.EFrameType), int32(api.VideoFrameTypeIDR), "eFrameType")

	iResult = e.pPtrEnc.GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_BASE, pEncParamBase)
	e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption")

	e.pPtrEnc.Uninitialize()
	e.expectEq(iResult, int32(api.CmResultSuccess), "Uninitialize")
}

func (e *encoderInterfaceTest) EncodeOneIDRandP(pPtrEnc api.ISVCEncoder) {
	e.t.Helper()
	iResult := pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
	e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
	e.expectEq(int32(e.sFbi.EFrameType), int32(api.VideoFrameTypeIDR), "eFrameType")

	e.pSrcPic.UiTimeStamp += 30
	iResult = pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
	e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
}

func (e *encoderInterfaceTest) InitializeParamExt() {
	e.pParamExt.IPicWidth = 1280
	e.pParamExt.IPicHeight = 720
	e.pParamExt.ITargetBitrate = 50000
	e.pParamExt.ITemporalLayerNum = 3
	e.pParamExt.ISpatialLayerNum = 1
	e.pParamExt.INumRefFrame = api.AUTO_REF_PIC_COUNT
	e.pParamExt.SSpatialLayers[0].IVideoHeight = e.pParamExt.IPicHeight
	e.pParamExt.SSpatialLayers[0].IVideoWidth = e.pParamExt.IPicWidth
	e.pParamExt.SSpatialLayers[0].ISpatialBitrate = 50000
}

func runEncoderInterfaceTest(t *testing.T, body func(e *encoderInterfaceTest)) {
	defer encTestRecover(t)
	e := newEncoderInterfaceTest(t)
	defer e.TearDown()
	body(e)
}

func TestEncoderInterfaceTestEncoderOptionSetTest(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var iResult, iValue, iReturn int32
		var fFrameRate, fReturn float32
		uiTraceLevel := int32(api.WELS_LOG_QUIET)

		e.pPtrEnc.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &uiTraceLevel)

		e.InitializeParamExt()
		iResult = e.pPtrEnc.InitializeExt(e.pParamExt)
		e.expectEq(iResult, int32(api.CmResultSuccess), "InitializeExt")

		e.PrepareOneSrcFrame()

		eOptionId := api.ENCODER_OPTION_DATAFORMAT
		iValue = e.rand31() % 256
		iResult = e.pPtrEnc.SetOption(eOptionId, &iValue)

		if iValue == 0 {
			e.expectEq(iResult, int32(api.CmInitParaError), "SetOption DATAFORMAT")
		} else {
			e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption DATAFORMAT")
		}

		iResult = e.pPtrEnc.GetOption(eOptionId, &iReturn)
		e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption DATAFORMAT")
		e.expectEq(iValue, iReturn, "DATAFORMAT value")

		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
		e.pSrcPic.UiTimeStamp += 30

		eOptionId = api.ENCODER_OPTION_IDR_INTERVAL
		iValue = e.rand31()%256 - 5
		iResult = e.pPtrEnc.SetOption(eOptionId, &iValue)
		e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption IDR_INTERVAL")

		iResult = e.pPtrEnc.GetOption(eOptionId, &iReturn)
		e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption IDR_INTERVAL")
		if iValue <= -1 {
			iValue = 0
		}
		e.expectEq(iValue, iReturn, "IDR_INTERVAL value")

		e.PrepareOneSrcFrame()
		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
		e.pSrcPic.UiTimeStamp += 30

		eOptionId = api.ENCODER_OPTION_FRAME_RATE
		fFrameRate = float32(e.rand31()%100 - 5)
		iResult = e.pPtrEnc.SetOption(eOptionId, &fFrameRate)

		if fFrameRate <= 0 {
			e.expectEq(iResult, int32(api.CmInitParaError), "SetOption FRAME_RATE")
		} else {
			e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption FRAME_RATE")

			fFrameRate = common.WELS_CLIP3(fFrameRate, 1, 60)
			iResult = e.pPtrEnc.GetOption(eOptionId, &fReturn)
			e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption FRAME_RATE")
			if fFrameRate != fReturn {
				e.t.Errorf("FRAME_RATE: got %f, want %f", fReturn, fFrameRate)
			}
		}
		e.PrepareOneSrcFrame()
		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
		e.pSrcPic.UiTimeStamp += 30

		for _, eOpt := range []api.ENCODER_OPTION{api.ENCODER_OPTION_BITRATE, api.ENCODER_OPTION_MAX_BITRATE} {
			eOptionId = eOpt
			var sInfo, sReturn api.SBitrateInfo
			sInfo.IBitrate = e.rand31()%100000 - 100
			sInfo.ILayer = api.SPATIAL_LAYER_0
			iResult = e.pPtrEnc.SetOption(eOptionId, &sInfo)
			e.pPtrEnc.GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, e.pParamExt)
			if sInfo.IBitrate <= 0 || (float32(e.pParamExt.SSpatialLayers[sInfo.ILayer].ISpatialBitrate) <= fFrameRate) {
				e.expectEq(iResult, int32(api.CmInitParaError), "SetOption BITRATE")
			} else if e.pParamExt.SSpatialLayers[sInfo.ILayer].ISpatialBitrate >
				e.pParamExt.SSpatialLayers[sInfo.ILayer].IMaxSpatialBitrate {
				e.expectEq(iResult, int32(api.CmInitParaError), "SetOption BITRATE")
			} else {
				e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption BITRATE")
				sReturn.ILayer = api.SPATIAL_LAYER_0
				iResult = e.pPtrEnc.GetOption(eOptionId, &sReturn)
				e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption BITRATE")
				e.expectEq(common.WELS_CLIP3(sInfo.IBitrate, 1, 2147483647), sReturn.IBitrate, "BITRATE value")
			}
			e.PrepareOneSrcFrame()
			iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
			e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
			e.pSrcPic.UiTimeStamp += 30
		}

		eOptionId = api.ENCODER_OPTION_RC_MODE
		iValue = (e.rand31() % 4) - 1
		iResult = e.pPtrEnc.SetOption(eOptionId, &iValue)
		e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption RC_MODE")

		e.PrepareOneSrcFrame()
		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
		e.pSrcPic.UiTimeStamp += 30

		uiTraceLevel = api.WELS_LOG_ERROR
		e.pPtrEnc.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &uiTraceLevel)
	})
}

func TestEncoderInterfaceTestMemoryCheckTest(t *testing.T) {
	if testing.Short() {
		t.Skip("long running")
	}
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		e.InitializeParamExt()
		e.pParamExt.IPicWidth = 1280
		e.pParamExt.IPicHeight = 720

		iResult := e.pPtrEnc.InitializeExt(e.pParamExt)
		kiFrameNumber := encUT_TEST_FRAMES

		e.m_iWidth = e.pParamExt.IPicWidth
		e.m_iHeight = e.pParamExt.IPicHeight
		e.m_iPicResSize = e.m_iWidth * e.m_iHeight * 3 >> 1
		e.pYUV = make([]uint8, e.m_iPicResSize)
		e.PrepareOneSrcFrame()

		varyAndEncode := func() {
			for i := 0; i < kiFrameNumber; i++ {
				iStartX := e.rand31() % (e.m_iPicResSize >> 1)
				iEndX := (iStartX + (e.rand31() % encUT_MEM_VARY_SIZE)) % e.m_iPicResSize
				for j := iStartX; j < iEndX; j++ {
					e.pYUV[j] = uint8(e.rand31() % 256)
				}

				iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
				e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
				e.pSrcPic.UiTimeStamp += 30
			}
		}
		varyAndEncode()

		e.pParamExt.IPicWidth += (e.rand31() << 1) % encUT_IMAGE_VARY_SIZE
		e.pParamExt.IPicHeight += (e.rand31() << 1) % encUT_IMAGE_VARY_SIZE
		e.m_iWidth = e.pParamExt.IPicWidth
		e.m_iHeight = e.pParamExt.IPicHeight
		e.m_iPicResSize = e.m_iWidth * e.m_iHeight * 3 >> 1
		e.pYUV = make([]uint8, e.m_iPicResSize)

		iResult = e.pPtrEnc.InitializeExt(e.pParamExt)
		e.PrepareOneSrcFrame()

		eOptionId := api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT
		*e.pOption = *e.pParamExt
		e.pOption.IPicWidth = e.m_iWidth
		e.pOption.IPicHeight = e.m_iHeight
		iResult = e.pPtrEnc.SetOption(eOptionId, e.pOption)
		e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption PARAM_EXT")

		varyAndEncode()

		e.pOption.ILTRRefNum += e.rand31()%8 + 1
		iResult = e.pPtrEnc.SetOption(eOptionId, e.pOption)
		e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption PARAM_EXT (LTR)")

		varyAndEncode()

		iResult = e.pPtrEnc.Uninitialize()
		e.expectEq(iResult, int32(api.CmResultSuccess), "Uninitialize")
	})
}

func (e *encoderInterfaceTest) GetValidEncParamBase(pEncParamBase *api.SEncParamBase) {
	pEncParamBase.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	pEncParamBase.IPicWidth = 2 + ((e.rand31() % ((encUT_MAX_WIDTH >> 1) - 1)) << 1)
	pEncParamBase.IPicHeight = 2 + ((e.rand31() % ((encUT_MAX_HEIGHT >> 1) - 1)) << 1)
	pEncParamBase.IPicWidth = encUT_VALID_SIZE(pEncParamBase.IPicWidth)
	pEncParamBase.IPicHeight = encUT_VALID_SIZE(pEncParamBase.IPicHeight)
	pEncParamBase.ITargetBitrate = e.rand31() + 1 //!=0
	// Force a bitrate of at least w*h/50, otherwise we will only get skipped frames
	pEncParamBase.ITargetBitrate = common.WELS_CLIP3(pEncParamBase.ITargetBitrate,
		pEncParamBase.IPicWidth*pEncParamBase.IPicHeight/50, 60000000)
	iLevelMaxBitrate := int32(common.G_ksLevelLimits[common.LEVEL_NUMBER-1].UiMaxBR * common.CpbBrNalFactor)
	if pEncParamBase.ITargetBitrate > iLevelMaxBitrate {
		pEncParamBase.ITargetBitrate = iLevelMaxBitrate
	}
	pEncParamBase.IRCMode = api.RC_BITRATE_MODE             //-1, 0, 1, 2
	pEncParamBase.FMaxFrameRate = float32(e.rand31()) + 0.5 //!=0
}

func TestEncoderInterfaceTestBasicInitializeTest(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamBase api.SEncParamBase
		e.GetValidEncParamBase(&sEncParamBase)

		iResult := e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmResultSuccess), "Initialize")

		e.PrepareOneSrcFrame()

		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
		e.expectEq(int32(e.sFbi.EFrameType), int32(api.VideoFrameTypeIDR), "eFrameType")

		iResult = e.pPtrEnc.Uninitialize()
		e.expectEq(iResult, int32(api.CmResultSuccess), "Uninitialize")
	})
}

func TestEncoderInterfaceTestBaseParamSettingTest(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamBase api.SEncParamBase
		e.GetValidEncParamBase(&sEncParamBase)

		iResult := e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmResultSuccess), "Initialize")

		e.PrepareOneSrcFrame()

		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
		e.expectEq(int32(e.sFbi.EFrameType), int32(api.VideoFrameTypeIDR), "eFrameType")

		e.GetValidEncParamBase(&sEncParamBase)
		iResult = e.pPtrEnc.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_BASE, &sEncParamBase)

		e.PrepareOneSrcFrame()

		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")

		iResult = e.pPtrEnc.Uninitialize()
		e.expectEq(iResult, int32(api.CmResultSuccess), "Uninitialize")
	})
}

func TestEncoderInterfaceTestBasicInitializeTestFalse(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var iResult int32
		var sEncParamBase api.SEncParamBase
		uiTraceLevel := int32(api.WELS_LOG_QUIET)

		e.pPtrEnc.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &uiTraceLevel)
		//iUsageType
		e.GetValidEncParamBase(&sEncParamBase)
		sEncParamBase.IUsageType = api.EUsageType(2)
		iResult = e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmInitParaError), "iUsageType=2")

		//iPicWidth
		e.GetValidEncParamBase(&sEncParamBase)
		sEncParamBase.IPicWidth = 0
		iResult = e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmInitParaError), "iPicWidth=0")

		e.GetValidEncParamBase(&sEncParamBase)
		sEncParamBase.IPicWidth = -1
		iResult = e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmInitParaError), "iPicWidth=-1")

		//iPicHeight
		e.GetValidEncParamBase(&sEncParamBase)
		sEncParamBase.IPicHeight = 0
		iResult = e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmInitParaError), "iPicHeight=0")

		e.GetValidEncParamBase(&sEncParamBase)
		sEncParamBase.IPicHeight = -1
		iResult = e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmInitParaError), "iPicHeight=-1")

		//iPicWidth * iPicHeight <= 36864 * 16 * 16, from Level 5.2 constraint
		//Initialize test: FALSE
		e.GetValidEncParamBase(&sEncParamBase)
		sEncParamBase.IPicWidth = 5000
		sEncParamBase.IPicHeight = 5000
		iResult = e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmInitParaError), "5000x5000")
		//Initialize test: TRUE, with SetOption test following
		e.GetValidEncParamBase(&sEncParamBase)
		sEncParamBase.IPicWidth = 36864
		sEncParamBase.IPicHeight = 256
		iResult = e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmResultSuccess), "36864x256")
		sEncParamBase.IPicWidth = 256
		sEncParamBase.IPicHeight = 36864
		iResult = e.pPtrEnc.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_BASE, &sEncParamBase)
		e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption 256x36864")
		sEncParamBase.IPicWidth = 5000
		sEncParamBase.IPicHeight = 5000
		iResult = e.pPtrEnc.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_BASE, &sEncParamBase)
		e.expectEq(iResult, int32(api.CmInitParaError), "SetOption 5000x5000")
		e.pPtrEnc.Uninitialize()

		//iTargetBitrate
		e.GetValidEncParamBase(&sEncParamBase)
		sEncParamBase.ITargetBitrate = 0
		iResult = e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmInitParaError), "iTargetBitrate=0")
		e.GetValidEncParamBase(&sEncParamBase)
		sEncParamBase.ITargetBitrate = -1
		iResult = e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmInitParaError), "iTargetBitrate=-1")

		uiTraceLevel = api.WELS_LOG_ERROR
		e.pPtrEnc.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &uiTraceLevel)
	})
}

func TestEncoderInterfaceTestInitializeExtRejectsGeometryOverflow(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		sCodingParam := NewSWelsSvcCodingParam()
		sCodingParam.FillDefault()

		// 46368 * 46368 overflows signed int32_t, so this must be validated using
		// widened arithmetic in ParamValidationExt().
		sCodingParam.IPicWidth = 46368
		sCodingParam.IPicHeight = 46368
		sCodingParam.ISpatialLayerNum = 1
		sCodingParam.ITemporalLayerNum = 1
		sCodingParam.ITargetBitrate = 500000
		sCodingParam.FMaxFrameRate = 30.0
		sCodingParam.SSpatialLayers[0].IVideoWidth = sCodingParam.IPicWidth
		sCodingParam.SSpatialLayers[0].IVideoHeight = sCodingParam.IPicHeight
		sCodingParam.SSpatialLayers[0].ISpatialBitrate = sCodingParam.ITargetBitrate
		sCodingParam.SSpatialLayers[0].FFrameRate = sCodingParam.FMaxFrameRate

		var sLogCtx common.SLogContext
		sLogCtx.PfLog = func(pCtx any, iLevel int32, kpFmt string, argv []any) {}

		iResult := ParamValidationExt(&sLogCtx, sCodingParam)
		e.expectEq(iResult, ENC_RETURN_UNSUPPORTED_PARA, "ParamValidationExt")
	})
}

func TestEncoderInterfaceTestHighBitrateIdrTargetBits(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamExt api.SEncParamExt
		e.pPtrEnc.GetDefaultParams(&sEncParamExt)
		sEncParamExt.IUsageType = api.CAMERA_VIDEO_REAL_TIME
		sEncParamExt.IPicWidth = 1280
		sEncParamExt.IPicHeight = 720
		sEncParamExt.ITargetBitrate = 200000000
		sEncParamExt.FMaxFrameRate = 30.0
		sEncParamExt.IRCMode = api.RC_BITRATE_MODE
		sEncParamExt.ISpatialLayerNum = 1
		sEncParamExt.SSpatialLayers[0].IVideoWidth = sEncParamExt.IPicWidth
		sEncParamExt.SSpatialLayers[0].IVideoHeight = sEncParamExt.IPicHeight
		sEncParamExt.SSpatialLayers[0].ISpatialBitrate = sEncParamExt.ITargetBitrate
		sEncParamExt.SSpatialLayers[0].FFrameRate = sEncParamExt.FMaxFrameRate

		iResult := e.pPtrEnc.InitializeExt(&sEncParamExt)
		e.expectEq(iResult, int32(api.CmResultSuccess), "InitializeExt")

		e.PrepareOneSrcFrame()

		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")

		// Force subsequent IDR frame so that iIdrNum != 0 and RcDecideTargetBits uses iIdrBitrateRatio
		bIDR := true
		e.pPtrEnc.ForceIntraFrame(bIDR, -1)
		e.pSrcPic.UiTimeStamp += 33
		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")

		e.pPtrEnc.Uninitialize()
	})
}

func TestEncoderInterfaceTestBasicInitializeTestAutoAdjustment(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamBase api.SEncParamBase

		for _, fr := range []float32{50000, 0, -1} {
			e.GetValidEncParamBase(&sEncParamBase)
			sEncParamBase.FMaxFrameRate = fr
			e.EncodeOneFrame(&sEncParamBase)
			if !(sEncParamBase.FMaxFrameRate <= 60.0 && sEncParamBase.FMaxFrameRate >= 1.0) {
				e.t.Errorf("fMaxFrameRate %f -> %f, want within [1, 60]", fr, sEncParamBase.FMaxFrameRate)
			}
		}
	})
}

func TestEncoderInterfaceTestForceIntraFrameSimulCastAVC(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamExt api.SEncParamExt
		e.pPtrEnc.GetDefaultParams(&sEncParamExt)
		sEncParamExt.IUsageType = api.CAMERA_VIDEO_REAL_TIME
		iSpatialLayerNum := e.rand31() % api.MAX_SPATIAL_LAYER_NUM
		sEncParamExt.ISpatialLayerNum = common.WELS_CLIP3(iSpatialLayerNum, 1, api.MAX_SPATIAL_LAYER_NUM)

		sEncParamExt.IPicWidth = 1280
		sEncParamExt.IPicHeight = 720
		sEncParamExt.ITargetBitrate = 0            //!=0
		sEncParamExt.IRCMode = api.RC_BITRATE_MODE //-1, 0, 1, 2
		sEncParamExt.BEnableFrameSkip = false
		sEncParamExt.FMaxFrameRate = 30 //!=0
		sEncParamExt.BSimulcastAVC = true

		for iNum := sEncParamExt.ISpatialLayerNum - 1; iNum >= 0; iNum-- {
			iScale := int32(1) << ((sEncParamExt.ISpatialLayerNum - 1) - iNum)
			sEncParamExt.SSpatialLayers[iNum].IVideoWidth = sEncParamExt.IPicWidth / iScale
			sEncParamExt.SSpatialLayers[iNum].IVideoHeight = sEncParamExt.IPicHeight / iScale
			sEncParamExt.SSpatialLayers[iNum].ISpatialBitrate = (sEncParamExt.SSpatialLayers[iNum].IVideoWidth *
				sEncParamExt.SSpatialLayers[iNum].IVideoHeight) / 3
			sEncParamExt.ITargetBitrate += sEncParamExt.SSpatialLayers[iNum].ISpatialBitrate
		}
		iResult := e.pPtrEnc.InitializeExt(&sEncParamExt)
		e.expectEq(iResult, int32(api.CmResultSuccess), "InitializeExt")

		kiFrameNumber := e.rand31() % 100
		if kiFrameNumber < 20 {
			kiFrameNumber = 20
		}
		for i := int32(0); i < kiFrameNumber; i++ {
			e.PrepareOneSrcFrame()
			bForceIdr := (i == (kiFrameNumber / 2))
			iLayerNum := e.rand31() % api.MAX_SPATIAL_LAYER_NUM
			iLayerNum = common.WELS_CLIP3(iSpatialLayerNum, 0, sEncParamExt.ISpatialLayerNum-1)
			if bForceIdr {
				e.pPtrEnc.ForceIntraFrame(true, iLayerNum)
			}

			iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
			e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
			if bForceIdr {
				for i := int32(0); i < e.sFbi.ILayerNum; i++ {
					if int32(e.sFbi.SLayerInfo[i].UiSpatialId) == iLayerNum {
						e.expectEq(int32(e.sFbi.SLayerInfo[i].EFrameType), int32(api.VideoFrameTypeIDR), "layer eFrameType")
					}
				}
			}
		}

		e.pPtrEnc.Uninitialize()
		e.expectEq(iResult, int32(api.CmResultSuccess), "Uninitialize")
	})
}

func TestEncoderInterfaceTestForceIntraFrame(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamBase api.SEncParamBase
		e.GetValidEncParamBase(&sEncParamBase)

		iResult := e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmResultSuccess), "Initialize")

		e.PrepareOneSrcFrame()

		bIDR := true
		e.pPtrEnc.ForceIntraFrame(bIDR, -1)
		e.EncodeOneIDRandP(e.pPtrEnc)

		//call next frame to be IDR
		e.pPtrEnc.ForceIntraFrame(bIDR, -1)
		iCount := 0
		for {
			iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
			e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
			e.pSrcPic.UiTimeStamp += 30
			cont := e.sFbi.EFrameType == api.VideoFrameTypeSkip && iCount < 100
			iCount++
			if !cont {
				break
			}
		}
		e.expectEq(int32(e.sFbi.EFrameType), int32(api.VideoFrameTypeIDR), "eFrameType")

		e.pPtrEnc.Uninitialize()
		e.expectEq(iResult, int32(api.CmResultSuccess), "Uninitialize")
	})
}

func TestEncoderInterfaceTestForceIntraFrameWithTemporal(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamExt api.SEncParamExt
		e.pPtrEnc.GetDefaultParams(&sEncParamExt)
		sEncParamExt.IUsageType = api.CAMERA_VIDEO_REAL_TIME
		sEncParamExt.IPicWidth = encUT_MB_SIZE + common.WELS_ABS((e.rand31()/2*2)%(encUT_MAX_WIDTH-encUT_MB_SIZE))
		sEncParamExt.IPicHeight = encUT_MB_SIZE + common.WELS_ABS((e.rand31()/2*2)%(encUT_MAX_HEIGHT-encUT_MB_SIZE))
		sEncParamExt.ITargetBitrate = e.rand31() + 1 //!=0
		// Force a bitrate of at least w*h/50, otherwise we will only get skipped frames
		sEncParamExt.ITargetBitrate = common.WELS_CLIP3(sEncParamExt.ITargetBitrate,
			sEncParamExt.IPicWidth*sEncParamExt.IPicHeight/50, 60000000)
		iLevelMaxBitrate := int32(common.G_ksLevelLimits[common.LEVEL_NUMBER-1].UiMaxBR * common.CpbBrNalFactor)
		if sEncParamExt.ITargetBitrate > iLevelMaxBitrate {
			sEncParamExt.ITargetBitrate = iLevelMaxBitrate
		}
		sEncParamExt.IRCMode = api.RC_BITRATE_MODE //-1, 0, 1, 2
		sEncParamExt.FMaxFrameRate = float32(e.rand31()) + 0.5
		sEncParamExt.SSpatialLayers[0].IVideoWidth = sEncParamExt.IPicWidth
		sEncParamExt.SSpatialLayers[0].IVideoHeight = sEncParamExt.IPicHeight
		sEncParamExt.SSpatialLayers[0].ISpatialBitrate = sEncParamExt.ITargetBitrate
		iTargetTemporalLayerNum := e.rand31() % api.MAX_TEMPORAL_LAYER_NUM
		if iTargetTemporalLayerNum > 2 {
			sEncParamExt.ITemporalLayerNum = iTargetTemporalLayerNum
		} else {
			sEncParamExt.ITemporalLayerNum = 2
		}

		iResult := e.pPtrEnc.InitializeExt(&sEncParamExt)
		e.expectEq(iResult, int32(api.CmResultSuccess), "InitializeExt")

		e.PrepareOneSrcFrame()

		bIDR := true
		e.EncodeOneIDRandP(e.pPtrEnc)

		//call next frame to be IDR
		e.pPtrEnc.ForceIntraFrame(bIDR, -1)
		iCount := 0
		for {
			iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
			e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
			e.pSrcPic.UiTimeStamp += 30
			cont := e.sFbi.EFrameType == api.VideoFrameTypeSkip && iCount < 100
			iCount++
			if !cont {
				break
			}
		}
		e.expectEq(int32(e.sFbi.EFrameType), int32(api.VideoFrameTypeIDR), "eFrameType")

		e.pPtrEnc.Uninitialize()
		e.expectEq(iResult, int32(api.CmResultSuccess), "Uninitialize")
	})
}

func TestEncoderInterfaceTestEncodeParameterSets(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamBase api.SEncParamBase
		e.GetValidEncParamBase(&sEncParamBase)

		iResult := e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmResultSuccess), "Initialize")
		e.PrepareOneSrcFrame()
		e.EncodeOneIDRandP(e.pPtrEnc)

		//try EncodeParameterSets
		e.pPtrEnc.EncodeParameterSets(&e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeParameterSets")
		e.expectEq(int32(e.sFbi.EFrameType), int32(api.VideoFrameTypeInvalid), "eFrameType")

		//check the result
		for i := int32(0); i < e.sFbi.ILayerNum; i++ {
			pLayerBsInfo := &e.sFbi.SLayerInfo[i]
			e.expectEq(int32(pLayerBsInfo.UiLayerType), int32(api.NON_VIDEO_CODING_LAYER), "uiLayerType")

			iNalType := encUT_GET_NAL_TYPE(pLayerBsInfo.PBsBuf)
			if !encUT_IS_PARASET(iNalType) {
				e.t.Errorf("NAL type %d is not a parameter set", iNalType)
			}
			for j := int32(0); j < (pLayerBsInfo.INalCount - 1); j++ {
				iNalType = encUT_GET_NAL_TYPE(pLayerBsInfo.PBsBuf[pLayerBsInfo.PNalLengthInByte[j]:])
				if !encUT_IS_PARASET(iNalType) {
					e.t.Errorf("NAL type %d is not a parameter set", iNalType)
				}
			}
		}

		//try another P to make sure no impact on succeeding frames
		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
		if e.sFbi.EFrameType == api.VideoFrameTypeIDR {
			e.t.Errorf("eFrameType is IDR")
		}

		iResult = e.pPtrEnc.Uninitialize()
		e.expectEq(iResult, int32(api.CmResultSuccess), "Uninitialize")
	})
}

func (e *encoderInterfaceTest) ChangeResolutionAndCheckStatistics(sEncParamBase *api.SEncParamBase,
	pEncoderStatistics *api.SEncoderStatistics) {
	e.t.Helper()
	uiExistingFrameCount := pEncoderStatistics.UiInputFrameCount
	uiExistingIDR := pEncoderStatistics.UiIDRSentNum
	uiExistingResolutionChange := pEncoderStatistics.UiResolutionChangeTimes

	// 1, get the existing param
	iResult := e.pPtrEnc.GetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, e.pParamExt)
	e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption PARAM_EXT")

	// 2, change setting
	uiKnownResolutionChangeTimes := uiExistingResolutionChange
	bCheckIDR := false
	if e.pParamExt.IPicWidth != sEncParamBase.IPicWidth || e.pParamExt.IPicHeight != sEncParamBase.IPicHeight {
		uiKnownResolutionChangeTimes += 1
		bCheckIDR = true
	}
	e.pParamExt.SSpatialLayers[0].IVideoWidth = sEncParamBase.IPicWidth
	e.pParamExt.IPicWidth = sEncParamBase.IPicWidth
	e.pParamExt.SSpatialLayers[0].IVideoHeight = sEncParamBase.IPicHeight
	e.pParamExt.IPicHeight = sEncParamBase.IPicHeight
	e.pPtrEnc.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, e.pParamExt)
	e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption PARAM_EXT")

	// 3, code one frame
	e.PrepareOneSrcFrame()
	e.pSrcPic.UiTimeStamp = int64(30 * pEncoderStatistics.UiInputFrameCount)
	iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
	e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
	iResult = e.pPtrEnc.GetOption(api.ENCODER_OPTION_GET_STATISTICS, pEncoderStatistics)
	e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption STATISTICS")

	e.expectEq(int32(pEncoderStatistics.UiInputFrameCount), int32(uiExistingFrameCount+1), "uiInputFrameCount")
	e.expectEq(int32(pEncoderStatistics.UiResolutionChangeTimes), int32(uiKnownResolutionChangeTimes), "uiResolutionChangeTimes")
	if bCheckIDR {
		e.expectEq(int32(pEncoderStatistics.UiIDRSentNum), int32(uiExistingIDR+1), "uiIDRSentNum")
	}

	e.expectEq(int32(pEncoderStatistics.UiWidth), sEncParamBase.IPicWidth, "uiWidth")
	e.expectEq(int32(pEncoderStatistics.UiHeight), sEncParamBase.IPicHeight, "uiHeight")
}

func TestEncoderInterfaceTestGetStatistics(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamBase api.SEncParamBase
		e.GetValidEncParamBase(&sEncParamBase)

		iResult := e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmResultSuccess), "Initialize")

		e.PrepareOneSrcFrame()
		e.EncodeOneIDRandP(e.pPtrEnc)

		var sEncoderStatistics api.SEncoderStatistics
		iResult = e.pPtrEnc.GetOption(api.ENCODER_OPTION_GET_STATISTICS, &sEncoderStatistics)
		e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption STATISTICS")
		e.expectEq(int32(sEncoderStatistics.UiInputFrameCount), 2, "uiInputFrameCount")
		e.expectEq(int32(sEncoderStatistics.UiIDRSentNum), 1, "uiIDRSentNum")
		e.expectEq(int32(sEncoderStatistics.UiResolutionChangeTimes), 0, "uiResolutionChangeTimes")

		e.expectEq(int32(sEncoderStatistics.UiWidth), sEncParamBase.IPicWidth, "uiWidth")
		e.expectEq(int32(sEncoderStatistics.UiHeight), sEncParamBase.IPicHeight, "uiHeight")

		// try param change
		e.GetValidEncParamBase(&sEncParamBase)
		e.ChangeResolutionAndCheckStatistics(&sEncParamBase, &sEncoderStatistics)

		e.GetValidEncParamBase(&sEncParamBase)
		sEncParamBase.IPicWidth = (sEncParamBase.IPicWidth % 16) + 1   //try 1~16
		sEncParamBase.IPicHeight = (sEncParamBase.IPicHeight % 16) + 1 //try 1~16
		e.ChangeResolutionAndCheckStatistics(&sEncParamBase, &sEncoderStatistics)

		// try timestamp and frame rate
		e.pSrcPic.UiTimeStamp = 1000
		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
		iResult = e.pPtrEnc.GetOption(api.ENCODER_OPTION_GET_STATISTICS, &sEncoderStatistics)
		e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption STATISTICS")
		e.expectEq(int32(uint32(sEncoderStatistics.FAverageFrameRate)), int32(sEncoderStatistics.UiInputFrameCount), "fAverageFrameRate")

		// 4, change log interval
		iInterval := int32(0)
		iResult = e.pPtrEnc.GetOption(api.ENCODER_OPTION_STATISTICS_LOG_INTERVAL, &iInterval)
		e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption LOG_INTERVAL")
		e.expectEq(iInterval, 5000, "iInterval")

		iInterval2 := int32(2000)
		iResult = e.pPtrEnc.SetOption(api.ENCODER_OPTION_STATISTICS_LOG_INTERVAL, &iInterval2)
		e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption LOG_INTERVAL")
		iResult = e.pPtrEnc.GetOption(api.ENCODER_OPTION_STATISTICS_LOG_INTERVAL, &iInterval)
		e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption LOG_INTERVAL")
		e.expectEq(iInterval, iInterval2, "iInterval")

		iInterval2 = 0
		iResult = e.pPtrEnc.SetOption(api.ENCODER_OPTION_STATISTICS_LOG_INTERVAL, &iInterval2)
		e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption LOG_INTERVAL")
		iResult = e.pPtrEnc.GetOption(api.ENCODER_OPTION_STATISTICS_LOG_INTERVAL, &iInterval)
		e.expectEq(iResult, int32(api.CmResultSuccess), "GetOption LOG_INTERVAL")
		e.expectEq(iInterval, iInterval2, "iInterval")

		// finish
		e.pPtrEnc.Uninitialize()
	})
}

func TestEncoderInterfaceTestFrameSizeCheck(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamBase api.SEncParamBase
		e.GetValidEncParamBase(&sEncParamBase)

		iResult := e.pPtrEnc.Initialize(&sEncParamBase)
		e.expectEq(iResult, int32(api.CmResultSuccess), "Initialize")

		e.PrepareOneSrcFrame()
		e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)

		length := frameBsLength(&e.sFbi)
		e.expectEq(length, e.sFbi.IFrameSizeInBytes, "iFrameSizeInBytes")

		// finish
		e.pPtrEnc.Uninitialize()
	})
}

func TestEncoderInterfaceTestSkipFrameCheck(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamExt api.SEncParamExt
		iResult := e.pPtrEnc.GetDefaultParams(&sEncParamExt)
		e.expectEq(iResult, int32(api.CmResultSuccess), "GetDefaultParams")

		sEncParamExt.IUsageType = api.CAMERA_VIDEO_REAL_TIME
		sEncParamExt.IPicWidth = 360
		sEncParamExt.IPicHeight = 640
		sEncParamExt.ITargetBitrate = 573000
		sEncParamExt.IRCMode = api.RC_BITRATE_MODE
		sEncParamExt.FMaxFrameRate = 28.248587

		sEncParamExt.ITemporalLayerNum = 3
		sEncParamExt.ISpatialLayerNum = 1
		sEncParamExt.BEnableLongTermReference = false
		sEncParamExt.BEnableSceneChangeDetect = false
		sEncParamExt.BEnableFrameSkip = true
		sEncParamExt.IMaxBitrate = 895839855
		sEncParamExt.UiMaxNalSize = 0

		sEncParamExt.SSpatialLayers[0].UiLevelIdc = api.LEVEL_5_0
		sEncParamExt.SSpatialLayers[0].IVideoWidth = 360
		sEncParamExt.SSpatialLayers[0].IVideoHeight = 640
		sEncParamExt.SSpatialLayers[0].FFrameRate = 28.248587
		sEncParamExt.SSpatialLayers[0].ISpatialBitrate = 573000
		sEncParamExt.SSpatialLayers[0].IMaxSpatialBitrate = 895839855
		sEncParamExt.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE

		e.pParamExt.IPicWidth = sEncParamExt.SSpatialLayers[0].IVideoWidth
		e.pParamExt.IPicHeight = sEncParamExt.SSpatialLayers[0].IVideoHeight

		iResult = e.pPtrEnc.InitializeExt(&sEncParamExt)
		e.expectEq(iResult, int32(api.CmResultSuccess), "InitializeExt")

		iInterval := int32(300)
		iResult = e.pPtrEnc.SetOption(api.ENCODER_OPTION_STATISTICS_LOG_INTERVAL, &iInterval)
		e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption LOG_INTERVAL")

		var sEncoderStatistics api.SEncoderStatistics
		for i := 0; i < 50; i++ {
			e.PrepareOneSrcFrame()
			e.pSrcPic.UiTimeStamp = int64(i * 33)
			iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)

			if i == 25 {
				sEncParamExt.FMaxFrameRate = 28
				sEncParamExt.SSpatialLayers[0].FFrameRate = 28
				sEncParamExt.SSpatialLayers[0].ISpatialBitrate = 500000
				iResult = e.pPtrEnc.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &sEncParamExt)
				e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption PARAM_EXT")
			}
		}
		e.pPtrEnc.GetOption(api.ENCODER_OPTION_GET_STATISTICS, &sEncoderStatistics)
		if !((sEncoderStatistics.UiInputFrameCount - sEncoderStatistics.UiSkippedFrameCount) > 2) {
			e.t.Errorf("uiInputFrameCount = %d, uiSkippedFrameCount = %d", sEncoderStatistics.UiInputFrameCount,
				sEncoderStatistics.UiSkippedFrameCount)
		}
		// finish
		e.pPtrEnc.Uninitialize()
	})
}

func TestEncoderInterfaceTestDiffResolutionCheck(t *testing.T) {
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		var sEncParamExt api.SEncParamExt
		iResult := e.pPtrEnc.GetDefaultParams(&sEncParamExt)
		e.expectEq(iResult, int32(api.CmResultSuccess), "GetDefaultParams")

		sEncParamExt.IUsageType = api.CAMERA_VIDEO_REAL_TIME
		sEncParamExt.IPicWidth = 360
		sEncParamExt.IPicHeight = 640
		sEncParamExt.ITargetBitrate = 1000000
		sEncParamExt.IRCMode = api.RC_BITRATE_MODE
		sEncParamExt.FMaxFrameRate = 24

		sEncParamExt.ITemporalLayerNum = 3
		sEncParamExt.ISpatialLayerNum = 3
		sEncParamExt.BEnableLongTermReference = false
		sEncParamExt.BEnableSceneChangeDetect = false
		sEncParamExt.BEnableFrameSkip = true
		sEncParamExt.UiMaxNalSize = 0

		setLayer := func(i int, w, h int32, fr float32, br int32) {
			sEncParamExt.SSpatialLayers[i].IVideoWidth = w
			sEncParamExt.SSpatialLayers[i].IVideoHeight = h
			sEncParamExt.SSpatialLayers[i].FFrameRate = fr
			sEncParamExt.SSpatialLayers[i].ISpatialBitrate = br
		}

		for i := 0; i < 3; i++ {
			sEncParamExt.SSpatialLayers[i].UiLevelIdc = api.LEVEL_UNKNOWN
			sEncParamExt.SSpatialLayers[i].SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
		}
		setLayer(0, 90, 160, 6, 80000)
		setLayer(1, 180, 320, 12, 200000)
		setLayer(2, 360, 640, 24, 600000)

		e.pParamExt.IPicWidth = sEncParamExt.IPicWidth
		e.pParamExt.IPicHeight = sEncParamExt.IPicWidth

		iResult = e.pPtrEnc.InitializeExt(&sEncParamExt)
		e.expectEq(iResult, int32(api.CmResultSuccess), "InitializeExt")

		e.PrepareOneSrcFrame()
		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
		e.pSrcPic.UiTimeStamp += 30

		//correct setting
		sEncParamExt.ISpatialLayerNum = 2
		sEncParamExt.SSpatialLayers[1].UiLevelIdc = api.LEVEL_UNKNOWN
		setLayer(1, 360, 640, 24, 600000)
		iResult = e.pPtrEnc.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &sEncParamExt)
		e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption PARAM_EXT (correct)")

		e.PrepareOneSrcFrame()
		iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
		e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
		e.pSrcPic.UiTimeStamp += 30

		//incorrect setting
		uiTraceLevel := int32(api.WELS_LOG_QUIET)
		e.pPtrEnc.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &uiTraceLevel)

		setLayer(1, 90, 160, 6, 80000)
		setLayer(0, 360, 640, 24, 600000)
		iResult = e.pPtrEnc.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &sEncParamExt)
		e.expectEq(iResult, int32(api.CmInitParaError), "SetOption PARAM_EXT (incorrect 1)")

		//incorrect setting
		sEncParamExt.ISpatialLayerNum = 3
		setLayer(1, 90, 160, 6, 80000)
		setLayer(0, 180, 320, 12, 200000)
		setLayer(2, 360, 640, 24, 600000)
		iResult = e.pPtrEnc.SetOption(api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &sEncParamExt)
		e.expectEq(iResult, int32(api.CmInitParaError), "SetOption PARAM_EXT (incorrect 2)")

		// finish
		e.pPtrEnc.Uninitialize()
	})
}

func TestEncoderInterfaceTestNalSizeChecking(t *testing.T) {
	if testing.Short() {
		t.Skip("long running")
	}
	runEncoderInterfaceTest(t, func(e *encoderInterfaceTest) {
		e.pParamExt.IPicWidth = 1280
		e.pParamExt.IPicHeight = 720
		e.pParamExt.IPicWidth += (e.rand31() << 1) % encUT_IMAGE_VARY_SIZE
		e.pParamExt.IPicHeight += (e.rand31() << 1) % encUT_IMAGE_VARY_SIZE
		e.pParamExt.FMaxFrameRate = 30
		e.pParamExt.ITemporalLayerNum = e.rand31() % 3
		e.pParamExt.ISpatialLayerNum = e.rand31() % 4
		e.pParamExt.INumRefFrame = api.AUTO_REF_PIC_COUNT
		e.pParamExt.ISpatialLayerNum = common.WELS_CLIP3(e.pParamExt.ISpatialLayerNum, 1, 4)

		e.pParamExt.IMultipleThreadIdc = 1 //multi-thread can't control size. will be fixed.
		e.pParamExt.IEntropyCodingModeFlag = e.rand31() % 2
		iMaxNalSize := e.rand31() % 5000
		iMaxNalSize = common.WELS_CLIP3(iMaxNalSize, 1000, 5000)
		e.pParamExt.UiMaxNalSize = uint32(iMaxNalSize)
		for i := int32(0); i < e.pParamExt.ISpatialLayerNum; i++ {
			e.pParamExt.SSpatialLayers[i].SSliceArgument.UiSliceMode = api.SM_SIZELIMITED_SLICE
			e.pParamExt.SSpatialLayers[i].SSliceArgument.UiSliceSizeConstraint = uint32(iMaxNalSize)
			e.pParamExt.SSpatialLayers[i].IVideoHeight = e.pParamExt.IPicHeight
			e.pParamExt.SSpatialLayers[i].IVideoWidth = e.pParamExt.IPicWidth
			bitrate := e.rand31() % 3000000
			e.pParamExt.SSpatialLayers[i].ISpatialBitrate = common.WELS_CLIP3(bitrate, 500000, 3000000)
			e.pParamExt.ITargetBitrate += e.pParamExt.SSpatialLayers[i].ISpatialBitrate
			e.pParamExt.SSpatialLayers[i].FFrameRate = 30
		}
		iResult := e.pPtrEnc.InitializeExt(e.pParamExt)
		kiFrameNumber := encUT_TEST_FRAMES

		e.m_iWidth = e.pParamExt.IPicWidth
		e.m_iHeight = e.pParamExt.IPicHeight
		e.m_iPicResSize = e.m_iWidth * e.m_iHeight * 3 >> 1
		e.pYUV = make([]uint8, e.m_iPicResSize)
		f, err := os.Open(filepath.Join(resDir, "res/Cisco_Absolute_Power_1280x720_30fps.yuv"))
		if err != nil {
			e.t.Skipf("OpenH264 conformance data is unavailable: %v", err)
		}
		defer f.Close()
		e.PrepareOneSrcFrame()

		checkNalSizes := func() {
			for i := int32(0); i < e.sFbi.ILayerNum; i++ {
				for j := int32(0); j < e.sFbi.SLayerInfo[i].INalCount; j++ {
					length := e.sFbi.SLayerInfo[i].PNalLengthInByte[j]
					if length > iMaxNalSize {
						e.t.Errorf("NAL length %d > %d", length, iMaxNalSize)
					}
				}
			}
		}

		for i := 0; i < kiFrameNumber; i++ {
			n, _ := io.ReadFull(f, e.pYUV[:e.m_iPicResSize])
			if int32(n) != e.m_iPicResSize {
				break
			}
			iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
			e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
			e.pSrcPic.UiTimeStamp += 30

			checkNalSizes()
		}
		e.pParamExt.IPicWidth = 1280
		e.pParamExt.IPicHeight = 720
		e.pParamExt.IPicWidth += (e.rand31() << 1) % encUT_IMAGE_VARY_SIZE
		e.pParamExt.IPicHeight += (e.rand31() << 1) % encUT_IMAGE_VARY_SIZE
		e.m_iWidth = e.pParamExt.IPicWidth
		e.m_iHeight = e.pParamExt.IPicHeight
		e.m_iPicResSize = e.m_iWidth * e.m_iHeight * 3 >> 1
		e.pYUV = make([]uint8, e.m_iPicResSize)
		iResult = e.pPtrEnc.InitializeExt(e.pParamExt)
		e.PrepareOneSrcFrame()

		eOptionId := api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT
		*e.pOption = *e.pParamExt
		e.pOption.IPicWidth = e.m_iWidth
		e.pOption.IPicHeight = e.m_iHeight
		iResult = e.pPtrEnc.SetOption(eOptionId, e.pOption)
		e.expectEq(iResult, int32(api.CmResultSuccess), "SetOption PARAM_EXT")

		for i := 0; i < kiFrameNumber; i++ {
			e.PrepareOneSrcFrame()
			iResult = e.pPtrEnc.EncodeFrame(e.pSrcPic, &e.sFbi)
			e.expectEq(iResult, int32(api.CmResultSuccess), "EncodeFrame")
			e.pSrcPic.UiTimeStamp += 30

			checkNalSizes()
		}
		iResult = e.pPtrEnc.Uninitialize()
		e.expectEq(iResult, int32(api.CmResultSuccess), "Uninitialize")
	})
}

// TestWelsEncoderExtOptionsUninitialized checks the option handling of an
// encoder that has not been initialized (no C counterpart).
func TestWelsEncoderExtOptionsUninitialized(t *testing.T) {
	var enc api.ISVCEncoder
	if WelsCreateSVCEncoder(&enc) != 0 || enc == nil {
		t.Fatalf("WelsCreateSVCEncoder failed")
	}
	defer WelsDestroySVCEncoder(enc)

	if rv := enc.SetOption(api.ENCODER_OPTION_IDR_INTERVAL, nil); rv != int32(api.CmInitParaError) {
		t.Errorf("SetOption(nil) = %d", rv)
	}
	var nilPtr *int32
	if rv := enc.SetOption(api.ENCODER_OPTION_IDR_INTERVAL, nilPtr); rv != int32(api.CmInitParaError) {
		t.Errorf("SetOption(typed nil) = %d", rv)
	}
	v := int32(10)
	if rv := enc.SetOption(api.ENCODER_OPTION_IDR_INTERVAL, &v); rv != int32(api.CmInitExpected) {
		t.Errorf("SetOption before init = %d", rv)
	}
	if rv := enc.GetOption(api.ENCODER_OPTION_IDR_INTERVAL, &v); rv != int32(api.CmInitExpected) {
		t.Errorf("GetOption before init = %d", rv)
	}
	lvl := uint32(api.WELS_LOG_QUIET)
	if rv := enc.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &lvl); rv != 0 {
		t.Errorf("SetOption TRACE_LEVEL = %d", rv)
	}
	if rv := enc.ForceIntraFrame(true, -1); rv != 1 {
		t.Errorf("ForceIntraFrame before init = %d", rv)
	}
	var defaults api.SEncParamExt
	if rv := enc.GetDefaultParams(&defaults); rv != 0 {
		t.Errorf("GetDefaultParams = %d", rv)
	}
	if defaults.INumRefFrame != api.AUTO_REF_PIC_COUNT || defaults.FMaxFrameRate != MAX_FRAME_RATE {
		t.Errorf("unexpected defaults: %+v", defaults)
	}
	if WelsGetCodecVersion() != api.G_stCodecVersion {
		t.Errorf("WelsGetCodecVersion mismatch")
	}
	var ver api.OpenH264Version
	WelsGetCodecVersionEx(&ver)
	if ver != api.G_stCodecVersion {
		t.Errorf("WelsGetCodecVersionEx mismatch")
	}
}

// TestEncoderNonDyadicSpatialLayers encodes two spatial layers whose sizes
// are not in a 1:2 ratio. GetRefMb then indexes past the end of the
// reference layer's MB list (into the next layer's list, as C does, since
// all lists share one allocation); this used to panic with an index out of
// range.
func TestEncoderNonDyadicSpatialLayers(t *testing.T) {
	var enc api.ISVCEncoder
	if WelsCreateSVCEncoder(&enc) != 0 || enc == nil {
		t.Fatalf("WelsCreateSVCEncoder failed")
	}
	defer WelsDestroySVCEncoder(enc)
	lvl := uint32(api.WELS_LOG_QUIET)
	enc.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &lvl)

	const w, h = 160, 96
	var p api.SEncParamExt
	enc.GetDefaultParams(&p)
	p.IPicWidth, p.IPicHeight = w, h
	p.ITargetBitrate = 1000000
	p.IRCMode = api.RC_OFF_MODE
	p.FMaxFrameRate = 30
	p.ISpatialLayerNum = 2
	p.ITemporalLayerNum = 1
	p.SSpatialLayers[0] = api.SSpatialLayerConfig{IVideoWidth: 48, IVideoHeight: 32, FFrameRate: 30, ISpatialBitrate: 200000, IDLayerQp: 26}
	p.SSpatialLayers[1] = api.SSpatialLayerConfig{IVideoWidth: w, IVideoHeight: h, FFrameRate: 30, ISpatialBitrate: 800000, IDLayerQp: 26}
	if rv := enc.InitializeExt(&p); rv != 0 {
		t.Fatalf("InitializeExt = %d", rv)
	}

	yuv := make([]uint8, w*h*3/2)
	var src api.SSourcePicture
	src.IColorFormat = int32(api.VideoFormatI420)
	src.IPicWidth, src.IPicHeight = w, h
	src.IStride = [4]int32{w, w / 2, w / 2, 0}
	src.PData[0] = yuv
	src.PData[1] = yuv[w*h:]
	src.PData[2] = yuv[w*h+w*h/4:]
	var info api.SFrameBSInfo
	total := 0
	for f := 0; f < 4; f++ {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				yuv[y*w+x] = uint8((x*3 + y*5 + f*7) ^ (x * y >> 3))
			}
		}
		src.UiTimeStamp = int64(f * 33)
		if rv := enc.EncodeFrame(&src, &info); rv != 0 {
			t.Fatalf("EncodeFrame(%d) = %d", f, rv)
		}
		total += int(info.IFrameSizeInBytes)
	}
	if total == 0 {
		t.Fatalf("no bitstream produced")
	}
}
