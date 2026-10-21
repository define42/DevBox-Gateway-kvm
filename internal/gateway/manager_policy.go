package gateway

import (
	"fmt"

	"github.com/define42/DevBox-Gateway-kvm/internal/usernet"
)

func normalizeManagerNetworkConfig(c *ManagerConfig) error {
	if c.NetworkPolicy == "" {
		c.NetworkPolicy = "internet"
	}
	if err := usernet.ValidateDestinationPolicy(c.NetworkPolicy, c.NetworkAllowCIDRs); err != nil {
		return fmt.Errorf("invalid VM network policy: %w: %w", err, ErrInvalid)
	}

	return nil
}
