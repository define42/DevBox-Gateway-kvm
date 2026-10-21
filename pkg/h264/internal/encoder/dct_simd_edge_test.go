package encoder

import (
	"bytes"
	"fmt"
	"slices"
	"testing"
)

func TestForwardDCTSSE2StrideAndPatternParity(t *testing.T) {
	type dctFunc func([]int16, []byte, int, int32, []byte, int, int32)
	for _, tc := range []struct {
		size         int
		scalar, simd dctFunc
	}{{4, WelsDctT4_c, WelsDctT4_sse2}, {8, WelsDctFourT4_c, WelsDctFourT4_sse2}} {
		for _, strides := range [][2]int{{tc.size, tc.size}, {-tc.size, tc.size}, {tc.size, -tc.size},
			{-19, -23}, {0, 0}, {0, 19}, {19, 0}, {1, 2}, {tc.size - 1, 1 - tc.size}, {19, 23}} {
			for _, pattern := range []string{"zero", "equal-max", "positive-max", "negative-max", "checkerboard", "rows", "columns", "impulse", "ramp"} {
				t.Run(fmt.Sprintf("size%d/strides%d,%d/%s", tc.size, strides[0], strides[1], pattern), func(t *testing.T) {
					var pixels [2][]byte
					var offsets [2]int
					for input, stride := range strides {
						offsets[input] = 3 + input*2
						span := (tc.size - 1) * stride
						if span < 0 {
							offsets[input] -= span
							span = -span
						}
						pixels[input] = make([]byte, 3+input*2+span+tc.size)
						for y := 0; y < tc.size; y++ {
							for x := 0; x < tc.size; x++ {
								var value byte
								switch pattern {
								case "equal-max":
									value = 255
								case "positive-max":
									if input == 0 {
										value = 255
									}
								case "negative-max":
									if input == 1 {
										value = 255
									}
								case "checkerboard":
									value = byte((x + y + input) % 2 * 255)
								case "rows":
									value = byte((y + input) % 2 * 255)
								case "columns":
									value = byte((x + input) % 2 * 255)
								case "impulse":
									if x == input*(tc.size-1) && y == (1-input)*(tc.size-1) {
										value = 255
									}
								case "ramp":
									value = byte(x*29 + y*37 + input*71)
								}
								pixels[input][offsets[input]+y*stride+x] = value
							}
						}
					}
					originalA, originalB := bytes.Clone(pixels[0]), bytes.Clone(pixels[1])
					coefficients := tc.size * tc.size
					backing := make([]int16, coefficients+4)
					for i := range backing {
						backing[i] = -12345
					}
					got := backing[2 : 2+coefficients : 2+coefficients]
					want := make([]int16, coefficients)
					tc.scalar(want, pixels[0], offsets[0], int32(strides[0]), pixels[1], offsets[1], int32(strides[1]))
					tc.simd(got, pixels[0], offsets[0], int32(strides[0]), pixels[1], offsets[1], int32(strides[1]))
					if !slices.Equal(got, want) {
						t.Fatalf("got %v want %v", got, want)
					}
					if backing[0] != -12345 || backing[1] != -12345 || backing[coefficients+2] != -12345 || backing[coefficients+3] != -12345 {
						t.Fatal("transform wrote outside coefficient output")
					}
					if !bytes.Equal(pixels[0], originalA) || !bytes.Equal(pixels[1], originalB) {
						t.Fatal("transform modified input pixels")
					}
				})
			}
		}
	}
}

func TestForwardDCTSSE2CoefficientLength(t *testing.T) {
	for _, tc := range []struct {
		size int
		fn   func([]int16, []byte, int, int32, []byte, int, int32)
	}{{4, WelsDctT4_sse2}, {8, WelsDctFourT4_sse2}} {
		t.Run(fmt.Sprintf("size%d", tc.size), func(t *testing.T) {
			coefficients := make([]int16, tc.size*tc.size)
			// Capacity must not authorize assembly stores past the visible
			// length. Both the native kernel wrapper and fallback must panic.
			coefficients = coefficients[:len(coefficients)-1]
			input := make([]byte, tc.size*tc.size)
			defer func() {
				if recover() == nil {
					t.Fatal("short coefficient slice did not panic")
				}
			}()
			tc.fn(coefficients, input, 0, int32(tc.size), input, 0, int32(tc.size))
		})
	}
}
