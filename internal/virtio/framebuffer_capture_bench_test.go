package virtio

import (
	"fmt"
	"image"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/avc"
)

// copiedCaptureDisplay exercises the original GPU RGBA publication contract.
// It deliberately exposes only the capabilities of that path.
type copiedCaptureDisplay struct{ display *RDPDisplay }

func (d copiedCaptureDisplay) Flush(width, height int, img *image.RGBA) error {
	return d.display.Flush(width, height, img)
}

func (d copiedCaptureDisplay) FlushDamage(width, height int, img *image.RGBA, damage []image.Rectangle) error {
	return d.display.FlushDamage(width, height, img, damage)
}

func (d copiedCaptureDisplay) SetCursor(cursor DisplayCursor) error {
	return d.display.SetCursor(cursor)
}

func (d copiedCaptureDisplay) Close() error { return nil }

// BenchmarkFramebufferCapture compares GPU publication through viewer I420
// preparation. Repeated identical pixels with explicit damage exercise every
// publication/capture/conversion pass while skipping codec work after warmup.
// Guest transfer, codec execution, socket writes and ACK waits are excluded.
func BenchmarkFramebufferCapture(b *testing.B) {
	for _, workload := range []struct {
		name   string
		damage image.Rectangle
	}{
		{name: "Full1080p", damage: image.Rect(0, 0, 1920, 1080)},
		{name: "Small64x32", damage: image.Rect(100, 100, 164, 132)},
	} {
		for _, viewers := range []int{1, 3} {
			for _, direct := range []bool{false, true} {
				mode := "Copied"
				if direct {
					mode = "Direct"
				}
				b.Run(fmt.Sprintf("%s/%dViewers/%s", workload.name, viewers, mode), func(b *testing.B) {
					d := &RDPDisplay{framebuffer: newFramebuffer()}
					b.Cleanup(d.shutdown)
					var sink Display = copiedCaptureDisplay{display: d}
					if direct {
						sink = d
					}
					g := NewGPU(11, nil, nil, sink)
					res := &gpuResource{width: 1920, height: 1080, format: gpuFormatB8G8R8X8, data: make([]byte, 1920*1080*4)}
					for i := 0; i < len(res.data); i += 4 {
						res.data[i], res.data[i+1], res.data[i+2] = byte(i/4), byte(i/1920), byte(i/7680)
					}
					writers := make([]rdpFrameWriter, viewers)
					for i := range writers {
						var err error
						writers[i].avc, err = avc.NewEncoder(1920, 1080)
						if err != nil {
							b.Fatal(err)
						}
						b.Cleanup(writers[i].close)
					}
					capture := func() {
						for i := range writers {
							w := &writers[i]
							if direct {
								if _, err := w.captureAVC(d.framebuffer, false); err != nil {
									b.Fatal(err)
								}
								if _, err := w.avc.EncodePrepared(); err != nil {
									b.Fatal(err)
								}
							} else {
								damage := d.copyFrameChanges(&w.frame, false)
								img := &image.RGBA{Pix: w.frame.pix, Stride: 1920 * 4, Rect: image.Rect(0, 0, 1920, 1080)}
								if _, err := w.avc.EncodeDamage(img, damage, false); err != nil {
									b.Fatal(err)
								}
							}
						}
					}
					g.flush(res)
					capture()
					packedBytes := len(d.frame)
					if g.frame != nil {
						packedBytes += len(g.frame.Pix)
					}
					for i := range writers {
						packedBytes += len(writers[i].frame.pix)
					}
					b.ReportAllocs()
					for b.Loop() {
						if err := g.flushDamage(res, workload.damage); err != nil {
							b.Fatal(err)
						}
						capture()
					}
					b.ReportMetric(float64(packedBytes), "packed-B")
				})
			}
		}
	}
}
