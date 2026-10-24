//go:build (amd64 || arm64) && !purego

package virtio

const notificationSuppression = true

// notificationBarrier orders shared guest-memory stores before subsequent
// loads. A Go acquire lock alone does not provide this hardware ordering.
func notificationBarrier()

// completionBarrier orders completed data and used entries before publishing
// Used.Idx. x86 already preserves Store-to-Store order; arm64 needs a barrier.
func completionBarrier()
