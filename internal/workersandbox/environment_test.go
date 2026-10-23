package workersandbox

import (
	"slices"
	"testing"
)

func TestEnvironmentExcludesServiceSecrets(t *testing.T) {
	t.Parallel()
	got := Environment([]string{
		"LDAP_PASSWORD=secret", "AUTH_FILE=/secret", "LD_PRELOAD=/evil.so",
		"VM_CGROUP_ROOT=/control", "LANG=C", "NVIDIA_VISIBLE_DEVICES=all",
		"NVIDIA_DRIVER_CAPABILITIES=graphics,utility",
	})
	want := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "GOTRACEBACK=none", "LANG=C"}
	if !slices.Equal(got, want) {
		t.Fatalf("worker environment = %v", got)
	}
}
