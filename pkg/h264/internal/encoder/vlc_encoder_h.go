// Port of codec/encoder/core/inc/vlc_encoder.h.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

/************************************************************************/
/* VLC FOR WELS ENCODER                                                 */
/************************************************************************/

// g_kuiVlcCoeffToken, g_kuiVlcTotalZeros, g_kuiVlcTotalZerosChromaDc,
// g_kuiVlcRunBefore and g_kuiEncNcMapTable are defined in
// encoder_data_tables.go. (g_kuiVlcLevelPrefix and
// g_kuiVlcTotalZerosChromaDc422 are declared but never defined in C.)

const CHROMA_DC_NC_OFFSET = 17

func WriteTotalCoeffTrailingones(pBs *common.SBitStringAux, uiNc uint8, uiTotalCoeff uint8,
	uiTrailingOnes uint8) int32 {
	kuiNcIdx := g_kuiEncNcMapTable[uiNc]
	kpCoeffToken := &g_kuiVlcCoeffToken[kuiNcIdx][uiTotalCoeff][uiTrailingOnes]
	return common.BsWriteBits(pBs, int32(kpCoeffToken[1]), uint32(kpCoeffToken[0]))
}

func WriteTotalcoeffTrailingonesChroma(pBs *common.SBitStringAux, uiTotalCoeff uint8,
	uiTrailingOnes uint8) int32 {
	kpCoeffToken := &g_kuiVlcCoeffToken[4][uiTotalCoeff][uiTrailingOnes]
	return common.BsWriteBits(pBs, int32(kpCoeffToken[1]), uint32(kpCoeffToken[0]))
}

// WriteLevelPrefix: kuiZeroCount = level_prefix;
func WriteLevelPrefix(pBs *common.SBitStringAux, kuiZeroCount uint32) int32 {
	common.BsWriteBits(pBs, int32(kuiZeroCount+1), 1)
	return 0
}

func WriteTotalZeros(pBs *common.SBitStringAux, uiTotalCoeff uint32, uiTotalZeros uint32) int32 {
	kpTotalZeros := &g_kuiVlcTotalZeros[uiTotalCoeff][uiTotalZeros]
	return common.BsWriteBits(pBs, int32(kpTotalZeros[1]), uint32(kpTotalZeros[0]))
}

func WriteTotalZerosChromaDc(pBs *common.SBitStringAux, uiTotalCoeff uint32, uiTotalZeros uint32) int32 {
	kpTotalZerosChromaDc := &g_kuiVlcTotalZerosChromaDc[uiTotalCoeff][uiTotalZeros]
	return common.BsWriteBits(pBs, int32(kpTotalZerosChromaDc[1]), uint32(kpTotalZerosChromaDc[0]))
}

func WriteRunBefore(pBs *common.SBitStringAux, uiZeroLeft uint8, uiRunBefore uint8) int32 {
	kpRunBefore := &g_kuiVlcRunBefore[uiZeroLeft][uiRunBefore]
	return common.BsWriteBits(pBs, int32(kpRunBefore[1]), uint32(kpRunBefore[0]))
}
