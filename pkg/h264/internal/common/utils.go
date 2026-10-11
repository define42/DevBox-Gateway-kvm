package common

import (
	"fmt"
	"math"
	"reflect"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

// Port of codec/common/src/utils.cpp.

// formatThisPointer renders logCtx->pCodecInstance the way "0x%p" does in
// the C code.
func formatThisPointer(p any) string {
	if p == nil {
		return "0x(nil)"
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Pointer, reflect.UnsafePointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		if v.IsNil() {
			return "0x(nil)"
		}
		return fmt.Sprintf("0x%p", p)
	}
	return fmt.Sprintf("0x%v", p)
}

// WelsLog formats a log message prefixed with the OpenH264 tag and passes it
// to logCtx.PfLog. kpFmt may use C printf syntax (length modifiers such as
// %lld / %u are accepted, see WelsVsnprintf) or Go fmt syntax.
func WelsLog(logCtx *SLogContext, iLevel int32, kpFmt string, args ...any) {
	if logCtx == nil || logCtx.PfLog == nil {
		return
	}
	var pTraceTag string
	this := formatThisPointer(logCtx.PCodecInstance)
	switch iLevel {
	case api.WELS_LOG_ERROR:
		pTraceTag = "[OpenH264] this = " + this + ", Error:"
	case api.WELS_LOG_WARNING:
		pTraceTag = "[OpenH264] this = " + this + ", Warning:"
	case api.WELS_LOG_INFO:
		pTraceTag = "[OpenH264] this = " + this + ", Info:"
	case api.WELS_LOG_DEBUG:
		pTraceTag = "[OpenH264] this = " + this + ", Debug:"
	default:
		pTraceTag = "[OpenH264] this = " + this + ", Detail:"
	}
	WelsStrcat(&pTraceTag, MAX_LOG_SIZE, kpFmt)
	logCtx.PfLog(logCtx.PLogCtx, iLevel, pTraceTag, args)
}

// CONST_FACTOR_PSNR is (10.0 / log(10.0)).
var CONST_FACTOR_PSNR = 10.0 / math.Log(10.0) // for good computation

// CALC_PSNR is ((float)(CONST_FACTOR_PSNR * log( 65025.0 * w * h / s ))).
func CALC_PSNR(w, h int32, s int64) float32 {
	return float32(CONST_FACTOR_PSNR * math.Log(65025.0*float64(w)*float64(h)/float64(s)))
}

// WelsCalcPsnr calculates the PSNR between the target picture
// (kpTarPic[kiTarOff:], stride kiTarStride) and the reference picture
// (kpRefPic[kiRefOff:], stride kiRefStride) of kiWidth x kiHeight pixels.
func WelsCalcPsnr(kpTarPic []uint8, kiTarOff int, kiTarStride int32, kpRefPic []uint8, kiRefOff int, kiRefStride int32,
	kiWidth int32, kiHeight int32) float32 {
	var iSqe int64

	if kpTarPic == nil || kpRefPic == nil {
		return -1.0
	}

	for y := int32(0); y < kiHeight; y++ { // OPTable !!
		for x := int32(0); x < kiWidth; x++ {
			kiT := int32(kpTarPic[kiTarOff+int(y*kiTarStride+x)]) - int32(kpRefPic[kiRefOff+int(y*kiRefStride+x)])
			iSqe += int64(kiT * kiT)
		}
	}
	if 0 == iSqe {
		return 99.99
	}
	return CALC_PSNR(kiWidth, kiHeight, iSqe)
}
