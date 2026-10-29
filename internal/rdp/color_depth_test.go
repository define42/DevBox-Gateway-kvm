package rdp

import (
	"encoding/binary"
	"image"
	"strconv"
	"testing"
)

func TestClientCoreSelectsSessionColorDepth(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                           string
		length, legacy, postBeta, high int
		supported, flags, want         int
		graphics, invalid              bool
	}{
		{name: "omitted optional fields", length: 128, want: 24},
		{name: "zero optional fields rejected", length: 142, invalid: true},
		{name: "post beta RGB565", length: 130, legacy: 0xca01, postBeta: 0xca03, want: 16},
		{name: "post beta true color", length: 130, legacy: 0xca01, postBeta: 0xca04, want: 24},
		{name: "zero post beta overrides default", length: 130, invalid: true},
		{name: "high color overrides post beta", length: 138, legacy: 0xca01, postBeta: 0xca04, high: 16, want: 16},
		{name: "zero high color overrides post beta", length: 138, postBeta: 0xca04, invalid: true},
		{name: "zero high color uses supported fallback", length: 140, postBeta: 0xca04, supported: 1, want: 24},
		{name: "high color overrides unknown legacy", length: 138, legacy: 0xffff, high: 24, want: 24},
		{name: "24 bpp with GFX capability", length: 142, high: 24, flags: 0x100, want: 24},
		{name: "32 bpp request with GFX", length: 142, high: 24, supported: 8, flags: 0x102, want: 32, graphics: true},
		{name: "32 bpp request without GFX", length: 142, high: 24, supported: 8, flags: 2, want: 32},
		{name: "32 bpp support alone keeps preference", length: 142, high: 24, supported: 8, flags: 0x100, want: 24},
		{name: "32 bpp requires support bit", length: 142, high: 16, supported: 2, flags: 0x102, want: 16},
		{name: "unsupported 8 bpp falls back to 24", length: 142, high: 8, supported: 3, want: 24},
		{name: "unsupported 15 bpp falls back to 16", length: 142, high: 15, supported: 2, want: 16},
		{name: "unsupported 4 bpp rejected", length: 128, legacy: 0xca00, invalid: true},
		{name: "unknown preference rejected", length: 128, legacy: 0xffff, invalid: true},
		{name: "32 bpp high color needs request flag", length: 142, high: 32, supported: 9, flags: 0x100, want: 24},
		{name: "32 bpp high color without fallback rejected", length: 142, high: 32, supported: 8, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			core := colorDepthCoreFixture(test.length)
			put16(core, 8, test.legacy)
			for _, field := range []struct{ offset, value int }{
				{128, test.postBeta}, {136, test.high}, {138, test.supported}, {140, test.flags},
			} {
				if field.offset+2 <= len(core) {
					put16(core, field.offset, field.value)
				}
			}
			settings, err := parseConnectInitialDetails(colorDepthConnectFixture(core))
			if test.invalid {
				if err == nil {
					t.Fatal("accepted unsupported color depth")
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if settings.bitsPerPixel != test.want || settings.graphics != test.graphics {
				t.Fatalf("depth=%d graphics=%v, want depth=%d graphics=%v",
					settings.bitsPerPixel, settings.graphics, test.want, test.graphics)
			}
		})
	}
}

func TestClientCoreRejectsPartialOptionalFields(t *testing.T) {
	t.Parallel()
	for _, length := range []int{127, 129, 131, 133, 134, 135, 137, 139, 141} {
		t.Run(strconv.Itoa(length), func(t *testing.T) {
			t.Parallel()
			if _, err := parseConnectInitialDetails(colorDepthConnectFixture(colorDepthCoreFixture(length))); err == nil {
				t.Fatalf("accepted CS_CORE ending inside a field at offset %d", length)
			}
		})
	}
}

func TestClientCoreAcceptsCompleteOptionalFields(t *testing.T) {
	t.Parallel()
	for _, length := range []int{128, 130, 132, 136, 138, 140, 142} {
		t.Run(strconv.Itoa(length), func(t *testing.T) {
			t.Parallel()
			core := colorDepthCoreFixture(length)
			if len(core) >= 130 {
				put16(core, 128, 0xca04)
			}
			if len(core) >= 138 {
				put16(core, 136, 24)
			}
			settings, err := parseConnectInitialDetails(colorDepthConnectFixture(core))
			if err != nil || settings.bitsPerPixel != 24 {
				t.Fatalf("complete CS_CORE fields at length %d: depth=%d, error=%v", length, settings.bitsPerPixel, err)
			}
		})
	}
}

func TestConfirmActiveRetainsAdvertisedColorDepth(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ server, client int }{{32, 24}, {24, 32}, {16, 24}} {
		t.Run(strconv.Itoa(test.server)+"/"+strconv.Itoa(test.client), func(t *testing.T) {
			t.Parallel()
			session := &Session{Width: 64, Height: 32, BitsPerPixel: test.server}
			capabilities := demandActiveCapabilities(t, session.demandActive())
			advertised := int(binary.LittleEndian.Uint16(capabilities[2]))
			if advertised != test.server {
				t.Fatalf("server advertised %d bpp, want %d", advertised, test.server)
			}
			confirm := relativeConfirmFixture(0x31)
			put16(confirm, 24, test.client)
			for _, phase := range []string{"initial activation", "reactivation"} {
				if err := session.confirmActiveDepth(confirm, phase == "initial activation"); err != nil {
					t.Fatalf("%s rejected client preference %d: %v", phase, test.client, err)
				}
				if session.BitsPerPixel != advertised {
					t.Fatalf("%s changed advertised depth from %d to %d", phase, advertised, session.BitsPerPixel)
				}
				caps := demandActiveCapabilities(t, session.demandActive())
				if got := int(binary.LittleEndian.Uint16(caps[2])); got != advertised {
					t.Fatalf("%s changed subsequent Demand Active to %d bpp", phase, got)
				}
				checkBitmapUsesAdvertisedDepth(t, session, advertised)
			}
		})
	}
}

func checkBitmapUsesAdvertisedDepth(t *testing.T, session *Session, advertised int) {
	t.Helper()
	encoder, err := NewBitmapEncoder(session.Width, session.Height, session.BitsPerPixel)
	if err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, session.Width, session.Height))
	updates := 0
	if err := encoder.WriteFrame(frame, false, func(packet []byte) error {
		updates++
		if len(packet) < 22 || binary.LittleEndian.Uint16(packet) != 1 || binary.LittleEndian.Uint16(packet[2:]) != 1 {
			t.Fatal("first bitmap update has invalid rectangle framing")
		}
		if depth := int(binary.LittleEndian.Uint16(packet[16:])); depth != advertised {
			t.Fatalf("first bitmap uses %d bpp, server advertised %d", depth, advertised)
		}
		if got, want := len(packet)-22, session.Width*session.Height*advertised/8; got != want {
			t.Fatalf("bitmap pixel byte count=%d, want %d for advertised depth", got, want)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if updates != 1 {
		t.Fatalf("expected one first-frame rectangle, got %d", updates)
	}
}

func colorDepthCoreFixture(length int) []byte {
	core := make([]byte, length)
	binary.LittleEndian.PutUint32(core, 0x80004)
	put16(core, 4, 1024)
	put16(core, 6, 768)

	return core
}

func colorDepthConnectFixture(core []byte) []byte {
	block := make([]byte, 4+len(core))
	put16(block, 0, 0xc001)
	put16(block, 2, len(block))
	copy(block[4:], core)
	gcc := appendPERLength([]byte{0, 5, 0, 0x14, 0x7c, 0, 1}, len(block)+14)
	gcc = append(gcc, 0, 8, 0, 16, 0, 1, 0xc0, 0, 'D', 'u', 'c', 'a')
	gcc = append(appendPERLength(gcc, len(block)), block...)
	body := []byte{4, 1, 1, 4, 1, 1, 1, 1, 0xff, 0x30, 0, 0x30, 0, 0x30, 0}
	body = append(body, ber(4, gcc)...)

	return append([]byte{0x7f}, ber(0x65, body)...)
}
