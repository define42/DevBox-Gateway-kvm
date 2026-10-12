package rdp

import (
	"testing"
	"time"
)

func TestOfficeProfileAcknowledgmentPacing(t *testing.T) {
	t.Parallel()
	fps, err := ProfileFrameRate(ProfileOffice)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Second / time.Duration(fps)
	g := &graphicsState{ready: true}
	session := &Session{graphics: g}
	if got := session.GraphicsFrameInterval(base); got != base {
		t.Fatalf("initial office pacing = %v", got)
	}
	now := time.Now()
	g.adaptAcknowledgments(0, graphicsPendingFrame{sentAt: now.Add(-400 * time.Millisecond)}, now)
	if got := session.GraphicsFrameInterval(base); got <= base {
		t.Fatalf("slow ACK did not slow office profile: %v", got)
	}
	for range 100 {
		g.adaptAcknowledgments(0, graphicsPendingFrame{sentAt: now.Add(-time.Millisecond)}, now)
		if got := session.GraphicsFrameInterval(base); got < base {
			t.Fatalf("ACK recovery exceeded office profile rate: %v", got)
		}
	}
	g.resetAcknowledgments()
	if got := session.GraphicsFrameInterval(base); got != base {
		t.Fatalf("ACK reset lost office profile: %v", got)
	}
}
