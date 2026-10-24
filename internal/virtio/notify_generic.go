//go:build (!amd64 && !arm64) || purego

package virtio

// Without an architecture-specific shared-memory fence, keep notifying. Virtio
// permits spurious notifications; an unfenced suppression check could lose one.
const notificationSuppression = false

func notificationBarrier() {}

func completionBarrier() {}
