//go:build amd64

package encoder

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

func TestSkipDctQuantRawParameterDomain(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	rng := rand.New(rand.NewPCG(264, 864))
	for fixture := 0; fixture < 8192; fixture++ {
		stride1, stride2 := []int{0, 1, 8, 19, 127}[fixture%5], []int{0, 3, 8, 23, 128}[(fixture/5)%5]
		p1, p2 := make([]byte, 3+7*stride1+8), make([]byte, 5+7*stride2+8)
		for i := range p1 {
			p1[i] = byte(rng.Uint32())
		}
		for i := range p2 {
			p2[i] = byte(rng.Uint32())
		}
		if fixture%4 == 0 {
			// Zero coefficients exercise the special positive sign rule.
			for i := range p1 {
				p1[i] = 128
			}
			for i := range p2 {
				p2[i] = 128
			}
		} else if fixture%4 == 1 {
			for i := range p1 {
				p1[i] = 255
			}
			clear(p2)
		} else if fixture%4 == 2 {
			clear(p1)
			for i := range p2 {
				p2[i] = 255
			}
		}
		var ff, mf [8]int16
		for i := range ff {
			// Every int16 bit pattern appears in both quantization tables.
			ff[i], mf[i] = int16(fixture*8+i), int16(65535-fixture*8-i)
		}
		assertSkipDctQuantRaw(t, fixture, p1, 3, stride1, p2, 5, stride2, ff, mf)
	}
}

func TestSkipDctQuantRawQPAndEdges(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	rng := rand.New(rand.NewPCG(264, 865))
	edges := [...]int16{-32768, -32767, -1, 0, 1, 16383, 16384, 32766, 32767}
	for fixture := 0; fixture < 52*16; fixture++ {
		stride := []int{8, 32, 1984}[fixture%3]
		p1, p2 := make([]byte, 7*stride+8), make([]byte, 7*stride+8)
		for i := range p1 {
			p1[i], p2[i] = byte(rng.Uint32()), byte(rng.Uint32())
		}
		ff, mf := g_kiQuantInterFF[fixture%52], g_kiQuantMF[fixture%52]
		if fixture%3 == 0 {
			for i := range ff {
				ff[i], mf[i] = edges[(fixture+i)%len(edges)], edges[(fixture/3+i)%len(edges)]
			}
		}
		assertSkipDctQuantRaw(t, fixture, p1, 0, stride, p2, 0, stride, ff, mf)
	}
}

func assertSkipDctQuantRaw(t *testing.T, fixture int, p1 []byte, off1, stride1 int, p2 []byte, off2, stride2 int, ff, mf [8]int16) {
	t.Helper()
	var got, want [68]int16
	var gotMax, wantMax [8]int16
	for i := range got {
		got[i], want[i] = -12345, -12345
	}
	for i := range gotMax {
		gotMax[i], wantMax[i] = -23456, -23456
	}
	beforeFF, beforeMF := ff, mf
	// Independent scalar transform plus the historical SSE2 quantizer also
	// defines signed FF/MF behavior outside the scalar quantizer's domain.
	WelsDctFourT4_c(want[1:65], p1, off1, int32(stride1), p2, off2, int32(stride2))
	quantFour4x4MaxSSE2(want[1:65], ff[:], mf[:], wantMax[1:5])
	skipDctQuant8x8AVX2((*[64]int16)(got[1:65]), &p1[off1], stride1, &p2[off2], stride2, &ff, &mf, (*[4]int16)(gotMax[1:5]))
	if got != want || gotMax != wantMax || ff != beforeFF || mf != beforeMF {
		t.Fatalf("fixture%d stride%d/%d: fused coefficients/maxima/tables/canaries differ", fixture, stride1, stride2)
	}
}

func TestSkipDctQuantEligibility(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, stride := range []int32{0, 1, 16, 32, 1984} {
		s := newSkipDctQuantPixelsState(stride, 32, 0)
		handled, accepted := trySkipDctQuantPixels(&s.ctx, &s.cache, s.source, s.cache.SPicData.iEncMbOff[0], stride, 32)
		if !handled || !accepted {
			t.Fatalf("stride%d: ordinary builtin path did not use fusion", stride)
		}
	}
	for _, name := range []string{"custom", "scalar", "nil", "short", "negative", "QP52", "nil-levels", "FF-alias", "MF-alias"} {
		t.Run(name, func(t *testing.T) {
			s := newSkipDctQuantPixelsState(32, 32, 0)
			qp := uint8(32)
			switch name {
			case "custom":
				dct := s.functions.pfDctFourT4
				s.functions.pfDctFourT4 = func(d []int16, a []byte, ao int, as int32, p []byte, po int, ps int32) { dct(d, a, ao, as, p, po, ps) }
			case "scalar":
				s.functions.pfDctFourT4 = WelsDctFourT4_c
			case "nil":
				s.functions.pfDctFourT4 = nil
			case "short":
				s.cache.pCoeffLevel = s.cache.pCoeffLevel[:255]
			case "negative":
				s.layer.iEncStride[0] = -32
			case "QP52":
				qp = 52
			case "nil-levels":
				s.cache.pDct = nil
			case "FF-alias":
				qp = 0
				s.cache.pCoeffLevel = unsafe.Slice(&g_kiQuantInterFF[0][0], 256)
			case "MF-alias":
				qp = 0
				s.cache.pCoeffLevel = unsafe.Slice(&g_kiQuantMF[0][0], 256)
			}
			beforeCoefficients, beforePrediction, beforeLevels := s.coefficients, s.prediction, s.levels
			beforeFF, beforeMF := g_kiQuantInterFF, g_kiQuantMF
			handled, accepted := trySkipDctQuantPixels(&s.ctx, &s.cache, s.source, s.cache.SPicData.iEncMbOff[0], s.layer.iEncStride[0], qp)
			if handled || accepted || s.coefficients != beforeCoefficients || s.prediction != beforePrediction || s.levels != beforeLevels ||
				g_kiQuantInterFF != beforeFF || g_kiQuantMF != beforeMF {
				t.Fatal("ineligible input was consumed or modified")
			}
		})
	}
}

// Raw-only numbers are diagnostic. The baseline-compatible complete callers
// above decide whether identity/validation overhead is repaid in production.
func BenchmarkSkipDctQuantRaw(b *testing.B) {
	if !cpu.X86.HasAVX2 {
		b.Skip("AVX2 unavailable")
	}
	for _, stride := range []int{32, 1984} {
		b.Run(fmt.Sprintf("stride%d", stride), func(b *testing.B) {
			p1, p2 := make([]byte, 7*stride+8), make([]byte, 7*16+8)
			for i := range p1 {
				p1[i] = byte(i*29 + 17)
			}
			for i := range p2 {
				p2[i] = byte(i*13 + 71)
			}
			var dct [64]int16
			var maximum [4]int16
			ff, mf := g_kiQuantInterFF[32], g_kiQuantMF[32]
			b.ReportAllocs()
			for b.Loop() {
				skipDctQuant8x8AVX2(&dct, &p1[0], stride, &p2[0], 16, &ff, &mf, &maximum)
			}
			dctBenchmarkSink = dct[0] + maximum[0]
		})
	}
}
