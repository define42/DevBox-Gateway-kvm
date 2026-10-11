// Port of codec/encoder/core/inc/svc_enc_slice_segment.h.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"

// NOTE:
// if PREFIX_NALs are used in base layer(iDid=0, qid=0), MAX_SLICES_NUM will be half of MAX_NAL_UNITS_IN_LAYER in case ST or MT without PACKING_ONE_SLICE_PER_LAYER
// in case MT and PACKING_ONE_SLICE_PER_LAYER, MAX_SLICES_NUM should not be exceeding MAX_LAYER_NUM_OF_FRAME
// for AVC cases, maximal resolution we can support up to (?x1024) for SM_ROWMB_SLICE slice mode
// fine solution for MAX_SLICES_NUM, need us use the variable instead of MACRO for any resolution combining any multiple-slice mode adaptive
const (
	SAVED_NALUNIT_NUM                = (api.MAX_SPATIAL_LAYER_NUM * api.MAX_QUALITY_LAYER_NUM) + 1 + api.MAX_SPATIAL_LAYER_NUM // SPS/PPS + SEI/SSEI + PADDING_NAL
	MAX_SLICES_NUM                   = (api.MAX_NAL_UNITS_IN_LAYER - SAVED_NALUNIT_NUM) / 3                                    // Also MAX_SLICES_NUM need constrained by implementation: uiSliceIdc allocated in SSliceCtx.pOverallMbMap need a byte range as expected
	AVERSLICENUM_CONSTRAINT          = MAX_SLICES_NUM                                                                          // used in sNalList initialization,
	MIN_NUM_MB_PER_SLICE             = 48                                                                                      // (128/16 * 96/16), addressing the lowest resolution for multiple slicing is 128x96 above
	DEFAULT_MAXPACKETSIZE_CONSTRAINT = 1200                                                                                    // in bytes
	AVER_MARGIN_BYTES                = 100                                                                                     // in bytes
)

// JUMPPACKETSIZE_CONSTRAINT is ( max_byte - AVER_MARGIN_BYTES ) in bytes.
func JUMPPACKETSIZE_CONSTRAINT(max_byte int32) int32 {
	return max_byte - AVER_MARGIN_BYTES
}

// JUMPPACKETSIZE_JUDGE is ( (len) > JUMPPACKETSIZE_CONSTRAINT(max_byte) );
// mb_idx is unused (kept for signature fidelity).
func JUMPPACKETSIZE_JUDGE(len int32, mb_idx int32, max_byte int32) bool {
	return len > JUMPPACKETSIZE_CONSTRAINT(max_byte)
}

// SSliceCtx is the SSlice context (single/multiple slices).
type SSliceCtx struct {
	uiSliceMode            api.SliceModeEnum // 0: single slice in frame; 1: multiple slices in frame;
	iMbWidth               int16             // width of picture size in mb
	iMbHeight              int16             // height of picture size in mb
	iSliceNumInFrame       int32             // count number of slices in frame;
	iMbNumInFrame          int32             // count number of MBs in frame
	pOverallMbMap          []uint16          // C uint16_t*: overall MB map in frame (iMbNumInFrame entries), store virtual slice idc;
	uiSliceSizeConstraint  uint32            // in byte
	iMaxSliceNumConstraint int32             // maximal number of slices constraint
}

// SDynamicSlicingStack stores the bitstream / CABAC state to step back one MB
// in dynamic slicing.
type SDynamicSlicingStack struct {
	iStartPos   int32
	iCurrentPos int32

	// C uint8_t*: current writing position; same representation as
	// common.SBitStringAux.pCurBuf (offset into the writer's buffer).
	pBsStackBufPtr   int
	uiBsStackCurBits uint32
	iBsStackLeftBits int32

	sStoredCabac SCabacCtx

	iMbSkipRunStack int32
	uiLastMbQp      uint8
	pRestoreBuffer  []uint8 // C uint8_t*: backup of the CABAC output bytes (nil when unused)
}
