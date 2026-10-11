package virtio

import (
	"fmt"
	"image"
	"log"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
)

type rdpStageTime struct {
	total time.Duration
	max   time.Duration
	count uint64
}

func (s *rdpStageTime) add(elapsed time.Duration) {
	if elapsed <= 0 {
		return
	}
	s.total += elapsed
	s.max = max(s.max, elapsed)
	s.count++
}

func (s rdpStageTime) String() string {
	var average time.Duration
	if s.count != 0 {
		average = s.total / time.Duration(s.count)
	}

	return fmt.Sprintf("%.2f/%.2fms", float64(average)/float64(time.Millisecond), float64(s.max)/float64(time.Millisecond))
}

type rdpWaitTime struct {
	start time.Time
	total time.Duration
}

func (w *rdpWaitTime) update(waiting bool, now time.Time) {
	if waiting && w.start.After(now) {
		return // Pacing still prevents sending; backpressure has not delayed a frame yet.
	}
	if !w.start.IsZero() {
		w.total += max(0, now.Sub(w.start))
		w.start = time.Time{}
	}
	if waiting {
		w.start = now
	}
}

// Stats belong to one frame writer. Disabled stats do not read the clock or
// allocate. Report only during activity and at disconnect; no idle timer.
type rdpFrameStats struct {
	enabled       bool
	peer          string
	encoder       string
	source        image.Point
	output        image.Point
	start         time.Time
	frames        uint64
	bytes         uint64
	copy          rdpStageTime
	convert       rdpStageTime
	encode        rdpStageTime
	write         rdpStageTime
	ack           rdpWaitTime
	pacing        rdpStageTime
	ackTurnaround time.Duration
}

func (s *rdpFrameStats) begin() time.Time {
	if !s.enabled {
		return time.Time{}
	}

	return time.Now()
}

func (s *rdpFrameStats) elapsed(start time.Time) time.Duration {
	if !s.enabled {
		return 0
	}

	return time.Since(start)
}

func (s *rdpFrameStats) waiting(ack bool, nextFrame time.Time) {
	if s.enabled {
		now := time.Now()
		s.ack.update(ack, now)
		if ack && s.ack.start.Before(nextFrame) {
			s.ack.start = nextFrame
		}
	}
}

// captured samples the active encoder and pacing once per framebuffer capture.
// Pacing describes the permitted send interval, not elapsed encode or wait time.
func (s *rdpFrameStats) captured(encoder string, source, output image.Point, timing rdp.GraphicsStats) {
	if !s.enabled {
		return
	}
	s.encoder, s.source, s.output = encoder, source, output
	s.pacing.add(timing.FrameInterval)
	s.ackTurnaround = timing.ACKTurnaround
}

func (s *rdpFrameStats) sent(bytes int) {
	if s.enabled && bytes != 0 {
		s.frames++
		s.bytes += uint64(bytes)
	}
}

func (s *rdpFrameStats) report(slices int, final bool) {
	if !s.enabled {
		return
	}
	now := time.Now()
	if s.start.IsZero() {
		s.start = now
	}
	if !final && now.Sub(s.start) < 5*time.Second {
		return
	}
	s.ack.update(!s.ack.start.IsZero(), now)
	if s.frames != 0 || s.ack.total != 0 {
		elapsed := now.Sub(s.start).Seconds()
		var fps float64
		if elapsed > 0 {
			fps = float64(s.frames) / elapsed
		}
		log.Printf(
			"rdp: stats client=%s encoder=%s source=%dx%d output=%dx%d slices=%d "+
				"frames=%d payload_bytes=%d elapsed=%.2fs fps=%.2f "+
				"avg/max copy=%s convert=%s encode=%s write=%s pacing=%s ack_turnaround=%s ack_wait=%s",
			s.peer,
			s.encoder,
			s.source.X,
			s.source.Y,
			s.output.X,
			s.output.Y,
			slices,
			s.frames,
			s.bytes,
			elapsed,
			fps,
			s.copy,
			s.convert,
			s.encode,
			s.write,
			s.pacing,
			s.ackTurnaround.Round(time.Microsecond),
			s.ack.total.Round(time.Millisecond),
		)
	}
	*s = rdpFrameStats{
		enabled: true, peer: s.peer, start: now,
		encoder: s.encoder, source: s.source, output: s.output,
		ack: rdpWaitTime{start: s.ack.start}, ackTurnaround: s.ackTurnaround,
	}
}
