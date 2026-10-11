// Port of codec/encoder/core/inc/mb_cache.h.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

/*
 * Cache for Luma               Cache for Chroma(Cb, Cr)
 *
 *  TL T T T T                  TL T T
 *   L - - - -                   L - -
 *   L - - - -                   L - - TR
 *   L - - - -
 *   L - - - - TR
 *
 */

// g_kuiSmb4AddrIn256 and g_kuiCache12_8x8RefIdx are defined in encoder_data_tables.go.

// SDCTCoeff holds the transform coefficients of one MB.
//
// iLumaBlock and iChromaBlock are flattened (C int16_t [16][16] and [8][16])
// because the C code walks one int16_t* across the rows: block i starts at
// index i*16, so C `pDct->iLumaBlock[i]` is `pDct.iLumaBlock[i*16:]`.
type SDCTCoeff struct {
	iLumaBlock    [16 * 16]int16 // based on block4x4 luma DC/AC; C [16][16]
	iLumaI16x16Dc [16]int16      // I_16x16 DC
	iChromaBlock  [8 * 16]int16  // based on block4x4  chroma DC/AC; C [8][16]
	iChromaDc     [2][4]int16    // chroma DC
}

// SMbCachePicData holds the per-MB pointers into the pictures
// (the anonymous struct SMbCache.SPicData in C). Every pointer is a
// (slice, offset) pair: the slice is the whole picture allocation, the
// offset is the position of the current MB's top-left sample of that plane.
type SMbCachePicData struct {
	/* pointer of current mb location in original frame */
	pEncMb    [3][]uint8
	iEncMbOff [3]int
	/* pointer of current mb location in recovery frame */
	pDecMb    [3][]uint8
	iDecMbOff [3]int
	/* pointer of co-located mb location in reference frame */
	pRefMb    [3][]uint8
	iRefMbOff [3]int
	// for SVC
	pCsMb    [3][]uint8 // locating current mb's CS in whole frame
	iCsMbOff [3]int
}

// SMbCache is the MB cache information, such one cache should be defined within a slice.
type SMbCache struct {
	// the followed pData now is promised aligned to 16 bytes
	sMvComponents      SMVComponentUnit
	iNonZeroCoeffCount [48]int8 // Cache line size
	iIntraPredMode     [48]int8 // must follow with iNonZeroCoeffCount!

	iSadCost [4]int32                          // avail 1; unavail 0
	sMbMvp   [common.MB_BLOCK4x4_NUM]SMVUnitXY // for write bs

	// Scratch passed through runtime-selected function tables. Keeping it in
	// the per-slice cache avoids one heap allocation per macroblock operation.
	iDiamondSad      [4]int32
	iBestModeScratch int32
	bKeepSkipScratch bool

	// for residual decoding (recovery) at the side of Encoder
	pCoeffLevel []int16 // C int16_t*: MB_COEFF_LIST_SIZE entries (temp)

	// malloc memory for prediction
	pSkipMb []uint8 // C uint8_t*: 384 bytes (Y 16x16, Cb 8x8 at 256, Cr 8x8 at 320)

	// The following prediction pointers are plain sub-slices of their
	// allocations (only indexed non-negatively); passing them to a pixel
	// function means (slice, 0).
	pMemPredMb           []uint8 // C uint8_t*: 2*256 bytes allocation
	pMemPredLuma         []uint8 // sub-slice of pMemPredMb; inter && intra share same pointer
	pMemPredChroma       []uint8 // sub-slice of pMemPredMb; inter && intra share same pointer
	pBestPredIntraChroma []uint8 // sub-slice of pMemPredMb; Cb:0~63;   Cr:64~127
	pMemPredBlk4         []uint8 // C uint8_t*: 2*16 bytes allocation
	pBestPredI4x4Blk4    []uint8 // sub-slice of pMemPredBlk4; I_4x4
	pBufferInterPredMe   []uint8 // C uint8_t*: 4*640 bytes allocation; inter type pBuffer for ME h & v & hv

	// no scan4[] order, just as memory order to store
	pPrevIntra4x4PredModeFlag []bool // C bool*: 16 entries; if 1, means no rem_intra4x4_pred_mode; if 0, means rem_intra4x4_pred_mode != 0
	pRemIntra4x4PredModeFlag  []int8 // C int8_t*: 16 entries; -1 as default

	iSadCostSkip [4]int32 // avail 1; unavail 0
	bMbTypeSkip  [4]bool  // 1: skip; 0: non-skip
	// C int32_t* pEncSad = &pDecPic->pMbSkipSad[iMbXY]: indexed negatively
	// (pEncSad[-1], pEncSad[-iMbWidth], ...) -> (slice, offset) pair.
	pEncSad    []int32
	iEncSadOff int

	// for residual encoding at the side of Encoder
	pDct *SDCTCoeff

	uiNeighborIntra     uint8 // LEFT_MB_POS:0x01, TOP_MB_POS:0x02, TOPLEFT_MB_POS = 0x04 ,TOPRIGHT_MB_POS = 0x08;
	uiLumaI16x16Mode    uint8
	uiChmaI8x8Mode      uint8
	bCollocatedPredFlag bool // denote if current MB is collocated predicted (MV==0).
	uiRefMbType         uint32

	SPicData SMbCachePicData
}
