package encoder

// Ports of test/encoder/EncUT_DecodeMbAux.cpp and EncUT_Reconstruct.cpp
// (C parts only).

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// ---------------------------------------------------------------------------
// EncUT_DecodeMbAux.cpp
// ---------------------------------------------------------------------------

// hadamard4x4Ref computes the reference output of the inverse 4x4 Hadamard
// transform as written out in the C test (T then Y, all in short).
func hadamard4x4Ref(W []int16) (Y [16]int32) {
	var T [16]int16
	for c := 0; c < 4; c++ {
		T[c] = W[c] + W[4+c] + W[8+c] + W[12+c]
		T[4+c] = W[c] + W[4+c] - W[8+c] - W[12+c]
		T[8+c] = W[c] - W[4+c] - W[8+c] + W[12+c]
		T[12+c] = W[c] - W[4+c] + W[8+c] - W[12+c]
	}
	for r := 0; r < 4; r++ {
		t0, t1, t2, t3 := int32(T[4*r]), int32(T[4*r+1]), int32(T[4*r+2]), int32(T[4*r+3])
		Y[4*r] = t0 + t1 + t2 + t3
		Y[4*r+1] = t0 + t1 - t2 - t3
		Y[4*r+2] = t0 - t1 - t2 + t3
		Y[4*r+3] = t0 - t1 + t2 - t3
	}
	return
}

func TestDecodeMbAuxTest_TestIhdm_4x4_dc(t *testing.T) {
	r := rand.New(rand.NewSource(21))
	for run := 0; run < 100; run++ {
		W := make([]int16, 16)
		for i := range W {
			W[i] = int16(r.Intn(256) + 1)
		}
		Y := hadamard4x4Ref(W)
		WelsIHadamard4x4Dc(W)
		for i := 0; i < 16; i++ {
			if int16(Y[i]) != W[i] {
				t.Fatalf("run %d idx %d: %d vs %d", run, i, Y[i], W[i])
			}
		}
	}
}

func TestDecodeMbAuxTest_TestDequant_4x4_luma_dc(t *testing.T) {
	r := rand.New(rand.NewSource(22))
	var T, W [16]int16
	for qp := int32(0); qp < 12; qp++ {
		for i := 0; i < 16; i++ {
			T[i] = int16(r.Intn(256) + 1)
			W[i] = T[i]
		}
		WelsDequantLumaDc4x4(W[:], qp)
		for i := 0; i < 16; i++ {
			T[i] = int16((int32(T[i])*int32(common.G_kuiDequantCoeff[qp%6][0]) + (1 << uint(1-qp/6))) >> uint(2-qp/6))
			if T[i] != W[i] {
				t.Fatalf("qp %d idx %d: %d vs %d", qp, i, T[i], W[i])
			}
		}
	}
}

func TestDecodeMbAuxTest_TestDequant_ihdm_4x4_c(t *testing.T) {
	r := rand.New(rand.NewSource(23))
	for run := 0; run < 100; run++ {
		mf := uint16(r.Intn(16) + 1)
		W := make([]int16, 16)
		for i := range W {
			W[i] = int16(r.Intn(256) + 1)
		}
		Y := hadamard4x4Ref(W)
		WelsDequantIHadamard4x4_c(W, mf)
		for i := 0; i < 16; i++ {
			if int16(Y[i]*int32(mf)) != W[i] {
				t.Fatalf("run %d idx %d", run, i)
			}
		}
	}
}

func TestDecodeMbAuxTest_TestDequant_4x4_c(t *testing.T) {
	r := rand.New(rand.NewSource(24))
	W := make([]int16, 16)
	T := make([]int16, 16)
	mf := make([]uint16, 16)
	for i := 0; i < 16; i++ {
		W[i] = int16(r.Intn(256) + 1)
		T[i] = W[i]
	}
	for i := 0; i < 8; i++ {
		mf[i] = uint16(r.Intn(16) + 1)
	}
	WelsDequant4x4_c(W, mf)
	for i := 0; i < 16; i++ {
		if int32(T[i])*int32(mf[i%8]) != int32(W[i]) {
			t.Fatalf("idx %d", i)
		}
	}
}

func TestDecodeMbAuxTest_TestDequant_4_4x4_c(t *testing.T) {
	r := rand.New(rand.NewSource(25))
	W := make([]int16, 64)
	T := make([]int16, 64)
	mf := make([]uint16, 16)
	for i := 0; i < 64; i++ {
		W[i] = int16(r.Intn(256) + 1)
		T[i] = W[i]
	}
	for i := 0; i < 8; i++ {
		mf[i] = uint16(r.Intn(16) + 1)
	}
	WelsDequantFour4x4_c(W, mf)
	for i := 0; i < 64; i++ {
		if int32(T[i])*int32(mf[i%8]) != int32(W[i]) {
			t.Fatalf("idx %d", i)
		}
	}
}

func welsDequantHadamard2x2DcAnchor(pDct []int16, iMF int16) {
	iSumU := pDct[0] + pDct[2]
	iDelU := pDct[0] - pDct[2]
	iSumD := pDct[1] + pDct[3]
	iDelD := pDct[1] - pDct[3]
	pDct[0] = int16(((int32(iSumU) + int32(iSumD)) * int32(iMF)) >> 1)
	pDct[1] = int16(((int32(iSumU) - int32(iSumD)) * int32(iMF)) >> 1)
	pDct[2] = int16(((int32(iDelU) + int32(iDelD)) * int32(iMF)) >> 1)
	pDct[3] = int16(((int32(iDelU) - int32(iDelD)) * int32(iMF)) >> 1)
}

func TestDecodeMbAuxTest_WelsDequantIHadamard2x2Dc(t *testing.T) {
	r := rand.New(rand.NewSource(26))
	for run := 0; run < 100; run++ {
		iDct := make([]int16, 4)
		iRefDct := make([]int16, 4)
		iMF := int16(cRandInt(r) & 127)
		for i := 0; i < 4; i++ {
			iDct[i] = int16((cRandInt(r) & 65535) - 32768)
			iRefDct[i] = iDct[i]
		}
		welsDequantHadamard2x2DcAnchor(iRefDct, iMF)
		WelsDequantIHadamard2x2Dc(iDct, uint16(iMF))
		for i := 0; i < 4; i++ {
			if iDct[i] != iRefDct[i] {
				t.Fatalf("run %d idx %d", run, i)
			}
		}
	}
}

// welsIDctT4Anchor is WelsIDctT4Anchor<int32_t> of the C test.
func welsIDctT4Anchor(pDst []uint8, off int, dct []int16) {
	var tmp [16]int16
	iStridex2 := fDEC_STRIDE << 1
	iStridex3 := iStridex2 + fDEC_STRIDE
	d := func(k int) int32 { return int32(dct[k]) }
	for i := 0; i < 4; i++ {
		b := i << 2
		tmp[b] = int16(d(b) + d(b+1) + d(b+2) + (d(b+3) >> 1))
		tmp[b+1] = int16(d(b) + (d(b+1) >> 1) - d(b+2) - d(b+3))
		tmp[b+2] = int16(d(b) - (d(b+1) >> 1) - d(b+2) + d(b+3))
		tmp[b+3] = int16(d(b) - d(b+1) + d(b+2) - (d(b+3) >> 1))
	}
	tt := func(k int) int32 { return int32(tmp[k]) }
	for i := 0; i < 4; i++ {
		o := off + i
		pDst[o] = common.WelsClip1(int32(pDst[o]) + ((tt(i) + tt(4+i) + tt(8+i) + (tt(12+i) >> 1) + 32) >> 6))
		o = off + i + fDEC_STRIDE
		pDst[o] = common.WelsClip1(int32(pDst[o]) + ((tt(i) + (tt(4+i) >> 1) - tt(8+i) - tt(12+i) + 32) >> 6))
		o = off + i + iStridex2
		pDst[o] = common.WelsClip1(int32(pDst[o]) + ((tt(i) - (tt(4+i) >> 1) - tt(8+i) + tt(12+i) + 32) >> 6))
		o = off + i + iStridex3
		pDst[o] = common.WelsClip1(int32(pDst[o]) + ((tt(i) - tt(4+i) + tt(8+i) - (tt(12+i) >> 1) + 32) >> 6))
	}
}

func TestDecodeMbAuxTest_WelsIDctT4Rec_c(t *testing.T) {
	r := rand.New(rand.NewSource(27))
	for run := 0; run < 1000; run++ {
		iRefDct := make([]int16, 16)
		iRefDst := make([]uint8, 16*fDEC_STRIDE)
		iDct := make([]int16, 16)
		iPred := make([]uint8, 16*fDEC_STRIDE)
		iRec := make([]uint8, 16*fDEC_STRIDE)
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				iDct[i*4+j] = int16((cRandInt(r) & 65535) - 32768)
				iRefDct[i*4+j] = iDct[i*4+j]
				iPred[i*fDEC_STRIDE+j] = uint8(cRandInt(r) & 255)
				iRefDst[i*fDEC_STRIDE+j] = iPred[i*fDEC_STRIDE+j]
			}
		}
		welsIDctT4Anchor(iRefDst, 0, iRefDct)
		WelsIDctT4Rec_c(iRec, 0, fDEC_STRIDE, iPred, 0, fDEC_STRIDE, iDct)
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				if iRec[i*fDEC_STRIDE+j] != iRefDst[i*fDEC_STRIDE+j] {
					t.Fatalf("run %d (%d,%d)", run, i, j)
				}
			}
		}
	}
}

func TestDecodeMbAuxTest_WelsIDctFourT4Rec_c(t *testing.T) {
	r := rand.New(rand.NewSource(28))
	for run := 0; run < 1000; run++ {
		iRefDct := make([]int16, 64)
		iRefDst := make([]uint8, 16*fDEC_STRIDE)
		iDct := make([]int16, 64)
		iPred := make([]uint8, 16*fDEC_STRIDE)
		iRec := make([]uint8, 16*fDEC_STRIDE)
		for i := 0; i < 64; i++ {
			iDct[i] = int16((cRandInt(r) & 65535) - 32768)
			iRefDct[i] = iDct[i]
		}
		for i := 0; i < 8; i++ {
			for j := 0; j < 8; j++ {
				iPred[i*fDEC_STRIDE+j] = uint8(cRandInt(r) & 255)
				iRefDst[i*fDEC_STRIDE+j] = iPred[i*fDEC_STRIDE+j]
			}
		}
		welsIDctT4Anchor(iRefDst, 0, iRefDct[0:])
		welsIDctT4Anchor(iRefDst, 4, iRefDct[16:])
		welsIDctT4Anchor(iRefDst, 4*fDEC_STRIDE, iRefDct[32:])
		welsIDctT4Anchor(iRefDst, 4*fDEC_STRIDE+4, iRefDct[48:])
		WelsIDctFourT4Rec_c(iRec, 0, fDEC_STRIDE, iPred, 0, fDEC_STRIDE, iDct)
		for i := 0; i < 8; i++ {
			for j := 0; j < 8; j++ {
				if iRec[i*fDEC_STRIDE+j] != iRefDst[i*fDEC_STRIDE+j] {
					t.Fatalf("run %d (%d,%d)", run, i, j)
				}
			}
		}
	}
}

func TestDecodeMbAuxTest_WelsIDctRecI16x16Dc_c(t *testing.T) {
	r := rand.New(rand.NewSource(29))
	for run := 0; run < 100; run++ {
		iRefDst := make([]uint8, 16*fDEC_STRIDE)
		var iRefDct [4][4]int16
		iDct := make([]int16, 16)
		iPred := make([]uint8, 16*fDEC_STRIDE)
		iRec := make([]uint8, 16*fDEC_STRIDE)
		for i := 0; i < 16; i++ {
			for j := 0; j < 16; j++ {
				iPred[i*fDEC_STRIDE+j] = uint8(cRandInt(r) & 255)
				iRefDst[i*fDEC_STRIDE+j] = iPred[i*fDEC_STRIDE+j]
			}
		}
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				iDct[i*4+j] = int16((cRandInt(r) & 65535) - 32768)
				iRefDct[i][j] = iDct[i*4+j]
			}
		}
		// WelsIDctRecI16x16DcAnchor
		for i := 0; i < 4; i++ {
			for y := 0; y < 4; y++ {
				o := (i*4 + y) * fDEC_STRIDE
				for x := 0; x < 16; x++ {
					iRefDst[o+x] = common.WelsClip1(int32(iRefDst[o+x]) + ((int32(iRefDct[i][x>>2]) + 32) >> 6))
				}
			}
		}
		WelsIDctRecI16x16Dc_c(iRec, 0, fDEC_STRIDE, iPred, 0, fDEC_STRIDE, iDct)
		for i := 0; i < 16; i++ {
			for j := 0; j < 16; j++ {
				if iRec[i*fDEC_STRIDE+j] != iRefDst[i*fDEC_STRIDE+j] {
					t.Fatalf("run %d (%d,%d)", run, i, j)
				}
			}
		}
	}
}

func TestDecodeMbAuxTest_WelsGetEncBlockStrideOffset(t *testing.T) {
	pBlock := make([]int32, 24)
	const kiStrideY, kiStrideUV = 64, 32
	WelsGetEncBlockStrideOffset(pBlock, kiStrideY, kiStrideUV)
	// luma 4x4 blocks in scan order: (x, y) block positions
	lumaPos := [16][2]int32{
		{0, 0}, {1, 0}, {0, 1}, {1, 1}, {2, 0}, {3, 0}, {2, 1}, {3, 1},
		{0, 2}, {1, 2}, {0, 3}, {1, 3}, {2, 2}, {3, 2}, {2, 3}, {3, 3},
	}
	for i, p := range lumaPos {
		if want := p[0]*4 + p[1]*4*kiStrideY; pBlock[i] != want {
			t.Fatalf("luma %d: %d want %d", i, pBlock[i], want)
		}
	}
	chromaPos := [4][2]int32{{0, 0}, {1, 0}, {0, 1}, {1, 1}}
	for i, p := range chromaPos {
		// as in the C code, r = j & 2 also scales the chroma row (8 rows per step)
		want := p[0]*4 + p[1]*8*kiStrideUV
		if pBlock[16+i] != want || pBlock[20+i] != want {
			t.Fatalf("chroma %d: %d/%d want %d", i, pBlock[16+i], pBlock[20+i], want)
		}
	}
}

// ---------------------------------------------------------------------------
// EncUT_Reconstruct.cpp: the C implementations against the function table
// filled by WelsInitEncodingFuncs / WelsInitReconstructionFuncs (which, in
// this port, are the same C implementations).
// ---------------------------------------------------------------------------

const reconTestNum = 1000

func fillWithRandomData(r *rand.Rand, p []uint8) {
	for i := range p {
		p[i] = uint8(r.Intn(256))
	}
}

func fillWithRandomInt16(r *rand.Rand, p []int16) {
	for i := range p {
		p[i] = int16(uint16(r.Intn(256)) | uint16(r.Intn(256))<<8)
	}
}

func newReconFuncList() *SWelsFuncPtrList {
	var s SWelsFuncPtrList
	WelsInitReconstructionFuncs(&s, 0)
	WelsInitEncodingFuncs(&s, 0)
	return &s
}

func TestReconstructionFunTest_WelsIDctRecI16x16Dc(t *testing.T) {
	r := rand.New(rand.NewSource(31))
	f := newReconFuncList()
	pRec0 := make([]uint8, 16*16)
	pRec1 := make([]uint8, 16*16)
	pPred := make([]uint8, 32*16)
	pDct := make([]int16, 16)
	for k := 0; k < reconTestNum; k++ {
		fillWithRandomData(r, pPred)
		fillWithRandomInt16(r, pDct)
		for i := range pDct {
			pDct[i] = common.WELS_CLIP3(pDct[i], -4080, 4080)
		}
		WelsIDctRecI16x16Dc_c(pRec0, 0, 16, pPred, 0, 32, pDct)
		f.pfIDctI16x16Dc(pRec1, 0, 16, pPred, 0, 32, pDct)
		for i := range pRec0 {
			if pRec0[i] != pRec1[i] {
				t.Fatalf("k %d idx %d", k, i)
			}
		}
	}
}

func TestReconstructionFunTest_WelsDctIDct(t *testing.T) {
	r := rand.New(rand.NewSource(32))
	f := newReconFuncList()
	pInput1 := make([]uint8, 16*8)
	pInput2 := make([]uint8, 32*8)
	pPred := make([]uint8, 32*8)
	pDct0 := make([]int16, 64)
	pDct1 := make([]int16, 64)
	pRec0 := make([]uint8, 16*8)
	pRec1 := make([]uint8, 16*8)
	check := func(k int) {
		for i := range pDct0 {
			if pDct0[i] != pDct1[i] {
				t.Fatalf("k %d dct idx %d", k, i)
			}
		}
		for i := range pRec0 {
			if pRec0[i] != pRec1[i] {
				t.Fatalf("k %d rec idx %d", k, i)
			}
		}
	}
	for k := 0; k < reconTestNum; k++ {
		fillWithRandomData(r, pInput1)
		fillWithRandomData(r, pInput2)
		fillWithRandomData(r, pPred)
		WelsDctT4_c(pDct0, pInput1, 0, 16, pInput2, 0, 32)
		f.pfDctT4(pDct1, pInput1, 0, 16, pInput2, 0, 32)
		WelsIDctT4Rec_c(pRec0, 0, 16, pPred, 0, 32, pDct0)
		f.pfIDctT4(pRec1, 0, 16, pPred, 0, 32, pDct1)
		check(k)
		WelsDctFourT4_c(pDct0, pInput1, 0, 16, pInput2, 0, 32)
		f.pfDctFourT4(pDct1, pInput1, 0, 16, pInput2, 0, 32)
		WelsIDctFourT4Rec_c(pRec0, 0, 16, pPred, 0, 32, pDct0)
		f.pfIDctFourT4(pRec1, 0, 16, pPred, 0, 32, pDct1)
		check(k)
	}
	// extreme inputs: DCT then IDCT of a constant difference reconstructs it exactly
	for _, c := range [][3]uint8{{255, 0, 0}, {0, 255, 255}} {
		for i := range pInput1 {
			pInput1[i] = c[0]
		}
		for i := range pInput2 {
			pInput2[i] = c[1]
			pPred[i] = c[2]
		}
		WelsDctT4_c(pDct0, pInput1, 0, 16, pInput2, 0, 32)
		f.pfDctT4(pDct1, pInput1, 0, 16, pInput2, 0, 32)
		WelsIDctT4Rec_c(pRec0, 0, 16, pPred, 0, 32, pDct0)
		f.pfIDctT4(pRec1, 0, 16, pPred, 0, 32, pDct1)
		check(-1)
	}
}

func TestReconstructionFunTest_QuantDequant(t *testing.T) {
	r := rand.New(rand.NewSource(33))
	f := newReconFuncList()
	p0 := make([]int16, 64)
	p1 := make([]int16, 64)
	var pMax0, pMax1 [4]int16
	eq := func(k int, n int) {
		for i := 0; i < n; i++ {
			if p0[i] != p1[i] {
				t.Fatalf("k %d idx %d: %d vs %d", k, i, p0[i], p1[i])
			}
		}
	}
	for k := 0; k < reconTestNum; k++ {
		uiQp := r.Intn(52)
		fillWithRandomInt16(r, p0)
		for i := range p0 {
			p0[i] = common.WELS_CLIP3(p0[i], -32000, 32000)
		}
		copy(p1, p0)
		pMF := g_kiQuantMF[uiQp][:]
		pFF := g_iQuantIntraFF[uiQp][:]
		switch k % 6 {
		case 0: // WelsDequant4x4
			WelsQuant4x4_c(p0, pFF, pMF)
			f.pfQuantization4x4(p1, pFF, pMF)
			WelsDequant4x4_c(p0, common.G_kuiDequantCoeff[uiQp][:])
			f.pfDequantization4x4(p1, common.G_kuiDequantCoeff[uiQp][:])
			eq(k, 16)
		case 1: // WelsDequantIHadamard4x4
			WelsQuant4x4_c(p0, pFF, pMF)
			f.pfQuantization4x4(p1, pFF, pMF)
			WelsDequantIHadamard4x4_c(p0, common.G_kuiDequantCoeff[uiQp][0])
			f.pfDequantizationIHadamard4x4(p1, common.G_kuiDequantCoeff[uiQp][0])
			eq(k, 16)
		case 2: // WelsQuant4x4Dc
			WelsQuant4x4Dc_c(p0, pFF[0], pMF[0])
			f.pfQuantizationDc4x4(p1, pFF[0], pMF[0])
			eq(k, 16)
		case 3: // WelsQuantFour4x4Max
			WelsQuantFour4x4Max_c(p0, pFF, pMF, pMax0[:])
			f.pfQuantizationFour4x4Max(p1, pFF, pMF, pMax1[:])
			eq(k, 64)
			if pMax0 != pMax1 {
				t.Fatalf("k %d max", k)
			}
		case 4: // WelsDeQuantFour4x4
			WelsQuantFour4x4_c(p0, pFF, pMF)
			f.pfQuantizationFour4x4(p1, pFF, pMF)
			WelsDequantFour4x4_c(p0, common.G_kuiDequantCoeff[uiQp][:])
			f.pfDequantizationFour4x4(p1, common.G_kuiDequantCoeff[uiQp][:])
			eq(k, 64)
		case 5: // WelsHadamardQuant2x2 / Skip
			for i := range p0 {
				p0[i] = common.WELS_CLIP3(p0[i], -4080, 4080)
			}
			copy(p1, p0)
			s0 := WelsHadamardQuant2x2Skip_c(p0, pFF[0], pMF[0])
			s1 := f.pfQuantizationHadamard2x2Skip(p1, pFF[0], pMF[0])
			if (s0 != 0) != (s1 != 0) {
				t.Fatalf("k %d skip", k)
			}
			var d0, d1, b0, b1 [4]int16
			n0 := WelsHadamardQuant2x2_c(p0, pFF[0], pMF[0], d0[:], b0[:])
			n1 := f.pfQuantizationHadamard2x2(p1, pFF[0], pMF[0], d1[:], b1[:])
			if (n0 != 0) != (n1 != 0) || d0 != d1 || b0 != b1 {
				t.Fatalf("k %d hadamard quant", k)
			}
			eq(k, 64)
		}
	}
}

func TestReconstructionFunTest_WelsGetNoneZeroCountHadamardT4Dc(t *testing.T) {
	r := rand.New(rand.NewSource(34))
	f := newReconFuncList()
	pInput := make([]int16, 64)
	pDct := make([]int16, 16*16)
	var l0, l1 [16]int16
	for k := 0; k < reconTestNum; k++ {
		fillWithRandomInt16(r, pInput)
		if WelsGetNoneZeroCount_c(pInput) != f.pfGetNoneZeroCount(pInput) {
			t.Fatalf("k %d nzc", k)
		}
		fillWithRandomInt16(r, pDct)
		for i := range pDct {
			pDct[i] = common.WELS_CLIP3(pDct[i], -4080, 4080)
		}
		WelsHadamardT4Dc_c(l0[:], pDct)
		f.pfTransformHadamard4x4Dc(l1[:], pDct)
		if l0 != l1 {
			t.Fatalf("k %d hadamard", k)
		}
	}
}
