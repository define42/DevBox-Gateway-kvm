package rdp

import (
	"encoding/binary"
	"errors"
	"maps"
	"reflect"
	"testing"
)

func slowInput(events ...[4]uint16) []byte {
	data := make([]byte, 22+12*len(events))
	binary.LittleEndian.PutUint16(data, uint16(len(data)))
	binary.LittleEndian.PutUint16(data[2:], 0x17)
	data[14] = 28
	binary.LittleEndian.PutUint16(data[18:], uint16(len(events)))
	for i, event := range events {
		for j, value := range event {
			binary.LittleEndian.PutUint16(data[26+12*i+2*j:], value)
		}
	}

	return data
}

type unicodeKeyInput struct {
	code uint16
	up   bool
}

func unicodeInputPacket(tb testing.TB, fast bool, keys ...unicodeKeyInput) []byte {
	tb.Helper()
	if fast {
		if len(keys) > 255 {
			tb.Fatal("too many fast-path events in test packet")
		}
		data := []byte{0, byte(len(keys))}
		for _, key := range keys {
			flags := byte(0x80)
			if key.up {
				flags |= 1
			}
			data = append(data, flags, byte(key.code), byte(key.code>>8))
		}

		return data
	}
	events := make([][4]uint16, len(keys))
	for i, key := range keys {
		flags := uint16(0)
		if key.up {
			flags = 0x8000
		}
		events[i] = [4]uint16{5, flags, key.code, 0}
	}

	return slowInput(events...)
}

func holdInputKeys(tb testing.TB, decoder *InputDecoder, fast bool, count int) []InputEvent {
	tb.Helper()
	releases := make([]InputEvent, 0, count)
	for i := range count {
		code := uint16(0x1000 + i)
		packet := unicodeInputPacket(tb, fast, unicodeKeyInput{code: code})
		if _, err := decoder.Decode(packet, fast); err != nil {
			tb.Fatalf("hold key %d: %v", i, err)
		}
		releases = append(releases, InputEvent{Kind: InputKey, Key: 0x01000000 | uint32(code)})
	}

	return releases
}

func TestInputHeldKeyLimit(t *testing.T) {
	t.Parallel()
	for _, path := range []struct {
		name string
		fast bool
	}{
		{name: "slow"},
		{name: "fast", fast: true},
	} {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			var decoder InputDecoder
			want := holdInputKeys(
				t,
				&decoder,
				path.fast,
				maxHeldKeys,
			)
			// Repeats and unmatched key-ups must not consume additional slots.
			packet := unicodeInputPacket(
				t,
				path.fast,
				unicodeKeyInput{code: 0x1000},
				unicodeKeyInput{code: 0x3000, up: true},
			)
			if events, err := decoder.Decode(packet, path.fast); err != nil || len(events) != 2 {
				t.Fatalf("repeat and unmatched release = %v, %v", events, err)
			}
			for _, keys := range [][]unicodeKeyInput{
				{{code: 0x3000}},
				{{code: 0x3000}, {code: 0x3000, up: true}},
				{{code: 0x1000, up: true}, {code: 0x3000}, {code: 0x3001}},
			} {
				packet := unicodeInputPacket(t, path.fast, keys...)
				if events, err := decoder.Decode(packet, path.fast); !errors.Is(err, ErrInvalidInput) || events != nil {
					t.Fatalf("over-limit packet = %v, %v; want nil, ErrInvalidInput", events, err)
				}
			}
			if got := decoder.ReleaseAll(); !reflect.DeepEqual(got, want) {
				t.Fatalf("rejected input changed held keys: got %v; want %v", got, want)
			}
			if got := decoder.ReleaseAll(); len(got) != 0 {
				t.Fatalf("second release = %v", got)
			}
			// Disconnect/reset frees all slots for a later use of the decoder.
			holdInputKeys(
				t,
				&decoder,
				path.fast,
				maxHeldKeys,
			)
		})
	}
}

func TestInputHeldKeyReplacement(t *testing.T) {
	t.Parallel()
	for _, path := range []struct {
		name string
		fast bool
	}{
		{name: "slow"},
		{name: "fast", fast: true},
	} {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			var decoder InputDecoder
			want := holdInputKeys(
				t,
				&decoder,
				path.fast,
				maxHeldKeys,
			)
			packet := unicodeInputPacket(
				t,
				path.fast,
				unicodeKeyInput{code: 0x1000, up: true},
				unicodeKeyInput{code: 0x1000},
				unicodeKeyInput{code: 0x1000, up: true},
				unicodeKeyInput{code: 0x3000},
			)
			if events, err := decoder.Decode(packet, path.fast); err != nil || len(events) != 4 {
				t.Fatalf("replace at limit = %v, %v", events, err)
			}
			want = append(want[1:], InputEvent{Kind: InputKey, Key: 0x01003000})
			if got := decoder.ReleaseAll(); !reflect.DeepEqual(got, want) {
				t.Fatalf("replacement releases = %v; want %v", got, want)
			}
		})
	}
}

func TestInputHeldKeyChurn(t *testing.T) {
	t.Parallel()
	var decoder InputDecoder
	want := holdInputKeys(
		t,
		&decoder,
		false,
		maxHeldKeys,
	)
	keys := []unicodeKeyInput{{code: 0x1000, up: true}}
	// More unique keys than the state cap are legal when each is released
	// before the next one is pressed. Net-zero changes must not accumulate.
	for i := range 1000 {
		code := uint16(0x3000 + i)
		keys = append(keys, unicodeKeyInput{code: code}, unicodeKeyInput{code: code, up: true})
	}
	keys = append(keys, unicodeKeyInput{code: 0x1000})
	packet := unicodeInputPacket(t, false, keys...)
	if events, err := decoder.Decode(packet, false); err != nil || len(events) != len(keys) {
		t.Fatalf("key churn: %d events, %v; want %d events", len(events), err, len(keys))
	}
	if got := decoder.ReleaseAll(); !reflect.DeepEqual(got, want) {
		t.Fatalf("key churn changed held keys: got %v; want %v", got, want)
	}
}

func TestInputHeldKeyLimitIncludesScancodes(t *testing.T) {
	t.Parallel()
	var decoder InputDecoder
	want := holdInputKeys(
		t,
		&decoder,
		false,
		maxHeldKeys-1,
	)
	if _, err := decoder.Decode([]byte{4, 0, 0x1e}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.Decode([]byte{4, 0, 0x30}, true); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("extra scancode = %v; want ErrInvalidInput", err)
	}
	packet := unicodeInputPacket(t, false, unicodeKeyInput{code: 0x3000})
	if _, err := decoder.Decode(packet, false); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("extra Unicode key = %v; want ErrInvalidInput", err)
	}
	want = append([]InputEvent{{Kind: InputKey, Key: 'a'}}, want...)
	if got := decoder.ReleaseAll(); !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed releases = %v; want %v", got, want)
	}
}

func TestInputHeldKeyLimitPreservesSurrogate(t *testing.T) {
	t.Parallel()
	var decoder InputDecoder
	holdInputKeys(
		t,
		&decoder,
		true,
		maxHeldKeys,
	)
	if _, err := decoder.Decode([]byte{4, 0x80, 0x3d, 0xd8}, true); err != nil {
		t.Fatal(err)
	}
	events, err := decoder.Decode([]byte{4, 0x80, 0x42, 0xde}, true)
	if !errors.Is(err, ErrInvalidInput) || events != nil {
		t.Fatalf("over-limit surrogate = %v, %v", events, err)
	}
	if _, err := decoder.Decode([]byte{4, 0x81, 0, 0x10}, true); err != nil {
		t.Fatal(err)
	}
	events, err = decoder.Decode([]byte{4, 0x80, 0x42, 0xde}, true)
	want := []InputEvent{{Kind: InputKey, Down: true, Key: 0x0101f642}}
	if err != nil || !reflect.DeepEqual(events, want) {
		t.Fatalf("surrogate after rejected packet = %v, %v; want %v", events, err, want)
	}
}

func TestInputFailuresPreserveAllState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		data []byte
		fast bool
	}{
		{name: "slow limit", data: slowInput(
			[4]uint16{0x8001, 0x9000, 20, 30}, [4]uint16{0x8001, 0x023c},
			[4]uint16{4, 0x0200, 0x1d}, [4]uint16{5, 0, 0xd83d}, [4]uint16{5, 0, 0x3000})},
		{name: "fast limit", fast: true, data: []byte{
			5 << 2, 0x20, 0, 0x90, 20, 0, 30, 0, 0x20, 0x3c, 2, 0, 0, 0, 0,
			4, 0x1d, 0x80, 0x3d, 0xd8, 0x80, 0, 0x30,
		}},
		{name: "slow malformed", data: slowInput(
			[4]uint16{5, 0x8000, 0x1000}, [4]uint16{5, 0, 0x3000},
			[4]uint16{0x8001, 0x9000, 20, 30}, [4]uint16{0x8001, 0x023c},
			[4]uint16{4, 0x0200, 0x1d}, [4]uint16{5, 0, 0xd83d}, [4]uint16{0x1234})},
		{name: "fast malformed", fast: true, data: []byte{
			7 << 2, 0x81, 0, 0x10, 0x80, 0, 0x30, 0x20, 0, 0x90, 20, 0, 30, 0,
			0x20, 0x3c, 2, 0, 0, 0, 0, 4, 0x1d, 0x80, 0x3d, 0xd8, 0xe0,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var decoder InputDecoder
			holdInputKeys(
				t,
				&decoder,
				tc.fast,
				maxHeldKeys,
			)
			before := decoder
			before.held = maps.Clone(decoder.held)
			if events, err := decoder.Decode(tc.data, tc.fast); !errors.Is(err, ErrInvalidInput) || events != nil {
				t.Fatalf("invalid packet = %v, %v; want nil, ErrInvalidInput", events, err)
			}
			if !reflect.DeepEqual(decoder, before) {
				t.Fatal("invalid packet changed decoder state")
			}
		})
	}
}

func TestInputPaths(t *testing.T) {
	t.Parallel()
	// Shift+A, right Control, Up, left drag, right click, and a scroll notch.
	slow := slowInput(
		[4]uint16{4, 0, 0x2a}, [4]uint16{4, 0, 0x1e},
		[4]uint16{4, 0x8000, 0x1e}, [4]uint16{4, 0x8000, 0x2a},
		[4]uint16{4, 0x100, 0x1d}, [4]uint16{4, 0x100, 0x48},
		[4]uint16{0x8001, 0x9000, 100, 80}, [4]uint16{0x8001, 0x0800, 120, 90},
		[4]uint16{0x8001, 0xa000, 120, 90}, [4]uint16{0x8001, 0x0278},
	)
	fast := []byte{
		10 << 2,
		0, 0x2a, 0, 0x1e, 1, 0x1e, 1, 0x2a, 2, 0x1d, 2, 0x48,
		0x20, 0, 0x90, 100, 0, 80, 0, 0x20, 0, 0x08, 120, 0, 90, 0,
		0x20, 0, 0xa0, 120, 0, 90, 0, 0x20, 0x78, 2, 0, 0, 0, 0,
	}
	want := []InputEvent{
		{Kind: InputKey, Down: true, Key: 0xffe1},
		{Kind: InputKey, Down: true, Key: 'a'},
		{Kind: InputKey, Key: 'a'},
		{Kind: InputKey, Key: 0xffe1},
		{Kind: InputKey, Down: true, Key: 0xffe4},
		{Kind: InputKey, Down: true, Key: 0xff52},
		{Kind: InputPointer, X: 100, Y: 80, Buttons: 1},
		{Kind: InputPointer, X: 120, Y: 90, Buttons: 1},
		{Kind: InputPointer, X: 120, Y: 90, Buttons: 5},
		{Kind: InputPointer, X: 120, Y: 90, Buttons: 13},
		{Kind: InputPointer, X: 120, Y: 90, Buttons: 5},
	}
	for _, tc := range []struct {
		name string
		data []byte
		fast bool
	}{{"slow", slow, false}, {"fast", fast, true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var decoder InputDecoder
			got, err := decoder.Decode(tc.data, tc.fast)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("Decode = %#v, %v; want %#v", got, err, want)
			}
			releases := decoder.ReleaseAll()
			wantReleases := []InputEvent{
				{Kind: InputKey, Key: 0xff52},
				{Kind: InputKey, Key: 0xffe4},
				{Kind: InputPointer, X: 120, Y: 90},
			}
			if !reflect.DeepEqual(releases, wantReleases) {
				t.Fatalf("ReleaseAll = %#v; want %#v", releases, wantReleases)
			}
			if got := decoder.ReleaseAll(); len(got) != 0 {
				t.Fatalf("second ReleaseAll = %#v", got)
			}
		})
	}
}

func TestInputWheelAccumulation(t *testing.T) {
	t.Parallel()
	var decoder InputDecoder
	for _, flags := range []uint16{0x023c, 0x023c, 0x0388} {
		events, err := decoder.Decode(slowInput([4]uint16{0x8001, flags}), false)
		if err != nil {
			t.Fatal(err)
		}
		switch flags {
		case 0x023c:
			if decoder.wheel == 0 && (len(events) != 2 || events[0].Buttons != 8 || events[1].Buttons != 0) {
				t.Fatalf("positive wheel events = %#v", events)
			}
		case 0x0388:
			if len(events) != 2 || events[0].Buttons != 16 || events[1].Buttons != 0 {
				t.Fatalf("negative wheel events = %#v", events)
			}
		}
	}
}

func TestInputUnicodeSurrogates(t *testing.T) {
	t.Parallel()
	var decoder InputDecoder
	// Down/up surrogate halves can be interleaved and cross packet boundaries.
	packets := [][]byte{
		{4, 0x80, 0x3d, 0xd8},
		{4, 0x81, 0x3d, 0xd8},
		{4, 0x80, 0x42, 0xde},
		{4, 0x81, 0x42, 0xde},
		{4, 0x80, 0x42, 0xde}, // Unpaired low surrogate is ignored.
		{4, 0x80, 0x0d, 0},
	}
	var got []InputEvent
	for _, packet := range packets {
		events, err := decoder.Decode(packet, true)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, events...)
	}
	want := []InputEvent{
		{Kind: InputKey, Down: true, Key: 0x0101f642},
		{Kind: InputKey, Key: 0x0101f642},
		{Kind: InputKey, Down: true, Key: 0xff0d},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Unicode events = %#v; want %#v", got, want)
	}
}

func TestInputExtendedCountAndPause(t *testing.T) {
	t.Parallel()
	var decoder InputDecoder
	events, err := decoder.Decode([]byte{0, 5, 4, 0x1d, 0, 0x45, 5, 0x1d, 1, 0x45, 0x67}, true)
	want := []InputEvent{{Kind: InputKey, Down: true, Key: 0xff13}, {Kind: InputKey, Key: 0xff13}}
	if err != nil || !reflect.DeepEqual(events, want) {
		t.Fatalf("Pause events = %#v, %v; want %#v", events, err, want)
	}
}

func TestInputRejectsTruncatedPacketsAtomically(t *testing.T) {
	t.Parallel()
	packets := []struct {
		data []byte
		fast bool
	}{
		{slowInput([4]uint16{4, 0, 0x1e}, [4]uint16{0x8001, 0x9000, 20, 30}), false},
		{[]byte{8, 0, 0x1e, 0x20, 0, 0x90, 20, 0, 30, 0}, true},
	}
	for _, packet := range packets {
		for end := 0; end < len(packet.data); end++ {
			var decoder InputDecoder
			if _, err := decoder.Decode(packet.data[:end], packet.fast); err == nil {
				t.Fatalf("accepted truncated packet fast=%v length=%d", packet.fast, end)
			}
			if got := decoder.ReleaseAll(); len(got) != 0 {
				t.Fatalf("malformed packet modified state: %#v", got)
			}
		}
	}
}

func TestInputRejectsUnknownAndTrailingData(t *testing.T) {
	t.Parallel()
	for _, packet := range [][]byte{{4, 0xe0}, {4, 0, 0x1e, 0}, {0x84, 0, 0x1e}, {5, 0, 0x1e}} {
		var decoder InputDecoder
		if _, err := decoder.Decode(packet, true); err == nil {
			t.Errorf("accepted invalid packet %x", packet)
		}
	}
	var decoder InputDecoder
	if _, err := decoder.Decode(slowInput([4]uint16{0x1234}), false); err == nil {
		t.Error("accepted unknown slow-path input event")
	}
}

func FuzzInputDecoder(f *testing.F) {
	f.Add([]byte{4, 0, 0x1e}, true)
	f.Add(slowInput([4]uint16{0x8001, 0x9000, 100, 80}), false)
	f.Add([]byte{0, 1, 0x80, 0x3d, 0xd8}, true)
	f.Fuzz(func(t *testing.T, data []byte, fast bool) {
		var decoder InputDecoder
		_, err := decoder.Decode(data, fast)
		releases := decoder.ReleaseAll()
		if err != nil && len(releases) != 0 {
			t.Fatalf("invalid packet changed input state: %#v", releases)
		}
	})
}

func FuzzInputDecoderStateful(f *testing.F) {
	f.Add([]byte{4, 0, 0x1e}, true)
	f.Add([]byte{8, 0x81, 0, 0x10, 0xe0}, true)
	f.Add(slowInput([4]uint16{5, 0x8000, 0x1000}, [4]uint16{5, 0, 0x3000}), false)
	keys := make([][4]uint16, maxHeldKeys+1)
	for i := range keys {
		keys[i] = [4]uint16{5, 0, uint16(0x3000 + i), 0}
	}
	f.Add(slowInput(keys...), false)
	f.Fuzz(func(t *testing.T, data []byte, fast bool) {
		var decoder InputDecoder
		initial := slowInput(
			[4]uint16{5, 0, 0x1000},
			[4]uint16{0x8001, 0x9000, 20, 30},
			[4]uint16{0x8001, 0x023c},
			[4]uint16{4, 0x0200, 0x1d},
			[4]uint16{5, 0, 0xd83d},
		)
		if _, err := decoder.Decode(initial, false); err != nil {
			t.Fatal(err)
		}
		before := decoder
		before.held = maps.Clone(decoder.held)
		events, err := decoder.Decode(data, fast)
		if err != nil && (events != nil || !reflect.DeepEqual(decoder, before)) {
			t.Fatal("invalid packet changed input state or returned events")
		}
		if len(decoder.held) > maxHeldKeys {
			t.Fatalf("held keys = %d, want at most %d", len(decoder.held), maxHeldKeys)
		}
	})
}
