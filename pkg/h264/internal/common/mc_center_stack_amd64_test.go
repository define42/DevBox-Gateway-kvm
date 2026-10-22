//go:build amd64

package common

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"golang.org/x/sys/cpu"
)

// The direct 36-tap oracle never calls either separable implementation. No
// rounding or clipping is applied until both filters have been accumulated.
func centerStackReference(src []byte, off, stride, x, y int) byte {
	taps := [...]int64{1, -5, 20, 20, -5, 1}
	var sum int64
	for ky, cy := range taps {
		for kx, cx := range taps {
			sum += cy * cx * int64(src[off+(y+ky-2)*stride+x+kx-2])
		}
	}
	return byte(min(max((sum+512)>>10, 0), 255))
}

func TestMcCenterStackIndependentParity(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	rng := rand.New(rand.NewSource(0x264c57))
	for _, width := range []int{4, 5, 8, 9, 16, 17} {
		for height := 1; height <= 17; height++ {
			for _, stride := range []int{0, 1, width + 5, 2048} {
				t.Run(fmt.Sprintf("%dx%d/stride%d", width, height, stride), func(t *testing.T) {
					off, length := mcRefinementSourceLayout("center", width, height, stride)
					backing := bytes.Repeat([]byte{0xab}, length+8)
					src := backing[3 : 3+length : 3+length]
					for pattern := range 7 {
						for i := range src {
							switch pattern {
							case 0:
								src[i] = 0
							case 1:
								src[i] = 255
							case 2:
								src[i] = byte((i & 1) * 255)
							case 3:
								src[i] = byte(((i / max(1, stride)) & 1) * 255)
							default:
								src[i] = byte(rng.Intn(256))
							}
						}
						before := bytes.Clone(backing)
						for _, dstStride := range []int{0, 1, width - 1, width, width + 3} {
							n := 3 + (height-1)*dstStride + width
							want := bytes.Repeat([]byte{0xda}, n+8)
							for y := range height {
								for x := range width {
									want[3+y*dstStride+x] = centerStackReference(src, off, stride, x, y)
								}
							}
							for _, raw := range []bool{false, true} {
								got := bytes.Repeat([]byte{0xda}, n+8)
								if raw {
									mcCenterRefineAVX2(&got[3], dstStride, &src[off-2], stride, width, height)
								} else {
									McHorVer22_avx2(src, off, int32(stride), got[:n], 3, int32(dstStride), int32(width), int32(height))
								}
								if !bytes.Equal(got, want) {
									t.Fatalf("pattern=%d dstStride=%d raw=%v differs", pattern, dstStride, raw)
								}
								if !bytes.Equal(backing, before) {
									t.Fatal("source or canaries changed")
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestMcCenterStackAliasedStoreOrder(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, width := range []int{4, 5, 8, 9, 16, 17} {
		for _, height := range []int{1, 2, 5, 9, 17} {
			for _, stride := range []int{0, 1, width + 5, 48} {
				off, n := mcRefinementSourceLayout("center", width, height, stride)
				for _, dstStride := range []int{0, 1, width - 1, width, 32} {
					for _, delta := range []int{-2, 0, 1, 7, 16} {
						t.Run(fmt.Sprintf("%dx%d/src%d/dst%d/delta%d", width, height, stride, dstStride, delta), func(t *testing.T) {
							dstOff := off + delta
							size := max(n, dstOff+(height-1)*dstStride+width) + 11
							original := make([]byte, size)
							for i := range original {
								original[i] = byte(i*73 + 19)
							}
							want := bytes.Clone(original)
							mcCenterStackOriginal(want[:n], off, int32(stride), want, dstOff, int32(dstStride), int32(width), int32(height))
							for _, raw := range []bool{false, true} {
								got := bytes.Clone(original)
								if raw {
									mcCenterRefineAVX2(&got[dstOff], dstStride, &got[off-2], stride, width, height)
								} else {
									McHorVer22_avx2(got[:n], off, int32(stride), got, dstOff, int32(dstStride), int32(width), int32(height))
								}
								if !bytes.Equal(got, want) {
									t.Fatalf("raw=%v changed alias/store order", raw)
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestMcCenterStackFallbackAndPartialPanics(t *testing.T) {
	// Include unsupported widths, nonpositive dimensions, negative strides,
	// short visible lengths despite extra capacity, and invalid offsets.
	cases := []struct{ width, height, srcStride, dstStride, srcOff, dstOff, srcLen, dstLen int }{
		{5, 5, 32, 32, 66, 0, 900, 900}, {9, 9, 32, 32, 66, 0, 900, 900}, {17, 17, 32, 32, 66, 0, 900, 900},
		{1, 5, 32, 32, 66, 0, 900, 900}, {4, 5, 32, 32, 66, 0, 900, 900}, {8, 9, 32, 32, 66, 0, 900, 900},
		{16, 17, 32, 32, 66, 0, 900, 900}, {7, 5, 32, 32, 66, 0, 900, 900}, {5, 18, 32, 32, 66, 0, 900, 900},
		{0, 5, 32, 32, -1, -1, 0, 0}, {5, 0, 32, 32, -1, -1, 0, 0}, {-1, -1, 32, 32, -1, -1, 0, 0},
		{5, 5, -32, 32, 226, 0, 900, 900}, {5, 5, 32, -32, 66, 128, 900, 900},
		{9, 9, 32, 32, 66, 0, 398, 900}, {9, 9, 32, 32, 66, 0, 900, 264},
		{17, 17, 32, 32, 66, 0, 694, 900}, {17, 17, 32, 32, 66, 0, 900, 528},
		{9, 9, 32, 32, -1, 0, 900, 900}, {9, 9, 32, 32, 66, -1, 900, 900},
		{9, 9, 32, 32, 66, 0, 0, 0}, {9, 9, -32, -32, 0, 0, 0, 0},
	}
	for i, c := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			run := func(fn func([]byte, int, int32, []byte, int, int32, int32, int32)) (out []byte, panicValue any) {
				src := make([]byte, 1024)
				out = bytes.Repeat([]byte{0xdc}, 1024)
				for j := range src {
					src[j] = byte(j*83 + 7)
				}
				defer func() { panicValue = recover() }()
				fn(src[:c.srcLen], c.srcOff, int32(c.srcStride), out[:c.dstLen], c.dstOff, int32(c.dstStride), int32(c.width), int32(c.height))
				return
			}
			want, pw := run(mcCenterStackOriginal)
			got, pg := run(McHorVer22_avx2)
			if fmt.Sprint(pg) != fmt.Sprint(pw) || !bytes.Equal(got, want) {
				t.Fatalf("panic or partial output differs: got %v want %v", pg, pw)
			}
		})
	}
}

// Small fresh goroutine stacks force the wrapper to check/grow before the
// NOSPLIT leaf. Repeated height/width changes also expose dirty scratch reads.
func TestMcCenterStackFreshGoroutineStacks(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for i := range 24 {
		done := make(chan bool, 1)
		go func() {
			const stride = 32
			src := make([]byte, stride*24)
			dst := make([]byte, stride*17)
			for j := range src {
				src[j] = byte(j*29 + i*71)
			}
			for _, width := range []int{17, 4, 5, 8, 9, 16, 17, 4, 5} {
				for _, height := range []int{17, 1, 9, 2} {
					McHorVer22_avx2(src, 2*stride+2, stride, dst, 0, stride, int32(width), int32(height))
					for y := range height {
						for x := range width {
							if dst[y*stride+x] != centerStackReference(src, 2*stride+2, stride, x, y) {
								done <- false
								return
							}
						}
					}
				}
			}
			done <- true
		}()
		if !<-done {
			t.Fatal("fresh-stack parity differs")
		}
	}
}
