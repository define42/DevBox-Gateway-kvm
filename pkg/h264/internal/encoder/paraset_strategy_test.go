// Port of test/encoder/EncUT_ParameterSetStrategy.cpp (plus a few checks of
// the parameter set strategies, au_set and nal_encap).

package encoder

import (
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func paramSetGenerateParam(pParam *SWelsSvcCodingParam) {
	var sEncParamBase api.SEncParamBase
	//TODO: consider randomize it
	sEncParamBase.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	sEncParamBase.IPicWidth = 1280
	sEncParamBase.IPicHeight = 720
	sEncParamBase.ITargetBitrate = 1000000
	sEncParamBase.IRCMode = api.RC_BITRATE_MODE
	sEncParamBase.FMaxFrameRate = 30.0
	pParam.ParamBaseTranscode(&sEncParamBase)
}

func TestParameterSetStrategyFindExistingSps(t *testing.T) {
	m_pSpsArray := make([]SWelsSPS, common.MAX_SPS_COUNT)
	m_pSubsetArray := make([]SSubsetSps, common.MAX_SPS_COUNT)

	var iDlayerIndex int32
	var iDlayerCount int32
	bUseSubsetSps := false
	var iFoundId int32

	//init parameter
	sParam1 := NewSWelsSvcCodingParam()
	paramSetGenerateParam(sParam1)

	//prepare first SPS
	var iCurSpsId int32
	var iCurSpsInUse int32 = 1
	m_pSpsArrayPointer := &m_pSpsArray[iCurSpsId]

	pDlayerParam := &sParam1.SSpatialLayers[iDlayerIndex]
	WelsInitSps(m_pSpsArrayPointer, pDlayerParam, &sParam1.sDependencyLayers[iDlayerIndex], sParam1.UiIntraPeriod,
		sParam1.iMaxNumRefFrame,
		uint32(iCurSpsId), sParam1.BEnableFrameCroppingFlag, sParam1.IRCMode != api.RC_OFF_MODE, iDlayerCount, false)

	// try finding #0
	iFoundId = FindExistingSps(sParam1, bUseSubsetSps, iDlayerIndex, iDlayerCount, iCurSpsInUse,
		m_pSpsArray, m_pSubsetArray, false)
	if iFoundId != iCurSpsId {
		t.Fatalf("find #0: got %d, want %d", iFoundId, iCurSpsId)
	}

	// try not finding
	sParam2 := *sParam1
	sParam2.iMaxNumRefFrame++
	iFoundId = FindExistingSps(&sParam2, bUseSubsetSps, iDlayerIndex, iDlayerCount, iCurSpsInUse,
		m_pSpsArray, m_pSubsetArray, false)
	if iFoundId != INVALID_ID {
		t.Fatalf("not finding: got %d, want INVALID_ID", iFoundId)
	}

	// add new sps
	iCurSpsId = 1
	m_pSpsArrayPointer = &m_pSpsArray[iCurSpsId]
	pDlayerParam = &sParam2.SSpatialLayers[iDlayerIndex]
	WelsInitSps(m_pSpsArrayPointer, pDlayerParam, &sParam2.sDependencyLayers[iDlayerIndex], sParam2.UiIntraPeriod,
		sParam2.iMaxNumRefFrame,
		uint32(iCurSpsId), sParam2.BEnableFrameCroppingFlag, sParam2.IRCMode != api.RC_OFF_MODE, iDlayerCount, false)
	iCurSpsInUse = 2

	// try finding #1
	iFoundId = FindExistingSps(&sParam2, bUseSubsetSps, iDlayerIndex, iDlayerCount, iCurSpsInUse,
		m_pSpsArray, m_pSubsetArray, false)
	if iFoundId != iCurSpsId {
		t.Fatalf("find #1: got %d, want %d", iFoundId, iCurSpsId)
	}

	// try finding #0
	iFoundId = FindExistingSps(sParam1, bUseSubsetSps, iDlayerIndex, iDlayerCount, iCurSpsInUse,
		m_pSpsArray, m_pSubsetArray, false)
	if iFoundId != 0 {
		t.Fatalf("find #0 again: got %d, want 0", iFoundId)
	}

	// try not finding
	if sParam2.sDependencyLayers[0].iActualWidth > 1 {
		sParam2.sDependencyLayers[0].iActualWidth--
	} else {
		sParam2.sDependencyLayers[0].iActualWidth++
	}

	iFoundId = FindExistingSps(&sParam2, bUseSubsetSps, iDlayerIndex, iDlayerCount, iCurSpsInUse,
		m_pSpsArray, m_pSubsetArray, false)
	if iFoundId != INVALID_ID {
		t.Fatalf("not finding after width change: got %d, want INVALID_ID", iFoundId)
	}
}

func TestParameterSetStrategyTestVSTPParameters(t *testing.T) {
	m_pSpsArray := make([]SWelsSPS, common.MAX_SPS_COUNT)

	// this test verifies that the client's "video signal type present" parameter values end up in SWelsSPS

	//init client parameters
	var sParamExt api.SEncParamExt
	FillDefaultParam(&sParamExt)
	sParamExt.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	sParamExt.IPicWidth = 1280
	sParamExt.IPicHeight = 720
	sParamExt.ITargetBitrate = 1000000
	sParamExt.IRCMode = api.RC_BITRATE_MODE
	sParamExt.FMaxFrameRate = 30.0

	// VSTP parameters should be their default values (see SWelsSvcCodingParam::FillDefault())
	for i := 0; i < api.MAX_SPATIAL_LAYER_NUM; i++ {
		l := &sParamExt.SSpatialLayers[i]
		if l.BVideoSignalTypePresent || l.UiVideoFormat != uint8(api.VF_UNDEF) || l.BFullRange ||
			l.BColorDescriptionPresent || l.UiColorPrimaries != uint8(api.CP_UNDEF) ||
			l.UiTransferCharacteristics != uint8(api.TRC_UNDEF) || l.UiColorMatrix != uint8(api.CM_UNDEF) {
			t.Fatalf("layer %d: unexpected default VSTP values %+v", i, *l)
		}
	}

	// set non-default VSTP values
	sParamExt.ISpatialLayerNum = 2

	sParamExt.SSpatialLayers[0].BVideoSignalTypePresent = true
	sParamExt.SSpatialLayers[0].UiVideoFormat = uint8(api.VF_NTSC)
	sParamExt.SSpatialLayers[0].BFullRange = true
	sParamExt.SSpatialLayers[0].BColorDescriptionPresent = true
	sParamExt.SSpatialLayers[0].UiColorPrimaries = uint8(api.CP_BT709)
	sParamExt.SSpatialLayers[0].UiTransferCharacteristics = uint8(api.TRC_BT709)
	sParamExt.SSpatialLayers[0].UiColorMatrix = uint8(api.CM_BT709)

	sParamExt.SSpatialLayers[1].BVideoSignalTypePresent = true
	sParamExt.SSpatialLayers[1].UiVideoFormat = uint8(api.VF_PAL)
	sParamExt.SSpatialLayers[1].BFullRange = true
	sParamExt.SSpatialLayers[1].BColorDescriptionPresent = true
	sParamExt.SSpatialLayers[1].UiColorPrimaries = uint8(api.CP_SMPTE170M)
	sParamExt.SSpatialLayers[1].UiTransferCharacteristics = uint8(api.TRC_SMPTE170M)
	sParamExt.SSpatialLayers[1].UiColorMatrix = uint8(api.CM_SMPTE170M)

	// transcode parameters from client
	sSvcCodingParam := NewSWelsSvcCodingParam()
	if iRet := sSvcCodingParam.ParamTranscode(&sParamExt); iRet != 0 {
		t.Fatalf("ParamTranscode returned %d", iRet)
	}

	// transcoded VSTP parameters should match the client values
	for i := int32(0); i < sParamExt.ISpatialLayerNum; i++ {
		a, b := &sParamExt.SSpatialLayers[i], &sSvcCodingParam.SSpatialLayers[i]
		if a.BVideoSignalTypePresent != b.BVideoSignalTypePresent || a.UiVideoFormat != b.UiVideoFormat ||
			a.BFullRange != b.BFullRange || a.BColorDescriptionPresent != b.BColorDescriptionPresent ||
			a.UiColorPrimaries != b.UiColorPrimaries || a.UiTransferCharacteristics != b.UiTransferCharacteristics ||
			a.UiColorMatrix != b.UiColorMatrix {
			t.Fatalf("layer %d: transcoded VSTP mismatch", i)
		}
	}

	// use transcoded parameters to initialize an SWelsSPS
	m_pSpsArrayPointer := &m_pSpsArray[0]
	pDlayerParam := &sSvcCodingParam.SSpatialLayers[0]
	iRet := WelsInitSps(
		m_pSpsArrayPointer,
		pDlayerParam,
		&sSvcCodingParam.sDependencyLayers[0],
		sSvcCodingParam.UiIntraPeriod,
		sSvcCodingParam.iMaxNumRefFrame,
		0, //SpsId
		sSvcCodingParam.BEnableFrameCroppingFlag,
		sSvcCodingParam.IRCMode != api.RC_OFF_MODE,
		0, //DlayerCount
		false)
	if iRet != 0 {
		t.Fatalf("WelsInitSps returned %d", iRet)
	}

	// SPS VSTP parameters should match the transcoded values
	l := &sSvcCodingParam.SSpatialLayers[0]
	s := m_pSpsArrayPointer
	if l.BVideoSignalTypePresent != s.bVideoSignalTypePresent || l.UiVideoFormat != s.uiVideoFormat ||
		l.BFullRange != s.bFullRange || l.BColorDescriptionPresent != s.bColorDescriptionPresent ||
		l.UiColorPrimaries != s.uiColorPrimaries || l.UiTransferCharacteristics != s.uiTransferCharacteristics ||
		l.UiColorMatrix != s.uiColorMatrix {
		t.Fatalf("SPS VSTP values do not match the transcoded parameters")
	}
}

// The C-layout emulation in paraset_strategy.go relies on Go laying out
// SParaSetOffset exactly like the C struct (release build, no _DEBUG member).
func TestParaSetOffsetLayout(t *testing.T) {
	align4 := func(x uintptr) uintptr { return (x + 3) &^ 3 }
	var v SParaSetOffsetVariable
	var p SParaSetOffset
	kDelta := uintptr(4 * MAX_DQ_LAYER_NUM)
	kNext := align4(kDelta + MAX_PPS_COUNT)
	if unsafe.Offsetof(v.bUsedParaSetIdInBs) != kDelta || unsafe.Offsetof(v.uiNextParaSetIdToUseInBs) != kNext ||
		unsafe.Sizeof(v) != kNext+4 {
		t.Fatalf("SParaSetOffsetVariable layout differs from C")
	}
	kMap := PARA_SET_TYPE * (kNext + 4)
	kList := align4(kMap + MAX_DQ_LAYER_NUM)
	kNeeded := kList + 4*MAX_DQ_LAYER_NUM*MAX_PPS_COUNT
	if unsafe.Offsetof(p.bPpsIdMappingIntoSubsetsps) != kMap || unsafe.Offsetof(p.iPpsIdList) != kList ||
		unsafe.Offsetof(p.uiNeededSpsNum) != kNeeded || unsafe.Sizeof(p) != kNeeded+6*4 {
		t.Fatalf("SParaSetOffset layout differs from C")
	}
}

func TestParameterSetStrategyIncreasing(t *testing.T) {
	s := CreateParametersetStrategy(api.INCREASING_ID, false, 1)
	if _, ok := s.(*CWelsParametersetIdIncreasing); !ok {
		t.Fatalf("wrong strategy type %T", s)
	}
	// three IDRs with one SPS (id 0) and one PPS (id 0)
	for k := int32(0); k < 3; k++ {
		s.Update(0, PARA_SET_TYPE_AVCSPS)
		s.Update(0, PARA_SET_TYPE_PPS)
		if got := s.GetSpsIdOffsetList(PARA_SET_TYPE_AVCSPS)[0]; got != k {
			t.Fatalf("idr %d: sps delta %d", k, got)
		}
		if got := s.GetPpsIdOffset(0); got != k {
			t.Fatalf("idr %d: pps delta %d", k, got)
		}
		if got := s.GetSpsIdOffset(0, 0); got != k {
			t.Fatalf("idr %d: GetSpsIdOffset %d", k, got)
		}
	}
	if s.GetAllNeededParasetNum() != 1+0+2 {
		t.Fatalf("GetAllNeededParasetNum = %d", s.GetAllNeededParasetNum())
	}

	l := CreateParametersetStrategy(api.SPS_LISTING_AND_PPS_INCREASING, false, 1)
	// SPS ids beyond MAX_DQ_LAYER_NUM must not panic (C layout emulation)
	l.Update(common.MAX_SPS_COUNT-1, PARA_SET_TYPE_AVCSPS)
	if got := l.GetSpsIdOffsetList(PARA_SET_TYPE_AVCSPS)[common.MAX_SPS_COUNT-1]; got != -(common.MAX_SPS_COUNT - 1) {
		t.Fatalf("listing sps delta %d", got)
	}
	if l.GetNeededSpsNum() != common.MAX_SPS_COUNT || l.GetNeededSubsetSpsNum() != common.MAX_SPS_COUNT ||
		l.GetNeededPpsNum() != 1 {
		t.Fatalf("listing needed nums %d %d %d", l.GetNeededSpsNum(), l.GetNeededSubsetSpsNum(), l.GetNeededPpsNum())
	}
}

func TestWelsEncodeNalEmulationPrevention(t *testing.T) {
	raw := []uint8{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x05}
	var nal SWelsNalRaw
	nal.pRawData = raw
	nal.iPayloadSize = int32(len(raw))
	nal.sNalExt.SNalUnitHeader.ENalUnitType = common.NAL_UNIT_SPS
	nal.sNalExt.SNalUnitHeader.UiNalRefIdc = 3
	dst := make([]uint8, 64)
	var iLen int32
	if r := WelsEncodeNal(&nal, nil, int32(len(dst)), dst, &iLen); r != ENC_RETURN_SUCCESS {
		t.Fatalf("WelsEncodeNal returned %d", r)
	}
	want := []uint8{0, 0, 0, 1, 0x67, 0x00, 0x00, 0x03, 0x01, 0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x00, 0x03, 0x05}
	if int(iLen) != len(want) {
		t.Fatalf("length %d, want %d (%x)", iLen, len(want), dst[:iLen])
	}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("byte %d: %#x want %#x (%x)", i, dst[i], want[i], dst[:iLen])
		}
	}
}

func TestAllocPicture(t *testing.T) {
	pPic := AllocPicture(176, 144, true, 0)
	if pPic == nil || pPic.iLineSize[0] != 256 || pPic.iLineSize[1] != 128 ||
		pPic.iDataOff[0] != (1+256)*32 || len(pPic.uiRefMbType) != 99 || pPic.iFrameNum != -1 {
		t.Fatalf("unexpected picture %+v", pPic)
	}
	FreePicture(&pPic)
	if pPic != nil {
		t.Fatalf("FreePicture did not clear the pointer")
	}
}
