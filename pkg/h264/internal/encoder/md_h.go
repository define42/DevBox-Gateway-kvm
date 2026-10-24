// Port of codec/encoder/core/inc/md.h.

package encoder

const (
	ME_REFINE_BUF_STRIDE      = 32
	ME_REFINE_BUF_WIDTH_BLK4  = 8
	ME_REFINE_BUF_WIDTH_BLK8  = 16
	ME_REFINE_BUF_STRIDE_BLK4 = 160
	ME_REFINE_BUF_STRIDE_BLK8 = 320

	REFINE_ME_NO_BEST_HALF_PIXEL = 0 // ( 0,  0)
	REFINE_ME_HALF_PIXEL_LEFT    = 3 // (-2,  0)
	REFINE_ME_HALF_PIXEL_RIGHT   = 4 // ( 2,  0)
	REFINE_ME_HALF_PIXEL_TOP     = 1 // ( 0, -2)
	REFINE_ME_HALF_PIXEL_BOTTOM  = 2 // ( 0,  2)

	ME_NO_BEST_QUAR_PIXEL = 1 // ( 0,  0) or best half pixel
	ME_QUAR_PIXEL_LEFT    = 2 // (-1,  0)
	ME_QUAR_PIXEL_RIGHT   = 3 // ( 1,  0)
	ME_QUAR_PIXEL_TOP     = 4 // ( 0, -1)
	ME_QUAR_PIXEL_BOTTOM  = 5 // ( 0,  1)
	NO_BEST_FRAC_PIX      = 1 // REFINE_ME_NO_BEST_HALF_PIXEL + ME_NO_BEST_QUAR_PIXEL

	// for vaa constants
	MBVAASIGN_FLAT  = 15
	MBVAASIGN_HOR1  = 3
	MBVAASIGN_HOR2  = 12
	MBVAASIGN_VER1  = 5
	MBVAASIGN_VER2  = 10
	MBVAASIGN_CMPX1 = 6
	MBVAASIGN_CMPX2 = 9
)

// g_kiQpCostTable, g_kiMapModeI16x16 and g_kiMapModeIntraChroma are defined
// in encoder_data_tables.go.

// SWelsMDMe holds the per partition ME states (anonymous struct SWelsMD.sMe in C).
// NO B frame in our Wels, we can ignore list1.
type SWelsMDMe struct {
	sMe16x16 SWelsME // adjust each SWelsME for 8 D-word!
	sMe8x8   [4]SWelsME
	sMe16x8  [2]SWelsME
	sMe8x16  [2]SWelsME
	sMe4x4   [4][4]SWelsME
	sMe8x4   [4][2]SWelsME
	sMe4x8   [4][2]SWelsME
}

// SWelsMD is the mode decision state of one MB.
type SWelsMD struct {
	iLambda int32
	// C uint16_t* pMvdCost: negatively indexed -> (slice, offset); pMvdCost is the
	// whole sWelsEncCtx.pMvdCostTable, iMvdCostOff the table centre for the MB's QP.
	pMvdCost    []uint16
	iMvdCostOff int

	iCostLuma   int32
	iCostChroma int32 // satd+lambda(best_pred_mode) //i_sad_chroma;
	iSadPredMb  int32

	uiRef       uint8 // uiRefIndex appointed by Encoder, used for MC
	bMdUsingSad bool
	uiReserved  uint16

	iCostSkipMb  int32
	iSadPredSkip int32

	iMbPixX int32 // pixel position of MB in horizontal axis
	iMbPixY int32 // pixel position of MB in vertical axis

	iBlock8x8StaticIdc [4]int32

	sMe SWelsMDMe
}

const refineRegion = 640

// SMeRefinePointer holds the fractional ME refinement buffers. All pointers
// are sub-slices of SMbCache.pBufferInterPredMe (only indexed non-negatively).
type SMeRefinePointer struct {
	// Record the allocation, with tracked pointers, to prove scratch regions
	// disjoint without comparing every input/output pair at each candidate.
	arena        []byte
	arenaOff     int
	pHalfPixH    []uint8
	pHalfPixV    []uint8
	pHalfPixHV   []uint8
	pQuarPixBest []uint8
	pQuarPixTmp  []uint8

	pfCopyBlockByMode PCopyFunc
}
