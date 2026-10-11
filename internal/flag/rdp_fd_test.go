package flag_test

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

func TestRDPInheritedListenerOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		fd   int
		bad  bool
	}{
		{name: "default"},
		{name: "disabled", args: []string{"-rdp-fd", "0"}},
		{name: "inherited", args: []string{"-rdp", "127.0.0.1:3390", "-rdp-fd", "3"}, fd: 3},
		{name: "higher descriptor", args: []string{"-rdp", "127.0.0.1:3390", "-rdp-fd", "9"}, fd: 9},
		{name: "negative", args: []string{"-rdp", "127.0.0.1:3390", "-rdp-fd", "-1"}, bad: true},
		{name: "stdout", args: []string{"-rdp", "127.0.0.1:3390", "-rdp-fd", "1"}, bad: true},
		{name: "stderr", args: []string{"-rdp", "127.0.0.1:3390", "-rdp-fd", "2"}, bad: true},
		{name: "missing address", args: []string{"-rdp-fd", "3"}, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args, _, err := flag.ParseArgs(append([]string{"gokvm", "boot"}, tc.args...))
			if tc.bad {
				if !errors.Is(err, flag.ErrRDPFD) {
					t.Fatalf("got %v, want inherited RDP listener option error", err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if args.RDPFD != tc.fd {
				t.Fatalf("RDPFD = %d, want %d", args.RDPFD, tc.fd)
			}
		})
	}
}
