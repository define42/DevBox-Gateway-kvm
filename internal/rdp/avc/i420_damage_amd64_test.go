//go:build amd64

package avc

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"golang.org/x/sys/cpu"
)

func TestRGB32ToI420DamageMatchesScalar(t *testing.T) {
	t.Parallel()
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 is unavailable")
	}
	for _, width := range []int{32, 64, 96, 256, maxDimension} {
		for _, redIndex := range []int{0, 2} {
			t.Run(fmt.Sprintf("width%d/red%d", width, redIndex), func(t *testing.T) {
				t.Parallel()
				random := rand.New(rand.NewPCG(uint64(width), uint64(redIndex+7))) //nolint:gosec // Deterministic test data.
				src0, src1 := make([]byte, width*4), make([]byte, width*4)
				planes := [][]byte{make([]byte, width), make([]byte, width), make([]byte, width/2), make([]byte, width/2)}
				for _, row := range append(slices.Clone(planes), src0, src1) {
					for i := range row {
						row[i] = byte(random.Uint32())
					}
				}
				wantY0, wantY1, wantU, wantV := referenceRGB32I420Rows(src0, src1, width, redIndex == 2)
				want := [][]byte{wantY0, wantY1, wantU, wantV}
				mask := make([]bool, width/16)
				wantMask := make([]bool, len(mask))
				wantChanged := false
				for block := range mask {
					mask[block], wantMask[block] = true, true
					for plane, row := range planes {
						blockSize := 16
						if plane >= 2 {
							blockSize = 8
						}
						start, end := block*blockSize, (block+1)*blockSize
						if !bytes.Equal(row[start:end], want[plane][start:end]) {
							wantMask[block], wantChanged = false, true
						}
					}
				}
				changed, ok := rgb32ToI420Damage(planes[0], planes[1], planes[2], planes[3], src0, src1, mask, redIndex)
				if !ok || changed != wantChanged || !slices.Equal(mask, wantMask) {
					t.Fatalf("conversion = changed %t, ok %t, mask %v; want changed %t, mask %v",
						changed, ok, mask, wantChanged, wantMask)
				}
				checkI420SIMDPlanes(t, planes[0], planes[1], planes[2], planes[3], wantY0, wantY1, wantU, wantV)
			})
		}
	}
}

// Every luma and chroma sample must clear exactly its own macroblock, including
// the low/high lanes and later 32-pixel iterations. Previously cleared bits
// represent earlier row pairs and must remain cleared on an unchanged retry.
func TestRGB32ToI420DamageMaskSamples(t *testing.T) {
	t.Parallel()
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 is unavailable")
	}
	const width = 96
	src0, src1 := make([]byte, width*4), make([]byte, width*4)
	for i := range src0 {
		src0[i], src1[i] = byte(i*29+17), byte(i*43+91)
	}
	for _, redIndex := range []int{0, 2} {
		wantY0, wantY1, wantU, wantV := referenceRGB32I420Rows(src0, src1, width, redIndex == 2)
		want := [][]byte{wantY0, wantY1, wantU, wantV}
		for plane, row := range want {
			for index := range row {
				planes := [][]byte{bytes.Clone(wantY0), bytes.Clone(wantY1), bytes.Clone(wantU), bytes.Clone(wantV)}
				planes[plane][index] ^= 1
				blockSize := 16
				if plane >= 2 {
					blockSize = 8
				}
				block := index / blockSize
				mask := make([]bool, width/16)
				for i := range mask {
					mask[i] = true
				}
				mask[(block+1)%len(mask)] = false
				wantMask := slices.Clone(mask)
				wantMask[block] = false
				changed, ok := rgb32ToI420Damage(planes[0], planes[1], planes[2], planes[3], src0, src1, mask, redIndex)
				if !ok || !changed || !slices.Equal(mask, wantMask) {
					t.Fatalf("red%d plane%d sample%d: changed %t, ok %t, mask %v; want %v",
						redIndex, plane, index, changed, ok, mask, wantMask)
				}
				checkI420SIMDPlanes(t, planes[0], planes[1], planes[2], planes[3], wantY0, wantY1, wantU, wantV)
				changed, ok = rgb32ToI420Damage(planes[0], planes[1], planes[2], planes[3], src0, src1, mask, redIndex)
				if !ok || changed || !slices.Equal(mask, wantMask) {
					t.Fatalf("unchanged retry modified accumulated mask: changed %t, ok %t, mask %v; want %v",
						changed, ok, mask, wantMask)
				}
			}
		}
	}
}

func TestRGB32ToI420DamageIgnoresPadding(t *testing.T) {
	t.Parallel()
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 is unavailable")
	}
	const width = 64
	for _, redIndex := range []int{0, 2} {
		src0, src1 := make([]byte, width*4), make([]byte, width*4)
		for i := range src0 {
			src0[i], src1[i] = byte(i*13), byte(i*17+3)
		}
		y0, y1, u, v := referenceRGB32I420Rows(src0, src1, width, redIndex == 2)
		for i := 3; i < len(src0); i += 4 {
			src0[i] ^= 0xff
			src1[i] ^= 0xff
		}
		mask := []bool{true, true, true, true}
		changed, ok := rgb32ToI420Damage(y0, y1, u, v, src0, src1, mask, redIndex)
		if !ok || changed || slices.Contains(mask, false) {
			t.Fatalf("alpha/padding changed the output: changed %t, ok %t, mask %v", changed, ok, mask)
		}
	}
}

func TestRGB32ToI420DamageRejectsShape(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                                        string
		width, y1, u, v, src0, src1, mask, redIndex int
	}{
		{name: "empty"},
		{name: "narrow", width: 16, y1: 16, u: 8, v: 8, src0: 64, src1: 64, mask: 1},
		{name: "unaligned", width: 48, y1: 48, u: 24, v: 24, src0: 192, src1: 192, mask: 3},
		{name: "oversized", width: maxDimension + 32},
		{name: "luma", width: 32, y1: 31, u: 16, v: 16, src0: 128, src1: 128, mask: 2},
		{name: "u", width: 32, y1: 32, u: 15, v: 16, src0: 128, src1: 128, mask: 2},
		{name: "v", width: 32, y1: 32, u: 16, v: 15, src0: 128, src1: 128, mask: 2},
		{name: "source0", width: 32, y1: 32, u: 16, v: 16, src0: 127, src1: 128, mask: 2},
		{name: "source1", width: 32, y1: 32, u: 16, v: 16, src0: 128, src1: 127, mask: 2},
		{name: "mask", width: 32, y1: 32, u: 16, v: 16, src0: 128, src1: 128, mask: 1},
		{name: "layout", width: 32, y1: 32, u: 16, v: 16, src0: 128, src1: 128, mask: 2, redIndex: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			planes := [][]byte{
				bytes.Repeat([]byte{7}, test.width), bytes.Repeat([]byte{7}, test.y1),
				bytes.Repeat([]byte{7}, test.u), bytes.Repeat([]byte{7}, test.v),
			}
			mask := make([]bool, test.mask)
			for i := range mask {
				mask[i] = true
			}
			changed, ok := rgb32ToI420Damage(
				planes[0], planes[1], planes[2], planes[3],
				make([]byte, test.src0), make([]byte, test.src1), mask, test.redIndex,
			)
			if changed || ok || slices.Contains(mask, false) {
				t.Fatalf("rejected shape changed output: changed %t, ok %t, mask %v", changed, ok, mask)
			}
			for _, plane := range planes {
				if bytes.Count(plane, []byte{7}) != len(plane) {
					t.Fatal("rejected shape modified destination samples")
				}
			}
		})
	}
}

// A changed RGB byte need not change the converted picture: compare actual
// quantized YUV samples rather than conservatively marking raw source changes.
func TestRGB32ToI420DamageQuantizedUnchanged(t *testing.T) {
	t.Parallel()
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 is unavailable")
	}
	const width = 32
	for _, redIndex := range []int{0, 2} {
		src0, src1 := make([]byte, width*4), make([]byte, width*4)
		y0, y1, u, v := referenceRGB32I420Rows(src0, src1, width, redIndex == 2)
		src0[2-redIndex] = 1 // Both the luma and 2x2 channel average still round to zero.
		mask := []bool{true, true}
		changed, ok := rgb32ToI420Damage(y0, y1, u, v, src0, src1, mask, redIndex)
		if !ok || changed || slices.Contains(mask, false) {
			t.Fatalf("unchanged quantized samples cleared mask: changed %t, ok %t, mask %v", changed, ok, mask)
		}
	}
}
