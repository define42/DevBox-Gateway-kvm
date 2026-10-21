package processing

import (
	"bytes"
	"testing"
)

func TestDownsamplingLazyScratchParity(t *testing.T) {
	t.Parallel()
	for _, size := range []struct {
		name          string
		width, height int32
	}{
		{"half", 64, 48}, {"quarter", 32, 24}, {"fractional", 48, 32}, {"multistage", 18, 14},
	} {
		t.Run(size.name, func(t *testing.T) {
			t.Parallel()
			lazy, eager := NewCDownsampling(0), NewCDownsampling(0)
			for _, planes := range lazy.m_pSampleBuffer {
				for _, plane := range planes {
					if plane != nil {
						t.Fatal("constructor allocated downsampling scratch")
					}
				}
			}
			eager.AllocateSampleBuffer()
			source := downsampleTestMap(128, 96)
			for plane := range source.PPixel {
				for i := range source.PPixel[plane] {
					source.PPixel[plane][i] = byte(i*37 + plane*73 + i/17)
				}
			}
			before := [3][]byte{bytes.Clone(source.PPixel[0]), bytes.Clone(source.PPixel[1]), bytes.Clone(source.PPixel[2])}
			for iteration := range 3 {
				got, want := downsampleTestMap(size.width, size.height), downsampleTestMap(size.width, size.height)
				if result := lazy.Process(0, &source, &got); result != RET_SUCCESS {
					t.Fatal(result)
				}
				if result := eager.Process(0, &source, &want); result != RET_SUCCESS {
					t.Fatal(result)
				}
				for plane := range got.PPixel {
					if !bytes.Equal(got.PPixel[plane], want.PPixel[plane]) {
						t.Fatalf("iteration %d plane %d differs", iteration, plane)
					}
					if !bytes.Equal(source.PPixel[plane], before[plane]) {
						t.Fatalf("source plane %d modified", plane)
					}
				}
				// Re-entering after explicit release must allocate on demand.
				if iteration == 1 {
					lazy.FreeSampleBuffer()
				}
			}
		})
	}
}

func TestDownsamplingInvalidGeometryDoesNotAllocate(t *testing.T) {
	t.Parallel()
	d := NewCDownsampling(0)
	source, dest := downsampleTestMap(32, 32), downsampleTestMap(64, 64)
	if result := d.Process(0, &source, &dest); result != RET_INVALIDPARAM {
		t.Fatal(result)
	}
	if d.m_pSampleBuffer[0][0] != nil {
		t.Fatal("invalid call allocated scratch")
	}
}

func downsampleTestMap(width, height int32) SPixMap {
	result := SPixMap{SRect: SRect{IRectWidth: width, IRectHeight: height}}
	for plane := range result.PPixel {
		w, h := width, height
		if plane > 0 {
			w /= 2
			h /= 2
		}
		stride := ((w + 31) &^ 31) + 32
		result.IStride[plane] = stride
		result.IPixelOff[plane] = 16
		result.PPixel[plane] = bytes.Repeat([]byte{0xa7}, 16+int(stride*h)+64)
	}
	return result
}
