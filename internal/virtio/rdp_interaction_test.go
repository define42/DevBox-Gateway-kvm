package virtio

import (
	"image"
	"testing"
	"testing/synctest"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/avc"
)

func TestRDPInteractionRecognizesDrag(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		events []rdp.InputEvent
		active bool
	}{
		{
			name: "absolute held movement", active: true,
			events: []rdp.InputEvent{
				{Kind: rdp.InputPointer, X: 10, Buttons: 1},
				{Kind: rdp.InputPointer, X: 11, Buttons: 1},
			},
		},
		{
			name: "press at new position needs another move",
			events: []rdp.InputEvent{
				{Kind: rdp.InputPointer, X: 10},
				{Kind: rdp.InputPointer, X: 11, Buttons: 1},
			},
		},
		{
			name: "right button movement",
			events: []rdp.InputEvent{
				{Kind: rdp.InputPointer, X: 10, Buttons: 4},
				{Kind: rdp.InputPointer, X: 11, Buttons: 4},
			},
		},
		{
			name: "relative held movement", active: true,
			events: []rdp.InputEvent{{Kind: rdp.InputRelativePointer, DX: -1, Buttons: 1}},
		},
		{
			name:   "relative stationary",
			events: []rdp.InputEvent{{Kind: rdp.InputRelativePointer, Buttons: 1}},
		},
		{
			name: "mode change resets absolute baseline",
			events: []rdp.InputEvent{
				{Kind: rdp.InputPointer, X: 10, Buttons: 1},
				{Kind: rdp.InputRelativePointer, Buttons: 1},
				{Kind: rdp.InputPointer, X: 50, Buttons: 1},
			},
		},
		{
			name: "absolute resumes after mode change", active: true,
			events: []rdp.InputEvent{
				{Kind: rdp.InputRelativePointer, Buttons: 1},
				{Kind: rdp.InputPointer, X: 50, Buttons: 1},
				{Kind: rdp.InputPointer, Y: 1, X: 50, Buttons: 1},
			},
		},
		{
			name: "release in same packet",
			events: []rdp.InputEvent{
				{Kind: rdp.InputRelativePointer, DX: 1, Buttons: 1},
				{Kind: rdp.InputRelativePointer, DX: 1},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				interaction := newRDPInteraction()
				interaction.observe(test.events)
				if interaction.active() != test.active {
					t.Fatalf("active = %v, want %v", interaction.active(), test.active)
				}
				assertRDPInteractionWake(t, interaction, test.active)
			})
		})
	}
}

func TestRDPInteractionTimeoutAndRenewal(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		event rdp.InputEvent
		renew bool
	}{
		{name: "absolute stationary", event: rdp.InputEvent{Kind: rdp.InputPointer, X: 11, Buttons: 1}},
		{name: "relative stationary", event: rdp.InputEvent{Kind: rdp.InputRelativePointer, Buttons: 1}},
		{name: "keyboard", event: rdp.InputEvent{Kind: rdp.InputKey, Key: 'a', Down: true}},
		{name: "absolute wheel", event: rdp.InputEvent{Kind: rdp.InputPointer, X: 20, Buttons: 1 | 8}},
		{name: "relative wheel", event: rdp.InputEvent{Kind: rdp.InputRelativePointer, DX: 1, Buttons: 1 | 16}},
		{name: "continued drag", event: rdp.InputEvent{Kind: rdp.InputRelativePointer, DY: 1, Buttons: 1}, renew: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				interaction := newRDPInteraction()
				interaction.observe([]rdp.InputEvent{
					{Kind: rdp.InputPointer, X: 10, Buttons: 1},
					{Kind: rdp.InputPointer, X: 11, Buttons: 1},
				})
				assertRDPInteractionWake(t, interaction, true)
				time.Sleep(100 * time.Millisecond)
				interaction.observe([]rdp.InputEvent{test.event})
				assertRDPInteractionWake(t, interaction, false)
				time.Sleep(rdpDragTimeout - 100*time.Millisecond - time.Nanosecond)
				if !interaction.active() {
					t.Fatal("drag expired before its deadline")
				}
				time.Sleep(time.Nanosecond)
				if interaction.active() != test.renew {
					t.Fatalf("active at original deadline = %v, want %v", interaction.active(), test.renew)
				}
				if test.renew {
					time.Sleep(100 * time.Millisecond)
					if interaction.active() {
						t.Fatal("renewed drag did not expire")
					}
				}
				assertRDPInteractionWake(t, interaction, false)
			})
		})
	}
}

func TestRDPInteractionReleaseAndIsolation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		first, second := newRDPInteraction(), newRDPInteraction()
		var absent *rdpInteraction
		if absent.active() || absent.notification() != nil {
			t.Fatal("absent interaction must remain inactive without a notification channel")
		}
		drag := []rdp.InputEvent{{Kind: rdp.InputRelativePointer, DX: 1, Buttons: 1}}
		first.observe(drag)
		if second.active() {
			t.Fatal("drag changed another connection")
		}
		assertRDPInteractionWake(t, second, false)
		assertRDPInteractionWake(t, first, true)
		first.observe([]rdp.InputEvent{{Kind: rdp.InputRelativePointer}})
		if first.active() {
			t.Fatal("release did not clear drag pacing")
		}
		assertRDPInteractionWake(t, first, true)
		first.observe([]rdp.InputEvent{{Kind: rdp.InputRelativePointer}})
		assertRDPInteractionWake(t, first, false)

		// Input processing must not block when the writer has not drained a wake.
		first.observe(drag)
		first.observe([]rdp.InputEvent{{Kind: rdp.InputRelativePointer}})
		first.observe(drag)
		assertRDPInteractionWake(t, first, true)
		assertRDPInteractionWake(t, first, false)
	})
}

func assertRDPInteractionWake(t *testing.T, interaction *rdpInteraction, want bool) {
	t.Helper()
	var got bool
	select {
	case <-interaction.notification():
		got = true
	default:
	}
	if got != want {
		t.Fatalf("interaction wake = %v, want %v", got, want)
	}
}

func TestRDPDragPacingRechecksPendingDeadline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		display, writer, timer := newRDPInteractionWriter(t)
		initial := writer.lastFrame
		codec := writer.avc
		time.Sleep(5 * time.Millisecond)
		assertRDPPendingFrame(t, display, writer, timer, initial.Add(time.Second/30))

		writer.interaction.observe([]rdp.InputEvent{{Kind: rdp.InputRelativePointer, DX: 1, Buttons: 1}})
		assertRDPInteractionWake(t, writer.interaction, true)
		assertRDPPendingFrame(t, display, writer, timer, initial.Add(time.Second/60))
		time.Sleep(5 * time.Millisecond)
		writer.interaction.observe([]rdp.InputEvent{{Kind: rdp.InputRelativePointer}})
		assertRDPInteractionWake(t, writer.interaction, true)
		assertRDPPendingFrame(t, display, writer, timer, initial.Add(time.Second/30))
		<-timer.C
		if _, err := writer.writeWhenReady(display, timer); err != nil {
			t.Fatal(err)
		}
		if writer.lastFrame != initial.Add(time.Second/30) {
			t.Fatal("release did not defer the pending capture to the normal deadline")
		}

		writer.interaction.observe([]rdp.InputEvent{{Kind: rdp.InputRelativePointer, DX: 1, Buttons: 1}})
		time.Sleep(rdpDragTimeout - time.Millisecond)
		if _, err := writer.writeWhenReady(display, timer); err != nil {
			t.Fatal(err)
		}
		last := writer.lastFrame
		assertRDPPendingFrame(t, display, writer, timer, last.Add(time.Second/60))
		<-timer.C
		// Activity expires while this frame waits. The stale fast timer must not
		// capture until the recomputed normal deadline.
		assertRDPPendingFrame(t, display, writer, timer, last.Add(time.Second/30))
		<-timer.C
		if _, err := writer.writeWhenReady(display, timer); err != nil {
			t.Fatal(err)
		}
		if writer.lastFrame != last.Add(time.Second/30) {
			t.Fatal("expired drag retained its fast capture deadline")
		}
		if writer.avc != codec || codec.FrameRate() != avc.FrameRate {
			t.Fatal("pacing transition replaced the codec or changed its clock")
		}
	})
}

func assertRDPPendingFrame(
	t *testing.T, display *framebuffer, writer *rdpFrameWriter, timer *time.Timer, deadline time.Time,
) {
	t.Helper()
	before, sequence := writer.lastFrame, writer.frame.seq
	paced, err := writer.writeWhenReady(display, timer)
	if err != nil || paced != timer.C {
		t.Fatalf("pending frame was not paced: channel=%v error=%v", paced, err)
	}
	if writer.frameDeadline() != deadline || writer.lastFrame != before || writer.frame.seq != sequence {
		t.Fatalf("pending frame captured early or used wrong deadline: %v, want %v", writer.frameDeadline(), deadline)
	}
}

func TestRDPDragKeepsBitmapAt30FPS(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		interaction := newRDPInteraction()
		interaction.observe([]rdp.InputEvent{{Kind: rdp.InputRelativePointer, DX: 1, Buttons: 1}})
		writer := &rdpFrameWriter{interaction: interaction, lastFrame: time.Now()}
		if writer.frameInterval() != time.Second/30 || writer.frameDeadline() != time.Now().Add(time.Second/30) {
			t.Fatal("drag increased the bitmap capture rate")
		}
	})
}

func newRDPInteractionWriter(t *testing.T) (*framebuffer, *rdpFrameWriter, *time.Timer) {
	t.Helper()
	display := newFramebuffer()
	t.Cleanup(display.shutdown)
	if err := display.flush(16, 16, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	writer := &rdpFrameWriter{
		session: &rdp.Session{Width: 16, Height: 16}, interaction: newRDPInteraction(),
		graphics: true, force: true,
	}
	t.Cleanup(writer.close)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	t.Cleanup(func() { timer.Stop() })
	if _, err := writer.writeWhenReady(display, timer); err != nil {
		t.Fatal(err)
	}

	return display, writer, timer
}

func TestRDPDragPublicationToCaptureWait(t *testing.T) {
	t.Parallel()
	for _, fps := range []int{30, 60} {
		t.Run((time.Second / time.Duration(fps)).String(), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				testRDPDragPublicationWait(t, fps)
			})
		})
	}
}

// Drive the actual capture gate with 120-Hz publications and its own timer.
// Half-period publication offsets avoid ambiguous simultaneous events. Every
// publication is measured until the capture that includes it, including updates
// coalesced before the latest framebuffer version. No network/ACK time is modeled.
func testRDPDragPublicationWait(t *testing.T, fps int) {
	t.Helper()
	display, writer, timer := newRDPInteractionWriter(t)
	start := time.Now()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	publications := make([]time.Time, 0, 4)
	var paced <-chan time.Time
	var total, maximum time.Duration
	var captures, measured int
	for i := 0; i < 120 || len(publications) != 0; {
		nextPublication := start.Add(time.Second * time.Duration(2*i+1) / 240)
		if i < 120 && (paced == nil || nextPublication.Before(writer.frameDeadline())) {
			time.Sleep(time.Until(nextPublication))
			img.Pix[0]++
			if err := display.flush(16, 16, img); err != nil {
				t.Fatal(err)
			}
			publications = append(publications, time.Now())
			if fps == 60 {
				writer.interaction.observe([]rdp.InputEvent{{Kind: rdp.InputRelativePointer, DX: 1, Buttons: 1}})
			}
			i++
		} else {
			<-paced
		}
		before := writer.lastFrame
		var err error
		paced, err = writer.writeWhenReady(display, timer)
		if err != nil {
			t.Fatal(err)
		}
		if writer.lastFrame.Equal(before) {
			continue
		}
		captures++
		for _, published := range publications {
			wait := time.Since(published)
			total += wait
			maximum = max(maximum, wait)
			measured++
		}
		publications = publications[:0]
	}
	mean := total / time.Duration(measured)
	wantMean := time.Second / time.Duration(fps*2)
	if captures != fps || measured != 120 || mean < wantMean-time.Microsecond || mean > wantMean+time.Microsecond {
		t.Fatalf("%d FPS: captures=%d publications=%d mean=%v, want %v", fps, captures, measured, mean, wantMean)
	}
	if maximum >= time.Second/time.Duration(fps) {
		t.Fatalf("%d FPS: publication waited beyond one capture interval: %v", fps, maximum)
	}
	t.Logf("%d FPS: %d publications, mean publication-to-capture wait %v, maximum %v", fps, measured, mean, maximum)
}
