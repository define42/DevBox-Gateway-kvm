package flag_test

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

func TestRDPProfileOptions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
		want string
		err  error
	}{
		{name: "default without RDP", want: "smooth"},
		{name: "default with RDP", args: []string{"-rdp", "127.0.0.1:3389"}, want: "smooth"},
		{name: "office", args: []string{"-rdp", "127.0.0.1:3389", "-rdp-profile=office"}, want: "office"},
		{name: "smooth", args: []string{"-rdp", "127.0.0.1:3389", "-rdp-profile=smooth"}, want: "smooth"},
		{
			name: "office H264",
			args: []string{"-rdp", "127.0.0.1:3389", "-rdp-h264", "-rdp-profile=office"},
			want: "office",
		},
		{name: "office without RDP", args: []string{"-rdp-profile=office"}, err: flag.ErrRDPProfile},
		{name: "explicit smooth without RDP", args: []string{"-rdp-profile=smooth"}, err: flag.ErrRDPProfile},
		{name: "empty without RDP", args: []string{"-rdp-profile="}, err: flag.ErrRDPProfile},
		{name: "empty", args: []string{"-rdp", "127.0.0.1:3389", "-rdp-profile="}, err: flag.ErrRDPProfile},
		{name: "unknown", args: []string{"-rdp", "127.0.0.1:3389", "-rdp-profile=fast"}, err: flag.ErrRDPProfile},
		{name: "wrong case", args: []string{"-rdp", "127.0.0.1:3389", "-rdp-profile=Office"}, err: flag.ErrRDPProfile},
		{name: "whitespace", args: []string{"-rdp", "127.0.0.1:3389", "-rdp-profile=office "}, err: flag.ErrRDPProfile},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args, _, err := flag.ParseArgs(append([]string{"gokvm", "boot"}, test.args...))
			if !errors.Is(err, test.err) {
				t.Fatalf("ParseArgs() error = %v, want %v", err, test.err)
			}
			if err == nil && args.RDPProfile != test.want {
				t.Fatalf("RDP profile = %q, want %q", args.RDPProfile, test.want)
			}
		})
	}
}
