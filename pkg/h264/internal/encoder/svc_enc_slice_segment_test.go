package encoder

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

func TestSliceSegment_RasterRowSlices(t *testing.T) {
	var dq SDqLayer
	var arg api.SSliceArgument
	arg.UiSliceMode = api.SM_RASTER_SLICE
	arg.UiSliceNum = 3
	if ret := InitSliceSegment(&dq, &arg, 4, 3); ret != 0 {
		t.Fatalf("InitSliceSegment returned %d", ret)
	}
	for i := int32(0); i < 12; i++ {
		if got, want := WelsMbToSliceIdc(&dq, i), uint16(i/4); got != want {
			t.Fatalf("mb %d: slice %d want %d", i, got, want)
		}
	}
	if WelsGetNextMbOfSlice(&dq, 3) != -1 || WelsGetNextMbOfSlice(&dq, 4) != 5 {
		t.Fatal("WelsGetNextMbOfSlice")
	}
	if WelsMbToSliceIdc(&dq, 12) != 0xffff {
		t.Fatal("out of range idc")
	}
	UninitSliceSegment(&dq)
	if dq.sSliceEncCtx.pOverallMbMap != nil || dq.sSliceEncCtx.iMbNumInFrame != 0 {
		t.Fatal("UninitSliceSegment")
	}
}

func TestSliceSegment_FixedSliceNum(t *testing.T) {
	var dq SDqLayer
	var arg api.SSliceArgument
	arg.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
	arg.UiSliceNum = 3
	if !CheckFixedSliceNumMultiSliceSetting(10, &arg) {
		t.Fatal("CheckFixedSliceNumMultiSliceSetting")
	}
	if arg.UiSliceMbNum[0] != 3 || arg.UiSliceMbNum[1] != 3 || arg.UiSliceMbNum[2] != 4 {
		t.Fatalf("assign list %v", arg.UiSliceMbNum[:3])
	}
	InitSliceSegment(&dq, &arg, 5, 2)
	want := []uint16{0, 0, 0, 1, 1, 1, 2, 2, 2, 2}
	for i, w := range want {
		if dq.sSliceEncCtx.pOverallMbMap[i] != w {
			t.Fatalf("map %v", dq.sSliceEncCtx.pOverallMbMap)
		}
	}
	if dq.sSliceEncCtx.uiSliceSizeConstraint != DEFAULT_MAXPACKETSIZE_CONSTRAINT ||
		dq.sSliceEncCtx.iMaxSliceNumConstraint != MAX_SLICES_NUM {
		t.Fatal("constraints")
	}
}

func TestSliceSegment_RasterSetting(t *testing.T) {
	var arg api.SSliceArgument
	arg.UiSliceMbNum[0] = 5
	arg.UiSliceMbNum[1] = 5
	if !CheckRasterMultiSliceSetting(12, &arg) || arg.UiSliceNum != 3 || arg.UiSliceMbNum[2] != 2 {
		t.Fatalf("raster correction: num %d list %v", arg.UiSliceNum, arg.UiSliceMbNum[:3])
	}
	arg = api.SSliceArgument{}
	arg.UiSliceMbNum[0] = 8
	arg.UiSliceMbNum[1] = 8
	if !CheckRasterMultiSliceSetting(12, &arg) || arg.UiSliceNum != 2 || arg.UiSliceMbNum[1] != 4 {
		t.Fatalf("raster cut: num %d list %v", arg.UiSliceNum, arg.UiSliceMbNum[:3])
	}
}

func TestSliceSegment_GomValidCheckSliceNum(t *testing.T) {
	n := uint32(8)
	// 10x2 MBs, GOM = 2 rows of 10 MB: only one GOM fits
	if GomValidCheckSliceNum(10, 2, &n) || n != 1 {
		t.Fatalf("GomValidCheckSliceNum -> %d", n)
	}
	n = 2
	if !GomValidCheckSliceNum(10, 4, &n) || n != 2 {
		t.Fatalf("GomValidCheckSliceNum -> %d", n)
	}
	if DynamicMaxSliceNumConstraint(35, 4, 2) != 15 {
		t.Fatal("DynamicMaxSliceNumConstraint")
	}
}
