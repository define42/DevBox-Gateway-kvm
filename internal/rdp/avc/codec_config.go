package avc

const (
	// RDP runs the AVC stream over an ordered reliable channel and explicitly
	// requests IDRs for initialization, refreshes, resizes, and encode retries.
	// Avoid periodic IDR latency and full-desktop repaints between those events.
	codecIntraPeriod          uint32 = 0
	codecLoopFilterDisableIDC int32  = 1
)

// codecConfig keeps policy alternatives private while allowing benchmarks to
// compare them through the same construction path used in production.
type codecConfig struct {
	intraPeriod          uint32
	loopFilterDisableIDC int32
}

// codecSettings reports a software backend's effective configuration and
// encoded-frame statistics, independent of which implementation is linked.
type codecSettings struct {
	threads, slices      int
	intraPeriod          uint32
	loopFilterDisableIDC int32
	maxFrameRate         float32
	layerFrameRate       float32
	layerQP              int
	averageFrameQP       uint32
	inputFrames          uint32
	lastTimestamp        int64
}

// softwareCodec is implemented by both software backends: native OpenH264
// when cgo is enabled, and the pure-Go port otherwise or with -tags purego.
type softwareCodec interface {
	codecEncoder
	settings() (codecSettings, error)
}

func newCodecEncoder(width, height, threads, frameRate, qp int) (codecEncoder, int, error) {
	return newCodecEncoderWithConfig(width, height, threads, frameRate, qp, codecConfig{
		intraPeriod:          codecIntraPeriod,
		loopFilterDisableIDC: codecLoopFilterDisableIDC,
	})
}
