// Port of codec/encoder/core/inc/paraset_strategy.h.
//
// Class hierarchy (C++ -> Go embedding):
//
//	CWelsParametersetIdConstant
//	└─ CWelsParametersetIdNonConstant
//	   ├─ CWelsParametersetIdIncreasing
//	   └─ CWelsParametersetSpsListing
//	      ├─ CWelsParametersetSpsPpsListing
//	      └─ CWelsParametersetSpsListingPpsIncreasing
//
// Every object is used through the IWelsParametersetStrategy interface.
// Virtual calls made from inside class code must go through p.pVirt (the
// most-derived object); CheckPpsGenerating / SpsReset, first declared in
// CWelsParametersetSpsListing, go through p.pSpsListingVirt.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

// IWelsParametersetStrategy is the virtual interface of the parameter set
// strategies (all virtual methods, including the protected ones).
type IWelsParametersetStrategy interface {
	GetPpsIdOffset(iPpsId int32) int32
	GetSpsIdOffset(iPpsId int32, iSpsId int32) int32
	// GetSpsIdOffsetList returns int32_t* &m_sParaSetOffset.sParaSetOffsetVariable[iParasetType].iParaSetIdDelta[0]
	// as a slice over that array.
	GetSpsIdOffsetList(iParasetType int32) []int32

	GetAllNeededParasetNum() uint32

	GetNeededSpsNum() uint32
	GetNeededSubsetSpsNum() uint32
	GetNeededPpsNum() uint32

	// LoadPrevious: the arrays are sWelsEncCtx.pSpsArray / pSubsetArray / pPPSArray.
	LoadPrevious(pExistingParasetList *SExistingParasetList, pSpsArray []SWelsSPS,
		pSubsetArray []SSubsetSps,
		pPpsArray []SWelsPPS)

	Update(kuiId uint32, iParasetType int32)
	UpdatePpsList(pCtx *sWelsEncCtx)

	CheckParamCompatibility(pCodingParam *SWelsSvcCodingParam, pLogCtx *common.SLogContext) bool

	// GenerateNewSps: SWelsSPS*& pSps, SSubsetSps*& pSubsetSps -> **.
	GenerateNewSps(pCtx *sWelsEncCtx, kbUseSubsetSps bool, iDlayerIndex int32,
		iDlayerCount int32,
		kuiSpsId uint32,
		pSps **SWelsSPS, pSubsetSps **SSubsetSps, bSVCBaselayer bool) uint32

	InitPps(pCtx *sWelsEncCtx, kiSpsId uint32,
		pSps *SWelsSPS,
		pSubsetSps *SSubsetSps,
		kuiPpsId uint32,
		kbDeblockingFilterPresentFlag bool,
		kbUsingSubsetSps bool,
		kbEntropyCodingModeFlag bool) uint32

	SetUseSubsetFlag(iPpsId uint32, bUseSubsetSps bool)

	UpdateParaSetNum(pCtx *sWelsEncCtx)

	GetCurrentPpsId(iPpsId int32, iIdrLoop int32) int32

	// OutputCurrentStructure / LoadPreviousStructure: SParaSetOffsetVariable* (PARA_SET_TYPE entries)
	// and int32_t* pPpsIdList (MAX_DQ_LAYER_NUM * MAX_PPS_COUNT entries) -> pointers to Go arrays.
	OutputCurrentStructure(pParaSetOffsetVariable *[PARA_SET_TYPE]SParaSetOffsetVariable, pPpsIdList *[MAX_DQ_LAYER_NUM][MAX_PPS_COUNT]int32,
		pCtx *sWelsEncCtx, pExistingParasetList *SExistingParasetList)
	LoadPreviousStructure(pParaSetOffsetVariable *[PARA_SET_TYPE]SParaSetOffsetVariable, pPpsIdList *[MAX_DQ_LAYER_NUM][MAX_PPS_COUNT]int32)

	GetSpsIdx(iIdx int32) int32

	// protected virtual:
	LoadPreviousSps(pExistingParasetList *SExistingParasetList, pSpsArray []SWelsSPS,
		pSubsetArray []SSubsetSps)
	LoadPreviousPps(pExistingParasetList *SExistingParasetList, pPpsArray []SWelsPPS)
}

// CWelsParametersetIdConstant is the CONSTANT_ID strategy (root class).
type CWelsParametersetIdConstant struct {
	// Go-only: the most-derived object, for virtual dispatch from class code.
	pVirt IWelsParametersetStrategy

	// protected:
	m_sParaSetOffset     SParaSetOffset
	m_bSimulcastAVC      bool
	m_iSpatialLayerNum   int32
	m_iBasicNeededSpsNum uint32
	m_iBasicNeededPpsNum uint32
}

// NewCWelsParametersetIdConstant is the C++ constructor (new CWelsParametersetIdConstant (...)).
func NewCWelsParametersetIdConstant(bSimulcastAVC bool, kiSpatialLayerNum int32) *CWelsParametersetIdConstant {
	p := &CWelsParametersetIdConstant{}
	p.pVirt = p
	p.ctorCWelsParametersetIdConstant(bSimulcastAVC, kiSpatialLayerNum)
	return p
}

// Inline methods of CWelsParametersetIdConstant (defined in the header).

func (p *CWelsParametersetIdConstant) UpdatePpsList(pCtx *sWelsEncCtx) {}

func (p *CWelsParametersetIdConstant) CheckParamCompatibility(pCodingParam *SWelsSvcCodingParam, pLogCtx *common.SLogContext) bool {
	return true
}

func (p *CWelsParametersetIdConstant) UpdateParaSetNum(pCtx *sWelsEncCtx) {}

func (p *CWelsParametersetIdConstant) GetCurrentPpsId(iPpsId int32, iIdrLoop int32) int32 {
	return iPpsId
}

func (p *CWelsParametersetIdConstant) OutputCurrentStructure(pParaSetOffsetVariable *[PARA_SET_TYPE]SParaSetOffsetVariable, pPpsIdList *[MAX_DQ_LAYER_NUM][MAX_PPS_COUNT]int32,
	pCtx *sWelsEncCtx,
	pExistingParasetList *SExistingParasetList) {
}

func (p *CWelsParametersetIdConstant) LoadPreviousStructure(pParaSetOffsetVariable *[PARA_SET_TYPE]SParaSetOffsetVariable, pPpsIdList *[MAX_DQ_LAYER_NUM][MAX_PPS_COUNT]int32) {
}

func (p *CWelsParametersetIdConstant) GetSpsIdx(iIdx int32) int32 {
	return 0
}

func (p *CWelsParametersetIdConstant) LoadPreviousSps(pExistingParasetList *SExistingParasetList, pSpsArray []SWelsSPS,
	pSubsetArray []SSubsetSps) {
}

func (p *CWelsParametersetIdConstant) LoadPreviousPps(pExistingParasetList *SExistingParasetList, pPpsArray []SWelsPPS) {
}

// CWelsParametersetIdNonConstant is the common base of the non-constant strategies.
type CWelsParametersetIdNonConstant struct {
	CWelsParametersetIdConstant
}

// NewCWelsParametersetIdNonConstant is the C++ constructor.
func NewCWelsParametersetIdNonConstant(bSimulcastAVC bool, kiSpatialLayerNum int32) *CWelsParametersetIdNonConstant {
	p := &CWelsParametersetIdNonConstant{}
	p.pVirt = p
	p.ctorCWelsParametersetIdNonConstant(bSimulcastAVC, kiSpatialLayerNum)
	return p
}

// ctorCWelsParametersetIdNonConstant is the (inline) constructor body.
func (p *CWelsParametersetIdNonConstant) ctorCWelsParametersetIdNonConstant(bSimulcastAVC bool, kiSpatialLayerNum int32) {
	p.ctorCWelsParametersetIdConstant(bSimulcastAVC, kiSpatialLayerNum)
}

// CWelsParametersetIdIncreasing is the INCREASING_ID strategy.
type CWelsParametersetIdIncreasing struct {
	CWelsParametersetIdNonConstant
}

// NewCWelsParametersetIdIncreasing is the C++ constructor.
func NewCWelsParametersetIdIncreasing(bSimulcastAVC bool, kiSpatialLayerNum int32) *CWelsParametersetIdIncreasing {
	p := &CWelsParametersetIdIncreasing{}
	p.pVirt = p
	p.ctorCWelsParametersetIdNonConstant(bSimulcastAVC, kiSpatialLayerNum)
	return p
}

// iCWelsParametersetSpsListingVirtual lists the virtual methods first
// declared in CWelsParametersetSpsListing.
type iCWelsParametersetSpsListingVirtual interface {
	CheckPpsGenerating() bool
	SpsReset(pCtx *sWelsEncCtx, kbUseSubsetSps bool) int32
}

// CWelsParametersetSpsListing is the SPS_LISTING strategy.
type CWelsParametersetSpsListing struct {
	CWelsParametersetIdNonConstant

	// Go-only: the most-derived object, for the virtual methods first declared in this class.
	pSpsListingVirt iCWelsParametersetSpsListingVirtual
}

// NewCWelsParametersetSpsListing is the C++ constructor.
func NewCWelsParametersetSpsListing(bSimulcastAVC bool, kiSpatialLayerNum int32) *CWelsParametersetSpsListing {
	p := &CWelsParametersetSpsListing{}
	p.pVirt = p
	p.pSpsListingVirt = p
	p.ctorCWelsParametersetSpsListing(bSimulcastAVC, kiSpatialLayerNum)
	return p
}

func (p *CWelsParametersetSpsListing) GetSpsIdx(iIdx int32) int32 {
	return iIdx
}

// CWelsParametersetSpsPpsListing is the SPS_PPS_LISTING strategy.
type CWelsParametersetSpsPpsListing struct {
	CWelsParametersetSpsListing
}

// NewCWelsParametersetSpsPpsListing is the C++ constructor.
func NewCWelsParametersetSpsPpsListing(bSimulcastAVC bool, kiSpatialLayerNum int32) *CWelsParametersetSpsPpsListing {
	p := &CWelsParametersetSpsPpsListing{}
	p.pVirt = p
	p.pSpsListingVirt = p
	p.ctorCWelsParametersetSpsPpsListing(bSimulcastAVC, kiSpatialLayerNum)
	return p
}

// CWelsParametersetSpsListingPpsIncreasing is the SPS_LISTING_AND_PPS_INCREASING strategy.
type CWelsParametersetSpsListingPpsIncreasing struct {
	CWelsParametersetSpsListing
}

// NewCWelsParametersetSpsListingPpsIncreasing is the C++ constructor.
func NewCWelsParametersetSpsListingPpsIncreasing(bSimulcastAVC bool, kiSpatialLayerNum int32) *CWelsParametersetSpsListingPpsIncreasing {
	p := &CWelsParametersetSpsListingPpsIncreasing{}
	p.pVirt = p
	p.pSpsListingVirt = p
	p.ctorCWelsParametersetSpsListing(bSimulcastAVC, kiSpatialLayerNum)
	return p
}

// compile-time checks
var (
	_ IWelsParametersetStrategy = (*CWelsParametersetIdConstant)(nil)
	_ IWelsParametersetStrategy = (*CWelsParametersetIdNonConstant)(nil)
	_ IWelsParametersetStrategy = (*CWelsParametersetIdIncreasing)(nil)
	_ IWelsParametersetStrategy = (*CWelsParametersetSpsListing)(nil)
	_ IWelsParametersetStrategy = (*CWelsParametersetSpsPpsListing)(nil)
	_ IWelsParametersetStrategy = (*CWelsParametersetSpsListingPpsIncreasing)(nil)

	_ iCWelsParametersetSpsListingVirtual = (*CWelsParametersetSpsListing)(nil)
	_ iCWelsParametersetSpsListingVirtual = (*CWelsParametersetSpsPpsListing)(nil)
)
