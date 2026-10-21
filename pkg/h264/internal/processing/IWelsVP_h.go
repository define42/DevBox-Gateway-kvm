// Package processing is a port of codec/processing (the WelsVP video
// pre-processing library used by the encoder).
//
// This file ports codec/processing/interface/IWelsVP.h.
//
// Enumerations are declared as aliases of int32 so that the C usage
// (`int32_t iMethodIdx = METHOD_DENOISE; vp->Process (iMethodIdx, ...)`,
// `if (vp->Process (...) == 0)`) translates without conversions.
package processing

const (
	WELSVP_MAJOR_VERSION = 1
	WELSVP_MINOR_VERSION = 1
	WELSVP_VERSION       = (WELSVP_MAJOR_VERSION << 8) + WELSVP_MINOR_VERSION
)

// EResult is the result code of every IWelsVP method.
type EResult = int32

const (
	RET_SUCCESS      EResult = 0
	RET_FAILED       EResult = -1
	RET_INVALIDPARAM EResult = -2
	RET_OUTOFMEMORY  EResult = -3
	RET_NOTSUPPORTED EResult = -4
	RET_UNEXPECTED   EResult = -5
	RET_NEEDREINIT   EResult = -6
)

type EVideoFormat = int32

const (
	VIDEO_FORMAT_NULL EVideoFormat = 0 /* invalid format   */
	/*rgb color formats*/
	VIDEO_FORMAT_RGB    EVideoFormat = 1 /* rgb 24bits       */
	VIDEO_FORMAT_RGBA   EVideoFormat = 2 /* rgba             */
	VIDEO_FORMAT_RGB555 EVideoFormat = 3 /* rgb555           */
	VIDEO_FORMAT_RGB565 EVideoFormat = 4 /* rgb565           */
	VIDEO_FORMAT_BGR    EVideoFormat = 5 /* bgr 24bits       */
	VIDEO_FORMAT_BGRA   EVideoFormat = 6 /* bgr 32bits       */
	VIDEO_FORMAT_ABGR   EVideoFormat = 7 /* abgr             */
	VIDEO_FORMAT_ARGB   EVideoFormat = 8 /* argb             */

	/*yuv color formats*/
	VIDEO_FORMAT_YUY2     EVideoFormat = 20 /* yuy2             */
	VIDEO_FORMAT_YVYU     EVideoFormat = 21 /* yvyu             */
	VIDEO_FORMAT_UYVY     EVideoFormat = 22 /* uyvy             */
	VIDEO_FORMAT_I420     EVideoFormat = 23 /* yuv 4:2:0 planar */
	VIDEO_FORMAT_YV12     EVideoFormat = 24 /* yuv 4:2:0 planar */
	VIDEO_FORMAT_INTERNAL EVideoFormat = 25 /* Only Used for SVC decoder testbed */
	VIDEO_FORMAT_NV12     EVideoFormat = 26 /* y planar + uv packed */
	VIDEO_FORMAT_I422     EVideoFormat = 27 /* yuv 4:2:2 planar */
	VIDEO_FORMAT_I444     EVideoFormat = 28 /* yuv 4:4:4 planar */
	VIDEO_FORMAT_YUYV     EVideoFormat = 20 /* yuv 4:2:2 packed */

	VIDEO_FORMAT_RGB24      EVideoFormat = 1
	VIDEO_FORMAT_RGB32      EVideoFormat = 2
	VIDEO_FORMAT_RGB24_INV  EVideoFormat = 5
	VIDEO_FORMAT_RGB32_INV  EVideoFormat = 6
	VIDEO_FORMAT_RGB555_INV EVideoFormat = 7
	VIDEO_FORMAT_RGB565_INV EVideoFormat = 8
	VIDEO_FORMAT_YUV2       EVideoFormat = 21
	VIDEO_FORMAT_420        EVideoFormat = 23

	// VIDEO_FORMAT_VFlip is 0x80000000 in C; stored here as the same bit
	// pattern in an int32.
	VIDEO_FORMAT_VFlip EVideoFormat = -0x80000000
)

type EPixMapBufferProperty = int32

const (
	BUFFER_HOSTMEM EPixMapBufferProperty = 0
	BUFFER_SURFACE EPixMapBufferProperty = 1
)

type SRect struct {
	IRectTop    int32
	IRectLeft   int32
	IRectWidth  int32
	IRectHeight int32
}

// SPixMap describes up to three pixel planes.
//
// C: `void* pPixel[3]`. Go: PPixel[i] is a []uint8 and IPixelOff[i] is the
// index of the plane origin (top-left pixel, i.e. the C pointer value) inside
// PPixel[i]. IPixelOff is an addition to the C struct.
//
// Two ways of filling it are supported:
//
//   - Recommended (exact C semantics): PPixel[i] = the whole picture
//     allocation (e.g. SPicture.PData[i]) and IPixelOff[i] = the plane origin
//     offset inside it (e.g. SPicture.IDataOff[i]). This lets modules reach
//     into the picture padding exactly as the C code does.
//   - PPixel[i] = a sub-slice that starts at the plane origin, IPixelOff[i] = 0.
//
// The second form is only safe if no module reads above/left of the origin.
// That holds for every module except METHOD_COMPLEXITY_ANALYSIS_SCREEN with a
// detected scroll motion vector: CComplexityAnalysisScreen reads the
// reference plane at `pRef - iScrollMvY * iStride + iScrollMvX`, which can be
// above the origin (into the top padding). Use the first form for the encoder.
//
// A plane is "NULL" when PPixel[i] == nil.
type SPixMap struct {
	PPixel      [3][]uint8
	IPixelOff   [3]int
	ISizeInBits int32
	IStride     [3]int32
	SRect       SRect
	EFormat     EVideoFormat
	EProperty   EPixMapBufferProperty //not use? to remove? but how about the size of SPixMap?
}

type EMethods = int32

const (
	METHOD_NULL                          EMethods = 0
	METHOD_COLORSPACE_CONVERT            EMethods = 1 //not support yet
	METHOD_DENOISE                       EMethods = 2
	METHOD_SCENE_CHANGE_DETECTION_VIDEO  EMethods = 3
	METHOD_SCENE_CHANGE_DETECTION_SCREEN EMethods = 4
	METHOD_DOWNSAMPLE                    EMethods = 5
	METHOD_VAA_STATISTICS                EMethods = 6
	METHOD_BACKGROUND_DETECTION          EMethods = 7
	METHOD_ADAPTIVE_QUANT                EMethods = 8
	METHOD_COMPLEXITY_ANALYSIS           EMethods = 9
	METHOD_COMPLEXITY_ANALYSIS_SCREEN    EMethods = 10
	METHOD_IMAGE_ROTATE                  EMethods = 11
	METHOD_SCROLL_DETECTION              EMethods = 12
	METHOD_MASK                          EMethods = 13
)

//-----------------------------------------------------------------//
//  Algorithm parameters define
//-----------------------------------------------------------------//

type ESceneChangeIdc = int32

const (
	SIMILAR_SCENE        ESceneChangeIdc = 0 //similar scene
	MEDIUM_CHANGED_SCENE ESceneChangeIdc = 1 //medium changed scene
	LARGE_CHANGED_SCENE  ESceneChangeIdc = 2 //large changed scene
)

type EStaticBlockIdc = int32

const (
	NO_STATIC            EStaticBlockIdc = 0 // motion block
	COLLOCATED_STATIC    EStaticBlockIdc = 1 // collocated static block
	SCROLLED_STATIC      EStaticBlockIdc = 2 // scrolled static block
	BLOCK_STATIC_IDC_ALL EStaticBlockIdc = 3
)

type SScrollDetectionParam struct {
	SMaskRect          SRect
	BMaskInfoAvailable bool
	IScrollMvX         int32
	IScrollMvY         int32
	BScrollDetectFlag  bool // 0:false ; 1:ltr; 2: scene change
}

type SSceneChangeResult struct {
	ESceneChangeIdc  ESceneChangeIdc       // SIMILAR_SCENE, MEDIUM_CHANGED_SCENE, LARGE_CHANGED_SCENE
	IMotionBlockNum  int32                 // Number of motion blocks
	IFrameComplexity int64                 // frame complexity
	PStaticBlockIdc  []uint8               // static block idc (one entry per 8x8 block, written from index 0)
	SScrollResult    SScrollDetectionParam //results from scroll detection
}

// SVAACalcResult. PCurY/PRefY are slices that start at the luma plane origin;
// they are set by METHOD_VAA_STATISTICS and only compared for identity (address
// of element 0) by METHOD_ADAPTIVE_QUANT.
type SVAACalcResult struct {
	PCurY             []uint8    // Y data of current frame
	PRefY             []uint8    // Y data of pRef frame for diff calc
	PSad8x8           [][4]int32 // sad of 8x8, every 4 in the same 16x16 get together
	PSsd16x16         []int32    // sum of square difference of 16x16
	PSum16x16         []int32    // sum of 16x16
	PSumOfSquare16x16 []int32    // sum of square of 16x16
	PSumOfDiff8x8     [][4]int32
	PMad8x8           [][4]uint8
	IFrameSad         int32 // sad of frame
}

type SVAACalcParam struct {
	// Borrowed for one Process call; true means current and reference source
	// samples are equal in this macroblock. Used only for SAD-only analysis.
	UnchangedMacroblocks []bool
	ICalcVar             int32
	ICalcBgd             int32
	ICalcSsd             int32
	IReserved            int32
	PCalcResult          *SVAACalcResult
}

type SBGDInterface struct {
	PBackgroundMbFlag []int8
	PCalcRes          *SVAACalcResult
}

type EAQModes = int32

const (
	AQ_QUALITY_MODE EAQModes = 0 //Quality mode
	AQ_BITRATE_MODE EAQModes = 1 //Bitrate mode
)

type SMotionTextureUnit struct {
	UiMotionIndex  uint16
	UiTextureIndex uint16
}

type SAdaptiveQuantizationParam struct {
	IAdaptiveQuantMode int32 // 0:quality mode, 1:bitrates mode
	PCalcResult        *SVAACalcResult
	PMotionTextureUnit []SMotionTextureUnit

	PMotionTextureIndexToDeltaQp     []int8
	IAverMotionTextureIndexToDeltaQp int32 // *AQ_STEP_INT_MULTIPLY
}

type EComplexityAnalysisMode = int32

const (
	FRAME_SAD EComplexityAnalysisMode = 0
	GOM_SAD   EComplexityAnalysisMode = -1
	GOM_VAR   EComplexityAnalysisMode = -2
)

type SComplexityAnalysisParam struct {
	IComplexityAnalysisMode int32
	ICalcBgd                int32
	IMbNumInGom             int32
	IFrameComplexity        int64
	PGomComplexity          []int32
	PGomForegroundBlockNum  []int32
	PBackgroundMbFlag       []int8
	UiRefMbType             []uint32
	PCalcResult             *SVAACalcResult
}

type SComplexityAnalysisScreenParam struct {
	IMbRowInGom      int32
	PGomComplexity   []int32
	IGomNumInFrame   int32
	IFrameComplexity int64 //255*255(MaxMbSAD)*36864(MaxFS) make the highest bit of 32-bit integer 1
	IIdrFlag         int32
	SScrollResult    SScrollDetectionParam
}

/////////////////////////////////////////////////////////////////////////////////////////////

// IWelsVPc is the C style interface: a context plus function pointers.
type IWelsVPc struct {
	PCtx           any
	Init           func(pCtx any, iType int32, pCfg any) EResult
	Uninit         func(pCtx any, iType int32) EResult
	Flush          func(pCtx any, iType int32) EResult
	Process        func(pCtx any, iType int32, pSrc *SPixMap, dst *SPixMap) EResult
	Get            func(pCtx any, iType int32, pParam any) EResult
	Set            func(pCtx any, iType int32, pParam any) EResult
	SpecialFeature func(pCtx any, iType int32, pIn any, pOut any) EResult
}

// IWelsVP is the C++ style interface. pParam/pCfg (C `void*`) hold a pointer
// to the parameter struct of the method, e.g. *SVAACalcParam for
// METHOD_VAA_STATISTICS Set, *SSceneChangeResult for scene change Get/Set.
type IWelsVP interface {
	Init(iType int32, pCfg any) EResult
	Uninit(iType int32) EResult
	Flush(iType int32) EResult
	Process(iType int32, pSrc *SPixMap, dst *SPixMap) EResult
	Get(iType int32, pParam any) EResult
	Set(iType int32, pParam any) EResult
	SpecialFeature(iType int32, pIn any, pOut any) EResult
}

/* C++ interface version */
const WELSVP_INTERFACE_VERION = 0x8000 + (WELSVP_VERSION & 0x7fff)

/* C interface version (WELSVP_INTERFACE_VERION when compiled as C) */
const WELSVP_INTERFACE_VERION_C = 0x0001 + (WELSVP_VERSION & 0x7fff)
