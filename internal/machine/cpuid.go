package machine

import (
	"math/bits"

	"github.com/define42/gokvm/kvm"
)

func (m *Machine) initCPUID(cpu int) error {
	var cpuid kvm.CPUID
	if err := kvm.GetSupportedCPUID(m.kvmFd, &cpuid); err != nil {
		return err
	}
	configureCPUIDTopology(&cpuid, cpu, len(m.vcpuFds))

	// https://www.kernel.org/doc/html/latest/virt/kvm/cpuid.html
	for i := range cpuid.Entries {
		entry := &cpuid.Entries[i]
		switch entry.Function {
		case kvm.CPUIDFuncPerMon:
			entry.Eax = 0 // disable
		case kvm.CPUIDSignature:
			entry.Eax = kvm.CPUIDFeatures
			entry.Ebx = 0x4b4d564b // KVMK
			entry.Ecx = 0x564b4d56 // VMKV
			entry.Edx = 0x4d       // M
		case 7:
			// Unset X86_FEATURE_FSRM (Fast Short Rep Mov).
			entry.Edx &^= uint32(1) << 4
		}
	}

	return kvm.SetCPUID2(m.vcpuFds[cpu], &cpuid)
}

// KVM_GET_SUPPORTED_CPUID describes host capabilities, including host topology.
// Describe the VM as one socket with one thread per core instead. APIC IDs must
// match KVM_CREATE_VCPU and the MP tables, not the host CPU running this ioctl.
// Only topology fields change: instruction, timer and hypervisor capabilities
// remain those reported by KVM. The MP-table boot path supports 1..64 CPUs.
func configureCPUIDTopology(cpuid *kvm.CPUID, cpu, count int) {
	id, cores := uint32(cpu), uint32(count)
	packageShift := uint32(bits.Len32(cores - 1))
	var maxBasic, maxExtended uint32
	var amdTopology bool
	for _, entry := range cpuid.Entries[:cpuid.Nent] {
		switch entry.Function {
		case 0:
			maxBasic = entry.Eax
		case 0x80000000:
			maxExtended = entry.Eax
		case 0x80000001:
			amdTopology = entry.Ecx&(1<<22) != 0 // AMD topology extensions.
		}
	}

	entries := make([]kvm.CPUIDEntry2, 0, int(cpuid.Nent)+8)
	topologySeen := make(map[uint32]bool, 3)
	for _, entry := range cpuid.Entries[:cpuid.Nent] {
		switch entry.Function {
		case 1:
			entry.Ebx = entry.Ebx&0xffff | cores<<16 | id<<24
			entry.Edx &^= 1 << 28
			if cores > 1 {
				// HTT indicates multiple logical CPUs per package, including
				// multiple cores without simultaneous multithreading.
				entry.Edx |= 1 << 28
			}
		case 4, 0x8000001d:
			if entry.Eax&0x1f != 0 { // Do not turn a cache terminator into a cache.
				entry.Eax &^= 0xfff << 14
				if (entry.Eax>>5)&7 >= 3 {
					entry.Eax |= (cores - 1) << 14 // Shared last-level cache.
				}
				if entry.Function == 4 {
					entry.Eax = entry.Eax&^(0x3f<<26) | (cores-1)<<26
				}
			}
		case 0xb, 0x1f, 0x80000026:
			limit := maxBasic
			if entry.Function == 0x80000026 {
				limit = maxExtended
			}
			if entry.Function <= limit {
				if !topologySeen[entry.Function] {
					entries = append(entries, cpuidTopologyLevels(entry.Function, id, cores, packageShift)...)
					topologySeen[entry.Function] = true
				}

				continue
			}
		case 0x80000008:
			entry.Ecx = entry.Ecx&^0xf0ff | packageShift<<12 | (cores - 1)
		case 0x8000001e:
			if amdTopology {
				entry.Eax = id
				entry.Ebx = entry.Ebx&^0xffff | id // Core ID; one thread per core.
				entry.Ecx &^= 0x7ff                // Node 0; one node per socket.
			}
		}
		entries = append(entries, entry)
	}
	cpuid.Entries = entries
	cpuid.Nent = uint32(len(entries))
}

func cpuidTopologyLevels(function, id, cores, packageShift uint32) []kvm.CPUIDEntry2 {
	const significantIndex = 1

	levels := uint32(2) // SMT and cores per package, followed by a terminator.
	if function == 0x80000026 {
		levels = 4 // AMD additionally enumerates CCD and socket boundaries.
	}
	entries := make([]kvm.CPUIDEntry2, levels+1)
	for i := range levels + 1 {
		entries[i] = kvm.CPUIDEntry2{Function: function, Index: i, Flags: significantIndex, Ecx: i, Edx: id}
		if i < levels {
			entries[i].Eax = packageShift
			entries[i].Ebx = cores
			entries[i].Ecx |= (i + 1) << 8
		}
	}
	entries[0].Eax, entries[0].Ebx = 0, 1 // One thread per core.

	return entries
}
