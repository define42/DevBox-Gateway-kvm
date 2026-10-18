//go:build legacyboot

package machine_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/machine"
	"github.com/define42/DevBox-Gateway-kvm/internal/pvh"
)

// Legacy direct-kernel and PVH smoke tests use fixtures prepared by make test-legacy.
// syncBuf is a thread-safe bytes.Buffer for capturing
// serial console output from the guest VM.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

const guestNetworkTimeout = 5 * time.Minute

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// waitForPing polls ping every 2s until it succeeds.
func waitForPing(t *testing.T, ip string) {
	t.Helper()

	deadline := time.Now().Add(guestNetworkTimeout)

	for {
		out, err := exec.Command(
			"ping", ip, "-c", "1", "-W", "1",
		).CombinedOutput()

		if err == nil {
			t.Logf("ping succeeded: %s", out)

			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("ping %s timed out after %s: %s",
				ip, guestNetworkTimeout, out)
		}

		time.Sleep(2 * time.Second)
	}
}

// waitForHTTP polls curl every 2s until the response body equals expected.
func waitForHTTP(
	t *testing.T, url, expected string,
) string {
	t.Helper()

	deadline := time.Now().Add(guestNetworkTimeout)

	attempt := 0

	for {
		attempt++

		out, err := exec.Command(
			"curl", "-sSfL", url,
		).CombinedOutput()

		body := string(out)

		if err == nil && body == expected {
			t.Logf("curl matched on attempt %d", attempt)

			return body
		}

		if err == nil {
			t.Logf("curl attempt %d: unexpected body: %q",
				attempt, body)
		} else {
			t.Logf("curl attempt %d: %s (%v)",
				attempt, out, err)
		}

		if time.Now().After(deadline) {
			t.Fatalf(
				"curl %s timed out after %d attempts"+
					" (%s): last body=%q err=%v",
				url, attempt, guestNetworkTimeout, body, err)
		}

		time.Sleep(2 * time.Second)
	}
}

// copyVDAImg copies ../../vda.img to a temp file so each
// test gets its own disk image, preventing cross-test
// corruption.
func copyVDAImg(t *testing.T) string {
	t.Helper()

	src, err := os.Open("../../vda.img")
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	dst, err := os.CreateTemp("",
		"vda-"+filepath.Base(t.Name())+"-*.img")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { os.Remove(dst.Name()) })

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		t.Fatal(err)
	}

	dst.Close()

	return dst.Name()
}

func testNewAndLoadLinux(t *testing.T, kernel, tap, guestIPv4, hostIPv4, prefixLen string) { // nolint:thelper
	if os.Getuid() != 0 {
		t.Skipf("Skipping test since we are not root")
	}

	m, err := machine.New("/dev/kvm", 1, 1<<29)
	if err != nil {
		t.Fatal(err)
	}

	var serialBuf syncBuf

	t.Cleanup(func() {
		m.Close()
		t.Logf("=== serial console output ===\n%s",
			serialBuf.String())
	})

	if err := m.AddTapIf(tap); err != nil {
		t.Fatal(err)
	}

	vdaPath := copyVDAImg(t)
	if err := m.AddDisk(vdaPath); err != nil {
		t.Fatal(err)
	}

	param := fmt.Sprintf(`console=ttyS0 earlyprintk=serial noapic noacpi nosmp `+
		`lapic pci=realloc=off virtio_pci.force_legacy=1 `+
		`rdinit=/init init=/init gokvm.ipv4_addr=%s/%s`, guestIPv4, prefixLen)

	kern, err := os.Open(kernel)
	if err != nil {
		t.Fatal(err)
	}

	isPVH, err := pvh.CheckPVH(kern)
	if err != nil {
		t.Fatal(err)
	}

	if isPVH {
		param += " tsc=reliable"
	}

	initrd, err := os.Open("../../initrd")
	if err != nil {
		t.Fatal(err)
	}

	if isPVH {
		if err := m.LoadPVH(kern, initrd, param); err != nil {
			t.Fatal(err)
		}
	} else {
		if err = m.LoadLinux(kern, initrd, param); err != nil {
			t.Fatal(err)
		}
	}

	m.GetSerial().SetOutput(&serialBuf)
	m.GetInputChan()

	if err := m.InjectSerialIRQ(); err != nil {
		t.Errorf("m.InjectSerialIRQ: %v != nil", err)
	}

	m.RunData()

	// Deadline watchdog: if the test deadline is
	// approaching (60s margin), log serial output and
	// close the VM so we get diagnostics instead of
	// a bare timeout panic.
	if dl, ok := t.Deadline(); ok {
		go func() {
			margin := 60 * time.Second

			timer := time.NewTimer(
				time.Until(dl) - margin)
			defer timer.Stop()

			<-timer.C
			t.Errorf("deadline watchdog fired "+
				"(60s before %s)\nserial:\n%s",
				dl.Format(time.RFC3339),
				serialBuf.String())
			m.Close()
		}()
	}

	go func() {
		if runErr := m.RunInfiniteLoop(0); runErr != nil {
			fmt.Printf("RunInfiniteLoop: %v\n", runErr)
		}
	}()

	if err := exec.Command("ip", "link", "set", tap, "up").Run(); err != nil {
		t.Fatal(err)
	}

	if err := exec.Command("ip", "addr", "add", hostIPv4+"/"+prefixLen, "dev", tap).Run(); err != nil { //nolint:gosec
		t.Fatal(err)
	}

	waitForPing(t, guestIPv4)
	t.Logf("ping OK at %s", time.Now().Format(time.RFC3339))

	const wantBody = "index.html: this message is " +
		"from /dev/vda in guest\n"

	output := waitForHTTP(t, fmt.Sprintf(
		"http://%s/mnt/dev_vda/index.html", guestIPv4),
		wantBody)

	if output != wantBody {
		t.Fatalf("unexpected response body: %q", output)
	}
}

func TestNewAndLoadLinuxWithBzImage(t *testing.T) { // nolint:paralleltest
	testNewAndLoadLinux(t, "../../bzImage", "tap0", "192.168.20.1", "192.168.20.2", "24")
}

func TestNewAndLoadLinuxWithVmlinux(t *testing.T) { // nolint:paralleltest
	testNewAndLoadLinux(t, "../../vmlinux", "tap1", "192.168.30.1", "192.168.30.2", "24")
}

func TestNewAndLoadLinuxPVH(t *testing.T) { // nolint:paralleltest
	testNewAndLoadLinux(t, "../../vmlinux_PVH", "tap2", "192.168.40.1", "192.168.40.2", "24")
}

func TestNewAndLoadEDK2PVH(t *testing.T) { // nolint:paralleltest
	if os.Getuid() != 0 {
		t.Skipf("Skipping test since we are not root")
	}

	m, err := machine.New("/dev/kvm", 1, 1<<30)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { m.Close() })

	edk2, err := os.Open("../../CLOUDHV.fd")
	if err != nil {
		t.Fatal(err)
	}

	if err := m.LoadPVH(edk2, nil, ""); err != nil {
		t.Fatal(err)
	}

	m.GetInputChan()

	if err := m.InjectSerialIRQ(); err != nil {
		t.Errorf("m.InjectSerialIRQ: %v != nil", err)
	}

	m.RunData()

	go func() {
		if runErr := m.RunInfiniteLoop(0); runErr != nil {
			fmt.Printf("RunInfiniteLoop: %v\n", runErr)
		}
	}()

	time.Sleep(15 * time.Second)
}
