package usernet

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

func TestDestinationPolicyValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, policy, cidrs string
		valid               bool
	}{
		{name: "safe default", valid: true},
		{name: "internet", policy: "internet", valid: true},
		{name: "deny forwarding", policy: "allowlist", valid: true},
		{name: "explicit LAN", policy: "internet", cidrs: "10.20.0.0/16, 192.168.5.1/32", valid: true},
		{name: "explicit host", policy: "allowlist", cidrs: "127.0.0.1/32", valid: true},
		{name: "unknown", policy: "unrestricted"},
		{name: "IPv6", cidrs: "::1/128"},
		{name: "mapped IPv6", cidrs: "::ffff:127.0.0.1/128"},
		{name: "hostname", cidrs: "localhost/32"},
		{name: "missing prefix", cidrs: "127.0.0.1"},
		{name: "host bits", cidrs: "10.0.0.1/24"},
		{name: "default route", cidrs: "0.0.0.0/0"},
		{name: "empty entry", cidrs: "10.0.0.0/8,"},
		{name: "only whitespace", cidrs: " "},
		{name: "too many entries", cidrs: strings.Repeat("10.0.0.0/8,", 64) + "10.0.0.0/8"},
		{name: "oversized input", cidrs: strings.Repeat("1", 2049)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateDestinationPolicy(tc.policy, tc.cidrs)
			if tc.valid && err != nil {
				t.Fatal(err)
			}
			if !tc.valid && !errors.Is(err, ErrDestinationPolicy) {
				t.Fatalf("invalid policy error = %v", err)
			}
		})
	}
}

func testDestinationPolicy(t *testing.T, mode, cidrs string) *destinationPolicy {
	t.Helper()
	policy, err := parseDestinationPolicy(mode, cidrs)
	if err != nil {
		t.Fatal(err)
	}
	policy.interfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.IPv4(8, 8, 4, 4), Mask: net.CIDRMask(24, 32)}}, nil
	}

	return policy
}

func TestDestinationPolicyRanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, address string
		public        bool
	}{
		{name: "public", address: "8.8.8.8", public: true},
		{name: "host public address", address: "8.8.4.4"},
		{name: "host public subnet", address: "8.8.4.5"},
		{name: "host subnet network", address: "8.8.4.0"},
		{name: "host subnet broadcast", address: "8.8.4.255"},
		{name: "before host subnet", address: "8.8.3.255", public: true},
		{name: "after host subnet", address: "8.8.5.0", public: true},
		{name: "private 10", address: "10.1.2.3"},
		{name: "private 172", address: "172.31.255.254"},
		{name: "private 192", address: "192.168.1.2"},
		{name: "shared", address: "100.64.1.2"},
		{name: "shared end", address: "100.127.255.254"},
		{name: "after shared", address: "100.128.0.1", public: true},
		{name: "before private 172", address: "172.15.255.254", public: true},
		{name: "after private 172", address: "172.32.0.1", public: true},
		{name: "loopback", address: "127.99.8.7"},
		{name: "metadata", address: "169.254.169.254"},
		{name: "unspecified", address: "0.0.0.0"},
		{name: "this network", address: "0.1.2.3"},
		{name: "limited broadcast", address: "255.255.255.255"},
		{name: "multicast", address: "224.0.0.1"},
		{name: "class E", address: "240.1.2.3"},
		{name: "protocol assignments", address: "192.0.0.8"},
		{name: "documentation 1", address: "192.0.2.1"},
		{name: "documentation 2", address: "198.51.100.1"},
		{name: "documentation 3", address: "203.0.113.1"},
		{name: "benchmarking", address: "198.18.0.1"},
		{name: "benchmarking end", address: "198.19.255.254"},
		{name: "deprecated transition", address: "192.88.99.1"},
		{name: "IPv6", address: "2606:4700:4700::1111"},
		{name: "IPv6 loopback", address: "::1"},
		{name: "mapped IPv6", address: "::ffff:8.8.8.8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			address := netip.MustParseAddr(tc.address)
			if got := testDestinationPolicy(t, "internet", "").permits(address); got != tc.public {
				t.Fatalf("internet permits %s = %t, want %t", address, got, tc.public)
			}
			if testDestinationPolicy(t, "allowlist", "").permits(address) {
				t.Fatalf("empty allowlist permits %s", address)
			}
		})
	}
}

func TestDestinationPolicyExplicitExceptions(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"internet", "allowlist"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			policy := testDestinationPolicy(t, mode, "192.168.10.0/24,127.0.0.1/32,169.254.169.254/32,8.8.4.4/31")
			for _, address := range []string{"192.168.10.7", "127.0.0.1", "169.254.169.254", "8.8.4.4", "8.8.4.5"} {
				if !policy.permits(netip.MustParseAddr(address)) {
					t.Errorf("explicit exception denied: %s", address)
				}
			}
			for _, address := range []string{
				"192.168.9.255", "192.168.11.0", "127.0.0.2", "169.254.169.253", "8.8.4.3", "8.8.4.6",
			} {
				if policy.permits(netip.MustParseAddr(address)) {
					t.Errorf("outside explicit exception allowed: %s", address)
				}
			}
		})
	}
	policy := testDestinationPolicy(t, "allowlist", "0.0.0.0/8,224.0.0.0/4,240.0.0.0/4")
	for _, address := range []string{"0.0.0.1", "224.0.0.1", "255.255.255.255"} {
		if policy.permits(netip.MustParseAddr(address)) {
			t.Errorf("non-unicast exception allowed: %s", address)
		}
	}
}

func TestDestinationPolicyHostAlias(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, cidrs string
		allowed     bool
	}{
		{name: "default denies host"},
		{name: "gateway CIDR does not authorize host", cidrs: "10.0.2.2/32"},
		{name: "guest subnet does not authorize host", cidrs: "10.0.2.0/24"},
		{name: "explicit host", cidrs: "127.0.0.1/32", allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id := stack.TransportEndpointID{LocalAddress: gatewayAddress, LocalPort: 3389}
			n := &Network{policy: testDestinationPolicy(t, "internet", tc.cidrs)}
			address, allowed := n.destination(id)
			if allowed != tc.allowed || (allowed && address != "127.0.0.1:3389") {
				t.Fatalf("alias = %q, %t; want allowed %t", address, allowed, tc.allowed)
			}
			id.LocalAddress = tcpip.AddrFrom4([4]byte{127, 0, 0, 1})
			if _, direct := n.destination(id); direct != tc.allowed {
				t.Fatal("direct loopback differs from gateway alias")
			}
		})
	}
}

func TestDestinationPolicyInterfaceDiscovery(t *testing.T) {
	t.Parallel()
	policy := testDestinationPolicy(t, "internet", "")
	address := netip.MustParseAddr("8.8.8.8")
	if !policy.permits(address) {
		t.Fatal("nonlocal public destination rejected")
	}
	policy.interfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.IPv4(8, 8, 8, 4), Mask: net.CIDRMask(24, 32)}}, nil
	}
	if policy.permits(address) {
		t.Fatal("newly attached host subnet allowed")
	}
	policy.interfaceAddrs = func() ([]net.Addr, error) { return nil, net.ErrClosed }
	if policy.permits(address) {
		t.Fatal("interface discovery failure allowed public destination")
	}
	policy.allowed = []netip.Prefix{netip.MustParsePrefix("8.8.8.8/32")}
	if !policy.permits(address) {
		t.Fatal("explicit exception unexpectedly requires interface discovery")
	}
}
