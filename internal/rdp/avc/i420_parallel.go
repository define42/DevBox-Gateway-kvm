package avc

import (
	"image"
	"runtime"
	"sync"
)

// Keep enough pixels in each band to amortize scheduling and stack setup.
const i420MinPixelsPerWorker = 128 * 1024

// The software codec currently uses at most four workers. Conversion shares
// its effective budget and finishes before any slice encoding starts.
const i420MaxWorkers = 4

func i420ConversionWorkers(threads, procs int, region image.Rectangle) int {
	return max(1, min(threads, procs, i420MaxWorkers, region.Dy()/2,
		region.Dx()*region.Dy()/i420MinPixelsPerWorker))
}

// convertI420Region preserves each 2x2 chroma block within one worker's band.
// EncodeDamage calls this synchronously for each mapped rectangle, so damage
// rectangles that overlap after alignment never write the same pixels at once.
func (e *Encoder) convertI420Region(img *image.RGBA, region image.Rectangle) bool {
	if e.backend != BackendSoftware || e.threads <= 1 ||
		region.Dx()*region.Dy() < 2*i420MinPixelsPerWorker {
		return scaleI420Region(e.i420, e.width, e.height, img, region)
	}
	workers := i420ConversionWorkers(e.threads, runtime.GOMAXPROCS(0), region)
	if workers == 1 {
		return scaleI420Region(e.i420, e.width, e.height, img, region)
	}

	var changed [i420MaxWorkers]bool
	var pending sync.WaitGroup
	pairs := region.Dy() / 2
	for worker := 1; worker < workers; worker++ {
		band := region
		band.Min.Y += 2 * (pairs * worker / workers)
		band.Max.Y = region.Min.Y + 2*(pairs*(worker+1)/workers)
		pending.Go(func() {
			changed[worker] = scaleI420Region(e.i420, e.width, e.height, img, band)
		})
	}
	band := region
	band.Max.Y = region.Min.Y + 2*(pairs/workers)
	changed[0] = scaleI420Region(e.i420, e.width, e.height, img, band)
	pending.Wait()

	for _, value := range changed[:workers] {
		if value {
			return true
		}
	}

	return false
}
