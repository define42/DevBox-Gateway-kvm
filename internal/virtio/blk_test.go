package virtio_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

// countingInjector counts InjectVirtioBlkIRQ calls.
type countingInjector struct {
	blkCount atomic.Int64
}

func (c *countingInjector) InjectVirtioNetIRQ() error {
	return nil
}

func (c *countingInjector) InjectVirtioBlkIRQ() error {
	c.blkCount.Add(1)

	return nil
}

func (c *countingInjector) InjectVirtioGPUIRQ() error {
	return nil
}

func TestBlkGetDeviceHeader(t *testing.T) {
	t.Parallel()

	v, err := virtio.NewBlk("/dev/zero", 9, &mockInjector{}, []byte{})
	if err != nil {
		t.Fatalf("err: %v\n", err)
	}

	hdr := v.GetDeviceHeader()

	// 0x1042 is the non-transitional (modern) virtio-blk device id.
	if hdr.DeviceID != 0x1042 {
		t.Fatalf("DeviceID: expected 0x1042, actual 0x%x", hdr.DeviceID)
	}

	if hdr.ClassCode != 0x01 || hdr.Subclass != 0x00 {
		t.Fatalf("class: got %#x/%#x, want 0x01/0x00", hdr.ClassCode, hdr.Subclass)
	}

	if hdr.Status&0x10 == 0 {
		t.Fatal("capabilities-list status bit not set")
	}

	if hdr.CapabilitiesPointer != 0x40 {
		t.Fatalf("CapabilitiesPointer: expected 0x40, actual 0x%x", hdr.CapabilitiesPointer)
	}
}

func TestBlkMMIOSize(t *testing.T) {
	t.Parallel()

	v, err := virtio.NewBlk("/dev/zero", 9, &mockInjector{}, []byte{})
	if err != nil {
		t.Fatalf("err: %v\n", err)
	}

	if sz := v.MMIOSize(); sz != 0x4000 {
		t.Fatalf("MMIOSize: expected 0x4000, actual 0x%x", sz)
	}

	if idx := v.MMIOBARIndex(); idx != 0 {
		t.Fatalf("MMIOBARIndex: expected 0, actual %d", idx)
	}
}

// TestBlkDeviceConfigCapacity verifies the device config exposes the capacity
// (in 512-byte sectors) of the backing file.
func TestBlkDeviceConfigCapacity(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp("", "blk-cap-*")
	if err != nil {
		t.Fatal(err)
	}

	defer os.Remove(f.Name())

	// 8 sectors worth of data.
	if _, err := f.Write(make([]byte, 8*virtio.SectorSize)); err != nil {
		t.Fatal(err)
	}

	f.Close()

	v, err := virtio.NewBlk(f.Name(), 10, &mockInjector{}, make([]byte, 0x1000))
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 8)
	v.ReadDeviceConfig(0, buf)

	capacity := uint64(0)
	for i := 0; i < 8; i++ {
		capacity |= uint64(buf[i]) << (8 * i)
	}

	if capacity != 8 {
		t.Fatalf("capacity: expected 8 sectors, got %d", capacity)
	}
}

func TestReadOnlyBlkRejectsWrites(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp("", "blk-ro-*")
	if err != nil {
		t.Fatal(err)
	}

	defer os.Remove(f.Name())

	if _, err := f.Write(make([]byte, 2*virtio.SectorSize)); err != nil {
		t.Fatal(err)
	}

	f.Close()

	mem := make([]byte, 0x10000)
	v, err := virtio.NewReadOnlyBlk(f.Name(), 10, &mockInjector{}, mem)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	const virtioBlkFeatureRO = uint64(1 << 5)
	if features := v.DeviceFeatures(); features&virtioBlkFeatureRO == 0 {
		t.Fatalf("DeviceFeatures: missing read-only bit in 0x%x", features)
	}

	q := newSplitQueue()
	q.Avail.Idx = 1

	q.Desc[0].Addr = 0x1000
	q.Desc[0].Len = 16
	q.Desc[0].Next = 1

	blkReq := (*virtio.BlkReq)(unsafe.Pointer(&mem[0x1000]))
	blkReq.Type = 1 // write
	blkReq.Sector = 0

	q.Desc[1].Addr = 0x2000
	q.Desc[1].Len = virtio.SectorSize
	q.Desc[1].Next = 2

	q.Desc[2].Addr = 0x3000
	q.Desc[2].Len = 1

	mem[0x3000] = 0
	v.VirtQueue[0] = q

	if err := v.IO(); err != nil {
		t.Fatal(err)
	}

	if mem[0x3000] != 1 {
		t.Fatalf("status: got %d, want VIRTIO_BLK_S_IOERR", mem[0x3000])
	}
}

func blockReadFixture(t *testing.T) (string, []byte) {
	t.Helper()

	// Only sector 2 is populated, so reading a different sector cannot pass.
	// Block I/O does not require a partition table or filesystem.
	expected := bytes.Repeat([]byte{0x13, 0x57, 0x9b, 0xdf}, virtio.SectorSize/4)
	data := make([]byte, 4*virtio.SectorSize)
	copy(data[2*virtio.SectorSize:], expected)

	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path, expected
}

func TestIO(t *testing.T) {
	t.Parallel()

	rawPath, expected := blockReadFixture(t)
	mem := make([]byte, 0x1000000)

	v, err := virtio.NewBlk(
		rawPath, 10, &mockInjector{}, mem,
	)
	if err != nil {
		t.Fatalf("err: %v\n", err)
	}

	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Errorf("close block device: %v", err)
		}
	})

	q := newSplitQueue()
	q.Avail.Idx = 1

	// desc[0]: blk request header
	q.Desc[0].Addr = 0
	q.Desc[0].Len = 16
	q.Desc[0].Next = 1

	blkReq := (*virtio.BlkReq)(unsafe.Pointer(&mem[0]))
	blkReq.Type = 0
	blkReq.Sector = 2

	// desc[1]: data buffer
	q.Desc[1].Addr = 0x400
	q.Desc[1].Len = 0x200
	q.Desc[1].Next = 2

	// desc[2]: status byte
	q.Desc[2].Addr = 0x700
	q.Desc[2].Len = 1

	mem[0x700] = 0xFF // poison status byte

	v.VirtQueue[0] = q

	if err := v.IO(); err != nil {
		t.Fatalf("err: %v\n", err)
	}

	if !v.IRQInjector.(*mockInjector).called {
		t.Fatalf("irqInjected = false\n")
	}

	// Verify status byte is VIRTIO_BLK_S_OK (0).
	if mem[0x700] != 0 {
		t.Fatalf("status: expected 0, got %d", mem[0x700])
	}

	actual := mem[0x400 : 0x400+virtio.SectorSize]
	if !bytes.Equal(expected, actual) {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}
}

func TestIOWithQCOW2(t *testing.T) {
	t.Parallel()

	rawPath, expected := blockReadFixture(t)
	qcow2Path := filepath.Join(t.TempDir(), "vda.qcow2")
	runQEMUImg(t, "convert", "-f", "raw", "-O", "qcow2", rawPath, qcow2Path)

	mem := make([]byte, 0x1000000)

	v, err := virtio.NewBlk(qcow2Path, 10, &mockInjector{}, mem)
	if err != nil {
		t.Fatalf("err: %v\n", err)
	}

	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Errorf("close block device: %v", err)
		}
	})

	q := newSplitQueue()
	q.Avail.Idx = 1

	// desc[0]: blk request header
	q.Desc[0].Addr = 0
	q.Desc[0].Len = 16
	q.Desc[0].Next = 1

	blkReq := (*virtio.BlkReq)(unsafe.Pointer(&mem[0]))
	blkReq.Type = 0
	blkReq.Sector = 2

	// desc[1]: data buffer
	q.Desc[1].Addr = 0x400
	q.Desc[1].Len = 0x200
	q.Desc[1].Next = 2

	// desc[2]: status byte
	q.Desc[2].Addr = 0x700
	q.Desc[2].Len = 1

	mem[0x700] = 0xFF // poison status byte

	v.VirtQueue[0] = q

	if err := v.IO(); err != nil {
		t.Fatalf("err: %v\n", err)
	}

	if !v.IRQInjector.(*mockInjector).called {
		t.Fatal("IRQ not injected")
	}

	if mem[0x700] != 0 {
		t.Fatalf("status: expected 0, got %d", mem[0x700])
	}

	actual := mem[0x400 : 0x400+virtio.SectorSize]
	if !bytes.Equal(expected, actual) {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}
}

func TestBlkIOStatusByte(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp("", "blk-test-*")
	if err != nil {
		t.Fatal(err)
	}

	defer os.Remove(f.Name())

	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i & 0xFF)
	}

	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}

	f.Close()

	mem := make([]byte, 0x100000)

	v, err := virtio.NewBlk(f.Name(), 10, &mockInjector{}, mem)
	if err != nil {
		t.Fatal(err)
	}

	q := newSplitQueue()
	q.Avail.Idx = 1

	// desc[0]: BlkReq header (16 bytes)
	q.Desc[0].Addr = 0x1000
	q.Desc[0].Len = 16
	q.Desc[0].Next = 1

	blkReq := (*virtio.BlkReq)(unsafe.Pointer(&mem[0x1000]))
	blkReq.Type = 0 // read
	blkReq.Sector = 0

	// desc[1]: data buffer (512 bytes)
	q.Desc[1].Addr = 0x2000
	q.Desc[1].Len = 512
	q.Desc[1].Next = 2

	// desc[2]: status byte
	q.Desc[2].Addr = 0x3000
	q.Desc[2].Len = 1

	mem[0x3000] = 0xB8 // poison status byte

	v.VirtQueue[0] = q

	if err := v.IO(); err != nil {
		t.Fatal(err)
	}

	// Status must be VIRTIO_BLK_S_OK (0).
	if mem[0x3000] != 0 {
		t.Fatalf("status: expected 0, got %d", mem[0x3000])
	}

	// Data buffer must contain file contents.
	if !bytes.Equal(mem[0x2000:0x2000+512], data[:512]) {
		t.Fatal("data mismatch")
	}

	// used ring Idx must be incremented.
	if q.Used.Idx != 1 {
		t.Fatalf("used.Idx: expected 1, got %d", q.Used.Idx)
	}

	if !v.IRQInjector.(*mockInjector).called {
		t.Fatal("IRQ not injected")
	}
}

func TestBlkClose(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp("", "blk-close-*")
	if err != nil {
		t.Fatal(err)
	}

	defer os.Remove(f.Name())
	f.Close()

	mem := make([]byte, 0x10000)

	v, err := virtio.NewBlk(f.Name(), 10, &mockInjector{}, mem)
	if err != nil {
		t.Fatal(err)
	}

	if err := v.Close(); err != nil {
		t.Fatalf("Close: got %v, want nil", err)
	}

	// Closing an already stopped device is harmless.
	if err := v.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestBlkIOThreadExitsOnClose(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp("", "blk-iothread-*")
	if err != nil {
		t.Fatal(err)
	}

	defer os.Remove(f.Name())
	f.Close()

	mem := make([]byte, 0x10000)

	v, err := virtio.NewBlk(f.Name(), 10, &mockInjector{}, mem)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()
		v.IOThreadEntry()
	}()

	if err := v.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("IOThreadEntry did not exit after Close")
	}
}

func TestBlkNotifyKick(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 0x10000)

	v, err := virtio.NewBlk("/dev/zero", 10, &mockInjector{}, mem)
	if err != nil {
		t.Fatal(err)
	}

	defer v.Close()

	// Notifying the queue twice must never block.
	for i := 0; i < 2; i++ {
		done := make(chan struct{})

		go func() {
			v.Notify(0)
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatalf("Notify #%d blocked", i)
		}
	}
}

func TestBlkNotifyAfterClose(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp("", "blk-wac-*")
	if err != nil {
		t.Fatal(err)
	}

	defer os.Remove(f.Name())
	f.Close()

	mem := make([]byte, 0x10000)

	v, err := virtio.NewBlk(f.Name(), 10, &mockInjector{}, mem)
	if err != nil {
		t.Fatal(err)
	}

	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	// Notify after Close must not panic.
	v.Notify(0)
}

func TestBlkConcurrentCloseAndNotify(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp("", "blk-ccw-*")
	if err != nil {
		t.Fatal(err)
	}

	defer os.Remove(f.Name())
	f.Close()

	mem := make([]byte, 0x10000)

	v, err := virtio.NewBlk(f.Name(), 10, &mockInjector{}, mem)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()
		v.Close()
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < 100; i++ {
			v.Notify(0)
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Close+Notify deadlocked")
	}
}

func TestBlkIOThreadReInjectsIRQ(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp("", "blk-reinject-*")
	if err != nil {
		t.Fatal(err)
	}

	defer os.Remove(f.Name())

	if _, err := f.Write(make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}

	f.Close()

	mem := make([]byte, 0x100000)
	inj := &countingInjector{}

	v, err := virtio.NewBlk(f.Name(), 10, inj, mem)
	if err != nil {
		t.Fatal(err)
	}

	q := newSplitQueue()
	q.Avail.Idx = 1

	q.Desc[0].Addr = 0x1000
	q.Desc[0].Len = 16
	q.Desc[0].Next = 1

	blkReq := (*virtio.BlkReq)(unsafe.Pointer(&mem[0x1000]))
	blkReq.Type = 0
	blkReq.Sector = 0

	q.Desc[1].Addr = 0x2000
	q.Desc[1].Len = 512
	q.Desc[1].Next = 2

	q.Desc[2].Addr = 0x3000
	q.Desc[2].Len = 1

	v.VirtQueue[0] = q

	if err := v.IO(); err != nil {
		t.Fatal(err)
	}

	// IO() injects once and leaves ISR set (no driver ack in this test).
	before := inj.blkCount.Load()

	// The IO thread's ticker should re-inject while ISR is still set.
	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()
		v.IOThreadEntry()
	}()

	time.Sleep(100 * time.Millisecond)

	after := inj.blkCount.Load()

	v.Close()
	wg.Wait()

	if reinjections := after - before; reinjections < 2 {
		t.Fatalf("expected >=2 re-injections, got %d", reinjections)
	}
}

func TestBlkIONilQueue(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 0x10000)

	v, err := virtio.NewBlk("/dev/zero", 10, &mockInjector{}, mem)
	if err != nil {
		t.Fatal(err)
	}

	// VirtQueue[0] is nil by default.
	if err := v.IO(); err == nil {
		t.Fatal("expected error for nil VirtQueue")
	}
}

func TestLoadU16StoreAddU16(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                 string
		initial, delta, want uint16
	}{
		{name: "zero", initial: 0, delta: 0, want: 0},
		{name: "increment", initial: 0, delta: 5, want: 5},
		{name: "wrap", initial: 65535, delta: 2, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			value := tc.initial
			if got := virtio.LoadU16(&value); got != tc.initial {
				t.Fatalf("initial: got %d, want %d", got, tc.initial)
			}
			virtio.StoreAddU16(&value, tc.delta)
			if got := virtio.LoadU16(&value); got != tc.want {
				t.Fatalf("after increment: got %d, want %d", got, tc.want)
			}
		})
	}
}

func runQEMUImg(t *testing.T, args ...string) {
	t.Helper()

	qemuImg, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img is not installed")
	}

	cmd := exec.Command(qemuImg, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("qemu-img %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
