package processing

// Port of codec/processing/src/downsample/downsample.cpp.

const (
	MAX_SAMPLE_WIDTH  = 1920
	MAX_SAMPLE_HEIGHT = 1088
)

///////////////////////////////////////////////////////////////////////////////////////////////////////////////

func NewCDownsampling(iCpuFlag int32) *CDownsampling {
	d := &CDownsampling{}
	d.initIStrategyBase()
	d.m_iCPUFlag = iCpuFlag
	d.m_eMethod = METHOD_DOWNSAMPLE
	d.m_pfDownsample = SDownsampleFuncs{}
	d.InitDownsampleFuncs(&d.m_pfDownsample, d.m_iCPUFlag)
	// Most single-layer encoders never downsample internally. Allocate the
	// intermediate pictures only when an actual downsampling call needs them.
	return d
}

// AllocateSampleBuffer returns true on failure (as in C); allocations never
// fail in Go.
func (d *CDownsampling) AllocateSampleBuffer() bool {
	for i := 0; i < 2; i++ {
		d.m_pSampleBuffer[i][0] = make([]uint8, MAX_SAMPLE_WIDTH*MAX_SAMPLE_HEIGHT)
		d.m_pSampleBuffer[i][1] = make([]uint8, MAX_SAMPLE_WIDTH*MAX_SAMPLE_HEIGHT/4)
		d.m_pSampleBuffer[i][2] = make([]uint8, MAX_SAMPLE_WIDTH*MAX_SAMPLE_HEIGHT/4)
	}
	return false
}

func (d *CDownsampling) FreeSampleBuffer() {
	for i := 0; i < 2; i++ {
		d.m_pSampleBuffer[i][0] = nil
		d.m_pSampleBuffer[i][1] = nil
		d.m_pSampleBuffer[i][2] = nil
	}
}

func (d *CDownsampling) InitDownsampleFuncs(sDownsampleFunc *SDownsampleFuncs, iCpuFlag int32) {
	sDownsampleFunc.pfHalfAverageWidthx32 = DyadicBilinearDownsampler_c
	sDownsampleFunc.pfHalfAverageWidthx16 = DyadicBilinearDownsampler_c
	sDownsampleFunc.pfOneThirdDownsampler = DyadicBilinearOneThirdDownsampler_c
	sDownsampleFunc.pfQuarterDownsampler = DyadicBilinearQuarterDownsampler_c
	sDownsampleFunc.pfGeneralRatioChroma = GeneralBilinearAccurateDownsampler_c
	sDownsampleFunc.pfGeneralRatioLuma = GeneralBilinearFastDownsampler_c
}

func (d *CDownsampling) Process(iType int32, pSrcPixMap *SPixMap, pDstPixMap *SPixMap) EResult {
	iSrcWidthY := pSrcPixMap.SRect.IRectWidth
	iSrcHeightY := pSrcPixMap.SRect.IRectHeight
	iDstWidthY := pDstPixMap.SRect.IRectWidth
	iDstHeightY := pDstPixMap.SRect.IRectHeight

	iSrcWidthUV := iSrcWidthY >> 1
	iSrcHeightUV := iSrcHeightY >> 1
	iDstWidthUV := iDstWidthY >> 1
	iDstHeightUV := iDstHeightY >> 1

	pDstPix := &pDstPixMap.PPixel
	pDstOff := &pDstPixMap.IPixelOff
	pSrcPix := &pSrcPixMap.PPixel
	pSrcOff := &pSrcPixMap.IPixelOff

	if iSrcWidthY <= iDstWidthY || iSrcHeightY <= iDstHeightY {
		return RET_INVALIDPARAM
	}
	if (iSrcWidthY>>1) > MAX_SAMPLE_WIDTH || (iSrcHeightY>>1) > MAX_SAMPLE_HEIGHT || d.m_bNoSampleBuffer {
		if (iSrcWidthY>>1) == iDstWidthY && (iSrcHeightY>>1) == iDstHeightY {
			// use half average functions
			d.DownsampleHalfAverage(pDstPix[0], pDstOff[0], pDstPixMap.IStride[0],
				pSrcPix[0], pSrcOff[0], pSrcPixMap.IStride[0], iSrcWidthY, iSrcHeightY)
			d.DownsampleHalfAverage(pDstPix[1], pDstOff[1], pDstPixMap.IStride[1],
				pSrcPix[1], pSrcOff[1], pSrcPixMap.IStride[1], iSrcWidthUV, iSrcHeightUV)
			d.DownsampleHalfAverage(pDstPix[2], pDstOff[2], pDstPixMap.IStride[2],
				pSrcPix[2], pSrcOff[2], pSrcPixMap.IStride[2], iSrcWidthUV, iSrcHeightUV)
		} else if (iSrcWidthY>>2) == iDstWidthY && (iSrcHeightY>>2) == iDstHeightY {

			d.m_pfDownsample.pfQuarterDownsampler(pDstPix[0], pDstOff[0], pDstPixMap.IStride[0],
				pSrcPix[0], pSrcOff[0], pSrcPixMap.IStride[0], iSrcWidthY, iSrcHeightY)

			d.m_pfDownsample.pfQuarterDownsampler(pDstPix[1], pDstOff[1], pDstPixMap.IStride[1],
				pSrcPix[1], pSrcOff[1], pSrcPixMap.IStride[1], iSrcWidthUV, iSrcHeightUV)

			d.m_pfDownsample.pfQuarterDownsampler(pDstPix[2], pDstOff[2], pDstPixMap.IStride[2],
				pSrcPix[2], pSrcOff[2], pSrcPixMap.IStride[2], iSrcWidthUV, iSrcHeightUV)

		} else if (iSrcWidthY/3) == iDstWidthY && (iSrcHeightY/3) == iDstHeightY {

			d.m_pfDownsample.pfOneThirdDownsampler(pDstPix[0], pDstOff[0], pDstPixMap.IStride[0],
				pSrcPix[0], pSrcOff[0], pSrcPixMap.IStride[0], iSrcWidthY, iDstHeightY)

			d.m_pfDownsample.pfOneThirdDownsampler(pDstPix[1], pDstOff[1], pDstPixMap.IStride[1],
				pSrcPix[1], pSrcOff[1], pSrcPixMap.IStride[1], iSrcWidthUV, iDstHeightUV)

			d.m_pfDownsample.pfOneThirdDownsampler(pDstPix[2], pDstOff[2], pDstPixMap.IStride[2],
				pSrcPix[2], pSrcOff[2], pSrcPixMap.IStride[2], iSrcWidthUV, iDstHeightUV)

		} else {
			d.m_pfDownsample.pfGeneralRatioLuma(pDstPix[0], pDstOff[0], pDstPixMap.IStride[0], iDstWidthY, iDstHeightY,
				pSrcPix[0], pSrcOff[0], pSrcPixMap.IStride[0], iSrcWidthY, iSrcHeightY)

			d.m_pfDownsample.pfGeneralRatioChroma(pDstPix[1], pDstOff[1], pDstPixMap.IStride[1], iDstWidthUV, iDstHeightUV,
				pSrcPix[1], pSrcOff[1], pSrcPixMap.IStride[1], iSrcWidthUV, iSrcHeightUV)

			d.m_pfDownsample.pfGeneralRatioChroma(pDstPix[2], pDstOff[2], pDstPixMap.IStride[2], iDstWidthUV, iDstHeightUV,
				pSrcPix[2], pSrcOff[2], pSrcPixMap.IStride[2], iSrcWidthUV, iSrcHeightUV)
		}
	} else {
		if d.m_pSampleBuffer[0][0] == nil {
			d.AllocateSampleBuffer()
		}

		iIdx := 0
		iHalfSrcWidth := iSrcWidthY >> 1
		iHalfSrcHeight := iSrcHeightY >> 1
		pSrcY, iSrcYOff := pSrcPix[0], pSrcOff[0]
		pSrcU, iSrcUOff := pSrcPix[1], pSrcOff[1]
		pSrcV, iSrcVOff := pSrcPix[2], pSrcOff[2]
		iSrcStrideY := pSrcPixMap.IStride[0]
		iSrcStrideU := pSrcPixMap.IStride[1]
		iSrcStrideV := pSrcPixMap.IStride[2]

		iDstStrideY := pDstPixMap.IStride[0]
		iDstStrideU := pDstPixMap.IStride[1]
		iDstStrideV := pDstPixMap.IStride[2]

		pDstY := d.m_pSampleBuffer[iIdx][0]
		pDstU := d.m_pSampleBuffer[iIdx][1]
		pDstV := d.m_pSampleBuffer[iIdx][2]
		iIdx++
		for {
			if (iHalfSrcWidth == iDstWidthY) && (iHalfSrcHeight == iDstHeightY) { //end
				// use half average functions
				d.DownsampleHalfAverage(pDstPix[0], pDstOff[0], pDstPixMap.IStride[0],
					pSrcY, iSrcYOff, iSrcStrideY, iSrcWidthY, iSrcHeightY)
				d.DownsampleHalfAverage(pDstPix[1], pDstOff[1], pDstPixMap.IStride[1],
					pSrcU, iSrcUOff, iSrcStrideU, iSrcWidthUV, iSrcHeightUV)
				d.DownsampleHalfAverage(pDstPix[2], pDstOff[2], pDstPixMap.IStride[2],
					pSrcV, iSrcVOff, iSrcStrideV, iSrcWidthUV, iSrcHeightUV)
				break
			} else if (iHalfSrcWidth > iDstWidthY) && (iHalfSrcHeight > iDstHeightY) {
				// use half average functions
				iDstStrideY = WELS_ALIGN(iHalfSrcWidth, 32)
				iDstStrideU = WELS_ALIGN(iHalfSrcWidth>>1, 32)
				iDstStrideV = WELS_ALIGN(iHalfSrcWidth>>1, 32)
				d.DownsampleHalfAverage(pDstY, 0, iDstStrideY,
					pSrcY, iSrcYOff, iSrcStrideY, iSrcWidthY, iSrcHeightY)
				d.DownsampleHalfAverage(pDstU, 0, iDstStrideU,
					pSrcU, iSrcUOff, iSrcStrideU, iSrcWidthUV, iSrcHeightUV)
				d.DownsampleHalfAverage(pDstV, 0, iDstStrideV,
					pSrcV, iSrcVOff, iSrcStrideV, iSrcWidthUV, iSrcHeightUV)

				pSrcY, iSrcYOff = pDstY, 0
				pSrcU, iSrcUOff = pDstU, 0
				pSrcV, iSrcVOff = pDstV, 0

				iSrcWidthY = iHalfSrcWidth
				iSrcWidthUV = iHalfSrcWidth >> 1
				iSrcHeightY = iHalfSrcHeight
				iSrcHeightUV = iHalfSrcHeight >> 1

				iSrcStrideY = iDstStrideY
				iSrcStrideU = iDstStrideU
				iSrcStrideV = iDstStrideV

				iHalfSrcWidth >>= 1
				iHalfSrcHeight >>= 1

				iIdx = iIdx % 2
				pDstY = d.m_pSampleBuffer[iIdx][0]
				pDstU = d.m_pSampleBuffer[iIdx][1]
				pDstV = d.m_pSampleBuffer[iIdx][2]
				iIdx++
			} else {
				d.m_pfDownsample.pfGeneralRatioLuma(pDstPix[0], pDstOff[0], pDstPixMap.IStride[0], iDstWidthY, iDstHeightY,
					pSrcY, iSrcYOff, iSrcStrideY, iSrcWidthY, iSrcHeightY)

				d.m_pfDownsample.pfGeneralRatioChroma(pDstPix[1], pDstOff[1], pDstPixMap.IStride[1], iDstWidthUV, iDstHeightUV,
					pSrcU, iSrcUOff, iSrcStrideU, iSrcWidthUV, iSrcHeightUV)

				d.m_pfDownsample.pfGeneralRatioChroma(pDstPix[2], pDstOff[2], pDstPixMap.IStride[2], iDstWidthUV, iDstHeightUV,
					pSrcV, iSrcVOff, iSrcStrideV, iSrcWidthUV, iSrcHeightUV)
				break
			}
		}
	}
	return RET_SUCCESS
}

func (d *CDownsampling) DownsampleHalfAverage(pDst []uint8, iDstOff int, iDstStride int32,
	pSrc []uint8, iSrcOff int, iSrcStride int32, iSrcWidth int32, iSrcHeight int32) {
	if (iSrcStride & 31) == 0 {
		d.m_pfDownsample.pfHalfAverageWidthx32(pDst, iDstOff, iDstStride,
			pSrc, iSrcOff, iSrcStride, WELS_ALIGN(iSrcWidth&^1, 32), iSrcHeight)
	} else {
		d.m_pfDownsample.pfHalfAverageWidthx16(pDst, iDstOff, iDstStride,
			pSrc, iSrcOff, iSrcStride, WELS_ALIGN(iSrcWidth&^1, 16), iSrcHeight)
	}
}
