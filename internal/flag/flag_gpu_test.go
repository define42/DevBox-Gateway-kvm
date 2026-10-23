package flag_test

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

func TestBootRejectsRemovedGPUFlag(t *testing.T) {
	t.Parallel()
	for _, arg := range []string{"-gpu-3d", "-gpu-3d=false"} {
		if _, _, err := flag.ParseArgs([]string{"devbox-kvm", "boot", arg}); err == nil {
			t.Fatalf("removed 3D flag accepted: %s", arg)
		}
	}
}
