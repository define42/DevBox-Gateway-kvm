//go:build (amd64 || arm64) && !purego

package virtio

const blkNotificationSuppression = true

// blkNotificationBarrier orders shared guest-memory stores before subsequent
// loads. A Go acquire lock alone does not provide this hardware ordering.
func blkNotificationBarrier()

// blkCompletionBarrier orders completed data and used entries before publishing
// Used.Idx. x86 already preserves Store-to-Store order; arm64 needs a barrier.
func blkCompletionBarrier()
