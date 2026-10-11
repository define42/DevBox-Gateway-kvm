// Command h264enc is the Wels SVC encoder console application, a port of
// codec/console/enc/src/welsenc.cpp.
//
// Usage:
//
//	h264enc -h
//	h264enc welsenc.cfg [options]
//	h264enc [options]          (at least two arguments, no .cfg file)
//
// Run "h264enc -h" for the list of options. The configuration file format is
// the one of testbin/welsenc.cfg and testbin/layer2.cfg.
package main

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"strings"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/console"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/encoder"
)

const ONLY_ENC_FRAMES_NUM = math.MaxInt32 // 2, INT_MAX // type the num you try to encode here, 2, 10, etc

/* Ctrl-C handler */
var g_iCtrlC = 0

var g_LevelSetting = int32(api.WELS_LOG_ERROR)

// stdout is buffered like C stdio stdout; it is flushed at exit.
var stdout = bufio.NewWriter(os.Stdout)

func printf(format string, a ...any) {
	fmt.Fprintf(stdout, format, a...)
}

// cFloat formats v like printf ("%f") in C.
func cFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		if math.Signbit(v) {
			return "-nan"
		}
		return "nan"
	case math.IsInf(v, 1):
		return "inf"
	case math.IsInf(v, -1):
		return "-inf"
	}
	return fmt.Sprintf("%f", v)
}

func PrintHelp() {
	printf("\n Wels SVC Encoder Usage:\n\n")
	printf(" Syntax: welsenc.exe -h\n")
	printf(" Syntax: welsenc.exe welsenc.cfg\n")
	printf(" Syntax: welsenc.exe welsenc.cfg [options]\n")

	printf("\n Supported Options:\n")
	printf("  -bf          Bit Stream File\n")
	printf("  -org         Original file, example: -org src.yuv\n")
	printf("  -sw          the source width\n")
	printf("  -sh          the source height\n")
	printf("  -utype       usage type\n")
	printf("  -savc        simulcast avc\n")
	printf("  -frms        Number of total frames to be encoded\n")
	printf("  -frin        input frame rate\n")
	printf("  -numtl       Temporal layer number (default: 1)\n")
	printf("  -iper        Intra period (default: -1) : must be a power of 2 of GOP size (or -1)\n")
	printf("  -nalsize     the Maximum NAL size. which should be larger than the each layer slicesize when slice mode equals to SM_SIZELIMITED_SLICE\n")
	printf("  -spsid       SPS/PPS id strategy: 0:const, 1: increase, 2: sps list, 3: sps list and pps increase, 4: sps/pps list\n")
	printf("  -cabac       Entropy coding mode(0:cavlc 1:cabac \n")
	printf("  -complexity  Complexity mode (default: 0),0: low complexity, 1: medium complexity, 2: high complexity\n")
	printf("  -denois      Control denoising  (default: 0)\n")
	printf("  -scene       Control scene change detection (default: 0)\n")
	printf("  -bgd         Control background detection (default: 0)\n")
	printf("  -aq          Control adaptive quantization (default: 0)\n")
	printf("  -ltr         Control long term reference (default: 0)\n")
	printf("  -ltrnum      Control the number of long term reference((1-4):screen LTR,(1-2):video LTR \n")
	printf("  -ltrper      Control the long term reference marking period \n")
	printf("  -threadIdc   0: auto(dynamic imp. internal encoder); 1: multiple threads imp. disabled; > 1: count number of threads \n")
	printf("  -loadbalancing   0: turn off loadbalancing between slices when multi-threading available; 1: (default value) turn on loadbalancing between slices when multi-threading available\n")
	printf("  -deblockIdc  Loop filter idc (0: on, 1: off, \n")
	printf("  -alphaOffset AlphaOffset(-6..+6): valid range \n")
	printf("  -betaOffset  BetaOffset (-6..+6): valid range\n")
	printf("  -rc          rate control mode: -1-rc off; 0-quality mode; 1-bitrate mode; 2: buffer based mode,can't control bitrate; 3: bitrate mode based on timestamp input;\n")
	printf("  -tarb        Overall target bitrate\n")
	printf("  -maxbrTotal  Overall max bitrate\n")
	printf("  -maxqp       Maximum Qp (default: %d, or for screen content usage: %d)\n", encoder.QP_MAX_VALUE, encoder.MAX_SCREEN_QP)
	printf("  -minqp       Minimum Qp (default: %d, or for screen content usage: %d)\n", encoder.QP_MIN_VALUE, encoder.MIN_SCREEN_QP)
	printf("  -numl        Number Of Layers: Must exist with layer_cfg file and the number of input layer_cfg file must equal to the value set by this command\n")
	printf("  The options below are layer-based: (need to be set with layer id)\n")
	printf("  -lconfig     (Layer) (spatial layer configure file)\n")
	printf("  -drec        (Layer) (reconstruction file);example: -drec 0 rec.yuv.  Setting the reconstruction file, this will only functioning when dumping reconstruction is enabled\n")
	printf("  -dprofile    (Layer) (layer profile);example: -dprofile 0 66.  Setting the layer profile, this should be the same for all layers\n")
	printf("  -dw          (Layer) (output width)\n")
	printf("  -dh          (Layer) (output height)\n")
	printf("  -frout       (Layer) (output frame rate)\n")
	printf("  -lqp         (Layer) (base quality layer qp : must work with -ldeltaqp or -lqparr)\n")
	printf("  -ltarb       (Layer) (spatial layer target bitrate)\n")
	printf("  -lmaxb       (Layer) (spatial layer max bitrate)\n")
	printf("  -slcmd       (Layer) (spatial layer slice mode): pls refer to layerX.cfg for details ( -slcnum: set target slice num; -slcsize: set target slice size constraint ; -slcmbnum: set the first slice mb num under some slice modes) \n")
	printf("  -trace       (Level)\n")
	printf("  -fixrc       Enable fix RC overshoot(default: 1)\n")
	printf("\n")
}

// layerArg parses the layer index of a layer-based option. Indices outside
// the spatial layer array (undefined behaviour in the C application) make the
// option be ignored.
func layerArg(pSvcParam *api.SEncParamExt, s string) *api.SSpatialLayerConfig {
	iLayer := uint32(console.Atoi(s))
	if iLayer >= uint32(len(pSvcParam.SSpatialLayers)) {
		return &api.SSpatialLayerConfig{}
	}
	return &pSvcParam.SSpatialLayers[iLayer]
}

func ParseCommandLine(argv []string, pSrcPic *api.SSourcePicture, pSvcParam *api.SEncParamExt, sFileSet *SFilesSet) int {
	argc := len(argv)
	var sLayerCtx [api.MAX_SPATIAL_LAYER_NUM]SLayerPEncCtx
	n := 0

	for n < argc {
		pCommand := argv[n]
		n++

		if pCommand == "-bf" && (n < argc) {
			sFileSet.strBsFile = argv[n]
			n++
		} else if pCommand == "-utype" && (n < argc) {
			pSvcParam.IUsageType = api.EUsageType(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-savc" && (n < argc) {
			pSvcParam.BSimulcastAVC = atoiBool(argv[n])
			n++
		} else if pCommand == "-org" && (n < argc) {
			sFileSet.strSeqFile = argv[n]
			n++
		} else if pCommand == "-sw" && (n < argc) { //source width
			pSrcPic.IPicWidth = console.Atoi(argv[n])
			n++
		} else if pCommand == "-sh" && (n < argc) { //source height
			pSrcPic.IPicHeight = console.Atoi(argv[n])
			n++
		} else if pCommand == "-frms" && (n < argc) {
			sFileSet.uiFrameToBeCoded = uint32(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-frin" && (n < argc) {
			pSvcParam.FMaxFrameRate = float32(console.Atof(argv[n]))
			n++
		} else if pCommand == "-numtl" && (n < argc) {
			pSvcParam.ITemporalLayerNum = console.Atoi(argv[n])
			n++
		} else if pCommand == "-mfile" && (n < argc) {
			sFileSet.bEnableMultiBsFile = atoiBool(argv[n])
			n++
		} else if pCommand == "-iper" && (n < argc) {
			pSvcParam.UiIntraPeriod = uint32(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-nalsize" && (n < argc) {
			pSvcParam.UiMaxNalSize = uint32(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-spsid" && (n < argc) {
			pSvcParam.ESpsPpsIdStrategy = spsPpsIdStrategy(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-cabac" && (n < argc) {
			pSvcParam.IEntropyCodingModeFlag = console.Atoi(argv[n])
			n++
		} else if pCommand == "-complexity" && (n < argc) {
			pSvcParam.IComplexityMode = api.ECOMPLEXITY_MODE(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-denois" && (n < argc) {
			pSvcParam.BEnableDenoise = atoiBool(argv[n])
			n++
		} else if pCommand == "-scene" && (n < argc) {
			pSvcParam.BEnableSceneChangeDetect = atoiBool(argv[n])
			n++
		} else if pCommand == "-bgd" && (n < argc) {
			pSvcParam.BEnableBackgroundDetection = atoiBool(argv[n])
			n++
		} else if pCommand == "-aq" && (n < argc) {
			pSvcParam.BEnableAdaptiveQuant = atoiBool(argv[n])
			n++
		} else if pCommand == "-fs" && (n < argc) {
			pSvcParam.BEnableFrameSkip = atoiBool(argv[n])
			n++
		} else if pCommand == "-fixrc" && (n < argc) {
			pSvcParam.BFixRCOverShoot = atoiBool(argv[n])
			n++
		} else if pCommand == "-idrBitrateRatio" && (n < argc) {
			pSvcParam.IIdrBitrateRatio = console.Atoi(argv[n])
			n++
		} else if pCommand == "-ltr" && (n < argc) {
			pSvcParam.BEnableLongTermReference = atoiBool(argv[n])
			n++
		} else if pCommand == "-ltrnum" && (n < argc) {
			pSvcParam.ILTRRefNum = console.Atoi(argv[n])
			n++
		} else if pCommand == "-ltrper" && (n < argc) {
			pSvcParam.ILtrMarkPeriod = uint32(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-threadIdc" && (n < argc) {
			pSvcParam.IMultipleThreadIdc = uint16(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-loadbalancing" && (n < argc) {
			pSvcParam.BUseLoadBalancing = atoiBool(argv[n])
			n++
		} else if pCommand == "-deblockIdc" && (n < argc) {
			pSvcParam.ILoopFilterDisableIdc = console.Atoi(argv[n])
			n++
		} else if pCommand == "-alphaOffset" && (n < argc) {
			pSvcParam.ILoopFilterAlphaC0Offset = console.Atoi(argv[n])
			n++
		} else if pCommand == "-betaOffset" && (n < argc) {
			pSvcParam.ILoopFilterBetaOffset = console.Atoi(argv[n])
			n++
		} else if pCommand == "-rc" && (n < argc) {
			pSvcParam.IRCMode = api.RC_MODES(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-trace" && (n < argc) {
			g_LevelSetting = console.Atoi(argv[n])
			n++
		} else if pCommand == "-tarb" && (n < argc) {
			pSvcParam.ITargetBitrate = 1000 * console.Atoi(argv[n])
			n++
		} else if pCommand == "-maxbrTotal" && (n < argc) {
			pSvcParam.IMaxBitrate = 1000 * console.Atoi(argv[n])
			n++
		} else if pCommand == "-maxqp" && (n < argc) {
			pSvcParam.IMaxQp = console.Atoi(argv[n])
			n++
		} else if pCommand == "-minqp" && (n < argc) {
			pSvcParam.IMinQp = console.Atoi(argv[n])
			n++
		} else if pCommand == "-numl" && (n < argc) {
			pSvcParam.ISpatialLayerNum = console.Atoi(argv[n])
			n++
		} else if pCommand == "-lconfig" && (n < argc) {
			iLayer := uint32(console.Atoi(argv[n]))
			n++
			strFile := ""
			if n < argc { // argv[argc] is NULL in C
				strFile = argv[n]
			}
			n++
			if iLayer >= encoder.MAX_DEPENDENCY_LAYER {
				continue // out of range: undefined behaviour in the C application
			}
			sFileSet.strLayerCfgFile[iLayer] = strFile
			cRdLayerCfg := console.NewCReadConfig(sFileSet.strLayerCfgFile[iLayer])
			r := ParseLayerConfig(cRdLayerCfg, int(iLayer), pSvcParam, sFileSet)
			cRdLayerCfg.Close()
			if -1 == r {
				return 1
			}
		} else if pCommand == "-dprofile" && (n+1 < argc) {
			pDLayer := layerArg(pSvcParam, argv[n])
			n++
			pDLayer.UiProfileIdc = api.EProfileIdc(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-drec" && (n+1 < argc) {
			iLayer := uint32(console.Atoi(argv[n]))
			n++
			iLen := len(argv[n])
			if iLen >= common.MAX_FNAME_LEN {
				return 1
			}
			if iLayer < encoder.MAX_DEPENDENCY_LAYER {
				sFileSet.sRecFileName[iLayer] = argv[n]
			}
			n++
		} else if pCommand == "-dw" && (n+1 < argc) {
			pDLayer := layerArg(pSvcParam, argv[n])
			n++
			pDLayer.IVideoWidth = console.Atoi(argv[n])
			n++
		} else if pCommand == "-dh" && (n+1 < argc) {
			pDLayer := layerArg(pSvcParam, argv[n])
			n++
			pDLayer.IVideoHeight = console.Atoi(argv[n])
			n++
		} else if pCommand == "-frout" && (n+1 < argc) {
			pDLayer := layerArg(pSvcParam, argv[n])
			n++
			pDLayer.FFrameRate = float32(console.Atof(argv[n]))
			n++
		} else if pCommand == "-lqp" && (n+1 < argc) {
			iLayer := uint32(console.Atoi(argv[n]))
			pDLayer := layerArg(pSvcParam, argv[n])
			n++
			v := console.Atoi(argv[n])
			n++
			if iLayer < uint32(len(sLayerCtx)) {
				sLayerCtx[iLayer].iDLayerQp = v
			}
			pDLayer.IDLayerQp = v
		} else if pCommand == "-ltarb" && (n+1 < argc) {
			//sLayerCtx[iLayer].num_quality_layers = pDLayer->num_quality_layers = 1;
			pDLayer := layerArg(pSvcParam, argv[n])
			n++
			pDLayer.ISpatialBitrate = 1000 * console.Atoi(argv[n])
			n++
		} else if pCommand == "-lmaxb" && (n+1 < argc) {
			pDLayer := layerArg(pSvcParam, argv[n])
			n++
			pDLayer.IMaxSpatialBitrate = 1000 * console.Atoi(argv[n])
			n++
		} else if pCommand == "-slcmd" && (n+1 < argc) {
			pDLayer := layerArg(pSvcParam, argv[n])
			n++

			switch console.Atoi(argv[n]) {
			case 0:
				pDLayer.SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
			case 1:
				pDLayer.SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
			case 2:
				pDLayer.SSliceArgument.UiSliceMode = api.SM_RASTER_SLICE
			case 3:
				pDLayer.SSliceArgument.UiSliceMode = api.SM_SIZELIMITED_SLICE
			default:
				pDLayer.SSliceArgument.UiSliceMode = api.SM_RESERVED
			}
			n++
		} else if pCommand == "-slcsize" && (n+1 < argc) {
			pDLayer := layerArg(pSvcParam, argv[n])
			n++
			pDLayer.SSliceArgument.UiSliceSizeConstraint = uint32(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-slcnum" && (n+1 < argc) {
			pDLayer := layerArg(pSvcParam, argv[n])
			n++
			pDLayer.SSliceArgument.UiSliceNum = uint32(console.Atoi(argv[n]))
			n++
		} else if pCommand == "-slcmbnum" && (n+1 < argc) {
			pDLayer := layerArg(pSvcParam, argv[n])
			n++
			pDLayer.SSliceArgument.UiSliceMbNum[0] = uint32(console.Atoi(argv[n]))
			n++
		}
	}
	return 0
}

func FillSpecificParameters(sParam *api.SEncParamExt) int {
	/* Test for temporal, spatial, SNR scalability */
	sParam.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	sParam.FMaxFrameRate = 60.0     // input frame rate
	sParam.IPicWidth = 1280         // width of picture in samples
	sParam.IPicHeight = 720         // height of picture in samples
	sParam.ITargetBitrate = 2500000 // target bitrate desired
	sParam.IMaxBitrate = api.UNSPECIFIED_BIT_RATE
	sParam.IRCMode = api.RC_QUALITY_MODE     //  rc mode control
	sParam.ITemporalLayerNum = 3             // layer number at temporal level
	sParam.ISpatialLayerNum = 4              // layer number at spatial level
	sParam.BEnableDenoise = false            // denoise control
	sParam.BEnableBackgroundDetection = true // background detection control
	sParam.BEnableAdaptiveQuant = true       // adaptive quantization control
	sParam.BEnableFrameSkip = true           // frame skipping
	sParam.BEnableLongTermReference = false  // long term reference control
	sParam.ILtrMarkPeriod = 30
	sParam.UiIntraPeriod = 320 // period of Intra frame
	sParam.ESpsPpsIdStrategy = api.INCREASING_ID
	sParam.BPrefixNalAddingCtrl = false
	sParam.IComplexityMode = api.LOW_COMPLEXITY
	sParam.BSimulcastAVC = false
	sParam.BFixRCOverShoot = true
	sParam.IIdrBitrateRatio = encoder.IDR_BITRATE_RATIO * 100
	iIndexLayer := 0
	sParam.SSpatialLayers[iIndexLayer].UiProfileIdc = api.PRO_BASELINE
	sParam.SSpatialLayers[iIndexLayer].IVideoWidth = 160
	sParam.SSpatialLayers[iIndexLayer].IVideoHeight = 90
	sParam.SSpatialLayers[iIndexLayer].FFrameRate = 7.5
	sParam.SSpatialLayers[iIndexLayer].ISpatialBitrate = 64000
	sParam.SSpatialLayers[iIndexLayer].IMaxSpatialBitrate = api.UNSPECIFIED_BIT_RATE
	sParam.SSpatialLayers[iIndexLayer].SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE

	iIndexLayer++
	sParam.SSpatialLayers[iIndexLayer].UiProfileIdc = api.PRO_SCALABLE_BASELINE
	sParam.SSpatialLayers[iIndexLayer].IVideoWidth = 320
	sParam.SSpatialLayers[iIndexLayer].IVideoHeight = 180
	sParam.SSpatialLayers[iIndexLayer].FFrameRate = 15.0
	sParam.SSpatialLayers[iIndexLayer].ISpatialBitrate = 160000
	sParam.SSpatialLayers[iIndexLayer].IMaxSpatialBitrate = api.UNSPECIFIED_BIT_RATE
	sParam.SSpatialLayers[iIndexLayer].SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE

	iIndexLayer++
	sParam.SSpatialLayers[iIndexLayer].UiProfileIdc = api.PRO_SCALABLE_BASELINE
	sParam.SSpatialLayers[iIndexLayer].IVideoWidth = 640
	sParam.SSpatialLayers[iIndexLayer].IVideoHeight = 360
	sParam.SSpatialLayers[iIndexLayer].FFrameRate = 30.0
	sParam.SSpatialLayers[iIndexLayer].ISpatialBitrate = 512000
	sParam.SSpatialLayers[iIndexLayer].IMaxSpatialBitrate = api.UNSPECIFIED_BIT_RATE
	sParam.SSpatialLayers[iIndexLayer].SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
	sParam.SSpatialLayers[iIndexLayer].SSliceArgument.UiSliceNum = 1

	iIndexLayer++
	sParam.SSpatialLayers[iIndexLayer].UiProfileIdc = api.PRO_SCALABLE_BASELINE
	sParam.SSpatialLayers[iIndexLayer].IVideoWidth = 1280
	sParam.SSpatialLayers[iIndexLayer].IVideoHeight = 720
	sParam.SSpatialLayers[iIndexLayer].FFrameRate = 30.0
	sParam.SSpatialLayers[iIndexLayer].ISpatialBitrate = 1500000
	sParam.SSpatialLayers[iIndexLayer].IMaxSpatialBitrate = api.UNSPECIFIED_BIT_RATE
	sParam.SSpatialLayers[iIndexLayer].SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
	sParam.SSpatialLayers[iIndexLayer].SSliceArgument.UiSliceNum = 1

	fMaxFr := sParam.SSpatialLayers[sParam.ISpatialLayerNum-1].FFrameRate
	for i := sParam.ISpatialLayerNum - 2; i >= 0; i-- {
		if sParam.SSpatialLayers[i].FFrameRate > fMaxFr+common.EPSN {
			fMaxFr = sParam.SSpatialLayers[i].FFrameRate
		}
	}
	sParam.FMaxFrameRate = fMaxFr

	return 0
}

func ProcessEncoding(pPtrEnc api.ISVCEncoder, argv []string, bConfigFile bool) (iRet int) {
	if pPtrEnc == nil {
		return 1
	}

	var sFbi api.SFrameBSInfo
	var sSvcParam api.SEncParamExt
	var iStart, iTotal int64

	// Preparing encoding process
	var pFileYUV *os.File
	iActualFrameEncodedCount := int32(0)
	iFrameIdx := int32(0)
	iTotalFrameMax := int32(-1)
	var pYUV []uint8
	var iSourceWidth, iSourceHeight, kiPicResSize uint32
	// Inactive with sink with output file handler
	var pFpBs [4]*bufio.Writer
	var pFpBsFile [4]*os.File
	var fs SFilesSet
	// for configuration file
	var cRdCfg console.CReadConfig
	iParsedNum := 1

	pPtrEnc.GetDefaultParams(&sSvcParam)
	fs.bEnableMultiBsFile = false

	FillSpecificParameters(&sSvcParam)
	pSrcPic := &api.SSourcePicture{}
	//fill default pSrcPic
	pSrcPic.IColorFormat = int32(api.VideoFormatI420)
	pSrcPic.UiTimeStamp = 0

	defer func() {
		// INSIDE_MEM_FREE:
		// The C code closes only the first iSpatialLayerNum files; any other
		// open file is flushed by exit(). Close them all here.
		for i := 0; i < len(pFpBs); i++ {
			if pFpBs[i] != nil {
				pFpBs[i].Flush()
				pFpBsFile[i].Close()
				pFpBs[i] = nil
			}
		}
		// Destruction memory introduced in this routine
		if pFileYUV != nil {
			pFileYUV.Close()
			pFileYUV = nil
		}
		cRdCfg.Close()
	}()

	// if configure file exit, reading configure file firstly
	if bConfigFile {
		iParsedNum = 2
		cRdCfg.Openf(argv[1])
		if !cRdCfg.ExistFile() {
			fmt.Fprintf(os.Stderr, "Specified file: %s not exist, maybe invalid path or parameter settting.\n",
				cRdCfg.GetFileName())
			return 1
		}
		iRet = ParseConfig(&cRdCfg, pSrcPic, &sSvcParam, &fs)
		if iRet != 0 {
			fmt.Fprintf(os.Stderr, "parse svc parameter config file failed.\n")
			return 1
		}
	}
	if ParseCommandLine(argv[iParsedNum:], pSrcPic, &sSvcParam, &fs) != 0 {
		printf("parse pCommand line failed\n")
		return 1
	}
	pPtrEnc.SetOption(api.ENCODER_OPTION_TRACE_LEVEL, &g_LevelSetting)
	//finish reading the configurations
	iSourceWidth = uint32(pSrcPic.IPicWidth)
	iSourceHeight = uint32(pSrcPic.IPicHeight)
	kiPicResSize = iSourceWidth * iSourceHeight * 3 >> 1

	pYUV = make([]uint8, kiPicResSize)

	//update pSrcPic
	pSrcPic.IStride[0] = int32(iSourceWidth)
	pSrcPic.IStride[1] = pSrcPic.IStride[0] >> 1
	pSrcPic.IStride[2] = pSrcPic.IStride[1]

	kiLumaSize := iSourceWidth * iSourceHeight
	pSrcPic.PData[0] = pYUV
	pSrcPic.PData[1] = pYUV[kiLumaSize:]
	pSrcPic.PData[2] = pYUV[kiLumaSize+(kiLumaSize>>2):]

	//update sSvcParam
	sSvcParam.IPicWidth = 0
	sSvcParam.IPicHeight = 0
	for iLayer := 0; iLayer < int(sSvcParam.ISpatialLayerNum) && iLayer < len(sSvcParam.SSpatialLayers); iLayer++ {
		pDLayer := &sSvcParam.SSpatialLayers[iLayer]
		sSvcParam.IPicWidth = max(sSvcParam.IPicWidth, pDLayer.IVideoWidth)
		sSvcParam.IPicHeight = max(sSvcParam.IPicHeight, pDLayer.IVideoHeight)
	}
	//if target output resolution is not set, use the source size
	if sSvcParam.IPicWidth == 0 {
		sSvcParam.IPicWidth = int32(iSourceWidth)
	}
	if sSvcParam.IPicHeight == 0 {
		sSvcParam.IPicHeight = int32(iSourceHeight)
	}

	iTotalFrameMax = int32(fs.uiFrameToBeCoded)
	//  sSvcParam.bSimulcastAVC = true;
	if int32(api.CmResultSuccess) != pPtrEnc.InitializeExt(&sSvcParam) { // SVC encoder initialization
		fmt.Fprintf(os.Stderr, "SVC encoder Initialize failed\n")
		return 1
	}
	for iLayer := 0; iLayer < encoder.MAX_DEPENDENCY_LAYER; iLayer++ {
		if fs.sRecFileName[iLayer] != "" {
			var sDumpLayer api.SDumpLayer
			sDumpLayer.ILayer = int32(iLayer)
			sDumpLayer.PFileName = fs.sRecFileName[iLayer]
			if int32(api.CmResultSuccess) != pPtrEnc.SetOption(api.ENCODER_OPTION_DUMP_FILE, &sDumpLayer) {
				fmt.Fprintf(os.Stderr, "SetOption ENCODER_OPTION_DUMP_FILE failed!\n")
				return 1
			}
		}
	}
	openBs := func(i int, name string) bool {
		f, err := os.Create(name)
		if err != nil {
			return false
		}
		pFpBsFile[i] = f
		pFpBs[i] = bufio.NewWriterSize(f, 1<<20)
		return true
	}
	// Inactive with sink with output file handler
	if len(fs.strBsFile) > 0 {
		bFileOpenErr := false
		if sSvcParam.ISpatialLayerNum == 1 || !fs.bEnableMultiBsFile {
			bFileOpenErr = !openBs(0, fs.strBsFile)
		} else { //enable multi bs file writing
			add_info := [4]string{"_layer0", "_layer1", "_layer2", "_layer3"}
			found := strings.LastIndexByte(fs.strBsFile, '.')
			if found < 0 {
				found = len(fs.strBsFile) // std::out_of_range in C++
			}
			for i := 0; i < int(sSvcParam.ISpatialLayerNum) && i < len(add_info); i++ {
				filename_layer := fs.strBsFile[:found] + add_info[i] + fs.strBsFile[found:]
				if !openBs(i, filename_layer) {
					bFileOpenErr = true
				}
			}
		}
		if bFileOpenErr {
			fmt.Fprintf(os.Stderr, "Can not open file (%s) to write bitstream!\n", fs.strBsFile)
			return 1
		}
	} else {
		fmt.Fprintf(os.Stderr, "Don't set the proper bitstream filename!\n")
		return 1
	}

	var err error
	pFileYUV, err = os.Open(fs.strSeqFile)
	if err == nil {
		if fi, err := pFileYUV.Stat(); err == nil && fi.Mode().IsRegular() {
			i_size := fi.Size()
			iTotalFrameMax = max(int32(i_size/int64(kiPicResSize)), iTotalFrameMax)
		}
	} else {
		pFileYUV = nil
		fmt.Fprintf(os.Stderr, "Unable to open source sequence file (%s), check corresponding path!\n",
			fs.strSeqFile)
		return 1
	}
	rdYUV := bufio.NewReaderSize(pFileYUV, 1<<20)

	iFrameIdx = 0
	for iFrameIdx < iTotalFrameMax && ((int32(fs.uiFrameToBeCoded) <= 0) ||
		(iFrameIdx < int32(fs.uiFrameToBeCoded))) {

		// Only encoded some limited frames here
		if iActualFrameEncodedCount >= ONLY_ENC_FRAMES_NUM {
			break
		}
		bCanBeRead := false
		nRead, _ := io.ReadFull(rdYUV, pYUV)
		bCanBeRead = uint32(nRead) == kiPicResSize

		if !bCanBeRead {
			break
		}
		// To encoder this frame
		iStart = common.WelsTime()
		fFrameDur := float32(1000) / sSvcParam.FMaxFrameRate
		pSrcPic.UiTimeStamp = int64(common.WELS_ROUND(float32(float32(iFrameIdx) * fFrameDur)))
		iEncFrames := pPtrEnc.EncodeFrame(pSrcPic, &sFbi)
		iTotal += common.WelsTime() - iStart
		iFrameIdx++
		if api.VideoFrameTypeSkip == sFbi.EFrameType {
			continue
		}

		if iEncFrames == int32(api.CmResultSuccess) {
			iLayer := 0
			for iLayer < int(sFbi.ILayerNum) {
				pLayerBsInfo := &sFbi.SLayerInfo[iLayer]
				iLayerSize := int32(0)
				for iNalIdx := pLayerBsInfo.INalCount - 1; iNalIdx >= 0; iNalIdx-- {
					iLayerSize += pLayerBsInfo.PNalLengthInByte[iNalIdx]
				}
				bs := pLayerBsInfo.PBsBuf[:iLayerSize]
				if sSvcParam.ISpatialLayerNum == 1 || !fs.bEnableMultiBsFile {
					pFpBs[0].Write(bs) // write pure bit stream into file
				} else { //multi bs file write
					if pLayerBsInfo.UiSpatialId == 0 {
						five_bits := pLayerBsInfo.PBsBuf[4] & 0x1f
						if (five_bits == 0x07) || (five_bits == 0x08) { //sps or pps
							for i := 0; i < int(sSvcParam.ISpatialLayerNum); i++ {
								pFpBs[i].Write(bs)
							}
						} else {
							pFpBs[0].Write(bs)
						}
					} else {
						pFpBs[pLayerBsInfo.UiSpatialId].Write(bs)
					}
				}
				iLayer++
			}
			iActualFrameEncodedCount++ // excluding skipped frame time
		} else {
			fmt.Fprintf(os.Stderr, "EncodeFrame(), ret: %d, frame index: %d.\n", iEncFrames, iFrameIdx)
		}
	}

	if iActualFrameEncodedCount > 0 {
		dElapsed := float64(iTotal) / 1e6
		printf("Width:\t\t%d\nHeight:\t\t%d\nFrames:\t\t%d\nencode time:\t%s sec\nFPS:\t\t%s fps\n",
			sSvcParam.IPicWidth, sSvcParam.IPicHeight,
			iActualFrameEncodedCount, cFloat(dElapsed), cFloat((float64(iActualFrameEncodedCount)*1.0)/dElapsed))
	}
	return iRet
}

func CreateSVCEncHandle(ppEncoder *api.ISVCEncoder) int32 {
	return encoder.WelsCreateSVCEncoder(ppEncoder)
}

func DestroySVCEncHandle(pEncoder api.ISVCEncoder) {
	if pEncoder != nil {
		encoder.WelsDestroySVCEncoder(pEncoder)
	}
}

func main() {
	iRet := encMain(os.Args)
	stdout.Flush()
	os.Exit(iRet)
}

func encMain(argv []string) int {
	argc := len(argv)
	var pSVCEncoder api.ISVCEncoder
	iRet := 0

	/* Control-C handler */
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		for range sigCh {
			g_iCtrlC = 1
		}
	}()

	exit := func() int {
		DestroySVCEncHandle(pSVCEncoder)
		PrintHelp()
		return 1
	}

	iRet = int(CreateSVCEncHandle(&pSVCEncoder))
	if iRet != 0 {
		stdout.WriteString("WelsCreateSVCEncoder() failed!!\n")
		return exit()
	}

	if argc < 2 {
		return exit()
	} else {
		if !strings.Contains(argv[1], ".cfg") { // check configuration type (like .cfg?)
			if argc > 2 {
				iRet = ProcessEncoding(pSVCEncoder, argv, false)
				if iRet != 0 {
					return exit()
				}
			} else if argc == 2 && argv[1] == "-h" {
				PrintHelp()
			} else {
				stdout.WriteString("You specified pCommand is invalid!!\n")
				return exit()
			}
		} else {
			iRet = ProcessEncoding(pSVCEncoder, argv, true)
			if iRet > 0 {
				return exit()
			}
		}
	}

	DestroySVCEncHandle(pSVCEncoder)
	return 0
}
