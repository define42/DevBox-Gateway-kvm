//go:build integration

package virtio

import (
	"crypto/tls"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

type rdpRelativeInputSink struct {
	events   chan relativePointerRecord
	absolute chan struct{}
}

func (*rdpRelativeInputSink) KeyEvent(bool, uint32) {}

func (s *rdpRelativeInputSink) PointerEvent(uint8, uint16, uint16) {
	s.absolute <- struct{}{}
}

func (s *rdpRelativeInputSink) RelativePointerEvent(buttons uint8, dx, dy int16) {
	s.events <- relativePointerRecord{buttons: buttons, dx: dx, dy: dy}
}

func TestRDPDisplayRelativePointerConnection(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		fast bool
	}{
		{name: "fast-path", fast: true},
		{name: "slow-path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			display := newRDPTestDisplay(t)
			input := &rdpRelativeInputSink{
				events: make(chan relativePointerRecord, 8), absolute: make(chan struct{}, 8),
			}
			display.SetInput(input)
			conn := dialRDPTestDisplay(t, display)
			negotiateRDPTestDisplay(t, conn, 1)
			secured := tls.Client(conn, rdpTestClientTLS(t, display))
			if err := secured.Handshake(); err != nil {
				t.Fatal(err)
			}
			demand := joinRDPInputTestChannelsWithVersion(t, secured, 0x80011)
			if flags := rdpDemandInputFlags(t, demand); flags&0x80 == 0 {
				t.Fatalf("relative input was not offered: %#x", flags)
			}
			confirmRDPInputTestActiveWithInput(t, secured, 0x80)
			writeRDPRelativeTestPacket(t, secured, test.fast, true, 0x9800, -7, 11)
			assertRDPRelativeEvent(t, input, relativePointerRecord{buttons: 1, dx: -7, dy: 11})
			// FreeRDP sends ordinary wheel events with reserved coordinates even
			// during relative capture. Neither event may move the absolute tablet.
			writeRDPRelativeTestPacket(t, secured, test.fast, false, 0x0278, 0, 0)
			assertRDPRelativeEvent(t, input, relativePointerRecord{buttons: 9})
			assertRDPRelativeEvent(t, input, relativePointerRecord{buttons: 1})
			if err := secured.Close(); err != nil {
				t.Fatal(err)
			}
			assertRDPRelativeEvent(t, input, relativePointerRecord{})
			if len(input.absolute) != 0 {
				t.Fatal("relative movement, wheel or disconnect release moved the absolute tablet")
			}
		})
	}
}

func rdpDemandInputFlags(t *testing.T, demand []byte) uint16 {
	t.Helper()
	offset := 14 + int(binary.LittleEndian.Uint16(demand[10:])) + 4
	for offset+4 <= len(demand) {
		kind := binary.LittleEndian.Uint16(demand[offset:])
		size := int(binary.LittleEndian.Uint16(demand[offset+2:]))
		if size < 4 || offset+size > len(demand) {
			break
		}
		if kind == 13 && size >= 6 {
			return binary.LittleEndian.Uint16(demand[offset+4:])
		}
		offset += size
	}
	t.Fatal("Demand Active omitted input capabilities")

	return 0
}

func writeRDPRelativeTestPacket(t *testing.T, conn net.Conn, fast, relative bool, flags uint16, dx, dy int16) {
	t.Helper()
	if fast {
		code := byte(1)
		if relative {
			code = 5
		}
		packet := []byte{
			4, 9, code << 5, byte(flags), byte(flags >> 8),
			byte(dx), byte(uint16(dx) >> 8), byte(dy), byte(uint16(dy) >> 8),
		}
		writeRDPTest(t, conn, packet)

		return
	}
	payload := make([]byte, 16)
	rdpInputPut16(payload, 0, 1)
	kind := 0x8001
	if relative {
		kind = 0x8004
	}
	rdpInputPut16(payload, 8, kind)
	rdpInputPut16(payload, 10, int(flags))
	rdpInputPut16(payload, 12, int(uint16(dx)))
	rdpInputPut16(payload, 14, int(uint16(dy)))
	writeRDPInputGlobal(t, conn, rdpInputShareData(28, payload))
}

func assertRDPRelativeEvent(t *testing.T, input *rdpRelativeInputSink, want relativePointerRecord) {
	t.Helper()
	select {
	case got := <-input.events:
		if got != want {
			t.Fatalf("relative input = %+v, want %+v", got, want)
		}
	case <-input.absolute:
		t.Fatal("relative input unexpectedly reached the absolute tablet")
	case <-time.After(2 * time.Second):
		t.Fatal("relative input did not reach the guest sink")
	}
}
