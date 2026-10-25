//go:build linux

package workersandbox

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestCheckSupport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		abi        int
		landlock   error
		seccomp    error
		wantText   string
		wantGuide  string
		wantCause  error
		checkCalls int
	}{
		{name: "minimum supported ABI", abi: 6, checkCalls: 1},
		{name: "newer ABI", abi: 9, checkCalls: 1},
		{
			name: "old ABI", abi: 5, landlock: ErrUnavailable,
			wantText:  "landlock ABI 5 is too old (need 6 or newer)",
			wantGuide: "boot Linux 6.12+ with Landlock enabled",
		},
		{
			name: "missing Landlock", landlock: fmt.Errorf("ABI query: %w", unix.ENOSYS),
			wantText: "landlock is unavailable or its syscall is blocked", wantCause: unix.ENOSYS,
			wantGuide: "Linux 6.12+ with Landlock enabled and allow its syscalls",
		},
		{
			name: "disabled Landlock", landlock: fmt.Errorf("%w: %w", ErrUnavailable, unix.EOPNOTSUPP),
			wantText: "landlock is disabled on this kernel", wantCause: unix.EOPNOTSUPP,
			wantGuide: "enable Landlock in the boot-time LSM configuration (Linux 6.12+ required)",
		},
		{
			name: "blocked Landlock query", landlock: unix.EPERM,
			wantText: "query landlock support", wantCause: unix.EPERM,
		},
		{
			name: "missing seccomp", abi: 6, seccomp: unix.ENOSYS,
			wantText: "seccomp syscall filtering is unavailable", wantCause: unix.ENOSYS, checkCalls: 1,
		},
		{
			name: "disabled seccomp filter", abi: 6, seccomp: unix.EINVAL,
			wantText: "seccomp syscall filtering is unavailable", wantCause: unix.EINVAL, checkCalls: 1,
		},
		{
			name: "unsupported seccomp action", abi: 6, seccomp: unix.EOPNOTSUPP,
			wantText: "seccomp syscall filtering is unavailable", wantCause: unix.EOPNOTSUPP, checkCalls: 1,
		},
		{
			name: "blocked seccomp", abi: 6, seccomp: fmt.Errorf("query: %w", unix.EACCES),
			wantText: "seccomp syscall filtering is unavailable", wantCause: unix.EACCES, checkCalls: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			err := checkSupport(func() (int, error) {
				return tc.abi, tc.landlock
			}, func() error {
				calls++

				return tc.seccomp
			})
			if calls != tc.checkCalls {
				t.Fatalf("seccomp checks = %d, want %d", calls, tc.checkCalls)
			}
			if tc.wantText == "" {
				if err != nil {
					t.Fatalf("supported kernel rejected: %v", err)
				}

				return
			}
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("error = %v, want ErrUnavailable", err)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("error = %q, want description %q", err, tc.wantText)
			}
			if tc.wantGuide != "" && !strings.Contains(err.Error(), tc.wantGuide) {
				t.Errorf("error = %q, want remediation %q", err, tc.wantGuide)
			}
			if tc.wantCause != nil && !errors.Is(err, tc.wantCause) {
				t.Errorf("error = %v, lost cause %v", err, tc.wantCause)
			}
		})
	}
}

// The query and intentionally invalid filter request must remain incapable of
// installing a filter, even if the kernel changes or reports unexpected success.
func TestCheckSeccompSupport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		actionErr unix.Errno
		filterErr unix.Errno
		wantErr   bool
		wantText  string
		wantCalls int
	}{
		{name: "supported filter", filterErr: unix.EFAULT, wantCalls: 2},
		{name: "missing syscall", actionErr: unix.ENOSYS, wantErr: true, wantCalls: 1},
		{name: "missing action", actionErr: unix.EOPNOTSUPP, wantErr: true, wantCalls: 1},
		{name: "filter disabled", filterErr: unix.EINVAL, wantErr: true, wantCalls: 2},
		{name: "filter blocked", filterErr: unix.EACCES, wantErr: true, wantCalls: 2},
		{
			name: "unexpected success", wantErr: true, wantCalls: 2,
			wantText: "seccomp NULL filter probe unexpectedly succeeded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			err := checkSeccompSupport(func(operation, flags uintptr, argument unsafe.Pointer) unix.Errno {
				calls++
				switch operation {
				case unix.SECCOMP_GET_ACTION_AVAIL:
					if calls != 1 || flags != 0 || argument == nil {
						t.Fatalf("invalid action query: call %d, flags %d, argument %p", calls, flags, argument)
					}
					if action := *(*uint32)(argument); action != unix.SECCOMP_RET_KILL_PROCESS {
						t.Fatalf("queried action = %#x, want KILL_PROCESS", action)
					}

					return tc.actionErr
				case unix.SECCOMP_SET_MODE_FILTER:
					if calls != 2 || flags != unix.SECCOMP_FILTER_FLAG_TSYNC || argument != nil {
						t.Fatalf("probe could change state: call %d, flags %d, argument %p", calls, flags, argument)
					}

					return tc.filterErr
				default:
					t.Fatalf("unexpected seccomp operation %d", operation)

					return unix.EINVAL
				}
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("probe error = %v, want error %t", err, tc.wantErr)
			}
			if tc.wantText != "" && !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("probe error = %q, want description %q", err, tc.wantText)
			}
			if tc.wantErr && errors.Is(err, unix.Errno(0)) {
				t.Errorf("failure wraps a successful syscall: %v", err)
			}
			if calls != tc.wantCalls {
				t.Errorf("probe calls = %d, want %d", calls, tc.wantCalls)
			}
			cause := tc.actionErr
			if cause == 0 {
				cause = tc.filterErr
			}
			if tc.wantErr && cause != 0 && !errors.Is(err, cause) {
				t.Errorf("probe error = %v, lost errno %v", err, cause)
			}
		})
	}
}
