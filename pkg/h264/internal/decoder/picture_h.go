// Port of codec/decoder/core/inc/picture.h.

package decoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

/*
*  Reconstructed Picture definition
*  It is used to express reference picture, also consequent reconstruction picture for output
 */

// SPicture ("Picture" declaration is comflict with Mac system).
type SPicture struct {
	/************************************payload data*********************************/
	// pBuffer[i]: the whole picture allocation (luma + Cb + Cr in one buffer,
	// C: pointer to the first allocated byte of plane i). pBuffer[0..2] all
	// hold the same slice; nil in parse-only mode. pBuffer[3] is unused.
	pBuffer [4][]uint8
	// pData[i]: (slice, offset) pair - pData[i] holds the same whole
	// allocation as pBuffer[i] and iDataOff[i] is the offset of plane i's
	// origin (top-left visible sample) inside it, so pData[i][iDataOff[i]+k]
	// is C's pData[i][k] (k may be negative to reach the padding).
	pData     [4][]uint8
	iDataOff  [4]int
	iLinesize [4]int32 // linesize of picture planes respectively used currently
	iPlanes   int32    // How many planes are introduced due to color space format?
	// picture information

	/*******************************from EC mv copy****************************/
	bIdrFlag bool

	/*******************************from other standard syntax****************************/
	/*from sps*/
	iWidthInPixel  int32 // picture width in pixel
	iHeightInPixel int32 // picture height in pixel
	/*from slice header*/
	iFramePoc int32 // frame POC

	/*******************************sef_definition for misc use****************************/
	bUsedAsRef bool                 // for ref pic management
	bIsLongRef bool                 // long term reference frame flag       //for ref pic management
	iRefCount  int8                 //
	pSetUnRef  func(pRef *SPicture) // C: void (*pSetUnRef)(SPicture*)

	bIsComplete bool // indicate whether current picture is complete, not from EC
	/*******************************for future use****************************/
	uiTemporalId uint8
	uiSpatialId  uint8
	uiQualityId  uint8

	iFrameNum         int32  // frame number                 //for ref pic management
	iFrameWrapNum     int32  // frame wrap number            //for ref pic management
	iLongTermFrameIdx int32  //id for long term ref pic
	uiLongTermPicNum  uint32 //long_term_pic_num

	iSpsId                 int32 //against mosaic caused by cross-IDR interval reference.
	iPpsId                 int32
	uiTimeStamp            uint64
	uiDecodingTimeStamp    uint32 //represent relative decoding time stamps
	iPicBuffIdx            int32
	eSliceType             common.EWelsSliceType
	bIsUngroupedMultiSlice bool //multi-slice picture with each each slice group contains one slice.
	bNewSeqBegin           bool
	iMbEcedNum             int32
	iMbEcedPropNum         int32
	iMbNum                 int32

	// Per-MB arrays, indexed by MB (len == MB count).
	pMbCorrectlyDecodedFlag []bool
	pNzc                    [][24]int8                                                  // nil unless multi-threaded (always nil in this port)
	pMbType                 []uint32                                                    // mb type used for direct mode
	pMv                     [common.LIST_A][][common.MB_BLOCK4x4_NUM][common.MV_A]int16 // used for direct mode
	pRefIndex               [common.LIST_A][][common.MB_BLOCK4x4_NUM]int8               //used for direct mode
	pRefPic                 [common.LIST_A][17]*SPicture                                //ref pictures used for direct mode
	pReadyEvent             []SWelsDecEvent                                             //MB line ready event (threading only: always nil)
}

type PPicture = *SPicture
