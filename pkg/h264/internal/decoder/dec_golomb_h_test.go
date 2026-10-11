package decoder

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// initReaderForTest mirrors DecInitBits + InitReadBits (bit_stream.cpp)
// without depending on bit_stream.go.
func initReaderForTest(pBs *common.SBitStringAux, buf []uint8, off int, iBits int32) {
	kiSizeBuf := int((iBits + 7) >> 3)
	pBs.PBuf = buf
	pBs.PStartBuf = off
	pBs.PEndBuf = off + kiSizeBuf
	pBs.IBits = iBits
	pBs.PCurBuf = off
	var v uint32
	for i := 0; i < 4; i++ {
		v <<= 8
		if i < kiSizeBuf {
			v |= uint32(buf[off+i])
		}
	}
	pBs.UiCurBits = v
	pBs.PCurBuf += 4
	pBs.ILeftBits = -16
}

func TestGolombRoundTrip(t *testing.T) {
	ues := []uint32{0, 1, 2, 3, 7, 8, 254, 255, 256, 1000, 32767, 65534}
	ses := []int32{0, 1, -1, 2, -2, 100, -100, 32767, -32768}
	buf := make([]uint8, 1024)
	var w common.SBitStringAux
	common.InitBits(&w, buf, 3, int32(len(buf)-3))
	for _, v := range ues {
		common.BsWriteUE(&w, v)
	}
	for _, v := range ses {
		common.BsWriteSE(&w, v)
	}
	common.BsWriteBits(&w, 5, 0x15)
	common.BsWriteOneBit(&w, 1)
	common.BsRbspTrailingBits(&w)
	nBytes := w.PCurBuf - 3

	var r common.SBitStringAux
	initReaderForTest(&r, buf, 3, int32(nBytes*8)-BsGetTrailingBits(buf[3+nBytes-1:]))
	for _, want := range ues {
		var got uint32
		if ret := BsGetUe(&r, &got); ret != ERR_NONE || got != want {
			t.Fatalf("BsGetUe = %d (ret %d), want %d", got, ret, want)
		}
	}
	for _, want := range ses {
		var got int32
		if ret := BsGetSe(&r, &got); ret != ERR_NONE || got != want {
			t.Fatalf("BsGetSe = %d (ret %d), want %d", got, ret, want)
		}
	}
	var code uint32
	if BsGetBits(&r, 5, &code); code != 0x15 {
		t.Fatalf("BsGetBits = %#x", code)
	}
	if BsGetOneBit(&r, &code); code != 1 {
		t.Fatalf("BsGetOneBit = %d", code)
	}
	if CheckMoreRBSPData(&r) {
		t.Fatalf("CheckMoreRBSPData: expected only trailing bits left")
	}
}

func TestGetPrefixBits(t *testing.T) {
	for i := uint32(0); i < 32; i++ {
		if got := GetPrefixBits(uint32(1) << i); got != 32-i {
			t.Fatalf("GetPrefixBits(1<<%d) = %d", i, got)
		}
	}
}

func TestInitVlcTable(t *testing.T) {
	var v SVlcTable
	InitVlcTable(&v)
	if len(v.kpCoeffTokenVlcTable[0][0]) != 256 || len(v.kpTotalZerosTable[1][2]) != 2 || len(v.kpZeroTable[6]) != 8 {
		t.Fatal("unexpected VLC table sizes")
	}
}
