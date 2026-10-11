package virtio

import (
	"image"
	"testing"
)

func BenchmarkGPUDesktop(b *testing.B) {
	const width, height = 1024, 768
	res := &gpuResource{width: width, height: height, format: gpuFormatB8G8R8X8, data: make([]byte, width*height*4)}
	b.Run("Convert", func(b *testing.B) {
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			resourceImageInto(res, false, img)
		}
	})
	b.Run("Convert64x64", func(b *testing.B) {
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		damage := image.Rect(400, 300, 464, 364)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			resourceImageRectInto(res, false, img, damage, image.Point{})
		}
	})
	b.Run("FlushCursor", func(b *testing.B) {
		d := gpuTestFramebuffer{newFramebuffer()}
		defer d.Close()
		g := &GPU{
			display: d,
			scanout: [gpuNumScanouts]uint32{1},
			cursor:  gpuCursor{image: image.NewRGBA(image.Rect(0, 0, 64, 64)), x: 400, y: 300},
		}
		g.flush(res)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			g.flush(res)
		}
	})
	b.Run("TransferFull", func(b *testing.B) {
		mem := make([]byte, len(res.data))
		g := NewGPU(11, nil, mem, nil)
		res.backing = nil
		for off := 0; off < len(mem); off += 4096 {
			res.backing = append(res.backing, gpuMemEntry{addr: uint64(off), length: 4096})
		}
		b.SetBytes(int64(len(mem)))
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			g.transferToHost2D(res, 0, 0, width, height, 0)
		}
	})
	b.Run("TransferBrowser", func(b *testing.B) {
		mem := make([]byte, len(res.data))
		g := NewGPU(11, nil, mem, nil)
		res.backing = nil
		for off := 0; off < len(mem); off += 4096 {
			res.backing = append(res.backing, gpuMemEntry{addr: uint64(off), length: 4096})
		}
		b.SetBytes(960 * 640 * 4)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			g.transferToHost2D(res, 32, 100, 960, 640, (100*width+32)*4)
		}
	})
}
