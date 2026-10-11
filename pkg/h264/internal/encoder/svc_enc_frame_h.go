// Port of codec/encoder/core/inc/svc_enc_frame.h.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

/*
 *  Frame level in SVC DQLayer instead.
 *  Dependency-Quaility layer struction definition for SVC extension of H.264/AVC
 */

// (typedef SDqLayer* pDqLayer is not ported: the name is used for variables.)

// SFeatureSearchPreparation is maintained only once per layer.
type SFeatureSearchPreparation struct {
	pRefBlockFeature       *SScreenBlockFeatureStorage // point the the ref frame storage
	pFeatureOfBlock        []uint16                    // C uint16_t*: Feature of every block (8x8), begin with the point
	uiFeatureStrategyIndex uint8                       // index of hash strategy

	/* for FME frame-level switch */
	bFMESwitchFlag      bool
	uiFMEGoodFrameCount uint8
	iHighFreMbCount     int32
}

// SSliceBufferInfo is the slice buffer of one thread.
type SSliceBufferInfo struct {
	pSliceBuffer   []SSlice // C SSlice*: slice buffer for multi thread (iMaxSliceNum entries)
	iMaxSliceNum   int32
	iCodedSliceNum int32
}

// SLayerInfo holds the NAL header and parameter sets of a layer.
type SLayerInfo struct {
	sNalHeaderExt common.SNalUnitHeaderExt
	pSubsetSpsP   *SSubsetSps // current pSubsetSps used, memory alloc in external
	pSpsP         *SWelsSPS   // current pSps based avc used, memory alloc in external
	pPpsP         *SWelsPPS   // current pPps used
}

// SDqLayer is the layer representation.
type SDqLayer struct {
	sLayerInfo SLayerInfo

	sSliceBufferInfo [MAX_THREADS_NUM]SSliceBufferInfo
	ppSliceInLayer   []*SSlice // C SSlice**: pointers into the sSliceBufferInfo[].pSliceBuffer arrays
	sSliceEncCtx     SSliceCtx // current slice context

	// C uint8_t* pCsData[3]: pointer to reconstructed picture pData;
	// (slice, offset): pCsData[i] is the whole picture allocation, iCsDataOff[i] the plane origin.
	pCsData    [3][]uint8
	iCsDataOff [3]int
	iCsStride  [3]int32 // Cs stride
	// C uint8_t* pEncData[3]: pData picture to be encoded in current layer; (slice, offset) as pCsData.
	pEncData    [3][]uint8
	iEncDataOff [3]int
	iEncStride  [3]int32 // pData picture stride

	sMbDataP  []SMB // C SMB*: the layer's MB list (iMbWidth*iMbHeight entries, indexed by iMbXY); a sub-slice of sWelsEncCtx.ppMbListD
	iMbWidth  int16 // MB width of this picture, equal to pSps.iMbWidth
	iMbHeight int16 // MB height of this picture, equal to pSps.iMbHeight;

	bBaseLayerAvailableFlag bool // whether base layer is available for prediction?
	bSatdInMdFlag           bool // whether SATD is calculated in ME and integer-pel MD

	iLoopFilterDisableIdc    uint8 // 0: on, 1: off, 2: on except for slice boundaries
	iLoopFilterAlphaC0Offset int8  // AlphaOffset: valid range [-6, 6], default 0
	iLoopFilterBetaOffset    int8  // BetaOffset:  valid range [-6, 6], default 0

	uiDisableInterLayerDeblockingFilterIdc uint8
	iInterLayerSliceAlphaC0Offset          int8
	iInterLayerSliceBetaOffset             int8
	bDeblockingParallelFlag                bool // parallel_deblocking_flag

	pRefPic *SPicture // reference picture pointer
	pDecPic *SPicture // reconstruction picture pointer for layer
	pRefOri [MAX_REF_PIC_COUNT]*SPicture

	bThreadSlcBufferFlag bool
	bSliceBsBufferFlag   bool

	iMaxSliceNum              int32
	NumSliceCodedOfPartition  [MAX_THREADS_NUM]int32 // for dynamic slicing mode
	LastCodedMbIdxOfPartition [MAX_THREADS_NUM]int32 // for dynamic slicing mode
	FirstMbIdxOfPartition     [MAX_THREADS_NUM]int32 // for dynamic slicing mode
	EndMbIdxOfPartition       [MAX_THREADS_NUM]int32 // for dynamic slicing mode
	pFirstMbIdxOfSlice        []int32                // C int32_t*: per slice (iMaxSliceNum entries)
	pCountMbNumInSlice        []int32                // C int32_t*: per slice (iMaxSliceNum entries)

	bNeedAdjustingSlicing bool

	pFeatureSearchPreparation *SFeatureSearchPreparation

	pRefLayer *SDqLayer // pointer to referencing dq_layer of current layer to be decoded
}

// SWelsSvcFrame is the frame structure for svc.
type SWelsSvcFrame = SDqLayer
