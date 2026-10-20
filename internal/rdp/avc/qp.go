package avc

// MinQP and MaxQP bound the H.264 quantizer supported by both encoders. Lower
// values retain more detail; higher values typically reduce bandwidth. A QP
// setting controls quantization, not a fixed bitrate or a bandwidth limit.
const (
	MinQP     = 0
	MaxQP     = 51
	DefaultQP = 20
)

// ResolveQP validates an optional quantizer. Nil preserves DefaultQP, while a
// pointer to zero explicitly selects MinQP.
func ResolveQP(qp *int) (int, error) {
	if qp == nil {
		return DefaultQP, nil
	}
	if *qp < MinQP || *qp > MaxQP {
		return 0, ErrQP
	}

	return *qp, nil
}
