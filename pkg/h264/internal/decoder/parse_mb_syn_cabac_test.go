package decoder

// Bit-exactness tests for cabac_decoder.go, parse_mb_syn_cabac.go and
// parse_mb_syn_cavlc.go.
//
// The expected hashes were produced by a C++ harness linked against the
// reference decoder (codec/decoder/core/src/*.cpp, C paths only). The harness
// and this file use the same pseudo-random generator, build the same
// decoder state (a 4x3-MB picture with random per-MB data, random neighbour
// availability, random bit-stream bytes, random reference lists) and run
// the same random sequences of syntax-element parsers, hashing every result
// (return codes, parsed values, CABAC/CAVLC engine state, caches, MB arrays,
// coefficients, PCM samples) with 64-bit FNV-1a.

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const (
	pmsW     = 4
	pmsH     = 3
	pmsN     = pmsW * pmsH
	pmsBufSz = 600
)

type pmsRand struct {
	s uint32
	h uint64
}

func (r *pmsRand) rnd() uint32 {
	r.s = r.s*1664525 + 1013904223
	return r.s >> 8
}

func (r *pmsRand) H(v int64) {
	r.h ^= uint64(v)
	r.h *= 1099511628211
}

func pmsB(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

var pmsMbTypes = []uint32{
	common.MB_TYPE_INTRA4x4, common.MB_TYPE_INTRA16x16, common.MB_TYPE_INTRA8x8, common.MB_TYPE_INTRA_PCM, common.MB_TYPE_16x16, common.MB_TYPE_16x8,
	common.MB_TYPE_8x16, common.MB_TYPE_8x8, common.MB_TYPE_8x8_REF0, common.MB_TYPE_SKIP, common.MB_TYPE_DIRECT,
	common.MB_TYPE_16x16 | common.MB_TYPE_P0L0 | common.MB_TYPE_P0L1, common.MB_TYPE_16x8 | common.MB_TYPE_P0L1 | common.MB_TYPE_P1L0,
}

type pmsState struct {
	r      pmsRand
	ctx    *SWelsDecoderContext
	dq     *SDqLayer
	pic    *SPicture
	refs   [2][4]SPicture
	sps    SSps
	param  api.SDecodingParam
	engine SWelsCabacDecEngine
	vlc    SVlcTable
	bs     common.SBitStringAux
	buf    []uint8

	planeY, planeU, planeV []uint8

	nzcCache    [48]uint8
	ipmCache    [48]int8
	mvCache     [2][30][2]int16
	mvdCache    [2][30][2]int16
	refCache    [2][30]int8
	directCache [30]int8
	coeff       []int16
}

func pmsSetup() *pmsState {
	S := &pmsState{}
	S.r.s = 12345
	S.ctx = &SWelsDecoderContext{}
	S.dq = &SDqLayer{}
	S.pic = &SPicture{}
	S.buf = make([]uint8, pmsBufSz)
	S.coeff = make([]int16, 384)
	ctx, dq, pic := S.ctx, S.dq, S.pic
	ctx.pCurDqLayer = dq
	ctx.pDec = pic
	dq.pDec = pic
	ctx.pCabacDecEngine = &S.engine
	ctx.pSps = &S.sps
	ctx.pParam = &S.param
	ctx.pVlcTable = &S.vlc
	InitVlcTable(&S.vlc)
	S.sps.pSLevelLimits = &common.G_ksLevelLimits[3]
	dq.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.pSps = &S.sps
	dq.iMbWidth = pmsW
	dq.iMbHeight = pmsH
	dq.pBitStringAux = &S.bs
	pic.pMbType = make([]uint32, pmsN)
	dq.pMbType = pic.pMbType
	dq.pSliceIdc = make([]int32, pmsN)
	dq.pCbp = make([]int8, pmsN)
	dq.pCbfDc = make([]uint16, pmsN)
	dq.pTransformSize8x8Flag = make([]bool, pmsN)
	dq.pChromaPredMode = make([]int8, pmsN)
	dq.pLumaQp = make([]int8, pmsN)
	dq.pChromaQp = make([][2]int8, pmsN)
	dq.pNzc = make([][24]int8, pmsN)
	dq.pIntraPredMode = make([][8]int8, pmsN)
	for l := 0; l < 2; l++ {
		pic.pRefIndex[l] = make([][16]int8, pmsN)
		pic.pMv[l] = make([][16][2]int16, pmsN)
		dq.pRefIndex[l] = pic.pRefIndex[l]
		dq.pMv[l] = make([][16][2]int16, pmsN)
		dq.pMvd[l] = make([][16][2]int16, pmsN)
	}
	dq.pDirect = make([][16]int8, pmsN)
	dq.pNoSubMbPartSizeLessThan8x8Flag = make([]bool, pmsN)
	dq.pSubMbType = make([][MB_SUB_PARTITION_SIZE]uint32, pmsN)
	S.planeY = make([]uint8, pmsW*16*pmsH*16)
	S.planeU = make([]uint8, pmsW*8*pmsH*8)
	S.planeV = make([]uint8, pmsW*8*pmsH*8)
	pic.pData[0] = S.planeY
	pic.pData[1] = S.planeU
	pic.pData[2] = S.planeV
	pic.iLinesize[0] = pmsW * 16
	pic.iLinesize[1] = pmsW * 8
	pic.iLinesize[2] = pmsW * 8
	for i := 0; i < 6; i++ {
		ctx.pDequant_coeff4x4[i] = ctx.pDequant_coeff_buffer4x4[i][:]
		ctx.pDequant_coeff8x8[i] = ctx.pDequant_coeff_buffer8x8[i][:]
		for q := 0; q < 52; q++ {
			for k := 0; k < 16; k++ {
				ctx.pDequant_coeff_buffer4x4[i][q][k] = uint16(1 + S.r.rnd()%64)
			}
			for k := 0; k < 64; k++ {
				ctx.pDequant_coeff_buffer8x8[i][q][k] = uint16(1 + S.r.rnd()%64)
			}
		}
	}
	return S
}

func (S *pmsState) randomize() {
	r := &S.r
	dq := S.dq
	for mb := 0; mb < pmsN; mb++ {
		dq.pMbType[mb] = pmsMbTypes[r.rnd()%uint32(len(pmsMbTypes))]
		if r.rnd()%4 == 0 {
			dq.pSliceIdc[mb] = 1
		} else {
			dq.pSliceIdc[mb] = 0
		}
		dq.pCbp[mb] = int8(r.rnd() % 48)
		dq.pCbfDc[mb] = uint16(r.rnd() & 0x1ff)
		dq.pTransformSize8x8Flag[mb] = r.rnd()&1 != 0
		dq.pChromaPredMode[mb] = int8(int32(r.rnd()%5) - 1)
		dq.pLumaQp[mb] = int8(r.rnd() % 52)
		dq.pChromaQp[mb][0] = int8(r.rnd() % 52)
		dq.pChromaQp[mb][1] = int8(r.rnd() % 52)
		for k := 0; k < 24; k++ {
			dq.pNzc[mb][k] = int8(r.rnd() % 17)
		}
		for k := 0; k < 8; k++ {
			dq.pIntraPredMode[mb][k] = int8(int32(r.rnd()%10) - 1)
		}
		for l := 0; l < 2; l++ {
			for k := 0; k < 16; k++ {
				S.pic.pRefIndex[l][mb][k] = int8(int32(r.rnd()%6) - 2)
				for c := 0; c < 2; c++ {
					S.pic.pMv[l][mb][k][c] = int16(int32(r.rnd()%512) - 256)
					dq.pMv[l][mb][k][c] = int16(int32(r.rnd()%512) - 256)
					dq.pMvd[l][mb][k][c] = int16(int32(r.rnd()%128) - 64)
				}
			}
		}
		for k := 0; k < 16; k++ {
			dq.pDirect[mb][k] = int8(r.rnd() & 1)
		}
		dq.pNoSubMbPartSizeLessThan8x8Flag[mb] = true
		for k := 0; k < 4; k++ {
			dq.pSubMbType[mb][k] = 0
		}
	}
	for i := range S.planeY {
		S.planeY[i] = uint8(r.rnd())
	}
	for i := range S.planeU {
		S.planeU[i] = uint8(r.rnd())
	}
	for i := range S.planeV {
		S.planeV[i] = uint8(r.rnd())
	}
	for i := range S.buf {
		S.buf[i] = uint8(r.rnd())
	}
	clear(S.coeff)
	for l := 0; l < 2; l++ {
		n := int(r.rnd()%4 + 1)
		for i := 0; i < MAX_DPB_COUNT; i++ {
			S.ctx.sRefPic.pRefList[l][i] = nil
		}
		for i := 0; i < n; i++ {
			S.refs[l][i].bIsComplete = r.rnd()&1 != 0
			S.refs[l][i].bIsLongRef = r.rnd()&1 != 0
			S.ctx.sRefPic.pRefList[l][i] = &S.refs[l][i]
		}
		S.ctx.sRefPic.uiRefCount[l] = uint8(n)
		dq.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.uiRefCount[l] = int32(r.rnd()%4 + 1)
	}
	S.param.EEcActiveIdc = api.ERROR_CON_IDC(r.rnd() % 2)
	S.ctx.bRPLRError = false
	S.ctx.bMbRefConcealed = false
	S.ctx.iErrorCode = 0
	S.ctx.bUseScalingList = r.rnd()&1 != 0
	S.sps.uiChromaFormatIdc = uint8(r.rnd() % 2)
	dq.sLayerInfo.sSliceInLayer.iLastDeltaQp = int32(r.rnd()%3) - 1

	iMbXy := int32(r.rnd() % pmsN)
	dq.iMbXyIndex = iMbXy
	dq.iMbX = iMbXy % pmsW
	dq.iMbY = iMbXy / pmsW
}

func (S *pmsState) hashNeigh(n *SWelsNeighAvail) {
	H := S.r.H
	H(int64(n.iTopAvail))
	H(int64(n.iLeftAvail))
	H(int64(n.iRightTopAvail))
	H(int64(n.iLeftTopAvail))
	H(int64(n.iLeftType))
	H(int64(n.iTopType))
	H(int64(n.iLeftTopType))
	H(int64(n.iRightTopType))
	H(int64(n.iTopCbp))
	H(int64(n.iLeftCbp))
}

func (S *pmsState) hashCaches() {
	H := S.r.H
	for i := 0; i < 48; i++ {
		H(int64(S.nzcCache[i]))
		H(int64(S.ipmCache[i]))
	}
	for l := 0; l < 2; l++ {
		for i := 0; i < 30; i++ {
			H(int64(S.mvCache[l][i][0]))
			H(int64(S.mvCache[l][i][1]))
			H(int64(S.mvdCache[l][i][0]))
			H(int64(S.mvdCache[l][i][1]))
			H(int64(S.refCache[l][i]))
		}
	}
	for i := 0; i < 30; i++ {
		H(int64(S.directCache[i]))
	}
}

func (S *pmsState) hashEngine() {
	H := S.r.H
	H(int64(S.engine.uiRange))
	H(int64(S.engine.uiOffset))
	H(int64(S.engine.iBitsLeft))
	H(int64(S.engine.pBuffCurr))
}

func (S *pmsState) hashBs() {
	H := S.r.H
	H(int64(S.bs.PCurBuf))
	H(int64(S.bs.ILeftBits))
	H(int64(S.bs.UiCurBits))
	H(int64(S.bs.IIndex))
}

func (S *pmsState) hashNzcCoeff() {
	H := S.r.H
	for i := 0; i < 48; i++ {
		H(int64(S.nzcCache[i]))
	}
	for i := 0; i < 384; i++ {
		H(int64(S.coeff[i]))
	}
}

func (S *pmsState) hashState() {
	H := S.r.H
	dq := S.dq
	for mb := 0; mb < pmsN; mb++ {
		H(int64(dq.pMbType[mb]))
		H(int64(dq.pCbfDc[mb]))
		H(int64(dq.pLumaQp[mb]))
		H(int64(dq.pChromaQp[mb][0]))
		H(int64(dq.pChromaQp[mb][1]))
		for k := 0; k < 24; k++ {
			H(int64(dq.pNzc[mb][k]))
		}
		for l := 0; l < 2; l++ {
			for k := 0; k < 16; k++ {
				H(int64(S.pic.pRefIndex[l][mb][k]))
				H(int64(S.pic.pMv[l][mb][k][0]))
				H(int64(S.pic.pMv[l][mb][k][1]))
				H(int64(dq.pMv[l][mb][k][0]))
				H(int64(dq.pMv[l][mb][k][1]))
				H(int64(dq.pMvd[l][mb][k][0]))
				H(int64(dq.pMvd[l][mb][k][1]))
			}
		}
		for k := 0; k < 16; k++ {
			H(int64(dq.pDirect[mb][k]))
		}
		H(pmsB(dq.pNoSubMbPartSizeLessThan8x8Flag[mb]))
		for k := 0; k < 4; k++ {
			H(int64(dq.pSubMbType[mb][k]))
		}
	}
	for _, v := range S.planeY {
		H(int64(v))
	}
	for _, v := range S.planeU {
		H(int64(v))
	}
	for _, v := range S.planeV {
		H(int64(v))
	}
	for i := 0; i < 384; i++ {
		H(int64(S.coeff[i]))
	}
	H(pmsB(S.ctx.bMbRefConcealed))
	H(int64(S.ctx.iErrorCode))
	H(int64(dq.sLayerInfo.sSliceInLayer.iLastDeltaQp))
}

var pmsResProps = [12]int32{I16_LUMA_DC, I16_LUMA_AC, LUMA_DC_AC_INTRA, LUMA_DC_AC_INTER, CHROMA_DC_U, CHROMA_DC_V,
	CHROMA_AC_U, CHROMA_AC_V, CHROMA_DC_U_INTER, CHROMA_DC_V_INTER, CHROMA_AC_U_INTER, CHROMA_AC_V_INTER}

// residualParams picks the block parameters of a residual property, as
// decode_slice.cpp does.
func (S *pmsState) residualParams(prop int32) (iIndex int32, iMaxNumCoeff int32, scan []uint8, coeffOff int) {
	r := &S.r
	switch prop {
	case I16_LUMA_DC:
		return 0, 16, g_kuiLumaDcZigzagScan[:], 0
	case I16_LUMA_AC:
		iIndex = int32(r.rnd() % 16)
		return iIndex, 15, g_kuiZigzagScan[1:], int(iIndex << 4)
	case LUMA_DC_AC_INTRA, LUMA_DC_AC_INTER:
		iIndex = int32(r.rnd() % 16)
		return iIndex, 16, g_kuiZigzagScan[:], int(iIndex << 4)
	case CHROMA_DC_U, CHROMA_DC_U_INTER:
		return 16, 4, g_kuiChromaDcScan[:], 256
	case CHROMA_DC_V, CHROMA_DC_V_INTER:
		return 20, 4, g_kuiChromaDcScan[:], 256 + 64
	case CHROMA_AC_U, CHROMA_AC_U_INTER:
		j := int32(r.rnd() % 4)
		return 16 + j, 15, g_kuiZigzagScan[1:], 256 + int(j<<4)
	default:
		j := int32(r.rnd() % 4)
		return 20 + j, 15, g_kuiZigzagScan[1:], 256 + 64 + int(j<<4)
	}
}

func (S *pmsState) initSlice() (sliceType int32) {
	r := &S.r
	sliceType = int32(r.rnd() % 3)
	S.ctx.eSliceType = common.EWelsSliceType(sliceType)
	S.dq.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.eSliceType = common.EWelsSliceType(sliceType)
	return sliceType
}

func (S *pmsState) initBits(nBytes int) {
	r := &S.r
	DecInitBits(&S.bs, S.buf, 0, int32(nBytes*8))
	skip := int32(r.rnd() % 24)
	var code uint32
	if skip > 0 {
		r.H(int64(BsGetBits(&S.bs, skip, &code)))
		r.H(int64(code))
	}
}

func (S *pmsState) runCabac(seed uint32, nCases int) uint64 {
	r := &S.r
	r.s = seed
	r.h = 1469598103934665603
	H := r.H
	for c := 0; c < nCases; c++ {
		S.randomize()
		ctx, dq := S.ctx, S.dq
		var neigh SWelsNeighAvail
		GetNeighborAvailMbType(&neigh, dq)
		S.hashNeigh(&neigh)

		sliceType := S.initSlice()
		qp := int32(r.rnd() % 52)
		idc := int32(r.rnd() % 3)
		WelsCabacContextInit(ctx, uint8(sliceType), idc, qp)

		S.initBits(pmsBufSz)
		ret := InitCabacDecEngineFromBS(&S.engine, &S.bs)
		H(int64(ret))
		S.hashEngine()

		S.nzcCache = [48]uint8{}
		S.ipmCache = [48]int8{}
		WelsFillCacheInterCabac(&neigh, S.nzcCache[:], &S.mvCache, &S.mvdCache, &S.refCache, dq)
		WelsFillDirectCacheCabac(&neigh, &S.directCache, dq)
		if r.rnd()&1 != 0 {
			WelsFillCacheConstrain1IntraNxN(&neigh, S.nzcCache[:], S.ipmCache[:], dq)
		} else {
			WelsFillCacheConstrain0IntraNxN(&neigh, S.nzcCache[:], S.ipmCache[:], dq)
		}
		S.hashCaches()

		var uiNeighAvail uint8
		if neigh.iLeftAvail != 0 {
			uiNeighAvail |= 4
		}
		if neigh.iLeftTopAvail != 0 {
			uiNeighAvail |= 2
		}
		if neigh.iTopAvail != 0 {
			uiNeighAvail |= 1
		}
		for op := 0; op < 40; op++ {
			k := r.rnd() % 22
			ret = 0
			H(int64(k))
			switch k {
			case 0:
				var v uint32
				ret = ParseSkipFlagCabac(ctx, &neigh, &v)
				H(int64(v))
			case 1:
				var v uint32
				ret = ParseMBTypeISliceCabac(ctx, &neigh, &v)
				H(int64(v))
			case 2:
				var v uint32
				ret = ParseMBTypePSliceCabac(ctx, &neigh, &v)
				H(int64(v))
			case 3:
				var v uint32
				ret = ParseMBTypeBSliceCabac(ctx, &neigh, &v)
				H(int64(v))
			case 4:
				var v bool
				ret = ParseTransformSize8x8FlagCabac(ctx, &neigh, &v)
				H(pmsB(v))
			case 5:
				var v uint32
				ret = ParseSubMBTypeCabac(ctx, &neigh, &v)
				H(int64(v))
			case 6:
				var v uint32
				ret = ParseBSubMBTypeCabac(ctx, &neigh, &v)
				H(int64(v))
			case 7:
				var v int32
				ret = ParseIntraPredModeLumaCabac(ctx, &v)
				H(int64(v))
			case 8:
				var v int32
				ret = ParseIntraPredModeChromaCabac(ctx, uiNeighAvail, &v)
				H(int64(v))
			case 9:
				var v uint32
				ret = ParseCbpInfoCabac(ctx, &neigh, &v)
				H(int64(v))
			case 10:
				var v int32
				ret = ParseDeltaQpCabac(ctx, &v)
				H(int64(v))
			case 11:
				var v int16
				idx := int32(r.rnd() % 16)
				list := int8(r.rnd() % 2)
				comp := int8(r.rnd() % 2)
				ret = ParseMvdInfoCabac(ctx, &neigh, &S.refCache, &S.mvdCache, idx, list, comp, &v)
				H(int64(v))
			case 12:
				var v int8
				kZ := [4]int32{0, 4, 8, 12}
				list := int32(r.rnd() % 2)
				z := kZ[r.rnd()%4]
				act := int32(r.rnd()%4 + 1)
				b8 := int32(r.rnd() % 2)
				ret = ParseRefIdxCabac(ctx, &neigh, S.nzcCache[:], &S.refCache, &S.directCache, list, z, act, b8, &v)
				H(int64(v))
			case 13, 14, 15:
				prop := pmsResProps[r.rnd()%12]
				iIndex, iMax, scan, off := S.residualParams(prop)
				q := uint8(r.rnd() % 52)
				ret = ParseResidualBlockCabac(&neigh, S.nzcCache[:], &S.bs, iIndex, iMax, scan, prop, S.coeff[off:], q, ctx)
				S.hashNzcCoeff()
				H(int64(dq.pCbfDc[dq.iMbXyIndex]))
			case 16:
				iIndex := int32(r.rnd()%4) << 2
				prop := int32(LUMA_DC_AC_INTER_8)
				if r.rnd()&1 != 0 {
					prop = LUMA_DC_AC_INTRA_8
				}
				q := uint8(r.rnd() % 52)
				ret = ParseResidualBlockCabac8x8(&neigh, S.nzcCache[:], &S.bs, iIndex, 64, g_kuiZigzagScan8x8[:], prop,
					S.coeff[iIndex<<4:], q, ctx)
				S.hashNzcCoeff()
			case 17:
				kP := [5]uint32{common.MB_TYPE_16x16, common.MB_TYPE_16x8, common.MB_TYPE_8x16, common.MB_TYPE_8x8, common.MB_TYPE_8x8_REF0}
				dq.pMbType[dq.iMbXyIndex] = kP[r.rnd()%5]
				if sliceType == common.B_SLICE {
					break
				}
				ret = ParseInterPMotionInfoCabac(ctx, &neigh, S.nzcCache[:], &S.mvCache, &S.mvdCache, &S.refCache)
				S.hashCaches()
			case 18:
				dq.pMbType[dq.iMbXyIndex] = g_ksInterBMbTypeInfo[1+r.rnd()%21].iType
				if sliceType != common.B_SLICE {
					break
				}
				ret = ParseInterBMotionInfoCabac(ctx, &neigh, S.nzcCache[:], &S.mvCache, &S.mvdCache, &S.refCache, &S.directCache)
				S.hashCaches()
			case 19:
				var v uint32
				ret = ParseEndOfSliceCabac(ctx, &v)
				H(int64(v))
			case 20:
				if r.rnd()%4 == 0 {
					S.param.BParseOnly = false
					ret = ParseIPCMInfoCabac(ctx)
					S.hashBs()
				}
			default:
				var v uint32
				ctxIdx := r.rnd() % 460
				ret = int32(DecodeUEGLevelCabac(ctx.pCabacDecEngine, &ctx.pCabacCtx[ctxIdx], &v))
				H(int64(v))
				ret = DecodeExpBypassCabac(ctx.pCabacDecEngine, int32(r.rnd()%4), &v)
				H(int64(v))
			}
			H(int64(ret))
			S.hashEngine()
			if ret != 0 {
				break
			}
		}
		for i := 0; i < 460; i++ {
			H(int64(ctx.pCabacCtx[i].uiState))
			H(int64(ctx.pCabacCtx[i].uiMPS))
		}
		S.hashState()
	}
	return r.h
}

func (S *pmsState) runCavlc(seed uint32, nCases int) uint64 {
	r := &S.r
	r.s = seed
	r.h = 1469598103934665603
	H := r.H
	for c := 0; c < nCases; c++ {
		S.randomize()
		ctx, dq := S.ctx, S.dq
		var neigh SWelsNeighAvail
		GetNeighborAvailMbType(&neigh, dq)
		S.hashNeigh(&neigh)

		S.initSlice()

		S.initBits(pmsBufSz)
		S.hashBs()

		S.nzcCache = [48]uint8{}
		S.ipmCache = [48]int8{}
		S.mvdCache = [2][30][2]int16{}
		WelsFillCacheInter(&neigh, S.nzcCache[:], &S.mvCache, &S.refCache, dq)
		S.hashCaches()

		var sampleAvail [30]int32
		for i := range sampleAvail {
			sampleAvail[i] = int32(r.rnd() & 1)
		}

		for op := 0; op < 30; op++ {
			k := r.rnd() % 10
			var ret int32
			H(int64(k))
			switch k {
			case 0, 1, 2:
				prop := pmsResProps[r.rnd()%12]
				iIndex, iMax, scan, off := S.residualParams(prop)
				q := uint8(r.rnd() % 52)
				BsStartCavlc(&S.bs)
				ret = WelsResidualBlockCavlc(&S.vlc, S.nzcCache[:], &S.bs, iIndex, iMax, scan, prop, S.coeff[off:], q, ctx)
				BsEndCavlc(&S.bs)
				S.hashNzcCoeff()
			case 3:
				iIndex := int32(r.rnd()%4) << 2
				prop := int32(LUMA_DC_AC_INTER_8)
				if r.rnd()&1 != 0 {
					prop = LUMA_DC_AC_INTRA_8
				}
				q := uint8(r.rnd() % 52)
				BsStartCavlc(&S.bs)
				for idx4 := int32(0); idx4 < 4; idx4++ {
					ret = WelsResidualBlockCavlc8x8(&S.vlc, S.nzcCache[:], &S.bs, iIndex+idx4, 16, g_kuiZigzagScan8x8[:], prop,
						S.coeff[iIndex<<4:], idx4, q, ctx)
					H(int64(ret))
					if ret != 0 {
						break
					}
				}
				BsEndCavlc(&S.bs)
				S.hashNzcCoeff()
			case 4:
				kP := [5]uint32{common.MB_TYPE_16x16, common.MB_TYPE_16x8, common.MB_TYPE_8x16, common.MB_TYPE_8x8, common.MB_TYPE_8x8_REF0}
				dq.pMbType[dq.iMbXyIndex] = kP[r.rnd()%5]
				ret = ParseInterInfo(ctx, &S.mvCache, &S.refCache, &S.bs)
				S.hashCaches()
			case 5:
				dq.pMbType[dq.iMbXyIndex] = g_ksInterBMbTypeInfo[1+r.rnd()%21].iType
				ret = ParseInterBInfo(ctx, &S.mvCache, &S.refCache, &S.bs)
				S.hashCaches()
			case 6:
				mode := int8(int32(r.rnd()%6) - 1)
				avail := uint8(r.rnd() % 8)
				ret = CheckIntra16x16PredMode(avail, &mode)
				H(int64(mode))
			case 7:
				mode := int8(r.rnd() % 4)
				avail := uint8(r.rnd() % 8)
				ret = CheckIntraChromaPredMode(avail, &mode)
				H(int64(mode))
			case 8:
				mode := int8(int32(r.rnd()%11) - 1)
				idx := int32(r.rnd() % 16)
				b8 := r.rnd()&1 != 0
				ret = CheckIntraNxNPredMode(sampleAvail[:], &mode, idx, b8)
				H(int64(mode))
			default:
				idx := int32(r.rnd() % 16)
				H(int64(PredIntra4x4Mode(S.ipmCache[:], idx)))
			}
			H(int64(ret))
			S.hashBs()
			// CheckIntra* return modes, not only errors
			if k <= 5 && ret != 0 {
				break
			}
		}
		S.hashState()
	}
	return r.h
}

func (S *pmsState) runEngine(seed uint32, nCases int) uint64 {
	r := &S.r
	r.s = seed
	r.h = 1469598103934665603
	H := r.H
	ctx := S.ctx
	for c := 0; c < nCases; c++ {
		for i := range S.buf {
			S.buf[i] = uint8(r.rnd())
		}
		length := 8 + int(r.rnd()%(pmsBufSz-8))
		sliceType := int32(r.rnd() % 3)
		ctx.eSliceType = common.EWelsSliceType(sliceType)
		idc := int32(r.rnd() % 3)
		qp := int32(r.rnd() % 52)
		WelsCabacContextInit(ctx, uint8(sliceType), idc, qp)
		DecInitBits(&S.bs, S.buf, 0, int32(length*8))
		var code uint32
		skip := int32(r.rnd() % 24)
		if skip > 0 {
			H(int64(BsGetBits(&S.bs, skip, &code)))
		}
		ret := InitCabacDecEngineFromBS(&S.engine, &S.bs)
		H(int64(ret))
		if ret != 0 {
			continue
		}
		for op := 0; op < 4000; op++ {
			var v uint32
			k := r.rnd() % 8
			switch k {
			case 0, 1, 2:
				ret = DecodeBinCabac(&S.engine, &ctx.pCabacCtx[r.rnd()%460], &v)
			case 3:
				ret = DecodeBypassCabac(&S.engine, &v)
			case 4:
				if r.rnd()%16 == 0 {
					ret = DecodeTerminateCabac(&S.engine, &v)
				} else {
					ret = 0
				}
			case 5:
				base := r.rnd() % 450
				ret = DecodeUnaryBinCabac(&S.engine, ctx.pCabacCtx[base:], int32(r.rnd()%3), &v)
			case 6:
				ret = DecodeUEGMvCabac(&S.engine, ctx.pCabacCtx[r.rnd()%450:], 3, &v)
			default:
				ret = DecodeExpBypassCabac(&S.engine, int32(r.rnd()%4), &v)
			}
			H(int64(k))
			H(int64(v))
			H(int64(ret))
			S.hashEngine()
			if ret != 0 {
				break
			}
		}
		for i := 0; i < 460; i++ {
			H(int64(ctx.pCabacCtx[i].uiState))
			H(int64(ctx.pCabacCtx[i].uiMPS))
		}
	}
	return r.h
}

// TestCabacSyntaxBitExact runs the same generator sequence as the C++
// harness main(): engine(1), engine(2), cabac(3), cabac(4), cavlc(5),
// cavlc(6), all on one state set up with seed 12345.
func TestCabacCavlcBitExact(t *testing.T) {
	S := pmsSetup()
	type run struct {
		name string
		f    func(uint32, int) uint64
		seed uint32
		n    int
		want uint64
	}
	runs := []run{
		{"engine", S.runEngine, 1, 200, 0x971607348ea7e1cf},
		{"engine", S.runEngine, 2, 200, 0xde72deda3a4a55c3},
		{"cabac", S.runCabac, 3, 3000, 0x225bcaa443127f91},
		{"cabac", S.runCabac, 4, 3000, 0x87bc02b235aaef78},
		{"cavlc", S.runCavlc, 5, 3000, 0xc75a8ce8952e63ca},
		{"cavlc", S.runCavlc, 6, 3000, 0x50c46e45e5cc9133},
	}
	for _, rr := range runs {
		if got := rr.f(rr.seed, rr.n); got != rr.want {
			t.Errorf("%s(seed %d): hash %016x, want %016x", rr.name, rr.seed, got, rr.want)
		}
	}
}
