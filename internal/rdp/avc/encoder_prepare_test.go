package avc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"slices"
	"testing"
)

func TestPrepareDamageOwnsCandidate(t *testing.T) {
	t.Parallel()
	for _, bgrx := range []bool{false, true} {
		name := "RGBA with nonzero origin"
		if bgrx {
			name = "BGRX with padded stride"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			codec := &macroblockCodec{}
			encoder := &Encoder{
				codec: codec, width: 64, height: 48, frameRate: FrameRate,
				i420: make([]byte, 64*48*3/2), measure: true,
			}
			bounds := image.Rect(-3, 5, 78, 62)
			if bgrx {
				bounds = image.Rect(0, 0, 81, 57)
			}
			parent := image.NewRGBA(bounds.Inset(-7))
			img := parent.SubImage(bounds).(*image.RGBA)
			for i := range parent.Pix {
				parent.Pix[i] = byte(i*17 + 31)
			}
			if _, err := encoder.Encode(img, false); err != nil {
				t.Fatal(err)
			}
			region := image.Rect(bounds.Min.X+1, bounds.Min.Y+1, bounds.Min.X+3, bounds.Min.Y+3)
			for y := region.Min.Y; y < region.Max.Y; y++ {
				for x := region.Min.X; x < region.Max.X; x++ {
					img.SetRGBA(x, y, color.RGBA{R: 23, G: 189, B: 71, A: 255})
				}
			}
			want := referenceI420(64, 48, img)
			regions := []image.Rectangle{region}
			var err error
			if bgrx {
				pix := packedBGRXForPreparation(img)
				err = encoder.PrepareBGRXDamage(pix, img.Stride, bounds.Dx(), bounds.Dy(), regions, false)
				clear(pix)
			} else {
				err = encoder.PrepareDamage(img, regions, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			if encoder.borrowedSource.Pix != nil {
				t.Fatal("preparation retained borrowed BGRX pixels")
			}
			wantHints := slices.Clone(encoder.unchanged)
			if codec.calls != 1 || encoder.LastStats().Conversion <= 0 {
				t.Fatalf("preparation invoked the codec or lost conversion timing: calls %d, stats %+v",
					codec.calls, encoder.LastStats())
			}
			// The producer may immediately overwrite both pixels and metadata.
			clear(img.Pix)
			*img = image.RGBA{}
			regions[0] = image.Rect(-1000, -1000, 1000, 1000)
			if _, err := encoder.EncodePrepared(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(codec.pixels, want) || !slices.Equal(codec.hints, wantHints) || len(wantHints) == 0 {
				t.Fatal("encoding read borrowed pixels or damage after preparation")
			}
			if encoder.LastStats().Encoding <= 0 {
				t.Fatal("encoding time was not recorded")
			}
		})
	}
}

func TestPrepareDamageLifecycle(t *testing.T) {
	t.Parallel()
	codec := &damageCodec{}
	encoder := &Encoder{codec: codec, width: 32, height: 32, frameRate: FrameRate, i420: make([]byte, 32*32*3/2)}
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	if _, err := encoder.EncodePrepared(); !errors.Is(err, ErrNoPreparedFrame) {
		t.Fatalf("unprepared encode error = %v", err)
	}
	if err := encoder.PrepareDamage(img, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := encoder.PrepareDamage(img, nil, true); !errors.Is(err, ErrPendingFrame) {
		t.Fatalf("overlapping preparation error = %v", err)
	}
	if _, err := encoder.EncodePrepared(); err != nil || codec.calls != 1 || codec.force {
		t.Fatalf("rejected preparation replaced pending frame: error %v, calls %d, force %v", err, codec.calls, codec.force)
	}
	if _, err := encoder.EncodePrepared(); !errors.Is(err, ErrNoPreparedFrame) {
		t.Fatalf("consumed preparation error = %v", err)
	}
	if err := encoder.PrepareDamage(img, []image.Rectangle{}, false); err != nil {
		t.Fatal(err)
	}
	if data, err := encoder.EncodePrepared(); err != nil || data != nil || codec.calls != 1 {
		t.Fatalf("unchanged preparation encoded: error %v, data %v, calls %d", err, data, codec.calls)
	}
	encoder.Close()
	if err := encoder.PrepareDamage(img, nil, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed prepare error = %v", err)
	}
	if _, err := encoder.EncodePrepared(); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed encode error = %v", err)
	}
}

func TestPrepareBGRXRetryRefreshAndGeometry(t *testing.T) {
	t.Parallel()
	codec := &macroblockCodec{}
	encoder := &Encoder{codec: codec, width: 64, height: 48, frameRate: FrameRate, i420: make([]byte, 64*48*3/2)}
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	prepare := func(regions []image.Rectangle, force bool) {
		t.Helper()
		err := encoder.PrepareBGRXDamage(
			packedBGRXForPreparation(img), img.Stride, img.Rect.Dx(), img.Rect.Dy(), regions, force,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	prepare(nil, false)
	if _, err := encoder.EncodePrepared(); err != nil {
		t.Fatal(err)
	}
	img.SetRGBA(1, 1, color.RGBA{R: 255, A: 255})
	prepare([]image.Rectangle{image.Rect(1, 1, 2, 2)}, false)
	codec.fail = true
	if _, err := encoder.EncodePrepared(); !errors.Is(err, ErrCodec) {
		t.Fatalf("injected failure = %v", err)
	}
	img.SetRGBA(50, 33, color.RGBA{G: 255, A: 255})
	prepare([]image.Rectangle{image.Rect(50, 33, 51, 34)}, false)
	if _, err := encoder.EncodePrepared(); err != nil || !codec.force || codec.hints != nil ||
		!bytes.Equal(codec.pixels, referenceI420(64, 48, img)) {
		t.Fatalf("retry lost retained changes or reused hints: %v", err)
	}
	img.SetRGBA(31, 23, color.RGBA{B: 255, A: 255})
	prepare([]image.Rectangle{}, true)
	if _, err := encoder.EncodePrepared(); err != nil || !codec.force || codec.hints != nil ||
		!bytes.Equal(codec.pixels, referenceI420(64, 48, img)) {
		t.Fatalf("forced preparation did not refresh all pixels: %v", err)
	}
	img = image.NewRGBA(image.Rect(0, 0, 47, 35))
	fillColorBars(img, 2)
	prepare([]image.Rectangle{}, false)
	if _, err := encoder.EncodePrepared(); err != nil || codec.hints != nil ||
		!bytes.Equal(codec.pixels, referenceI420(64, 48, img)) {
		t.Fatalf("geometry change did not refresh all pixels: %v", err)
	}
}

func TestPrepareBGRXRejectsMalformedGeometry(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                          string
		width, height, stride, length int
	}{
		{"zero width", 0, 32, 128, 4096},
		{"negative width", -32, 32, 128, 4096},
		{"negative height", 32, -32, 128, 4096},
		{"short stride", 32, 32, 127, 4096},
		{"negative stride", 32, 32, -128, 4096},
		{"short pixels", 32, 32, 128, 4095},
		{"overflowing width", int(^uint(0) >> 1), 32, 128, 4096},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoder := &Encoder{codec: &damageCodec{}, width: 32, height: 32, i420: make([]byte, 32*32*3/2)}
			err := encoder.PrepareBGRXDamage(
				make([]byte, test.length), test.stride, test.width, test.height, nil, false,
			)
			if !errors.Is(err, ErrGeometry) {
				t.Fatalf("geometry error = %v", err)
			}
			if encoder.prepared || encoder.initialized {
				t.Fatal("invalid geometry changed encoder state")
			}
		})
	}
}

func packedBGRXForPreparation(img *image.RGBA) []byte {
	pix := bytes.Clone(img.Pix)
	for y := range img.Rect.Dy() {
		for x := range img.Rect.Dx() {
			at := y*img.Stride + x*4
			pix[at], pix[at+2] = pix[at+2], pix[at]
		}
	}

	return pix
}
