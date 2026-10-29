package rdp

import (
	"bytes"
	"encoding/asn1"
	"encoding/binary"
	"testing"
)

func TestConnectResponseBEREncoding(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		channels int
	}{
		{"no static channels", 0},
		{"odd channel count", 1},
		{"long form length", 16},
		{"maximum channels", 31},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkConnectResponseEncoding(t, connectResponse(1, test.channels, 0x80004))
		})
	}
}

func checkConnectResponseEncoding(t *testing.T, packet []byte) {
	t.Helper()
	var response struct {
		Result           asn1.Enumerated
		CalledConnectID  int
		DomainParameters []int
		UserData         []byte
	}
	// The standard decoder checks signed INTEGER encoding independently of
	// our wire helpers. FreeRDP's unsigned decoder accepts malformed integers.
	rest, err := asn1.UnmarshalWithParams(packet, &response, "application,tag:102")
	if err != nil || len(rest) != 0 {
		t.Fatalf("invalid MCS Connect Response encoding: rest=%x err=%v", rest, err)
	}
	if response.Result != 0 || response.CalledConnectID != 0 || len(response.UserData) == 0 {
		t.Fatalf("invalid successful MCS Connect Response: %+v", response)
	}
	if len(response.DomainParameters) != 8 {
		t.Fatalf("domain parameter count = %d, want 8", len(response.DomainParameters))
	}
	// 65528 leaves room for the TPKT and X.224 headers in a 65535-byte frame.
	if got := response.DomainParameters[6]; got != 65528 {
		t.Errorf("maxMCSPDUsize = %d, want 65528", got)
	}
	if got := response.DomainParameters[7]; got != 2 {
		t.Errorf("MCS protocolVersion = %d, want 2", got)
	}
}

func TestDemandActiveBitmapRequirements(t *testing.T) {
	t.Parallel()
	session := &Session{Width: 1024, Height: 768, BitsPerPixel: 24}
	checkDemandActiveBitmap(t, session.demandActive())
}

func checkDemandActiveBitmap(t *testing.T, packet []byte) {
	t.Helper()
	capabilities := demandActiveCapabilities(t, packet)
	bitmap := capabilities[2]
	if len(bitmap) != 24 {
		t.Fatalf("bitmap capability payload length = %d, want 24", len(bitmap))
	}
	// MS-RDPBCGR 2.2.7.1.2 requires both flags for a connection to proceed.
	if got := binary.LittleEndian.Uint16(bitmap[16:]); got != 1 {
		t.Errorf("bitmapCompressionFlag = %d, want TRUE", got)
	}
	if got := binary.LittleEndian.Uint16(bitmap[20:]); got != 1 {
		t.Errorf("multipleRectangleSupport = %d, want TRUE", got)
	}
}

func TestDemandActiveMandatoryCapabilities(t *testing.T) {
	t.Parallel()
	session := &Session{Width: 1024, Height: 768, BitsPerPixel: 24}
	capabilities := demandActiveCapabilities(t, session.demandActive())
	for _, kind := range []uint16{1, 2, 3, 8, 13, 20} {
		if _, exists := capabilities[kind]; !exists {
			t.Errorf("missing mandatory server capability %d", kind)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	order := capabilities[3]
	if len(order) != 84 {
		t.Fatalf("order capability payload length = %d, want 84", len(order))
	}
	if got := binary.LittleEndian.Uint16(order[26:]); got != 1 {
		t.Errorf("maximumOrderLevel = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(order[30:]); got != 2 {
		t.Errorf("orderFlags = %#x, want NEGOTIATEORDERSUPPORT", got)
	}
	if !bytes.Equal(order[32:64], make([]byte, 32)) {
		t.Error("bitmap-only server advertised primary drawing orders")
	}
	channel := capabilities[20]
	if len(channel) != 8 {
		t.Fatalf("virtual channel capability payload length = %d, want 8", len(channel))
	}
	if got := binary.LittleEndian.Uint32(channel); got != 0 {
		t.Errorf("virtual channel compression flags = %#x, want VCCAPS_NO_COMPR", got)
	}
	if got := binary.LittleEndian.Uint32(channel[4:]); got != 1600 {
		t.Errorf("VCChunkSize = %d, want 1600", got)
	}
}

func demandActiveCapabilities(t *testing.T, packet []byte) map[uint16][]byte {
	t.Helper()
	if len(packet) < 18 || int(binary.LittleEndian.Uint16(packet)) != len(packet) ||
		binary.LittleEndian.Uint16(packet[2:]) != 0x11 {
		t.Fatalf("invalid Demand Active header: %x", packet)
	}
	offset := 14 + int(binary.LittleEndian.Uint16(packet[10:]))
	length := int(binary.LittleEndian.Uint16(packet[12:]))
	if length < 4 || offset > len(packet)-4 || offset+length != len(packet)-4 {
		t.Fatalf("invalid Demand Active capabilities length: %x", packet)
	}
	count := int(binary.LittleEndian.Uint16(packet[offset:]))
	data := packet[offset+4 : offset+length]
	capabilities := make(map[uint16][]byte, count)
	for range count {
		if len(data) < 4 {
			t.Fatal("truncated Demand Active capability header")
		}
		kind := binary.LittleEndian.Uint16(data)
		size := int(binary.LittleEndian.Uint16(data[2:]))
		if size < 4 || size > len(data) {
			t.Fatalf("invalid capability %d length %d", kind, size)
		}
		if _, exists := capabilities[kind]; exists {
			t.Fatalf("duplicate capability %d", kind)
		}
		capabilities[kind] = data[4:size]
		data = data[size:]
	}
	if len(data) != 0 {
		t.Fatalf("unaccounted capability bytes: %x", data)
	}

	return capabilities
}
