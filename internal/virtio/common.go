package virtio

const (
	// QueueSize is the maximum ring size every queue advertises, as in QEMU.
	// Indirect descriptors are not offered, so each descriptor of a request
	// takes a ring slot: a 4 KiB block request uses three, and Linux stops
	// network transmission while fewer than MAX_SKB_FRAGS + 2 are free.
	//
	// refs https://github.com/torvalds/linux/blob/5859a2b/drivers/net/virtio_net.c#L1754
	QueueSize = 256
)

type IRQInjector interface {
	InjectVirtioNetIRQ() error
	InjectVirtioBlkIRQ() error
	InjectVirtioGPUIRQ() error
}
