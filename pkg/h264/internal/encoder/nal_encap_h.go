// Port of codec/encoder/core/inc/nal_encap.h.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

const NAL_HEADER_SIZE = 4

// SWelsNalRaw is the raw payload pData for NAL unit, AVC/SVC compatible.
type SWelsNalRaw struct {
	// C uint8_t*: &pBsBuffer[iStartPos] -> sub-slice pBsBuffer[iStartPos:]
	// of the owning bitstream buffer (pRawNal payload for slice pData).
	pRawData     []uint8
	iPayloadSize int32                    // size of pRawNal pData
	sNalExt      common.SNalUnitHeaderExt // NAL header information
	iStartPos    int32                    // NAL start position in buffer
}

// SWelsEncoderOutput is the encoder major output pData.
type SWelsEncoderOutput struct {
	pBsBuffer []uint8              // C uint8_t*: overall bitstream pBuffer allocation for a coded picture, recycling use intend.
	uiSize    uint32               // size of allocation pBuffer above
	sBsWrite  common.SBitStringAux // writer over pBsBuffer

	sNalList   []SWelsNalRaw // C SWelsNalRaw*: nal list, adaptive for AVC/SVC in case single slice, multiple slices or fmo
	pNalLen    []int32       // C int32_t*: one entry per NAL of sNalList
	iCountNals int32         // count number of NAL in list
	// SVC: num_sps (MAX_D) + num_pps (MAX_D) + num_vcl (MAX_D * MAX_Q)
	iNalIndex     int32 // coding NAL currently, 0 based
	iLayerBsIndex int32 // layer index of  bit stream for SFrameBsIfo
}

// SWelsSliceBs holds the per-slice bitstream.
type SWelsSliceBs struct {
	pBs       []uint8              // C uint8_t*: output bitstream (the slice's encapsulated NALs)
	uiBsSize  uint32               // size of pBs buffer
	uiBsPos   uint32               // position of output bitstream
	pBsBuffer []uint8              // C uint8_t*: overall bitstream pBuffer allocation for a coded slice, recycling use intend.
	uiSize    uint32               // size of allocation pBuffer above
	sBsWrite  common.SBitStringAux // writer over pBsBuffer
	sNalList  [2]SWelsNalRaw       // nal list, PREFIX NAL(if applicable) + SLICE NAL
	iNalLen   [2]int32
	iNalIndex int32 // coding NAL currently, 0 based
}
