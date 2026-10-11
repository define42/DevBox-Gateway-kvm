package flag_test

import (
	"errors"
	"testing"

	"github.com/define42/gokvm/flag"
)

func TestRDPWaitOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want bool
		bad  bool
	}{
		{name: "default"},
		{name: "rdp-default", args: []string{"-rdp", "127.0.0.1:3390"}},
		{name: "enabled", args: []string{"-rdp-wait", "-rdp", "127.0.0.1:3390"}, want: true},
		{name: "explicitly-enabled", args: []string{"-rdp", ":3390", "-rdp-wait=true"}, want: true},
		{name: "disabled-without-listener", args: []string{"-rdp-wait=false"}},
		{name: "missing-listener", args: []string{"-rdp-wait"}, bad: true},
		{name: "empty-listener", args: []string{"-rdp-wait", "-rdp", ""}, bad: true},
		{name: "vnc-only", args: []string{"-rdp-wait", "-vnc", "127.0.0.1:5900"}, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args, _, err := flag.ParseArgs(append([]string{"gokvm", "boot"}, tc.args...))
			if tc.bad {
				if !errors.Is(err, flag.ErrRDPWait) {
					t.Fatalf("got %v, want RDP wait option error", err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if args.RDPWait != tc.want {
				t.Fatalf("RDPWait = %v, want %v", args.RDPWait, tc.want)
			}
		})
	}
}
