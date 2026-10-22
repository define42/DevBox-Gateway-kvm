//go:build amd64

package common

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/sys/cpu"
)

var mcAVX2TestKernels = []struct {
	name string
	c    PWelsLumaHalfpelMcFunc
	sse2 PWelsLumaHalfpelMcFunc
	avx2 PWelsLumaHalfpelMcFunc
}{
	{name: "horizontal", c: McHorVer20_c, sse2: McHorVer20_sse2, avx2: McHorVer20_avx2},
	{name: "vertical", c: McHorVer02_c, sse2: McHorVer02_sse2, avx2: McHorVer02_avx2},
	{name: "center", c: McHorVer22_c, sse2: McHorVer22_sse2, avx2: McHorVer22_avx2},
}

func TestMcAVX2Dispatch(t *testing.T) {
	var funcs SMcFunc
	InitMcFunc(&funcs, WELS_CPU_SSE2|WELS_CPU_AVX2)
	pointer := func(fn any) uintptr { return reflect.ValueOf(fn).Pointer() }
	wantHor, wantVer, wantCen := McHorVer20_sse2, McHorVer02_sse2, McHorVer22_sse2
	wantLuma := McLuma_sse2
	if cpu.X86.HasAVX2 {
		wantHor, wantVer, wantCen = McHorVer20_avx2, McHorVer02_avx2, McHorVer22_avx2
		wantLuma = McLuma_avx2
	}
	if pointer(funcs.PfLumaHalfpelHor) != pointer(wantHor) ||
		pointer(funcs.PfLumaHalfpelVer) != pointer(wantVer) ||
		pointer(funcs.PfLumaHalfpelCen) != pointer(wantCen) ||
		pointer(funcs.PMcLumaFunc) != pointer(wantLuma) {
		t.Fatal("half-pel dispatch does not match CPU flags and host capabilities")
	}
}

func TestMcAVX2HalfpelParity(t *testing.T) {
	// Run wrappers even without AVX2 to cover their host-feature fallback.
	rng := rand.New(rand.NewSource(0xA7264))
	for _, kernel := range mcAVX2TestKernels {
		t.Run(kernel.name, func(t *testing.T) {
			for width := 1; width <= 17; width++ {
				for _, height := range []int{1, 3, 16, 17} {
					stride := width + 11
					src := make([]byte, (height+5)*stride)
					for pattern := 0; pattern < 8; pattern++ {
						for i := range src {
							switch pattern {
							case 0:
								src[i] = 0
							case 1:
								src[i] = 255
							case 2:
								src[i] = byte((i & 1) * 255)
							case 3:
								src[i] = byte(((i / stride) & 1) * 255)
							default:
								src[i] = byte(rng.Intn(256))
							}
						}
						srcBefore := bytes.Clone(src)
						srcOff, dstOff, dstStride := 2*stride+2, 3, width+7
						want := bytes.Repeat([]byte{0xCD}, dstOff+dstStride*height+7)
						got := bytes.Clone(want)
						kernel.c(src, srcOff, int32(stride), want, dstOff, int32(dstStride),
							int32(width), int32(height))
						kernel.avx2(src, srcOff, int32(stride), got, dstOff, int32(dstStride),
							int32(width), int32(height))
						if !bytes.Equal(got, want) {
							t.Fatalf("%dx%d pattern %d: output or padding differs", width, height, pattern)
						}
						if !bytes.Equal(src, srcBefore) {
							t.Fatalf("%dx%d pattern %d: source changed", width, height, pattern)
						}
					}
				}
			}
		})
	}
}

func TestMcAVX2RawPassParity(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	rng := rand.New(rand.NewSource(0xA722))
	for width := 1; width <= 22; width++ {
		t.Run(fmt.Sprintf("width%d", width), func(t *testing.T) {
			const height = 3
			stride := width + 7
			src := make([]byte, stride*(height+5))
			for i := range src {
				src[i] = byte(rng.Intn(256))
			}
			wantRaw := make([]int16, stride*height)
			for i := range wantRaw {
				wantRaw[i] = -12345
			}
			gotRaw := slices.Clone(wantRaw)
			mcVerticalRawSSE2(wantRaw, stride, src[2*stride:], stride, width, height)
			mcVerticalRawAVX2(gotRaw, stride, src[2*stride:], stride, width, height)
			if !slices.Equal(gotRaw, wantRaw) {
				t.Fatal("signed vertical intermediates or padding differ")
			}

			// Exercise the full signed int16 domain, not just first-pass output.
			input := make([]int16, stride*height)
			for iteration := 0; iteration < 12; iteration++ {
				for i := range input {
					switch iteration {
					case 0:
						input[i] = -32768
					case 1:
						input[i] = 32767
					case 2:
						input[i] = int16((i&1)*65535 - 32768)
					default:
						input[i] = int16(rng.Intn(65536) - 32768)
					}
				}
				before := slices.Clone(input)
				want := bytes.Repeat([]byte{0xA5}, stride*height)
				got := bytes.Clone(want)
				for row := 0; row < height; row++ {
					for col := 0; col < width; col++ {
						value := (HorFilterInput16bit_c(input, row*stride+col) + 512) >> 10
						want[row*stride+col] = byte(min(max(value, 0), 255))
					}
				}
				mcHorizontalRawToU8AVX2(got, stride, input, stride, width, height)
				if !bytes.Equal(got, want) {
					t.Fatalf("signed horizontal pass differs for iteration %d", iteration)
				}
				if !slices.Equal(input, before) {
					t.Fatal("signed input changed")
				}
			}
		})
	}
}

func TestMcAVX2LumaPhases(t *testing.T) {
	rng := rand.New(rand.NewSource(0xA74))
	const srcStride, dstStride = 37, 23
	src := make([]byte, srcStride*23)
	for i := range src {
		src[i] = byte(rng.Intn(256))
	}
	for _, width := range []int32{4, 8, 16} {
		for _, height := range []int32{4, 8, 16} {
			for x := int16(-4); x < 4; x++ {
				for y := int16(-4); y < 4; y++ {
					want := bytes.Repeat([]byte{0xDB}, dstStride*16+8)
					got := bytes.Clone(want)
					McLuma_c(src, 3*srcStride+3, srcStride, want, 3, dstStride, x, y, width, height)
					McLuma_avx2(src, 3*srcStride+3, srcStride, got, 3, dstStride, x, y, width, height)
					if !bytes.Equal(got, want) {
						t.Fatalf("%dx%d phase (%d,%d) differs", width, height, x, y)
					}
				}
			}
		}
	}
}

func TestMcAVX2NegativeStrides(t *testing.T) {
	const width, height, stride = 17, 7, 29
	src := make([]byte, stride*(height+5))
	for i := range src {
		src[i] = byte(i*37 + 11)
	}
	for _, kernel := range mcAVX2TestKernels {
		t.Run(kernel.name, func(t *testing.T) {
			for _, reverseSource := range []bool{false, true} {
				for _, reverseDestination := range []bool{false, true} {
					srcOff, srcStride := 2*stride+2, int32(stride)
					dstOff, dstStride := 0, int32(stride)
					if reverseSource {
						srcOff, srcStride = (height+2)*stride+2, -stride
					}
					if reverseDestination {
						dstOff, dstStride = (height-1)*stride, -stride
					}
					want := bytes.Repeat([]byte{0xA3}, stride*height)
					got := bytes.Clone(want)
					kernel.c(src, srcOff, srcStride, want, dstOff, dstStride, width, height)
					kernel.avx2(src, srcOff, srcStride, got, dstOff, dstStride, width, height)
					if !bytes.Equal(got, want) {
						t.Fatalf("negative source=%v destination=%v differs", reverseSource, reverseDestination)
					}
				}
			}
		})
	}
}

func TestMcAVX2OverlappingBuffers(t *testing.T) {
	for _, kernel := range mcAVX2TestKernels[:2] {
		t.Run(kernel.name, func(t *testing.T) {
			for _, dstDelta := range []int{-1, 0, 1, 4, 16, 32} {
				want := make([]byte, 32*24)
				for i := range want {
					want[i] = byte(i*37 + 13)
				}
				got := bytes.Clone(want)
				const srcOff = 3*32 + 3
				kernel.sse2(want, srcOff, 32, want, srcOff+dstDelta, 32, 17, 17)
				kernel.avx2(got, srcOff, 32, got, srcOff+dstDelta, 32, 17, 17)
				if !bytes.Equal(got, want) {
					t.Fatalf("overlap offset %d differs from SSE2", dstDelta)
				}
			}
		})
	}
}

func TestMcAVX2BoundsAndEmpty(t *testing.T) {
	for _, kernel := range mcAVX2TestKernels {
		t.Run(kernel.name, func(t *testing.T) {
			kernel.avx2(nil, -1, -1, nil, -1, -1, 0, 17)
			kernel.avx2(nil, -1, -1, nil, -1, -1, 17, -1)
			for _, shortSource := range []bool{false, true} {
				t.Run(fmt.Sprintf("shortSource%v", shortSource), func(t *testing.T) {
					src, dst := make([]byte, 32*24), make([]byte, 17*17)
					if shortSource {
						src = src[:3*32+3+16]
					} else {
						dst = dst[:len(dst)-1]
					}
					defer func() {
						if recover() == nil {
							t.Fatal("short buffer did not panic before assembly")
						}
					}()
					kernel.avx2(src, 3*32+3, 32, dst, 0, 17, 17, 17)
				})
			}
		})
	}
}
