//go:build linux && amd64

package workersandbox

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"unsafe"

	"golang.org/x/sys/unix"
)

// LockRuntime is called after opening inherited control/profiling sockets and
// before guest initialization. TSYNC covers existing Go and native threads.
// Landlock already covers them all because it was inherited across Exec.
func LockRuntime() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	value, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNotConfined, err)
	}
	if value != 1 {
		return ErrNotConfined
	}
	for _, limit := range []struct {
		resource int
		value    uint64
	}{
		{unix.RLIMIT_NOFILE, 4096}, {unix.RLIMIT_CORE, 0},
	} {
		var inherited unix.Rlimit
		if err := unix.Getrlimit(limit.resource, &inherited); err != nil {
			return err
		}
		value := min(limit.value, inherited.Max)
		if err := unix.Setrlimit(limit.resource, &unix.Rlimit{Cur: value, Max: value}); err != nil {
			return fmt.Errorf("set worker resource limit: %w", err)
		}
	}
	debug.SetMaxThreads(512)
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("disable worker dumps: %w", err)
	}
	filter := runtimeFilter(uint32(os.Getpid()))
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	result, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER,
		unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program)))
	runtime.KeepAlive(filter)
	if errno != 0 || result != 0 {
		return fmt.Errorf("synchronize worker syscall restrictions (thread %d): %w", result, errno)
	}

	return nil
}

func runtimeFilter(pid uint32) []unix.SockFilter {
	load := func(offset uint32) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: offset}
	}
	equal := func(value uint32, yes, no uint8) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: value, Jt: yes, Jf: no}
	}
	bits := func(value uint32, yes, no uint8) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, K: value, Jt: yes, Jf: no}
	}
	ret := func(value uint32) unix.SockFilter { return unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: value} }
	deny := ret(unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM))
	allow := ret(unix.SECCOMP_RET_ALLOW)
	filter := []unix.SockFilter{
		load(4), equal(unix.AUDIT_ARCH_X86_64, 1, 0), ret(unix.SECCOMP_RET_KILL_PROCESS),
		load(0), bits(0x40000000, 0, 1), ret(unix.SECCOMP_RET_KILL_PROCESS), // reject x32 syscall ABI
	}
	for _, number := range []uint32{
		unix.SYS_EXECVE, unix.SYS_EXECVEAT, unix.SYS_FORK, unix.SYS_VFORK,
		unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV,
		unix.SYS_PIDFD_GETFD, unix.SYS_PROCESS_MADVISE, unix.SYS_PROCESS_MRELEASE,
		unix.SYS_KCMP, unix.SYS_BPF, unix.SYS_PERF_EVENT_OPEN, unix.SYS_USERFAULTFD,
		unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER,
		unix.SYS_UNSHARE, unix.SYS_SETNS, unix.SYS_MOUNT, unix.SYS_UMOUNT2,
		unix.SYS_PIVOT_ROOT, unix.SYS_CHROOT, unix.SYS_OPEN_BY_HANDLE_AT,
		unix.SYS_FSOPEN, unix.SYS_FSCONFIG, unix.SYS_FSMOUNT, unix.SYS_FSPICK,
		unix.SYS_OPEN_TREE, unix.SYS_MOVE_MOUNT, unix.SYS_MOUNT_SETATTR,
		unix.SYS_KEYCTL, unix.SYS_ADD_KEY, unix.SYS_REQUEST_KEY,
		unix.SYS_INIT_MODULE, unix.SYS_FINIT_MODULE, unix.SYS_DELETE_MODULE,
		unix.SYS_KEXEC_LOAD, unix.SYS_KEXEC_FILE_LOAD, unix.SYS_REBOOT,
		unix.SYS_SOCKETPAIR, unix.SYS_SHMGET, unix.SYS_SHMAT, unix.SYS_SHMCTL,
		unix.SYS_MSGGET, unix.SYS_MSGSND, unix.SYS_MSGRCV, unix.SYS_MSGCTL,
		unix.SYS_SEMGET, unix.SYS_SEMOP, unix.SYS_SEMTIMEDOP, unix.SYS_SEMCTL,
		unix.SYS_CHMOD, unix.SYS_FCHMOD, unix.SYS_FCHMODAT, unix.SYS_FCHMODAT2,
		unix.SYS_CHOWN, unix.SYS_FCHOWN, unix.SYS_FCHOWNAT, unix.SYS_LCHOWN,
		unix.SYS_SETXATTR, unix.SYS_LSETXATTR, unix.SYS_FSETXATTR,
		unix.SYS_REMOVEXATTR, unix.SYS_LREMOVEXATTR, unix.SYS_FREMOVEXATTR,
		unix.SYS_SETXATTRAT, unix.SYS_REMOVEXATTRAT,
		unix.SYS_UTIME, unix.SYS_UTIMES, unix.SYS_FUTIMESAT, unix.SYS_UTIMENSAT,
		unix.SYS_SCHED_SETAFFINITY, unix.SYS_SCHED_SETPARAM, unix.SYS_SCHED_SETSCHEDULER,
		unix.SYS_SCHED_SETATTR, unix.SYS_SETPRIORITY, unix.SYS_IOPRIO_SET,
	} {
		filter = append(filter, equal(number, 0, 1), deny)
	}
	// libc falls back to clone for thread creation on ENOSYS. The clone3
	// argument structure cannot be safely inspected by classic seccomp BPF.
	filter = append(filter, equal(unix.SYS_CLONE3, 0, 1), ret(unix.SECCOMP_RET_ERRNO|uint32(unix.ENOSYS)))
	filter = append(filter,
		equal(unix.SYS_CLONE, 0, 6), load(16),
		bits(unix.CLONE_NEWUSER|unix.CLONE_NEWNET|unix.CLONE_NEWNS|unix.CLONE_NEWPID|
			unix.CLONE_NEWIPC|unix.CLONE_NEWUTS|unix.CLONE_NEWCGROUP, 0, 1), deny,
		bits(unix.CLONE_THREAD, 1, 0), deny, allow,
	)
	// Do not permit a compromised worker to lower a sibling's resource limits.
	filter = append(filter, equal(unix.SYS_PRLIMIT64, 0, 5), load(16), equal(0, 2, 0), equal(pid, 1, 0), deny, allow)
	// Unix sockets are inherited or opened before this filter. Blocking new ones
	// also closes the pathname-socket gap in Landlock ABIs before 9.
	filter = append(filter,
		equal(unix.SYS_SOCKET, 0, 15), load(16),
		equal(unix.AF_INET, 3, 0), equal(unix.AF_INET6, 2, 0), equal(unix.AF_NETLINK, 7, 0), deny,
		load(24), unix.SockFilter{Code: unix.BPF_ALU | unix.BPF_AND | unix.BPF_K, K: 0xf},
		equal(unix.SOCK_STREAM, 2, 0), equal(unix.SOCK_DGRAM, 1, 0), deny, allow,
		load(32), equal(unix.NETLINK_ROUTE, 1, 0), deny, allow,
		allow,
	)

	return filter
}
