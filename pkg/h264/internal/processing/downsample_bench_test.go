package processing

import (
	"fmt"
	"testing"
)

func BenchmarkDownsamplingConstruction(b *testing.B) {
	for _, eager := range []bool{false, true} {
		b.Run(fmt.Sprintf("eager=%t", eager), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				d := NewCDownsampling(0)
				if eager {
					d.AllocateSampleBuffer()
				}
				d.FreeSampleBuffer()
			}
		})
	}
}
