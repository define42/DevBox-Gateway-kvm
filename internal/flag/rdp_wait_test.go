package flag_test

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
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
		{name: "rdp-default", args: []string{"-rdp-fd", "3"}},
		{name: "enabled", args: []string{"-rdp-wait", "-rdp-fd", "3"}, want: true},
		{name: "explicitly-enabled", args: []string{"-rdp-fd", "3", "-rdp-wait=true"}, want: true},
		{name: "disabled-without-listener", args: []string{"-rdp-wait=false"}},
		{name: "missing-listener", args: []string{"-rdp-wait"}, bad: true},
		{name: "empty-listener", args: []string{"-rdp-wait", "-rdp-fd", "0"}, bad: true},
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
