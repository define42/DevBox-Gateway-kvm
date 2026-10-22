//go:build amd64

package encoder

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestUpdateMbMvSIMDDispatch(t *testing.T) {
	var functions SWelsFuncPtrList
	for _, flags := range []uint32{0, common.WELS_CPU_SSE2, common.WELS_CPU_SSSE3,
		common.WELS_CPU_SSE2 | common.WELS_CPU_SSSE3, common.WELS_CPU_AVX2, 0} {
		InitIntraAnalysisVaaInfo(&functions, flags)
		want := reflect.ValueOf(UpdateMbMv_c).Pointer()
		if flags&common.WELS_CPU_SSE2 != 0 {
			want = reflect.ValueOf(UpdateMbMv_sse2).Pointer()
		}
		if got := reflect.ValueOf(functions.pfUpdateMbMv).Pointer(); got != want {
			t.Fatalf("flags%x: wrong update callback", flags)
		}
		if reflect.ValueOf(functions.pfGetMbSignFromInterVaa).Pointer() != reflect.ValueOf(MdInterAnalysisVaaInfo_c).Pointer() {
			t.Fatal("motion-vector dispatch changed inter analysis")
		}
	}
}

// Every possible bit pattern occurs in each int16 component, with two distinct
// pairings. Compare the sixteen vectors directly, independently of byte packing.
func TestUpdateMbMvSIMDFullComponentRange(t *testing.T) {
	for pairing := 0; pairing < 2; pairing++ {
		for bits := uint32(0); bits < 1<<16; bits++ {
			y := ^bits
			if pairing != 0 {
				y = bits*40503 + 17619 // Odd multiplier permutes the full 16-bit range.
			}
			value := SMVUnitXY{int16(bits), int16(y)}
			var got, want [18]SMVUnitXY
			for i := range got {
				got[i] = SMVUnitXY{int16(i*1701 + 5), int16(i*301 - 1703)}
			}
			want = got
			for i := 1; i <= 16; i++ {
				want[i] = value
			}
			UpdateMbMv_sse2(got[1:17:17], value)
			if got != want {
				t.Fatalf("pairing%d bits%x: fill or canary differs", pairing, bits)
			}
		}
	}
}

// Exercise every byte alignment and independent x/y extremes, including raw ABI
// packing. The expected bytes use separate component writes, not the kernel's
// packed-value calculation.
func TestUpdateMbMvSIMDExactSpan(t *testing.T) {
	if unsafe.Sizeof(SMVUnitXY{}) != 4 {
		t.Fatal("unexpected motion-vector layout")
	}
	values := []int16{-32768, -32767, -1, 0, 1, 32766, 32767}
	for offset := 16; offset < 32; offset++ {
		for _, x := range values {
			for _, y := range values {
				for _, raw := range []bool{false, true} {
					got := bytes.Repeat([]byte{0xa5}, 112)
					want := bytes.Clone(got)
					for i := 0; i < 16; i++ {
						binary.LittleEndian.PutUint16(want[offset+i*4:], uint16(x))
						binary.LittleEndian.PutUint16(want[offset+i*4+2:], uint16(y))
					}
					dst := unsafe.Slice((*SMVUnitXY)(unsafe.Pointer(&got[offset])), 16)
					if raw {
						updateMbMvSSE2((*[16]SMVUnitXY)(dst), uint32(uint16(x))|uint32(uint16(y))<<16)
					} else {
						UpdateMbMv_sse2(dst, SMVUnitXY{x, y})
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("offset%d value%d,%d raw%t: output or canary differs", offset, x, y, raw)
					}
				}
			}
		}
	}
}

func updateMvCallPanic(call func()) (panicText string) {
	defer func() {
		if value := recover(); value != nil {
			panicText = fmt.Sprint(value)
		}
	}()
	call()
	return ""
}

func TestUpdateMbMvSIMDShortSlices(t *testing.T) {
	value := SMVUnitXY{-32768, 32767}
	for length := 0; length <= 18; length++ {
		for _, spareCapacity := range []bool{false, true} {
			var got, want [20]SMVUnitXY
			for i := range got {
				got[i] = SMVUnitXY{int16(i + 1), int16(-i - 17)}
			}
			want = got
			capacity := 1 + length
			if spareCapacity {
				capacity = len(got)
			}
			gotSlice, wantSlice := got[1:1+length:capacity], want[1:1+length:capacity]
			gotPanic := updateMvCallPanic(func() { UpdateMbMv_sse2(gotSlice, value) })
			wantPanic := updateMvCallPanic(func() { UpdateMbMv_c(wantSlice, value) })
			if got != want || gotPanic != wantPanic || (length < 16) != (gotPanic != "") {
				t.Fatalf("length%d spare%t: output/panic differs: %q / %q", length, spareCapacity, gotPanic, wantPanic)
			}
		}
	}
	gotPanic := updateMvCallPanic(func() { UpdateMbMv_sse2(nil, value) })
	wantPanic := updateMvCallPanic(func() { UpdateMbMv_c(nil, value) })
	if gotPanic == "" || gotPanic != wantPanic {
		t.Fatalf("nil panic differs: %q / %q", gotPanic, wantPanic)
	}
}

func TestUpdateMbMvSIMDCopiedValueAliases(t *testing.T) {
	for source := 0; source < 16; source++ {
		var got, want [18]SMVUnitXY
		for i := range got {
			got[i] = SMVUnitXY{int16(i*1701 - 32768), int16(i*301 + 19)}
		}
		want = got
		value := want[source+1]
		for i := 1; i <= 16; i++ {
			want[i] = value
		}
		UpdateMbMv_sse2(got[1:17], got[source+1])
		if got != want {
			t.Fatalf("source%d: value was not copied before writes", source)
		}
	}
}

func TestUpdateMbMvSIMDSkipCaller(t *testing.T) {
	flags := common.WelsCPUFeatureDetect(nil)
	for _, scenario := range []string{"Direct", "PredThreshold", "RefThreshold", "RejectLuma", "RejectU", "RejectV", "TransformSuccess"} {
		for _, useSAD := range []bool{false, true} {
			for _, mutation := range []string{"none", "finish", "sad-threshold"} {
				t.Run(fmt.Sprintf("%s/SAD%t/%s", scenario, useSAD, mutation), func(t *testing.T) {
					got := newSkipDctQuantCallerState(scenario, flags, useSAD)
					want := newSkipDctQuantCallerState(scenario, flags, useSAD)
					InitIntraAnalysisVaaInfo(&got.functions, flags)
					compareSkipDctQuantCaller(t, got, want, mutation)
				})
			}
		}
	}
	for _, alias := range []bool{false, true} {
		for length := 0; length <= 16; length++ {
			t.Run(fmt.Sprintf("partial/alias%t/length%d", alias, length), func(t *testing.T) {
				got := newSkipDctQuantCallerState("Direct", flags, true)
				want := newSkipDctQuantCallerState("Direct", flags, true)
				InitIntraAnalysisVaaInfo(&got.functions, flags)
				for _, state := range []*skipDctQuantCallerState{got, want} {
					if alias {
						state.blocks[1].sMv = state.cache.sMvComponents.sMotionVectorCache[:16]
					}
					state.blocks[1].sMv = state.blocks[1].sMv[:length:length]
				}
				gotResult, wantResult := false, false
				gotPanic := updateMvCallPanic(func() { gotResult = WelsMdPSkipEnc(&got.ctx, &got.md, &got.blocks[1], &got.cache) })
				wantPanic := updateMvCallPanic(func() { wantResult = WelsMdPSkipEnc(&want.ctx, &want.md, &want.blocks[1], &want.cache) })
				if gotPanic != wantPanic || gotResult != wantResult || (length < 16) != (gotPanic != "") ||
					!reflect.DeepEqual(got.ctx.pMvUnitBlock4x4, want.ctx.pMvUnitBlock4x4) ||
					!reflect.DeepEqual(got.blocks, want.blocks) || !reflect.DeepEqual(got.cache, want.cache) ||
					!reflect.DeepEqual(got.md, want.md) || !reflect.DeepEqual(got.decPic, want.decPic) ||
					got.enc != want.enc || got.ref != want.ref || got.prediction != want.prediction ||
					got.coefficients != want.coefficients || got.levels != want.levels {
					t.Fatalf("caller partial state differs: panic %q/%q result %t/%t", gotPanic, wantPanic, gotResult, wantResult)
				}
			})
		}
	}
}

// Count only the update callback, leaving the MC/cost/transform functions as in
// the timed caller fixture. Repeated calls cover both warmup and steady state.
func TestUpdateMbMvSkipCallerVisits(t *testing.T) {
	flags := common.WelsCPUFeatureDetect(nil)
	for _, scenario := range []string{"Direct", "PredThreshold", "RefThreshold", "RejectLuma", "RejectU", "RejectV", "TransformSuccess"} {
		for _, useSAD := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/SAD%t", scenario, useSAD), func(t *testing.T) {
				s := newSkipDctQuantCallerState(scenario, flags, useSAD)
				InitIntraAnalysisVaaInfo(&s.functions, flags)
				wantAccepted := scenario != "RejectLuma" && scenario != "RejectU" && scenario != "RejectV"
				wantCalls := 0
				if wantAccepted {
					wantCalls = 1
				}
				update := s.functions.pfUpdateMbMv
				calls := 0
				s.functions.pfUpdateMbMv = func(dst []SMVUnitXY, value SMVUnitXY) {
					calls++
					update(dst, value)
				}
				for iteration := 0; iteration < 3; iteration++ {
					calls = 0
					accepted := WelsMdPSkipEnc(&s.ctx, &s.md, &s.blocks[1], &s.cache)
					if accepted != wantAccepted || calls != wantCalls {
						t.Fatalf("iteration%d: accepted%t calls%d, want%t/%d", iteration, accepted, calls, wantAccepted, wantCalls)
					}
				}
			})
		}
	}
}
