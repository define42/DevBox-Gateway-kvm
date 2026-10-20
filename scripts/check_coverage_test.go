package scripts_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCheckCoverage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, profile, minimum, want string
		status                       int
	}{
		{
			name: "exact_threshold", minimum: "80",
			profile: "mode: set\nexample/a.go:1.1,2.1 8 1\nexample/b.go:3.1,4.1 2 0\n",
			want:    "80.000% (8/10 statements)",
		},
		{
			name: "weighted_statements_not_blocks", minimum: "80", status: 1,
			profile: "mode: count\nexample/a.go:1.1,2.1 1 100\nexample/b.go:3.1,4.1 9 0\n",
			want:    "10.000% (1/10 statements)",
		},
		{
			name: "rounding_must_not_pass", minimum: "80", status: 1,
			profile: "mode: atomic\nexample/a.go:1.1,2.1 79999 1\nexample/b.go:3.1,4.1 20001 0\n",
			want:    "79.999%",
		},
		{
			name: "duplicate_blocks_union", minimum: "80",
			profile: "mode: atomic\nexample/a.go:1.1,2.1 8 0\nexample/b.go:3.1,4.1 2 0\n" +
				"example/a.go:1.1,2.1 8 1\nexample/a.go:1.1,2.1 8 0\n",
			want: "80.000% (8/10 statements)",
		},
		{
			name: "all_paths_included", minimum: "80", status: 1,
			profile: "mode: set\nexample/app.go:1.1,2.1 8 1\nexample/pkg/h264/generated.go:1.1,2.1 3 0\n",
			want:    "72.727% (8/11 statements)",
		},
		{
			name: "conflicting_blocks", minimum: "80", status: 2,
			profile: "mode: set\nexample/a.go:1.1,2.1 8 1\nexample/a.go:1.1,2.1 9 1\n",
			want:    "inconsistent duplicate block",
		},
		{name: "empty", minimum: "80", status: 2, want: "no statements"},
		{name: "header_only", minimum: "80", profile: "mode: set\n", status: 2, want: "no statements"},
		{name: "invalid_header", minimum: "80", profile: "mode: unknown\n", status: 2, want: "invalid profile header"},
		{
			name: "malformed_block", minimum: "80", status: 2,
			profile: "mode: set\nexample/a.go:1.1,2.1 8 nope\n", want: "invalid block",
		},
		{
			name: "negative_count", minimum: "80", status: 2,
			profile: "mode: set\nexample/a.go:1.1,2.1 8 -1\n", want: "invalid block",
		},
		{name: "invalid_minimum", minimum: "bad", status: 2, want: "minimum must be"},
		{name: "out_of_range_minimum", minimum: "101", status: 2, want: "minimum must be"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			//nolint:gosec // Fixed test-case arguments are passed directly to awk, never through a shell.
			cmd := exec.Command("awk", "-v", "minimum="+tc.minimum, "-f", "check-coverage.awk")
			cmd.Stdin = strings.NewReader(tc.profile)
			output, err := cmd.CombinedOutput()
			if cmd.ProcessState == nil {
				t.Fatalf("start coverage check: %v", err)
			}
			if got := cmd.ProcessState.ExitCode(); got != tc.status {
				t.Fatalf("exit = %d, want %d: %v\n%s", got, tc.status, err, output)
			}
			if !strings.Contains(string(output), tc.want) {
				t.Fatalf("output = %q, want %q", output, tc.want)
			}
		})
	}
}
