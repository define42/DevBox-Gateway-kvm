package virtio

import (
	"bytes"
	"fmt"
	"testing"
)

func TestBGRToRGBARow(t *testing.T) {
	t.Parallel()
	for _, impl := range []struct {
		name    string
		convert func([]byte, []byte, uint32)
	}{
		{name: "native", convert: bgrToRGBARow},
		{name: "generic", convert: bgrToRGBARowGeneric},
	} {
		for _, pixels := range []int{0, 1, 2, 7, 8, 9, 15, 16, 17, 31, 32, 33, 63, 64, 65, 1919, 1920} {
			for _, offset := range []int{0, 1, 7, 31} {
				for _, alpha := range []uint32{0, 0xff000000} {
					name := fmt.Sprintf("%s/pixels=%d/offset=%d/alpha=%x", impl.name, pixels, offset, alpha)
					t.Run(name, func(t *testing.T) {
						t.Parallel()
						const guard = 37
						n := pixels * gpuBytesPerPixel
						source := make([]byte, offset+n+guard)
						for i := range source {
							source[i] = byte(i*37 + i/4 + 11)
						}
						before := bytes.Clone(source)
						prefix := guard - offset
						dest := bytes.Repeat([]byte{0xa5}, prefix+n+guard)
						want := bytes.Clone(dest)
						for x := range pixels {
							s, d := offset+x*4, prefix+x*4
							want[d], want[d+1], want[d+2] = source[s+2], source[s+1], source[s]
							want[d+3] = source[s+3] | byte(alpha>>24)
						}
						impl.convert(dest[prefix:prefix+n], source[offset:offset+n], alpha)
						if !bytes.Equal(dest, want) {
							t.Fatal("converted pixels or surrounding guard bytes differ")
						}
						if !bytes.Equal(source, before) {
							t.Fatal("conversion changed source bytes")
						}
					})
				}
			}
		}
	}
}
