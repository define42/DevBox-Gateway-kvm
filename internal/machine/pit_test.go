package machine

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/kvm"
)

func TestPITChannel2Speaker(t *testing.T) {
	t.Parallel()
	m, err := New("/dev/kvm", 1, MinMemSize)
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
		t.Skipf("KVM unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	fd := m.vcpuFds[0]
	sregs, err := kvm.GetSregs(fd)
	if err != nil {
		t.Fatal(err)
	}
	sregs.CS.Base, sregs.CS.Selector, sregs.DS.Base, sregs.DS.Selector = 0, 0, 0, 0
	if err := kvm.SetSregs(fd, sregs); err != nil {
		t.Fatal(err)
	}

	// Port 0x61 exposes the PIT channel 2 gate (bit 0) and speaker data
	// (bit 1). Linux uses it to calibrate its timers. Exercise every bit
	// combination through guest I/O, which must stay inside KVM rather than
	// reach the userspace PS/2 fallback. Port 0x5000 is an explicit checkpoint.
	values := []byte{0, 3, 2, 1}
	var code []byte
	for i, value := range values {
		code = append(code,
			0xb0, value, // mov al, value
			0xe6, 0x61, // out 0x61, al
			0xe4, 0x61, // in al, 0x61
			0xa2, byte(i), 0x20, // mov [0x2000+i], al
			0xba, 0x00, 0x50, // mov dx, 0x5000
			0xee, // out dx, al
		)
	}
	copy(m.mem[0x1000:], code)
	if err := kvm.SetRegs(fd, &kvm.Regs{RIP: 0x1000, RFLAGS: 2}); err != nil {
		t.Fatal(err)
	}

	for i, value := range values {
		for {
			err := kvm.Run(fd)
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}

			break
		}
		run := m.runs[0]
		if kvm.ExitType(run.ExitReason) != kvm.EXITIO {
			t.Fatalf("speaker value %#x: exit %v, want checkpoint I/O", value, kvm.ExitType(run.ExitReason))
		}
		direction, size, port, count, _ := run.IO()
		if direction != kvm.EXITIOOUT || size != 1 || port != 0x5000 || count != 1 {
			t.Fatalf("speaker value %#x: unexpected I/O exit: direction %d, size %d, port %#x, count %d",
				value, direction, size, port, count)
		}
		if got := m.mem[0x2000+i] & 3; got != value {
			t.Errorf("speaker readback: got %#x, want %#x", got, value)
		}
		var pit kvm.PITState2
		if err := kvm.GetPIT2(m.vmFd, &pit); err != nil {
			t.Fatal(err)
		}
		if got, want := pit.Channels[2].Gate, value&1; got != want {
			t.Errorf("PIT channel 2 gate after writing %#x: got %d, want %d", value, got, want)
		}
	}
}
