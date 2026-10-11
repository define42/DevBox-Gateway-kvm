# machine

## Memory layout

Guest RAM is indexed by guest-physical address in the host mapping. For guests
up to 3.25 GiB, RAM is contiguous. Larger guests have two KVM memory slots:

| Guest physical addresses | Contents |
| --- | --- |
| `0 .. 0xd0000000` | Low RAM, including legacy boot data and reserved BIOS areas |
| `0xd0000000 .. 0xf8000000` | PCI MMIO aperture; no KVM RAM mapping |
| `0xf8000000 .. 0x100000000` | Reserved platform space; no KVM RAM mapping |
| `0x100000000 ..` | Remaining requested RAM |

The host reserves virtual address space for the hole so device buffers can still
use guest addresses as slice indices. Loaders and devices validate complete
ranges with `internal/guestmem.ValidRange` before accessing RAM. The Linux E820
and PVH memory maps and CMOS all describe the same low/high RAM sizes. The PCI
aperture is omitted from E820 so Linux can allocate device BARs there.

## Direct Linux boot

| Location | Contents |
| --- | --- |
| `0x00010000` | Linux boot parameters (`boot_params`) |
| `0x00020000` | NUL-terminated kernel command line |
| `0x00100000` or kernel-preferred aligned address | bzImage protected-mode payload |
| Highest suitable low-RAM range | Page-aligned initramfs |

The loader checks the kernel's runtime workspace, relocation alignment,
`initrd_addr_max`, and command-line capacity. The initramfs must fit below both
its advertised address limit and the PCI hole, without overlapping the kernel
or reserved regions. When enabled for ISO guests, the VESA framebuffer reserves
`0x01000000 .. 0x01400000`; relocatable kernels are placed outside that range.

UKI boot uses the same Linux loader with bounded readers over the `.linux` and
`.initrd` sections. It does not enter UEFI firmware. ELF and PVH boot preserve
their load addresses and clear each segment's uninitialized data.
