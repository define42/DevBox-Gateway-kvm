//go:build !amd64

package common

// SadPtrSupported is false: callers keep the checked SAD functions.
const SadPtrSupported = false

func SadPtr(_, _ int, _ *byte, _ int, _ *byte, _ int) int32 { panic("h264: SadPtr unsupported") }

func SadFourPtr(_, _ int, _ *byte, _ int, _ *byte, _ int, _ *[4]int32) {
	panic("h264: SadFourPtr unsupported")
}
