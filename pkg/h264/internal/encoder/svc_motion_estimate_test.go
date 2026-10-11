// Port of test/encoder/EncUT_MotionEstimate.cpp and test/encoder/EncUT_SVC_me.cpp
// (C paths only; the SIMD comparisons are not ported).

package encoder

import (
	"math"
	"math/rand"
	"os"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// meTestYUVPixelDataGenerator is a port of test/api/DataGenerator.cpp: it copies a
// random iWidth x iHeight window of the first frame of
// res/CiscoVT2people_160x96_6fps.yuv into pPointer.
func meTestYUVPixelDataGenerator(r *rand.Rand, pPointer []uint8, iWidth, iHeight, iStride int32) bool {
	const SRC_FRAME_WIDTH = 160
	const SRC_FRAME_HEIGHT = 96
	if SRC_FRAME_WIDTH-iWidth <= 0 || SRC_FRAME_HEIGHT-iHeight <= 0 {
		return false
	}
	const kiFrameSize = SRC_FRAME_WIDTH * SRC_FRAME_HEIGHT
	data, err := os.ReadFile("../../../../res/CiscoVT2people_160x96_6fps.yuv")
	if err != nil || len(data) < kiFrameSize {
		return false
	}
	iStartPosX := r.Int31n(SRC_FRAME_WIDTH - iWidth)
	iStartPosY := r.Int31n(SRC_FRAME_HEIGHT - iHeight)
	src := int(iStartPosX + iStartPosY*SRC_FRAME_WIDTH)
	dst := 0
	for j := int32(0); j < iHeight; j++ {
		copy(pPointer[dst:dst+int(iWidth)], data[src:src+int(iWidth)])
		dst += int(iStride)
		src += SRC_FRAME_WIDTH
	}
	return true
}

func meTestRandomPixelData(r *rand.Rand, p []uint8, iWidth, iHeight, iStride int32) {
	for j := int32(0); j < iHeight; j++ {
		for i := int32(0); i < iWidth; i++ {
			p[j*iStride+i] = uint8(r.Intn(256))
		}
	}
}

// meTestCopyTargetBlock copies the block at sTargetMv (relative to pRefPic+iRefOff) into pSrcBlock.
func meTestCopyTargetBlock(pSrcBlock []uint8, kiBlockSize int32, sTargetMv SMVUnitXY, kiRefPicStride int32, pRefPic []uint8, iRefOff int) {
	pTargetPos := iRefOff + int(int32(sTargetMv.iMvY)*kiRefPicStride+int32(sTargetMv.iMvX))
	pSourcePos := 0
	for i := int32(0); i < kiBlockSize; i++ {
		copy(pSrcBlock[pSourcePos:pSourcePos+int(kiBlockSize)], pRefPic[pTargetPos:pTargetPos+int(kiBlockSize)])
		pTargetPos += int(kiRefPicStride)
		pSourcePos += int(kiBlockSize)
	}
}

func meTestInitMe(kuiQp uint8, kuiMvdTableMiddle uint32, kuiMvdTableStride uint32, pMvdCostTable []uint16, pMe *SWelsME) {
	MvdCostInit(pMvdCostTable, int32(kuiMvdTableStride))
	pMe.pMvdCost = pMvdCostTable
	pMe.iMvdCostOff = int(uint32(kuiQp)*kuiMvdTableStride + kuiMvdTableMiddle)
	pMe.sMvp = SMVUnitXY{}
	pMe.sMvBase = SMVUnitXY{}
	pMe.sMv = SMVUnitXY{}
}

type meTestFixture struct {
	r               *rand.Rand
	pRefData        []uint8
	pSrcBlock       []uint8
	uiMvdTableSize  uint32
	pMvdCostTable   []uint16
	iWidth          int32
	iHeight         int32
	iMaxSearchBlock int32
}

func newMeTestFixture(seed int64) *meTestFixture {
	m := &meTestFixture{r: rand.New(rand.NewSource(seed))}
	m.iWidth = 64
	m.iHeight = 64
	m.iMaxSearchBlock = 16
	m.uiMvdTableSize = 1 + (648 << 1)
	m.pRefData = make([]uint8, m.iWidth*m.iHeight)
	m.pSrcBlock = make([]uint8, m.iMaxSearchBlock*m.iMaxSearchBlock)
	m.pMvdCostTable = make([]uint16, 52*m.uiMvdTableSize)
	return m
}

func TestMotionEstimateTest_TestDiamondSearch(t *testing.T) {
	m := newMeTestFixture(1)
	kiPositionToCheck := [5][2]int32{{0, 0}, {0, 1}, {1, 0}, {0, -1}, {-1, 0}}
	const kiMaxBlock16Sad = 72000 //a rough number
	var sFuncList SWelsFuncPtrList
	var sMe SWelsME
	var sSlice SSlice

	kuiQp := uint8(m.r.Intn(52))
	meTestInitMe(kuiQp, 648, m.uiMvdTableSize, m.pMvdCostTable, &sMe)

	WelsInitSampleSadFunc(&sFuncList, 0) //test c functions

	pRefPicCenter := int((m.iHeight/2)*m.iWidth + (m.iWidth / 2))
	for i := 0; i < len(kiPositionToCheck); i++ {
		sTargetMv := SMVUnitXY{int16(kiPositionToCheck[i][0]), int16(kiPositionToCheck[i][1])}
		iTryTimes := 100
		bDataGeneratorSucceed := false
		bFoundMatch := false
		for !bFoundMatch && iTryTimes > 0 {
			iTryTimes--
			if !meTestYUVPixelDataGenerator(m.r, m.pRefData, m.iWidth, m.iHeight, m.iWidth) {
				continue
			}
			bDataGeneratorSucceed = true
			meTestCopyTargetBlock(m.pSrcBlock, 16, sTargetMv, m.iWidth, m.pRefData, pRefPicCenter)

			//clean the sMe status
			sMe.uiBlockSize = uint8(m.r.Intn(5))
			sMe.pEncMb, sMe.iEncMbOff = m.pSrcBlock, 0
			sMe.pRefMb, sMe.iRefMbOff = m.pRefData, pRefPicCenter
			sMe.sMv = SMVUnitXY{}
			sMe.uiSadCost = kiMaxBlock16Sad
			sMe.uiSatdCost = kiMaxBlock16Sad
			WelsDiamondSearch(&sFuncList, &sMe, &sSlice, m.iMaxSearchBlock, m.iWidth)

			bFoundMatch = (sMe.sMv.iMvX == sTargetMv.iMvX || sMe.sMv.iMvX == 0) &&
				(sMe.sMv.iMvY == sTargetMv.iMvY || sMe.sMv.iMvY == 0)
		}
		if bDataGeneratorSucceed && !bFoundMatch {
			t.Fatalf("diamond search: no match for target %v", sTargetMv)
		}
	}
}

type meTestRangeFixture struct {
	r                     *rand.Rand
	pRefStart             []uint8
	pSrc                  []uint8
	pMvdCostTable         []uint16
	iWidth, iHeight       int32
	iWidthExt, iHeightExt int32
	iMbWidth, iMbHeight   int32
	iMvRange              int32
	iMvdRange             int32
	uiMvdInterTableSize   uint32
	uiMvdInterTableStride uint32
}

func newMeTestRangeFixture(seed int64) *meTestRangeFixture {
	m := &meTestRangeFixture{r: rand.New(rand.NewSource(seed))}
	m.iWidth = 320
	m.iHeight = 240
	m.iWidthExt = m.iWidth + 2*common.PADDING_LENGTH
	m.iHeightExt = m.iHeight + 2*common.PADDING_LENGTH
	m.iMbWidth = m.iWidth >> 4
	m.iMbHeight = m.iHeight >> 4
	iUsageType := 0
	iNumDependencyLayers := 1
	if iUsageType != 0 {
		m.iMvRange = EXPANDED_MV_RANGE
		m.iMvdRange = EXPANDED_MVD_RANGE
	} else {
		m.iMvRange = CAMERA_STARTMV_RANGE
		if iNumDependencyLayers == 1 {
			m.iMvdRange = CAMERA_MVD_RANGE
		} else {
			m.iMvdRange = CAMERA_HIGHLAYER_MVD_RANGE
		}
	}
	m.uiMvdInterTableSize = uint32(m.iMvdRange << 2)             //intepel*4=qpel
	m.uiMvdInterTableStride = 1 + (m.uiMvdInterTableSize << 1)   //qpel_mv_range*2=(+/-);
	m.pMvdCostTable = make([]uint16, 52*m.uiMvdInterTableStride) // 52 * aligned size (bytes/2)
	m.pRefStart = make([]uint8, m.iWidthExt*m.iHeightExt)
	m.pSrc = make([]uint8, m.iWidth*m.iHeight)
	return m
}

func TestMotionEstimateRangeTest_TestDiamondSearch(t *testing.T) {
	m := newMeTestRangeFixture(2)
	const kiMaxBlock16Sad = 72000 //a rough number
	pRef := int(common.PADDING_LENGTH*m.iWidthExt + common.PADDING_LENGTH)
	var sFuncList SWelsFuncPtrList
	var sMe SWelsME
	var sSlice SSlice
	kuiQp := uint8(m.r.Intn(52))
	meTestInitMe(kuiQp, m.uiMvdInterTableSize, m.uiMvdInterTableStride, m.pMvdCostTable, &sMe)

	WelsInitSampleSadFunc(&sFuncList, 0) //test c functions

	for i := range m.pSrc {
		m.pSrc[i] = 128
	}
	clear(m.pRefStart)

	sMe.uiBlockSize = BLOCK_16x16

	sMe.sMvp.iMvX = int16(m.r.Int31n(m.iMvRange))
	sMe.sMvp.iMvY = int16(m.r.Int31n(m.iMvRange))

	for h := int32(0); h < m.iHeight; h++ {
		o := pRef + int(h*m.iWidthExt)
		for k := 0; k < int(m.iWidthExt); k++ {
			m.pRefStart[o+k] = uint8(h)
		}
	}

	sMe.pEncMb, sMe.iEncMbOff = m.pSrc, 0

	sMe.sMv = sMe.sMvp

	sMe.uiSadCost = kiMaxBlock16Sad
	sMe.uiSatdCost = kiMaxBlock16Sad
	SetMvWithinIntegerMvRange(m.iMbWidth, m.iMbHeight, 0, 0, m.iMvRange, &sSlice.sMvStartMin, &sSlice.sMvStartMax)

	sMe.pRefMb, sMe.iRefMbOff = m.pRefStart, pRef+int(int32(sMe.sMvp.iMvY)*m.iWidthExt)
	WelsDiamondSearch(&sFuncList, &sMe, &sSlice, m.iWidth, m.iWidthExt)

	if common.WELS_ABS(int32(sMe.sMv.iMvX)) > m.iMvRange {
		t.Fatalf("mvx = %d", sMe.sMv.iMvX)
	}
	if common.WELS_ABS(int32(sMe.sMv.iMvY)) > m.iMvRange {
		t.Fatalf("mvy = %d", sMe.sMv.iMvY)
	}
}

func TestMotionEstimateRangeTest_TestWelsMotionCrossSearch(t *testing.T) {
	m := newMeTestRangeFixture(3)
	var sFuncList SWelsFuncPtrList
	var sMe SWelsME
	var sSlice SSlice
	bUsageType := true
	pRef := int(common.PADDING_LENGTH*m.iWidthExt + common.PADDING_LENGTH)
	const kiMaxBlock16Sad = 72000 //a rough number

	WelsInitSampleSadFunc(&sFuncList, 0) //test c functions
	WelsInitMeFunc(&sFuncList, 0, bUsageType)

	meTestRandomPixelData(m.r, m.pSrc, m.iWidth, m.iHeight, m.iWidth)
	meTestRandomPixelData(m.r, m.pRefStart, m.iWidthExt, m.iHeightExt, m.iWidthExt)

	sMe.uiBlockSize = BLOCK_16x16
	for iMby := int32(0); iMby < m.iMbHeight; iMby++ {
		for iMbx := int32(0); iMbx < m.iMbWidth; iMbx++ {
			kuiQp := uint8(m.r.Intn(52))

			meTestInitMe(kuiQp, m.uiMvdInterTableSize, m.uiMvdInterTableStride, m.pMvdCostTable, &sMe)
			SetMvWithinIntegerMvRange(m.iMbWidth, m.iMbHeight, iMbx, iMby, m.iMvRange, &sSlice.sMvStartMin, &sSlice.sMvStartMax)

			sMe.sMvp.iMvX = int16(m.r.Int31n(m.iMvRange))
			sMe.sMvp.iMvY = int16(m.r.Int31n(m.iMvRange))
			sMe.iCurMeBlockPixX = iMbx << 4
			sMe.iCurMeBlockPixY = iMby << 4
			sMe.pRefMb, sMe.iRefMbOff = m.pRefStart, pRef+int(sMe.iCurMeBlockPixX+sMe.iCurMeBlockPixY*m.iWidthExt)
			sMe.pEncMb, sMe.iEncMbOff = m.pSrc, int(sMe.iCurMeBlockPixX+sMe.iCurMeBlockPixY*m.iWidth)
			sMe.uiSadCost = kiMaxBlock16Sad
			sMe.uiSatdCost = kiMaxBlock16Sad
			sMe.pColoRefMb, sMe.iColoRefMbOff = sMe.pRefMb, sMe.iRefMbOff
			WelsMotionCrossSearch(&sFuncList, &sMe, &sSlice, m.iWidth, m.iWidthExt)
			if common.WELS_ABS(int32(sMe.sMv.iMvX)) > m.iMvRange {
				t.Fatalf("mvx = %d", sMe.sMv.iMvX)
			}
			if common.WELS_ABS(int32(sMe.sMv.iMvY)) > m.iMvRange {
				t.Fatalf("mvy = %d", sMe.sMv.iMvY)
			}
		}
	}
}

func (m *meTestFixture) DoLineTest(t *testing.T, fn PLineFullSearchFunc, vertical bool) {
	const kiMaxBlock16Sad = 72000 //a rough number
	var sFuncList SWelsFuncPtrList
	var sMe SWelsME

	kuiQp := uint8(m.r.Intn(52))
	meTestInitMe(kuiQp, 648, m.uiMvdTableSize, m.pMvdCostTable, &sMe)

	var sTargetMv SMVUnitXY
	WelsInitSampleSadFunc(&sFuncList, 0) //test c functions
	WelsInitMeFunc(&sFuncList, 0, true)

	pRefPicCenter := int((m.iHeight/2)*m.iWidth + (m.iWidth / 2))
	sMe.iCurMeBlockPixX = m.iWidth / 2
	sMe.iCurMeBlockPixY = m.iHeight / 2

	bDataGeneratorSucceed := false
	bFoundMatch := false
	iTryTimes := 100

	if vertical {
		sTargetMv.iMvX = 0
		sTargetMv.iMvY = int16(-sMe.iCurMeBlockPixY + INTPEL_NEEDED_MARGIN + m.r.Int31n(m.iHeight-16-2*INTPEL_NEEDED_MARGIN))
	} else {
		sTargetMv.iMvX = int16(-sMe.iCurMeBlockPixX + INTPEL_NEEDED_MARGIN + m.r.Int31n(m.iWidth-16-2*INTPEL_NEEDED_MARGIN))
		sTargetMv.iMvY = 0
	}
	for !bFoundMatch && iTryTimes > 0 {
		iTryTimes--
		if !meTestYUVPixelDataGenerator(m.r, m.pRefData, m.iWidth, m.iHeight, m.iWidth) {
			continue
		}

		bDataGeneratorSucceed = true
		meTestCopyTargetBlock(m.pSrcBlock, 16, sTargetMv, m.iWidth, m.pRefData, pRefPicCenter)

		//clean the sMe status
		sMe.uiBlockSize = uint8(m.r.Intn(5))
		sMe.pEncMb, sMe.iEncMbOff = m.pSrcBlock, 0
		sMe.pRefMb, sMe.iRefMbOff = m.pRefData, pRefPicCenter
		sMe.pColoRefMb, sMe.iColoRefMbOff = m.pRefData, pRefPicCenter
		sMe.sMv = SMVUnitXY{}
		sMe.uiSadCost = kiMaxBlock16Sad
		sMe.uiSatdCost = kiMaxBlock16Sad
		iCurMeBlockQpelPixX := sMe.iCurMeBlockPixX << 2
		iCurMeBlockQpelPixY := sMe.iCurMeBlockPixY << 2
		iMvdCostX := sMe.iMvdCostOff - int(iCurMeBlockQpelPixX) - int(sMe.sMvp.iMvX) //do the offset here
		iMvdCostY := sMe.iMvdCostOff - int(iCurMeBlockQpelPixY) - int(sMe.sMvp.iMvY)
		iMvdCost := iMvdCostX
		iSize := m.iWidth
		if vertical {
			iMvdCost = iMvdCostY
			iSize = m.iHeight
		}

		//the last selection may be affected by MVDcost, that is when smaller MvY will be better
		if vertical {
			fn(&sFuncList, &sMe, sMe.pMvdCost, iMvdCost, m.iMaxSearchBlock, m.iWidth,
				int16(INTPEL_NEEDED_MARGIN-sMe.iCurMeBlockPixY),
				int16(iSize-INTPEL_NEEDED_MARGIN-16-sMe.iCurMeBlockPixY), vertical)
			bFoundMatch = sMe.sMv.iMvX == 0 &&
				(sMe.sMv.iMvY == sTargetMv.iMvY || common.WELS_ABS(sMe.sMv.iMvY) < common.WELS_ABS(sTargetMv.iMvY))
		} else {
			fn(&sFuncList, &sMe, sMe.pMvdCost, iMvdCost, m.iMaxSearchBlock, m.iWidth,
				int16(INTPEL_NEEDED_MARGIN-sMe.iCurMeBlockPixX),
				int16(iSize-INTPEL_NEEDED_MARGIN-16-sMe.iCurMeBlockPixX), vertical)
			bFoundMatch = sMe.sMv.iMvY == 0 &&
				(sMe.sMv.iMvX == sTargetMv.iMvX || common.WELS_ABS(sMe.sMv.iMvX) < common.WELS_ABS(sTargetMv.iMvX))
		}
	}
	if bDataGeneratorSucceed && !bFoundMatch {
		t.Fatalf("line search (vertical=%v): no match for target %v", vertical, sTargetMv)
	}
}

func TestMotionEstimateTest_TestVerticalSearch(t *testing.T) {
	newMeTestFixture(4).DoLineTest(t, LineFullSearch_c, true)
}

func TestMotionEstimateTest_TestHorizontalSearch(t *testing.T) {
	newMeTestFixture(5).DoLineTest(t, LineFullSearch_c, false)
}

func TestFeatureMotionEstimateTest_TestFeatureSearch(t *testing.T) {
	r := rand.New(rand.NewSource(6))
	const iWidth, iHeight = 64, 64 //size of search window
	const iMaxSearchBlock = 8
	uiMvdTableSize := uint32(1 + (648 << 1))
	pRefData := make([]uint8, iWidth*iHeight)
	pSrcBlock := make([]uint8, iMaxSearchBlock*iMaxSearchBlock)
	pMvdCostTable := make([]uint16, 52*uiMvdTableSize)
	pFeatureSearchPreparation := new(SFeatureSearchPreparation)
	pScreenBlockFeatureStorage := new(SScreenBlockFeatureStorage)
	defer func() {
		ReleaseFeatureSearchPreparation(&pFeatureSearchPreparation.pFeatureOfBlock)
		ReleaseScreenBlockFeatureStorage(pScreenBlockFeatureStorage)
	}()

	const kiMaxBlock16Sad = 72000 //a rough number
	var sFuncList SWelsFuncPtrList
	WelsInitSampleSadFunc(&sFuncList, 0) //test c functions
	WelsInitMeFunc(&sFuncList, 0, true)

	var sMe SWelsME
	kuiQp := uint8(r.Intn(52))
	meTestInitMe(kuiQp, 648, uiMvdTableSize, pMvdCostTable, &sMe)
	sMe.iCurMeBlockPixX = iWidth / 2
	sMe.iCurMeBlockPixY = iHeight / 2
	pRefPicCenter := (iHeight/2)*iWidth + (iWidth / 2)

	var sRef SPicture
	sRef.pData[0] = pRefData
	sRef.iDataOff[0] = 0
	sRef.iLineSize[0] = iWidth
	sRef.iFrameAverageQp = r.Int31n(52)
	sRef.iWidthInPixel = iWidth
	sRef.iHeightInPixel = iHeight

	var sSlice SSlice
	const kiSupposedPaddingLength = 16
	SetMvWithinIntegerMvRange(iWidth/16-kiSupposedPaddingLength, iHeight/16-kiSupposedPaddingLength,
		iWidth/2/16, iHeight/2/16, 508, &sSlice.sMvStartMin, &sSlice.sMvStartMax)
	const kiNeedFeatureStorage = ME_DIA_CROSS_FME
	if iReturn := RequestFeatureSearchPreparation(iWidth, iHeight, kiNeedFeatureStorage, pFeatureSearchPreparation); iReturn != ENC_RETURN_SUCCESS {
		t.Fatalf("RequestFeatureSearchPreparation = %d", iReturn)
	}
	if iReturn := RequestScreenBlockFeatureStorage(iWidth, iHeight, kiNeedFeatureStorage, pScreenBlockFeatureStorage); iReturn != ENC_RETURN_SUCCESS {
		t.Fatalf("RequestScreenBlockFeatureStorage = %d", iReturn)
	}

	for i := int32(sSlice.sMvStartMin.iMvX); i <= int32(sSlice.sMvStartMax.iMvX); i++ {
		for j := int32(sSlice.sMvStartMin.iMvY); j <= int32(sSlice.sMvStartMax.iMvY); j++ {
			if i == 0 || j == 0 {
				continue //exclude x=0 or y=0 since that will be skipped by FME
			}
			if !meTestYUVPixelDataGenerator(r, pRefData, iWidth, iHeight, iWidth) {
				continue
			}
			sTargetMv := SMVUnitXY{int16(i), int16(j)}
			meTestCopyTargetBlock(pSrcBlock, iMaxSearchBlock, sTargetMv, iWidth, pRefData, pRefPicCenter)

			//clean sMe status
			sMe.uiBlockSize = BLOCK_8x8
			sMe.pEncMb, sMe.iEncMbOff = pSrcBlock, 0
			sMe.pRefMb, sMe.iRefMbOff = pRefData, pRefPicCenter
			sMe.pColoRefMb, sMe.iColoRefMbOff = pRefData, pRefPicCenter
			sMe.sMv = SMVUnitXY{}
			sMe.uiSadCost = kiMaxBlock16Sad
			sMe.uiSatdCost = kiMaxBlock16Sad

			//begin FME process
			PerformFMEPreprocess(&sFuncList, &sRef, pFeatureSearchPreparation.pFeatureOfBlock, pScreenBlockFeatureStorage)
			pScreenBlockFeatureStorage.uiSadCostThreshold[BLOCK_8x8] = math.MaxUint32 //to avoid early skip
			uiMaxSearchPoint := uint32(math.MaxInt32)
			var sFeatureSearchIn SFeatureSearchIn
			if SetFeatureSearchIn(&sFuncList, sMe, &sSlice, pScreenBlockFeatureStorage, iMaxSearchBlock, iWidth, &sFeatureSearchIn) {
				MotionEstimateFeatureFullSearch(&sFeatureSearchIn, uiMaxSearchPoint, &sMe)
			}

			fob := pScreenBlockFeatureStorage.pFeatureOfBlockPointer
			bMvMatch := sMe.sMv.iMvX == sTargetMv.iMvX && sMe.sMv.iMvY == sTargetMv.iMvY
			bFeatureMatch := fob[(iHeight/2+int32(sTargetMv.iMvY))*(iWidth-8)+(iWidth/2+int32(sTargetMv.iMvX))] ==
				fob[(iHeight/2+int32(sMe.sMv.iMvY))*(iWidth-8)+(iWidth/2+int32(sMe.sMv.iMvX))] &&
				(int32(sMe.pMvdCost[sMe.iMvdCostOff+int(sMe.sMv.iMvY)<<2])+int32(sMe.pMvdCost[sMe.iMvdCostOff+int(sMe.sMv.iMvX)<<2])) <=
					(int32(sMe.pMvdCost[sMe.iMvdCostOff+int(sTargetMv.iMvY)<<2])+int32(sMe.pMvdCost[sMe.iMvdCostOff+int(sTargetMv.iMvX)<<2]))
			if !(bMvMatch || bFeatureMatch) {
				t.Errorf("TestFeatureSearch Target: %d,%d, Result: %d,%d", sTargetMv.iMvX, sTargetMv.iMvY, sMe.sMv.iMvX, sMe.sMv.iMvY)
			}
		}
	}
}

// Feature search on a frame where the search window is non-empty: the block
// must be found at its exact position (unique random content).
func TestFeatureSearchFindsBlock(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	const iWidth, iHeight = 64, 64
	const iBlk = 8
	pRefData := make([]uint8, iWidth*iHeight)
	meTestRandomPixelData(r, pRefData, iWidth, iHeight, iWidth)
	uiMvdTableSize := uint32(1 + (648 << 1))
	pMvdCostTable := make([]uint16, 52*uiMvdTableSize)

	var sFuncList SWelsFuncPtrList
	WelsInitSampleSadFunc(&sFuncList, 0)
	WelsInitMeFunc(&sFuncList, 0, true)

	var prep SFeatureSearchPreparation
	var storage SScreenBlockFeatureStorage
	if RequestFeatureSearchPreparation(iWidth, iHeight, ME_DIA_CROSS_FME, &prep) != ENC_RETURN_SUCCESS ||
		RequestScreenBlockFeatureStorage(iWidth, iHeight, ME_DIA_CROSS_FME, &storage) != ENC_RETURN_SUCCESS {
		t.Fatal("request failed")
	}
	var sRef SPicture
	sRef.pData[0] = pRefData
	sRef.iLineSize[0] = iWidth
	sRef.iFrameAverageQp = 26
	sRef.iWidthInPixel = iWidth
	sRef.iHeightInPixel = iHeight
	PerformFMEPreprocess(&sFuncList, &sRef, prep.pFeatureOfBlock, &storage)
	if !storage.bRefBlockFeatureCalculated {
		t.Fatal("feature not calculated")
	}
	storage.uiSadCostThreshold[BLOCK_8x8] = math.MaxUint32

	var sSlice SSlice
	sSlice.sMvStartMin = SMVUnitXY{-20, -20}
	sSlice.sMvStartMax = SMVUnitXY{20, 20}

	var sMe SWelsME
	meTestInitMe(20, 648, uiMvdTableSize, pMvdCostTable, &sMe)
	sMe.iCurMeBlockPixX = 24
	sMe.iCurMeBlockPixY = 24
	center := 24*iWidth + 24
	target := SMVUnitXY{7, -5}
	src := make([]uint8, iBlk*iBlk)
	meTestCopyTargetBlock(src, iBlk, target, iWidth, pRefData, center)
	sMe.uiBlockSize = BLOCK_8x8
	sMe.pEncMb, sMe.iEncMbOff = src, 0
	sMe.pRefMb, sMe.iRefMbOff = pRefData, center
	sMe.pColoRefMb, sMe.iColoRefMbOff = pRefData, center
	sMe.uiSadCost = 72000
	var in SFeatureSearchIn
	if !SetFeatureSearchIn(&sFuncList, sMe, &sSlice, &storage, iBlk, iWidth, &in) {
		t.Fatal("SetFeatureSearchIn failed")
	}
	MotionEstimateFeatureFullSearch(&in, math.MaxInt32, &sMe)
	if sMe.sMv != target {
		t.Fatalf("feature search found %v, want %v", sMe.sMv, target)
	}
	if sMe.iRefMbOff != center+int(target.iMvY)*iWidth+int(target.iMvX) {
		t.Fatalf("pRefMb offset mismatch")
	}
}

/********************* EncUT_SVC_me.cpp *********************/

const svcMeTestNum = 10

func meTestFillRandom(r *rand.Rand, p []uint8) {
	for i := range p {
		p[i] = uint8(r.Intn(256))
	}
}

func meTestSumOf8x8SingleBlockRef(pRef []uint8, off int, kiRefStride int32) int32 {
	var iSum int32
	for i := 0; i < 8; i++ {
		for k := 0; k < 8; k++ {
			iSum += int32(pRef[off+k])
		}
		off += int(kiRefStride)
	}
	return iSum
}

func meTestSumOf16x16SingleBlockRef(pRef []uint8, off int, kiRefStride int32) int32 {
	var iSum int32
	for i := 0; i < 16; i++ {
		for k := 0; k < 16; k++ {
			iSum += int32(pRef[off+k])
		}
		off += int(kiRefStride)
	}
	return iSum
}

func meTestSumOfBlockOfFrameRef(single func([]uint8, int, int32) int32, pRefPicture []uint8, kiWidth, kiHeight, kiRefStride int32,
	pFeatureOfBlock []uint16, pTimesOfFeatureValue []uint32) {
	for y := int32(0); y < kiHeight; y++ {
		for x := int32(0); x < kiWidth; x++ {
			iSum := single(pRefPicture, int(kiRefStride*y+x), kiRefStride)
			pFeatureOfBlock[kiWidth*y+x] = uint16(iSum)
			pTimesOfFeatureValue[iSum]++
		}
	}
}

func TestSVC_ME_FunTest_SumOfSingleBlock(t *testing.T) {
	r := rand.New(rand.NewSource(8))
	uiRefBuf := make([]uint8, 16*320)
	for k := 0; k < svcMeTestNum; k++ {
		meTestFillRandom(r, uiRefBuf)
		if a, b := meTestSumOf8x8SingleBlockRef(uiRefBuf, 0, 320), SumOf8x8SingleBlock_c(uiRefBuf, 0, 320); a != b {
			t.Fatalf("SumOf8x8SingleBlock_c: %d != %d", b, a)
		}
		if a, b := meTestSumOf16x16SingleBlockRef(uiRefBuf, 0, 320), SumOf16x16SingleBlock_c(uiRefBuf, 0, 320); a != b {
			t.Fatalf("SumOf16x16SingleBlock_c: %d != %d", b, a)
		}
	}
}

func TestSVC_ME_FunTest_SumOfFrame(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	sizes := [][2]int32{{1, 1}, {1, 320}, {640, 320}}
	for _, sz := range sizes {
		kiWidth, kiHeight := sz[0], sz[1]
		stride := (((kiWidth + 15) >> 4) << 4) + 16
		pRefPicture := make([]uint8, (kiHeight+16)*stride)
		pFeatureOfBlock1 := make([]uint16, kiWidth*kiHeight)
		pFeatureOfBlock2 := make([]uint16, kiWidth*kiHeight)
		iters := svcMeTestNum
		if kiWidth*kiHeight > 10000 {
			iters = 2
		}
		for _, is16 := range []bool{false, true} {
			for k := 0; k < iters; k++ {
				meTestFillRandom(r, pRefPicture)
				var pTimes [2][65536]uint32
				if is16 {
					meTestSumOfBlockOfFrameRef(meTestSumOf16x16SingleBlockRef, pRefPicture, kiWidth, kiHeight, stride, pFeatureOfBlock1, pTimes[0][:])
					SumOf16x16BlockOfFrame_c(pRefPicture, 0, kiWidth, kiHeight, stride, pFeatureOfBlock2, pTimes[1][:])
				} else {
					meTestSumOfBlockOfFrameRef(meTestSumOf8x8SingleBlockRef, pRefPicture, kiWidth, kiHeight, stride, pFeatureOfBlock1, pTimes[0][:])
					SumOf8x8BlockOfFrame_c(pRefPicture, 0, kiWidth, kiHeight, stride, pFeatureOfBlock2, pTimes[1][:])
				}
				for j := range pFeatureOfBlock1 {
					if pFeatureOfBlock1[j] != pFeatureOfBlock2[j] {
						t.Fatalf("%dx%d is16=%v: feature[%d] %d != %d", kiWidth, kiHeight, is16, j, pFeatureOfBlock2[j], pFeatureOfBlock1[j])
					}
				}
				if pTimes[0] != pTimes[1] {
					t.Fatalf("%dx%d is16=%v: times mismatch", kiWidth, kiHeight, is16)
				}
			}
		}
	}
}

// meTestSliceOffset returns the offset of the sub-slice s inside base (both must share the backing array).
func meTestSliceOffset(base, s []uint16) int {
	return cap(base) - cap(s)
}

func TestSVC_ME_FunTest_InitializeHashforFeature(t *testing.T) {
	r := rand.New(rand.NewSource(10))
	for _, sz := range [][2]int32{{10, 10}, {640, 320}} {
		kiWidth, kiHeight := sz[0], sz[1]
		stride := (((kiWidth + 15) >> 4) << 4) + 16
		pRefPicture := make([]uint8, (kiHeight+16)*stride)
		pFeatureOfBlock := make([]uint16, kiWidth*kiHeight)
		pLocation1 := make([]uint16, kiWidth*kiHeight*2)
		pTimesOfFeatureValue := make([]uint32, 65536)
		pLocationFeature1 := make([][]uint16, 65536)
		pFeaturePointValueList1 := make([][]uint16, 65536)
		iters := svcMeTestNum
		if kiWidth*kiHeight > 10000 {
			iters = 2
		}
		for k := 0; k < iters; k++ {
			meTestFillRandom(r, pRefPicture)
			clear(pTimesOfFeatureValue)
			SumOf8x8BlockOfFrame_c(pRefPicture, 0, kiWidth, kiHeight, stride, pFeatureOfBlock, pTimesOfFeatureValue)
			const iActSize = 65536
			InitializeHashforFeature_c(pTimesOfFeatureValue, pLocation1, iActSize, pLocationFeature1, pFeaturePointValueList1)
			// anchor: running offsets
			pos := 0
			for j := 0; j < iActSize; j++ {
				if o := meTestSliceOffset(pLocation1, pLocationFeature1[j]); o != pos {
					t.Fatalf("pLocationOfFeature[%d] at %d, want %d", j, o, pos)
				}
				if o := meTestSliceOffset(pLocation1, pFeaturePointValueList1[j]); o != pos {
					t.Fatalf("pFeatureValuePointerList[%d] at %d, want %d", j, o, pos)
				}
				pos += int(pTimesOfFeatureValue[j] << 1)
			}
		}
	}
}

func TestSVC_ME_FunTest_FillQpelLocationByFeatureValue(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	for _, sz := range [][2]int32{{16, 16}, {640, 320}} {
		kiWidth, kiHeight := sz[0], sz[1]
		stride := (((kiWidth + 15) >> 4) << 4) + 16
		pRefPicture := make([]uint8, (kiHeight+16)*stride)
		pFeatureOfBlock := make([]uint16, kiWidth*kiHeight)
		pLocation1 := make([]uint16, kiWidth*kiHeight*2)
		pLocation2 := make([]uint16, kiWidth*kiHeight*2)
		pTimesOfFeatureValue := make([]uint32, 65536)
		pLocationFeature1 := make([][]uint16, 65536)
		pFeaturePointValueList1 := make([][]uint16, 65536)
		iters := svcMeTestNum
		if kiWidth*kiHeight > 10000 {
			iters = 2
		}
		for k := 0; k < iters; k++ {
			meTestFillRandom(r, pRefPicture)
			clear(pTimesOfFeatureValue)
			SumOf8x8BlockOfFrame_c(pRefPicture, 0, kiWidth, kiHeight, stride, pFeatureOfBlock, pTimesOfFeatureValue)
			const iActSize = 65536
			// anchor with integer offsets
			ptr := make([]int, iActSize)
			pos := 0
			for j := 0; j < iActSize; j++ {
				ptr[j] = pos
				pos += int(pTimesOfFeatureValue[j] << 1)
			}
			iQpelY := 0
			for y := int32(0); y < kiHeight; y++ {
				for x := int32(0); x < kiWidth; x++ {
					f := pFeatureOfBlock[y*kiWidth+x]
					pLocation1[ptr[f]] = uint16(x << 2)
					pLocation1[ptr[f]+1] = uint16(iQpelY)
					ptr[f] += 2
				}
				iQpelY += 4
			}
			InitializeHashforFeature_c(pTimesOfFeatureValue, pLocation2, iActSize, pLocationFeature1, pFeaturePointValueList1)
			FillQpelLocationByFeatureValue_c(pFeatureOfBlock, kiWidth, kiHeight, pFeaturePointValueList1)
			for j := range pLocation1 {
				if pLocation1[j] != pLocation2[j] {
					t.Fatalf("%dx%d: location[%d] %d != %d", kiWidth, kiHeight, j, pLocation2[j], pLocation1[j])
				}
			}
		}
	}
}

// Diamond search on a 1-D gradient must walk to the exact target.
func TestDiamondSearchFindsTarget(t *testing.T) {
	const w, h = 96, 96
	refX := make([]uint8, w*h)
	refY := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			refX[y*w+x] = uint8(2 * x)
			refY[y*w+x] = uint8(2 * y)
		}
	}
	tbl := make([]uint16, 52*(1+(648<<1)))
	var sFuncList SWelsFuncPtrList
	WelsInitSampleSadFunc(&sFuncList, 0)
	var sSlice SSlice
	sSlice.sMvStartMin = SMVUnitXY{-16, -16}
	sSlice.sMvStartMax = SMVUnitXY{16, 16}
	center := 32*w + 32
	cases := []struct {
		ref    []uint8
		target SMVUnitXY
	}{{refX, SMVUnitXY{3, 0}}, {refX, SMVUnitXY{-4, 0}}, {refY, SMVUnitXY{0, 5}}, {refY, SMVUnitXY{0, -3}}}
	for _, c := range cases {
		ref, target := c.ref, c.target
		var sMe SWelsME
		meTestInitMe(0, 648, 1+(648<<1), tbl, &sMe)
		src := make([]uint8, 256)
		meTestCopyTargetBlock(src, 16, target, w, ref, center)
		sMe.uiBlockSize = BLOCK_16x16
		sMe.pEncMb, sMe.iEncMbOff = src, 0
		sMe.pRefMb, sMe.iRefMbOff = ref, center
		sMe.uiSadCost = uint32(common.WelsSampleSad16x16_c(src, 0, 16, ref, center, w)) + uint32(COST_MVD(tbl, sMe.iMvdCostOff, 0, 0))
		WelsDiamondSearch(&sFuncList, &sMe, &sSlice, 16, w)
		if sMe.sMv != target {
			t.Fatalf("diamond: got %v want %v", sMe.sMv, target)
		}
		if sMe.iRefMbOff != center+int(target.iMvY)*w+int(target.iMvX) {
			t.Fatalf("diamond: ref offset mismatch")
		}
	}
}
