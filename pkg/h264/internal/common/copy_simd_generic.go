//go:build !amd64

package common

func copyBlockFast(_ []uint8, _ int, _ int32, _ []uint8, _ int, _ int32, _, _ int) bool {
	return false
}

func copyBlockFast16(_ []uint8, _ int, _ int32, _ []uint8, _ int, _ int32, _ int) bool { return false }

func copyBlockFast8(_ []uint8, _ int, _ int32, _ []uint8, _ int, _ int32, _ int) bool { return false }
