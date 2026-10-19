package rdp

import (
	"fmt"
	"testing"
)

func BenchmarkInputDecoderHeldKeys(b *testing.B) {
	for _, held := range []int{1, 16, 128, 256} {
		for _, packet := range []struct {
			name string
			data []byte
		}{
			{name: "pointer", data: []byte{4, 0x20, 0, 8, 10, 0, 20, 0}},
			{name: "repeat", data: []byte{4, 0x80, 0, 0x10}},
		} {
			b.Run(fmt.Sprintf("%s/held=%d", packet.name, held), func(b *testing.B) {
				var decoder InputDecoder
				for i := range held {
					code := uint16(0x1000 + i)
					if _, err := decoder.Decode([]byte{4, 0x80, byte(code), byte(code >> 8)}, true); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					if _, err := decoder.Decode(packet.data, true); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
