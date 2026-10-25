// Package avc provides pure Go H.264 encoding for the RDP graphics pipeline.
package avc

import (
	"errors"
	"image"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/damage"
)

// FrameRate is the default and maximum frame rate of the RDP AVC420 encoder.
const FrameRate = 60

// MaxThreads limits the requested H.264 slice count. The pure Go encoder
// schedules those slices on at most four workers.
const MaxThreads = 16

const maxDimension = 4096

var (
	ErrGeometry        = errors.New("invalid H.264 frame geometry")
	ErrCodec           = errors.New("H.264 encoding failed")
	ErrClosed          = errors.New("H.264 encoder is closed")
	ErrThreads         = errors.New("H.264 slice count must be between 1 and 16")
	ErrBackend         = errors.New("H.264 encoder must be software or auto")
	ErrFrameRate       = errors.New("H.264 frame rate must be between 1 and 60")
	ErrQP              = errors.New("H.264 quantizer must be between 0 and 51")
	ErrPendingFrame    = errors.New("H.264 frame is already prepared")
	ErrNoPreparedFrame = errors.New("no H.264 frame is prepared")
)

// Backend preserves compatibility with existing software and auto configurations.
type Backend string

const (
	BackendSoftware Backend = "software"
	// BackendAuto is a compatibility alias for BackendSoftware.
	BackendAuto Backend = "auto"
)

// Options controls the backend, software slices, frame rate, quantizer, and measurements.
type Options struct {
	Threads   int
	Measure   bool
	Backend   Backend // Empty, software, and auto all select the bundled pure Go encoder.
	FrameRate int     // Zero selects FrameRate; otherwise must be between 1 and FrameRate.
	QP        *int    // Nil selects DefaultQP; explicit values range from MinQP to MaxQP.
}

// EncodeStats describes the most recent frame preparation and encoding. Encoding
// includes damage hints, the codec and preparing its output as the returned Go slice.
type EncodeStats struct {
	Conversion time.Duration
	Encoding   time.Duration
}

type codecEncoder interface {
	encode([]byte, bool, int64) ([]byte, error)
	close()
}

// Encoder produces Annex-B AVC access units, including parameter sets with IDR
// frames. One goroutine must own the encoder, including calls to Close.
type Encoder struct {
	codec           codecEncoder
	width, height   int
	i420            []byte
	unchanged       []bool // Reused, output-space macroblock hints for software encoding.
	source          image.Rectangle
	initialized     bool
	retry           bool
	frame           int64
	frameRate       int
	qp              int
	threads         int
	measure         bool
	stats           EncodeStats
	prepared        bool
	preparedChanged bool
	preparedForce   bool
	preparedHints   bool
	borrowedSource  image.RGBA // Temporary BGRX header, cleared before preparation returns.
}

// NewEncoder fixes the output size for the lifetime of the encoder. Dimensions
// must be even and between 16 and 4096 pixels, inclusive.
func NewEncoder(width, height int) (*Encoder, error) {
	return NewEncoderWithOptions(width, height, Options{Threads: 1})
}

// NewEncoderWithOptions fixes the output size for the bundled pure Go encoder.
// Threads must be between 1 and MaxThreads. The encoder may reduce the requested
// count for small pictures; Threads reports the effective slice count.
// FrameRate selects the codec clock; zero preserves the 60-FPS default. Callers
// must pace Encode calls to the selected rate to bound encoding work. QP controls
// quantization: higher values reduce quality and usually produce fewer bytes.
// Nil QP preserves DefaultQP; the value is copied during construction.
func NewEncoderWithOptions(width, height int, options Options) (*Encoder, error) {
	if width < 16 || height < 16 || width > maxDimension || height > maxDimension || width%2 != 0 || height%2 != 0 {
		return nil, ErrGeometry
	}
	if options.Threads < 1 || options.Threads > MaxThreads {
		return nil, ErrThreads
	}
	frameRate := options.FrameRate
	if frameRate == 0 {
		frameRate = FrameRate
	}
	if frameRate < 1 || frameRate > FrameRate {
		return nil, ErrFrameRate
	}
	qp, err := ResolveQP(options.QP)
	if err != nil {
		return nil, err
	}
	if options.Backend != "" && options.Backend != BackendSoftware && options.Backend != BackendAuto {
		return nil, ErrBackend
	}
	codec, threads, err := newCodecEncoder(width, height, options.Threads, frameRate, qp)
	if err != nil {
		return nil, err
	}

	return &Encoder{
		codec: codec, width: width, height: height,
		i420: make([]byte, width*height*3/2), threads: threads,
		measure: options.Measure, frameRate: frameRate, qp: qp,
	}, nil
}

// Threads returns the effective software slice count.
func (e *Encoder) Threads() int { return e.threads }

// FrameRate reports the configured frame rate.
func (e *Encoder) FrameRate() int { return e.frameRate }

// QP reports the configured quantizer.
func (e *Encoder) QP() int { return e.qp }

// Backend always reports BackendSoftware, the bundled pure Go implementation.
func (e *Encoder) Backend() Backend { return BackendSoftware }

// LastStats returns measurements for the most recent encode or preparation call.
// After preparation, Encoding includes only damage-hint preparation; EncodePrepared
// adds codec execution. Measurements
// are zero when Options.Measure is false; that path does not read the clock.
func (e *Encoder) LastStats() EncodeStats { return e.stats }

// Encode scales img to the session dimensions and encodes it. Force requests an
// independent IDR frame, for example when a client asks for a complete refresh.
// The returned slice is owned by the caller and survives the next Encode call.
// Identical I420 frames return nil unless force is true.
func (e *Encoder) Encode(img *image.RGBA, force bool) ([]byte, error) {
	return e.EncodeDamage(img, nil, force)
}

// EncodeDamage updates only changed source regions in the retained I420 image.
// Nil damage means the entire image; an empty, nonnil slice means no new changes.
// First frames, source geometry changes, and forced refreshes convert everything.
// Callers must include all changes since the previous call, including frames
// skipped by pacing. A codec failure retains the candidate image and requests an
// IDR on retry, so subsequent partial updates cannot lose the failed changes.
// If an encoded frame is not delivered, callers must force the next frame to
// resynchronize the decoder.
// Software encoding can reuse reconstructed blocks outside these regions when
// the reference and motion predictor permit it; the AVC picture size is unchanged.
func (e *Encoder) EncodeDamage(img *image.RGBA, regions []image.Rectangle, force bool) ([]byte, error) {
	if err := e.PrepareDamage(img, regions, force); err != nil {
		return nil, err
	}

	return e.EncodePrepared()
}

// EncodePrepared encodes and consumes the frame prepared by PrepareDamage or
// PrepareBGRXDamage. It does not access the source pixels or damage rectangles.
// An unchanged frame returns nil unless a refresh or retry is required. A frame
// is consumed even if the codec fails; prepare again to retry the retained image.
func (e *Encoder) EncodePrepared() ([]byte, error) {
	if e.codec == nil {
		return nil, ErrClosed
	}
	if !e.prepared {
		return nil, ErrNoPreparedFrame
	}
	e.prepared = false
	if !e.preparedChanged {
		return nil, nil
	}
	var started time.Time
	if e.measure {
		started = time.Now()
	}
	data, err := e.encodeWithDamageHints()
	if e.measure {
		e.stats.Encoding += time.Since(started)
	}
	e.frame++
	e.retry = err != nil

	return data, err
}

// Close releases all codec buffers and is safe to call more than once.
func (e *Encoder) Close() {
	if e.codec != nil {
		e.codec.close()
		e.codec = nil
		e.i420 = nil
		e.unchanged = nil
		e.prepared = false
	}
}

func validRGBA(img *image.RGBA) bool { return damage.ValidRGBA(img) }

// MS-RDPEGFX 3.3.8.3.1 requires full-range BT.709, rather than the studio-range
// BT.601 used by many general-purpose I420 conversion routines. Average each
// 2x2 source block for the subsampled chroma planes.
func scaleI420(dst []byte, width, height int, img *image.RGBA) {
	scaleI420Region(dst, width, height, img, image.Rect(0, 0, width, height))
}

// region is clipped to the output and aligned to complete 2x2 chroma blocks.
// The persistent image is compared while converting, avoiding a second full
// frame buffer and a separate full-image comparison or copy.
func scaleI420Region(dst []byte, width, height int, img *image.RGBA, region image.Rectangle) bool {
	if region.Empty() {
		return false
	}
	if img.Rect.Dx() == width && img.Rect.Dy() == height {
		if changed, ok := scaleI420RegionNative(dst, width, height, img, region); ok {
			return changed
		}
	} else if changed, ok := scaleI420RegionScaled(dst, width, height, img, region); ok {
		return changed
	}

	return scaleI420RegionScalar(dst, width, height, img, region)
}

// scaleI420RegionNative converts an unscaled region two rows at a time. The
// architecture-specific row kernel returns ok=false when it cannot handle the
// input, allowing the caller to preserve the portable scaling path.
func scaleI420RegionNative(dst []byte, width, height int, img *image.RGBA, region image.Rectangle) (bool, bool) {
	luma := width * height
	chroma := luma / 4
	changed := false
	for y := region.Min.Y; y < region.Max.Y; y += 2 {
		chromaStart := y/2*(width/2) + region.Min.X/2
		chromaEnd := chromaStart + region.Dx()/2
		x0, x1 := region.Min.X, region.Max.X
		src0 := img.Pix[y*img.Stride+x0*4 : y*img.Stride+x1*4]
		src1 := img.Pix[(y+1)*img.Stride+x0*4 : (y+1)*img.Stride+x1*4]
		rowChanged, ok := rgbaToI420AVX2(
			dst[y*width+x0:y*width+x1],
			dst[(y+1)*width+x0:(y+1)*width+x1],
			dst[luma+chromaStart:luma+chromaEnd],
			dst[luma+chroma+chromaStart:luma+chroma+chromaEnd],
			src0, src1,
		)
		if !ok {
			return false, false
		}
		changed = rowChanged || changed
	}

	return changed, true
}

func scaleI420RegionScalar(dst []byte, width, height int, img *image.RGBA, region image.Rectangle) bool {
	luma := width * height
	chroma := luma / 4
	// Mapping columns once avoids repeated integer divisions for every pixel.
	// NewEncoder bounds width, so this fixed-size scratch array stays on the
	// stack without allocating a lookup table for every frame.
	var columns [maxDimension]int
	for x := region.Min.X; x < region.Max.X; x++ {
		columns[x] = x * img.Rect.Dx() / width * 4
	}
	changed := false
	for y := region.Min.Y; y < region.Max.Y; y += 2 {
		pos := y/2*(width/2) + region.Min.X/2
		row0 := img.Pix[(y*img.Rect.Dy()/height)*img.Stride:]
		row1 := img.Pix[((y+1)*img.Rect.Dy()/height)*img.Stride:]
		luma0 := dst[y*width : (y+1)*width]
		luma1 := dst[(y+1)*width : (y+2)*width]
		for x := region.Min.X; x < region.Max.X; x += 2 {
			p00 := row0[columns[x]:][:3]
			p01 := row0[columns[x+1]:][:3]
			p10 := row1[columns[x]:][:3]
			p11 := row1[columns[x+1]:][:3]
			r00, g00, b00 := int(p00[0]), int(p00[1]), int(p00[2])
			r01, g01, b01 := int(p01[0]), int(p01[1]), int(p01[2])
			r10, g10, b10 := int(p10[0]), int(p10[1]), int(p10[2])
			r11, g11, b11 := int(p11[0]), int(p11[1]), int(p11[2])
			y00 := byte((54*r00 + 183*g00 + 18*b00) >> 8)
			y01 := byte((54*r01 + 183*g01 + 18*b01) >> 8)
			y10 := byte((54*r10 + 183*g10 + 18*b10) >> 8)
			y11 := byte((54*r11 + 183*g11 + 18*b11) >> 8)
			red := (r00 + r01 + r10 + r11) / 4
			green := (g00 + g01 + g10 + g11) / 4
			blue := (b00 + b01 + b10 + b11) / 4
			u := byte(((-29*red - 99*green + 128*blue) >> 8) + 128)
			v := byte(((128*red - 116*green - 12*blue) >> 8) + 128)
			changed = changed || luma0[x] != y00 || luma0[x+1] != y01 ||
				luma1[x] != y10 || luma1[x+1] != y11 || dst[luma+pos] != u || dst[luma+chroma+pos] != v
			luma0[x], luma0[x+1], luma1[x], luma1[x+1] = y00, y01, y10, y11
			dst[luma+pos], dst[luma+chroma+pos] = u, v
			pos++
		}
	}

	return changed
}
