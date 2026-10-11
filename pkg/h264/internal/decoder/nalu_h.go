// Port of codec/decoder/core/inc/nalu.h.

package decoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

///////////////////////////////////NAL UNIT level///////////////////////////////////

// SVclNal is the VCL member of the TagNalUnit.sNalData union.
type SVclNal struct {
	sSliceHeaderExt SSliceHeaderExt
	// sSliceBitsRead.PBuf is pCtx.sRawData.pHead (the whole raw buffer) and its
	// offsets are relative to it, so buffer reallocation only swaps PBuf.
	sSliceBitsRead common.SBitStringAux
	// pNalPos: save the address of slice nal for GPU function (parse only).
	// (slice, offset) pair: pNalPos is the whole pCtx.sSavedData.pHead
	// allocation (nil == NULL), iNalPosOff the position inside it.
	pNalPos             []uint8
	iNalPosOff          int
	iNalLength          int32 // save the nal length for GPU function
	bSliceHeaderExtFlag bool
}

// SNalData is the TagNalUnit.sNalData union. Go has no unions: both members
// are stored side by side; the decoder only ever reads the member it wrote
// (and memset-ing the whole SNalUnit maps to assigning SNalUnit{}).
type SNalData struct {
	sVclNal    SVclNal
	sPrefixNal SPrefixNalUnit
}

/* NAL Unit Structure */
type SNalUnit struct {
	sNalHeaderExt common.SNalUnitHeaderExt

	sNalData    SNalData
	uiTimeStamp uint64
}

type PNalUnit = *SNalUnit

///////////////////////////////////ACCESS Unit level///////////////////////////////////

/* Access Unit structure */
type SAccessUnit struct {
	pNalUnitsList    []*SNalUnit // list of NAL Units pointer in this AU (len == uiCountUnitsNum)
	uiAvailUnitsNum  uint32      // Number of NAL Units available in each AU list based current bitstream,
	uiActualUnitsNum uint32      // actual number of NAL units belong to current au
	// While available number exceeds count size below, need realloc extra NAL Units for list space.
	uiCountUnitsNum  uint32 // Count size number of malloced NAL Units in each AU list
	uiStartPos       uint32
	uiEndPos         uint32
	bCompletedAuFlag bool // Indicate whether it is a completed AU
}

type PAccessUnit = *SAccessUnit
