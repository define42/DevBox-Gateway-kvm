//go:build !amd64

package encoder

func WelsIDctT4Rec_sse2(rec []byte, recOff int, recStride int32, pred []byte, predOff int, predStride int32, coeff []int16) {
	WelsIDctT4Rec_c(rec, recOff, recStride, pred, predOff, predStride, coeff)
}

func WelsIDctFourT4Rec_sse2(rec []byte, recOff int, recStride int32, pred []byte, predOff int, predStride int32, coeff []int16) {
	WelsIDctFourT4Rec_c(rec, recOff, recStride, pred, predOff, predStride, coeff)
}

func WelsIDctRecI16x16Dc_sse2(rec []byte, recOff int, recStride int32, pred []byte, predOff int, predStride int32, coeff []int16) {
	WelsIDctRecI16x16Dc_c(rec, recOff, recStride, pred, predOff, predStride, coeff)
}
