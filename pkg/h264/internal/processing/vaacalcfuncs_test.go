package processing

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestVAACalcSadMaskedMatchesFull(t *testing.T) {
	t.Parallel()
	const width, height, stride, curOff, refOff = 80, 48, 93, 7, 3
	const count = (width / 16) * (height / 16)
	for _, test := range []struct {
		name string
		mask []bool
	}{
		{name: "nil"},
		{name: "short", mask: []bool{true}},
		{name: "oversized", mask: make([]bool, count+1)},
		{name: "all changed", mask: make([]bool, count)},
		{name: "all unchanged", mask: slices.Repeat([]bool{true}, count)},
		{name: "runs and gaps", mask: []bool{true, false, false, true, true, false, true, false, false, true, true, true, true, true, false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cur, ref := make([]byte, stride*height+curOff), make([]byte, stride*height+refOff)
			rng := rand.New(rand.NewSource(42))
			rng.Read(cur)
			rng.Read(ref)
			if len(test.mask) == count {
				for mb, equal := range test.mask {
					if !equal {
						continue
					}
					x, y := (mb%(width/16))*16, (mb/(width/16))*16
					for row := range 16 {
						off := (y+row)*stride + x
						copy(cur[curOff+off:curOff+off+16], ref[refOff+off:refOff+off+16])
					}
				}
			}
			want := make([][4]int32, count+1)
			got := make([][4]int32, count+1)
			for i := range got {
				got[i] = [4]int32{91, 92, 93, 94}
				want[i] = got[i]
			}
			var wantFrame, gotFrame int32
			VAACalcSad_c(cur, curOff, ref, refOff, width, height, stride, &wantFrame, want)
			for _, flags := range []uint32{0, common.WelsCPUFeatureDetect(nil)} {
				calc := NewCVAACalculation(int32(flags))
				vaaCalcSadMasked(calc.m_sVaaFuncs.pfVAACalcSad, cur, curOff, ref, refOff,
					width, height, stride, &gotFrame, got, test.mask)
				if gotFrame != wantFrame || !slices.Equal(got, want) {
					t.Fatalf("flags=%x frame=%d want=%d SAD=%v want=%v", flags, gotFrame, wantFrame, got, want)
				}
			}
		})
	}
}

func TestVAAProcessHintsOnlyAffectSadAndReleaseBorrow(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name               string
		bgd, ssd, variance int32
	}{
		{name: "SAD"}, {name: "background", bgd: 1}, {name: "SSD", ssd: 1}, {name: "variance", variance: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cur, ref := make([]byte, 256), make([]byte, 256)
			for i := range cur {
				cur[i] = 1
			}
			result := newVaaResult(1)
			calc := NewCVAACalculation(int32(common.WelsCPUFeatureDetect(nil)))
			param := SVAACalcParam{ICalcBgd: test.bgd, ICalcSsd: test.ssd, ICalcVar: test.variance,
				PCalcResult: result, UnchangedMacroblocks: []bool{true}}
			calc.Set(0, &param)
			src := SPixMap{PPixel: [3][]uint8{cur}, IStride: [3]int32{16}, SRect: SRect{IRectWidth: 16, IRectHeight: 16}}
			refMap := src
			refMap.PPixel[0] = ref
			calc.Process(0, &src, &refMap)
			want := int32(256)
			if test.name == "SAD" {
				want = 0
			}
			if result.IFrameSad != want {
				t.Fatalf("SAD=%d want=%d", result.IFrameSad, want)
			}
			if calc.m_sCalcParam.UnchangedMacroblocks != nil {
				t.Fatal("retained borrowed hint")
			}
			calc.Process(0, &src, &refMap)
			if result.IFrameSad != 256 {
				t.Fatal("reused hint without a new Set")
			}
		})
	}
}

func TestVAACalcSadMaskedRejectsShortOutput(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("masked VAA wrote beyond output length")
		}
	}()
	out := make([][4]int32, 1)
	var frame int32
	vaaCalcSadMasked(VAACalcSad_c, nil, 0, nil, 0, 16, 16, 16, &frame, out[:0], []bool{true})
}
