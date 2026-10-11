package kvm_test

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/kvm"
)

func TestIoctlPointerSurvivesStackGrowth(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	if _, err := writer.Write([]byte{42}); err != nil {
		t.Fatal(err)
	}

	// Fresh goroutines and varying recursion depths exercise stack growth in
	// the ioctl wrappers. The kernel must write to the caller's live storage.
	var workers sync.WaitGroup
	for depth := range 100 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				pending, err := ioctlPendingBytesAtDepth(reader.Fd(), depth)
				if err != nil {
					t.Errorf("stack depth %d: %v", depth, err)

					return
				}
				if pending != 1 {
					t.Errorf("stack depth %d: FIONREAD returned %d, want 1", depth, pending)

					return
				}
			}
		}()
	}
	workers.Wait()
}

//go:noinline
func ioctlPendingBytesAtDepth(fd uintptr, depth int) (int32, error) {
	var padding [200]byte
	padding[0] = byte(depth)
	defer runtime.KeepAlive(&padding)
	if depth > 0 {
		return ioctlPendingBytesAtDepth(fd, depth-1)
	}

	pending := int32(-1)
	_, err := kvm.Ioctl(fd, syscall.TIOCINQ, uintptr(unsafe.Pointer(&pending)))

	return pending, err
}

func TestIoctlEINTRRetry(t *testing.T) {
	t.Parallel()

	devKVM, err := os.OpenFile(
		"/dev/kvm", os.O_RDWR, 0o644,
	)
	if err != nil {
		t.Fatal(err)
	}

	defer devKVM.Close()

	// KVM_GET_API_VERSION exercises the Ioctl retry loop.
	// It must succeed despite the EINTR-retry wrapper.
	_, err = kvm.GetAPIVersion(devKVM.Fd())
	if err != nil {
		t.Fatalf("GetAPIVersion failed: %v", err)
	}
}
