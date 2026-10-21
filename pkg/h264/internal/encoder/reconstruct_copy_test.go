package encoder

import (
	"bytes"
	"fmt"
	"testing"
)

func TestReconstructedBlockCopy(t *testing.T) {
	t.Parallel()
	for _, block := range []struct {
		size int
		copy func([]byte, int, int32, []byte, int, int32)
	}{{16, copyReconstructed16x16}, {8, copyReconstructed8x8}} {
		t.Run(fmt.Sprintf("%dx%d", block.size, block.size), func(t *testing.T) {
			t.Parallel()
			for _, geometry := range []struct {
				name                 string
				dstOffset, srcOffset int
				dstStride, srcStride int32
			}{
				{"tight", 0, 0, int32(block.size), int32(block.size)},
				{"unaligned-padded", 7, 3, 111, 97},
				{"picture-strides", 13, 31, 1952, 1984},
				{"zero-stride", 1, 2, 0, 0},
			} {
				t.Run(geometry.name, func(t *testing.T) {
					t.Parallel()
					srcLength := geometry.srcOffset + (block.size-1)*int(geometry.srcStride) + block.size
					dstLength := geometry.dstOffset + (block.size-1)*int(geometry.dstStride) + block.size
					src := make([]byte, srcLength)
					for i := range src {
						src[i] = byte(i*37 + i/19)
					}
					originalSource := bytes.Clone(src)
					dst := bytes.Repeat([]byte{0xa5}, dstLength+17)
					want := bytes.Clone(dst)
					for row := range block.size {
						d := geometry.dstOffset + row*int(geometry.dstStride)
						s := geometry.srcOffset + row*int(geometry.srcStride)
						copy(want[d:d+block.size], src[s:s+block.size])
					}
					// Hide the trailing guard from the wrapper too: only the exact
					// source and destination spans may be accessed.
					block.copy(dst[:dstLength], geometry.dstOffset, geometry.dstStride,
						src, geometry.srcOffset, geometry.srcStride)
					if !bytes.Equal(dst, want) {
						t.Fatal("copy changed pixels or padding outside the reconstructed block")
					}
					if !bytes.Equal(src, originalSource) {
						t.Fatal("copy modified the reconstructed reference")
					}
				})
			}
			t.Run("overlap", func(t *testing.T) {
				t.Parallel()
				for _, offset := range []int{0, 1, 5} {
					got := make([]byte, 2048)
					for i := range got {
						got[i] = byte(i*17 + 23)
					}
					want := bytes.Clone(got)
					for row := range block.size {
						d, s := 31+offset+row*64, 31+row*64
						copy(want[d:d+block.size], want[s:s+block.size])
					}
					block.copy(got, 31+offset, 64, got, 31, 64)
					if !bytes.Equal(got, want) {
						t.Fatalf("overlap at offset %d differs from row-wise copy", offset)
					}
				}
			})
		})
	}
}

func TestReconstructedBlockCopyRejectsInvalidSpan(t *testing.T) {
	t.Parallel()
	for _, block := range []struct {
		name string
		size int
		copy func([]byte, int, int32, []byte, int, int32)
	}{{"16x16", 16, copyReconstructed16x16}, {"8x8", 8, copyReconstructed8x8}} {
		t.Run(block.name, func(t *testing.T) {
			t.Parallel()
			for _, invalid := range []struct {
				name   string
				offset int
				stride int32
				short  bool
			}{
				{name: "negative-offset", offset: -1, stride: int32(block.size)},
				{name: "past-end", offset: 4096, stride: int32(block.size)},
				{name: "overflowing-offset", offset: int(^uint(0) >> 1), stride: int32(block.size)},
				{name: "negative-stride", stride: -1},
				{name: "huge-stride", stride: 1<<31 - 1},
				{name: "short-length-with-capacity", stride: int32(block.size), short: true},
			} {
				for _, source := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/source=%t", invalid.name, source), func(t *testing.T) {
						t.Parallel()
						dst := bytes.Repeat([]byte{0xa5}, block.size*block.size)
						src := bytes.Repeat([]byte{0x12}, block.size*block.size)
						before := bytes.Clone(dst)
						defer func() {
							if recover() == nil {
								t.Fatal("copy accepted an invalid buffer span")
							}
							if !bytes.Equal(dst, before) {
								t.Fatal("copy wrote before validating both buffer spans")
							}
						}()
						if source {
							if invalid.short {
								src = src[:len(src)-1]
							}
							block.copy(dst, 0, int32(block.size), src, invalid.offset, invalid.stride)
						} else {
							if invalid.short {
								dst = dst[:len(dst)-1]
								before = before[:len(before)-1]
							}
							block.copy(dst, invalid.offset, invalid.stride, src, 0, int32(block.size))
						}
					})
				}
			}
		})
	}
}
