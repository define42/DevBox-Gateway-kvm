package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

var i4WordCases = []struct {
	name      string
	got, want PGetIntraPredFunc
}{
	{"V", WelsI4x4LumaPredV_c, legacyI4WordWelsI4x4LumaPredV_c},
	{"H", WelsI4x4LumaPredH_c, legacyI4WordWelsI4x4LumaPredH_c},
	{"DC", WelsI4x4LumaPredDc_c, legacyI4WordWelsI4x4LumaPredDc_c},
	{"DCLeft", WelsI4x4LumaPredDcLeft_c, legacyI4WordWelsI4x4LumaPredDcLeft_c},
	{"DCTop", WelsI4x4LumaPredDcTop_c, legacyI4WordWelsI4x4LumaPredDcTop_c},
	{"DCNA", WelsI4x4LumaPredDcNA_c, legacyI4WordWelsI4x4LumaPredDcNA_c},
}

func TestI4WordPredictorParity(t *testing.T) {
	rng := rand.New(rand.NewSource(4257))
	for _, tc := range i4WordCases {
		for _, stride := range []int32{-31, 0, 1, 4, 31, 2048} {
			t.Run(fmt.Sprintf("%s/stride%d", tc.name, stride), func(t *testing.T) {
				for pattern := 0; pattern < 25; pattern++ {
					for _, alias := range []bool{false, true} {
						for _, dstOff := range []int{0, 3, 8188, 8191, 8192, 8193, 8196} {
							ref := make([]byte, 16384)
							rng.Read(ref)
							if pattern < 2 {
								for i := range ref {
									ref[i] = byte(pattern * 255)
								}
							}
							a, z := bytes.Repeat([]byte{0x5a}, 16384), bytes.Repeat([]byte{0x5a}, 16384)
							ar, zr := bytes.Clone(ref), bytes.Clone(ref)
							if alias {
								a, z = ar, zr
							}
							tc.got(a, dstOff, ar, 8192, stride)
							tc.want(z, dstOff, zr, 8192, stride)
							if !bytes.Equal(a, z) || !bytes.Equal(ar, zr) {
								t.Fatalf("pattern=%d alias=%v offset=%d", pattern, alias, dstOff)
							}
						}
					}
				}
			})
		}
	}
}

func TestI4WordFillAllValues(t *testing.T) {
	for value := 0; value < 256; value++ {
		for off := 0; off < 17; off++ {
			a, z := bytes.Repeat([]byte{0xa5}, 33), bytes.Repeat([]byte{0xa5}, 33)
			WelsFillingPred1to16_c(a, off, byte(value))
			legacyI4WordWelsFillingPred1to16_c(z, off, byte(value))
			if !bytes.Equal(a, z) {
				t.Fatalf("value=%d offset=%d", value, off)
			}
		}
	}
}

func legacyI4WordWelsFillingPred1to16_c(pPred []uint8, iPredOff int, kuiSrc uint8) {
	legacyI4WordintraPredFillBytes(pPred[iPredOff:iPredOff+16], kuiSrc)
}

func legacyI4WordintraPredFillBytes(p []uint8, v uint8) {
	for i := range p {
		p[i] = v
	}
}

func legacyI4WordWelsI4x4LumaPredV_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	var uiSrcx2 [8]uint8
	top := iRefOff - int(kiStride)
	copy(uiSrcx2[0:4], pRef[top:top+4])
	copy(uiSrcx2[4:8], pRef[top:top+4])

	WelsFillingPred8to16_c(pPred, iPredOff, uiSrcx2[:])
}

func legacyI4WordWelsI4x4LumaPredH_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	kiStridex2Left := int((kiStride << 1) - 1)
	kiStridex3Left := int(kiStride) + kiStridex2Left
	kuiHor1 := pRef[iRefOff-1]
	kuiHor2 := pRef[iRefOff+int(kiStride)-1]
	kuiHor3 := pRef[iRefOff+kiStridex2Left]
	kuiHor4 := pRef[iRefOff+kiStridex3Left]
	var uiSrc [16]uint8
	legacyI4WordintraPredFillBytes(uiSrc[0:4], kuiHor1)
	legacyI4WordintraPredFillBytes(uiSrc[4:8], kuiHor2)
	legacyI4WordintraPredFillBytes(uiSrc[8:12], kuiHor3)
	legacyI4WordintraPredFillBytes(uiSrc[12:16], kuiHor4)

	WelsFillingPred8x2to16_c(pPred, iPredOff, uiSrc[:])
}

func legacyI4WordWelsI4x4LumaPredDc_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kuiDcValue := uint8((r(-1) + r(s-1) + r((s<<1)-1) + r((s<<1)+s-1) +
		r(-s) + r(1-s) + r(2-s) + r(3-s) + 4) >> 3)

	legacyI4WordWelsFillingPred1to16_c(pPred, iPredOff, kuiDcValue)
}

func legacyI4WordWelsI4x4LumaPredDcLeft_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kuiDcValue := uint8((r(-1) + r(s-1) + r((s<<1)-1) + r((s<<1)+s-1) + 2) >> 2)

	legacyI4WordWelsFillingPred1to16_c(pPred, iPredOff, kuiDcValue)
}

func legacyI4WordWelsI4x4LumaPredDcTop_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kuiDcValue := uint8((r(-s) + r(1-s) + r(2-s) + r(3-s) + 2) >> 2)

	legacyI4WordWelsFillingPred1to16_c(pPred, iPredOff, kuiDcValue)
}

func legacyI4WordWelsI4x4LumaPredDcNA_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	const kuiDcValue = 0x80

	legacyI4WordWelsFillingPred1to16_c(pPred, iPredOff, kuiDcValue)
}

func TestI4WordExactInputAndOutputSpans(t *testing.T) {
	// These dimensions give each predictor exactly its last required input byte.
	for _, tc := range []struct {
		name      string
		fn        PGetIntraPredFunc
		size, off int
		stride    int32
	}{
		{"V", WelsI4x4LumaPredV_c, 4, 4, 4},
		{"H", WelsI4x4LumaPredH_c, 13, 1, 4},
		{"DC", WelsI4x4LumaPredDc_c, 16, 4, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := make([]byte, tc.size)
			for i := range ref {
				ref[i] = byte(i * 17)
			}
			dst := make([]byte, 16)
			tc.fn(dst, 0, ref, tc.off, tc.stride)
			mustPanic := func(fn func()) {
				t.Helper()
				defer func() {
					if recover() == nil {
						t.Fatal("short buffer accepted")
					}
				}()
				fn()
			}
			mustPanic(func() { tc.fn(make([]byte, 15), 0, ref, tc.off, tc.stride) })
			mustPanic(func() { tc.fn(dst, 0, ref[:len(ref)-1:len(ref)-1], tc.off, tc.stride) })
		})
	}
}
