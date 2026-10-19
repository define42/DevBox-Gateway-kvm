package flag_test

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

func TestProfileSocketOption(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "disabled by default"},
		{name: "explicit disabled", args: []string{"-profile-socket", ""}},
		{name: "private socket", args: []string{"-profile-socket", "/private/cpu.sock"}, want: "/private/cpu.sock"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args, _, err := flag.ParseArgs(append([]string{"gokvm", "boot"}, test.args...))
			if err != nil {
				t.Fatal(err)
			}
			if args.ProfileSocket != test.want {
				t.Fatalf("profile socket = %q, want %q", args.ProfileSocket, test.want)
			}
		})
	}
}
