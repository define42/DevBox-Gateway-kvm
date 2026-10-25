package gateway

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerBundledFirmware(t *testing.T) {
	t.Parallel()
	manager, err := NewManager(managerTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	for _, name := range []string{"OVMF_CODE_4M.fd", "OVMF_VARS_4M.fd", "LICENSE"} {
		want, err := bundledFirmware.ReadFile("firmware/" + name)
		if err != nil || len(want) == 0 {
			t.Fatalf("bundled firmware asset %s is missing: %v", name, err)
		}
		if name != "LICENSE" && (len(want) < 4096 || len(want)%4096 != 0 || string(want[40:44]) != "_FVH") {
			t.Fatalf("bundled %s is not a page-aligned firmware volume", name)
		}
		assertFirmwareFile(t, filepath.Join(manager.config.DataDir, "firmware", name), want)
	}
}

func TestManagerBundledFirmwarePreservesVMVariables(t *testing.T) {
	t.Parallel()
	config := managerTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	first := createManagerVM(t, manager, "alice", "first")
	second := createManagerVM(t, manager, "alice", "second")
	pristine, err := bundledFirmware.ReadFile("firmware/OVMF_VARS_4M.fd")
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Clone(pristine)
	changed[len(changed)-1] ^= 0xff
	firstVars := filepath.Join(manager.directory(first.ID), "vars.fd")
	if err := os.WriteFile(firstVars, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	secondVars := filepath.Join(manager.directory(second.ID), "vars.fd")
	assertFirmwareFile(t, secondVars, pristine)
	assertFirmwareFile(t, manager.firmwarePath(bundledVarsName), pristine)
	for _, path := range []string{manager.firmwarePath(bundledCodeName), manager.firmwarePath(bundledVarsName)} {
		if err := os.WriteFile(path, []byte("corrupted cache"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if other, err := NewManager(config); err == nil {
		_ = other.Close()
		t.Fatal("second manager acquired the firmware cache while state was locked")
	}
	assertFirmwareFile(t, manager.firmwarePath(bundledCodeName), []byte("corrupted cache"))
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	code, err := bundledFirmware.ReadFile("firmware/OVMF_CODE_4M.fd")
	if err != nil {
		t.Fatal(err)
	}
	assertFirmwareFile(t, reopened.firmwarePath(bundledCodeName), code)
	assertFirmwareFile(t, reopened.firmwarePath(bundledVarsName), pristine)
	assertFirmwareFile(t, firstVars, changed)
	assertFirmwareFile(t, secondVars, pristine)
}

func TestManagerFirmwareCacheFailureReleasesLock(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"symlink directory", "public directory", "regular file", "asset directory"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := managerTestConfig(t)
			if err := os.Mkdir(config.DataDir, 0o700); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(config.DataDir, "firmware")
			outside := t.TempDir()
			var setupErr error
			switch name {
			case "symlink directory":
				setupErr = os.Symlink(outside, dir)
			case "public directory":
				setupErr = os.Mkdir(dir, 0o700)
				if setupErr == nil {
					setupErr = os.Chmod(dir, 0o755)
				}
			case "regular file":
				setupErr = os.WriteFile(dir, []byte("not a directory"), 0o600)
			case "asset directory":
				setupErr = os.MkdirAll(filepath.Join(dir, "OVMF_CODE_4M.fd"), 0o700)
			}
			if setupErr != nil {
				t.Fatal(setupErr)
			}
			if manager, err := NewManager(config); err == nil {
				_ = manager.Close()
				t.Fatal("invalid firmware cache was accepted")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("firmware extraction wrote outside private state: %v", err)
			}
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
			manager, err := NewManager(config)
			if err != nil {
				t.Fatalf("manager failed after repairing cache: %v", err)
			}
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func assertFirmwareFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("firmware contents differ for %s", path)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("firmware file %s must be private (0600): %v", path, err)
	}
}
