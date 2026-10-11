package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

// Compile-time checks that the functions stored in the function pointer
// tables (SWelsFuncPtrList & co.) have exactly the declared Go types. They
// keep the stubs and the later ports consistent with the shared contract.
var (
	// get_intra_predictor
	_ PGetIntraPredFunc = WelsI4x4LumaPredV_c
	_ PGetIntraPredFunc = WelsIChromaPredDc_c
	_ PGetIntraPredFunc = WelsI16x16LumaPredPlane_c
	_ PGetIntraPredFunc = common.WelsI16x16LumaPredV_c
	_ PGetIntraPredFunc = common.WelsI16x16LumaPredH_c

	// sample / sad
	_ PSampleSadSatdCostFunc       = WelsSampleSatd4x4_c
	_ PSampleSadSatdCostFunc       = common.WelsSampleSad16x16_c
	_ PSample4SadCostFunc          = common.WelsSampleSadFour16x16_c
	_ PIntraPred4x4Combined3Func   = WelsSampleSatdIntra4x4Combined3_c
	_ PIntraPred16x16Combined3Func = WelsSampleSatdIntra16x16Combined3_c
	_ PIntraPred16x16Combined3Func = WelsSampleSadIntra16x16Combined3_c
	_ PIntraPred8x8Combined3Func   = WelsSampleSatdIntra8x8Combined3_c
	_ PIntraPred8x8Combined3Func   = WelsSampleSadIntra8x8Combined3_c

	// copy / dct / quant / reconstruction
	_ PCopyFunc                   = common.WelsCopy16x16_c
	_ PDctFunc                    = WelsDctT4_c
	_ PDctFunc                    = WelsDctFourT4_c
	_ PIDctFunc                   = WelsIDctT4Rec_c
	_ PIDctFunc                   = WelsIDctFourT4Rec_c
	_ PIDctFunc                   = WelsIDctRecI16x16Dc_c
	_ PDeQuantizationFunc         = WelsDequant4x4_c
	_ PDeQuantizationFunc         = WelsDequantFour4x4_c
	_ PDeQuantizationHadamardFunc = WelsDequantIHadamard4x4_c
	_ PQuantizationFunc           = WelsQuant4x4_c
	_ PQuantizationFunc           = WelsQuantFour4x4_c
	_ PQuantizationDcFunc         = WelsQuant4x4Dc_c
	_ PQuantizationMaxFunc        = WelsQuantFour4x4Max_c
	_ PQuantizationFunc           = WelsQuant4x4_sse2
	_ PQuantizationFunc           = WelsQuantFour4x4_sse2
	_ PQuantizationDcFunc         = WelsQuant4x4Dc_sse2
	_ PQuantizationMaxFunc        = WelsQuantFour4x4Max_sse2
	_ PQuantizationHadamardFunc   = WelsHadamardQuant2x2_c
	_ PQuantizationSkipFunc       = WelsHadamardQuant2x2Skip_c
	_ PTransformHadamard4x4Func   = WelsHadamardT4Dc_c
	_ PGetNoneZeroCountFunc       = WelsGetNoneZeroCount_c
	_ PScanFunc                   = WelsScan4x4DcAc_c
	_ PScanFunc                   = WelsScan4x4Ac_c
	_ PCalculateSingleCtrFunc     = WelsCalculateSingleCtr4x4_c
	_ PSetMemoryZero              = WelsSetMemZero_c

	// deblocking
	_ PLumaDeblockingLT4Func    = common.DeblockLumaLt4V_c
	_ PLumaDeblockingEQ4Func    = common.DeblockLumaEq4V_c
	_ PChromaDeblockingLT4Func  = common.DeblockChromaLt4V_c
	_ PChromaDeblockingEQ4Func  = common.DeblockChromaEq4V_c
	_ PDeblockingBSCalc         = DeblockingBSCalc_c
	_ PDeblockingFilterSlice    = DeblockingFilterSliceAvcbase
	_ PDeblockingFilterSlice    = DeblockingFilterSliceAvcbaseNull
	_ PSetNoneZeroCountZeroFunc = common.WelsNonZeroCount_c

	// mode decision
	_ PFillInterNeighborCacheFunc        = FillNeighborCacheInterWithBGD
	_ PFillInterNeighborCacheFunc        = FillNeighborCacheInterWithoutBGD
	_ PGetVarianceFromIntraVaaFunc       = AnalysisVaaInfoIntra_c
	_ PGetMbSignFromInterVaaFunc         = MdInterAnalysisVaaInfo_c
	_ PUpdateMbMvFunc                    = UpdateMbMv_c
	_ PInterMdFirstIntraModeFunc         = WelsMdFirstIntraMode
	_ PIntraFineMdFunc                   = WelsMdIntraFinePartition
	_ PIntraFineMdFunc                   = WelsMdIntraFinePartitionVaa
	_ PInterFineMdFunc                   = WelsMdInterFinePartition
	_ PInterFineMdFunc                   = WelsMdInterFinePartitionVaa
	_ PInterFineMdFunc                   = WelsMdInterFinePartitionVaaOnScreen
	_ PInterMdFunc                       = WelsMdInterMb
	_ PInterMdFunc                       = WelsMdInterMbEnhancelayer
	_ PInterMdBackgroundDecisionFunc     = WelsMdInterJudgeBGDPskip
	_ PInterMdBackgroundDecisionFunc     = WelsMdInterJudgeBGDPskipFalse
	_ PMdBackgroundInfoUpdateFunc        = WelsMdUpdateBGDInfo
	_ PMdBackgroundInfoUpdateFunc        = WelsMdUpdateBGDInfoNULL
	_ PInterMdScrollingPSkipDecisionFunc = WelsMdInterJudgeSCDPskip
	_ PInterMdScrollingPSkipDecisionFunc = WelsMdInterJudgeSCDPskipFalse
	_ PSetScrollingMv                    = SetScrollingMvToMd
	_ PSetScrollingMv                    = SetScrollingMvToMdNull
	_ pJudgeSkipFun                      = JudgeStaticSkip
	_ pJudgeSkipFun                      = JudgeScrollSkip

	// motion estimation
	_ PMotionSearchFunc                   = WelsMotionEstimateSearch
	_ PMotionSearchFunc                   = WelsMotionEstimateSearchStatic
	_ PMotionSearchFunc                   = WelsMotionEstimateSearchScrolled
	_ PSearchMethodFunc                   = WelsDiamondSearch
	_ PSearchMethodFunc                   = WelsDiamondCrossSearch
	_ PSearchMethodFunc                   = WelsDiamondCrossFeatureSearch
	_ PSearchMethodFunc                   = WelsMotionCrossSearch
	_ PCalculateSatdFunc                  = CalculateSatdCost
	_ PCalculateSatdFunc                  = NotCalculateSatdCost
	_ PCheckDirectionalMv                 = CheckDirectionalMv
	_ PCheckDirectionalMv                 = CheckDirectionalMvFalse
	_ PLineFullSearchFunc                 = LineFullSearch_c
	_ PInitializeHashforFeatureFunc       = InitializeHashforFeature_c
	_ PFillQpelLocationByFeatureValueFunc = FillQpelLocationByFeatureValue_c
	_ PCalculateBlockFeatureOfFrame       = SumOf8x8BlockOfFrame_c
	_ PCalculateBlockFeatureOfFrame       = SumOf16x16BlockOfFrame_c
	_ PCalculateSingleBlockFeature        = SumOf8x8SingleBlock_c
	_ PCalculateSingleBlockFeature        = SumOf16x16SingleBlock_c
	_ PUpdateFMESwitch                    = UpdateFMESwitch
	_ PUpdateFMESwitch                    = UpdateFMESwitchNull

	// rate control
	_ PWelsRCPictureInitFunc                = WelsRcPictureInitGom
	_ PWelsRCPictureInitFunc                = WelRcPictureInitScc
	_ PWelsRCPictureDelayJudgeFunc          = WelsRcFrameDelayJudgeTimeStamp
	_ PWelsRCPictureInfoUpdateFunc          = WelsRcPictureInfoUpdateGom
	_ PWelsRCMBInitFunc                     = WelsRcMbInitGom
	_ PWelsRCMBInfoUpdateFunc               = WelsRcMbInfoUpdateGom
	_ PWelsCheckFrameSkipBasedMaxbrFunc     = CheckFrameSkipBasedMaxbr
	_ PWelsUpdateBufferWhenFrameSkippedFunc = UpdateBufferWhenFrameSkipped
	_ PWelsUpdateMaxBrCheckWindowStatusFunc = UpdateMaxBrCheckWindowStatus
	_ PWelsRCPostFrameSkippingFunc          = WelsRcPostFrameSkipping

	// entropy coding
	_ PCavlcParamCalFunc     = CavlcParamCal_c
	_ PWelsSpatialWriteMbSyn = WelsSpatialWriteMbSyn
	_ PWelsSpatialWriteMbSyn = WelsSpatialWriteMbSynCabac
	_ PStashMBStatus         = StashMBStatusCavlc
	_ PStashMBStatus         = StashMBStatusCabac
	_ PStashPopMBStatus      = StashPopMBStatusCavlc
	_ PStashPopMBStatus      = StashPopMBStatusCabac
	_ PGetBsPosition         = GetBsPosCavlc
	_ PGetBsPosition         = GetBsPosCabac

	// reference list
	_ PBuildRefListFunc      = WelsBuildRefList
	_ PBuildRefListFunc      = WelsBuildRefListScreen
	_ PMarkPicFunc           = WelsMarkPic
	_ PMarkPicFunc           = WelsMarkPicScreen
	_ PUpdateRefListFunc     = WelsUpdateRefList
	_ PUpdateRefListFunc     = WelsUpdateRefListScreen
	_ PAfterBuildRefListFunc = DoNothing
)
