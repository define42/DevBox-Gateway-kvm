// Port of codec/encoder/core/src/au_set.cpp.
//
// Units set (SPS/PPS/subset SPS writing and initialization, level checks).

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func auSetB2U(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

func WelsCheckLevelLimitation(kpSps *SWelsSPS, kpLevelLimit *common.SLevelLimits, fFrameRate float32, iTargetBitRate int32) int32 {
	uiPicWidthInMBs := uint32(int32(kpSps.iMbWidth))
	uiPicHeightInMBs := uint32(int32(kpSps.iMbHeight))
	uiPicInMBs := uiPicWidthInMBs * uiPicHeightInMBs
	uiNumRefFrames := uint32(int32(kpSps.iNumRefFrames))

	if kpLevelLimit.UiMaxMBPS < uint32(float32(uiPicInMBs)*fFrameRate) {
		return 0
	}
	if kpLevelLimit.UiMaxFS < uiPicInMBs {
		return 0
	}
	if (kpLevelLimit.UiMaxFS << 3) < (uiPicWidthInMBs * uiPicWidthInMBs) {
		return 0
	}
	if (kpLevelLimit.UiMaxFS << 3) < (uiPicHeightInMBs * uiPicHeightInMBs) {
		return 0
	}
	if kpLevelLimit.UiMaxDPBMbs < uiNumRefFrames*uiPicInMBs {
		return 0
	}
	if (iTargetBitRate != api.UNSPECIFIED_BIT_RATE) &&
		(int32(kpLevelLimit.UiMaxBR)*1200) < iTargetBitRate { //RC enabled, considering bitrate constraint
		return 0
	}
	//add more checks here if needed in future

	return 1
}

// WelsAdjustLevel walks the level table starting at pCurLevel (which must
// point into common.G_ksLevelLimits, C pointer arithmetic pCurLevel++).
func WelsAdjustLevel(pSpatialLayer *api.SSpatialLayerConfig, pCurLevel *common.SLevelLimits) int32 {
	iIdx := 0
	for i := range common.G_ksLevelLimits {
		if &common.G_ksLevelLimits[i] == pCurLevel {
			iIdx = i
			break
		}
	}
	iMaxBitrate := pSpatialLayer.IMaxSpatialBitrate
	for {
		pCurLevel = &common.G_ksLevelLimits[iIdx]
		if iMaxBitrate <= int32(pCurLevel.UiMaxBR*common.CpbBrNalFactor) {
			pSpatialLayer.UiLevelIdc = pCurLevel.UiLevelIdc
			return 0
		}
		iIdx++
		pCurLevel = &common.G_ksLevelLimits[iIdx]
		if pCurLevel.UiLevelIdc == api.LEVEL_5_2 {
			break
		}
	}
	return 1
}

func WelsCheckNumRefSetting(pLogCtx *common.SLogContext, pParam *SWelsSvcCodingParam, bStrictCheck bool) int32 {
	// validate LTR num
	iCurrentSupportedLtrNum := int32(LONG_TERM_REF_NUM_SCREEN)
	if pParam.IUsageType == api.CAMERA_VIDEO_REAL_TIME {
		iCurrentSupportedLtrNum = LONG_TERM_REF_NUM
	}
	if (pParam.BEnableLongTermReference) && (iCurrentSupportedLtrNum != pParam.ILTRRefNum) {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "iLTRRefNum(%d) does not equal to currently supported %d, will be reset",
			pParam.ILTRRefNum, iCurrentSupportedLtrNum)
		pParam.ILTRRefNum = iCurrentSupportedLtrNum
	} else if !pParam.BEnableLongTermReference {
		pParam.ILTRRefNum = 0
	}

	//TODO: here is a fix needed here, the most reasonable value should be:
	//        iCurrentStrNum = WELS_MAX (1, WELS_LOG2 (pParam->uiGopSize));
	//      but reference list updating need to be changed
	var iCurrentStrNum int32
	if pParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME && pParam.BEnableLongTermReference {
		iCurrentStrNum = common.WELS_MAX(1, common.WELS_LOG2(pParam.uiGopSize))
	} else {
		iCurrentStrNum = int32(common.WELS_MAX(uint32(1), (pParam.uiGopSize >> 1)))
	}
	var iNeededRefNum int32
	if pParam.UiIntraPeriod != 1 {
		iNeededRefNum = iCurrentStrNum + pParam.ILTRRefNum
	}

	iMaxRef := int32(MAX_REFERENCE_PICTURE_COUNT_NUM_SCREEN)
	if pParam.IUsageType == api.CAMERA_VIDEO_REAL_TIME {
		iMaxRef = MAX_REFERENCE_PICTURE_COUNT_NUM_CAMERA
	}
	iNeededRefNum = common.WELS_CLIP3(iNeededRefNum, MIN_REF_PIC_COUNT, iMaxRef)
	// to adjust default or invalid input, in case pParam->iNumRefFrame do not have a valid value for the next step
	if pParam.INumRefFrame == api.AUTO_REF_PIC_COUNT {
		pParam.INumRefFrame = iNeededRefNum
	} else if pParam.INumRefFrame < iNeededRefNum {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
			"iNumRefFrame(%d) setting does not support the temporal and LTR setting, will be reset to %d",
			pParam.INumRefFrame, iNeededRefNum)
		if bStrictCheck {
			return ENC_RETURN_UNSUPPORTED_PARA
		}
		pParam.INumRefFrame = iNeededRefNum
	}

	// after adjustment, do the following:
	// if the setting is larger than needed, we will use the needed, and write the max into sps and for memory to wait for further expanding
	if pParam.iMaxNumRefFrame < pParam.INumRefFrame {
		pParam.iMaxNumRefFrame = pParam.INumRefFrame
	}
	pParam.INumRefFrame = iNeededRefNum

	return ENC_RETURN_SUCCESS
}

func WelsCheckRefFrameLimitationNumRefFirst(pLogCtx *common.SLogContext, pParam *SWelsSvcCodingParam) int32 {
	if WelsCheckNumRefSetting(pLogCtx, pParam, false) != 0 {
		// we take num-ref as the honored setting but it conflicts with temporal and LTR
		return ENC_RETURN_UNSUPPORTED_PARA
	}
	return ENC_RETURN_SUCCESS
}

// auSetLevelLimitsByIdc returns g_ksLevelLimits[uiLevelIdc - 1]. The C code
// indexes the LEVEL_NUMBER-entry table with the level idc itself, which
// reads past its end for level idcs above LEVEL_NUMBER (undefined behaviour);
// in that case the entry describing that level is used instead.
func auSetLevelLimitsByIdc(uiLevelIdc api.ELevelIdc) *common.SLevelLimits {
	iIdx := int(uiLevelIdc) - 1
	if iIdx >= 0 && iIdx < common.LEVEL_NUMBER {
		return &common.G_ksLevelLimits[iIdx]
	}
	for i := range common.G_ksLevelLimits {
		if common.G_ksLevelLimits[i].UiLevelIdc == uiLevelIdc {
			return &common.G_ksLevelLimits[i]
		}
	}
	return &common.G_ksLevelLimits[common.LEVEL_NUMBER-1]
}

func WelsCheckRefFrameLimitationLevelIdcFirst(pLogCtx *common.SLogContext, pParam *SWelsSvcCodingParam) int32 {
	if (pParam.INumRefFrame == api.AUTO_REF_PIC_COUNT) || (pParam.iMaxNumRefFrame == api.AUTO_REF_PIC_COUNT) {
		//no need to do the checking
		return ENC_RETURN_SUCCESS
	}

	WelsCheckNumRefSetting(pLogCtx, pParam, false)

	var i int32
	var iRefFrame int32
	//get the number of reference frame according to level limitation.
	for i = 0; i < pParam.ISpatialLayerNum; i++ {
		pSpatialLayer := &pParam.SSpatialLayers[i]
		if pSpatialLayer.UiLevelIdc == api.LEVEL_UNKNOWN {
			continue
		}

		uiPicInMBs := uint32(((pSpatialLayer.IVideoHeight + 15) >> 4) * ((pSpatialLayer.IVideoWidth + 15) >> 4))
		iRefFrame = int32(auSetLevelLimitsByIdc(pSpatialLayer.UiLevelIdc).UiMaxDPBMbs / uiPicInMBs)

		//check iMaxNumRefFrame
		if iRefFrame < pParam.iMaxNumRefFrame {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "iMaxNumRefFrame(%d) adjusted to %d because of limitation from uiLevelIdc=%d",
				pParam.iMaxNumRefFrame, iRefFrame, int32(pSpatialLayer.UiLevelIdc))
			pParam.iMaxNumRefFrame = iRefFrame

			//check iNumRefFrame
			if iRefFrame < pParam.INumRefFrame {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "iNumRefFrame(%d) adjusted to %d because of limitation from uiLevelIdc=%d",
					pParam.INumRefFrame, iRefFrame, int32(pSpatialLayer.UiLevelIdc))
				pParam.INumRefFrame = iRefFrame
			}
		} else {
			//because it is level first now, so adjust max-ref
			common.WelsLog(pLogCtx, api.WELS_LOG_INFO,
				"iMaxNumRefFrame(%d) adjusted to %d because of uiLevelIdc=%d -- under level-idc first strategy ",
				pParam.iMaxNumRefFrame, iRefFrame, int32(pSpatialLayer.UiLevelIdc))
			pParam.iMaxNumRefFrame = iRefFrame
		}
	}

	return ENC_RETURN_SUCCESS
}

func WelsGetLevelIdc(kpSps *SWelsSPS, fFrameRate float32, iTargetBitRate int32) api.ELevelIdc {
	var iOrder int32
	for iOrder = 0; iOrder < common.LEVEL_NUMBER; iOrder++ {
		if WelsCheckLevelLimitation(kpSps, &common.G_ksLevelLimits[iOrder], fFrameRate, iTargetBitRate) != 0 {
			return common.G_ksLevelLimits[iOrder].UiLevelIdc
		}
	}
	return api.LEVEL_5_1 //final decision: select the biggest level
}

func WelsWriteVUI(pSps *SWelsSPS, pBitStringAux *common.SBitStringAux) int32 {
	pLocalBitStringAux := pBitStringAux

	common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pSps.bAspectRatioPresent)) //aspect_ratio_info_present_flag
	if pSps.bAspectRatioPresent {
		common.BsWriteBits(pLocalBitStringAux, 8, uint32(pSps.eAspectRatio)) // aspect_ratio_idc
		if pSps.eAspectRatio == api.ASP_EXT_SAR {
			common.BsWriteBits(pLocalBitStringAux, 16, uint32(pSps.sAspectRatioExtWidth))  // sar_width
			common.BsWriteBits(pLocalBitStringAux, 16, uint32(pSps.sAspectRatioExtHeight)) // sar_height
		}
	}
	common.BsWriteOneBit(pLocalBitStringAux, 0) //overscan_info_present_flag

	// See codec_app_def.h and parameter_sets.h for more info about members bVideoSignalTypePresent through uiColorMatrix.
	common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pSps.bVideoSignalTypePresent)) //video_signal_type_present_flag
	if pSps.bVideoSignalTypePresent {
		//write video signal type info to header

		common.BsWriteBits(pLocalBitStringAux, 3, uint32(pSps.uiVideoFormat))
		common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pSps.bFullRange))
		common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pSps.bColorDescriptionPresent))

		if pSps.bColorDescriptionPresent {
			//write color description info to header

			common.BsWriteBits(pLocalBitStringAux, 8, uint32(pSps.uiColorPrimaries))
			common.BsWriteBits(pLocalBitStringAux, 8, uint32(pSps.uiTransferCharacteristics))
			common.BsWriteBits(pLocalBitStringAux, 8, uint32(pSps.uiColorMatrix))

		} //write color description info to header

	} //write video signal type info to header

	common.BsWriteOneBit(pLocalBitStringAux, 0) //chroma_loc_info_present_flag
	common.BsWriteOneBit(pLocalBitStringAux, 0) //timing_info_present_flag
	common.BsWriteOneBit(pLocalBitStringAux, 0) //nal_hrd_parameters_present_flag
	common.BsWriteOneBit(pLocalBitStringAux, 0) //vcl_hrd_parameters_present_flag
	common.BsWriteOneBit(pLocalBitStringAux, 0) //pic_struct_present_flag
	common.BsWriteOneBit(pLocalBitStringAux, 1) //bitstream_restriction_flag

	//
	common.BsWriteOneBit(pLocalBitStringAux, 1) //motion_vectors_over_pic_boundaries_flag
	common.BsWriteUE(pLocalBitStringAux, 0)     //max_bytes_per_pic_denom
	common.BsWriteUE(pLocalBitStringAux, 0)     //max_bits_per_mb_denom
	common.BsWriteUE(pLocalBitStringAux, 16)    //log2_max_mv_length_horizontal
	common.BsWriteUE(pLocalBitStringAux, 16)    //log2_max_mv_length_vertical

	common.BsWriteUE(pLocalBitStringAux, 0)                                 //max_num_reorder_frames
	common.BsWriteUE(pLocalBitStringAux, uint32(int32(pSps.iNumRefFrames))) //max_dec_frame_buffering

	return 0
}

// WelsWriteSpsSyntax sets Sequence Parameter Set (SPS).
// pSpsIdDelta: int32_t* from IWelsParametersetStrategy.GetSpsIdOffsetList, indexed by uiSpsId.
func WelsWriteSpsSyntax(pSps *SWelsSPS, pBitStringAux *common.SBitStringAux, pSpsIdDelta []int32, bBaseLayer bool) int32 {
	pLocalBitStringAux := pBitStringAux

	uiProfileIdc := api.EProfileIdc(pSps.uiProfileIdc)

	common.BsWriteBits(pLocalBitStringAux, 8, uint32(pSps.uiProfileIdc))

	common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pSps.bConstraintSet0Flag)) // bConstraintSet0Flag
	common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pSps.bConstraintSet1Flag)) // bConstraintSet1Flag
	common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pSps.bConstraintSet2Flag)) // bConstraintSet2Flag
	common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pSps.bConstraintSet3Flag)) // bConstraintSet3Flag
	if api.PRO_HIGH == uiProfileIdc || api.PRO_EXTENDED == uiProfileIdc ||
		api.PRO_MAIN == uiProfileIdc {
		common.BsWriteOneBit(pLocalBitStringAux, 1)  // bConstraintSet4Flag: If profile_idc is equal to 77, 88, or 100, constraint_set4_flag equal to 1 indicates that the value of frame_mbs_only_flag is equal to 1. constraint_set4_flag equal to 0 indicates that the value of frame_mbs_only_flag may or may not be equal to 1.
		common.BsWriteOneBit(pLocalBitStringAux, 1)  // bConstraintSet5Flag: If profile_idc is equal to 77, 88, or 100, constraint_set5_flag equal to 1 indicates that B slice types are not present in the coded video sequence. constraint_set5_flag equal to 0 indicates that B slice types may or may not be present in the coded video sequence.
		common.BsWriteBits(pLocalBitStringAux, 2, 0) // reserved_zero_2bits, equal to 0
	} else {
		common.BsWriteBits(pLocalBitStringAux, 4, 0) // reserved_zero_4bits, equal to 0
	}
	common.BsWriteBits(pLocalBitStringAux, 8, uint32(pSps.iLevelIdc))                    // iLevelIdc
	common.BsWriteUE(pLocalBitStringAux, pSps.uiSpsId+uint32(pSpsIdDelta[pSps.uiSpsId])) // seq_parameter_set_id

	if api.PRO_SCALABLE_BASELINE == uiProfileIdc || api.PRO_SCALABLE_HIGH == uiProfileIdc ||
		api.PRO_HIGH == uiProfileIdc || api.PRO_HIGH10 == uiProfileIdc ||
		api.PRO_HIGH422 == uiProfileIdc || api.PRO_HIGH444 == uiProfileIdc ||
		api.PRO_CAVLC444 == uiProfileIdc || 44 == uiProfileIdc {
		common.BsWriteUE(pLocalBitStringAux, 1)     //uiChromaFormatIdc, now should be 1
		common.BsWriteUE(pLocalBitStringAux, 0)     //uiBitDepthLuma
		common.BsWriteUE(pLocalBitStringAux, 0)     //uiBitDepthChroma
		common.BsWriteOneBit(pLocalBitStringAux, 0) //qpprime_y_zero_transform_bypass_flag
		common.BsWriteOneBit(pLocalBitStringAux, 0) //seq_scaling_matrix_present_flag
	}

	common.BsWriteUE(pLocalBitStringAux, pSps.uiLog2MaxFrameNum-4) // log2_max_frame_num_minus4
	common.BsWriteUE(pLocalBitStringAux, pSps.uiPocType)           // pic_order_cnt_type
	if pSps.uiPocType == 0 {
		common.BsWriteUE(pLocalBitStringAux, uint32(pSps.iLog2MaxPocLsb-4)) // log2_max_pic_order_cnt_lsb_minus4
	} else if pSps.uiPocType == 1 {
		// TODO: implement
		// assert (0);
	} else {
		// no-op for uiPocType 2.
	}

	common.BsWriteUE(pLocalBitStringAux, uint32(int32(pSps.iNumRefFrames)))                  // max_num_ref_frames
	common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pSps.bGapsInFrameNumValueAllowedFlag)) //gaps_in_frame_numvalue_allowed_flag
	common.BsWriteUE(pLocalBitStringAux, uint32(int32(pSps.iMbWidth)-1))                     // pic_width_in_mbs_minus1
	common.BsWriteUE(pLocalBitStringAux, uint32(int32(pSps.iMbHeight)-1))                    // pic_height_in_map_units_minus1
	common.BsWriteOneBit(pLocalBitStringAux, 1 /*pSps->bFrameMbsOnlyFlag*/)                  // bFrameMbsOnlyFlag

	var d8x8 uint8
	if pSps.iLevelIdc >= 30 {
		d8x8 = 1
	}
	common.BsWriteOneBit(pLocalBitStringAux, uint32(d8x8) /*pSps->bDirect8x8InferenceFlag*/) // direct_8x8_inference_flag

	common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pSps.bFrameCroppingFlag)) // bFrameCroppingFlag
	if pSps.bFrameCroppingFlag {
		common.BsWriteUE(pLocalBitStringAux, uint32(int32(pSps.sFrameCrop.iCropLeft)))   // frame_crop_left_offset
		common.BsWriteUE(pLocalBitStringAux, uint32(int32(pSps.sFrameCrop.iCropRight)))  // frame_crop_right_offset
		common.BsWriteUE(pLocalBitStringAux, uint32(int32(pSps.sFrameCrop.iCropTop)))    // frame_crop_top_offset
		common.BsWriteUE(pLocalBitStringAux, uint32(int32(pSps.sFrameCrop.iCropBottom))) // frame_crop_bottom_offset
	}
	if bBaseLayer {
		common.BsWriteOneBit(pLocalBitStringAux, 1) // vui_parameters_present_flag
		WelsWriteVUI(pSps, pBitStringAux)
	} else {
		common.BsWriteOneBit(pLocalBitStringAux, 0)
	}
	return 0
}

func WelsWriteSpsNal(pSps *SWelsSPS, pBitStringAux *common.SBitStringAux, pSpsIdDelta []int32) int32 {
	WelsWriteSpsSyntax(pSps, pBitStringAux, pSpsIdDelta, true)

	common.BsRbspTrailingBits(pBitStringAux)

	return 0
}

// WelsWriteSubsetSpsSyntax writes SubSet Sequence Parameter Set.
func WelsWriteSubsetSpsSyntax(pSubsetSps *SSubsetSps, pBitStringAux *common.SBitStringAux, pSpsIdDelta []int32) int32 {
	pSps := &pSubsetSps.pSps

	WelsWriteSpsSyntax(pSps, pBitStringAux, pSpsIdDelta, false)

	if api.EProfileIdc(pSps.uiProfileIdc) == api.PRO_SCALABLE_BASELINE || api.EProfileIdc(pSps.uiProfileIdc) == api.PRO_SCALABLE_HIGH {
		pSubsetSpsExt := &pSubsetSps.sSpsSvcExt

		common.BsWriteOneBit(pBitStringAux, 1 /*pSubsetSpsExt->bInterLayerDeblockingFilterCtrlPresentFlag*/)
		common.BsWriteBits(pBitStringAux, 2, uint32(pSubsetSpsExt.iExtendedSpatialScalability))
		common.BsWriteOneBit(pBitStringAux, 0 /*pSubsetSpsExt->uiChromaPhaseXPlus1Flag*/)
		common.BsWriteBits(pBitStringAux, 2, 1 /*pSubsetSpsExt->uiChromaPhaseYPlus1*/)
		if pSubsetSpsExt.iExtendedSpatialScalability == 1 {
			common.BsWriteOneBit(pBitStringAux, 0 /*pSubsetSpsExt->uiSeqRefLayerChromaPhaseXPlus1Flag*/)
			common.BsWriteBits(pBitStringAux, 2, 1 /*pSubsetSpsExt->uiSeqRefLayerChromaPhaseYPlus1*/)
			common.BsWriteSE(pBitStringAux, 0 /*pSubsetSpsExt->sSeqScaledRefLayer.left_offset*/)
			common.BsWriteSE(pBitStringAux, 0 /*pSubsetSpsExt->sSeqScaledRefLayer.top_offset*/)
			common.BsWriteSE(pBitStringAux, 0 /*pSubsetSpsExt->sSeqScaledRefLayer.right_offset*/)
			common.BsWriteSE(pBitStringAux, 0 /*pSubsetSpsExt->sSeqScaledRefLayer.bottom_offset*/)
		}
		common.BsWriteOneBit(pBitStringAux, auSetB2U(pSubsetSpsExt.bSeqTcoeffLevelPredFlag))
		if pSubsetSpsExt.bSeqTcoeffLevelPredFlag {
			common.BsWriteOneBit(pBitStringAux, auSetB2U(pSubsetSpsExt.bAdaptiveTcoeffLevelPredFlag))
		}
		common.BsWriteOneBit(pBitStringAux, auSetB2U(pSubsetSpsExt.bSliceHeaderRestrictionFlag))

		common.BsWriteOneBit(pBitStringAux, 0 /*pSubsetSps->bSvcVuiParamPresentFlag*/)
	}
	common.BsWriteOneBit(pBitStringAux, 0 /*pSubsetSps->bAdditionalExtension2Flag*/)

	common.BsRbspTrailingBits(pBitStringAux)

	return 0
}

// WelsWritePpsSyntax writes Picture Parameter Set (PPS).
func WelsWritePpsSyntax(pPps *SWelsPPS, pBitStringAux *common.SBitStringAux, pParametersetStrategy IWelsParametersetStrategy) int32 {
	pLocalBitStringAux := pBitStringAux

	common.BsWriteUE(pLocalBitStringAux, pPps.iPpsId+uint32(pParametersetStrategy.GetPpsIdOffset(int32(pPps.iPpsId))))
	common.BsWriteUE(pLocalBitStringAux, pPps.iSpsId+uint32(pParametersetStrategy.GetSpsIdOffset(int32(pPps.iPpsId), int32(pPps.iSpsId))))

	common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pPps.bEntropyCodingModeFlag))
	common.BsWriteOneBit(pLocalBitStringAux, 0 /*pPps->bPicOrderPresentFlag*/)

	// DISABLE_FMO_FEATURE
	common.BsWriteUE(pLocalBitStringAux, 0 /*pPps->uiNumSliceGroups - 1*/)

	common.BsWriteUE(pLocalBitStringAux, 0 /*pPps->uiNumRefIdxL0Active - 1*/)
	common.BsWriteUE(pLocalBitStringAux, 0 /*pPps->uiNumRefIdxL1Active - 1*/)

	common.BsWriteOneBit(pLocalBitStringAux, 0 /*pPps->bWeightedPredFlag*/)
	common.BsWriteBits(pLocalBitStringAux, 2, 0 /*pPps->uiWeightedBiPredIdc*/)

	common.BsWriteSE(pLocalBitStringAux, int32(pPps.iPicInitQp)-26)
	common.BsWriteSE(pLocalBitStringAux, int32(pPps.iPicInitQs)-26)

	common.BsWriteSE(pLocalBitStringAux, int32(pPps.uiChromaQpIndexOffset))
	common.BsWriteOneBit(pLocalBitStringAux, auSetB2U(pPps.bDeblockingFilterControlPresentFlag))
	common.BsWriteOneBit(pLocalBitStringAux, 0 /*pPps->bConstainedIntraPredFlag*/)
	common.BsWriteOneBit(pLocalBitStringAux, 0 /*pPps->bRedundantPicCntPresentFlag*/)

	common.BsRbspTrailingBits(pLocalBitStringAux)

	return 0
}

func WelsGetPaddingOffset(iActualWidth int32, iActualHeight int32, iWidth int32, iHeight int32, pOffset *SCropOffset) bool {
	if (iWidth < iActualWidth) || (iHeight < iActualHeight) {
		return false
	}

	// make actual size even
	iActualWidth -= (iActualWidth & 1)
	iActualHeight -= (iActualHeight & 1)

	pOffset.iCropLeft = 0
	pOffset.iCropRight = int16((iWidth - iActualWidth) / 2)
	pOffset.iCropTop = 0
	pOffset.iCropBottom = int16((iHeight - iActualHeight) / 2)

	return (iWidth > iActualWidth) || (iHeight > iActualHeight)
}

func WelsInitSps(pSps *SWelsSPS, pLayerParam *api.SSpatialLayerConfig, pLayerParamInternal *SSpatialLayerInternal, kuiIntraPeriod uint32, kiNumRefFrame int32, kuiSpsId uint32, kbEnableFrameCropping bool, bEnableRc bool, kiDlayerCount int32, bSVCBaselayer bool) int32 {
	*pSps = SWelsSPS{}
	pSps.uiSpsId = kuiSpsId
	pSps.iMbWidth = int16((pLayerParam.IVideoWidth + 15) >> 4)
	pSps.iMbHeight = int16((pLayerParam.IVideoHeight + 15) >> 4)

	//max value of both iFrameNum and POC are 2^16-1, in our encoder, iPOC=2*iFrameNum, so max of iFrameNum should be 2^15-1.--
	pSps.uiLog2MaxFrameNum = 15 //16;
	pSps.uiPocType = 2
	pSps.iLog2MaxPocLsb = int32(1 + pSps.uiLog2MaxFrameNum)

	pSps.iNumRefFrames = int16(kiNumRefFrame) /* min pRef size when fifo pRef operation*/

	if kbEnableFrameCropping {
		// TODO: get frame_crop_left_offset, frame_crop_right_offset, frame_crop_top_offset, frame_crop_bottom_offset
		pSps.bFrameCroppingFlag = WelsGetPaddingOffset(pLayerParamInternal.iActualWidth, pLayerParamInternal.iActualHeight,
			pLayerParam.IVideoWidth, pLayerParam.IVideoHeight, &pSps.sFrameCrop)
	} else {
		pSps.bFrameCroppingFlag = false
	}
	if pLayerParam.UiProfileIdc != 0 {
		pSps.uiProfileIdc = uint8(pLayerParam.UiProfileIdc)
	} else {
		pSps.uiProfileIdc = uint8(api.PRO_BASELINE)
	}
	if pLayerParam.UiProfileIdc == api.PRO_BASELINE {
		pSps.bConstraintSet0Flag = true
	}
	if pLayerParam.UiProfileIdc <= api.PRO_MAIN {
		pSps.bConstraintSet1Flag = true
	}
	if (kiDlayerCount > 1) && bSVCBaselayer {
		pSps.bConstraintSet2Flag = true
	}

	uiLevel := WelsGetLevelIdc(pSps, pLayerParamInternal.fOutputFrameRate, pLayerParam.ISpatialBitrate)
	//update level
	//for Scalable Baseline, Scalable High, and Scalable High Intra profiles.If level_idc is equal to 9, the indicated level is level 1b.
	//for the Baseline, Constrained Baseline, Main, and Extended profiles,If level_idc is equal to 11 and constraint_set3_flag is equal to 1, the indicated level is level 1b.
	kuiProfile := api.EProfileIdc(pSps.uiProfileIdc)
	if (uiLevel == api.LEVEL_1_B) &&
		((kuiProfile == api.PRO_BASELINE) || (kuiProfile == api.PRO_MAIN) || (kuiProfile == api.PRO_EXTENDED)) {
		uiLevel = api.LEVEL_1_1
		pSps.bConstraintSet3Flag = true
	}
	if (pLayerParam.UiLevelIdc == api.LEVEL_UNKNOWN) || (pLayerParam.UiLevelIdc < uiLevel) {
		pLayerParam.UiLevelIdc = uiLevel
	}
	pSps.iLevelIdc = uint8(pLayerParam.UiLevelIdc)

	//bGapsInFrameNumValueAllowedFlag is false when only spatial layer number and temporal layer number is 1, and ltr is 0.
	if (kiDlayerCount == 1) && (pSps.iNumRefFrames == 1) {
		pSps.bGapsInFrameNumValueAllowedFlag = false
	} else {
		pSps.bGapsInFrameNumValueAllowedFlag = true
	}

	pSps.bVuiParamPresentFlag = true

	pSps.bAspectRatioPresent = pLayerParam.BAspectRatioPresent
	pSps.eAspectRatio = pLayerParam.EAspectRatio
	pSps.sAspectRatioExtWidth = pLayerParam.SAspectRatioExtWidth
	pSps.sAspectRatioExtHeight = pLayerParam.SAspectRatioExtHeight

	// See codec_app_def.h and parameter_sets.h for more info about members bVideoSignalTypePresent through uiColorMatrix.
	pSps.bVideoSignalTypePresent = pLayerParam.BVideoSignalTypePresent
	pSps.uiVideoFormat = pLayerParam.UiVideoFormat
	pSps.bFullRange = pLayerParam.BFullRange
	pSps.bColorDescriptionPresent = pLayerParam.BColorDescriptionPresent
	pSps.uiColorPrimaries = pLayerParam.UiColorPrimaries
	pSps.uiTransferCharacteristics = pLayerParam.UiTransferCharacteristics
	pSps.uiColorMatrix = pLayerParam.UiColorMatrix

	return 0
}

func WelsInitSubsetSps(pSubsetSps *SSubsetSps, pLayerParam *api.SSpatialLayerConfig, pLayerParamInternal *SSpatialLayerInternal, kuiIntraPeriod uint32, kiNumRefFrame int32, kuiSpsId uint32, kbEnableFrameCropping bool, bEnableRc bool, kiDlayerCount int32) int32 {
	pSps := &pSubsetSps.pSps

	*pSubsetSps = SSubsetSps{}

	WelsInitSps(pSps, pLayerParam, pLayerParamInternal, kuiIntraPeriod, kiNumRefFrame, kuiSpsId, kbEnableFrameCropping,
		bEnableRc, kiDlayerCount, false)

	pSps.uiProfileIdc = uint8(pLayerParam.UiProfileIdc)

	pSubsetSps.sSpsSvcExt.iExtendedSpatialScalability = 0 /* ESS is 0 in default */
	pSubsetSps.sSpsSvcExt.bAdaptiveTcoeffLevelPredFlag = false
	pSubsetSps.sSpsSvcExt.bSeqTcoeffLevelPredFlag = false
	pSubsetSps.sSpsSvcExt.bSliceHeaderRestrictionFlag = true

	return 0
}

func WelsInitPps(pPps *SWelsPPS, pSps *SWelsSPS, pSubsetSps *SSubsetSps, kuiPpsId uint32, kbDeblockingFilterPresentFlag bool, kbUsingSubsetSps bool, kbEntropyCodingModeFlag bool) int32 {
	var pUsedSps *SWelsSPS
	if pPps == nil || (pSps == nil && pSubsetSps == nil) {
		return 1
	}
	if !kbUsingSubsetSps {
		if nil == pSps {
			return 1
		}
		pUsedSps = pSps
	} else {
		if nil == pSubsetSps {
			return 1
		}
		pUsedSps = &pSubsetSps.pSps
	}

	/* fill picture parameter set syntax */
	pPps.iPpsId = kuiPpsId
	pPps.iSpsId = pUsedSps.uiSpsId
	pPps.bEntropyCodingModeFlag = kbEntropyCodingModeFlag

	pPps.iPicInitQp = 26
	pPps.iPicInitQs = 26

	pPps.uiChromaQpIndexOffset = 0
	pPps.bDeblockingFilterControlPresentFlag = kbDeblockingFilterPresentFlag

	return 0
}
