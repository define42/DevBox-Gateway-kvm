//go:build integration

package virtio

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"net"
	"testing"
)

func TestRDPBackendBitmapOnlyPreservesDisplayAndAudio(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		h264 bool
	}{
		{name: "bitmap only"},
		{name: "H264 control", h264: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			display := newRDPTestDisplayWithConfig(t, RDPConfig{H264: test.h264, Audio: true})
			input := &rdpInputSink{events: make(chan rdpInputSinkEvent, 2)}
			display.SetInput(input)
			raw := dialRDPTestDisplay(t, display)
			negotiateRDPTestDisplay(t, raw, 1)
			conn := tls.Client(raw, rdpTestClientTLS(t, display))
			if err := conn.Handshake(); err != nil {
				t.Fatal(err)
			}
			// Advertise graphics support in both cases: bitmap-only mode must
			// omit graphics even when a Windows-style client supports it.
			channels := []string{"drdynvc", "rdpsnd"}
			initial := rdpInputConnectInitialWithGraphics(0x80004, true, channels...)
			joinRDPInputTestChannelsWithInitial(t, conn, initial, channels...)
			confirmRDPInputTestActive(t, conn)
			audio := readRDPBitmapOnlyChannel(t, conn, 1005)
			if len(audio) != 42 || audio[0] != 7 {
				t.Fatalf("audio format offer = %x", audio)
			}
			caps := readRDPDiagnosticDynamicMessage(t, conn)
			if !bytes.Equal(caps, []byte{0x50, 0, 1, 0}) {
				t.Fatalf("dynamic capability offer = %x", caps)
			}
			writeRDPDiagnosticDynamicMessage(t, conn, []byte{0x5c, 0, 3, 0})
			if test.h264 {
				want := append([]byte{0x10, 1}, "Microsoft::Windows::RDS::Graphics\x00"...)
				if create := readRDPDiagnosticDynamicMessage(t, conn); !bytes.Equal(create, want) {
					t.Fatalf("H264 control graphics create = %x, want %x", create, want)
				}
			}
			// If bitmap mode accidentally enables graphics, its CREATE appears
			// before this display CREATE and fails the assertion.
			assertRDPDiagnosticDisplayCreate(t, conn)
			writeRDPDiagnosticDynamicMessage(t, conn, []byte{0x10, 2, 0, 0, 0, 0})
			displayCaps := readRDPDiagnosticDynamicMessage(t, conn)
			if len(displayCaps) != 22 || !bytes.Equal(displayCaps[:2], []byte{0x30, 2}) ||
				binary.LittleEndian.Uint32(displayCaps[2:]) != 5 {
				t.Fatalf("display-control capabilities = %x", displayCaps)
			}
			writeRDPInputUnicode(t, conn, true, 0x100)
			assertRDPAudioTestKey(t, input, true, 0x01000100)
			// Without completing optional graphics negotiation, both modes
			// must continue producing normal bitmap updates on the global channel.
			writeRDPInputGlobal(t, conn, rdpInputShareData(33,
				[]byte{1, 0, 0, 0, 0, 0, 0, 0, 0x1f, 3, 0x57, 2})) // Refresh 800x600.
			for {
				data := readRDPInputGlobal(t, conn)
				if len(data) >= 20 && data[14] == 2 && binary.LittleEndian.Uint16(data[18:]) == 1 {
					break
				}
			}
		})
	}
}

func readRDPBitmapOnlyChannel(t *testing.T, conn net.Conn, want uint16) []byte {
	t.Helper()
	for {
		packet := readRDPInputMCS(t, conn)
		if len(packet) < 7 || packet[0] != 0x68 {
			t.Fatalf("invalid channel indication: %x", packet)
		}
		channel := binary.BigEndian.Uint16(packet[3:])
		if channel == rdpInputGlobalChannel {
			continue
		}
		if channel != want {
			t.Fatalf("output channel = %d, want %d", channel, want)
		}
		fragment := rdpInputTakePER(t, packet[6:])
		if len(fragment) < 8 || binary.LittleEndian.Uint32(fragment) != uint32(len(fragment)-8) ||
			binary.LittleEndian.Uint32(fragment[4:]) != 3 {
			t.Fatalf("invalid unfragmented channel message: %x", fragment)
		}

		return fragment[8:]
	}
}
