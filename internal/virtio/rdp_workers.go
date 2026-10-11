package virtio

import "errors"

var errRDPThreads = errors.New("rdp: H.264 slice count must be 0..16; nonzero requires H.264")

const rdpAutoSlices = 1

// rdpEncoderSlices bounds the requested slice count using the existing host
// budget calculation. Small hosts still get one slice.
func rdpEncoderSlices(cpus, guestCPUs, limit int) int {
	if limit == 0 {
		limit = rdpAutoSlices
	}

	return min(limit, max(1, cpus-max(1, guestCPUs)-1))
}
