package encoder

import (
	"bytes"
	"fmt"
	"math/bits"
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestReconstructionCBPParity(t *testing.T) {
	for _, mode := range []uint32{common.MB_TYPE_16x16, common.MB_TYPE_16x8, common.MB_TYPE_8x16, common.MB_TYPE_8x8, common.MB_TYPE_8x8_REF0, common.MB_TYPE_INTRA_BL, common.MB_TYPE_DIRECT} {
		for cbp := uint8(0); cbp < 48; cbp++ {
			t.Run(fmt.Sprintf("mode=%d/cbp=%d", mode, cbp), func(t *testing.T) {
				rng := rand.New(rand.NewSource(483))
				ordinary := mode != common.MB_TYPE_INTRA_BL && mode != common.MB_TYPE_DIRECT
				coeff := make([]int16, 384)
				for group := 0; group < 6; group++ {
					active := !ordinary || (group < 4 && cbp&(1<<group) != 0) || (group >= 4 && cbp>>4 != 0)
					if active {
						for i := 0; i < 64; i++ {
							if !ordinary || group < 4 || cbp>>4 > 1 || i%16 == 0 {
								coeff[group*64+i] = int16(rng.Uint32())
							}
						}
					}
				}
				var funcs SWelsFuncPtrList
				WelsInitReconstructionFuncs(&funcs, common.WELS_CPU_SSE2)
				selected := funcs.pfIDctFourT4
				calls := 0
				funcs.pfIDctFourT4 = func(rec []byte, recOff int, recStride int32, pred []byte, predOff int, predStride int32, c []int16) {
					calls++
					selected(rec, recOff, recStride, pred, predOff, predStride, c)
				}
				ctx := sWelsEncCtx{pFuncList: &funcs}
				dq := SDqLayer{pDecPic: &SPicture{iLineSize: [3]int32{23, 13, 13}}}
				slice := SSlice{}
				slice.sMbCacheInfo.pCoeffLevel = coeff
				mb := SMB{uiMbType: mode, uiCbp: cbp}
				var want [3][]byte
				for plane, size := range []int{16, 8, 8} {
					buf := make([]byte, 3+(size-1)*int(dq.pDecPic.iLineSize[plane])+size+11)
					rng.Read(buf)
					want[plane] = bytes.Clone(buf)
					slice.sMbCacheInfo.SPicData.pDecMb[plane] = buf
					slice.sMbCacheInfo.SPicData.iDecMbOff[plane] = 3
				}
				WelsIDctT4RecOnMb(want[0], 3, 23, want[0], 3, 23, coeff, WelsIDctFourT4Rec_c)
				WelsIDctFourT4Rec_c(want[1], 3, 13, want[1], 3, 13, coeff[256:])
				WelsIDctFourT4Rec_c(want[2], 3, 13, want[2], 3, 13, coeff[320:])
				OutputPMbWithoutConstructCsRsNoCopy(&ctx, &dq, &slice, &mb)
				wantCalls := 6
				if ordinary {
					wantCalls = bits.OnesCount8(cbp & 15)
					if cbp>>4 != 0 {
						wantCalls += 2
					}
				}
				if calls != wantCalls {
					t.Fatalf("transform calls=%d want=%d", calls, wantCalls)
				}
				for plane := range want {
					if !bytes.Equal(slice.sMbCacheInfo.SPicData.pDecMb[plane], want[plane]) {
						t.Fatalf("plane %d differs", plane)
					}
				}
			})
		}
	}
}
