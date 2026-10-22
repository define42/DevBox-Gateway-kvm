package encoder

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var intraDCDcOffsets = [...]int{0, 16, 64, 80, 32, 48, 96, 112, 128, 144, 192, 208, 160, 176, 224, 240}

func intraDCPanic(call func()) (failure string) {
	defer func() {
		if value := recover(); value != nil {
			failure = fmt.Sprintf("%T: %v", value, value)
		}
	}()
	call()
	return ""
}

func TestIntraDCForwardCheckedParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x264dc4))
	for trial := 0; trial < 2048; trial++ {
		var source [243]int16
		for i := range source {
			source[i] = int16(rng.Uint32())
		}
		before := source
		var got, want [18]int16
		for i := range got {
			got[i], want[i] = -1234, -1234
		}
		hadamardT4Dc(got[1:17], source[1:242])
		WelsHadamardT4Dc_c(want[1:17], source[1:242])
		if got != want || source != before {
			t.Fatalf("trial%d: output%v want%v or input changed", trial, got, want)
		}
	}
	for _, value := range []int16{-32768, -32767, -8192, -1, 0, 1, 8192, 32766, 32767} {
		for pattern := 0; pattern < 4; pattern++ {
			var source [241]int16
			for i, offset := range intraDCDcOffsets {
				source[offset] = value
				if (pattern == 1 && i%2 != 0) || (pattern == 2 && i/4%2 != 0) || (pattern == 3 && (i+i/4)%2 != 0) {
					source[offset] = ^value
				}
			}
			var got, want [16]int16
			hadamardT4Dc(got[:], source[:])
			WelsHadamardT4Dc_c(want[:], source[:])
			if got != want {
				t.Fatalf("clipping value%d pattern%d: %v want%v", value, pattern, got, want)
			}
		}
	}
}

func TestIntraDCForwardAliasesAndShortSlices(t *testing.T) {
	for _, sourceLength := range []int{0, 1, 16, 80, 160, 224, 240, 241, 256} {
		for dstLength := 0; dstLength <= 17; dstLength++ {
			for _, dstOff := range []int{0, 1, 15, 16, 32, 80, 160, 224, 240, 255, 256} {
				var got, want [280]int16
				for i := range got {
					got[i], want[i] = int16(i*1701-32768), int16(i*1701-32768)
				}
				gotPanic := intraDCPanic(func() { hadamardT4Dc(got[dstOff:dstOff+dstLength], got[:sourceLength]) })
				wantPanic := intraDCPanic(func() { WelsHadamardT4Dc_c(want[dstOff:dstOff+dstLength], want[:sourceLength]) })
				if gotPanic != wantPanic || got != want {
					t.Fatalf("sourceLen%d dstLen%d dstOff%d: panic%q/%q or partial output differs", sourceLength, dstLength, dstOff, gotPanic, wantPanic)
				}
			}
		}
	}
}

func TestIntraDCInverseCheckedParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x2641dc))
	multipliers := []uint16{0, 1, 2, 3, 10, 32767, 32768, 65534, 65535}
	for qp := range 52 {
		multipliers = append(multipliers, common.G_kuiDequantCoeff[qp][0]>>2)
	}
	for _, mf := range multipliers {
		for trial := 0; trial < 128; trial++ {
			var got [18]int16
			for i := range got {
				got[i] = int16(rng.Uint32())
			}
			want := got
			dequantIHadamard4x4(got[1:17], mf)
			WelsDequantIHadamard4x4_c(want[1:17], mf)
			if got != want {
				t.Fatalf("mf%d trial%d: %v want%v", mf, trial, got, want)
			}
		}
	}
	for mf := 0; mf <= 65535; mf++ {
		got := [18]int16{1234, -32768, 32767, -1, 1, 16384, -16385, 8191, -8192, 32766, -32767, 3, -5, 7, -11, 13, -17, 5678}
		want := got
		dequantIHadamard4x4(got[1:17], uint16(mf))
		WelsDequantIHadamard4x4_c(want[1:17], uint16(mf))
		if got != want {
			t.Fatalf("multiplier domain mf%d: %v want%v", mf, got, want)
		}
	}
}

func TestIntraDCInverseShortSlices(t *testing.T) {
	for length := 0; length <= 17; length++ {
		for _, mf := range []uint16{0, 1, 32768, 65535} {
			var got, want [19]int16
			for i := range got {
				got[i], want[i] = int16(i*1701-32768), int16(i*1701-32768)
			}
			gotPanic := intraDCPanic(func() { dequantIHadamard4x4(got[1:1+length], mf) })
			wantPanic := intraDCPanic(func() { WelsDequantIHadamard4x4_c(want[1:1+length], mf) })
			if gotPanic != wantPanic || !reflect.DeepEqual(got, want) {
				t.Fatalf("length%d mf%d: panic%q/%q or partial output differs", length, mf, gotPanic, wantPanic)
			}
		}
	}
}
