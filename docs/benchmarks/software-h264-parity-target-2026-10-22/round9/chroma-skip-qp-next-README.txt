SOURCE FROZEN: chroma-skip-qp-next
Baseline: /tmp/h264-parity-target-20261022/round7
Candidate: /tmp/h264-parity-target-20261022/chroma-skip-qp-next
No Go builds/tests/benchmarks/profiles or native compilation run. Main untouched.

Scope
-----
This is separate from the held chroma-skip-simd-next experiment. There is no
assembly or function-table change. WelsHadamardQuant2x2Skip_c stays unchanged.
The one production caller replacement passes its known chroma QP and the same
live effective ff/mf values to a private scalar helper.

A 52-entry startup snapshot stores {effective ff, effective mf, int16 threshold}
in 312 bytes. Cache hits require exact equality with both live effective
parameters and mf!=0. Changed parameters, invalid QP, and zero mf use the
original scalar function. A raw table change that leaves the effective DC
parameters equal safely uses the cache; the following AC quantizer still
reads the original live tables.

The hot helper removes only the signed division. It retains int16 wrapping at
both Hadamard stages and reads all four DC positions before deciding. Widened
signed comparisons against +/-threshold are equivalent to abs(int32(dc)) for
all int16 values, including MinInt16 and negative thresholds. Short slices
retain the scalar index/read order. mf==0 falls back before any coefficient
load, preserving division-by-zero precedence. No source/output state changes
occur during the DC decision; existing scan, callback, early-exit, and alias
behavior in the complete caller remains untouched.

Changed files
-------------
pkg/h264/internal/encoder/hadamard_skip_qp.go
pkg/h264/internal/encoder/svc_encode_mb.go
pkg/h264/internal/encoder/hadamard_skip_qp_test.go
pkg/h264/internal/encoder/hadamard_skip_qp_caller_test.go
pkg/h264/internal/encoder/hadamard_skip_caller_bench_test.go

Validation (root owns CPU scheduling)
-------------------------------------
cd /tmp/h264-parity-target-20261022/chroma-skip-qp-next
/snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestChromaSkipQP' -count=1
/snap/go/current/bin/go test -race ./pkg/h264/internal/encoder -run '^TestChromaSkipQP' -count=1
/snap/go/current/bin/go test ./pkg/h264/... -count=1
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 /snap/go/current/bin/go test -c -o /tmp/h264-parity-target-20261022/chroma-skip-qp-arm64.test ./pkg/h264/internal/encoder

Baseline-compatible benchmark
-----------------------------
Copy ONLY pkg/h264/internal/encoder/hadamard_skip_caller_bench_test.go unchanged
to the round7 baseline. It is byte-identical to the held SIMD experiment's
complete-caller benchmark, uses only existing APIs, and needs no shim.
/snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^$' -bench '^BenchmarkChromaSkipCaller$' -benchmem -count=6
36 cases: QP0/26/51 x UV1/2 x Zero/DCReject/DCBoundary/SparseAC/ACReject/Dense.
The fixture resets the 64-word residual each invocation. The same benchmark
can be used to compare against the held SIMD candidate independently.

Coverage
--------
All 52 QPs and threshold +/-1 at every DC position; all 65,536 coefficients at
every DC position under QP0/51; full-range random butterfly combinations;
unaligned views and untouched canaries; every short visible length 0..64;
invalid QP, mf0/1/negative, arbitrary signed ff/mf and exact panic parity.
Mutable ff/mf tables for all 52 QPs, including effective-parameter-preserving
raw changes. Whole caller: all 52 QPs/both planes and seven residual patterns;
short coefficients, nil fields, invalid UV, callback mutations/panic and level
aliases; changed DC/AC tables and ignored nil/custom Hadamard callbacks.

Risks/gate
----------
The extra QP lookup/equality checks may cost more than division on some paths.
Complete caller controls and frame-level measurements decide selection. Do not
claim a speedup based on this source proof. No new per-slice mutable state.

SHA256
------
06ce10f02ab7ff7c3ee9d9572b54119ba14838fccc5d0456dee68b3bc2033bbc  pkg/h264/internal/encoder/hadamard_skip_qp.go
e56b801951a86a6cea7216b4037c68a90f3d8e5d9bbe45efa8926a368fd802ed  pkg/h264/internal/encoder/svc_encode_mb.go
51724af54688ac167185db3512f7678507186fbe1a1e9acad6a2549e7de9b5b9  pkg/h264/internal/encoder/hadamard_skip_qp_test.go
3fa7def15275d5319d350960a4dfb3cffc305878e480c62d1c8df4467c1a7d82  pkg/h264/internal/encoder/hadamard_skip_qp_caller_test.go
bc974430b447d62180c8ffe52d13e16f8f164c559ad7a0aba8c532b734a8caf3  pkg/h264/internal/encoder/hadamard_skip_caller_bench_test.go
