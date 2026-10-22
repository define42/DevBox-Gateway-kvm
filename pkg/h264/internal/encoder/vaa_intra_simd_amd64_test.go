//go:build amd64

package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"golang.org/x/sys/cpu"
)

// Use direct spatial indexing and int64 reduction independently of both the
// scalar implementation's block offsets and the SIMD pairwise row reduction.
func intraVaaReference(src []byte, off int, stride int32) int32 {
	var sum, squares int64
	for by := range 4 {
		for bx := range 4 {
			var total int64
			for y := range 4 {
				for x := range 4 {
					total += int64(src[off+(by*4+y)*int(stride)+bx*4+x])
				}
			}
			mean := total / 16
			sum += mean
			squares += mean * mean
		}
	}
	return int32(squares - sum*sum/16)
}

func TestIntraVaaSIMDDispatch(t *testing.T) {
	var funcs SWelsFuncPtrList
	for _, flags := range []uint32{0, common.WELS_CPU_SSE2, common.WELS_CPU_SSSE3, 0} {
		InitIntraAnalysisVaaInfo(&funcs, flags)
		want := reflect.ValueOf(AnalysisVaaInfoIntra_c).Pointer()
		if flags&common.WELS_CPU_SSSE3 != 0 && cpu.X86.HasSSSE3 {
			want = reflect.ValueOf(AnalysisVaaInfoIntra_ssse3).Pointer()
		}
		if got := reflect.ValueOf(funcs.pfGetVarianceFromIntraVaa).Pointer(); got != want {
			t.Fatalf("flags=%x selected unexpected intra analysis", flags)
		}
		if reflect.ValueOf(funcs.pfGetMbSignFromInterVaa).Pointer() != reflect.ValueOf(MdInterAnalysisVaaInfo_c).Pointer() {
			t.Fatal("intra dispatch changed the inter-analysis callback")
		}
		wantUpdate := reflect.ValueOf(UpdateMbMv_c).Pointer()
		if flags&common.WELS_CPU_SSE2 != 0 {
			wantUpdate = reflect.ValueOf(UpdateMbMv_sse2).Pointer()
		}
		if reflect.ValueOf(funcs.pfUpdateMbMv).Pointer() != wantUpdate {
			t.Fatal("unexpected motion-vector update callback")
		}
	}
}

func TestIntraVaaSIMDParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x264a))
	for _, stride := range []int32{0, 1, 3, 7, 15, 16, 17, 31, 64, 2048, -1, -3, -16, -31} {
		t.Run(fmt.Sprintf("stride%d", stride), func(t *testing.T) {
			absStride := int(stride)
			if absStride < 0 {
				absStride = -absStride
			}
			for offset := range 8 {
				off := offset
				if stride < 0 {
					off += 15 * absStride
				}
				src := make([]byte, offset+15*absStride+16)
				for pattern := range 40 {
					rng.Read(src)
					for i := range src {
						switch pattern {
						case 0:
							src[i] = 0
						case 1:
							src[i] = 255
						case 2:
							src[i] = byte((i & 1) * 255)
						case 3:
							src[i] = byte(i*17 + i/7)
						}
					}
					before := bytes.Clone(src)
					want := intraVaaReference(src, off, stride)
					if got := AnalysisVaaInfoIntra_c(src, off, stride); got != want {
						t.Fatalf("scalar offset=%d pattern=%d: got=%d want=%d", offset, pattern, got, want)
					}
					if got := AnalysisVaaInfoIntra_ssse3(src, off, stride); got != want {
						t.Fatalf("SIMD offset=%d pattern=%d: got=%d want=%d", offset, pattern, got, want)
					}
					if !bytes.Equal(src, before) {
						t.Fatal("analysis modified input")
					}
				}
			}
		})
	}
}

func TestIntraVaaSIMDFloorAndLanes(t *testing.T) {
	const stride, off = 23, 3
	src := make([]byte, off+15*stride+16)
	for y := range 16 {
		for x := range 16 {
			at := off + y*stride + x
			for _, value := range []byte{15, 16, 31, 32, 127, 255} {
				src[at] = value
				mean := int32(value / 16)
				want := mean*mean - mean*mean/16
				if got := AnalysisVaaInfoIntra_ssse3(src, off, stride); got != want {
					t.Fatalf("pixel(%d,%d)=%d: got=%d want=%d", x, y, value, got, want)
				}
			}
			src[at] = 0
		}
	}
	// Constant blocks exercise every mean lane and the maximum variance.
	for pattern := range 3 {
		for y := range 16 {
			for x := range 16 {
				block := (y/4)*4 + x/4
				value := byte(255)
				if pattern == 0 {
					value = byte((block & 1) * 255)
				}
				if pattern == 1 {
					value = byte(block * 17)
				}
				src[off+y*stride+x] = value
			}
		}
		want := intraVaaReference(src, off, stride)
		if pattern == 0 && want != 260100 {
			t.Fatalf("maximum variance oracle=%d", want)
		}
		if pattern == 2 && want != 0 {
			t.Fatalf("constant variance oracle=%d", want)
		}
		if got := AnalysisVaaInfoIntra_ssse3(src, off, stride); got != want {
			t.Fatalf("block pattern=%d: got=%d want=%d", pattern, got, want)
		}
	}
}

func TestIntraVaaSIMDVisibleBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		off    int
		stride int32
		length int
	}{
		{"short-with-capacity", 3, 32, 3 + 15*32 + 15},
		{"short-zero-stride", 0, 0, 15},
		{"negative-offset", -1, 32, 1024},
		{"overflowing-offset", int(^uint(0) >> 1), 32, 1024},
		{"negative-stride-low-end", 15*32 - 1, -32, 1024},
		{"negative-stride-high-end", 15 * 32, -32, 15*32 + 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := make([]byte, 2048)[:tc.length]
			defer func() {
				if recover() == nil {
					t.Fatal("invalid visible span did not panic")
				}
			}()
			AnalysisVaaInfoIntra_ssse3(src, tc.off, tc.stride)
		})
	}
}
