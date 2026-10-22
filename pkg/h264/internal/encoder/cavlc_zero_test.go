package encoder

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type cavlcZeroWriter func(*SWelsFuncPtrList, []int16, int32, int32, int32, int8, *common.SBitStringAux) int32

func observeCavlcZero(writer cavlcZeroWriter, coeff []int16, end, property int32, nc int8, bits *common.SBitStringAux) (result int32, failure string) {
	defer func() {
		if value := recover(); value != nil {
			failure = fmt.Sprintf("%T: %v", value, value)
		}
	}()
	result = writer(nil, coeff, end, 0, property, nc, bits)
	return
}

// Known-zero calls must not inspect coefficients/end/property, and must retain
// the original writer-before-nC panic order. The reference is the independent
// pre-optimization arithmetic writer, not a duplicate of the new early branch.
func TestCavlcKnownZeroStateAndFailures(t *testing.T) {
	for _, nc := range []int8{-128, -1, 0, 1, 2, 3, 4, 7, 8, 16, 17, 18, 127} {
		got, gotFailure := observeCavlcZero(WriteBlockResidualCavlc, nil, -1, -1, nc, nil)
		want, wantFailure := observeCavlcZero(writeBlockResidualCavlcReference, nil, -1, -1, nc, nil)
		if got != want || gotFailure != wantFailure || gotFailure == "" {
			t.Fatalf("nil writer nC%d: got %d/%q want %d/%q", nc, got, gotFailure, want, wantFailure)
		}
	}

	for _, nc := range []int8{-1, 0, 1, 2, 3, 4, 7, 8, 16, 17, 18, 127} {
		for _, left := range []int32{-1 << 31, -3, 0, 1, 4, 8, 17, 31, 32, 64, 1<<31 - 1} {
			for length := 0; length <= 9; length++ {
				for _, cursor := range []int{-1, 0, 3, 7} {
					var gotBuffer, wantBuffer [16]byte
					for i := range gotBuffer {
						gotBuffer[i], wantBuffer[i] = 0xa5, 0xa5
					}
					gotBits := common.SBitStringAux{
						PBuf: gotBuffer[:length], PCurBuf: cursor, PStartBuf: 17, PEndBuf: -12,
						UiCurBits: 0x8d23f9ab, ILeftBits: left, IBits: 1234, IIndex: -17,
					}
					wantBits := gotBits
					wantBits.PBuf = wantBuffer[:length]
					// Nonzero input and invalid metadata must be ignored with flag0.
					coeff := [1]int16{-32768}
					got, gotFailure := observeCavlcZero(WriteBlockResidualCavlc, coeff[:], 1<<31-1, -1, nc, &gotBits)
					want, wantFailure := observeCavlcZero(writeBlockResidualCavlcReference, coeff[:], 1<<31-1, -1, nc, &wantBits)
					if got != want || gotFailure != wantFailure || gotBuffer != wantBuffer ||
						!reflect.DeepEqual(gotBits, wantBits) || cap(gotBits.PBuf) != cap(wantBits.PBuf) || coeff[0] != -32768 {
						t.Fatalf("nC%d left%d length%d cursor%d: got %d/%q %+v want %d/%q %+v, output parity=%t",
							nc, left, length, cursor, got, gotFailure, gotBits, want, wantFailure, wantBits, gotBuffer == wantBuffer)
					}
				}
			}
		}
	}
}

// A caller can provide a byte view of the writer's scalar state as output.
// Snapshotting local state before flush must preserve partial writes on panic
// and the final commit order even when those writes overwrite live fields.
func TestCavlcKnownZeroAliasedWriterState(t *testing.T) {
	span := int(unsafe.Sizeof(common.SBitStringAux{}.PCurBuf)) + 8
	for _, nc := range []int8{0, 2, 4, 8, 17} {
		for _, left := range []int32{-3, 0, 1, 4, 17, 32} {
			for length := 0; length <= span; length++ {
				for _, cursor := range []int{0, 3, 7, span - 4} {
					gotBits := common.SBitStringAux{
						PCurBuf: cursor, PStartBuf: 19, PEndBuf: -11,
						UiCurBits: 0x8d23f9ab, ILeftBits: left, IBits: 1234, IIndex: -17,
					}
					wantBits := gotBits
					gotBits.PBuf = unsafe.Slice((*byte)(unsafe.Pointer(&gotBits.PCurBuf)), span)[:length]
					wantBits.PBuf = unsafe.Slice((*byte)(unsafe.Pointer(&wantBits.PCurBuf)), span)[:length]
					got, gotFailure := observeCavlcZero(WriteBlockResidualCavlc, nil, -1, -1, nc, &gotBits)
					want, wantFailure := observeCavlcZero(writeBlockResidualCavlcReference, nil, -1, -1, nc, &wantBits)
					if got != want || gotFailure != wantFailure || !reflect.DeepEqual(gotBits, wantBits) ||
						cap(gotBits.PBuf) != cap(wantBits.PBuf) {
						t.Fatalf("aliased nC%d left%d length%d cursor%d: got %d/%q %+v want %d/%q %+v",
							nc, left, length, cursor, got, gotFailure, gotBits, want, wantFailure, wantBits)
					}
				}
			}
		}
	}
}
