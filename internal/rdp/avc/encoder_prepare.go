package avc

import (
	"image"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/damage"
)

// PrepareDamage converts the indicated RGBA regions into the encoder's retained
// I420 image. It consumes img and regions synchronously without retaining either,
// allowing their owner to release its lock before calling EncodePrepared.
// Damage, refresh and retry semantics match EncodeDamage. Every successful
// preparation must be consumed by EncodePrepared before preparing another frame.
func (e *Encoder) PrepareDamage(img *image.RGBA, regions []image.Rectangle, force bool) error {
	return e.prepareFrame(img, regions, force, false)
}

// PrepareBGRXDamage is PrepareDamage for packed B, G, R, ignored-byte pixels.
// Pixels and damage use a zero-origin source rectangle of width by height;
// stride may include row padding. Conversion preserves full-range BT.709 and
// averages complete 2x2 output blocks for chroma. Neither pix nor regions is
// retained, and no intermediate RGBA pixel buffer is created.
func (e *Encoder) PrepareBGRXDamage(
	pix []byte, stride, width, height int, regions []image.Rectangle, force bool,
) error {
	// Reuse the header: the RGBA parallel path makes prepareFrame's image
	// parameter escape, even though BGRX conversion only borrows its fields.
	e.borrowedSource = image.RGBA{Pix: pix, Stride: stride, Rect: image.Rectangle{Max: image.Pt(width, height)}}
	defer func() { e.borrowedSource = image.RGBA{} }()

	return e.prepareFrame(&e.borrowedSource, regions, force, true)
}

func (e *Encoder) prepareFrame(img *image.RGBA, regions []image.Rectangle, force, bgrx bool) error {
	if e.codec == nil {
		e.stats = EncodeStats{}

		return ErrClosed
	}
	if e.prepared {
		return ErrPendingFrame
	}
	e.stats = EncodeStats{}
	if !validRGBA(img) {
		return ErrGeometry
	}
	var started time.Time
	if e.measure {
		started = time.Now()
	}
	changed := !e.initialized || e.retry
	output := image.Rect(0, 0, e.width, e.height)
	fullConversion := force || regions == nil || !e.initialized || e.source != img.Rect
	e.surface.begin()
	e.mapped = e.mapped[:0]
	if fullConversion {
		changed = e.convertPreparedRegion(img, output, bgrx) || changed
	} else {
		changed = e.convertDamage(img, regions, bgrx) || changed
	}
	e.source, e.initialized = img.Rect, true
	if e.measure {
		e.stats.Conversion = time.Since(started)
		started = time.Now()
	}
	e.prepared, e.preparedChanged, e.preparedForce = true, force || changed, force || e.retry
	_, supportsDamage := e.codec.(damageCodecEncoder)
	e.preparedHints = supportsDamage && !fullConversion && !e.preparedForce && e.preparedChanged
	e.preparedForced, e.preparedShift = nil, nil
	if e.preparedHints {
		e.unchangedMacroblocks(img.Rect, regions)
		if e.surface.enabled {
			e.preparedForced, e.preparedShift = e.surface.plan(e.unchanged, e.width, e.height, e.mapped)
		}
	} else if e.surface.enabled && e.preparedChanged {
		// Without hints every block is coded from its source.
		e.surface.repaintAll()
	}
	if e.measure && e.preparedChanged {
		e.stats.Encoding = time.Since(started)
	}

	return nil
}

// convertDamage converts the output regions that regions map to, recording
// them in e.mapped, and reports whether any samples changed.
func (e *Encoder) convertDamage(img *image.RGBA, regions []image.Rectangle, bgrx bool) bool {
	output := image.Rect(0, 0, e.width, e.height)
	for _, region := range regions {
		e.mapped = append(e.mapped, damage.Align(damage.Map(region, img.Rect, output), 2, output))
	}
	changed := false
	if !e.surface.enabled || img.Rect != output {
		for _, mapped := range e.mapped {
			changed = e.convertPreparedRegion(img, mapped, bgrx) || changed
		}

		return changed
	}
	// Scrolls are found by comparing each unscaled area with the samples it
	// replaces, snapshotted before any of it is converted.
	e.surface.areas = detectionAreas(e.surface.areas, e.mapped)
	for _, area := range e.surface.areas {
		shift := 0
		if scrollCandidate(area) {
			shift = e.surface.detector.vote(img, bgrx, area, e.i420, e.width)
		}
		if shift != 0 {
			e.surface.detector.snapshot.take(e.i420, e.width, e.height, area)
		}
		for _, mapped := range e.mapped {
			if mapped.In(area) {
				changed = e.convertPreparedRegion(img, mapped, bgrx) || changed
			}
		}
		if shift != 0 {
			e.surface.matches = append(e.surface.matches, e.surface.detector.detect(e.i420, e.width, e.height, shift))
		}
	}

	return changed
}

func (e *Encoder) convertPreparedRegion(img *image.RGBA, region image.Rectangle, bgrx bool) bool {
	if bgrx {
		return e.convertBGRXI420Region(img.Pix, img.Stride, img.Rect.Dx(), img.Rect.Dy(), region)
	}

	return e.convertI420Region(img, region)
}
