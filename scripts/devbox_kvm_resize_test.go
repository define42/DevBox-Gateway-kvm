package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuestResizePreferredMode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, query, want string
	}{
		{
			name: "changed_preferred",
			query: "Screen 0: current 1024 x 768\nVirtual-1 connected primary 1024x768+0+0\n" +
				"   1280x720 60.00 +\n   1024x768 60.00*\n",
			want: "--output Virtual-1 --mode 1280x720\n",
		},
		{
			name: "already_current",
			query: "Virtual-1 connected 1280x720+0+0\n   1280x720 60.00*+\n" +
				"   1024x768 60.00\n",
		},
		{
			name: "physical_output",
			query: "HDMI-1 connected 1024x768+0+0\n   1280x720 60.00+\n" +
				"   1024x768 60.00*\n",
		},
		{
			name: "physical_and_virtual",
			query: "Virtual-1 connected 1024x768+0+0\n   1280x720 60.00+\n" +
				"   1024x768 60.00*\nHDMI-1 connected 1920x1080+1024+0\n   1920x1080 60.00*+\n",
		},
		{
			name: "two_virtual_outputs",
			query: "Virtual-1 connected 1024x768+0+0\n   1280x720 60.00+\n" +
				"   1024x768 60.00*\nVirtual-2 connected 800x600+1024+0\n   800x600 60.00*+\n",
		},
		{
			name: "disconnected_physical",
			query: "Virtual-1 connected 1024x768+0+0\n   1280x800 59.81+\n" +
				"   1024x768 60.00*\nDP-1 disconnected\n",
			want: "--output Virtual-1 --mode 1280x800\n",
		},
		{
			name: "no_preferred_mode",
			query: "Virtual-1 connected 1024x768+0+0\n   1280x720 60.00\n" +
				"   1024x768 60.00*\n",
		},
		{
			name: "first_preferred_mode",
			query: "Virtual-1 connected 1024x768+0+0\n   1280x720 60.00+\n" +
				"   1280x800 60.00+\n   1024x768 60.00*\n",
			want: "--output Virtual-1 --mode 1280x720\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			taskDir := t.TempDir()
			writeResizeFixture(t, filepath.Join(taskDir, "query"), tc.query, 0o600)
			writeResizeFixture(t, filepath.Join(taskDir, "xrandr"), `#!/bin/sh
if [ "$1" = --query ]; then
    cat "$RESIZE_FIXTURE_DIR/query"
else
    printf '%s\n' "$*" >>"$RESIZE_FIXTURE_DIR/applied"
fi
`, 0o700)
			cmd := exec.Command("sh", "devbox-kvm-resize", "--once")
			cmd.Env = append(os.Environ(), "DISPLAY=:999", "RESIZE_FIXTURE_DIR="+taskDir,
				"PATH="+taskDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("resize helper: %v\n%s", err, output)
			}
			applied, err := os.ReadFile(filepath.Join(taskDir, "applied"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if string(applied) != tc.want {
				t.Fatalf("xrandr calls=%q, want%q", applied, tc.want)
			}
		})
	}
}

func TestGuestResizeRequiresX11Session(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("sh", "devbox-kvm-resize", "--once")
	cmd.Env = append(os.Environ(), "DISPLAY=")
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "DISPLAY is unset") {
		t.Fatalf("missing X11 session: %v, %s", err, output)
	}
}

func writeResizeFixture(t *testing.T, name, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
