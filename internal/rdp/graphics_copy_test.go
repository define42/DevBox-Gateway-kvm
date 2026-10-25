package rdp

import (
	"bytes"
	"encoding/binary"
	"image"
	"slices"
	"testing"
)

// decodeSurfaceCopiesFixture removes the RDPGFX_SURFACE_TO_SURFACE_PDUs that
// follow START_FRAME, returning them and the frame without them.
func decodeSurfaceCopiesFixture(t *testing.T, frame []byte) ([]SurfaceCopy, []byte) {
	t.Helper()
	var copies []SurfaceCopy
	rest := frame[16:]
	for len(rest) >= 8 && binary.LittleEndian.Uint16(rest) == 0x05 {
		if binary.LittleEndian.Uint32(rest[4:]) != graphicsCopyLength || len(rest) < graphicsCopyLength ||
			binary.LittleEndian.Uint16(rest[8:]) != 0 || binary.LittleEndian.Uint16(rest[10:]) != 0 ||
			binary.LittleEndian.Uint16(rest[20:]) != 1 {
			t.Fatal("invalid RDPGFX_SURFACE_TO_SURFACE_PDU")
		}
		value := func(offset int) int { return int(binary.LittleEndian.Uint16(rest[offset:])) }
		copies = append(copies, SurfaceCopy{
			Source:      image.Rect(value(12), value(14), value(16), value(18)),
			Destination: image.Pt(value(22), value(24)),
		})
		rest = rest[graphicsCopyLength:]
	}

	return copies, append(slices.Clone(frame[:16]), rest...)
}

func TestGraphicsAVC420UpdateCopiesAndExactRegions(t *testing.T) {
	t.Parallel()
	conn := &graphicsTestConn{}
	session := &Session{
		Width: 640, Height: 480, conn: conn,
		graphics: &graphicsState{ready: true, ackOff: true}, dynamic: graphicsDynamicFixture(3),
	}
	predicted := []byte{0, 0, 1, 0x41, 0x55}
	// Adjacent but disjoint regions stay exactly as given: no halo or merging.
	regions := []image.Rectangle{image.Rect(32, 400, 608, 416), image.Rect(32, 416, 48, 432)}
	copies := []SurfaceCopy{{Source: image.Rect(32, 40, 608, 400), Destination: image.Pt(32, 32)}}
	if sent, err := session.WriteAVC420Update(predicted, regions, copies); err != nil || !sent {
		t.Fatalf("send update: %t, %v", sent, err)
	}
	messages := graphicsOutputFixture(t, conn.outgoing.Bytes())
	gotCopies, frame := decodeSurfaceCopiesFixture(t, decodeZGFXFixture(t, messages[0][2:]))
	gotRegions, payload := decodeAVC420DamageFixture(t, frame, 640, 480, 1)
	if !slices.Equal(gotCopies, copies) || !slices.Equal(gotRegions, regions) || !bytes.Equal(payload, predicted) {
		t.Fatalf("update copies=%v regions=%v payload=%x", gotCopies, gotRegions, payload)
	}

	// Nil regions repaint everything after the copies.
	conn.outgoing.Reset()
	if sent, err := session.WriteAVC420Update(predicted, nil, copies); err != nil || !sent {
		t.Fatalf("send full update: %t, %v", sent, err)
	}
	messages = graphicsOutputFixture(t, conn.outgoing.Bytes())
	gotCopies, frame = decodeSurfaceCopiesFixture(t, decodeZGFXFixture(t, messages[0][2:]))
	gotRegions, _ = decodeAVC420DamageFixture(t, frame, 640, 480, 2)
	if !slices.Equal(gotCopies, copies) || !slices.Equal(gotRegions, []image.Rectangle{image.Rect(0, 0, 640, 480)}) {
		t.Fatalf("full update copies=%v regions=%v", gotCopies, gotRegions)
	}

	// A keyframe repaints the whole desktop, which makes the copies redundant.
	conn.outgoing.Reset()
	keyframe := []byte{0, 0, 0, 1, 0x67, 0x42, 0, 0, 1, 0x65, 0x11}
	if sent, err := session.WriteAVC420Update(keyframe, regions, copies); err != nil || !sent {
		t.Fatalf("send keyframe: %t, %v", sent, err)
	}
	messages = graphicsOutputFixture(t, conn.outgoing.Bytes())
	gotCopies, frame = decodeSurfaceCopiesFixture(t, decodeZGFXFixture(t, messages[0][2:]))
	gotRegions, _ = decodeAVC420DamageFixture(t, frame, 640, 480, 3)
	if len(gotCopies) != 0 || !slices.Equal(gotRegions, []image.Rectangle{image.Rect(0, 0, 640, 480)}) {
		t.Fatalf("keyframe copies=%v regions=%v", gotCopies, gotRegions)
	}
}

func TestGraphicsAVC420UpdateRejectsInvalidGeometry(t *testing.T) {
	t.Parallel()
	inside := []image.Rectangle{image.Rect(0, 0, 16, 16)}
	move := []SurfaceCopy{{Source: image.Rect(0, 16, 64, 64), Destination: image.Pt(0, 0)}}
	for _, test := range []struct {
		name    string
		regions []image.Rectangle
		copies  []SurfaceCopy
	}{
		{"overlapping regions", []image.Rectangle{image.Rect(0, 0, 32, 32), image.Rect(16, 16, 48, 48)}, nil},
		{"region outside", []image.Rectangle{image.Rect(600, 0, 656, 16)}, nil},
		{"empty region", []image.Rectangle{image.Rect(16, 16, 16, 32)}, nil},
		{"too many regions", make([]image.Rectangle, graphicsMaxRegions+1), nil},
		{"copy source outside", inside, []SurfaceCopy{{Source: image.Rect(0, 400, 64, 500)}}},
		{
			"copy destination outside", inside,
			[]SurfaceCopy{{Source: image.Rect(0, 0, 64, 64), Destination: image.Pt(600, 0)}},
		},
		{"empty copy", inside, []SurfaceCopy{{Source: image.Rect(0, 0, 0, 64)}}},
		{"too many copies", inside, slices.Repeat(move, graphicsMaxCopies+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			conn := &graphicsTestConn{}
			session := &Session{
				Width: 640, Height: 480, conn: conn,
				graphics: &graphicsState{ready: true}, dynamic: graphicsDynamicFixture(3),
			}
			if sent, err := session.WriteAVC420Update([]byte{0, 0, 1, 0x41}, test.regions, test.copies); err == nil || sent {
				t.Fatalf("accepted: %t, %v", sent, err)
			}
			if conn.outgoing.Len() != 0 || session.graphics.frameID != 0 || len(session.graphics.inFlight) != 0 {
				t.Fatal("rejected update changed sender state")
			}
		})
	}
}
