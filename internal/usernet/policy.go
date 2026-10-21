package usernet

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/miekg/dns"
)

// ErrDestinationPolicy identifies an invalid operator-supplied IPv4 policy.
var ErrDestinationPolicy = errors.New("invalid network destination policy")

// destinationPolicy is immutable after network construction. A nil policy is
// reserved for the explicitly selected legacy, unrestricted user backend.
type destinationPolicy struct {
	internet       bool
	allowed        []netip.Prefix
	interfaceAddrs func() ([]net.Addr, error)
}

// ValidateDestinationPolicy validates the public-internet or allowlist policy.
// Empty selects internet. The allowlist is at most 64 comma-separated canonical
// IPv4 CIDRs; a default route is rejected to require explicit destination ranges.
func ValidateDestinationPolicy(policy, allowedCIDRs string) error {
	_, err := parseDestinationPolicy(policy, allowedCIDRs)

	return err
}

func parseDestinationPolicy(policy, allowedCIDRs string) (*destinationPolicy, error) {
	if policy != "" && policy != "internet" && policy != "allowlist" {
		return nil, fmt.Errorf("%w: policy must be internet or allowlist", ErrDestinationPolicy)
	}
	result := &destinationPolicy{internet: policy != "allowlist", interfaceAddrs: net.InterfaceAddrs}
	if allowedCIDRs == "" {
		return result, nil
	}
	if len(allowedCIDRs) > 2048 {
		return nil, fmt.Errorf("%w: CIDR list is too long", ErrDestinationPolicy)
	}
	parts := strings.Split(allowedCIDRs, ",")
	if len(parts) > 64 {
		return nil, fmt.Errorf("%w: at most 64 allowed CIDRs", ErrDestinationPolicy)
	}
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() == 0 || prefix != prefix.Masked() {
			return nil, fmt.Errorf("%w: allowlist requires canonical IPv4 CIDRs more specific than /0", ErrDestinationPolicy)
		}
		result.allowed = append(result.allowed, prefix)
	}

	return result, nil
}

// NewIsolatedWithPolicy starts the managed IPv4 network with an explicit
// destination policy. Built-in DHCP/DNS remain available; only the DNS proxy
// may reach configured resolvers without an allowlist entry. Gateway service
// aliases are checked after translation, so host loopback requires 127.0.0.1/32.
func NewIsolatedWithPolicy(policy, allowedCIDRs string) (*Network, error) {
	parsed, err := parseDestinationPolicy(policy, allowedCIDRs)
	if err != nil {
		return nil, err
	}
	config, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		return nil, err
	}
	servers := make([]string, 0, len(config.Servers))
	for _, server := range config.Servers {
		servers = append(servers, net.JoinHostPort(server, config.Port))
	}

	return newNetworkConfigured(servers, parsed)
}

func newNetworkWithPolicy(servers []string, policy, allowedCIDRs string) (*Network, error) {
	parsed, err := parseDestinationPolicy(policy, allowedCIDRs)
	if err != nil {
		return nil, err
	}

	return newNetworkConfigured(servers, parsed)
}

func (p *destinationPolicy) permits(address netip.Addr) bool {
	// The forwarding transport is IPv4 only. Never accept IPv4-mapped IPv6
	// as an alternative spelling, nor route unspecified, multicast or reserved
	// class-E addresses, even when an operator includes them in a CIDR.
	if !address.Is4() {
		return false
	}
	bytes := address.As4()
	if bytes[0] == 0 || bytes[0] >= 224 {
		return false
	}
	if p == nil {
		return !address.IsLinkLocalUnicast()
	}
	for _, prefix := range p.allowed {
		if prefix.Contains(address) {
			return true
		}
	}
	if !p.internet || !publicIPv4(address) {
		return false
	}
	// A public destination can still be on a locally attached network. Query
	// each new flow so interface changes cannot silently open host/LAN services.
	// Discovery failure denies the flow; explicit CIDRs remain usable.
	addresses, err := p.interfaceAddrs()
	if err != nil {
		return false
	}
	for _, host := range addresses {
		if host == nil {
			return false
		}
		prefix, err := netip.ParsePrefix(host.String())
		if err != nil || prefix.Contains(address) {
			return false
		}
	}

	return true
}

// publicIPv4 conservatively excludes private, shared, link-local, documentation,
// benchmarking, protocol-assignment and deprecated transition ranges. Explicit
// operator CIDRs can permit selected unicast destinations in these ranges.
func publicIPv4(address netip.Addr) bool {
	bytes := address.As4()
	switch bytes[0] {
	case 0, 10, 127:
		return false
	case 100:
		return bytes[1] < 64 || bytes[1] > 127
	case 169:
		return bytes[1] != 254
	case 172:
		return bytes[1] < 16 || bytes[1] > 31
	case 192:
		return (bytes[1] != 0 || bytes[2] != 0 && bytes[2] != 2) &&
			bytes[1] != 168 && (bytes[1] != 88 || bytes[2] != 99)
	case 198:
		return bytes[1] != 18 && bytes[1] != 19 && (bytes[1] != 51 || bytes[2] != 100)
	case 203:
		return bytes[1] != 0 || bytes[2] != 113
	default:
		return bytes[0] < 224
	}
}
