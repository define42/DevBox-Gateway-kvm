package flag_test

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

func TestRDPH264QPOptions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
		want int
		err  error
	}{
		{name: "default without RDP", want: 20},
		{name: "default bitmap", args: []string{"-rdp-fd=3"}, want: 20},
		{name: "default H264", args: []string{"-rdp-fd=3", "-rdp-h264"}, want: 20},
		{name: "minimum", args: []string{"-rdp-fd=3", "-rdp-h264", "-rdp-h264-qp=0"}},
		{name: "maximum", args: []string{"-rdp-fd=3", "-rdp-h264", "-rdp-h264-qp=51"}, want: 51},
		{name: "selected", args: []string{"-rdp-fd=3", "-rdp-h264", "-rdp-h264-qp=32"}, want: 32},
		{name: "negative", args: []string{"-rdp-fd=3", "-rdp-h264", "-rdp-h264-qp=-1"}, err: flag.ErrRDPH264QP},
		{name: "excessive", args: []string{"-rdp-fd=3", "-rdp-h264", "-rdp-h264-qp=52"}, err: flag.ErrRDPH264QP},
		{name: "without RDP", args: []string{"-rdp-h264-qp=20"}, err: flag.ErrRDPH264QP},
		{name: "bitmap minimum", args: []string{"-rdp-fd=3", "-rdp-h264-qp=0"}, err: flag.ErrRDPH264QP},
		{name: "bitmap default", args: []string{"-rdp-fd=3", "-rdp-h264-qp=20"}, err: flag.ErrRDPH264QP},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args, _, err := flag.ParseArgs(append([]string{"devbox-kvm", "boot"}, test.args...))
			if !errors.Is(err, test.err) {
				t.Fatalf("parse QP: got %v, want %v", err, test.err)
			}
			if err == nil && args.RDPH264QP != test.want {
				t.Fatalf("QP = %d, want %d", args.RDPH264QP, test.want)
			}
		})
	}
}

func TestRDPH264QPRejectsNonInteger(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "20.5", "auto", "99999999999999999999"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			_, _, err := flag.ParseArgs([]string{"devbox-kvm", "boot", "-rdp-fd=3", "-rdp-h264", "-rdp-h264-qp=" + value})
			if err == nil {
				t.Fatalf("non-integer QP %q accepted", value)
			}
		})
	}
}
