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
	} {
		v := New(config)
		if err := v.Init(); !errors.Is(err, errNetworkConfig) {
			t.Errorf("network=%q TAP=%q: got %v, want network config error", config.Network, config.TapIfName, err)
		}
	}
}
