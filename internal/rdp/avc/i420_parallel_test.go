package avc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"runtime"
	"testing"
)

func TestI420ConversionWorkers(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name           string
		threads, procs int
		width, height  int
		want           int
	}{
		{"empty", 4, 4, 0, 0, 1},
		{"no-budget", 0, 4, 1024, 770, 1},
		{"one-thread", 1, 4, 1024, 770, 1},
		{"one-processor", 4, 1, 1024, 770, 1},
		{"below-two-workers", 4, 4, 1024, 254, 1},
		{"two-workers", 4, 4, 1024, 256, 2},
		{"three-workers", 4, 4, 1024, 384, 3},
		{"four-workers", 4, 4, 1024, 512, 4},
		{"effective-budget", 2, 4, 1024, 770, 2},
		{"processor-budget", 4, 3, 1024, 770, 3},
		{"maximum-workers", 16, 16, 1920, 1080, 4},
		{"single-row-pair", 4, 4, 1024, 2, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			region := image.Rect(2, 6, test.width+2, test.height+6)
			if got := i420ConversionWorkers(test.threads, test.procs, region); got != test.want {
				t.Fatalf("workers = %d, want %d", got, test.want)
			}
		})
	}
}

//nolint:paralleltest // Temporarily changes the process-wide GOMAXPROCS limit.
func TestI420ParallelConversionParity(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousProcs) })
	const width, height = 1024, 770 // 385 row pairs exercise unequal band sizes.
	for _, fixture := range []struct {
		name          string
		width, height int
	}{
		{"native", width, height},
		{"upscale", 643, 479},
		{"downscale", 1537, 1135},
		{"vertical-scale", width, 519},
		{"single-pixel", 1, 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			img, sourceStorage := newParallelI420TestImage(fixture.width, fixture.height)
			sourceBefore := bytes.Clone(sourceStorage)
			reference := referenceI420(width, height, img)
			alphaImage := *img
			alphaImage.Pix = bytes.Clone(img.Pix)
			for y := range img.Rect.Dy() {
				for x := range img.Rect.Dx() {
					alphaImage.Pix[y*img.Stride+x*4+3] ^= 0xff
				}
			}
			alphaBefore := bytes.Clone(alphaImage.Pix)
			for _, threads := range []int{2, 3, 4} {
				for _, region := range []struct {
					name string
					rect image.Rectangle
				}{
					{"full", image.Rect(0, 0, width, height)},
					{"partial", image.Rect(2, 8, 1020, 764)},
				} {
					t.Run(fmt.Sprintf("%dworkers/%s", threads, region.name), func(t *testing.T) {
						if got := i420ConversionWorkers(threads, runtime.GOMAXPROCS(0), region.rect); got != threads {
							t.Fatalf("fixture only permits %d workers, want %d", got, threads)
						}
						const prefix, suffix = 19, 37
						storage := bytes.Repeat([]byte{0xa5}, prefix+len(reference)+suffix)
						pixels := storage[prefix : prefix+len(reference) : prefix+len(reference)]
						want := bytes.Clone(pixels)
						copyI420TestRegion(want, reference, width, height, region.rect)
						wantChanged := !bytes.Equal(pixels, want)
						encoder := Encoder{
							width: width, height: height, i420: pixels,
							threads: threads,
						}
						if changed := encoder.convertI420Region(img, region.rect); changed != wantChanged ||
							!bytes.Equal(pixels, want) {
							t.Fatalf("conversion changed = %t, want %t; pixels equal = %t",
								changed, wantChanged, bytes.Equal(pixels, want))
						}
						if encoder.convertI420Region(img, region.rect) ||
							encoder.convertI420Region(&alphaImage, region.rect) || !bytes.Equal(pixels, want) {
							t.Fatal("repeated or alpha-only conversion changed the retained image")
						}
						if fixture.name == "native" {
							// Only the last band changes; false results from the other
							// workers must not overwrite its change notification.
							modified := *img
							modified.Pix = bytes.Clone(img.Pix)
							pixel := image.Rect(region.rect.Max.X-1, region.rect.Max.Y-1,
								region.rect.Max.X, region.rect.Max.Y).Add(img.Rect.Min)
							xorParallelI420TestRGB(&modified, pixel, 0xff)
							changedWant := bytes.Clone(want)
							lastBlock := image.Rect(region.rect.Max.X-2, region.rect.Max.Y-2,
								region.rect.Max.X, region.rect.Max.Y)
							scaleI420RegionScalar(changedWant, width, height, &modified, lastBlock)
							if !encoder.convertI420Region(&modified, region.rect) || !bytes.Equal(pixels, changedWant) {
								t.Fatal("change in the final row band was lost")
							}
						}
						if !bytes.Equal(storage[:prefix], bytes.Repeat([]byte{0xa5}, prefix)) ||
							!bytes.Equal(storage[prefix+len(reference):], bytes.Repeat([]byte{0xa5}, suffix)) {
							t.Fatal("conversion changed destination guard bytes")
						}
						if !bytes.Equal(sourceStorage, sourceBefore) || !bytes.Equal(alphaImage.Pix, alphaBefore) {
							t.Fatal("conversion changed source pixels, padding or guards")
						}
					})
				}
			}
		})
	}
}

//nolint:paralleltest // Temporarily changes the process-wide GOMAXPROCS limit.
func TestI420ParallelConversionSerialFallbacks(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousProcs) })
	const width, height = 1024, 770
	img, _ := newParallelI420TestImage(width, height)
	want := referenceI420(width, height, img)
	for _, test := range []struct {
		name           string
		threads, procs int
	}{
		{"one-thread", 1, 4},
		{"one-processor", 4, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime.GOMAXPROCS(test.procs)
			encoder := Encoder{
				width: width, height: height, i420: make([]byte, len(want)),
				threads: test.threads,
			}
			region := image.Rect(0, 0, width, height)
			if !encoder.convertI420Region(img, region) || !bytes.Equal(encoder.i420, want) ||
				encoder.convertI420Region(img, region) {
				t.Fatal("serial fallback output or change detection differs")
			}
		})
	}
}

//nolint:paralleltest // Temporarily changes the process-wide GOMAXPROCS limit.
func TestI420ParallelEncodeDamageState(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousProcs) })
	for _, threads := range []int{2, 3, 4} {
		t.Run(fmt.Sprintf("%dworkers", threads), func(t *testing.T) {
			const width, height = 1024, 770
			serialCodec, parallelCodec := &damageCodec{}, &damageCodec{}
			serial := Encoder{
				codec: serialCodec, frameRate: FrameRate, width: width, height: height,
				i420: make([]byte, width*height*3/2), threads: 1,
			}
			parallel := Encoder{
				codec: parallelCodec, frameRate: FrameRate, width: width, height: height,
				i420: make([]byte, width*height*3/2), threads: threads,
			}
			compare := func(label string, img *image.RGBA, regions []image.Rectangle, force bool, wantErr error) {
				t.Helper()
				want, serialErr := serial.EncodeDamage(img, regions, force)
				got, parallelErr := parallel.EncodeDamage(img, regions, force)
				if !errors.Is(serialErr, wantErr) || !errors.Is(parallelErr, wantErr) || !bytes.Equal(got, want) {
					t.Fatalf("%s: serial err=%v parallel err=%v want=%v", label, serialErr, parallelErr, wantErr)
				}
				if !bytes.Equal(serial.i420, parallel.i420) || !bytes.Equal(serialCodec.pixels, parallelCodec.pixels) ||
					serialCodec.calls != parallelCodec.calls || serialCodec.force != parallelCodec.force ||
					serial.frame != parallel.frame || serial.source != parallel.source ||
					serial.initialized != parallel.initialized || serial.retry != parallel.retry {
					t.Fatalf("%s: serial and parallel encoder state differs", label)
				}
			}
			img, _ := newParallelI420TestImage(width, height)
			empty := []image.Rectangle{}
			compare("initial empty damage", img, empty, false, nil)
			if parallelCodec.calls != 1 || !bytes.Equal(parallel.i420, referenceI420(width, height, img)) {
				t.Fatal("initial empty damage did not convert the full image")
			}
			xorParallelI420TestRGB(img, img.Rect, 0x5a)
			fullDamage := []image.Rectangle{img.Rect, img.Rect}
			compare("explicit full damage and duplicate", img, fullDamage, false, nil)
			calls := parallelCodec.calls
			compare("repeated explicit full damage", img, fullDamage, false, nil)
			if parallelCodec.calls != calls {
				t.Fatal("unchanged full damage called the codec")
			}
			large := img.Rect.Inset(1)
			second := img.Rect.Inset(3)
			xorParallelI420TestRGB(img, large, 0x33)
			compare("overlapping odd damage", img, []image.Rectangle{large, second, large}, false, nil)
			if !bytes.Equal(parallel.i420, referenceI420(width, height, img)) {
				t.Fatal("overlapping mapped damage lost changed pixels")
			}
			serialCodec.fail, parallelCodec.fail = true, true
			xorParallelI420TestRGB(img, large, 0x71)
			compare("codec failure", img, []image.Rectangle{large}, false, ErrCodec)
			corner := image.Rect(img.Rect.Min.X, img.Rect.Min.Y, img.Rect.Min.X+1, img.Rect.Min.Y+1)
			xorParallelI420TestRGB(img, corner, 0xa3)
			compare("retry with separate damage", img, []image.Rectangle{corner}, false, nil)
			if !parallelCodec.force || !bytes.Equal(parallelCodec.pixels, referenceI420(width, height, img)) {
				t.Fatal("retry lost failed damage or did not request an IDR")
			}
			calls = parallelCodec.calls
			xorParallelI420TestRGB(img, large, 0x17)
			compare("empty damage retains old pixels", img, empty, false, nil)
			if parallelCodec.calls != calls {
				t.Fatal("empty damage scanned undeclared changes")
			}
			compare("forced refresh", img, empty, true, nil)
			if !parallelCodec.force || !bytes.Equal(parallelCodec.pixels, referenceI420(width, height, img)) {
				t.Fatal("forced refresh did not rebuild the image")
			}
			resized, _ := newParallelI420TestImage(643, 479)
			compare("source resize with empty damage", resized, empty, false, nil)
			if !bytes.Equal(parallelCodec.pixels, referenceI420(width, height, resized)) {
				t.Fatal("source resize did not rebuild the image")
			}
			compare("nil damage", resized, nil, false, nil)
		})
	}
}

// Nonzero origins and padded rows are unrelated to Pix offsets. The slice ends
// at the final real pixel, and guards remain accessible only through storage.
func newParallelI420TestImage(width, height int) (*image.RGBA, []byte) {
	const prefix, suffix = 13, 29
	stride := width*4 + 17
	length := (height-1)*stride + width*4
	storage := bytes.Repeat([]byte{0x96}, prefix+length+suffix)
	pixels := storage[prefix : prefix+length : prefix+length]
	for index := range pixels {
		pixels[index] = byte(index*17 + index/31*13)
	}

	return &image.RGBA{
		Pix: pixels, Stride: stride, Rect: image.Rect(-7, 5, width-7, height+5),
	}, storage
}

func xorParallelI420TestRGB(img *image.RGBA, region image.Rectangle, value byte) {
	region = region.Intersect(img.Rect)
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			offset := (y-img.Rect.Min.Y)*img.Stride + (x-img.Rect.Min.X)*4
			img.Pix[offset] ^= value
			img.Pix[offset+1] ^= value >> 1
			img.Pix[offset+2] ^= value << 1
		}
	}
}
