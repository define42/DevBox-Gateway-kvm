// Port of test/decoder/DecUT_ManageRef.cpp (plus a few extra checks of the
// sliding-window / B-slice list paths, not in the C suite).

package decoder

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Regression test for the MMCO5 stale sTmpRefPic snapshot (see the C test
// for the full rationale): WelsMarkAsRef() with a non-nil pLastDec operates
// on sTmpRefPic; an MMCO_RESET must not leave the stale picture in it.
type manageDecRefMmco5Fixture struct {
	ctx_            SWelsDecoderContext
	dqLayer_        SDqLayer
	refMarking_     SRefPicMarking
	sps_            SSps
	pps_            SPps
	lastDecPicInfo_ SWelsLastDecPicInfo
	nalUnit_        SNalUnit
	accessUnit_     SAccessUnit
	stalePic_       SPicture
	newPic_         SPicture
}

func newManageDecRefMmco5Fixture() *manageDecRefMmco5Fixture {
	f := &manageDecRefMmco5Fixture{}
	// A non-IDR slice signalling adaptive reference picture marking with a
	// single MMCO_RESET (MMCO5) command.
	f.refMarking_.bAdaptiveRefPicMarkingModeFlag = true
	f.refMarking_.sMmcoRef[0].uiMmcoType = common.MMCO_RESET
	f.refMarking_.sMmcoRef[1].uiMmcoType = common.MMCO_END

	f.sps_.iSpsId = 0
	f.sps_.iNumRefFrames = 4
	f.sps_.uiLog2MaxFrameNum = 4
	f.pps_.iPpsId = 0

	f.dqLayer_.pRefPicMarking = &f.refMarking_
	f.dqLayer_.sLayerInfo.pSps = &f.sps_ // read directly by MMCO()

	f.nalUnit_.sNalHeaderExt.SNalUnitHeader.ENalUnitType = common.NAL_UNIT_CODED_SLICE // non-IDR
	f.nalUnit_.sNalHeaderExt.BIdrFlag = false

	f.accessUnit_.pNalUnitsList = []*SNalUnit{&f.nalUnit_}
	f.accessUnit_.uiStartPos = 0
	f.accessUnit_.uiEndPos = 0

	f.ctx_.pCurDqLayer = &f.dqLayer_
	f.ctx_.pSps = &f.sps_
	f.ctx_.pPps = &f.pps_
	f.ctx_.pAccessUnitList = &f.accessUnit_
	f.ctx_.pLastDecPicInfo = &f.lastDecPicInfo_

	f.stalePic_.iRefCount = 0
	f.stalePic_.eSliceType = common.I_SLICE
	f.stalePic_.iFrameNum = 999

	f.ctx_.sRefPic.pShortRefList[common.LIST_0][0] = &f.stalePic_
	f.ctx_.sRefPic.uiShortRefCount[common.LIST_0] = 1
	f.ctx_.sTmpRefPic = f.ctx_.sRefPic // the real snapshot idiom

	f.newPic_.iFrameNum = 0
	return f
}

func TestManageDecRefMmco5Test_Mmco5ResetInvalidatesThreadedSnapshot(t *testing.T) {
	f := newManageDecRefMmco5Fixture()
	if ret := WelsMarkAsRef(&f.ctx_, &f.newPic_); ret != 0 {
		t.Fatalf("WelsMarkAsRef = %d, want 0", ret)
	}

	if got := f.ctx_.sTmpRefPic.uiShortRefCount[common.LIST_0]; got != 1 {
		t.Errorf("sTmpRefPic short ref count = %d, want 1", got)
	}
	for i := 0; i < int(f.ctx_.sTmpRefPic.uiShortRefCount[common.LIST_0]); i++ {
		if f.ctx_.sTmpRefPic.pShortRefList[common.LIST_0][i] == &f.stalePic_ {
			t.Errorf("stale reference picture pointer leaked into sTmpRefPic after MMCO5 reset")
		}
	}

	if got := f.ctx_.sRefPic.uiShortRefCount[common.LIST_0]; got != 0 {
		t.Errorf("sRefPic short ref count = %d, want 0", got)
	}
	if got := f.ctx_.sRefPic.uiLongRefCount[common.LIST_0]; got != 0 {
		t.Errorf("sRefPic long ref count = %d, want 0", got)
	}
}

// Extra: sliding window drops the oldest short-term reference.
func TestManageDecRef_SlidingWindow(t *testing.T) {
	f := newManageDecRefMmco5Fixture()
	f.refMarking_.bAdaptiveRefPicMarkingModeFlag = false
	f.ctx_.pParam = &api.SDecodingParam{}
	f.sps_.iNumRefFrames = 2
	f.ctx_.sRefPic = SRefPic{}
	pics := make([]SPicture, 4)
	for i := range pics {
		pics[i].iFrameNum = int32(i)
		pics[i].eSliceType = common.P_SLICE
		f.ctx_.pDec = &pics[i]
		if ret := WelsMarkAsRef(&f.ctx_, nil); ret != 0 {
			t.Fatalf("WelsMarkAsRef(%d) = %d", i, ret)
		}
	}
	if got := f.ctx_.sRefPic.uiShortRefCount[common.LIST_0]; got != 2 {
		t.Fatalf("short ref count = %d, want 2", got)
	}
	if f.ctx_.sRefPic.pShortRefList[0][0] != &pics[3] || f.ctx_.sRefPic.pShortRefList[0][1] != &pics[2] {
		t.Fatalf("unexpected short ref list order")
	}
	if pics[0].bUsedAsRef || pics[1].bUsedAsRef || pics[0].iFrameNum != -1 {
		t.Fatalf("dropped pictures still referenced")
	}
}

// Extra: B-slice default list order (POC based).
func TestManageDecRef_InitBSliceRefList(t *testing.T) {
	var ctx SWelsDecoderContext
	var dq SDqLayer
	var sps SSps
	sps.uiLog2MaxFrameNum = 4
	dq.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.pSps = &sps
	dq.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.iFrameNum = 5
	ctx.pCurDqLayer = &dq
	ctx.eSliceType = common.B_SLICE
	pocs := []int32{8, 2, 12, 4} // short list (most recent first)
	pics := make([]SPicture, len(pocs))
	for i, p := range pocs {
		pics[i].iFramePoc = p
		pics[i].iFrameNum = int32(i)
		ctx.sRefPic.pShortRefList[0][i] = &pics[i]
	}
	ctx.sRefPic.uiShortRefCount[0] = uint8(len(pocs))
	if ret := WelsInitBSliceRefList(&ctx, 6); ret != ERR_NONE {
		t.Fatalf("ret = %d", ret)
	}
	want0 := []int32{4, 2, 8, 12}
	want1 := []int32{8, 12, 4, 2}
	for i := range want0 {
		if ctx.sRefPic.pRefList[0][i].iFramePoc != want0[i] {
			t.Errorf("L0[%d] poc = %d, want %d", i, ctx.sRefPic.pRefList[0][i].iFramePoc, want0[i])
		}
		if ctx.sRefPic.pRefList[1][i].iFramePoc != want1[i] {
			t.Errorf("L1[%d] poc = %d, want %d", i, ctx.sRefPic.pRefList[1][i].iFramePoc, want1[i])
		}
	}
	if ctx.sRefPic.uiRefCount[0] != 4 || ctx.sRefPic.uiRefCount[1] != 4 {
		t.Errorf("ref counts = %d, %d", ctx.sRefPic.uiRefCount[0], ctx.sRefPic.uiRefCount[1])
	}
}
