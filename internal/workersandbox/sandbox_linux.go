//go:build linux

package workersandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Policy grants access to individual VM resources. No parent state directory or
// cgroup-management path is granted. Optional GPU access remains a trusted native
// driver boundary and is enabled only for workers configured to use it.
type Policy struct {
	ReadOnly  []string
	ReadWrite []string
	SocketDir string
	KVM       string
	GPU       bool
}

const (
	minABI       = 6 // Signal and abstract Unix socket scopes require Linux 6.12.
	readFile     = uint64(unix.LANDLOCK_ACCESS_FS_READ_FILE)
	readTree     = readFile | unix.LANDLOCK_ACCESS_FS_READ_DIR
	writeFile    = readFile | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE
	deviceAccess = readFile | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	// All filesystem rights defined through ABI 5, including rename and truncate.
	// Later ABIs do not weaken these restrictions.
	filesystemRights = uint64((1 << 16) - 1)
)

var (
	ErrUnavailable = errors.New("managed workers require Landlock ABI 6 or newer and seccomp")
	ErrRoot        = errors.New("managed VM workers must run as a non-root user")
	ErrNotConfined = errors.New("worker must enter its filesystem sandbox before boot")
)

type rulesetAttr struct {
	Filesystem uint64
	Network    uint64
	Scoped     uint64
}

// ABI returns the kernel Landlock ABI without changing process state.
func ABI() (int, error) {
	version, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0, fmt.Errorf("%w: %w", ErrUnavailable, errno)
	}
	if version < minABI {
		return int(version), fmt.Errorf("%w (found ABI %d)", ErrUnavailable, version)
	}

	return int(version), nil
}

// Environment deliberately excludes gateway credentials, VM_* overrides, loader
// injection variables and the inherited service environment. Arguments supply VM
// configuration. These display/driver settings contain no gateway secrets.
func Environment(parent []string) []string {
	result := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "GOTRACEBACK=none"}
	for _, entry := range parent {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case "LANG", "LC_ALL", "TZ", "NVIDIA_VISIBLE_DEVICES", "NVIDIA_DRIVER_CAPABILITIES":
			result = append(result, entry)
		}
	}

	return result
}

// Exec applies a Landlock domain on the calling OS thread, then replaces the
// process from that same thread. Exec discards other threads, avoiding the common
// mistake of sandboxing only one thread in a Go/CGO process. On any failure after
// locking, the caller must exit; the goroutine must never return to the scheduler.
func Exec(executable string, args, env []string, policy Policy) error {
	if os.Geteuid() == 0 {
		return ErrRoot
	}
	if _, err := ABI(); err != nil {
		return err
	}
	ruleset, err := makeRuleset(executable, policy)
	if err != nil {
		return err
	}
	defer unix.Close(ruleset)
	runtime.LockOSThread()
	// Do not unlock: this thread is either replaced by exec or discarded on exit.
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs: %w", err)
	}
	var capabilities [2]unix.CapUserData
	if err := unix.Capset(&unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}, &capabilities[0]); err != nil {
		return fmt.Errorf("drop worker capabilities: %w", err)
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return fmt.Errorf("clear ambient capabilities: %w", err)
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(ruleset), 0, 0); errno != 0 {
		return fmt.Errorf("restrict worker filesystem and IPC: %w", errno)
	}

	return unix.Exec(executable, args, env)
}

func makeRuleset(executable string, policy Policy) (int, error) {
	attr := rulesetAttr{
		Filesystem: filesystemRights,
		Scoped:     unix.LANDLOCK_SCOPE_SIGNAL | unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET,
	}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return -1, fmt.Errorf("create worker ruleset: %w", errno)
	}
	ruleset := int(fd)
	if err := addResources(ruleset, executable, policy); err != nil {
		_ = unix.Close(ruleset)

		return -1, err
	}

	return ruleset, nil
}

func addResources(ruleset int, executable string, policy Policy) error {
	if err := allowPath(ruleset, executable, readFile|unix.LANDLOCK_ACCESS_FS_EXECUTE, false); err != nil {
		return err
	}
	for _, path := range policy.ReadOnly {
		if path != "" {
			if err := allowPath(ruleset, path, readFile, false); err != nil {
				return err
			}
		}
	}
	for _, path := range policy.ReadWrite {
		if path != "" {
			if err := allowPath(ruleset, path, writeFile, false); err != nil {
				return err
			}
		}
	}
	if policy.KVM != "" {
		if err := allowPath(ruleset, policy.KVM, deviceAccess, false); err != nil {
			return err
		}
	}
	if policy.SocketDir != "" {
		// The manager supplies a dedicated empty profiling directory. No regular
		// file creation or access to the parent VM directory is permitted.
		access := uint64(unix.LANDLOCK_ACCESS_FS_READ_DIR | unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
			unix.LANDLOCK_ACCESS_FS_REMOVE_FILE)
		if err := allowPath(ruleset, policy.SocketDir, access, false); err != nil {
			return err
		}
	}
	// Dynamic libraries and their public configuration are needed by CGO, EGL
	// and NVENC. Library directories are read-only; the runtime filter forbids
	// executing additional programs after this one controlled re-exec.
	for _, path := range []string{"/lib", "/lib64", "/usr/lib", "/usr/lib64", "/usr/local/lib"} {
		if err := allowPath(ruleset, path, readTree|unix.LANDLOCK_ACCESS_FS_EXECUTE, true); err != nil {
			return err
		}
	}
	for _, path := range []string{
		"/etc/ld.so.cache", "/etc/ld.so.conf", "/etc/ld.so.conf.d", "/etc/resolv.conf",
		"/etc/hosts", "/etc/nsswitch.conf", "/etc/gai.conf", "/etc/localtime",
		"/usr/share/zoneinfo", "/usr/share/glvnd", "/usr/share/drirc.d",
		"/proc/self/statm", "/proc/self/maps", "/proc/meminfo", "/sys/devices/system/cpu",
	} {
		if err := allowPath(ruleset, path, readTree, true); err != nil {
			return err
		}
	}
	for _, path := range []string{"/dev/null", "/dev/zero", "/dev/urandom", "/dev/random"} {
		if err := allowPath(ruleset, path, deviceAccess, false); err != nil {
			return err
		}
	}
	if policy.GPU {
		for _, pattern := range []string{"/dev/dri/renderD*", "/dev/nvidia*", "/proc/driver/nvidia", "/sys/dev/char"} {
			paths, err := filepath.Glob(pattern)
			if err != nil {
				return err
			}
			for _, path := range paths {
				access := deviceAccess | unix.LANDLOCK_ACCESS_FS_READ_DIR
				if strings.HasPrefix(path, "/proc/") || strings.HasPrefix(path, "/sys/") {
					access = readTree
				}
				if err := allowPath(ruleset, path, access, false); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func allowPath(ruleset int, path string, access uint64, optional bool) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if optional && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open sandbox resource %q: %w", path, err)
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		access &^= unix.LANDLOCK_ACCESS_FS_READ_DIR
	}
	// The kernel struct is packed: uint64 allowed_access followed by int32 fd.
	attr := struct {
		Access uint64
		FD     int32
	}{Access: access, FD: int32(fd)}
	if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(ruleset), unix.LANDLOCK_RULE_PATH_BENEATH,
		uintptr(unsafe.Pointer(&attr)), 0, 0, 0); errno != 0 {
		return fmt.Errorf("allow sandbox resource %q: %w", path, errno)
	}

	return nil
}
