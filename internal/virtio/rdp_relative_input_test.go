package virtio

import (
	"image"
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
)

type relativePointerRecord struct {
	buttons uint8
	dx, dy  int16
}

type relativePointerRecorder struct {
	resizePointerInput
	relative []relativePointerRecord
}

func (p *relativePointerRecorder) RelativePointerEvent(buttons uint8, dx, dy int16) {
	p.relative = append(p.relative, relativePointerRecord{buttons: buttons, dx: dx, dy: dy})
}

func TestRDPRelativePointerPreservesRawMovementAcrossResize(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	t.Cleanup(d.shutdown)
	input := &relativePointerRecorder{}
	d.SetInput(input)
	session := &rdp.Session{Width: 800, Height: 600}
	for _, size := range []image.Point{{X: 1024, Y: 768}, {X: 1920, Y: 1080}} {
		if err := d.Flush(size.X, size.Y, image.NewRGBA(image.Rectangle{Max: size})); err != nil {
			t.Fatal(err)
		}
		d.dispatchInput(session, []rdp.InputEvent{
			{Kind: rdp.InputRelativePointer, DX: -7, DY: 11, Buttons: 1},
			{Kind: rdp.InputRelativePointer, DX: -32768, DY: 32767},
			{Kind: rdp.InputRelativePointer},
		})
	}
	want := []relativePointerRecord{
		{buttons: 1, dx: -7, dy: 11},
		{dx: -32768, dy: 32767},
		{},
		{buttons: 1, dx: -7, dy: 11},
		{dx: -32768, dy: 32767},
		{},
	}
	if !slices.Equal(input.relative, want) {
		t.Fatalf("relative movement changed with desktop geometry: got %v, want %v", input.relative, want)
	}
	if input.resizePointerInput != (resizePointerInput{}) || len(d.motions) != 0 {
		t.Fatal("relative motion was also delivered as an absolute position or remembered as a cursor echo")
	}
	d.dispatchInput(session, []rdp.InputEvent{{Kind: rdp.InputPointer, X: 400, Y: 300}})
	if input.x != 960 || input.y != 540 {
		t.Fatalf("desktop absolute input stopped using framebuffer geometry: %+v", input.resizePointerInput)
	}
}

func TestRDPRelativePointerRequiresCapableSink(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		input DisplayInput
		want  bool
	}{
		{name: "missing"},
		{name: "absolute tablet", input: &resizePointerInput{}},
		{name: "relative mouse", input: &relativePointerRecorder{}, want: true},
		{name: "firmware without mouse", input: NewFirmwareInput(nil, nil)},
		{name: "firmware with mouse", input: NewFirmwareInput(&relativePointerRecorder{}, nil), want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := supportsRelativePointer(test.input); got != test.want {
				t.Fatalf("relative capability = %v, want %v", got, test.want)
			}
		})
	}
}
