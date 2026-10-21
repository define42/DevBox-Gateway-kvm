//go:build !amd64

package avc

import "image"

func scaleI420RegionScaled(dst []byte, width, height int, img *image.RGBA, region image.Rectangle) (bool, bool) {
	return false, false
}
