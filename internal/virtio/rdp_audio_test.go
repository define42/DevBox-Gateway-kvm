//go:build integration

package virtio

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/audio"
)

const rdpAudioTestChannel = 1004

func TestRDPDisplayAudioPlaybackReconnect(t *testing.T) {
	t.Parallel()
	display := newRDPTestDisplayWithConfig(t, RDPConfig{Audio: true})
	input := &rdpInputSink{events: make(chan rdpInputSinkEvent, 4)}
	display.SetInput(input)

	for attempt := range 2 {
		conn := connectRDPAudioTestSession(t, display, "rdpsnd")
		formats := readRDPAudioTestMessage(t, conn)
		if len(formats) != 42 || formats[0] != 7 ||
			binary.LittleEndian.Uint16(formats[18:]) != 1 ||
			binary.LittleEndian.Uint16(formats[21:]) != 8 {
			t.Fatalf("connection %d: unexpected audio formats %x", attempt, formats)
		}
		wantFormat := []byte{1, 0, 2, 0, 0x80, 0xbb, 0, 0, 0, 0xee, 2, 0, 4, 0, 16, 0, 0, 0}
		if !bytes.Equal(formats[24:], wantFormat) {
			t.Fatalf("audio format = %x, want 48 kHz stereo PCM16", formats[24:])
		}
		binary.LittleEndian.PutUint32(formats[4:], 1) // TSSNDCAPS_ALIVE
		writeRDPAudioTestMessage(t, conn, formats)
		writeRDPAudioTestMessage(t, conn, []byte{12, 0, 4, 0, 2, 0, 0, 0})
		training := readRDPAudioTestMessage(t, conn)
		if !bytes.Equal(training, []byte{6, 0, 4, 0, 1, 0, 0, 0}) {
			t.Fatalf("audio training = %x", training)
		}
		writeRDPAudioTestMessage(t, conn, training)

		// Input after training synchronizes with the session's reader without
		// reaching into its audio state or delaying for an arbitrary interval.
		code := uint16(0x100 + attempt)
		writeRDPInputUnicode(t, conn, true, code)
		assertRDPAudioTestKey(t, input, true, uint32(0x01000000)|uint32(code))
		pcm := bytes.Repeat([]byte{byte(attempt + 1), 2, 3, 4}, audio.PacketBytes/4)
		wave := streamRDPAudioTestPCM(t, conn, display, pcm)
		if len(wave) != 16+len(pcm) || wave[0] != 13 || wave[8] != 0 ||
			binary.LittleEndian.Uint16(wave[6:]) != 0 || !bytes.Equal(wave[16:], pcm) {
			t.Fatalf("connection %d: invalid Wave2 header or samples: %x", attempt, wave[:min(16, len(wave))])
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		waitRDPClientCount(t, display, 0)
		assertRDPAudioTestKey(t, input, false, uint32(0x01000000)|uint32(code))
	}
}

func TestRDPDisplayAudioWithoutClientChannelPreservesDesktop(t *testing.T) {
	t.Parallel()
	display := newRDPTestDisplayWithConfig(t, RDPConfig{Audio: true})
	input := &rdpInputSink{events: make(chan rdpInputSinkEvent, 2)}
	display.SetInput(input)
	conn := connectRDPAudioTestSession(t, display)
	display.WritePCM(make([]byte, audio.PacketBytes))

	// A client without rdpsnd must still receive bitmap output, not an audio
	// negotiation request on an unjoined channel.
	for {
		data := readRDPInputGlobal(t, conn)
		if len(data) >= 18 && data[14] == 2 {
			break
		}
	}
	writeRDPInputUnicode(t, conn, true, 0x100)
	assertRDPAudioTestKey(t, input, true, 0x01000100)
}

func connectRDPAudioTestSession(t *testing.T, display *RDPDisplay, channels ...string) net.Conn {
	t.Helper()
	conn := dialRDPTestDisplay(t, display)
	response := negotiateRDPTestDisplay(t, conn, 1)
	if response[11] != 2 || binary.LittleEndian.Uint32(response[15:]) != 1 {
		t.Fatalf("TLS negotiation response: got %x", response)
	}
	secured := tls.Client(conn, rdpTestClientTLS(t, display))
	if err := secured.Handshake(); err != nil {
		t.Fatal(err)
	}
	joinRDPInputTestChannels(t, secured, channels...)
	confirmRDPInputTestActive(t, secured)

	return secured
}

func writeRDPAudioTestMessage(t *testing.T, conn net.Conn, message []byte) {
	t.Helper()
	fragment := make([]byte, 8+len(message))
	binary.LittleEndian.PutUint32(fragment, uint32(len(message)))
	binary.LittleEndian.PutUint32(fragment[4:], 3) // CHANNEL_FLAG_FIRST | CHANNEL_FLAG_LAST
	copy(fragment[8:], message)
	packet := rdpInputAppendPERLength([]byte{0x64, 0, 0, 3, 0xec, 0x70}, len(fragment))
	writeRDPInputMCS(t, conn, append(packet, fragment...))
}

func readRDPAudioTestMessage(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	message := []byte{}
	for {
		packet := readRDPInputMCS(t, conn)
		if len(packet) < 7 || packet[0] != 0x68 {
			t.Fatalf("invalid channel indication: %x", packet)
		}
		channel := binary.BigEndian.Uint16(packet[3:])
		if channel == rdpInputGlobalChannel {
			continue // Pointer and bitmap updates can interleave with audio.
		}
		if channel != rdpAudioTestChannel {
			t.Fatalf("unexpected output channel %d", channel)
		}
		fragment := rdpInputTakePER(t, packet[6:])
		if len(fragment) < 8 {
			t.Fatalf("truncated audio fragment: %x", fragment)
		}
		total := int(binary.LittleEndian.Uint32(fragment))
		flags := binary.LittleEndian.Uint32(fragment[4:])
		if (flags&1 != 0) != (len(message) == 0) {
			t.Fatalf("invalid first-fragment flag: %#x", flags)
		}
		message = append(message, fragment[8:]...)
		if len(message) > total {
			t.Fatal("audio fragments exceed advertised length")
		}
		if flags&2 != 0 {
			if len(message) != total {
				t.Fatalf("audio message length = %d, want %d", len(message), total)
			}

			return message
		}
	}
}

func assertRDPAudioTestKey(t *testing.T, input *rdpInputSink, down bool, keysym uint32) {
	t.Helper()
	select {
	case event := <-input.events:
		assertRDPInputKeyEvent(t, event, down, keysym)
	case <-time.After(time.Second):
		t.Fatal("desktop input did not arrive during audio playback")
	}
}

func streamRDPAudioTestPCM(t *testing.T, conn net.Conn, display *RDPDisplay, pcm []byte) []byte {
	t.Helper()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				display.WritePCM(pcm)
			}
		}
	}()
	defer func() {
		close(stop)
		<-done
	}()

	return readRDPAudioTestMessage(t, conn)
}
