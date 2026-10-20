package flag_test

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

//nolint:paralleltest // Verifies environment defaults and explicit flag precedence.
func TestBootGPU3DConfiguration(t *testing.T) {
	for _, test := range []struct {
		name string
		env  string
		args []string
		want bool
	}{
		{name: "disabled default"},
		{name: "enabled environment", env: "true", want: true},
		{name: "disabled environment", env: "false"},
		{name: "trimmed environment", env: "  true ", want: true},
		{name: "malformed environment disables", env: "invalid"},
		{name: "flag disables environment", env: "true", args: []string{"-gpu-3d=false"}},
		{name: "flag enables environment", env: "false", args: []string{"-gpu-3d"}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("VM_GPU_3D", test.env)
			args, _, err := flag.ParseArgs(append([]string{"devbox-kvm", "boot"}, test.args...))
			if err != nil {
				t.Fatal(err)
			}
			if args.GPU3D != test.want {
				t.Fatalf("GPU3D = %t, want %t", args.GPU3D, test.want)
			}
		})
	}
}
