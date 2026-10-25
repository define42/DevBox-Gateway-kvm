package rdp

import (
	"testing"
	"time"
)

func TestGraphicsAcknowledgmentPacingAt30FPS(t *testing.T) {
	t.Parallel()
	base := time.Second / 30
	g := &graphicsState{ready: true}
	session := &Session{graphics: g}
	if got := session.GraphicsFrameInterval(base); got != base {
		t.Fatalf("initial base pacing = %v", got)
	}
	now := time.Now()
	g.adaptAcknowledgments(0, graphicsPendingFrame{sentAt: now.Add(-400 * time.Millisecond)}, now)
	if got := session.GraphicsFrameInterval(base); got <= base {
		t.Fatalf("slow ACK did not slow base pacing: %v", got)
	}
	for range 100 {
		g.adaptAcknowledgments(0, graphicsPendingFrame{sentAt: now.Add(-time.Millisecond)}, now)
		if got := session.GraphicsFrameInterval(base); got < base {
			t.Fatalf("ACK recovery exceeded base frame rate: %v", got)
		}
	}
	g.resetAcknowledgments()
	if got := session.GraphicsFrameInterval(base); got != base {
		t.Fatalf("ACK reset lost base pacing: %v", got)
	}
}

// A requested floor is policy rather than congestion. It must not make a
// healthy connection wait for more ACKs before a faster interaction can start.
func TestGraphicsFrameIntervalDynamicBase(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		ackOff bool
	}{
		{name: "before first ACK"},
		{name: "ACK disabled", ackOff: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			session := &Session{graphics: &graphicsState{ready: true, ackOff: test.ackOff}}
			for _, base := range []time.Duration{time.Second / 30, time.Second / 60, time.Second / 30, time.Second / 60} {
				if got := session.GraphicsFrameInterval(base); got != base {
					t.Fatalf("requested floor %v: interval = %v", base, got)
				}
				if got := session.GraphicsStats().FrameInterval; got != base {
					t.Fatalf("requested floor %v: reported interval = %v", base, got)
				}
			}
		})
	}
}

func TestGraphicsFrameIntervalPreservesHiddenACKTurnaround(t *testing.T) {
	t.Parallel()
	g := &graphicsState{ready: true}
	session := &Session{graphics: g}
	base := time.Second / 30
	session.GraphicsFrameInterval(base)
	now := time.Unix(100, 0)
	g.adaptAcknowledgments(0, graphicsPendingFrame{sentAt: now.Add(-60 * time.Millisecond)}, now)
	if got := session.GraphicsStats().FrameInterval; got != base {
		t.Fatalf("30-ms ACK pacing should be hidden by the 30-FPS floor: %v", got)
	}
	for _, floor := range []time.Duration{time.Second / 60, base, time.Second / 60} {
		want := max(floor, 30*time.Millisecond)
		if got := session.GraphicsFrameInterval(floor); got != want {
			t.Fatalf("requested floor %v: interval = %v, want %v", floor, got, want)
		}
		if got := session.GraphicsStats(); got.FrameInterval != want || got.ACKTurnaround != 60*time.Millisecond {
			t.Fatalf("floor change lost ACK pacing or reported the wrong effective interval: %+v", got)
		}
	}
	g.resetAcknowledgments()
	if got := session.GraphicsStats(); got != (GraphicsStats{}) {
		t.Fatalf("reset retained pacing statistics: %+v", got)
	}
	if got := session.GraphicsFrameInterval(time.Second / 60); got != time.Second/60 {
		t.Fatalf("reset retained hidden ACK pressure: %v", got)
	}
}

func TestGraphicsFrameIntervalPreservesHiddenQueueRecovery(t *testing.T) {
	t.Parallel()
	fast, slow := time.Second/60, time.Second/30
	g := &graphicsState{ready: true, averageBytes: 1000}
	session := &Session{graphics: g}
	session.GraphicsFrameInterval(fast)
	now := time.Unix(100, 0)
	ack := graphicsPendingFrame{sentAt: now.Add(-2 * time.Millisecond)}
	g.adaptAcknowledgments(1000, ack, now)
	if got, want := session.GraphicsStats().FrameInterval, 2*fast; got != want {
		t.Fatalf("one buffered frame: interval = %v, want %v", got, want)
	}
	g.adaptAcknowledgments(0, ack, now)
	recovery := 2*fast - fast/4
	if got := session.GraphicsStats().FrameInterval; got != recovery {
		t.Fatalf("initial queue recovery = %v, want %v", got, recovery)
	}
	if got := session.GraphicsFrameInterval(slow); got != slow {
		t.Fatalf("30-FPS floor failed to hide queue recovery: %v", got)
	}
	if got := session.GraphicsFrameInterval(fast); got != recovery {
		t.Fatalf("floor toggle discarded queue recovery: %v, want %v", got, recovery)
	}
	// Recovery still progresses with actual ACKs while the slower floor hides
	// it, but another floor toggle alone must not erase the remaining delay.
	session.GraphicsFrameInterval(slow)
	g.adaptAcknowledgments(0, ack, now)
	recovery -= slow / 4
	if got := session.GraphicsStats().FrameInterval; got != slow {
		t.Fatalf("hidden recovery changed the effective floor: %v", got)
	}
	if got := session.GraphicsFrameInterval(fast); got != recovery {
		t.Fatalf("hidden recovery was lost: %v, want %v", got, recovery)
	}
	// A renewed queue sample applies immediate slowdown at the current base.
	g.adaptAcknowledgments(2000, ack, now)
	if got, want := session.GraphicsStats().FrameInterval, 3*fast; got != want {
		t.Fatalf("renewed queue pressure: interval = %v, want %v", got, want)
	}
}

func TestGraphicsAcknowledgmentsDefaultPacing(t *testing.T) {
	t.Parallel()
	g := &graphicsState{}
	session := &Session{graphics: g}
	now := time.Unix(100, 0)
	g.adaptAcknowledgments(0, graphicsPendingFrame{sentAt: now.Add(-time.Millisecond)}, now)
	if got := session.GraphicsStats().FrameInterval; got != graphicsMinFrameInterval {
		t.Fatalf("ACK before explicit floor used interval %v, want %v", got, graphicsMinFrameInterval)
	}
	if got := session.GraphicsFrameInterval(0); got != 0 {
		t.Fatalf("zero floor returned %v", got)
	}
	if got := session.GraphicsStats().FrameInterval; got != graphicsMinFrameInterval {
		t.Fatalf("zero floor changed pacing: %v", got)
	}
}
