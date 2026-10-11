package virtio

import (
	"bytes"
	"testing"
)

func TestGPUTransferFragmentedBacking(t *testing.T) {
	t.Parallel()
	const width, height = 11, 7
	logical := make([]byte, width*height*gpuBytesPerPixel)
	for i := range logical {
		logical[i] = byte(i*37 + 11)
	}
	for _, tc := range []struct {
		name       string
		x, y, w, h uint32
		offset     uint64
	}{
		{"full", 0, 0, width, height, 0},
		{"partial", 3, 2, 5, 4, 100},
		{"independent_offset", 2, 1, 7, 5, 3},
		{"same_entry_rows", 1, 1, 2, 5, 160},
		{"short_backing", 1, 1, 4, 3, 290},
		{"past_backing", 1, 1, 4, 3, uint64(len(logical))},
		{"empty", 1, 1, 0, 5, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Entries include empty pages, sub-pixel splits, several row
			// boundaries inside one page, and gaps in physical guest memory.
			lengths := []int{0, 1, 5, 0, 13, 47, 9, 0, 96, 137}
			mem := make([]byte, len(logical)+len(lengths)*16)
			var entries []gpuMemEntry
			var logicalPos, physicalPos int
			for _, length := range lengths {
				physicalPos += 16
				copy(mem[physicalPos:physicalPos+length], logical[logicalPos:logicalPos+length])
				entries = append(entries, gpuMemEntry{addr: uint64(physicalPos), length: uint32(length)})
				logicalPos += length
				physicalPos += length
			}
			res := &gpuResource{
				width: width, height: height, backing: entries,
				data: bytes.Repeat([]byte{0xa5}, len(logical)),
			}
			want := bytes.Clone(res.data)
			for row := uint32(0); row < tc.h; row++ {
				src := int(tc.offset) + int(row)*width*gpuBytesPerPixel
				dst := int((tc.y+row)*width+tc.x) * gpuBytesPerPixel
				if src < len(logical) {
					copy(want[dst:dst+int(tc.w)*gpuBytesPerPixel], logical[src:])
				}
			}
			g := NewGPU(11, nil, mem, nil)
			g.transferToHost2D(res, tc.x, tc.y, tc.w, tc.h, tc.offset)
			if !bytes.Equal(res.data, want) {
				t.Fatalf("fragmented transfer = %x, want %x", res.data, want)
			}
		})
	}
}

func TestGPUBackingReadInvalidAndMissingBytes(t *testing.T) {
	t.Parallel()
	mem := []byte{10, 11, 12, 13, 14, 15}
	g := NewGPU(11, nil, mem, nil)
	res := &gpuResource{backing: []gpuMemEntry{
		{addr: 0, length: 2},
		{addr: 100, length: 3}, // Invalid guest range leaves destination unchanged.
		{addr: 2, length: 0},
		{addr: 3, length: 3},
	}}
	dst := bytes.Repeat([]byte{0xa5}, 10)
	g.backingRead(res, 1, dst)
	want := []byte{11, 0xa5, 0xa5, 0xa5, 13, 14, 15, 0xa5, 0xa5, 0xa5}
	if !bytes.Equal(dst, want) {
		t.Fatalf("backing read = %x, want %x", dst, want)
	}

	// Each transfer must start with the current backing, without retaining
	// an entry cursor from a previous read or backing attachment.
	res.backing = []gpuMemEntry{{addr: 0, length: uint32(len(mem))}}
	g.backingRead(res, 0, dst)
	copy(want, mem)
	if !bytes.Equal(dst, want) {
		t.Fatalf("replacement backing read = %x, want %x", dst, want)
	}
}
