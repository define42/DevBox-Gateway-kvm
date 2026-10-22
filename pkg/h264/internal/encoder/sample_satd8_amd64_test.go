//go:build amd64

package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

// Normalize the input layouts and rounded average before applying the existing
// independent two-dimensional matrix reference. No SIMD butterfly is reused.
func sampleSatd8Matrix(inputs [3][]byte, offsets [3]int, strides [3]int32, height int, average bool) int32 {
	var src, ref [8 * 16]byte
	for y := range height {
		for x := range 8 {
			src[y*8+x] = inputs[0][offsets[0]+y*int(strides[0])+x]
			value := int(inputs[1][offsets[1]+y*int(strides[1])+x])
			if average {
				value = (value + int(inputs[2][offsets[2]+y*int(strides[2])+x]) + 1) >> 1
			}
			ref[y*8+x] = byte(value)
		}
	}
	var cost int32
	for y := 0; y < height; y += 4 {
		for x := 0; x < 8; x += 4 {
			cost += satdMatrix4x4(src[:], ref[:], y*8+x, 8)
		}
	}
	return cost
}

func sampleSatd8Checked(inputs [3][]byte, offsets [3]int, strides [3]int32, height int, average bool) int32 {
	if average {
		return sampleAvgSatdAVX2(inputs[0], offsets[0], strides[0], inputs[1], offsets[1], strides[1],
			inputs[2], offsets[2], strides[2], 8, int32(height))
	}
	if height == 8 {
		return WelsSampleSatd8x8_avx2(inputs[0], offsets[0], strides[0], inputs[1], offsets[1], strides[1])
	}
	if height == 16 {
		return WelsSampleSatd8x16_avx2(inputs[0], offsets[0], strides[0], inputs[1], offsets[1], strides[1])
	}
	return sampleSatdBlockAVX2(inputs[0], offsets[0], strides[0], inputs[1], offsets[1], strides[1], 8, height)
}

func TestSampleSatd8RowsMatrixParity(t *testing.T) {
	requireSampleAVX2(t)
	for _, height := range []int{4, 8, 12, 16} {
		for _, strides := range [][3]int32{{13, 19, 23}, {0, 3, 5}, {-13, -19, -23}, {13, -19, 0}} {
			t.Run(fmt.Sprintf("height%d/strides%v", height, strides), func(t *testing.T) {
				rng := rand.New(rand.NewSource(0x8a7d))
				for iteration := range 44 {
					var inputs, before [3][]byte
					var offsets [3]int
					for n, stride := range strides {
						span := (height - 1) * int(stride)
						offsets[n] = 3 + n
						if span < 0 {
							offsets[n] -= span
							span = -span
						}
						inputs[n] = make([]byte, span+8+3+n)
						rng.Read(inputs[n])
						if iteration < 4 {
							// Distinct four-row and four-column blocks expose
							// lane duplication, omission and wrong row pairing.
							for y := range height {
								for x := range 8 {
									value := byte(((y/4 + x/4 + n + iteration) % 4) * 85)
									if iteration == 0 {
										value = byte(n % 2 * 255)
									}
									inputs[n][offsets[n]+y*int(stride)+x] = value
								}
							}
						}
						before[n] = bytes.Clone(inputs[n])
					}
					for _, average := range []bool{false, true} {
						got := sampleSatd8Checked(inputs, offsets, strides, height, average)
						want := sampleSatd8Matrix(inputs, offsets, strides, height, average)
						if got != want {
							t.Fatalf("iteration=%d average=%v: got %d, want %d", iteration, average, got, want)
						}
					}
					for n := range inputs {
						if !bytes.Equal(inputs[n], before[n]) {
							t.Fatalf("input %d modified", n)
						}
					}
				}
			})
		}
	}
}

func TestSampleSatd8RowsEveryLane(t *testing.T) {
	requireSampleAVX2(t)
	for _, height := range []int{8, 12, 16} {
		t.Run(fmt.Sprintf("height%d", height), func(t *testing.T) {
			inputs := [3][]byte{make([]byte, 8*height), make([]byte, 8*height), make([]byte, 8*height)}
			strides := [3]int32{8, 8, 8}
			for input := range inputs {
				for pixel := range inputs[input] {
					for _, value := range []byte{1, 254, 255} {
						inputs[input][pixel] = value
						for _, average := range []bool{false, true} {
							got := sampleSatd8Checked(inputs, [3]int{}, strides, height, average)
							want := sampleSatd8Matrix(inputs, [3]int{}, strides, height, average)
							if got != want {
								t.Fatalf("input=%d pixel=%d value=%d average=%v: got %d, want %d", input, pixel, value, average, got, want)
							}
						}
					}
					inputs[input][pixel] = 0
				}
			}
		})
	}
}

func TestSampleSatd8RowsAliasedInputs(t *testing.T) {
	requireSampleAVX2(t)
	for _, height := range []int{4, 8, 12, 16} {
		for _, shifted := range []bool{false, true} {
			t.Run(fmt.Sprintf("height%d/shifted%v", height, shifted), func(t *testing.T) {
				backing := make([]byte, 32*height+17)
				for i := range backing {
					backing[i] = byte(i*37 + 13)
				}
				before := bytes.Clone(backing)
				inputs := [3][]byte{backing, backing, backing}
				offsets := [3]int{3, 3, 3}
				strides := [3]int32{32, 32, 32}
				if shifted {
					offsets = [3]int{3, 7, 11}
					strides = [3]int32{31, 29, 32}
				}
				for _, average := range []bool{false, true} {
					got := sampleSatd8Checked(inputs, offsets, strides, height, average)
					want := sampleSatd8Matrix(inputs, offsets, strides, height, average)
					if got != want {
						t.Fatalf("average=%v: got %d, want %d", average, got, want)
					}
				}
				if !bytes.Equal(backing, before) {
					t.Fatal("aliased inputs modified")
				}
			})
		}
	}
}

func TestSampleSatd8RowsChecksVisibleLength(t *testing.T) {
	requireSampleAVX2(t)
	for _, height := range []int{4, 8, 12, 16} {
		for _, average := range []bool{false, true} {
			inputCount := 2
			if average {
				inputCount = 3
			}
			for shortInput := range inputCount {
				t.Run(fmt.Sprintf("height%d/average%v/input%d", height, average, shortInput), func(t *testing.T) {
					inputs := [3][]byte{make([]byte, height*8), make([]byte, height*8), make([]byte, height*8)}
					inputs[shortInput] = inputs[shortInput][:height*8-1]
					defer func() {
						if recover() == nil {
							t.Fatal("missing bounds panic for short length with sufficient capacity")
						}
					}()
					sampleSatd8Checked(inputs, [3]int{}, [3]int32{8, 8, 8}, height, average)
				})
			}
		}
	}
}
