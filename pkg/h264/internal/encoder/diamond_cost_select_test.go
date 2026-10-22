package encoder

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func diamondCostSelectReference(iSadCost []int32, kpMvdCost []uint16, iMvdCostOff int, pBestCost *int32, kiDx int32, kiDy int32, pIx *int32, pIy *int32) bool {
	var iTempSadCost [4]int32
	iInputSadCost := *pBestCost
	iTempSadCost[0] = iSadCost[0] + COST_MVD(kpMvdCost, iMvdCostOff, kiDx, kiDy-4)
	iTempSadCost[1] = iSadCost[1] + COST_MVD(kpMvdCost, iMvdCostOff, kiDx, kiDy+4)
	iTempSadCost[2] = iSadCost[2] + COST_MVD(kpMvdCost, iMvdCostOff, kiDx-4, kiDy)
	iTempSadCost[3] = iSadCost[3] + COST_MVD(kpMvdCost, iMvdCostOff, kiDx+4, kiDy)

	if iTempSadCost[0] < *pBestCost {
		*pBestCost = iTempSadCost[0]
		*pIx = 0
		*pIy = 1
	}

	if iTempSadCost[1] < *pBestCost {
		*pBestCost = iTempSadCost[1]
		*pIx = 0
		*pIy = -1
	}

	if iTempSadCost[2] < *pBestCost {
		*pBestCost = iTempSadCost[2]
		*pIx = 1
		*pIy = 0
	}

	if iTempSadCost[3] < *pBestCost {
		*pBestCost = iTempSadCost[3]
		*pIx = -1
		*pIy = 0
	}
	return *pBestCost == iInputSadCost
}

func TestDiamondCostSelectStateParity(t *testing.T) {
	r := rand.New(rand.NewSource(741))
	table := make([]uint16, 2048)
	for i := range table {
		table[i] = uint16(r.Uint32())
	}
	for iteration := 0; iteration < 12000; iteration++ {
		initial := [8]int32{}
		for i := range initial {
			initial[i] = int32(r.Uint32())
		}
		if iteration%5 == 0 {
			initial[1] = initial[0]
			initial[2] = initial[0]
			initial[3] = initial[0]
		}
		// Include aliased best/x/y pointers and aliases with the input SAD costs.
		best, x, y := r.Intn(8), r.Intn(8), r.Intn(8)
		dx, dy := int32(r.Intn(1800)-900), int32(r.Intn(1800)-900)
		got, want := initial, initial
		gotStop := WelsMeSadCostSelect(got[:4], table, 1024, &got[best], dx, dy, &got[x], &got[y])
		wantStop := diamondCostSelectReference(want[:4], table, 1024, &want[best], dx, dy, &want[x], &want[y])
		if gotStop != wantStop || got != want {
			t.Fatalf("iteration=%d pointers=%d/%d/%d got=%v/%t want=%v/%t", iteration, best, x, y, got, gotStop, want, wantStop)
		}
	}
}

func TestDiamondCostSelectFailureParity(t *testing.T) {
	for sadLen := 0; sadLen <= 4; sadLen++ {
		for _, tableLen := range []int{0, 1, 7, 8, 9, 12, 16} {
			for _, dx := range []int32{-100, -4, 0, 4, 100, 2147483647, -2147483648} {
				for _, dy := range []int32{-4, 0, 4} {
					var table [16]uint16
					got, want := [8]int32{40, 30, 20, 10, 100, -7, -8, 999}, [8]int32{40, 30, 20, 10, 100, -7, -8, 999}
					run := func(fn func([]int32, []uint16, int, *int32, int32, int32, *int32, *int32) bool, state *[8]int32) (result bool, panicValue any) {
						defer func() { panicValue = recover() }()
						return fn(state[:sadLen], table[:tableLen], 8, &state[4], dx, dy, &state[5], &state[6]), nil
					}
					gb, gp := run(WelsMeSadCostSelect, &got)
					wb, wp := run(diamondCostSelectReference, &want)
					if gb != wb || got != want || reflect.TypeOf(gp) != reflect.TypeOf(wp) || fmt.Sprint(gp) != fmt.Sprint(wp) {
						t.Fatalf("sad=%d table=%d dx=%d dy=%d: got=%v/%t/%v want=%v/%t/%v", sadLen, tableLen, dx, dy, got, gb, gp, want, wb, wp)
					}
				}
			}
		}
	}
}
