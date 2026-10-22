CAVLC trailing-one classification experiment — source frozen 2026-10-22

Candidate: /tmp/h264-parity-target-20261022/cavlc-trailing-next
Baseline: /tmp/h264-parity-target-20261022/round10-next
Main repository is untouched. No Go/native build, test, benchmark, profiler,
formatter or executable has been run by this agent for this experiment.

PRODUCTION SCOPE
Only pkg/h264/internal/encoder/set_mb_syn_cavlc.go changes: remove iCount and
replace the min(total,3) variable-index trailing-one loop with three guarded
fixed-position checks. An int16 level is accepted only if exactly +1 or -1;
its uint16 top bit supplies the existing sign. The nesting preserves first
non-one termination, total bounds, three-one cap and reverse-scan sign order.
The later uses of loop variable i assign it before reading it.

There are no changes to known-zero handling, extraction, scratch initialization,
BS initialization/write/flush/commit, level lookup/escape, run/zero coding,
public function signature or dispatch. Classification has no external writes,
and consumes the same initialized scratch prefix after extraction completes.
Nil/short coefficients, invalid nC/pending state, output aliases and partial
flush failures retain the old ordering. No new table or state is introduced.

SOURCE/MOTIVATION
Native codec/encoder/core/src/set_mb_syn_cavlc.cpp:131-142 expresses the same
bounded loop; the earlier remaining-gap review recorded native compilation
unrolling it while Go retained a loop and variable-index checks. This experiment
makes those three positions explicit in Go. Root should inspect current object
code, then measure complete writers; source reasoning alone establishes no win.
It is distinct from the rejected level/run range-bound rewrite, flush rewrites,
whole-MB scratch reuse, end3 extraction, table128 and single-symbol branches.
A larger branch body/code-layout or register-pressure regression is the principal
performance risk. No speedup claim is made before repeated controlled results.

ADDED TEST / BENCHMARK SCOPE
cavlc_trailing_test.go (candidate parity tests, baseline-compatible):
- TestCavlcTrailingPrefixSignMatrix: end3/14/15; every total coefficient count;
  trailing prefix 0..3; every sign pattern; non-one and signed-extreme stoppers;
  fourth +/-1 after three signs; sparse/contiguous placement; nC classes;
  partial bit accumulators; exact visible input lengths and ignored end14 tail.
  Complete output/error/state compared against the independent arithmetic writer.
- TestCavlcTrailingDirectSignBits: independent exact bit expectations assembled
  from chosen signs plus token/total-zero tables for one/two/three-only +/-1.
- TestCavlcTrailingFailureAndAliasParity: all four trailing prefix lengths with
  both sign directions, small/extreme following levels, malformed nC/pending
  states, short output/cursors and coefficient/output aliases. Captures panic,
  return, all bit-writer fields, output bytes and coefficient mutations.

cavlc_scratch_bench_test.go is copied byte-for-byte from the held scratch
experiment as a fixture only. It needs no scratch implementation or shim.
BenchmarkCavlcMacroblock has 40 complete-MB cases: ten shapes (0,1,2,4,6,10,16,
17,26,27 coded blocks) times Zero/Sparse/Dense/Mixed, with 17 pending bits.
Existing BenchmarkCavlcLevelWriter and other public-writer controls are unchanged.

MEASUREMENT-ONLY ADDITIONS: EXCLUDE THESE THREE FROM ANY MAIN MERGE
cavlc_native_full_test.go, cavlc_native_full_bench_test.go and
 testdata/cavlc_native_fixtures.txt are unchanged copies from
 /tmp/h264-parity-target-20261022/cavlc-native-review/go/pkg/h264/internal/encoder.
They provide native/reference full-state parity and 32-block continuous writer
throughput. The default fixture is the original 333-suite corpus. The expanded
447-suite corpus can instead be selected through the absolute environment path
below. Both native source and native executable are unchanged/read-only.

ROOT VALIDATION COMMANDS (CPU SERIALIZED BY ROOT)
cd /tmp/h264-parity-target-20261022/cavlc-trailing-next
/snap/go/current/bin/gofmt -w pkg/h264/internal/encoder/cavlc_trailing_test.go
CAVLC_NATIVE_BINARY=/tmp/h264-parity-target-20261022/cavlc-native-review/native_cavlc CAVLC_NATIVE_FIXTURES=/tmp/h264-parity-target-20261022/cavlc-single-next/pkg/h264/internal/encoder/testdata/cavlc_native_single_fixtures.txt CGO_ENABLED=0 GOMAXPROCS=2 /snap/go/current/bin/go test -tags integration ./pkg/h264/internal/encoder -run '^TestCavlc' -count=1
GODEBUG=cpu.all=off CGO_ENABLED=0 GOMAXPROCS=2 /snap/go/current/bin/go test -tags integration ./pkg/h264/internal/encoder -run '^TestCavlcTrailing' -count=1

Baseline preparation: use a new isolated copy of round10-next, then copy ALL four
candidate *_test.go additions and testdata/cavlc_native_fixtures.txt into that
copy. Do not change frozen round10-next. The targeted tests and benchmark/harness
fixtures are baseline-compatible, so both measured binaries can contain identical
test sources. Native harness files must be copied together.

ROOT COMPONENT COMMANDS (RUN IN BOTH ISOLATED TREES, ROTATING PAIRED ORDER)
CGO_ENABLED=0 GOMAXPROCS=2 /snap/go/current/bin/go test -tags integration ./pkg/h264/internal/encoder -run '^$' -bench '^(BenchmarkCavlcMacroblock|BenchmarkCavlcLevelWriter)$' -benchtime=200ms -count=1
CAVLC_NATIVE_FIXTURES=/tmp/h264-parity-target-20261022/cavlc-native-review/go/pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt CGO_ENABLED=0 GOMAXPROCS=2 /snap/go/current/bin/go test -tags integration ./pkg/h264/internal/encoder -run '^$' -bench '^BenchmarkCavlcNativeFull$/^end(3|14|15)$/^(zero|known-zero|sparse|dense|mixed)$/^prefix17$' -benchtime=200ms -count=1

Use existing root CPU affinity/environment and at least six rotating paired runs
with benchstat, preserving zero/known-zero, sparse, dense and mixed controls.
If component results support retention, run the existing full codec/frame hash,
decode and matched native timing workflow; component gains alone do not establish
frame improvement. Baseline here is round10-next, not original, round7 or round9.

FROZEN SOURCE SHA256 (before root gofmt)
d096c0adc38f9c5d149322f95791ea24c9bc279c27eb5722f9b66240598a103b  pkg/h264/internal/encoder/set_mb_syn_cavlc.go
ad91ddb4fed5cdbd26992829eeee7df637aeacd66c91a9816e918f5418b2559c  pkg/h264/internal/encoder/cavlc_trailing_test.go
e8c43dc4545aa3566d0be1345b626282deedaa5126398fa528b45fbc7acb997f  pkg/h264/internal/encoder/cavlc_scratch_bench_test.go
fee581e7a1cc34d112d0642036991f4aff84df56f9d3591e99864195e6e38fd7  pkg/h264/internal/encoder/cavlc_native_full_test.go
2538502df865e1f56e0e9c652ddf37a4d07606b8aa3fc05afecc15cab0a83893  pkg/h264/internal/encoder/cavlc_native_full_bench_test.go
c40cda2ad2d66c1e398e195d9a8f6658d461c88735354c9211cb773b556e195a  pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt

Baseline production SHA256
1306e558d25934d27d5ac50e14f6c6b01ffb956fcccd83ab5d8f4203485973b2  pkg/h264/internal/encoder/set_mb_syn_cavlc.go
