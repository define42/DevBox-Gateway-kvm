// Compile-time checks that the functions installed into the decoder's
// function-pointer tables (InitPredFunc, InitDecFuncs, DeblockingInit,
// WelsBlockFuncInit, InitErrorCon, ...) match the function types declared in
// the *_h.go files. Not a port of any C file.

package decoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

var (
	_ PGetIntraPredFunc    = WelsI16x16LumaPredV_c
	_ PGetIntraPredFunc    = WelsI4x4LumaPredV_c
	_ PGetIntraPredFunc    = WelsIChromaPredV_c
	_ PGetIntraPred8x8Func = WelsI8x8LumaPredV_c
	_ PIdctResAddPredFunc  = IdctResAddPred_c
	_ PIdctResAddPredFunc  = IdctResAddPred8x8_c
	_ PExpandPictureFunc   = common.ExpandPictureLuma_c

	_ PCopyFunc = common.WelsCopy16x16_c
	_ PCopyFunc = common.WelsCopy8x8_c

	_ PLumaDeblockingLT4Func    = common.DeblockLumaLt4V_c
	_ PLumaDeblockingEQ4Func    = common.DeblockLumaEq4V_c
	_ PChromaDeblockingLT4Func  = common.DeblockChromaLt4V_c
	_ PChromaDeblockingEQ4Func  = common.DeblockChromaEq4V_c
	_ PChromaDeblockingLT4Func2 = common.DeblockChromaLt4V2_c
	_ PChromaDeblockingEQ4Func2 = common.DeblockChromaEq4V2_c

	_ PWelsNonZeroCountFunc = common.WelsNonZeroCount_c
	_ PWelsBlockZeroFunc    = WelsBlockZero16x16_c
	_ PWelsBlockZeroFunc    = WelsBlockZero8x8_c

	_ PWelsFillNeighborMbInfoIntra4x4Func = WelsFillCacheConstrain0IntraNxN
	_ PWelsFillNeighborMbInfoIntra4x4Func = WelsFillCacheConstrain1IntraNxN
	_ PWelsMapNeighToSample               = WelsMapNxNNeighToSampleNormal
	_ PWelsMapNeighToSample               = WelsMapNxNNeighToSampleConstrain1
	_ PWelsMap16NeighToSample             = WelsMap16x16NeighToSampleNormal
	_ PWelsMap16NeighToSample             = WelsMap16x16NeighToSampleConstrain1

	_ PDeblockingFilterMbFunc = WelsDeblockingMb
	_ PWelsDecMbFunc          = WelsDecodeMbCavlcISlice
	_ PWelsDecMbFunc          = WelsDecodeMbCabacBSlice
)
