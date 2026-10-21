package vmm

import (
	"errors"
	"testing"
)

func TestInvalidNetworkConfigFailsBeforeOpeningKVM(t *testing.T) {
	t.Parallel()

	for _, config := range []Config{
		{Network: "invalid"},
		{Network: "user", TapIfName: "tap0"},
		{Network: "user-isolated", TapIfName: "tap0"},
		{Network: "none", TapIfName: "tap0"},
		{Network: "user-isolated", NetworkPolicy: "unrestricted"},
		{Network: "user-isolated", NetworkAllowCIDRs: "::1/128"},
		{Network: "user-isolated", NetworkAllowCIDRs: "0.0.0.0/0"},
		{Network: "user", NetworkPolicy: "allowlist"},
		{Network: "none", NetworkAllowCIDRs: "127.0.0.1/32"},
	} {
		v := New(config)
		if err := v.Init(); !errors.Is(err, errNetworkConfig) {
			t.Errorf("network=%q TAP=%q: got %v, want network config error", config.Network, config.TapIfName, err)
		}
	}
}
