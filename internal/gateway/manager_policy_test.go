package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerNetworkDestinationPolicyArguments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, policy, cidrs string }{
		{name: "safe default", policy: "internet"},
		{name: "explicit LAN", policy: "internet", cidrs: "10.20.0.0/16"},
		{name: "explicit host only", policy: "allowlist", cidrs: "127.0.0.1/32"},
		{name: "no forwarding", policy: "allowlist"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, "graceful")
			if m.config.NetworkPolicy != "internet" || m.config.NetworkAllowCIDRs != "" {
				t.Fatal("programmatic manager defaults do not restrict host/LAN access")
			}
			m.config.NetworkPolicy, m.config.NetworkAllowCIDRs = tc.policy, tc.cidrs
			vm := createManagerVM(t, m, "alice", "desktop")
			if err := m.Start(t.Context(), vm.Owner, vm.ID); err != nil {
				t.Fatal(err)
			}
			policy, err := os.ReadFile(filepath.Join(m.directory(vm.ID), "helper.net-policy"))
			if err != nil {
				t.Fatal(err)
			}
			if string(policy) != tc.policy+"\n"+tc.cidrs {
				t.Fatalf("worker received policy %q, want %q and %q", policy, tc.policy, tc.cidrs)
			}
		})
	}
}

func TestManagerRejectsInvalidNetworkPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, policy, cidrs string }{
		{name: "unknown mode", policy: "unrestricted"},
		{name: "IPv6", cidrs: "::1/128"},
		{name: "default route", cidrs: "0.0.0.0/0"},
		{name: "host bits", cidrs: "10.20.0.1/16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := managerTestConfig(t)
			config.NetworkPolicy, config.NetworkAllowCIDRs = tc.policy, tc.cidrs
			m, err := NewManager(config)
			if m != nil {
				_ = m.Close()
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid policy accepted: %v", err)
			}
		})
	}
}
