// Port of test/decoder/DecUT_DecExt.cpp (DecoderInterfaceTest).

package decoder

import (
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const decUtBufSize = 100

// payload size exclude 6 bytes: 0001, nal type and final '\0'
const decUtPayloadSize = decUtBufSize - 6

// decUtResDir is the repository res/ directory relative to this package.
const decUtResDir = "../../../../res/"

type decoderInterfaceTest struct {
	t               *testing.T
	rnd             *rand.Rand
	m_pDec          api.ISVCDecoder
	m_sDecParam     api.SDecodingParam
	m_sBufferInfo   api.SBufferInfo
	m_sParserBsInfo api.SParserBsInfo
	m_pData         [3][]uint8
	m_szBuffer      [decUtBufSize]uint8 //for mocking packet
	m_iBufLength    int                 //record the valid data in m_szBuffer
}

func (dt *decoderInterfaceTest) rand() int32 {
	return dt.rnd.Int31()
}

func (dt *decoderInterfaceTest) SetUp() {
	rv := WelsCreateDecoder(&dt.m_pDec)
	if rv != 0 {
		dt.t.Fatalf("WelsCreateDecoder returned %d", rv)
	}
	if dt.m_pDec == nil {
		dt.t.Fatalf("WelsCreateDecoder returned a nil decoder")
	}
}

func (dt *decoderInterfaceTest) TearDown() {
	if dt.m_pDec != nil {
		WelsDestroyDecoder(dt.m_pDec)
	}
}

// Init members
func (dt *decoderInterfaceTest) Init() int32 {
	dt.m_sBufferInfo = api.SBufferInfo{}
	dt.m_sParserBsInfo = api.SParserBsInfo{}
	dt.m_sDecParam = api.SDecodingParam{}
	dt.m_sDecParam.PFileNameRestructed = ""
	dt.m_sDecParam.UiCpuLoad = uint32(dt.rand() % 100)
	dt.m_sDecParam.UiTargetDqLayer = uint8(dt.rand() % 100)
	dt.m_sDecParam.EEcActiveIdc = api.ERROR_CON_IDC(dt.rand() & 7)
	dt.m_sDecParam.SVideoProperty.Size = 8 // sizeof (SVideoProperty)
	dt.m_sDecParam.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_TYPE(dt.rand() % 2)

	dt.m_pData = [3][]uint8{}
	dt.m_szBuffer[0], dt.m_szBuffer[1], dt.m_szBuffer[2] = 0, 0, 0
	dt.m_szBuffer[3] = 1
	dt.m_iBufLength = 4
	eRet := dt.m_pDec.Initialize(&dt.m_sDecParam)
	if eRet != int32(api.CmResultSuccess) {
		dt.t.Errorf("Initialize: got %d, want %d", eRet, api.CmResultSuccess)
	}
	return eRet
}

func (dt *decoderInterfaceTest) ValidInit() int32 {
	dt.m_sBufferInfo = api.SBufferInfo{}
	dt.m_sDecParam = api.SDecodingParam{}
	dt.m_sDecParam.PFileNameRestructed = ""
	dt.m_sDecParam.UiCpuLoad = 1
	dt.m_sDecParam.UiTargetDqLayer = 1
	dt.m_sDecParam.EEcActiveIdc = api.ERROR_CON_IDC(dt.rand() & 7)
	dt.m_sDecParam.SVideoProperty.Size = 8 // sizeof (SVideoProperty)
	dt.m_sDecParam.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_DEFAULT

	dt.m_pData = [3][]uint8{}
	dt.m_szBuffer[0], dt.m_szBuffer[1], dt.m_szBuffer[2] = 0, 0, 0
	dt.m_szBuffer[3] = 1
	dt.m_iBufLength = 4
	eRet := dt.m_pDec.Initialize(&dt.m_sDecParam)
	if eRet != int32(api.CmResultSuccess) {
		dt.t.Errorf("Initialize: got %d, want %d", eRet, api.CmResultSuccess)
	}
	return eRet
}

// Uninit members
func (dt *decoderInterfaceTest) Uninit() {
	if dt.m_pDec != nil {
		eRet := dt.m_pDec.Uninitialize()
		if eRet != int32(api.CmResultSuccess) {
			dt.t.Fatalf("Uninitialize: got %d", eRet)
		}
	}
	dt.m_sDecParam = api.SDecodingParam{}
	dt.m_sParserBsInfo = api.SParserBsInfo{}
	dt.m_sBufferInfo = api.SBufferInfo{}
	dt.m_pData = [3][]uint8{}
	dt.m_iBufLength = 0
}

// Decoder real bitstream
func (dt *decoderInterfaceTest) DecoderBs(sFileName string) {
	var iBufPos int32
	var i int32
	var iSliceSize int32
	var iEndOfStreamFlag int32
	uiStartCode := [4]uint8{0, 0, 0, 1}

	data, err := os.ReadFile(decUtResDir + sFileName)
	if err != nil {
		dt.t.Skipf("OpenH264 conformance data is unavailable: %v", err)
	}
	iFileSize := int32(len(data))
	// iFileSize + 4 bytes in C; a little more slack here so that the start
	// code search never indexes past the end.
	pBuf := make([]uint8, int(iFileSize)+8)
	copy(pBuf, data)
	copy(pBuf[iFileSize:], uiStartCode[:]) //confirmed_safe_unsafe_usage
	for {
		if iBufPos >= iFileSize {
			iEndOfStreamFlag = 1
			if iEndOfStreamFlag != 0 {
				dt.m_pDec.SetOption(api.DECODER_OPTION_END_OF_STREAM, &iEndOfStreamFlag)
			}
			break
		}
		for i = 0; i < iFileSize; i++ {
			if pBuf[iBufPos+i] == 0 && pBuf[iBufPos+i+1] == 0 && pBuf[iBufPos+i+2] == 0 && pBuf[iBufPos+i+3] == 1 &&
				i > 0 {
				break
			}
		}
		iSliceSize = i
		dt.m_pDec.DecodeFrame2(pBuf[iBufPos:], iSliceSize, &dt.m_pData, &dt.m_sBufferInfo)
		dt.m_pDec.DecodeFrame2(nil, 0, &dt.m_pData, &dt.m_sBufferInfo)
		iBufPos += iSliceSize
	}
}

// Mock input data for test
func (dt *decoderInterfaceTest) MockPacketType(eNalUnitType common.EWelsNalUnitType, iPacketLength int) {
	switch eNalUnitType {
	case common.NAL_UNIT_SEI:
		dt.m_szBuffer[dt.m_iBufLength] = 6
	case common.NAL_UNIT_SPS:
		dt.m_szBuffer[dt.m_iBufLength] = 67
	case common.NAL_UNIT_PPS:
		dt.m_szBuffer[dt.m_iBufLength] = 68
	case common.NAL_UNIT_SUBSET_SPS:
		dt.m_szBuffer[dt.m_iBufLength] = 15
	case common.NAL_UNIT_PREFIX:
		dt.m_szBuffer[dt.m_iBufLength] = 14
	case common.NAL_UNIT_CODED_SLICE:
		dt.m_szBuffer[dt.m_iBufLength] = 61
	case common.NAL_UNIT_CODED_SLICE_IDR:
		dt.m_szBuffer[dt.m_iBufLength] = 65
	default:
		dt.m_szBuffer[dt.m_iBufLength] = 0 //NAL_UNIT_UNSPEC_0
	}
	dt.m_iBufLength++
	iAddLength := iPacketLength - 5 //excluding 0001 and type
	if iAddLength > decUtPayloadSize {
		iAddLength = decUtPayloadSize
	}
	for i := 0; i < iAddLength; i++ {
		dt.m_szBuffer[dt.m_iBufLength] = uint8(dt.rand() % 256)
		dt.m_iBufLength++
	}
	dt.m_szBuffer[dt.m_iBufLength] = 0
	dt.m_iBufLength++
}

func (dt *decoderInterfaceTest) expectEq(name string, got, want any) {
	dt.t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		dt.t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

// Test Initialize/Uninitialize
func (dt *decoderInterfaceTest) TestInitUninit() {
	var iOutput int32
	var eRet int32
	var iRet int32 = ERR_NONE
	//No initialize, no GetOption can be done
	dt.m_pDec.Uninitialize()
	for i := 0; i <= int(api.DECODER_OPTION_TRACE_CALLBACK_CONTEXT); i++ {
		eRet = dt.m_pDec.GetOption(api.DECODER_OPTION(i), &iOutput)
		dt.expectEq(fmt.Sprintf("GetOption(%d) before init", i), eRet, int32(api.CmInitExpected))
	}
	//Initialize first, can get input color format
	iRet = dt.Init()
	if iRet != ERR_NONE {
		dt.t.Fatalf("Init: %d", iRet)
	}

	dt.m_sDecParam.BParseOnly = false
	eRet = dt.m_pDec.Initialize(&dt.m_sDecParam)
	if eRet != int32(api.CmResultSuccess) {
		dt.t.Fatalf("Initialize: %d", eRet)
	}

	eRet = dt.m_pDec.GetOption(api.DECODER_OPTION_END_OF_STREAM, &iOutput)
	dt.expectEq("GetOption(END_OF_STREAM)", eRet, int32(api.CmResultSuccess))
	dt.expectEq("END_OF_STREAM value", iOutput, int32(0))

	//Uninitialize, no GetOption can be done
	dt.m_pDec.Uninitialize()
	iOutput = 21
	for i := 0; i <= int(api.DECODER_OPTION_TRACE_CALLBACK_CONTEXT); i++ {
		eRet = dt.m_pDec.GetOption(api.DECODER_OPTION(i), &iOutput)
		dt.expectEq("iOutput untouched", iOutput, int32(21))
		dt.expectEq(fmt.Sprintf("GetOption(%d) after uninit", i), eRet, int32(api.CmInitExpected))
	}
}

// Test parse only API (ported, but - as in the C test - not part of
// DecoderInterfaceAll).
func (dt *decoderInterfaceTest) TestParseOnlyAPI() {
	var iOutput int32
	var iRet int32

	dt.m_pData = [3][]uint8{}
	dt.m_szBuffer[0], dt.m_szBuffer[1], dt.m_szBuffer[2] = 0, 0, 0
	dt.m_szBuffer[3] = 1
	dt.m_iBufLength = 4
	dt.MockPacketType(common.NAL_UNIT_SPS, 12)

	dt.m_pDec.Uninitialize()

	//test 1: bParseOnly = true; eEcActiveIdc = 0,1
	for iNum := int32(0); iNum < 2; iNum++ { //loop for EC
		dt.m_sBufferInfo = api.SBufferInfo{}
		dt.m_sParserBsInfo = api.SParserBsInfo{}
		dt.m_sDecParam = api.SDecodingParam{}
		dt.m_sDecParam.UiCpuLoad = uint32(dt.rand() % 100)
		dt.m_sDecParam.UiTargetDqLayer = 0xff // -1
		dt.m_sDecParam.BParseOnly = true
		dt.m_sDecParam.EEcActiveIdc = api.ERROR_CON_IDC(iNum)
		dt.m_sDecParam.SVideoProperty.Size = 8
		dt.m_sDecParam.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_TYPE(dt.rand() % 2)

		iRet = dt.m_pDec.Initialize(&dt.m_sDecParam)
		if iRet != int32(api.CmResultSuccess) {
			dt.t.Fatalf("Initialize: %d", iRet)
		}
		iRet = dt.m_pDec.GetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iOutput)
		dt.expectEq("GetOption", iRet, int32(api.CmResultSuccess))
		dt.expectEq("EC idc", iOutput, int32(api.ERROR_CON_DISABLE)) //should be 0
		//call DecodeParser(), correct call
		iRet = int32(dt.m_pDec.DecodeParser(dt.m_szBuffer[:], int32(dt.m_iBufLength), &dt.m_sParserBsInfo))
		dt.expectEq("DecodeParser", iRet, int32(api.DsNoParamSets))
		iRet = dt.m_pDec.GetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iOutput)
		dt.expectEq("GetOption", iRet, int32(api.CmResultSuccess))
		dt.expectEq("EC idc", iOutput, int32(api.ERROR_CON_DISABLE)) //should be 0
		//call DecodeFrame2(), incorrect call
		iRet = int32(dt.m_pDec.DecodeFrame2(dt.m_szBuffer[:], int32(dt.m_iBufLength), &dt.m_pData, &dt.m_sBufferInfo))
		dt.expectEq("DecodeFrame2", iRet, int32(api.DsInvalidArgument))
		iRet = dt.m_pDec.GetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iOutput)
		dt.expectEq("GetOption", iRet, int32(api.CmResultSuccess))
		dt.expectEq("EC idc", iOutput, int32(api.ERROR_CON_DISABLE)) //should be 0
		dt.m_pDec.Uninitialize()
	}

	//test 2: bParseOnly = false; eEcActiveIdc = 0,1
	for iNum := int32(0); iNum < 2; iNum++ { //loop for EC
		dt.m_sBufferInfo = api.SBufferInfo{}
		dt.m_sParserBsInfo = api.SParserBsInfo{}
		dt.m_sDecParam = api.SDecodingParam{}
		dt.m_sDecParam.UiCpuLoad = uint32(dt.rand() % 100)
		dt.m_sDecParam.UiTargetDqLayer = 0xff // -1
		dt.m_sDecParam.BParseOnly = false
		dt.m_sDecParam.EEcActiveIdc = api.ERROR_CON_IDC(iNum)
		dt.m_sDecParam.SVideoProperty.Size = 8
		dt.m_sDecParam.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_TYPE(dt.rand() % 2)

		iRet = dt.m_pDec.Initialize(&dt.m_sDecParam)
		if iRet != int32(api.CmResultSuccess) {
			dt.t.Fatalf("Initialize: %d", iRet)
		}
		iRet = dt.m_pDec.GetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iOutput)
		dt.expectEq("GetOption", iRet, int32(api.CmResultSuccess))
		dt.expectEq("EC idc", iOutput, iNum)
		//call DecodeParser(), incorrect call
		iRet = int32(dt.m_pDec.DecodeParser(dt.m_szBuffer[:], int32(dt.m_iBufLength), &dt.m_sParserBsInfo))
		dt.expectEq("DecodeParser", iRet, int32(api.DsInvalidArgument)) //error call
		iRet = dt.m_pDec.GetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iOutput)
		dt.expectEq("GetOption", iRet, int32(api.CmResultSuccess))
		dt.expectEq("EC idc", iOutput, iNum)
		//call DecodeFrame2(), correct call
		iRet = int32(dt.m_pDec.DecodeFrame2(dt.m_szBuffer[:], int32(dt.m_iBufLength), &dt.m_pData, &dt.m_sBufferInfo))
		dt.expectEq("DecodeFrame2", iRet, int32(api.DsNoParamSets))
		iRet = dt.m_pDec.GetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iOutput)
		dt.expectEq("GetOption", iRet, int32(api.CmResultSuccess))
		dt.expectEq("EC idc", iOutput, iNum)
		dt.m_pDec.Uninitialize()
	}
}

// DECODER_OPTION_END_OF_STREAM
func (dt *decoderInterfaceTest) TestEndOfStream() {
	var iTmp, iOut int32
	var eRet int32
	var iRet int32 = ERR_NONE

	iRet = dt.ValidInit()
	if iRet != ERR_NONE {
		dt.t.Fatalf("ValidInit: %d", iRet)
	}

	//invalid input
	eRet = dt.m_pDec.SetOption(api.DECODER_OPTION_END_OF_STREAM, nil)
	dt.expectEq("SetOption(EOS, NULL)", eRet, int32(api.CmInitParaError))

	//valid random input
	for i := 0; i < 10; i++ {
		iTmp = dt.rand()
		eRet = dt.m_pDec.SetOption(api.DECODER_OPTION_END_OF_STREAM, &iTmp)
		dt.expectEq("SetOption(EOS)", eRet, int32(api.CmResultSuccess))
		eRet = dt.m_pDec.GetOption(api.DECODER_OPTION_END_OF_STREAM, &iOut)
		dt.expectEq("GetOption(EOS)", eRet, int32(api.CmResultSuccess))
		want := int32(0)
		if iTmp != 0 {
			want = 1
		}
		dt.expectEq("EOS value", iOut, want)
	}

	//set false as input
	iTmp = 0
	eRet = dt.m_pDec.SetOption(api.DECODER_OPTION_END_OF_STREAM, &iTmp)
	dt.expectEq("SetOption(EOS)", eRet, int32(api.CmResultSuccess))
	eRet = dt.m_pDec.GetOption(api.DECODER_OPTION_END_OF_STREAM, &iOut)
	dt.expectEq("GetOption(EOS)", eRet, int32(api.CmResultSuccess))

	dt.expectEq("EOS value", iOut, int32(0))

	//set true as input
	iTmp = 1
	eRet = dt.m_pDec.SetOption(api.DECODER_OPTION_END_OF_STREAM, &iTmp)
	dt.expectEq("SetOption(EOS)", eRet, int32(api.CmResultSuccess))
	eRet = dt.m_pDec.GetOption(api.DECODER_OPTION_END_OF_STREAM, &iOut)
	dt.expectEq("GetOption(EOS)", eRet, int32(api.CmResultSuccess))

	dt.expectEq("EOS value", iOut, int32(1))

	//Mock data packet in
	//Test NULL data input for decoder, should be true for EOS
	eRet = int32(dt.m_pDec.DecodeFrame2(nil, 0, &dt.m_pData, &dt.m_sBufferInfo))
	dt.expectEq("DecodeFrame2(NULL)", eRet, int32(0)) //decode should return OK
	dt.m_pDec.GetOption(api.DECODER_OPTION_END_OF_STREAM, &iOut)
	dt.expectEq("EOS value", iOut, int32(1)) //decoder should have EOS == true

	//Test valid data input for decoder, should be false for EOS
	dt.MockPacketType(common.NAL_UNIT_UNSPEC_0, 50)
	dt.m_pDec.DecodeFrame2(dt.m_szBuffer[:], int32(dt.m_iBufLength), &dt.m_pData, &dt.m_sBufferInfo)
	dt.m_pDec.GetOption(api.DECODER_OPTION_END_OF_STREAM, &iOut)
	dt.expectEq("EOS value", iOut, int32(0)) //decoder should have EOS == false
	//Test NULL data input for decoder, should be true for EOS
	dt.m_pDec.DecodeFrame2(nil, 0, &dt.m_pData, &dt.m_sBufferInfo)
	dt.m_pDec.GetOption(api.DECODER_OPTION_END_OF_STREAM, &iOut)
	dt.expectEq("EOS value", iOut, int32(1)) //decoder should have EOS == true

	dt.Uninit()
}

// DECODER_OPTION_VCL_NAL
// Here Test illegal bitstream input
// legal bitstream decoding test, please see api test
func (dt *decoderInterfaceTest) TestVclNal() {
	var iTmp, iOut int32
	var eRet int32
	var iRet int32 = ERR_NONE

	iRet = dt.ValidInit()
	if iRet != ERR_NONE {
		dt.t.Fatalf("ValidInit: %d", iRet)
	}

	//Test SetOption
	//VclNal never supports SetOption
	iTmp = dt.rand()
	eRet = dt.m_pDec.SetOption(api.DECODER_OPTION_VCL_NAL, &iTmp)
	dt.expectEq("SetOption(VCL_NAL)", eRet, int32(api.CmInitParaError))

	//Test GetOption
	//invalid input
	eRet = dt.m_pDec.GetOption(api.DECODER_OPTION_VCL_NAL, nil)
	dt.expectEq("GetOption(VCL_NAL, NULL)", eRet, int32(api.CmInitParaError))

	//valid input without actual decoding
	eRet = dt.m_pDec.GetOption(api.DECODER_OPTION_VCL_NAL, &iOut)
	dt.expectEq("GetOption(VCL_NAL)", eRet, int32(api.CmResultSuccess))
	dt.expectEq("VCL_NAL value", iOut, int32(api.FEEDBACK_NON_VCL_NAL))

	//valid input with decoding error
	dt.MockPacketType(common.NAL_UNIT_CODED_SLICE_IDR, 50)
	dt.m_pDec.DecodeFrame2(dt.m_szBuffer[:], int32(dt.m_iBufLength), &dt.m_pData, &dt.m_sBufferInfo)
	eRet = dt.m_pDec.GetOption(api.DECODER_OPTION_VCL_NAL, &iOut)
	dt.expectEq("GetOption(VCL_NAL)", eRet, int32(api.CmResultSuccess))
	dt.expectEq("VCL_NAL value", iOut, int32(api.FEEDBACK_UNKNOWN_NAL))
	dt.m_pDec.DecodeFrame2(nil, 0, &dt.m_pData, &dt.m_sBufferInfo)
	eRet = dt.m_pDec.GetOption(api.DECODER_OPTION_VCL_NAL, &iOut)
	dt.expectEq("GetOption(VCL_NAL)", eRet, int32(api.CmResultSuccess))
	dt.expectEq("VCL_NAL value", iOut, int32(api.FEEDBACK_UNKNOWN_NAL))

	dt.Uninit()
}

// DECODER_OPTION_TEMPORAL_ID, _FRAME_NUM, _IDR_PIC_ID, _LTR_MARKING_FLAG,
// _LTR_MARKED_FRAME_NUM, _TRACE_LEVEL, _TRACE_CALLBACK,
// _TRACE_CALLBACK_CONTEXT: TODO in the C test as well.

// DECODER_OPTION_ERROR_CON_IDC
func (dt *decoderInterfaceTest) TestErrorConIdc() {
	var iTmp, iOut int32
	var eRet int32
	var iRet int32 = ERR_NONE

	iRet = dt.Init()
	if iRet != ERR_NONE {
		dt.t.Fatalf("Init: %d", iRet)
	}

	//Test GetOption
	//invalid input
	eRet = dt.m_pDec.GetOption(api.DECODER_OPTION_ERROR_CON_IDC, nil)
	dt.expectEq("GetOption(EC, NULL)", eRet, int32(api.CmInitParaError))

	//Test GetOption
	//valid input
	eRet = dt.m_pDec.GetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iOut)
	dt.expectEq("GetOption(EC)", eRet, int32(api.CmResultSuccess))

	//Test SetOption
	iTmp = dt.rand() & 7
	eRet = dt.m_pDec.SetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iTmp)
	dt.expectEq("SetOption(EC)", eRet, int32(api.CmResultSuccess))

	//Test GetOption
	eRet = dt.m_pDec.GetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iOut)
	dt.expectEq("GetOption(EC)", eRet, int32(api.CmResultSuccess))
	dt.expectEq("EC value", iOut, iTmp)

	dt.Uninit()
}

// DECODER_OPTION_GET_STATISTICS
func (dt *decoderInterfaceTest) TestGetDecStatistics() {
	var eRet int32
	var iRet int32
	var sDecStatic api.SDecoderStatistics
	var iError int32

	iRet = dt.ValidInit()
	if iRet != ERR_NONE {
		dt.t.Fatalf("ValidInit: %d", iRet)
	}
	//GetOption before decoding
	dt.m_pDec.GetOption(api.DECODER_OPTION_GET_STATISTICS, &sDecStatic)
	dt.expectEq("uiDecodedFrameCount", sDecStatic.UiDecodedFrameCount, uint32(0))
	dt.expectEq("iAvgLumaQp", sDecStatic.IAvgLumaQp, int32(-1))
	// setoption not support,
	eRet = dt.m_pDec.SetOption(api.DECODER_OPTION_GET_STATISTICS, nil)
	dt.expectEq("SetOption(GET_STATISTICS)", eRet, int32(api.CmInitParaError))
	//EC on UT
	iError = 2
	dt.m_pDec.SetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iError)
	//Decoder error bs
	dt.DecoderBs("Error_I_P.264")
	dt.m_pDec.GetOption(api.DECODER_OPTION_GET_STATISTICS, &sDecStatic)
	dt.expectEq("Error_I_P uiAvgEcRatio", sDecStatic.UiAvgEcRatio, 65)
	dt.expectEq("Error_I_P uiAvgEcPropRatio", sDecStatic.UiAvgEcPropRatio, 7)
	dt.expectEq("Error_I_P uiDecodedFrameCount", sDecStatic.UiDecodedFrameCount, 5)
	dt.expectEq("Error_I_P uiHeight", sDecStatic.UiHeight, 288)
	dt.expectEq("Error_I_P uiIDRCorrectNum", sDecStatic.UiIDRCorrectNum, 1)
	dt.expectEq("Error_I_P uiResolutionChangeTimes", sDecStatic.UiResolutionChangeTimes, 3)
	dt.expectEq("Error_I_P uiWidth", sDecStatic.UiWidth, 352)
	dt.expectEq("Error_I_P uiEcFrameNum", sDecStatic.UiEcFrameNum, 4)
	dt.expectEq("Error_I_P uiEcIDRNum", sDecStatic.UiEcIDRNum, 2)
	dt.expectEq("Error_I_P uiIDRLostNum", sDecStatic.UiIDRLostNum, 0)
	dt.Uninit()

	//Decoder error bs when the first IDR lost
	iRet = dt.ValidInit()
	if iRet != ERR_NONE {
		dt.t.Fatalf("ValidInit: %d", iRet)
	}
	iError = 2
	dt.m_pDec.SetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iError)
	dt.DecoderBs("BA_MW_D_IDR_LOST.264")
	dt.m_pDec.GetOption(api.DECODER_OPTION_GET_STATISTICS, &sDecStatic)
	dt.expectEq("IDR_LOST uiAvgEcRatio", sDecStatic.UiAvgEcRatio, 88)
	dt.expectEq("IDR_LOST uiAvgEcPropRatio", sDecStatic.UiAvgEcPropRatio, 88)
	dt.expectEq("IDR_LOST uiDecodedFrameCount", sDecStatic.UiDecodedFrameCount, 97)
	dt.expectEq("IDR_LOST uiHeight", sDecStatic.UiHeight, 144)
	dt.expectEq("IDR_LOST uiIDRCorrectNum", sDecStatic.UiIDRCorrectNum, 3)
	dt.expectEq("IDR_LOST uiEcIDRNum", sDecStatic.UiEcIDRNum, 0)
	dt.expectEq("IDR_LOST uiResolutionChangeTimes", sDecStatic.UiResolutionChangeTimes, 1)
	dt.expectEq("IDR_LOST uiWidth", sDecStatic.UiWidth, 176)
	dt.expectEq("IDR_LOST uiEcFrameNum", sDecStatic.UiEcFrameNum, 27)
	dt.expectEq("IDR_LOST uiIDRLostNum", sDecStatic.UiIDRLostNum, 1)
	dt.Uninit()

	//ecoder error bs when the first P lost
	iRet = dt.ValidInit()
	if iRet != ERR_NONE {
		dt.t.Fatalf("ValidInit: %d", iRet)
	}

	iError = 2
	dt.m_pDec.SetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iError)

	dt.DecoderBs("BA_MW_D_P_LOST.264")

	dt.m_pDec.GetOption(api.DECODER_OPTION_GET_STATISTICS, &sDecStatic)
	dt.expectEq("P_LOST uiAvgEcRatio", sDecStatic.UiAvgEcRatio, 85)
	dt.expectEq("P_LOST uiAvgEcPropRatio", sDecStatic.UiAvgEcPropRatio, 85)
	dt.expectEq("P_LOST uiDecodedFrameCount", sDecStatic.UiDecodedFrameCount, 99)
	dt.expectEq("P_LOST uiHeight", sDecStatic.UiHeight, 144)
	dt.expectEq("P_LOST uiIDRCorrectNum", sDecStatic.UiIDRCorrectNum, 4)
	dt.expectEq("P_LOST uiEcIDRNum", sDecStatic.UiEcIDRNum, 0)
	dt.expectEq("P_LOST uiResolutionChangeTimes", sDecStatic.UiResolutionChangeTimes, 1)
	dt.expectEq("P_LOST uiWidth", sDecStatic.UiWidth, 176)
	dt.expectEq("P_LOST uiEcFrameNum", sDecStatic.UiEcFrameNum, 28)
	dt.expectEq("P_LOST uiIDRLostNum", sDecStatic.UiIDRLostNum, 0)
	dt.Uninit()
	//EC enable

	//EC Off UT just correc bitstream
	iRet = dt.ValidInit()
	if iRet != ERR_NONE {
		dt.t.Fatalf("ValidInit: %d", iRet)
	}

	iError = 0
	dt.m_pDec.SetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iError)
	dt.DecoderBs("test_vd_1d.264")

	dt.m_pDec.GetOption(api.DECODER_OPTION_GET_STATISTICS, &sDecStatic)

	dt.expectEq("vd_1d uiAvgEcRatio", sDecStatic.UiAvgEcRatio, 0)
	dt.expectEq("vd_1d uiAvgEcPropRatio", sDecStatic.UiAvgEcPropRatio, 0)
	dt.expectEq("vd_1d uiDecodedFrameCount", sDecStatic.UiDecodedFrameCount, 9)
	dt.expectEq("vd_1d uiHeight", sDecStatic.UiHeight, 192)
	dt.expectEq("vd_1d uiIDRCorrectNum", sDecStatic.UiIDRCorrectNum, 1)
	dt.expectEq("vd_1d uiResolutionChangeTimes", sDecStatic.UiResolutionChangeTimes, 1)
	dt.expectEq("vd_1d uiWidth", sDecStatic.UiWidth, 320)
	dt.expectEq("vd_1d uiEcFrameNum", sDecStatic.UiEcFrameNum, 0)
	dt.expectEq("vd_1d uiIDRLostNum", sDecStatic.UiIDRLostNum, 0)
	dt.Uninit()
}

// DECODER_OPTION_GET_SAR_INFO
func (dt *decoderInterfaceTest) TestGetDecSarInfo() {
	var eRet int32
	var iRet int32
	var sVuiSarInfo api.SVuiSarInfo

	iRet = dt.ValidInit()
	if iRet != ERR_NONE {
		dt.t.Fatalf("ValidInit: %d", iRet)
	}
	//GetOption before decoding
	dt.m_pDec.GetOption(api.DECODER_OPTION_GET_SAR_INFO, &sVuiSarInfo)
	dt.expectEq("uiSarWidth", sVuiSarInfo.UiSarWidth, 0)
	dt.expectEq("uiSarHeight", sVuiSarInfo.UiSarHeight, 0)
	dt.expectEq("bOverscanAppropriateFlag", sVuiSarInfo.BOverscanAppropriateFlag, false)
	// setoption not support,
	eRet = dt.m_pDec.SetOption(api.DECODER_OPTION_GET_SAR_INFO, nil)
	dt.expectEq("SetOption(GET_SAR_INFO)", eRet, int32(api.CmInitParaError))

	//Decoder specific bs
	dt.DecoderBs("SarVui.264")
	dt.m_pDec.GetOption(api.DECODER_OPTION_GET_SAR_INFO, &sVuiSarInfo)
	dt.expectEq("uiSarWidth", sVuiSarInfo.UiSarWidth, 80)                               //DO NOT MODIFY the data value
	dt.expectEq("uiSarHeight", sVuiSarInfo.UiSarHeight, 33)                             //DO NOT MODIFY the data value
	dt.expectEq("bOverscanAppropriateFlag", sVuiSarInfo.BOverscanAppropriateFlag, true) //DO NOT MODIFY the data value
	dt.Uninit()
}

// DECODER_OPTION_GET_SAR_INFO, test Vui in subset sps
func (dt *decoderInterfaceTest) TestVuiInSubsetSps() {
	var iRet int32
	var sVuiSarInfo api.SVuiSarInfo

	iRet = dt.ValidInit()
	if iRet != ERR_NONE {
		dt.t.Fatalf("ValidInit: %d", iRet)
	}

	//GetOption before decoding
	dt.m_pDec.GetOption(api.DECODER_OPTION_GET_SAR_INFO, &sVuiSarInfo)
	dt.expectEq("uiSarWidth", sVuiSarInfo.UiSarWidth, 0)
	dt.expectEq("uiSarHeight", sVuiSarInfo.UiSarHeight, 0)
	dt.expectEq("bOverscanAppropriateFlag", sVuiSarInfo.BOverscanAppropriateFlag, false)

	dt.DecoderBs("sps_subsetsps_bothVUI.264")
	dt.m_pDec.GetOption(api.DECODER_OPTION_GET_SAR_INFO, &sVuiSarInfo)
	dt.expectEq("uiSarWidth", sVuiSarInfo.UiSarWidth, 1)                                 //DO NOT MODIFY the data value
	dt.expectEq("uiSarHeight", sVuiSarInfo.UiSarHeight, 1)                               //DO NOT MODIFY the data value
	dt.expectEq("bOverscanAppropriateFlag", sVuiSarInfo.BOverscanAppropriateFlag, false) //DO NOT MODIFY the data value
	dt.Uninit()
}

// TEST_F (DecoderInterfaceTest, DecoderInterfaceAll)
func TestDecoderInterfaceAll(t *testing.T) {
	dt := &decoderInterfaceTest{t: t, rnd: rand.New(rand.NewSource(1))}
	dt.SetUp()
	defer dt.TearDown()

	steps := []struct {
		name string
		fn   func()
	}{
		//Initialize Uninitialize
		{"InitUninit", dt.TestInitUninit},
		//DECODER_OPTION_END_OF_STREAM
		{"EndOfStream", dt.TestEndOfStream},
		//DECODER_OPTION_VCL_NAL
		{"VclNal", dt.TestVclNal},
		//DECODER_OPTION_ERROR_CON_IDC
		{"ErrorConIdc", dt.TestErrorConIdc},
		//DECODER_OPTION_GET_STATISTICS
		{"GetDecStatistics", dt.TestGetDecStatistics},
		//DECODER_OPTION_GET_SAR_INFO
		{"GetDecSarInfo", dt.TestGetDecSarInfo},
		//DECODER_OPTION_GET_SAR_INFO with vui in subsetsps
		{"VuiInSubsetSps", dt.TestVuiInSubsetSps},
	}
	for _, s := range steps {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s: panic: %v", s.name, r)
				}
			}()
			s.fn()
		}()
	}
}

// TEST_F (DecoderCapabilityTest, JustInit) from test/api/decoder_test.cpp.
func TestDecoderCapabilityJustInit(t *testing.T) {
	var sDecCap api.SDecoderCapability
	iRet := WelsGetDecoderCapability(&sDecCap)
	if iRet != 0 {
		t.Fatalf("WelsGetDecoderCapability: %d", iRet)
	}
	if sDecCap.IProfileIdc != 66 || sDecCap.IProfileIop != 0xE0 || sDecCap.ILevelIdc != 32 ||
		sDecCap.IMaxMbps != 216000 || sDecCap.IMaxFs != 5120 || sDecCap.IMaxCpb != 20000 ||
		sDecCap.IMaxDpb != 20480 || sDecCap.IMaxBr != 20000 || sDecCap.BRedPicCap != false {
		t.Errorf("unexpected capability %+v", sDecCap)
	}
}
