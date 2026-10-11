package virtio

const (
	// The number of free descriptors in virt queue must exceed
	// MAX_SKB_FRAGS (16). Otherwise, packet transmission from
	// the guest to the host will be stopped.
	//
	// refs https://github.com/torvalds/linux/blob/5859a2b/drivers/net/virtio_net.c#L1754
	QueueSize = 32
)

type IRQInjector interface {
	InjectVirtioNetIRQ() error
	InjectVirtioBlkIRQ() error
	InjectVirtioGPUIRQ() error
}
