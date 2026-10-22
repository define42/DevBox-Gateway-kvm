package encoder

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"
)

func assertChromaSkipQP(t *testing.T, rs []int16, qp uint8, ff, mf int16) {
	t.Helper()
	before := append([]int16(nil), rs...)
	got, gp := hadamardQuantCallResult(func() int32 { return hadamardQuant2x2SkipQP(rs, qp, ff, mf) })
	want, wp := hadamardQuantCallResult(func() int32 { return WelsHadamardQuant2x2Skip_c(rs, ff, mf) })
	if got != want || gp != wp || !reflect.DeepEqual(rs, before) && len(rs) != 0 {
		t.Fatalf("QP%d len%d FF%d MF%d: got%d/%q want%d/%q or input changed", qp, len(rs), ff, mf, got, gp, want, wp)
	}
}

func TestChromaSkipQPThresholdBoundaries(t *testing.T) {
	for qp := range 52 {
		ff, mf := int16(int32(g_kiQuantInterFF[qp][0])<<1), g_kiQuantMF[qp][0]>>1
		entry := chromaSkipQPThresholds[qp]
		threshold := int16(65535/int32(mf) - int32(ff))
		if entry.ff != ff || entry.mf != mf || entry.threshold != threshold {
			t.Fatalf("QP%d snapshot differs from original scalar threshold", qp)
		}
		for _, offset := range []int{0, 16, 32, 48} {
			for delta := int32(-1); delta <= 1; delta++ {
				for _, sign := range []int32{-1, 1} {
					var input [64]int16
					input[offset] = int16(sign * (int32(threshold) + delta))
					assertChromaSkipQP(t, input[:49], uint8(qp), ff, mf)
				}
			}
		}
		for length := 0; length <= 64; length++ {
			var input [64]int16
			for i := range input {
				input[i] = int16(i*1701 + 13)
			}
			assertChromaSkipQP(t, input[:length], uint8(qp), ff, mf)
		}
	}
}

func TestChromaSkipQPFullCoefficientDomain(t *testing.T) {
	for _, qp := range []uint8{0, 51} {
		ff, mf := int16(int32(g_kiQuantInterFF[qp][0])<<1), g_kiQuantMF[qp][0]>>1
		for _, offset := range []int{0, 16, 32, 48} {
			for value := -32768; value <= 32767; value++ {
				var input [49]int16
				input[offset] = int16(value)
				got := hadamardQuant2x2SkipQP(input[:], qp, ff, mf)
				want := WelsHadamardQuant2x2Skip_c(input[:], ff, mf)
				if got != want {
					t.Fatalf("QP%d offset%d value%d: got%d want%d", qp, offset, value, got, want)
				}
			}
		}
	}
	// Full-range combinations exercise wrap at both butterfly stages.
	rng := rand.New(rand.NewPCG(264, 522))
	for trial := range 16384 {
		var input [80]int16
		for i := range input {
			input[i] = int16(rng.Uint32())
		}
		qp := uint8(trial % 52)
		ff, mf := int16(int32(g_kiQuantInterFF[qp][0])<<1), g_kiQuantMF[qp][0]>>1
		offset := trial % 16
		before := input
		assertChromaSkipQP(t, input[offset:offset+49], qp, ff, mf)
		if input != before {
			t.Fatal("unaligned input or canaries changed")
		}
	}
}

func TestChromaSkipQPFallbackParameters(t *testing.T) {
	for _, qp := range []uint8{0, 20, 51, 52, 255} {
		for _, params := range [][2]int16{{0, 0}, {-32768, 0}, {0, 1}, {0, 2}, {32767, 32767}, {-32768, 2}, {32767, -32768}, {32767, -1}} {
			for _, length := range []int{0, 1, 16, 32, 48, 49, 64} {
				var input [64]int16
				for i := range input {
					input[i] = int16(i*1701 + 13)
				}
				assertChromaSkipQP(t, input[:length], qp, params[0], params[1])
			}
			assertChromaSkipQP(t, nil, qp, params[0], params[1])
		}
	}
}

func TestChromaSkipQPMutableTables(t *testing.T) {
	for qp := range 52 {
		t.Run(fmt.Sprintf("QP%d", qp), func(t *testing.T) {
			oldFF, oldMF := g_kiQuantInterFF[qp], g_kiQuantMF[qp]
			defer func() { g_kiQuantInterFF[qp], g_kiQuantMF[qp] = oldFF, oldMF }()
			entry := chromaSkipQPThresholds[qp]
			for _, params := range [][2]int16{
				{oldFF[0] ^ 1, oldMF[0]}, {oldFF[0], oldMF[0] ^ 2},
				{16384, oldMF[0]}, {-32768, -32768}, {oldFF[0], 0}, {oldFF[0], 1},
				// Different raw words with identical effective DC parameters.
				{oldFF[0] ^ -32768, oldMF[0] ^ 1},
			} {
				g_kiQuantInterFF[qp][0], g_kiQuantMF[qp][0] = params[0], params[1]
				ff, mf := int16(int32(params[0])<<1), params[1]>>1
				for _, length := range []int{0, 32, 48, 49, 64} {
					var input [64]int16
					for i := range input {
						input[i] = int16(i*1701 + 13)
					}
					assertChromaSkipQP(t, input[:length], uint8(qp), ff, mf)
				}
			}
			if chromaSkipQPThresholds[qp] != entry {
				t.Fatal("mutable source table changed the cached snapshot")
			}
		})
	}
}
