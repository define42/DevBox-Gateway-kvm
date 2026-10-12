package encoder

import "testing"

func TestRcConvertQStep2QpRoundTrip(t *testing.T) {
	for q := int32(0); q < 52; q++ {
		if got := RcConvertQStep2Qp(RcConvertQp2QStep(q)); got != q {
			t.Errorf("RcConvertQStep2Qp(table[%d]) = %d", q, got)
		}
	}
	// values cross-checked against the C expression (float std::log overload)
	cases := map[int32]int32{1: 0, 63: 0, 64: 0, 1000: 24, 216978: 71, 1093501: 84}
	for step, want := range cases {
		if got := RcConvertQStep2Qp(step); got != want {
			t.Errorf("RcConvertQStep2Qp(%d) = %d, want %d", step, got, want)
		}
	}
}

func TestGetTimestampForRc(t *testing.T) {
	if got := GetTimestampForRc(100, 200, 30); got != 233 {
		t.Errorf("got %d", got)
	}
	if got := GetTimestampForRc(0, -1, 30); got != 0 {
		t.Errorf("got %d", got)
	}
	if got := GetTimestampForRc(300, 200, 30); got != 300 {
		t.Errorf("got %d", got)
	}
}

func TestRcDivRoundF32(t *testing.T) {
	// WELS_DIV_ROUND (int, float) is evaluated in float.
	if got := rcDivRoundF32(1000000, 30); got != 33333 {
		t.Errorf("got %d", got)
	}
	if got := rcDivRoundF32(5, 0); got != 5 {
		t.Errorf("got %d", got)
	}
}

func TestRcUpdatePredictedFrameBits(t *testing.T) {
	ctx := &sWelsEncCtx{pWelsSvcRc: make([]SWelsSvcRc, 1)}
	logged := false
	ctx.sLogCtx.PfLog = func(any, int32, string, []any) { logged = true }

	ctx.pWelsSvcRc[0].iFrameDqBits = 1200
	RcUpdatePredictedFrameBits(ctx)
	if got := ctx.pWelsSvcRc[0].iPredFrameBit; got != 1200 {
		t.Fatalf("initial prediction = %d, want 1200", got)
	}

	ctx.pWelsSvcRc[0].iFrameDqBits = 800
	RcUpdatePredictedFrameBits(ctx)
	if got := ctx.pWelsSvcRc[0].iPredFrameBit; got != 1000 {
		t.Fatalf("smoothed prediction = %d, want 1000", got)
	}
	if logged {
		t.Fatal("prediction update emitted a codec trace")
	}

	if allocs := testing.AllocsPerRun(100, func() {
		RcUpdatePredictedFrameBits(ctx)
	}); allocs != 0 {
		t.Fatalf("prediction update allocated %v objects, want 0", allocs)
	}
}
