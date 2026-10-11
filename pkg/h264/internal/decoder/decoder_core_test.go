package decoder

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// newDecoderCoreTestCtx builds a minimal decoder context with the static
// memory of WelsInitStaticMemory (the C tests use the full decoder Init()).
func newDecoderCoreTestCtx(t *testing.T, bParseOnly bool) *SWelsDecoderContext {
	t.Helper()
	pCtx := &SWelsDecoderContext{}
	pCtx.pParam = &api.SDecodingParam{BParseOnly: bParseOnly}
	pCtx.pDecoderStatistics = &api.SDecoderStatistics{}
	pCtx.pLastDecPicInfo = &SWelsLastDecPicInfo{}
	if r := WelsInitStaticMemory(pCtx); r != ERR_NONE {
		t.Fatalf("WelsInitStaticMemory: %d", r)
	}
	return pCtx
}

// Port of DecUT_ParseSyntax.cpp ExpandBsBufferRetargetsParseOnlyNalPos: verify
// that ExpandBsBuffer retargets pNalPos for both current-AU and queued next-AU
// NALs.
func TestExpandBsBufferRetargetsParseOnlyNalPos(t *testing.T) {
	m_pCtx := newDecoderCoreTestCtx(t, true)
	if m_pCtx.sSavedData.pHead == nil || m_pCtx.pAccessUnitList == nil || m_pCtx.pAccessUnitList.uiCountUnitsNum < 2 {
		t.Fatal("bad init")
	}

	// Slot 0: current-AU NAL; slot 1: queued next-AU NAL (uiAvailUnitsNum > uiActualUnitsNum).
	m_pCtx.pAccessUnitList.uiActualUnitsNum = 1
	m_pCtx.pAccessUnitList.uiAvailUnitsNum = 2

	pOldSavedHead := m_pCtx.sSavedData.pHead
	kiOldSavedSize := m_pCtx.iMaxBsBufferSizeInByte
	if kiOldSavedSize <= 64 {
		t.Fatal("buffer too small")
	}

	pNal0 := m_pCtx.pAccessUnitList.pNalUnitsList[0]
	pNal1 := m_pCtx.pAccessUnitList.pNalUnitsList[1]

	pNal0.sNalData.sVclNal.pNalPos = pOldSavedHead
	pNal0.sNalData.sVclNal.iNalPosOff = 16
	pNal1.sNalData.sVclNal.pNalPos = pOldSavedHead // queued next-AU entry
	pNal1.sNalData.sVclNal.iNalPosOff = 32
	pOldSavedHead[16] = 0xAB
	pOldSavedHead[32] = 0xCD

	kiSrcLen := kiOldSavedSize/MAX_BUFFERED_NUM + 1
	if r := ExpandBsBuffer(m_pCtx, kiSrcLen); r != ERR_NONE {
		t.Fatalf("ExpandBsBuffer: %d", r)
	}

	if m_pCtx.sSavedData.pHead == nil || dcSameBuffer(m_pCtx.sSavedData.pHead, pOldSavedHead) {
		t.Fatal("sSavedData not reallocated")
	}
	// Both current-AU and queued next-AU pNalPos must be retargeted.
	for i, pNal := range []*SNalUnit{pNal0, pNal1} {
		if !dcSameBuffer(pNal.sNalData.sVclNal.pNalPos, m_pCtx.sSavedData.pHead) {
			t.Errorf("nal %d: pNalPos not retargeted", i)
		}
	}
	if pNal0.sNalData.sVclNal.iNalPosOff != 16 || pNal1.sNalData.sVclNal.iNalPosOff != 32 {
		t.Errorf("offsets changed")
	}
	if pNal0.sNalData.sVclNal.pNalPos[16] != 0xAB || pNal1.sNalData.sVclNal.pNalPos[32] != 0xCD {
		t.Errorf("content not copied")
	}
	if m_pCtx.iMaxBsBufferSizeInByte != kiOldSavedSize<<1 || m_pCtx.sSavedData.pEnd != int(kiOldSavedSize<<1) ||
		m_pCtx.sRawData.pEnd != int(kiOldSavedSize<<1) {
		t.Errorf("unexpected new size %d", m_pCtx.iMaxBsBufferSizeInByte)
	}
}

// Port of DecUT_ParseSyntax.cpp ExpandBsBufferRetargetsQueuedNalUnitsOnly.
func TestExpandBsBufferRetargetsQueuedNalUnitsOnly(t *testing.T) {
	m_pCtx := newDecoderCoreTestCtx(t, false)
	pAu := m_pCtx.pAccessUnitList
	if pAu.uiCountUnitsNum < 3 || m_pCtx.sRawData.pHead == nil {
		t.Fatal("bad init")
	}

	pOldHead := m_pCtx.sRawData.pHead
	set := func(pBs *common.SBitStringAux, s, c, e int) {
		pBs.PBuf = pOldHead
		pBs.PStartBuf = s
		pBs.PCurBuf = c
		pBs.PEndBuf = e
	}
	pActual := &pAu.pNalUnitsList[0].sNalData.sVclNal.sSliceBitsRead
	set(pActual, 8, 12, 16)
	pQueued := &pAu.pNalUnitsList[1].sNalData.sVclNal.sSliceBitsRead
	set(pQueued, 24, 28, 32)
	pOnePastAvail := &pAu.pNalUnitsList[2].sNalData.sVclNal.sSliceBitsRead
	set(pOnePastAvail, 40, 44, 48)

	pAu.uiAvailUnitsNum = 2
	pAu.uiActualUnitsNum = 1 // queued count is still 2, so index 1 must be retargeted

	if r := ExpandBsBuffer(m_pCtx, m_pCtx.iMaxBsBufferSizeInByte); r != ERR_NONE {
		t.Fatalf("ExpandBsBuffer: %d", r)
	}
	if dcSameBuffer(m_pCtx.sRawData.pHead, pOldHead) {
		t.Fatal("sRawData not reallocated")
	}

	check := func(name string, pBs *common.SBitStringAux, pBuf []uint8, s, c, e int) {
		if !dcSameBuffer(pBs.PBuf, pBuf) || pBs.PStartBuf != s || pBs.PCurBuf != c || pBs.PEndBuf != e {
			t.Errorf("%s: unexpected bit string state", name)
		}
	}
	check("actual", pActual, m_pCtx.sRawData.pHead, 8, 12, 16)
	check("queued", pQueued, m_pCtx.sRawData.pHead, 24, 28, 32)
	// Slot at index uiAvailUnitsNum (one-past queued range) must not be touched.
	check("one past", pOnePastAvail, pOldHead, 40, 44, 48)
}

func TestCheckBsBuffer(t *testing.T) {
	pCtx := newDecoderCoreTestCtx(t, false)
	if r := CheckBsBuffer(pCtx, MAX_ACCESS_UNIT_CAPACITY+1); r != ERR_INFO_INVALID_ACCESS {
		t.Errorf("oversized AU: got %d", r)
	}
	if pCtx.iErrorCode&int32(api.DsBitstreamError) == 0 {
		t.Errorf("error code not set")
	}
	kiOld := pCtx.iMaxBsBufferSizeInByte
	if r := CheckBsBuffer(pCtx, kiOld/MAX_BUFFERED_NUM); r != ERR_NONE || pCtx.iMaxBsBufferSizeInByte != kiOld {
		t.Errorf("no expansion expected")
	}
	if r := CheckBsBuffer(pCtx, kiOld/MAX_BUFFERED_NUM+1); r != ERR_NONE || pCtx.iMaxBsBufferSizeInByte != kiOld*2 {
		t.Errorf("expansion expected")
	}
}

func TestDecodeNalHeaderExt(t *testing.T) {
	var sNal SNalUnit
	// idr=1, priority=0x15 | no_inter_layer_pred=1, dependency=3, quality=2 | temporal=5, use_ref_base=1, discardable=0, output=1, reserved=3
	DecodeNalHeaderExt(&sNal, []uint8{0x40 | 0x15, 0x80 | 0x30 | 0x02, 0xA0 | 0x10 | 0x04 | 0x03})
	h := &sNal.sNalHeaderExt
	if !h.BIdrFlag || h.UiPriorityId != 0x15 || h.INoInterLayerPredFlag != 1 || h.UiDependencyId != 3 ||
		h.UiQualityId != 2 || h.UiTemporalId != 5 || !h.BUseRefBasePicFlag || h.BDiscardableFlag ||
		!h.BOutputFlag || h.UiReservedThree2Bits != 3 || h.UiLayerDqId != (3<<4|2) {
		t.Errorf("unexpected header %+v", *h)
	}
}

func TestForceResetCurrentAccessUnit(t *testing.T) {
	var pAu *SAccessUnit
	MemInitNalList(&pAu, 6)
	orig := append([]*SNalUnit(nil), pAu.pNalUnitsList...)
	pAu.uiAvailUnitsNum = 5
	pAu.uiEndPos = 2
	pAu.uiActualUnitsNum = 3
	pAu.bCompletedAuFlag = true
	ForceResetCurrentAccessUnit(pAu)
	if pAu.uiAvailUnitsNum != 2 || pAu.uiActualUnitsNum != 0 || pAu.uiEndPos != 0 || pAu.bCompletedAuFlag {
		t.Errorf("unexpected counters %+v", *pAu)
	}
	if pAu.pNalUnitsList[0] != orig[3] || pAu.pNalUnitsList[1] != orig[4] ||
		pAu.pNalUnitsList[3] != orig[0] || pAu.pNalUnitsList[4] != orig[1] {
		t.Errorf("succeeding NALs not swapped to the front")
	}
}

func TestResetCurrentAccessUnit(t *testing.T) {
	pCtx := &SWelsDecoderContext{}
	MemInitNalList(&pCtx.pAccessUnitList, 6)
	pAu := pCtx.pAccessUnitList
	orig := append([]*SNalUnit(nil), pAu.pNalUnitsList...)
	pAu.uiAvailUnitsNum = 5
	pAu.uiActualUnitsNum = 3
	ResetCurrentAccessUnit(pCtx)
	if pAu.uiAvailUnitsNum != 2 || pAu.uiActualUnitsNum != 2 {
		t.Errorf("unexpected counters %+v", *pAu)
	}
	if pAu.pNalUnitsList[0] != orig[3] || pAu.pNalUnitsList[1] != orig[4] {
		t.Errorf("succeeding NALs not swapped to the front")
	}
	// counter mismatch guard
	pAu.uiAvailUnitsNum = 1
	pAu.uiActualUnitsNum = 3
	ResetCurrentAccessUnit(pCtx)
	if pAu.uiAvailUnitsNum != 0 || pAu.uiActualUnitsNum != 0 {
		t.Errorf("guard failed")
	}
}

// dcTestBitWriter is a minimal RBSP writer for slice header tests.
type dcTestBitWriter struct {
	buf  []uint8
	nbit int
}

func (w *dcTestBitWriter) bit(b uint32) {
	if w.nbit%8 == 0 {
		w.buf = append(w.buf, 0)
	}
	if b != 0 {
		w.buf[w.nbit/8] |= 0x80 >> uint(w.nbit%8)
	}
	w.nbit++
}

func (w *dcTestBitWriter) bits(v uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		w.bit((v >> uint(i)) & 1)
	}
}

func (w *dcTestBitWriter) ue(v uint32) {
	v++
	n := 0
	for t := v; t > 1; t >>= 1 {
		n++
	}
	w.bits(0, n)
	w.bits(v, n+1)
}

func (w *dcTestBitWriter) se(v int32) {
	if v > 0 {
		w.ue(uint32(2*v - 1))
	} else {
		w.ue(uint32(-2 * v))
	}
}

func TestParseSliceHeaderSyntaxsIdr(t *testing.T) {
	pCtx := newDecoderCoreTestCtx(t, false)
	pSps := &pCtx.sSpsPpsCtx.sSpsBuffer[0]
	pSps.iSpsId = 0
	pSps.iMbWidth, pSps.iMbHeight, pSps.uiTotalMbCount = 20, 15, 300
	pSps.uiLog2MaxFrameNum = 4
	pSps.uiPocType = 0
	pSps.iLog2MaxPocLsb = 6
	pSps.iNumRefFrames = 1
	pSps.bFrameMbsOnlyFlag = true
	pCtx.sSpsPpsCtx.bSpsAvailFlags[0] = true
	pPps := &pCtx.sSpsPpsCtx.sPpsBuffer[0]
	pPps.iSpsId, pPps.iPpsId = 0, 0
	pPps.uiNumSliceGroups = 1
	pPps.uiNumRefIdxL0Active = 1
	pPps.iPicInitQp = 26
	pPps.bDeblockingFilterControlPresentFlag = true
	pCtx.sSpsPpsCtx.bPpsAvailFlags[0] = true

	var w dcTestBitWriter
	w.ue(0)       // first_mb_in_slice
	w.ue(7)       // slice_type (I)
	w.ue(0)       // pps id
	w.bits(0, 4)  // frame_num
	w.ue(3)       // idr_pic_id
	w.bits(10, 6) // pic_order_cnt_lsb
	w.bit(0)      // no_output_of_prior_pics_flag
	w.bit(0)      // long_term_reference_flag
	w.se(-2)      // slice_qp_delta
	w.ue(0)       // disable_deblocking_filter_idc
	w.se(1)       // slice_alpha_c0_offset_div2
	w.se(-1)      // slice_beta_offset_div2
	w.bit(1)      // rbsp stop bit
	w.buf = append(w.buf, 0, 0, 0, 0, 0, 0, 0, 0)

	pAu := pCtx.pAccessUnitList
	pAu.uiAvailUnitsNum = 1
	pNal := pAu.pNalUnitsList[0]
	pNal.sNalHeaderExt.SNalUnitHeader.ENalUnitType = common.NAL_UNIT_CODED_SLICE_IDR
	pNal.sNalHeaderExt.SNalUnitHeader.UiNalRefIdc = 3
	pNal.sNalHeaderExt.INoInterLayerPredFlag = 1

	var sBs common.SBitStringAux
	if r := DecInitBits(&sBs, w.buf, 0, int32(len(w.buf)*8)); r != ERR_NONE {
		t.Fatalf("DecInitBits: %d", r)
	}
	if r := ParseSliceHeaderSyntaxs(pCtx, &sBs, false); r != ERR_NONE {
		t.Fatalf("ParseSliceHeaderSyntaxs: 0x%x", r)
	}
	pSh := &pNal.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader
	if pSh.eSliceType != common.I_SLICE || !pSh.bIdrFlag || pSh.uiIdrPicId != 3 || pSh.iPicOrderCntLsb != 10 ||
		pSh.iSliceQp != 24 || pSh.iSliceAlphaC0Offset != 2 || pSh.iSliceBetaOffset != -2 ||
		pSh.iMbWidth != 20 || pSh.iMbHeight != 15 || pSh.pSps != pSps || pSh.pPps != pPps {
		t.Errorf("unexpected slice header: type=%d idr=%v id=%d poc=%d qp=%d a=%d b=%d",
			pSh.eSliceType, pSh.bIdrFlag, pSh.uiIdrPicId, pSh.iPicOrderCntLsb, pSh.iSliceQp,
			pSh.iSliceAlphaC0Offset, pSh.iSliceBetaOffset)
	}
	pShExt := &pNal.sNalData.sVclNal.sSliceHeaderExt
	if pShExt.uiRefLayerDqId != 0xff || pShExt.uiScanIdxEnd != 15 || pShExt.bBasePredWeightTableFlag {
		t.Errorf("default slice header ext not filled")
	}
	if pCtx.pLastDecPicInfo.iPrevPicOrderCntLsb != 10 || pCtx.uiCurIdrPicId != 3 {
		t.Errorf("poc state not updated")
	}

	// Invalid PPS id is rejected.
	var w2 dcTestBitWriter
	w2.ue(0)
	w2.ue(7)
	w2.ue(1)
	w2.buf = append(w2.buf, 0, 0, 0, 0, 0, 0, 0, 0)
	DecInitBits(&sBs, w2.buf, 0, int32(len(w2.buf)*8))
	if r := ParseSliceHeaderSyntaxs(pCtx, &sBs, false); r != GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_PPS_ID) {
		t.Errorf("invalid pps: got 0x%x", r)
	}
}

func TestParseRefPicListReordering(t *testing.T) {
	var sSps SSps
	sSps.uiLog2MaxFrameNum = 4
	var sSh SSliceHeader
	sSh.pSps = &sSps
	sSh.eSliceType = common.P_SLICE
	sSh.uiRefCount[0] = 2

	var w dcTestBitWriter
	w.bit(1) // ref_pic_list_modification_flag_l0
	w.ue(0)  // idc
	w.ue(5)  // abs_diff_pic_num_minus1
	w.ue(2)  // idc
	w.ue(7)  // long_term_pic_num
	w.ue(3)  // end
	w.buf = append(w.buf, 0, 0, 0, 0, 0, 0, 0, 0)
	var sBs common.SBitStringAux
	DecInitBits(&sBs, w.buf, 0, int32(len(w.buf)*8))
	if r := ParseRefPicListReordering(&sBs, &sSh); r != ERR_NONE {
		t.Fatalf("got %d", r)
	}
	r := &sSh.pRefPicListReordering
	if !r.bRefPicListReorderingFlag[0] || r.sReorderingSyn[0][0].uiReorderingOfPicNumsIdc != 0 ||
		r.sReorderingSyn[0][0].uiAbsDiffPicNumMinus1 != 5 || r.sReorderingSyn[0][1].uiReorderingOfPicNumsIdc != 2 ||
		r.sReorderingSyn[0][1].uiLongTermPicNum != 7 || r.sReorderingSyn[0][2].uiReorderingOfPicNumsIdc != 3 {
		t.Errorf("unexpected reordering %+v", r.sReorderingSyn[0][:3])
	}

	// abs_diff_pic_num_minus1 above MaxPicNum is rejected.
	var w2 dcTestBitWriter
	w2.bit(1)
	w2.ue(1)
	w2.ue(17)
	w2.buf = append(w2.buf, 0, 0, 0, 0, 0, 0, 0, 0)
	DecInitBits(&sBs, w2.buf, 0, int32(len(w2.buf)*8))
	if r := ParseRefPicListReordering(&sBs, &sSh); r != GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_REF_REORDERING) {
		t.Errorf("got 0x%x", r)
	}
}

func TestInitialDqLayersContext(t *testing.T) {
	pCtx := &SWelsDecoderContext{}
	if r := InitialDqLayersContext(pCtx, 0, 16); r != ERR_INFO_INVALID_PARAM {
		t.Errorf("got %d", r)
	}
	if r := InitialDqLayersContext(pCtx, 33, 17); r != ERR_NONE {
		t.Fatalf("got %d", r)
	}
	if pCtx.sMb.iMbWidth != 3 || pCtx.sMb.iMbHeight != 2 || len(pCtx.sMb.pMbType[0]) != 6 ||
		pCtx.sMb.pSliceIdc[0][5] != -1 || pCtx.pDqLayersList[0] == nil || !pCtx.bInitialDqLayersMem {
		t.Errorf("unexpected allocation")
	}
	InitCurDqLayerData(pCtx, pCtx.pDqLayersList[0])
	if len(pCtx.pDqLayersList[0].pMv[common.LIST_1]) != 6 {
		t.Errorf("dq layer data not bound")
	}
	UninitialDqLayersContext(pCtx)
	if pCtx.pDqLayersList[0] != nil || pCtx.sMb.pMbType[0] != nil || pCtx.bInitialDqLayersMem {
		t.Errorf("not released")
	}
}
