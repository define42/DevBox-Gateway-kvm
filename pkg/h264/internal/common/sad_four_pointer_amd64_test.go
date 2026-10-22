//go:build amd64

package common

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"
	"unsafe"
)

// This preserves the checked slice-based wrapper from round10. The scalar
// oracle consumes all input before storing costs, independently of assembly.
func sadFourPointerLegacy(a []byte, aOff int, aStride int32, b []byte, bOff int, bStride int32, width, height int, sad []int32) {
	out := (*[4]int32)(sad)
	if aStride < 0 || bStride < 0 {
		panic(simdBoundsPanic)
	}
	aEnd := aOff + (height-1)*int(aStride) + width
	checkSIMDSpan(a, int64(aOff), int64(aEnd-1))
	aBlock := a[aOff:aEnd]
	padding := max(int(bStride), 1)
	bEnd := bOff + (height-1)*int(bStride) + width + padding
	checkSIMDSpan(b, int64(bOff-padding), int64(bEnd-1))
	_ = b[bOff:bEnd]
	*out = sadFourReference(aBlock, 0, int(aStride), b, bOff, int(bStride), width, height)
}

func TestSampleSadFourPointerOutputAliases(t *testing.T) {
	for _, tc := range sadFourSIMDCases {
		for _, strides := range [][2]int{{0, 0}, {1, 1}, {tc.width, tc.width}, {32, 33}, {33, 0}, {0, 32}} {
			for _, inputAlias := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/strides%d_%d/shared%t", tc.name, strides[0], strides[1], inputAlias), func(t *testing.T) {
					const sourceOff = 128
					refStart := 2048
					if inputAlias {
						refStart = sourceOff
					}
					refOff := refStart + max(strides[1], 1)
					sourceEnd := sourceOff + (tc.height-1)*strides[0] + tc.width
					refEnd := refOff + (tc.height-1)*strides[1] + tc.width + max(strides[1], 1)
					for _, rawOutOff := range []int{sourceOff, sourceEnd - 4, refStart, refOff - 1, refOff + 1, refOff + strides[1], refEnd - 4} {
						// Use aligned storage; only the explicitly constructed test
						// views alias bytes with the four int32 results.
						storage := make([]uint64, 512)
						pixels := unsafe.Slice((*byte)(unsafe.Pointer(&storage[0])), len(storage)*8)
						for i := range pixels {
							pixels[i] = byte((i*73 + i*i*11 + 19) >> 3)
						}
						outOff := rawOutOff &^ 3
						out := unsafe.Slice((*int32)(unsafe.Pointer(&pixels[outOff])), 4)
						want := bytes.Clone(pixels)
						costs := sadFourReference(pixels[:sourceEnd:sourceEnd], sourceOff, strides[0], pixels[:refEnd:refEnd], refOff, strides[1], tc.width, tc.height)
						for i, cost := range costs {
							binary.LittleEndian.PutUint32(want[outOff+i*4:], uint32(cost))
						}
						tc.batched(pixels[:sourceEnd:sourceEnd], sourceOff, int32(strides[0]), pixels[:refEnd:refEnd], refOff, int32(strides[1]), out)
						if !bytes.Equal(pixels, want) {
							t.Fatalf("output offset=%d: got=%v want=%v, or writes escaped the output", outOff, out, costs)
						}
					}
				})
			}
		}
	}
}

func TestSampleSadFourPointerFailureOrder(t *testing.T) {
	const maxInt = int(^uint(0) >> 1)
	for _, tc := range sadFourSIMDCases {
		t.Run(tc.name, func(t *testing.T) {
			stride := tc.width + 3
			aLen := (tc.height-1)*stride + tc.width
			bLen := (tc.height+1)*stride + tc.width
			type inputs struct {
				aLen, aOff int
				aStride    int32
				bLen, bOff int
				bStride    int32
			}
			valid := inputs{aLen, 0, int32(stride), bLen, stride, int32(stride)}
			cases := []struct {
				name string
				in   inputs
			}{
				{"valid", valid},
				{"nil inputs", inputs{0, 0, 0, 0, 0, 0}},
				{"negative source stride", inputs{aLen, 0, -1, bLen, stride, int32(stride)}},
				{"negative reference stride before invalid source", inputs{0, -1, 0, bLen, stride, -1}},
				{"both negative", inputs{0, -1, -1, 0, -1, -1}},
				{"source start", inputs{aLen, -1, int32(stride), bLen, stride, int32(stride)}},
				{"source tail len below cap", inputs{aLen - 1, 0, int32(stride), bLen, stride, int32(stride)}},
				{"reference start", inputs{aLen, 0, int32(stride), bLen, stride - 1, int32(stride)}},
				{"reference tail len below cap", inputs{aLen, 0, int32(stride), bLen - 1, stride, int32(stride)}},
				{"source offset overflow", inputs{aLen, maxInt - 1, int32(stride), bLen, stride, int32(stride)}},
				{"source minimum offset", inputs{aLen, -maxInt - 1, int32(stride), bLen, stride, int32(stride)}},
				{"reference offset overflow", inputs{aLen, 0, int32(stride), bLen, maxInt - 1, int32(stride)}},
				{"reference minimum offset", inputs{aLen, 0, int32(stride), bLen, -maxInt - 1, int32(stride)}},
				{"maximum strides", inputs{aLen, 0, 2147483647, bLen, stride, 2147483647}},
				{"zero stride left halo", inputs{aLen, 0, 0, bLen, 0, 0}},
				{"zero strides valid", inputs{tc.width, 0, 0, tc.width + 2, 1, 0}},
				{"overlap strides valid", inputs{tc.height + tc.width - 1, 0, 1, tc.height + tc.width + 1, 1, 1}},
			}
			for _, test := range cases {
				for outLen := 0; outLen <= 5; outLen++ {
					for _, outputAlias := range []bool{false, true} {
						run := func(legacy bool) (pixels []byte, output []int32, panicValue any) {
							storage := make([]uint64, 256)
							pixels = unsafe.Slice((*byte)(unsafe.Pointer(&storage[0])), len(storage)*8)
							for i := range pixels {
								pixels[i] = byte(i*39 + i*i + 17)
							}
							output = []int32{-7, -11, -13, -17, -19}
							if outputAlias {
								output = unsafe.Slice((*int32)(unsafe.Pointer(&pixels[1024])), 5)
							}
							in := test.in
							a := pixels[:in.aLen]
							ref := pixels[1024 : 1024+in.bLen]
							defer func() { panicValue = recover() }()
							if legacy {
								sadFourPointerLegacy(a, in.aOff, in.aStride, ref, in.bOff, in.bStride, tc.width, tc.height, output[:outLen])
							} else {
								tc.batched(a, in.aOff, in.aStride, ref, in.bOff, in.bStride, output[:outLen])
							}
							return pixels, output, nil
						}
						got, gotOut, gotPanic := run(false)
						want, wantOut, wantPanic := run(true)
						if !bytes.Equal(got, want) || !reflect.DeepEqual(gotOut, wantOut) || reflect.TypeOf(gotPanic) != reflect.TypeOf(wantPanic) || fmt.Sprint(gotPanic) != fmt.Sprint(wantPanic) {
							t.Fatalf("%s output=%d alias=%t: got=%v/%v want=%v/%v or input writes differ", test.name, outLen, outputAlias, gotOut, gotPanic, wantOut, wantPanic)
						}
					}
				}
			}
		})
	}
}
