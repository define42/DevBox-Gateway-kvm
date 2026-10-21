package flag_test

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

func TestDiskSizeOptions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		args    []string
		size    int64
		invalid bool
	}{
		{name: "no disk"},
		{name: "standalone capacity inspection", args: []string{"-d", "guest.qcow2"}},
		{name: "explicit capacity", args: []string{"-d", "guest.qcow2", "-disk-size-bytes=4096"}, size: 4096},
		{name: "negative capacity", args: []string{"-d", "guest.qcow2", "-disk-size-bytes=-1"}, invalid: true},
		{name: "capacity without disk", args: []string{"-disk-size-bytes=4096"}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			boot, _, err := flag.ParseArgs(append([]string{"devbox-kvm", "boot"}, tc.args...))
			if tc.invalid {
				if !errors.Is(err, flag.ErrDiskOptions) {
					t.Fatalf("error = %v, want invalid disk options", err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if boot.DiskSizeBytes != tc.size {
				t.Fatalf("disk capacity = %d, want %d", boot.DiskSizeBytes, tc.size)
			}
		})
	}
}

func TestDiskFormatOptionIsUnsupported(t *testing.T) {
	t.Parallel()
	_, _, err := flag.ParseArgs([]string{"devbox-kvm", "boot", "-d", "guest.img", "-disk-format=raw"})
	if err == nil {
		t.Fatal("primary raw format selection is still accepted")
	}
}
