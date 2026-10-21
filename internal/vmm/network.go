package vmm

import (
	"fmt"

	"github.com/define42/DevBox-Gateway-kvm/internal/usernet"
)

// validateNetwork rejects ignored or malformed destination policies before KVM
// opens. A direct boot must opt into user-isolated to apply policy exceptions.
func (v *VMM) validateNetwork() error {
	if (v.Network != "" && v.Network != "none" && v.Network != "user" && v.Network != "user-isolated") ||
		(v.Network != "" && v.TapIfName != "") {
		return errNetworkConfig
	}

	if err := usernet.ValidateDestinationPolicy(v.NetworkPolicy, v.NetworkAllowCIDRs); err != nil {
		return fmt.Errorf("%w: %w", errNetworkConfig, err)
	}
	if v.Network != "user-isolated" && (v.NetworkAllowCIDRs != "" ||
		(v.NetworkPolicy != "" && v.NetworkPolicy != "internet")) {
		return fmt.Errorf("%w: destination policy options require user-isolated", errNetworkConfig)
	}

	return nil
}
