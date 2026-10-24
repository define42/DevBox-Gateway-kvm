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

## Virtio notifications and interrupts

Each modern virtio function has a 32 KiB memory BAR: common configuration at
`0x0000`, ISR at `0x1000`, device configuration at `0x2000`, one 16-bit queue
doorbell per queue at `0x3000 + 4 * queue`, and the MSI-X table and pending
bits at `0x4000` and `0x4800`.

| Path | With KVM support | Fallback |
| --- | --- | --- |
| Queue kick | A KVM ioeventfd per doorbell completes the guest write inside KVM; a worker forwards it to the device | MMIO exit to the vCPU thread |
| Queue interrupt | Per-queue MSI-X vector: an irqfd bound to a GSI routed to the guest-programmed MSI message | ISR bit plus INTx line, read and acknowledged by the guest |

The bus reports every BAR assignment, so doorbells follow firmware and kernel
BAR moves. A doorbell KVM cannot bind, for example while two BARs briefly
overlap during enumeration, keeps using the MMIO exit until its address is
free. Every MSI-X vector has a dedicated GSI and irqfd from attachment. KVM
replaces its routing table as a whole, so the table always keeps KVM's default
IOAPIC/PIC routes for line interrupts.

The guest chooses the interrupt mode: drivers and firmware that do not enable
MSI-X, and Linux booted with `pci=nomsi`, keep using INTx with ISR-based
recovery. Delivered MSI-X messages are never re-sent; masked vectors latch
their pending bit and are delivered on unmask, and a failed delivery stays
pending until the device worker's recovery pass resends it.

Without `EVENT_IDX`, used-buffer notifications honor `VRING_AVAIL_F_NO_INTERRUPT`
for block, network receive and sound queues. Network transmit completions always
notify: Linux `napi_tx` drivers such as 5.14's (fixed by 6.5) can stop a full
TX queue with notifications disabled and still wait for that interrupt. INTx
hid this because any interrupt services every queue.

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
or reserved regions. When enabled, the VESA framebuffer reserves
`0x01000000 .. 0x01400000`; relocatable kernels are placed outside that range.

ELF and PVH boot preserve their load addresses and clear each segment's
uninitialized data.
