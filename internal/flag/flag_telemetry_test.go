package flag_test

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

func TestTelemetryDescriptorOption(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
		want int
		err  error
	}{
		{name: "disabled by default"},
		{name: "explicit disabled", args: []string{"-telemetry-fd", "0"}},
		{name: "managed telemetry", args: []string{"-telemetry-fd", "4"}, want: 4},
		{
			name: "managed with inherited RDP", want: 4,
			args: []string{"-rdp-fd", "3", "-telemetry-fd", "4"},
		},
		{name: "negative", args: []string{"-telemetry-fd", "-1"}, err: flag.ErrTelemetryFD},
		{name: "stdout", args: []string{"-telemetry-fd", "1"}, err: flag.ErrTelemetryFD},
		{name: "stderr", args: []string{"-telemetry-fd", "2"}, err: flag.ErrTelemetryFD},
		{
			name: "collides with RDP", err: flag.ErrTelemetryFD,
			args: []string{"-rdp-fd", "3", "-telemetry-fd", "3"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args, _, err := flag.ParseArgs(append([]string{"devbox-kvm", "boot"}, test.args...))
			if !errors.Is(err, test.err) {
				t.Fatalf("ParseArgs error = %v, want %v", err, test.err)
			}
			if err == nil && args.TelemetryFD != test.want {
				t.Fatalf("telemetry descriptor = %d, want %d", args.TelemetryFD, test.want)
			}
		})
	}
}
