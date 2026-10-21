//go:build amd64

package processing

import (
	"fmt"
	"math/rand"
	"testing"
)

var vaaBenchmarkSink int32

func BenchmarkVAACalcSad(b *testing.B) {
	sizes := []struct {
		width, height int32
	}{
		{width: 320, height: 192},
		{width: 1920, height: 1088},
	}
	implementations := []struct {
		name string
		call VAACalcSadFunc
	}{
		{name: "Scalar", call: VAACalcSad_c},
		{name: "SSE2", call: VAACalcSad_sse2},
	}

	for _, size := range sizes {
		stride := (size.width + 95) &^ 31
		offset := int(32*stride + 32)
		bufferSize := offset + int((size.height+32)*stride) + 32
		cur := make([]uint8, bufferSize)
		ref := make([]uint8, bufferSize)
		rng := rand.New(rand.NewSource(int64(size.width)<<32 | int64(size.height)))
		rng.Read(cur)
		rng.Read(ref)
		mbCount := int(size.width>>4) * int(size.height>>4)

		for _, implementation := range implementations {
			b.Run(fmt.Sprintf("%dx%d/%s", size.width, size.height, implementation.name), func(b *testing.B) {
				sad := make([][4]int32, mbCount)
				var frameSad int32
				b.SetBytes(int64(size.width * size.height))
				b.ReportAllocs()
				for b.Loop() {
					implementation.call(cur, offset, ref, offset, size.width, size.height, stride, &frameSad, sad)
				}
				vaaBenchmarkSink = frameSad + sad[len(sad)-1][3]
			})
		}
	}
}

// BenchmarkVAACalcSadMasked compares the same source pair with and without a
// valid damage hint: a 64x32 change in an otherwise identical 1080p source.
func BenchmarkVAACalcSadMasked(b *testing.B) {
	const width, height, stride = 1920, 1088, 1984
	const offset = stride*32 + 32
	cur, ref := make([]byte, offset+stride*height), make([]byte, offset+stride*height)
	rng := rand.New(rand.NewSource(42))
	rng.Read(cur)
	copy(ref, cur)
	mask := make([]bool, width/16*height/16)
	for i := range mask {
		mask[i] = true
	}
	for y := 32; y < 34; y++ {
		for x := 40; x < 44; x++ {
			mask[y*(width/16)+x] = false
			for row := range 16 {
				for col := range 16 {
					cur[offset+(y*16+row)*stride+x*16+col] ^= 0x5a
				}
			}
		}
	}
	for _, test := range []struct {
		name string
		mask []bool
	}{
		{name: "Full"}, {name: "Sparse64x32", mask: mask},
	} {
		b.Run(test.name, func(b *testing.B) {
			sad := make([][4]int32, len(mask))
			var frameSad int32
			b.ReportAllocs()
			for b.Loop() {
				vaaCalcSadMasked(VAACalcSad_sse2, cur, offset, ref, offset, width, height, stride, &frameSad, sad, test.mask)
			}
			vaaBenchmarkSink = frameSad + sad[len(sad)-1][3]
		})
	}
}
