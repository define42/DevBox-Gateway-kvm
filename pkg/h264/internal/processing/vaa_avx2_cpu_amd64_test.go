//go:build amd64

package processing

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/cpu"
)

// Exercise the exported AVX2 entry even when the actual runtime feature is
// disabled. A subprocess avoids mutating x/sys/cpu's process-wide flags.
func TestVAAAVX2DirectDisabledCPU(t *testing.T) {
	const marker = "H264_VAA_AVX2_DISABLED_CHILD"
	if os.Getenv(marker) == "1" {
		if cpu.X86.HasAVX2 {
			t.Fatal("GODEBUG did not disable AVX2")
		}
		TestVAAAVX2CompleteParity(t)
		TestVAAAVX2PartialFailureAndAliases(t)
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestVAAAVX2DirectDisabledCPU$", "-test.count=1")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GODEBUG=") && !strings.HasPrefix(entry, marker+"=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	debug := os.Getenv("GODEBUG")
	if debug != "" {
		debug += ","
	}
	cmd.Env = append(cmd.Env, "GODEBUG="+debug+"cpu.avx2=off", marker+"=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("AVX2-disabled direct caller: %v\n%s", err, output)
	}
}
