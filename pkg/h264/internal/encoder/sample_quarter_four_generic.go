//go:build !amd64

package encoder

// The capability is never installed on this architecture. Keep the direct
// call available to common dispatch code without exposing an unchecked API.
func sampleAvgSatdFourValidated(_ *byte, _ int, _ *SQuarRefineParams, _, _ int) [4]int32 {
	panic("h264: four-candidate SATD unavailable")
}
