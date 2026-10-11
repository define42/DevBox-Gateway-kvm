// Port of codec/encoder/core/inc/svc_motion_estimate.h.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

const (
	CAMERA_STARTMV_RANGE       = 64
	ITERATIVE_TIMES            = 16
	CAMERA_MV_RANGE            = CAMERA_STARTMV_RANGE + ITERATIVE_TIMES
	CAMERA_MVD_RANGE           = (CAMERA_MV_RANGE + 1) << 1 // mvd=mv_range*2;
	BASE_MV_MB_NMB             = (2 * CAMERA_MV_RANGE / common.MB_WIDTH_LUMA) - 1
	CAMERA_HIGHLAYER_MVD_RANGE = 243 // mvd range;
	EXPANDED_MV_RANGE          = 504 // =512-8 rather than 511 to sacrifice same edge point but save complexity in assemblys
	EXPANDED_MVD_RANGE         = (504 + 1) << 1
)

const (
	ME_DIA   = 0x01 // LITTLE DIAMOND= 0x01
	ME_CROSS = 0x02 // CROSS=  0x02
	ME_FME   = 0x04 // FME = 0x04
	ME_FULL  = 0x10 // FULL

	// derived ME methods combination
	ME_DIA_CROSS     = ME_DIA | ME_CROSS     // DIA+CROSS
	ME_DIA_CROSS_FME = ME_DIA_CROSS | ME_FME // DIA+CROSS+FME
)

// SWelsME holds the motion estimation state of one block.
//
// The C union SadPredISatdUnit {uint32_t uiSadPred; uint32_t uiSatd;} is
// the single field uSadPredISatd: both C members
// (pMe->uSadPredISatd.uiSadPred / .uiSatd) map to pMe.uSadPredISatd.
type SWelsME struct {
	/* input */
	// C uint16_t* pMvdCost: indexed with negative MVD values -> (slice, offset);
	// pMvdCost is the whole sWelsEncCtx.pMvdCostTable, iMvdCostOff the table centre for the MB's QP.
	pMvdCost    []uint16
	iMvdCostOff int

	uSadPredISatd       uint32 // reuse the sad_pred as a temp pData (union uiSadPred / uiSatd)
	uiSadCost           uint32 // used by ME and RC //max SAD should be max_delta*size+lambda*mvdsize = 255*256+91*33*2 = 65280 + 6006 = 71286 > (2^16)-1 = 65535
	uiSatdCost          uint32 /* satd + lm * nbits */
	uiSadCostThreshold  uint32
	iCurMeBlockPixX     int32
	iCurMeBlockPixY     int32
	uiBlockSize         uint8 /* BLOCK_WxH */
	uiReserved          uint8
	iDirectionalSadCost int32 // scratch for runtime-selected directional-MV checks

	// C uint8_t* pEncMb / pRefMb / pColoRefMb: pixel pointers -> (slice, offset).
	// pEncMb: source picture; pRefMb/pColoRefMb: reference picture (pRefMb may also
	// point into a refinement buffer after fractional ME).
	pEncMb        []uint8
	iEncMbOff     int
	pRefMb        []uint8
	iRefMbOff     int
	pColoRefMb    []uint8
	iColoRefMbOff int

	sMvp           SMVUnitXY
	sMvBase        SMVUnitXY
	sDirectionalMv SMVUnitXY

	pRefFeatureStorage *SScreenBlockFeatureStorage

	/* output */
	sMv SMVUnitXY
}

// SFeatureSearchIn is the input of the feature (FME) search.
type SFeatureSearchIn struct {
	pSad                   PSampleSadSatdCostFunc
	pTimesOfFeature        []uint32   // C uint32_t*: SScreenBlockFeatureStorage.pTimesOfFeatureValue
	pQpelLocationOfFeature [][]uint16 // C uint16_t**: SScreenBlockFeatureStorage.pLocationOfFeature
	// C uint16_t* pMvdCostX / pMvdCostY: negatively indexed -> (slice, offset)
	pMvdCostX    []uint16
	iMvdCostXOff int
	pMvdCostY    []uint16
	iMvdCostYOff int
	// C uint8_t* pEnc / pColoRef: pixel pointers -> (slice, offset)
	pEnc        []uint8
	iEncOff     int
	pColoRef    []uint8
	iColoRefOff int

	iEncStride        int32
	iRefStride        int32
	uiSadCostThresh   uint16
	iFeatureOfCurrent int32

	iCurPixX     int32
	iCurPixY     int32
	iCurPixXQpel int32
	iCurPixYQpel int32

	iMinQpelX int32
	iMinQpelY int32
	iMaxQpelX int32
	iMaxQpelY int32
}

// SFeatureSearchOut is the output of the feature (FME) search.
type SFeatureSearchOut struct {
	sBestMv       SMVUnitXY
	uiBestSadCost uint32
	// C uint8_t* pBestRef: pixel pointer -> (slice, offset)
	pBestRef    []uint8
	iBestRefOff int
}

// COST_MVD is (table[mx] + table[my]) on a (slice, offset) MVD cost table.
func COST_MVD(table []uint16, tableOff int, mx, my int32) int32 {
	return int32(table[tableOff+int(mx)]) + int32(table[tableOff+int(my)])
}

// QStepx16ByQp is file-local data of svc_motion_estimate.cpp (defined there).

// Feature Search Basics
const (
	LIST_SIZE_SUM_16x16             = 0x0FF01 // (256*255+1)
	LIST_SIZE_SUM_8x8               = 0x03FC1 // (64*255+1)
	LIST_SIZE_MSE_16x16             = 0x00878 // (avg+mse)/2, max= (255+16*255)/2
	FME_DEFAULT_FEATURE_INDEX       = 0
	FMESWITCH_DEFAULT_GOODFRAME_NUM = 2
	FMESWITCH_MBSAD_THRESHOLD       = 30 // empirically set.
)

// SetMvWithinIntegerMvRange computes the integer MV search window of an MB.
func SetMvWithinIntegerMvRange(kiMbWidth, kiMbHeight, kiMbX, kiMbY int32, kiMaxMvRange int32,
	pMvMin *SMVUnitXY, pMvMax *SMVUnitXY) {
	pMvMin.iMvX = int16(common.WELS_MAX(-1*((kiMbX+1)*(1<<4))+INTPEL_NEEDED_MARGIN, -1*kiMaxMvRange))
	pMvMin.iMvY = int16(common.WELS_MAX(-1*((kiMbY+1)*(1<<4))+INTPEL_NEEDED_MARGIN, -1*kiMaxMvRange))
	pMvMax.iMvX = int16(common.WELS_MIN(((kiMbWidth-kiMbX)*(1<<4))-INTPEL_NEEDED_MARGIN, kiMaxMvRange))
	pMvMax.iMvY = int16(common.WELS_MIN(((kiMbHeight-kiMbY)*(1<<4))-INTPEL_NEEDED_MARGIN, kiMaxMvRange))
}

// CheckMvInRange reports whether ksCurrentMv lies in [ksMinMv, ksMaxMv).
func CheckMvInRange(ksCurrentMv, ksMinMv, ksMaxMv SMVUnitXY) bool {
	return common.CheckInRangeCloseOpen(ksCurrentMv.iMvX, ksMinMv.iMvX, ksMaxMv.iMvX) &&
		common.CheckInRangeCloseOpen(ksCurrentMv.iMvY, ksMinMv.iMvY, ksMaxMv.iMvY)
}

// CalcFMESwitchFlag is the FME switch decision.
func CalcFMESwitchFlag(uiFMEGoodFrameCount uint8, iHighFreMbPrecentage int32,
	iAvgMbSAD int32, bScrollingDetected bool) bool {
	return bScrollingDetected || (uiFMEGoodFrameCount > 0 && iAvgMbSAD > FMESWITCH_MBSAD_THRESHOLD)
	// TODO: add the logic of iHighFreMbPrecentage
}
