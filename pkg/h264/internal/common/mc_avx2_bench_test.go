//go:build amd64

package common

import (
	"fmt"
	"testing"

	"golang.org/x/sys/cpu"
)

func BenchmarkMcAVX2Halfpel(b *testing.B) {
	if !cpu.X86.HasAVX2 {
		b.Skip("AVX2 unavailable")
	}
	const srcStride, dstStride = 48, 32
	src := make([]byte, srcStride*32)
	dst := make([]byte, dstStride*17)
	for i := range src {
		src[i] = byte(i*29 + 41)
	}
	for _, size := range []int32{8, 16, 17} {
		for _, kernel := range mcAVX2TestKernels {
			for _, impl := range []struct {
				name string
				fn   PWelsLumaHalfpelMcFunc
			}{
				{name: "SSE2", fn: kernel.sse2},
				{name: "AVX2", fn: kernel.avx2},
			} {
				b.Run(fmt.Sprintf("%s/%dx%d/%s", kernel.name, size, size, impl.name), func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(size * size))
					for b.Loop() {
						impl.fn(src, 3*srcStride+3, srcStride, dst, 0, dstStride, size, size)
					}
					mcSIMDBenchmarkSink = dst[0]
				})
			}
		}
	}
}

func BenchmarkMcAVX2RawHorizontal(b *testing.B) {
	if !cpu.X86.HasAVX2 {
		b.Skip("AVX2 unavailable")
	}
	const stride, height = 24, 17
	src := make([]int16, stride*height)
	dst := make([]byte, stride*height)
	for i := range src {
		src[i] = int16(i*41%13261 - 2550)
	}
	for _, width := range []int{8, 16, 17} {
		for _, impl := range []struct {
			name string
			fn   func([]byte, int, []int16, int, int, int)
		}{
			{name: "SSE2", fn: mcHorizontalRawToU8SSE2},
			{name: "AVX2", fn: mcHorizontalRawToU8AVX2},
		} {
			b.Run(fmt.Sprintf("width%d/%s", width, impl.name), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(width * height))
				for b.Loop() {
					impl.fn(dst, stride, src, stride, width, height)
				}
				mcSIMDBenchmarkSink = dst[0]
			})
		}
	}
}

func BenchmarkMcAVX2RawVertical(b *testing.B) {
	if !cpu.X86.HasAVX2 {
		b.Skip("AVX2 unavailable")
	}
	const srcStride, dstStride, height = 48, 24, 17
	src := make([]byte, srcStride*(height+5))
	dst := make([]int16, dstStride*height)
	for i := range src {
		src[i] = byte(i*29 + 41)
	}
	for _, width := range []int{13, 21, 22} {
		for _, impl := range []struct {
			name string
			fn   func([]int16, int, []byte, int, int, int)
		}{
			{name: "SSE2", fn: mcVerticalRawSSE2},
			{name: "AVX2", fn: mcVerticalRawAVX2},
		} {
			b.Run(fmt.Sprintf("width%d/%s", width, impl.name), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(width * height))
				for b.Loop() {
					impl.fn(dst, dstStride, src[2*srcStride:], srcStride, width, height)
				}
				mcFilterBenchmarkSink = int32(dst[0])
			})
		}
	}
}

func BenchmarkMcAVX2LumaQuarterpel(b *testing.B) {
	if !cpu.X86.HasAVX2 {
		b.Skip("AVX2 unavailable")
	}
	const stride = 48
	src := make([]byte, stride*24)
	dst := make([]byte, 16*16)
	for i := range src {
		src[i] = byte(i*29 + 41)
	}
	for _, phase := range [][2]int16{{1, 0}, {1, 1}, {1, 2}, {2, 1}, {3, 3}} {
		for _, impl := range []struct {
			name string
			fn   func([]byte, int, int32, []byte, int, int32, int16, int16, int32, int32)
		}{
			{name: "SSE2", fn: McLuma_sse2},
			{name: "AVX2", fn: McLuma_avx2},
		} {
			b.Run(fmt.Sprintf("phase%d%d/%s", phase[0], phase[1], impl.name), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(16 * 16)
				for b.Loop() {
					impl.fn(src, 3*stride+3, stride, dst, 0, 16, phase[0], phase[1], 16, 16)
				}
				mcSIMDBenchmarkSink = dst[0]
			})
		}
	}
}
