package flag_test

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

func TestAudioOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want string
		bad  bool
	}{
		{name: "default"},
		{name: "disabled", args: []string{"-audio", "none"}, want: "none"},
		{name: "rdp", args: []string{"-audio", "rdp", "-rdp", "127.0.0.1:3390"}, want: "rdp"},
		{name: "missing-listener", args: []string{"-audio", "rdp"}, bad: true},
		{name: "vnc-only", args: []string{"-audio", "rdp", "-vnc", "127.0.0.1:5900"}, bad: true},
		{name: "unknown", args: []string{"-audio", "alsa", "-rdp", ":3390"}, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args, _, err := flag.ParseArgs(append([]string{"gokvm", "boot"}, tc.args...))
			if tc.bad {
				if !errors.Is(err, flag.ErrAudio) {
					t.Fatalf("got %v, want audio option error", err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if args.Audio != tc.want {
				t.Fatalf("audio = %q, want %q", args.Audio, tc.want)
			}
		})
	}
}
