SOURCE FROZEN: cavlc-scratch-next
Baseline: /tmp/h264-parity-target-20261022/round8
Candidate: /tmp/h264-parity-target-20261022/cavlc-scratch-next
Main untouched. No Go builds/tests/benchmarks/profiles/gopls or native builds run.

Production scope
----------------
One shared private entropy writer. Its full body is the round8 block writer,
with only its unused function-list argument replaced by a scratch pointer,
local level/run arrays replaced by pointers into that scratch, and corresponding
extractor arguments adjusted. A source-only normalization check verified that
all coding and bitstream logic is otherwise byte-for-byte identical.

WelsWriteMbResidual owns one 48-byte stack scratch and reuses it across the ten
existing ordered call sites. There is no per-slice/global mutable scratch,
no additional input/context snapshots, no callback changes and no persistent
bitstream state. Both scalar and SIMD extractors overwrite all level/run
entries consumed by the writer; dirty unused tails are never read.

The public WriteBlockResidualCavlc API remains present. Its known-zero fast path
is retained verbatim; nonzero calls supply local scratch to the private writer.
This avoids two full entropy implementations. The small known-zero branch also
exists in the private writer for direct whole-MB calls.

Each block still initializes its own bitstream state and commits only on success.
The error/partial-write/public-API contracts and sequential coefficient/context
loads remain unchanged. Scratch is disjoint from all visible input/output state.

Changed files
-------------
pkg/h264/internal/encoder/cavlc_scratch.go
pkg/h264/internal/encoder/set_mb_syn_cavlc.go
pkg/h264/internal/encoder/svc_set_mb_syn_cavlc.go
pkg/h264/internal/encoder/cavlc_scratch_bench_test.go
pkg/h264/internal/encoder/cavlc_scratch_reference_test.go
pkg/h264/internal/encoder/cavlc_scratch_test.go

Validation (root owns CPU scheduling)
-------------------------------------
cd /tmp/h264-parity-target-20261022/cavlc-scratch-next
/snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestCavlc' -count=1
/snap/go/current/bin/go test -race ./pkg/h264/internal/encoder -run '^TestCavlcScratch' -count=1
GODEBUG=cpu.ssse3=off,cpu.popcnt=off /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestCavlcScratch' -count=1
/snap/go/current/bin/go test ./pkg/h264/... -count=1
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 /snap/go/current/bin/go test -c -o /tmp/h264-parity-target-20261022/cavlc-scratch-arm64.test ./pkg/h264/internal/encoder

Baseline-compatible complete-MB benchmark
-----------------------------------------
Copy ONLY pkg/h264/internal/encoder/cavlc_scratch_bench_test.go unchanged to an
isolated round8 baseline. It uses only pre-existing types/APIs and needs no shim.
/snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^$' -bench '^BenchmarkCavlcMacroblock$' -benchmem -count=6
40 cases: 0,1,2,4,6,10,16,17,26,27 block writes x Zero/Sparse/Dense/Mixed.
Includes CBP0 no-residual and known-zero group controls. Ordinary I16/inter/
chroma patterns use actual coefficient counts; deliberately coded zero groups
are overhead controls. Every MB starts with17pending bits and includes writer
state reset in the timed operation. Input coefficients remain untouched.

Public-wrapper controls already present in round8 and candidate
--------------------------------------------------------------
/snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^$' -bench '^(BenchmarkCavlcResidual|BenchmarkCavlcExtractionResidual|BenchmarkCavlcLevelWriter)$' -benchmem -count=6
Use the existing matched native full-writer fixtures if the extra public call
needs closer sparse/mixed/dense comparison. Do not infer frame benefit from a
private-only writer benchmark.

Tests
-----
Independent frozen round8 whole-MB AND full block references. All40complete
fixtures across pending prefixes0/1/17/31; dirty scratch sequences alternating
dense/extracted-zero/known-zero/sparse/full-range overflow with scalar and SIMD
end14/15 extraction plus end3; overflow at each of27ordered I16All blocks;
exact return/panic/cursor/bits/input/output/cache state; short output lengths
0..65; invalid nC/cursor/left and nil writer/DCT combinations; output aliasing
coefficient storage, neighbor counts and pending writer state; ignored custom
extractor callback and zero steady-state allocations. Existing public CAVLC
full-int16/malformed/known-zero/alias/flush parity tests run unchanged.

Measurable risks
----------------
The public nonzero writer now has one extra Go call and could regress; the
macroblock path supplies scratch directly. Pointer-based scratch can change
register allocation enough to outweigh the three avoided vector zero stores.
Each MB now initializes scratch once even if every coded block is known-zero
or CBP==0. The maximum plausible frame win is modest: this is not a claim that
scratch zeroing explains most of the remaining native CAVLC gap. Keep only if
complete-MB and full-codec results justify it without harmful controls.

Independent source review is pending: motion agent is finishing another bounded
prototype first. Root may arrange another reviewer.

SHA256
------
15fe43d18038f033fd084989e800a1fac47eeae6bbefddd608101636a3875ad3  pkg/h264/internal/encoder/cavlc_scratch.go
1937c51b752a75a0897fc67521208275573da2862c573d45f9412673d1749cba  pkg/h264/internal/encoder/set_mb_syn_cavlc.go
c173145a575bd131d9b640b30107ed593699708c2acfdf3b0e9dd2d5013733a9  pkg/h264/internal/encoder/svc_set_mb_syn_cavlc.go
e8c43dc4545aa3566d0be1345b626282deedaa5126398fa528b45fbc7acb997f  pkg/h264/internal/encoder/cavlc_scratch_bench_test.go
6d137785493e16c7c8385d9eefcb971b7733bfc349f4870cd33272b7c14eead2  pkg/h264/internal/encoder/cavlc_scratch_reference_test.go
181a262549cd770fadf848aab13c3479477b7aecfb6446427073d8c810cc69e1  pkg/h264/internal/encoder/cavlc_scratch_test.go
