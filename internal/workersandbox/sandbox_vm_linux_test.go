//go:build linux && amd64 && integration

package workersandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
	"github.com/define42/DevBox-Gateway-kvm/internal/profiling"
	"github.com/define42/DevBox-Gateway-kvm/internal/vmm"
)

const confinedKVMDiskSize = 1 << 20

func TestConfinedKVMLifecycle(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("worker intentionally rejects root")
	}
	if _, err := ABI(); err != nil {
		t.Skip(err)
	}
	device, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("KVM unavailable: %v", err)
	}
	device.Close()
	code := "/usr/share/OVMF/OVMF_CODE_4M.fd"
	if _, err := os.Stat(code); err != nil {
		t.Skipf("firmware unavailable: %v", err)
	}
	template, err := os.ReadFile("/usr/share/OVMF/OVMF_VARS_4M.fd")
	if err != nil {
		t.Skipf("firmware variables unavailable: %v", err)
	}
	dir := t.TempDir()
	diskPath := filepath.Join(dir, "disk.qcow2")
	vars, profiles := filepath.Join(dir, "vars.fd"), filepath.Join(dir, "profiles")
	image, err := disk.CreateQCOW2(diskPath, confinedKVMDiskSize)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(image.Sync(), image.Close()); err != nil {
		t.Fatal(err)
	}
	// #nosec G703 -- vars is inside the private test temporary directory.
	if err := os.WriteFile(vars, template, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(profiles, 0o700); err != nil {
		t.Fatal(err)
	}
	// #nosec G204 G702 -- Re-exec this test binary with private fixtures created above.
	cmd := exec.Command(os.Args[0], "-test.run=^TestConfinedKVMHelper$", "--", diskPath, vars, profiles, code)
	cmd.Env = append(os.Environ(), "DEVBOX_SANDBOX_KVM=launch", "GORACE=atexit_sleep_ms=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("confined VM: %v\n%s", err, output)
	} else {
		t.Logf("%s", output)
	}
}

//nolint:paralleltest // Helper re-execs and restricts its entire process.
func TestConfinedKVMHelper(t *testing.T) {
	stage := os.Getenv("DEVBOX_SANDBOX_KVM")
	if stage == "" {
		return
	}
	var paths []string
	for i, arg := range os.Args {
		if arg == "--" {
			paths = os.Args[i+1:]

			break
		}
	}
	if len(paths) != 4 {
		t.Fatal("missing fixture paths")
	}
	if stage == "launch" {
		policy := Policy{ReadOnly: []string{paths[3]}, ReadWrite: paths[:2], SocketDir: paths[2], KVM: "/dev/kvm"}
		env := append(Environment(os.Environ()), "DEVBOX_SANDBOX_KVM=confined", "GORACE=atexit_sleep_ms=0")
		if err := Exec(os.Args[0], os.Args, env, policy); err != nil {
			t.Fatal(err)
		}
		t.Fatal("exec returned")
	}
	stop, err := profiling.Start(t.Context(), filepath.Join(paths[2], "cpu.sock"))
	if err != nil {
		t.Fatalf("profiling: %v", err)
	}
	defer stop()
	if err := LockRuntime(); err != nil {
		t.Fatal(err)
	}
	// Reopening firmware, vars, disk and KVM after teardown exercises the reset
	// lifecycle; the same sandbox remains active throughout both instances.
	for range 2 {
		vm := vmm.New(vmm.Config{
			Dev: "/dev/kvm", NCPUs: 2, MemSize: 256 << 20, DiskSizeBytes: confinedKVMDiskSize,
			UEFI: paths[3], UEFIVars: paths[1], Disk: paths[0], Network: "user-isolated",
		})
		if err := vm.Init(); err != nil {
			t.Fatalf("VM init: %v", err)
		}
		if err := vm.Setup(); err != nil {
			vm.Close()
			t.Fatalf("VM setup: %v", err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
		err := vm.BootContext(ctx)
		cancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			vm.Close()
			t.Fatalf("VM run: %v", err)
		}
		if err := vm.Close(); err != nil {
			t.Fatalf("VM close: %v", err)
		}
	}
	fmt.Println("two confined KVM lifecycles completed")
}
