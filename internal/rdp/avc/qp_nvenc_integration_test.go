//go:build nvenc && cgo && integration

package avc

import (
	"fmt"
	"os"
	"testing"
)

//nolint:paralleltest // GPU tests share limited hardware encoder sessions.
func TestNVENCHardwareQP(t *testing.T) {
	if os.Getenv("DEVBOX_KVM_TEST_NVENC") != "1" {
		t.Skip("set DEVBOX_KVM_TEST_NVENC=1 to test the NVIDIA GPU")
	}
	for _, qp := range []int{MinQP, DefaultQP, MaxQP} {
		t.Run(fmt.Sprintf("QP%d", qp), func(t *testing.T) {
			encoder, err := NewEncoderWithOptions(128, 96, Options{Threads: 1, Backend: BackendNVENC, QP: &qp})
			if err != nil {
				t.Fatal(err)
			}
			defer encoder.Close()
			if encoder.Backend() != BackendNVENC || encoder.QP() != qp {
				t.Fatalf("backend=%s QP=%d, want NVENC QP%d", encoder.Backend(), encoder.QP(), qp)
			}
			checkEncodedQP(t, qpTestStream(t, encoder), qp)
		})
	}
}
