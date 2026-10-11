package decoder

import (
	"bytes"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Port of TEST (DecoderBitStreamBoundsTest, DecInitBitsHandlesShortSeedBytesSafely)
// from test/decoder/DecUT_ParseSyntax.cpp.
func TestDecInitBitsHandlesShortSeedBytesSafely(t *testing.T) {
	uiBuf := []uint8{0xff, 0xff, 0xff}
	var sBs common.SBitStringAux

	if r := DecInitBits(&sBs, uiBuf, 0, 24); r != ERR_NONE {
		t.Fatalf("DecInitBits = %d", r)
	}
	var uiCode uint32
	if r := BsGetBits(&sBs, 16, &uiCode); r != ERR_NONE {
		t.Fatalf("read 1 = %d", r)
	}
	if r := BsGetBits(&sBs, 16, &uiCode); r != ERR_NONE {
		t.Fatalf("read 2 = %d", r)
	}
	if r := BsGetBits(&sBs, 16, &uiCode); r != ERR_INFO_READ_OVERFLOW {
		t.Fatalf("read 3 = %d, want ERR_INFO_READ_OVERFLOW", r)
	}
}

// Port of TEST (DecoderBitStreamBoundsTest, BsGetBitsStopsOnTwoByteOverread).
func TestBsGetBitsStopsOnTwoByteOverread(t *testing.T) {
	uiBuf := []uint8{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	var sBs common.SBitStringAux

	if r := DecInitBits(&sBs, uiBuf, 0, 48); r != ERR_NONE {
		t.Fatalf("DecInitBits = %d", r)
	}
	var uiCode uint32
	for i := 0; i < 3; i++ {
		if r := BsGetBits(&sBs, 16, &uiCode); r != ERR_NONE {
			t.Fatalf("read %d = %d", i, r)
		}
	}
	if r := BsGetBits(&sBs, 16, &uiCode); r != ERR_INFO_READ_OVERFLOW {
		t.Fatalf("read 4 = %d, want ERR_INFO_READ_OVERFLOW", r)
	}
}

func TestDecInitBitsOffsetAndNil(t *testing.T) {
	var sBs common.SBitStringAux
	if r := DecInitBits(&sBs, nil, 0, 8); r != ERR_INFO_INVALID_ACCESS {
		t.Fatalf("nil buffer: got %d", r)
	}
	buf := []uint8{0x11, 0x22, 0xab, 0xcd, 0xef, 0x01}
	if r := DecInitBits(&sBs, buf, 2, 32); r != ERR_NONE {
		t.Fatalf("DecInitBits = %d", r)
	}
	if sBs.PStartBuf != 2 || sBs.PEndBuf != 6 || sBs.IBits != 32 || sBs.PCurBuf != 6 || sBs.ILeftBits != -16 {
		t.Fatalf("unexpected state %+v", sBs)
	}
	var uiCode uint32
	if r := BsGetBits(&sBs, 24, &uiCode); r != ERR_NONE || uiCode != 0xabcdef {
		t.Fatalf("BsGetBits = %d, %#x", r, uiCode)
	}
	if GetValue4Bytes(buf[2:]) != 0xabcdef01 {
		t.Fatal("GetValue4Bytes")
	}
	if GetValue4BytesSafe(buf[4:], 2) != 0xef010000 {
		t.Fatal("GetValue4BytesSafe")
	}
}

func TestRBSP2EBSP(t *testing.T) {
	src := []uint8{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x03}
	want := []uint8{0x00, 0x00, 0x03, 0x01, 0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x03}
	dst := make([]uint8, 32)
	RBSP2EBSP(dst, src, int32(len(src)))
	if !bytes.Equal(dst[:len(want)], want) {
		t.Fatalf("got % x want % x", dst[:len(want)], want)
	}
}
