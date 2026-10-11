package machine

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"syscall"
	"testing"

	"github.com/define42/gokvm/kvm"
)

func TestCPUIDTopologySingleSocket(t *testing.T) {
	t.Parallel()
	for _, count := range []int{1, 2, 3, 64} {
		t.Run(fmt.Sprintf("%d-cores", count), func(t *testing.T) {
			t.Parallel()
			checkCPUIDTopologySingleSocket(t, count)
		})
	}
}

func checkCPUIDTopologySingleSocket(t *testing.T, count int) {
	t.Helper()
	seen := make(map[uint32]bool)
	for cpu := range count {
		cpuid := topologyFixture()
		configureCPUIDTopology(&cpuid, cpu, count)
		id := checkCPUIDLegacyTopology(t, cpuid, cpu, count, seen)
		checkCPUIDExtendedTopology(t, cpuid, id, count)
		checkCPUIDAMDTopology(t, cpuid, id, count)
		checkCPUIDCacheTopology(t, cpuid, count)
	}
}

func checkCPUIDLegacyTopology(t *testing.T, cpuid kvm.CPUID, cpu, count int, seen map[uint32]bool) uint32 {
	t.Helper()
	legacy := topologyEntry(t, cpuid, 1, 0)
	id := legacy.Ebx >> 24
	if id != uint32(cpu) || seen[id] {
		t.Fatalf("CPU %d has duplicate or incorrect APIC ID %d", cpu, id)
	}
	seen[id] = true
	if got := (legacy.Ebx >> 16) & 0xff; got != uint32(count) {
		t.Fatalf("logical CPUs per socket: got %d, want %d", got, count)
	}
	if htt := legacy.Edx&(1<<28) != 0; htt != (count > 1) {
		t.Fatalf("HTT topology bit = %v for %d cores", htt, count)
	}

	return id
}

func checkCPUIDExtendedTopology(t *testing.T, cpuid kvm.CPUID, id uint32, count int) {
	t.Helper()
	leaves := []struct {
		function uint32
		endIndex uint32
	}{
		{function: 0xb, endIndex: 2},
		{function: 0x1f, endIndex: 2},
		{function: 0x80000026, endIndex: 4},
	}
	for _, leaf := range leaves {
		smt := topologyEntry(t, cpuid, leaf.function, 0)
		core := topologyEntry(t, cpuid, leaf.function, 1)
		end := topologyEntry(t, cpuid, leaf.function, leaf.endIndex)
		if smt.Eax != 0 || smt.Ebx != 1 || smt.Ecx != 0x100 || smt.Edx != id {
			t.Fatalf("leaf %#x advertises incorrect SMT topology: %+v", leaf.function, smt)
		}
		if core.Ebx != uint32(count) || core.Ecx != 0x201 || core.Edx != id {
			t.Fatalf("leaf %#x advertises incorrect core topology: %+v", leaf.function, core)
		}
		// The package shift must fit every core ID, without wasting
		// another bit (including non-power-of-two core counts).
		if uint32(count) > 1<<core.Eax || (core.Eax > 0 && uint32(count) <= 1<<(core.Eax-1)) ||
			id>>core.Eax != 0 {
			t.Fatalf("leaf %#x puts cores into multiple sockets: %+v", leaf.function, core)
		}
		if end.Eax != 0 || end.Ebx != 0 || end.Ecx != leaf.endIndex || end.Edx != id {
			t.Fatalf("leaf %#x has no valid topology terminator: %+v", leaf.function, end)
		}
		for index := uint32(2); index < leaf.endIndex; index++ {
			node := topologyEntry(t, cpuid, leaf.function, index)
			if node.Eax != core.Eax || node.Ebx != core.Ebx || node.Edx != id ||
				node.Ecx != (index+1)<<8|index {
				t.Fatalf("AMD CCD/socket does not contain exactly the guest cores: %+v", node)
			}
		}
	}
}

func checkCPUIDAMDTopology(t *testing.T, cpuid kvm.CPUID, id uint32, count int) {
	t.Helper()
	amd := topologyEntry(t, cpuid, 0x80000008, 0)
	if amd.Ecx&0xff != uint32(count-1) || id>>((amd.Ecx>>12)&0xf) != 0 {
		t.Fatalf("inconsistent AMD core count/socket ID: %+v", amd)
	}
	ext := topologyEntry(t, cpuid, 0x8000001e, 0)
	if ext.Eax != id || ext.Ebx&0xffff != id || ext.Ecx&0x7ff != 0 {
		t.Fatalf("inconsistent AMD APIC/core/node IDs: %+v", ext)
	}
}

func checkCPUIDCacheTopology(t *testing.T, cpuid kvm.CPUID, count int) {
	t.Helper()
	for _, function := range []uint32{4, 0x8000001d} {
		private := topologyEntry(t, cpuid, function, 0)
		shared := topologyEntry(t, cpuid, function, 1)
		end := topologyEntry(t, cpuid, function, 2)
		if (private.Eax>>14)&0xfff != 0 || (shared.Eax>>14)&0xfff != uint32(count-1) {
			t.Fatalf("leaf %#x has cache sharing outside the guest topology", function)
		}
		if function == 4 && (shared.Eax>>26)+1 != uint32(count) {
			t.Fatalf("cache leaf has wrong core count: %+v", shared)
		}
		if end.Eax != 0 {
			t.Fatalf("leaf %#x cache terminator was turned into a cache", function)
		}
	}
}

func TestCPUIDTopologyPreservesCapabilities(t *testing.T) {
	t.Parallel()
	before := topologyFixture()
	after := topologyFixture()
	configureCPUIDTopology(&after, 1, 2)
	for _, function := range []uint32{0, 7, 0x80000000, 0x80000001, 0x80000007, kvm.CPUIDSignature, kvm.CPUIDFeatures} {
		if got, want := topologyEntry(t, after, function, 0), topologyEntry(t, before, function, 0); got != want {
			t.Errorf("capability leaf %#x changed: got %+v, want %+v", function, got, want)
		}
	}
	oldLegacy, legacy := topologyEntry(t, before, 1, 0), topologyEntry(t, after, 1, 0)
	if legacy.Eax != oldLegacy.Eax || legacy.Ecx != oldLegacy.Ecx ||
		legacy.Edx&^(1<<28) != oldLegacy.Edx&^(1<<28) || legacy.Ebx&0xffff != oldLegacy.Ebx&0xffff {
		t.Fatal("legacy instruction capabilities or cache-line size changed")
	}
	oldAMD, amd := topologyEntry(t, before, 0x80000008, 0), topologyEntry(t, after, 0x80000008, 0)
	if amd.Eax != oldAMD.Eax || amd.Ebx != oldAMD.Ebx || amd.Edx != oldAMD.Edx ||
		amd.Ecx&^0xf0ff != oldAMD.Ecx&^0xf0ff {
		t.Fatal("AMD address sizes or instruction capabilities changed")
	}
	for _, function := range []uint32{4, 0x8000001d} {
		for index := range uint32(3) {
			oldCache := topologyEntry(t, before, function, index)
			cache := topologyEntry(t, after, function, index)
			if cache.Ebx != oldCache.Ebx || cache.Ecx != oldCache.Ecx || cache.Edx != oldCache.Edx ||
				cache.Eax&0x3fff != oldCache.Eax&0x3fff {
				t.Fatalf("cache geometry or capabilities changed for %#x/%d", function, index)
			}
		}
	}
}

func TestCPUIDTopologyDoesNotAdvertiseUnsupportedLeaves(t *testing.T) {
	t.Parallel()
	entries := []kvm.CPUIDEntry2{
		{Function: 0, Eax: 4},
		{Function: 1, Ebx: 0x7f200800},
		{Function: 0x80000001}, // AMD topology extensions are absent.
		{Function: 0x8000001e},
	}
	cpuid := kvm.CPUID{Nent: uint32(len(entries)), Entries: entries}
	configureCPUIDTopology(&cpuid, 1, 2)
	if len(cpuid.Entries) != len(entries) || topologyEntry(t, cpuid, 0, 0).Eax != 4 ||
		topologyEntry(t, cpuid, 0x8000001e, 0) != (kvm.CPUIDEntry2{Function: 0x8000001e}) {
		t.Fatal("unsupported topology capabilities were advertised")
	}
}

func TestCPUIDTopologyCompletesEmptyIndexedLeaf(t *testing.T) {
	t.Parallel()
	cpuid := kvm.CPUID{Nent: 3, Entries: []kvm.CPUIDEntry2{
		{Function: 0, Eax: 0x10},
		{Function: 1},
		{Function: 0xb, Flags: 1, Edx: 7},
	}}
	configureCPUIDTopology(&cpuid, 1, 2)
	if leaf := topologyEntry(t, cpuid, 0xb, 1); leaf.Eax != 1 || leaf.Ebx != 2 || leaf.Edx != 1 {
		t.Fatalf("missing core level for host's empty topology leaf: %+v", leaf)
	}
	before := append([]kvm.CPUIDEntry2(nil), cpuid.Entries...)
	configureCPUIDTopology(&cpuid, 1, 2)
	if !reflect.DeepEqual(cpuid.Entries, before) || int(cpuid.Nent) != len(before) {
		t.Fatal("reconfiguring topology duplicated entries or changed the topology")
	}
}

func TestCPUIDTopologyKVM(t *testing.T) {
	t.Parallel()
	dev, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		t.Skipf("KVM unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dev.Close() })
	vm, err := kvm.CreateVM(dev.Fd())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(int(vm)) })
	if err := kvm.CreateIRQChip(vm); err != nil {
		t.Fatal(err)
	}
	m := &Machine{kvmFd: dev.Fd(), vcpuFds: make([]uintptr, 2)}
	for cpu := range m.vcpuFds {
		fd, err := kvm.CreateVCPU(vm, cpu)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = syscall.Close(int(fd)) })
		m.vcpuFds[cpu] = fd
		if err := m.initCPUID(cpu); err != nil {
			t.Fatalf("CPU %d: %v", cpu, err)
		}
		cpuid := kvm.CPUID{Nent: 256, Entries: make([]kvm.CPUIDEntry2, 256)}
		if err := kvm.GetCPUID2(fd, &cpuid); err != nil {
			t.Fatal(err)
		}
		legacy := topologyEntry(t, cpuid, 1, 0)
		var lapic kvm.LAPICState
		if err := kvm.GetLocalAPIC(fd, &lapic); err != nil {
			t.Fatal(err)
		}
		if id := legacy.Ebx >> 24; id != uint32(cpu) || id != uint32(lapic.Regs[0x23]) {
			t.Fatalf("CPU %d CPUID APIC ID %d disagrees with LAPIC ID %d", cpu, id, lapic.Regs[0x23])
		}
		if (legacy.Ebx>>16)&0xff != 2 {
			t.Fatal("KVM did not retain the two-core topology")
		}
	}
}

func topologyEntry(t *testing.T, cpuid kvm.CPUID, function, index uint32) kvm.CPUIDEntry2 {
	t.Helper()
	for _, entry := range cpuid.Entries[:cpuid.Nent] {
		if entry.Function == function && entry.Index == index {
			return entry
		}
	}
	t.Fatalf("missing CPUID entry %#x/%d", function, index)

	return kvm.CPUIDEntry2{}
}

func topologyFixture() kvm.CPUID {
	entries := []kvm.CPUIDEntry2{
		{Function: 0, Eax: 0x1f, Ebx: 0x12345678, Ecx: 0x87654321, Edx: 0xabcdef},
		{Function: 1, Eax: 0xa20f10, Ebx: 0x7f200800, Ecx: 1, Edx: 0x078bfbff},
		{Function: 4, Index: 0, Eax: 63<<26 | 15<<14 | 1<<5 | 1, Ebx: 0x1c0003f, Ecx: 0x3f, Edx: 1},
		{Function: 4, Index: 1, Eax: 63<<26 | 31<<14 | 3<<5 | 3, Ebx: 0x3c0003f, Ecx: 0x1fff, Edx: 6},
		{Function: 4, Index: 2},
		{Function: 7, Eax: 2, Ebx: 0x1234, Ecx: 0x5678, Edx: 0x9abc},
		{Function: 0xb, Flags: 1, Eax: 1, Ebx: 2, Ecx: 0x100, Edx: 127},
		{Function: 0xb, Index: 1, Flags: 1, Eax: 6, Ebx: 64, Ecx: 0x201, Edx: 127},
		{Function: 0xb, Index: 2, Flags: 1, Ecx: 2, Edx: 127},
		{Function: 0x1f, Flags: 1, Eax: 1, Ebx: 2, Ecx: 0x100, Edx: 127},
		{Function: 0x1f, Index: 1, Flags: 1, Eax: 6, Ebx: 64, Ecx: 0x201, Edx: 127},
		{Function: 0x1f, Index: 2, Flags: 1, Eax: 7, Ebx: 128, Ecx: 0x502, Edx: 127},
		{Function: 0x1f, Index: 3, Flags: 1, Ecx: 3, Edx: 127},
		{Function: 0x80000000, Eax: 0x80000026},
		{Function: 0x80000001, Eax: 0xa20f10, Ecx: 1<<22 | 0x3f7, Edx: 0x2fd3fbff},
		{Function: 0x80000007},
		{Function: 0x80000008, Eax: 0x303030, Ebx: 0x1302d205, Ecx: 0x2000603f},
		{Function: 0x8000001d, Index: 0, Eax: 15<<14 | 1<<5 | 1, Ebx: 0x1c0003f, Ecx: 0x3f, Edx: 1},
		{Function: 0x8000001d, Index: 1, Eax: 31<<14 | 3<<5 | 3, Ebx: 0x3c0003f, Ecx: 0x1fff, Edx: 6},
		{Function: 0x8000001d, Index: 2},
		{Function: 0x8000001e, Eax: 127, Ebx: 0x90001f7f, Ecx: 0x80000003},
		{Function: 0x80000026, Flags: 1, Eax: 1, Ebx: 2, Ecx: 0x100, Edx: 127},
		{Function: 0x80000026, Index: 1, Flags: 1, Eax: 4, Ebx: 16, Ecx: 0x201, Edx: 127},
		{Function: 0x80000026, Index: 2, Flags: 1, Eax: 5, Ebx: 32, Ecx: 0x302, Edx: 127},
		{Function: 0x80000026, Index: 3, Flags: 1, Eax: 6, Ebx: 64, Ecx: 0x403, Edx: 127},
		{Function: 0x80000026, Index: 4, Flags: 1, Ecx: 4, Edx: 127},
		{Function: kvm.CPUIDSignature, Eax: kvm.CPUIDFeatures, Ebx: 0x4b4d564b, Ecx: 0x564b4d56, Edx: 0x4d},
		{Function: kvm.CPUIDFeatures, Eax: 0x1007efb},
	}

	return kvm.CPUID{Nent: uint32(len(entries)), Entries: entries}
}
