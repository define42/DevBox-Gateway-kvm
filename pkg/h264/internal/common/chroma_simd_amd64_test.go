//go:build amd64

package common

import "testing"

func TestChromaSIMDBounds(t *testing.T) {
	for _, width := range []int32{2, 4, 8} {
		for _, field := range []string{"source", "destination", "negative-offset", "overflowing-offset"} {
			t.Run(field, func(t *testing.T) {
				src := make([]byte, 256)
				dst := make([]byte, 256)
				srcOff, dstOff := 1, 3
				if field == "source" {
					src = src[:srcOff+8*16+int(width)]
				}
				if field == "destination" {
					dst = dst[:dstOff+7*16+int(width)-1]
				}
				if field == "negative-offset" {
					srcOff = -1
				}
				if field == "overflowing-offset" {
					dstOff = int(^uint(0) >> 1)
				}
				defer func() {
					if recover() == nil {
						t.Fatal("invalid span did not panic before assembly")
					}
				}()
				McChroma_sse2(src, srcOff, 16, dst, dstOff, 16, 3, 5, width, 8)
			})
		}
	}
}
