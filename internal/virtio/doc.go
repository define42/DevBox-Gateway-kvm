// Package virtio implements guest virtio devices, PCI transports and queues,
// together with host-side networking and remote-display backends.
//
// Keyboard and absolute-tablet devices receive remote input; GPU output and
// playback-only sound can be delivered through RDP. Device constructors borrow
// guest memory, so the machine owner must stop and join device workers before
// releasing that memory. InputPair routes events without owning device lifetimes.
package virtio
