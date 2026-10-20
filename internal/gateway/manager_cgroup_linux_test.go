package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxCgroupControlFiles(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	for name, value := range map[string]string{
		"memory.max": "123\n",
		"bounded":    strings.Repeat("x", maxCgroupControlBytes),
		"oversized":  strings.Repeat("x", maxCgroupControlBytes+1),
	} {
		if err := os.WriteFile(filepath.Join(path, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directory := openTestCgroupDirectory(t, path)
	if value, err := directory.ReadFile("memory.max"); err != nil || value != "123\n" {
		t.Fatalf("read control = %q, %v", value, err)
	}
	if err := directory.CheckWritable("memory.max"); err != nil {
		t.Fatal(err)
	}
	if err := directory.WriteFile("memory.max", "456\n"); err != nil {
		t.Fatal(err)
	}
	if value, err := os.ReadFile(filepath.Join(path, "memory.max")); err != nil || string(value) != "456\n" {
		t.Fatalf("write control = %q, %v", value, err)
	}
	if value, err := directory.ReadFile("bounded"); err != nil || len(value) != maxCgroupControlBytes {
		t.Fatalf("maximum control size = %d, %v", len(value), err)
	}
	if _, err := directory.ReadFile("oversized"); !errors.Is(err, errInvalidCgroupState) {
		t.Fatalf("oversized control = %v, want invalid state", err)
	}
	for _, operation := range []struct {
		name string
		run  func(string) error
	}{
		{name: "read", run: func(name string) error {
			_, err := directory.ReadFile(name)

			return err
		}},
		{name: "write", run: func(name string) error { return directory.WriteFile(name, "0") }},
		{name: "check writable", run: directory.CheckWritable},
	} {
		t.Run(operation.name+" missing", func(t *testing.T) {
			t.Parallel()
			if err := operation.run("missing"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing control = %v, want not exist", err)
			}
		})
	}
}

func TestLinuxCgroupRejectsUnsafeEntries(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(path, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	directory := openTestCgroupDirectory(t, path)
	for _, name := range []string{"", ".", "..", "../private", "nested/control", "space name"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := directory.ReadFile(name); !errors.Is(err, errInvalidCgroupState) {
				t.Errorf("ReadFile(%q) = %v", name, err)
			}
			if child, err := directory.CreateChild(name); !errors.Is(err, errInvalidCgroupState) {
				if child != nil {
					_ = child.Close()
				}
				t.Errorf("CreateChild(%q) = %v", name, err)
			}
			if child, err := directory.OpenChild(name, vmCgroupDirectoryInfo{}); !errors.Is(err, errInvalidCgroupState) {
				if child != nil {
					_ = child.Close()
				}
				t.Errorf("OpenChild(%q) = %v", name, err)
			}
			if err := directory.RemoveChild(name, vmCgroupDirectoryInfo{}); !errors.Is(err, errInvalidCgroupState) {
				t.Errorf("RemoveChild(%q) = %v", name, err)
			}
		})
	}
	if _, err := directory.ReadFile("directory"); !errors.Is(err, errInvalidCgroupState) {
		t.Fatalf("directory used as control = %v", err)
	}
	if _, err := directory.ReadFile("linked"); !errors.Is(err, unix.ELOOP) {
		t.Fatalf("symlink read = %v, want ELOOP", err)
	}
	if err := directory.WriteFile("linked", "changed"); !errors.Is(err, unix.ELOOP) {
		t.Fatalf("symlink write = %v, want ELOOP", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "secret" {
		t.Fatalf("external file changed: %q, %v", data, err)
	}
}

func TestLinuxCgroupChildIdentityAndCleanup(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	directory := openTestCgroupDirectory(t, path)
	child, err := directory.CreateChild("vm-original")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Close() })
	expected, err := child.Info()
	if err != nil {
		t.Fatal(err)
	}
	if expected.mode&unix.S_IFMT != unix.S_IFDIR || expected.mode&0o777 != 0o700 {
		t.Fatalf("child mode = %#o, want private directory", expected.mode)
	}
	if duplicate, err := directory.CreateChild("vm-original"); !errors.Is(err, os.ErrExist) {
		if duplicate != nil {
			_ = duplicate.Close()
		}
		t.Fatalf("duplicate creation = %v, want exist", err)
	}
	opened, err := directory.OpenChild("vm-original", expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(path, "vm-original"), filepath.Join(path, "retained")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, "vm-original"), 0o700); err != nil {
		t.Fatal(err)
	}
	if replacement, err := directory.OpenChild("vm-original", expected); !errors.Is(err, errCgroupPathChanged) {
		if replacement != nil {
			_ = replacement.Close()
		}
		t.Fatalf("replacement opened: %v", err)
	}
	if err := directory.RemoveChild("vm-original", expected); !errors.Is(err, errCgroupPathChanged) {
		t.Fatalf("replacement removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "vm-original")); err != nil {
		t.Fatalf("replacement was not preserved: %v", err)
	}
	if err := directory.RemoveChild("retained", expected); err != nil {
		t.Fatal(err)
	}
	if err := directory.RemoveChild("retained", expected); err != nil {
		t.Fatalf("repeated removal = %v", err)
	}
	if _, err := directory.OpenChild("retained", expected); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed child reopened: %v", err)
	}
	if err := os.Symlink("vm-original", filepath.Join(path, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := directory.OpenChild("linked", expected); !errors.Is(err, unix.ELOOP) {
		t.Fatalf("symlink child opened: %v", err)
	}
	if err := directory.RemoveChild("linked", expected); !errors.Is(err, errCgroupPathChanged) {
		t.Fatalf("symlink child removed: %v", err)
	}
}

func TestLinuxCgroupDescriptorPinsRootAndCloses(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	path := filepath.Join(base, "root")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "memory.max"), []byte("123"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := openTestCgroupDirectory(t, path)
	flags, err := unix.FcntlInt(uintptr(directory.FD()), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("root descriptor is inheritable: flags=%d, %v", flags, err)
	}
	if err := os.Rename(path, filepath.Join(base, "pinned")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), path); err != nil {
		t.Fatal(err)
	}
	if value, err := directory.ReadFile("memory.max"); err != nil || value != "123" {
		t.Fatalf("root replacement redirected read: %q, %v", value, err)
	}
	if err := directory.Close(); err != nil {
		t.Fatal(err)
	}
	if err := directory.Close(); err != nil {
		t.Fatalf("second close = %v", err)
	}
	if _, err := directory.Info(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed directory info = %v", err)
	}
	if _, err := directory.ReadFile("memory.max"); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed directory read = %v", err)
	}
	if err := directory.RemoveChild("missing", vmCgroupDirectoryInfo{}); !errors.Is(err, unix.EBADF) {
		t.Fatalf("closed directory removal = %v", err)
	}
}

func openTestCgroupDirectory(t *testing.T, path string) vmCgroupDirectory {
	t.Helper()
	directory, err := (linuxVMCgroupSystem{}).OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := directory.Close(); err != nil {
			t.Error(err)
		}
	})

	return directory
}
