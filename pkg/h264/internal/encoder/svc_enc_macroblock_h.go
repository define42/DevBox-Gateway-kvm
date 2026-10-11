// Port of codec/encoder/core/inc/svc_enc_macroblock.h.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

// SMB is the MB syntax and context, refer to Page 399 in JVT X201wcm.
type SMB struct {
	/*************************mb_layer() syntax and generated********************************/
	/*mb_layer():*/
	uiMbType        Mb_Type  // including MB detailed partition type, number and type of reference list
	uiSubMbType     [4]uint8 // sub MB types
	iMbXY           int32    // offset position of MB top left point based
	iMbX            int16    // position of MB in horizontal axis [0..32767]
	iMbY            int16    // position of MB in vertical axis [0..32767]
	uiNeighborAvail uint8    // avail && same_slice: LEFT_MB_POS:0x01, TOP_MB_POS:0x02, TOPRIGHT_MB_POS = 0x04 ,TOPLEFT_MB_POS = 0x08;
	uiCbp           uint8

	sMv               []SMVUnitXY // C SMVUnitXY*: the MB's MB_BLOCK4x4_NUM entries of sWelsEncCtx.pMvUnitBlock4x4
	pRefIndex         []int8      // C int8_t*: the MB's MB_BLOCK8x8_NUM entries of sWelsEncCtx.pRefIndexBlock4x4
	pSadCost          *int32      // C int32_t*: &sWelsEncCtx.pSadCostMb[iMbXY]; mb sad. set to 0 for intra mb
	pIntra4x4PredMode []int8      // C int8_t*: [INTRA_4x4_MODE_NUM] entries of sWelsEncCtx.pIntra4x4PredModeBlocks
	pNonZeroCount     []int8      // C int8_t*: [MB_LUMA_CHROMA_BLOCK4x4_NUM] entries of sWelsEncCtx.pNonZeroCountBlocks

	sP16x16Mv       SMVUnitXY
	uiLumaQp        uint8 // uiLumaQp: pPps->iInitialQp + sSliceHeader->delta_qp + mb->dquant.
	uiChromaQp      uint8
	uiSliceIdc      uint16 // 2^16=65536 > MaxFS(36864) of level 5.1
	uiChromPredMode uint32
	iLumaDQp        int32
	sMvd            [common.MB_BLOCK4x4_NUM]SMVUnitXY // only for CABAC writing; storage structure the same as sMv, in 4x4 scan order.
	iCbpDc          int32

	// Go-only: the MB list of the layer this MB belongs to (SDqLayer.sMbDataP),
	// indexed by iMbXY. Set together with sMv & co. in InitMbInfo. Used to
	// express the C pointer arithmetic pCurMb + n (see Add).
	pMbList []SMB
}

// PMb is a pointer alias.
type PMb = *SMB

// Add returns the MB at pointer distance n from p in its MB list:
// C `pCurMb + n` (n may be negative, e.g. pCurMb - 1, pCurMb - iMbWidth).
func (p *SMB) Add(n int32) *SMB {
	return &p.pMbList[int(p.iMbXY)+int(n)]
}
