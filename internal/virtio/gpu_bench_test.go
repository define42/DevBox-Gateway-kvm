package virtio

import (
	"image"
	"testing"
)

func BenchmarkGPUResourceConversion(b *testing.B) {
	for _, format := range []struct {
		name string
		code uint32
	}{
		{name: "BGRX", code: gpuFormatB8G8R8X8},
		{name: "BGRA", code: gpuFormatB8G8R8A8},
	} {
		for _, size := range []struct {
			name   string
			width  int
			height int
			damage image.Rectangle
		}{
			{name: "720p", width: 1280, height: 720, damage: image.Rect(0, 0, 1280, 720)},
			{name: "1080p", width: 1920, height: 1080, damage: image.Rect(0, 0, 1920, 1080)},
			{name: "4K", width: 3840, height: 2160, damage: image.Rect(0, 0, 3840, 2160)},
			{name: "dirty64x32", width: 1920, height: 1080, damage: image.Rect(401, 303, 465, 335)},
			{name: "tail13x7", width: 63, height: 17, damage: image.Rect(3, 5, 16, 12)},
		} {
			b.Run(format.name+"/"+size.name, func(b *testing.B) {
				res := &gpuResource{
					width: uint32(size.width), height: uint32(size.height), format: format.code,
					data: make([]byte, size.width*size.height*gpuBytesPerPixel),
				}
				for i := range res.data {
					res.data[i] = byte(i*37 + 11)
				}
				img := image.NewRGBA(image.Rect(0, 0, size.width, size.height))
				b.SetBytes(int64(size.damage.Dx() * size.damage.Dy() * gpuBytesPerPixel))
				b.ReportAllocs()
				for b.Loop() {
					resourceImageRectInto(res, false, img, size.damage, image.Point{})
				}
			})
		}
	}
}
