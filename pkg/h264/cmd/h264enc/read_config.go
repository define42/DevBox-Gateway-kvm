package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/console"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/encoder"
)

// Configuration file parsing of welsenc.cpp (ParseLayerConfig, ParseConfig).
// The line reader itself (CReadConfig, codec/console/common/src/read_config.cpp)
// lives in internal/console as it is shared with h264dec.

// SLayerPEncCtx is the layer context.
type SLayerPEncCtx struct {
	iDLayerQp      int32
	sSliceArgument api.SSliceArgument
}

// SFilesSet holds the file names given in the configuration / command line.
type SFilesSet struct {
	strBsFile          string
	strSeqFile         string // for cmd lines
	strLayerCfgFile    [encoder.MAX_DEPENDENCY_LAYER]string
	sRecFileName       [encoder.MAX_DEPENDENCY_LAYER]string
	uiFrameToBeCoded   uint32
	bEnableMultiBsFile bool
}

// spsPpsIdStrategy maps the SpsPpsIDStrategy / -spsid value.
func spsPpsIdStrategy(iValue int32) api.EParameterSetStrategy {
	switch iValue {
	case 0:
		return api.CONSTANT_ID
	case 0x01:
		return api.INCREASING_ID
	case 0x02:
		return api.SPS_LISTING
	case 0x03:
		return api.SPS_LISTING_AND_PPS_INCREASING
	case 0x06:
		return api.SPS_PPS_LISTING
	default:
		return api.CONSTANT_ID
	}
}

func atoiBool(s string) bool {
	return console.Atoi(s) != 0
}

func ParseLayerConfig(cRdLayerCfg *console.CReadConfig, iLayer int, pSvcParam *api.SEncParamExt, sFileSet *SFilesSet) int {
	if !cRdLayerCfg.ExistFile() {
		fmt.Fprintf(os.Stderr, "Unabled to open layer #%d configuration file: %s.\n", iLayer, cRdLayerCfg.GetFileName())
		return 1
	}

	pDLayer := &pSvcParam.SSpatialLayers[iLayer]
	iLeftTargetBitrate := int32(0)
	if pSvcParam.IRCMode != api.RC_OFF_MODE {
		iLeftTargetBitrate = pSvcParam.ITargetBitrate
	}
	var sLayerCtx SLayerPEncCtx

	strTag := make([]string, 4)
	str_ := "SlicesAssign"
	kiSize := len(str_)

	for !cRdLayerCfg.EndOfFile() {
		iLayerRd := cRdLayerCfg.ReadLine(strTag, 4)
		if iLayerRd > 0 {
			if strTag[0] == "" {
				continue
			}
			if strTag[0] == "FrameWidth" {
				pDLayer.IVideoWidth = console.Atoi(strTag[1])
			} else if strTag[0] == "FrameHeight" {
				pDLayer.IVideoHeight = console.Atoi(strTag[1])
			} else if strTag[0] == "FrameRateOut" {
				pDLayer.FFrameRate = float32(console.Atof(strTag[1]))
			} else if strTag[0] == "ReconFile" {
				kiLen := len(strTag[1])
				if kiLen >= common.MAX_FNAME_LEN {
					return -1
				}
				sFileSet.sRecFileName[iLayer] = strTag[1]
			} else if strTag[0] == "ProfileIdc" {
				pDLayer.UiProfileIdc = api.EProfileIdc(console.Atoi(strTag[1]))
			} else if strTag[0] == "FRExt" {
				//        pDLayer->frext_mode = (bool)atoi(strTag[1].c_str());
			} else if strTag[0] == "SpatialBitrate" {
				pDLayer.ISpatialBitrate = 1000 * console.Atoi(strTag[1])
				if pSvcParam.IRCMode != api.RC_OFF_MODE {
					if pDLayer.ISpatialBitrate <= 0 {
						fmt.Fprintf(os.Stderr, "Invalid spatial bitrate(%d) in dependency layer #%d.\n", pDLayer.ISpatialBitrate, iLayer)
						return -1
					}
					if pDLayer.ISpatialBitrate > iLeftTargetBitrate {
						fmt.Fprintf(os.Stderr, "Invalid spatial(#%d) bitrate(%d) setting due to unavailable left(%d)!\n", iLayer,
							pDLayer.ISpatialBitrate, iLeftTargetBitrate)
						return -1
					}
					iLeftTargetBitrate -= pDLayer.ISpatialBitrate
				}
			} else if strTag[0] == "MaxSpatialBitrate" {
				pDLayer.IMaxSpatialBitrate = 1000 * console.Atoi(strTag[1])
				if pSvcParam.IRCMode != api.RC_OFF_MODE {
					if pDLayer.IMaxSpatialBitrate < 0 {
						fmt.Fprintf(os.Stderr, "Invalid max spatial bitrate(%d) in dependency layer #%d.\n", pDLayer.IMaxSpatialBitrate, iLayer)
						return -1
					}
					if pDLayer.IMaxSpatialBitrate > 0 && pDLayer.IMaxSpatialBitrate < pDLayer.ISpatialBitrate {
						fmt.Fprintf(os.Stderr, "Invalid max spatial(#%d) bitrate(%d) setting::: < layerBitrate(%d)!\n", iLayer,
							pDLayer.IMaxSpatialBitrate, pDLayer.ISpatialBitrate)
						return -1
					}
				}
			} else if strTag[0] == "InitialQP" {
				sLayerCtx.iDLayerQp = console.Atoi(strTag[1])
			} else if strTag[0] == "SliceMode" {
				sLayerCtx.sSliceArgument.UiSliceMode = api.SliceModeEnum(console.Atoi(strTag[1]))
			} else if strTag[0] == "SliceSize" { //SM_SIZELIMITED_SLICE
				sLayerCtx.sSliceArgument.UiSliceSizeConstraint = uint32(console.Atoi(strTag[1]))
				continue
			} else if strTag[0] == "SliceNum" {
				sLayerCtx.sSliceArgument.UiSliceNum = uint32(console.Atoi(strTag[1]))
			} else if strings.HasPrefix(strTag[0], str_) {
				uiSliceIdx := console.Atoi(strTag[0][kiSize:])
				// assert (uiSliceIdx < MAX_SLICES_NUM) -- compiled out (NDEBUG) in
				// the C build; out-of-range indices are ignored here.
				if uiSliceIdx >= 0 && uiSliceIdx < encoder.MAX_SLICES_NUM && int(uiSliceIdx) < len(sLayerCtx.sSliceArgument.UiSliceMbNum) {
					sLayerCtx.sSliceArgument.UiSliceMbNum[uiSliceIdx] = uint32(console.Atoi(strTag[1]))
				}
			}
		}
	}
	pDLayer.IDLayerQp = sLayerCtx.iDLayerQp
	pDLayer.SSliceArgument = sLayerCtx.sSliceArgument

	return 0
}

func ParseConfig(cRdCfg *console.CReadConfig, pSrcPic *api.SSourcePicture, pSvcParam *api.SEncParamExt, sFileSet *SFilesSet) int {
	strTag := make([]string, 4)
	iRet := 0
	iLayerCount := int8(0)

	for !cRdCfg.EndOfFile() {
		iRd := cRdCfg.ReadLine(strTag, 4)
		if iRd > 0 {
			if strTag[0] == "" {
				continue
			}

			if strTag[0] == "UsageType" {
				pSvcParam.IUsageType = api.EUsageType(console.Atoi(strTag[1]))
			} else if strTag[0] == "SimulcastAVC" {
				pSvcParam.BSimulcastAVC = atoiBool(strTag[1])
			} else if strTag[0] == "SourceWidth" {
				pSrcPic.IPicWidth = console.Atoi(strTag[1])
			} else if strTag[0] == "SourceHeight" {
				pSrcPic.IPicHeight = console.Atoi(strTag[1])
			} else if strTag[0] == "InputFile" {
				if len(strTag[1]) > 0 {
					sFileSet.strSeqFile = strTag[1]
				}
			} else if strTag[0] == "OutputFile" {
				sFileSet.strBsFile = strTag[1]
			} else if strTag[0] == "MaxFrameRate" {
				pSvcParam.FMaxFrameRate = float32(console.Atof(strTag[1]))
			} else if strTag[0] == "FramesToBeEncoded" {
				sFileSet.uiFrameToBeCoded = uint32(console.Atoi(strTag[1]))
			} else if strTag[0] == "TemporalLayerNum" {
				pSvcParam.ITemporalLayerNum = console.Atoi(strTag[1])
			} else if strTag[0] == "IntraPeriod" {
				pSvcParam.UiIntraPeriod = uint32(console.Atoi(strTag[1]))
			} else if strTag[0] == "MaxNalSize" {
				pSvcParam.UiMaxNalSize = uint32(console.Atoi(strTag[1]))
			} else if strTag[0] == "SpsPpsIDStrategy" {
				pSvcParam.ESpsPpsIdStrategy = spsPpsIdStrategy(console.Atoi(strTag[1]))
			} else if strTag[0] == "EnableMultiBsFile" {
				sFileSet.bEnableMultiBsFile = atoiBool(strTag[1])
			} else if strTag[0] == "EnableScalableSEI" {
				pSvcParam.BEnableSSEI = atoiBool(strTag[1])
			} else if strTag[0] == "EnableFrameCropping" {
				pSvcParam.BEnableFrameCroppingFlag = console.Atoi(strTag[1]) != 0
			} else if strTag[0] == "EntropyCodingModeFlag" {
				if console.Atoi(strTag[1]) != 0 {
					pSvcParam.IEntropyCodingModeFlag = 1
				} else {
					pSvcParam.IEntropyCodingModeFlag = 0
				}
			} else if strTag[0] == "ComplexityMode" {
				pSvcParam.IComplexityMode = api.ECOMPLEXITY_MODE(console.Atoi(strTag[1]))
			} else if strTag[0] == "LoopFilterDisableIDC" {
				pSvcParam.ILoopFilterDisableIdc = int32(int8(console.Atoi(strTag[1])))
				if pSvcParam.ILoopFilterDisableIdc > 6 || pSvcParam.ILoopFilterDisableIdc < 0 {
					fmt.Fprintf(os.Stderr, "Invalid parameter in iLoopFilterDisableIdc: %d.\n", pSvcParam.ILoopFilterDisableIdc)
					iRet = 1
					break
				}
			} else if strTag[0] == "LoopFilterAlphaC0Offset" {
				pSvcParam.ILoopFilterAlphaC0Offset = int32(int8(console.Atoi(strTag[1])))
				if pSvcParam.ILoopFilterAlphaC0Offset < -6 {
					pSvcParam.ILoopFilterAlphaC0Offset = -6
				} else if pSvcParam.ILoopFilterAlphaC0Offset > 6 {
					pSvcParam.ILoopFilterAlphaC0Offset = 6
				}
			} else if strTag[0] == "LoopFilterBetaOffset" {
				pSvcParam.ILoopFilterBetaOffset = int32(int8(console.Atoi(strTag[1])))
				if pSvcParam.ILoopFilterBetaOffset < -6 {
					pSvcParam.ILoopFilterBetaOffset = -6
				} else if pSvcParam.ILoopFilterBetaOffset > 6 {
					pSvcParam.ILoopFilterBetaOffset = 6
				}
			} else if strTag[0] == "MultipleThreadIdc" {
				// # 0: auto(dynamic imp. internal encoder); 1: multiple threads imp. disabled; > 1: count number of threads;
				// iMultipleThreadIdc is unsigned short, so the "< 0" test of the C code never fires.
				pSvcParam.IMultipleThreadIdc = uint16(console.Atoi(strTag[1]))
				if pSvcParam.IMultipleThreadIdc > encoder.MAX_THREADS_NUM {
					pSvcParam.IMultipleThreadIdc = encoder.MAX_THREADS_NUM
				}
			} else if strTag[0] == "UseLoadBalancing" {
				pSvcParam.BUseLoadBalancing = atoiBool(strTag[1])
			} else if strTag[0] == "RCMode" {
				pSvcParam.IRCMode = api.RC_MODES(console.Atoi(strTag[1]))
			} else if strTag[0] == "TargetBitrate" {
				pSvcParam.ITargetBitrate = 1000 * console.Atoi(strTag[1])
				if (pSvcParam.IRCMode != api.RC_OFF_MODE) && pSvcParam.ITargetBitrate <= 0 {
					fmt.Fprintf(os.Stderr, "Invalid target bitrate setting due to RC enabled. Check TargetBitrate field please!\n")
					return 1
				}
			} else if strTag[0] == "MaxOverallBitrate" {
				pSvcParam.IMaxBitrate = 1000 * console.Atoi(strTag[1])
				if (pSvcParam.IRCMode != api.RC_OFF_MODE) && pSvcParam.IMaxBitrate < 0 {
					fmt.Fprintf(os.Stderr, "Invalid max overall bitrate setting due to RC enabled. Check MaxOverallBitrate field please!\n")
					return 1
				}
			} else if strTag[0] == "MaxQp" {
				pSvcParam.IMaxQp = console.Atoi(strTag[1])
			} else if strTag[0] == "MinQp" {
				pSvcParam.IMinQp = console.Atoi(strTag[1])
			} else if strTag[0] == "EnableDenoise" {
				pSvcParam.BEnableDenoise = atoiBool(strTag[1])
			} else if strTag[0] == "EnableSceneChangeDetection" {
				pSvcParam.BEnableSceneChangeDetect = atoiBool(strTag[1])
			} else if strTag[0] == "EnableBackgroundDetection" {
				pSvcParam.BEnableBackgroundDetection = atoiBool(strTag[1])
			} else if strTag[0] == "EnableAdaptiveQuantization" {
				pSvcParam.BEnableAdaptiveQuant = atoiBool(strTag[1])
			} else if strTag[0] == "EnableFrameSkip" {
				pSvcParam.BEnableFrameSkip = atoiBool(strTag[1])
			} else if strTag[0] == "EnableLongTermReference" {
				pSvcParam.BEnableLongTermReference = atoiBool(strTag[1])
			} else if strTag[0] == "LongTermReferenceNumber" {
				pSvcParam.ILTRRefNum = console.Atoi(strTag[1])
			} else if strTag[0] == "LtrMarkPeriod" {
				pSvcParam.ILtrMarkPeriod = uint32(console.Atoi(strTag[1]))
			} else if strTag[0] == "LosslessLink" {
				pSvcParam.BIsLosslessLink = atoiBool(strTag[1])
			} else if strTag[0] == "NumLayers" {
				pSvcParam.ISpatialLayerNum = int32(int8(console.Atoi(strTag[1])))
				if pSvcParam.ISpatialLayerNum > encoder.MAX_DEPENDENCY_LAYER || pSvcParam.ISpatialLayerNum <= 0 {
					fmt.Fprintf(os.Stderr, "Invalid parameter in iSpatialLayerNum: %d.\n", pSvcParam.ISpatialLayerNum)
					iRet = 1
					break
				}
			} else if strTag[0] == "LayerCfg" {
				if len(strTag[1]) > 0 && iLayerCount >= 0 && int(iLayerCount) < len(sFileSet.strLayerCfgFile) {
					sFileSet.strLayerCfgFile[iLayerCount] = strTag[1]
				}
				//          pSvcParam.sDependencyLayers[iLayerCount].uiDependencyId = iLayerCount;
				iLayerCount++
			} else if strTag[0] == "PrefixNALAddingCtrl" {
				ctrl_flag := console.Atoi(strTag[1])
				if ctrl_flag > 1 {
					ctrl_flag = 1
				} else if ctrl_flag < 0 {
					ctrl_flag = 0
				}
				pSvcParam.BPrefixNalAddingCtrl = ctrl_flag != 0
			}
		}
	}

	kiActualLayerNum := int8(pSvcParam.ISpatialLayerNum)
	if iLayerCount < kiActualLayerNum {
		kiActualLayerNum = iLayerCount
	}
	if pSvcParam.ISpatialLayerNum > int32(kiActualLayerNum) { // fixed number of dependency layer due to parameter error in settings
		pSvcParam.ISpatialLayerNum = int32(kiActualLayerNum)
	}

	for iLayer := int8(0); iLayer < kiActualLayerNum && int(iLayer) < encoder.MAX_DEPENDENCY_LAYER; iLayer++ {
		cRdLayerCfg := console.NewCReadConfig(sFileSet.strLayerCfgFile[iLayer])
		r := ParseLayerConfig(cRdLayerCfg, int(iLayer), pSvcParam, sFileSet)
		cRdLayerCfg.Close()
		if -1 == r {
			iRet = 1
			break
		}
	}

	return iRet
}
