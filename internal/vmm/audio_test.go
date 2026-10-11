package vmm

import (
	"errors"
	"testing"
)

func TestAudioConfigRejectsBeforeOpeningKVM(t *testing.T) {
	t.Parallel()
	for _, config := range []Config{
		{Audio: "rdp"},
		{Audio: "unknown", RDP: "127.0.0.1:0"},
	} {
		config.Dev = "/missing-kvm-device"
		if err := New(config).Init(); !errors.Is(err, errAudioConfig) {
			t.Fatalf("got %v, want audio configuration error", err)
		}
	}
}

func TestAudioDisplayConfiguration(t *testing.T) {
	t.Parallel()
	v := New(Config{Audio: "rdp", RDP: "127.0.0.1:0"})
	display, err := v.display(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer display.Close()
	// Audio with no client must be discarded without stopping guest playback.
	for range 20 {
		v.rdpDisplay.WritePCM(make([]byte, 1920))
	}
	if err := display.Close(); err != nil {
		t.Fatal(err)
	}
	v.rdpDisplay.WritePCM(make([]byte, 1920))
}
