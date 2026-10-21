package processing

// Port of codec/processing/src/vaacalc/vaacalculation.cpp.

func NewCVAACalculation(iCpuFlag int32) *CVAACalculation {
	c := &CVAACalculation{}
	c.initIStrategyBase()
	c.m_iCPUFlag = iCpuFlag
	c.m_eMethod = METHOD_VAA_STATISTICS

	c.m_sCalcParam = SVAACalcParam{}
	c.m_sVaaFuncs = SVaaFuncs{}
	c.InitVaaFuncs(&c.m_sVaaFuncs, c.m_iCPUFlag)
	return c
}

func (c *CVAACalculation) InitVaaFuncs(sVaaFuncs *SVaaFuncs, iCpuFlag int32) {
	sVaaFuncs.pfVAACalcSad = VAACalcSad_c
	sVaaFuncs.pfVAACalcSadBgd = VAACalcSadBgd_c
	sVaaFuncs.pfVAACalcSadSsd = VAACalcSadSsd_c
	sVaaFuncs.pfVAACalcSadSsdBgd = VAACalcSadSsdBgd_c
	sVaaFuncs.pfVAACalcSadVar = VAACalcSadVar_c
	initVaaSIMDFuncs(sVaaFuncs, uint32(iCpuFlag))
}

func (c *CVAACalculation) Process(iType int32, pSrcPixMap *SPixMap, pRefPixMap *SPixMap) EResult {
	unchanged := c.m_sCalcParam.UnchangedMacroblocks
	c.m_sCalcParam.UnchangedMacroblocks = nil // Never retain caller-owned damage hints.
	pCurData := pSrcPixMap.PPixel[0]
	iCurOff := pSrcPixMap.IPixelOff[0]
	pRefData := pRefPixMap.PPixel[0]
	iRefOff := pRefPixMap.IPixelOff[0]
	iPicWidth := pSrcPixMap.SRect.IRectWidth
	iPicHeight := pSrcPixMap.SRect.IRectHeight
	iPicStride := pSrcPixMap.IStride[0]

	pResult := c.m_sCalcParam.PCalcResult

	if pCurData == nil || pRefData == nil {
		return RET_INVALIDPARAM
	}

	pResult.PCurY = pCurData[iCurOff:]
	pResult.PRefY = pRefData[iRefOff:]
	if c.m_sCalcParam.ICalcBgd != 0 {
		if c.m_sCalcParam.ICalcSsd != 0 {
			c.m_sVaaFuncs.pfVAACalcSadSsdBgd(pCurData, iCurOff, pRefData, iRefOff, iPicWidth, iPicHeight, iPicStride, &pResult.IFrameSad,
				pResult.PSad8x8, pResult.PSum16x16, pResult.PSumOfSquare16x16, pResult.PSsd16x16,
				pResult.PSumOfDiff8x8, pResult.PMad8x8)
		} else {
			c.m_sVaaFuncs.pfVAACalcSadBgd(pCurData, iCurOff, pRefData, iRefOff, iPicWidth, iPicHeight, iPicStride, &pResult.IFrameSad,
				pResult.PSad8x8, pResult.PSumOfDiff8x8, pResult.PMad8x8)
		}
	} else {
		if c.m_sCalcParam.ICalcSsd != 0 {
			c.m_sVaaFuncs.pfVAACalcSadSsd(pCurData, iCurOff, pRefData, iRefOff, iPicWidth, iPicHeight, iPicStride, &pResult.IFrameSad,
				pResult.PSad8x8, pResult.PSum16x16, pResult.PSumOfSquare16x16, pResult.PSsd16x16)
		} else {
			if c.m_sCalcParam.ICalcVar != 0 {
				c.m_sVaaFuncs.pfVAACalcSadVar(pCurData, iCurOff, pRefData, iRefOff, iPicWidth, iPicHeight, iPicStride, &pResult.IFrameSad,
					pResult.PSad8x8, pResult.PSum16x16, pResult.PSumOfSquare16x16)
			} else {
				vaaCalcSadMasked(c.m_sVaaFuncs.pfVAACalcSad, pCurData, iCurOff, pRefData, iRefOff,
					iPicWidth, iPicHeight, iPicStride, &pResult.IFrameSad, pResult.PSad8x8, unchanged)
			}
		}
	}

	return RET_SUCCESS
}

// Set takes a *SVAACalcParam.
func (c *CVAACalculation) Set(iType int32, pParam any) EResult {
	p, ok := pParam.(*SVAACalcParam)
	if !ok || p == nil || p.PCalcResult == nil {
		return RET_INVALIDPARAM
	}

	c.m_sCalcParam = *p

	return RET_SUCCESS
}
