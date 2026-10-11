package flag_test

import (
	"errors"
	"testing"

	"github.com/define42/gokvm/internal/flag"
)

func TestNetworkOptions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		args    []string
		network string
		tap     string
		invalid bool
	}{
		{name: "disabled by default"},
		{name: "user", args: []string{"-net", "user"}, network: "user"},
		{name: "isolated user", args: []string{"-net", "user-isolated"}, network: "user-isolated"},
		{name: "isolated user and TAP", args: []string{"-net", "user-isolated", "-t", "tap0"}, invalid: true},
		{name: "none", args: []string{"-net", "none"}, network: "none"},
		{name: "existing TAP", args: []string{"-t", "tap0"}, tap: "tap0"},
		{name: "unknown", args: []string{"-net", "invalid"}, invalid: true},
		{name: "user and TAP", args: []string{"-net", "user", "-t", "tap0"}, invalid: true},
		{name: "none and TAP", args: []string{"-t", "tap0", "-net", "none"}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args, _, err := flag.ParseArgs(append([]string{"gokvm", "boot"}, tc.args...))
			if tc.invalid {
				if !errors.Is(err, flag.ErrNetwork) {
					t.Fatalf("got %v, want network option error", err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if args.Network != tc.network || args.TapIfName != tc.tap {
				t.Fatalf("network=%q TAP=%q, want %q/%q", args.Network, args.TapIfName, tc.network, tc.tap)
			}
		})
	}
}
