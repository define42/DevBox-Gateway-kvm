//go:build amd64

package encoder

import "golang.org/x/sys/cpu"

//go:noescape
func cavlcExtractSSSE3(coeff *[16]int16, run *[16]uint8, level *[16]int16, total *int32, end int32) int32

// The kernel uses writer-owned scratch arrays, disjoint from coeff and one
// another. Keep CavlcParamCal_c for callers requiring sequential alias semantics.
// Despite the native kernel's sse42 name, this kernel needs SSSE3 and POPCNT;
// neither instruction set implies the other. It uses no SSE4.2 instructions.
func cavlcExtractSupported(coeff []int16, end int32) bool {
	return uint32(end-14) <= 1 && len(coeff) >= 16 && cpu.X86.HasSSSE3 && cpu.X86.HasPOPCNT
}
