// Port of codec/decoder/core/inc/dec_frame.h.

package decoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

///////////////////////////////////DQ Layer level///////////////////////////////////

type SLayerInfo struct {
	sNalHeaderExt common.SNalUnitHeaderExt
	sSliceInLayer SSlice      // Here Slice identify to Frame on concept
	pSubsetSps    *SSubsetSps // current pSubsetSps used, memory alloc in external
	pSps          *SSps       // current sps based avc used, memory alloc in external
	pPps          *SPps       // current pps used
}

type PLayerInfo = *SLayerInfo

/* Layer Representation */

// SDqLayer (struct TagDqLayer).
//
// All per-MB arrays are slices indexed by MB address (iMbXyIndex); they
// alias the arrays allocated in SWelsDecoderContext.sMb (or, for some, the
// current picture), exactly like the C pointers.
type SDqLayer struct {
	sLayerInfo SLayerInfo

	pBitStringAux                   *common.SBitStringAux // pointer to SBitStringAux (the current NAL's sSliceBitsRead)
	pFmo                            *SFmo                 // Current fmo context pointer used
	pMbType                         []uint32
	pSliceIdc                       []int32 // using int32_t for slice_idc
	pMv                             [common.LIST_A][][common.MB_BLOCK4x4_NUM][common.MV_A]int16
	pMvd                            [common.LIST_A][][common.MB_BLOCK4x4_NUM][common.MV_A]int16
	pRefIndex                       [common.LIST_A][][common.MB_BLOCK4x4_NUM]int8
	pDirect                         [][common.MB_BLOCK4x4_NUM]int8
	pNoSubMbPartSizeLessThan8x8Flag []bool
	pTransformSize8x8Flag           []bool
	pLumaQp                         []int8
	pChromaQp                       [][2]int8
	pCbp                            []int8
	pCbfDc                          []uint16
	pNzc                            [][24]int8
	pNzcRs                          [][24]int8
	pResidualPredFlag               []int8
	pInterPredictionDoneFlag        []int8
	pMbCorrectlyDecodedFlag         []bool
	pMbRefConcealedFlag             []bool
	pScaledTCoeff                   [][common.MB_COEFF_LIST_SIZE]int16
	pIntraPredMode                  [][8]int8 //0~3 top4x4 ; 4~6 left 4x4; 7 intra16x16
	pIntra4x4FinalMode              [][common.MB_BLOCK4x4_NUM]int8
	pIntraNxNAvailFlag              []uint8
	pChromaPredMode                 []int8
	//uint8_t (*motion_pred_flag[LIST_A])[MB_PARTITION_SIZE]; // 8x8
	pSubMbType    [][MB_SUB_PARTITION_SIZE]uint32
	iLumaStride   int32
	iChromaStride int32
	// pPred[i]: (slice, offset) pair - pPred[i] is the whole allocation of the
	// picture being reconstructed (pDec.pData[i]) and iPredOff[i] the offset of
	// the current MB's top-left sample of plane i inside it.
	pPred      [3][]uint8
	iPredOff   [3]int
	iMbX       int32
	iMbY       int32
	iMbXyIndex int32
	iMbWidth   int32 // MB width of this picture, equal to sSps.iMbWidth
	iMbHeight  int32 // MB height of this picture, equal to sSps.iMbHeight;

	/* Common syntax elements across all slices of a DQLayer */
	iSliceIdcBackup                        int32
	uiSpsId                                uint32
	uiPpsId                                uint32
	uiDisableInterLayerDeblockingFilterIdc uint32
	iInterLayerSliceAlphaC0Offset          int32
	iInterLayerSliceBetaOffset             int32
	//SPosOffset              sScaledRefLayer;
	iSliceGroupChangeCycle int32

	pRefPicListReordering *SRefPicListReorderSyn
	pPredWeightTable      *SPredWeightTabSyn
	pRefPicMarking        *SRefPicMarking // Decoded reference picture marking syntaxs
	pRefPicBaseMarking    *SRefBasePicMarking

	pRef *SPicture // reference picture pointer
	pDec *SPicture // reconstruction picture pointer for layer

	iColocMv       [2][16][2]int16 //Colocated MV cache
	iColocRefIndex [2][16]int8     //Colocated RefIndex cache
	iColocIntra    [16]int8        //Colocated Intra cache

	bUseWeightPredictionFlag        bool
	bUseWeightedBiPredIdc           bool
	bStoreRefBasePicFlag            bool // iCurTid == 0 && iCurQid = 0 && bEncodeKeyPic = 1
	bTCoeffLevelPredFlag            bool
	bConstrainedIntraResamplingFlag bool
	uiRefLayerDqId                  uint8
	uiRefLayerChromaPhaseXPlus1Flag uint8
	uiRefLayerChromaPhaseYPlus1     uint8
	uiLayerDqId                     uint8 // dq_id of current layer
	bUseRefBasePicFlag              bool  // whether reference pic or reference base pic is referred?
}

type PDqLayer = *SDqLayer

// SGpuAvcDqLayer (TagGpuAvcLayer). Unused by the decoder; kept for fidelity.
type SGpuAvcDqLayer struct {
	sLayerInfo    SLayerInfo
	pBitStringAux *common.SBitStringAux // pointer to SBitStringAux

	pMbType        []uint32
	pSliceIdc      []int32 // using int32_t for slice_idc
	pLumaQp        []int8
	pCbp           []int8
	pNzc           [][24]int8
	pIntraPredMode [][8]int8 //0~3 top4x4 ; 4~6 left 4x4; 7 intra16x16
	iMbX           int32
	iMbY           int32
	iMbXyIndex     int32
	iMbWidth       int32 // MB width of this picture, equal to sSps.iMbWidth
	iMbHeight      int32 // MB height of this picture, equal to sSps.iMbHeight;
}

type PGpuAvcDqLayer = *SGpuAvcDqLayer
