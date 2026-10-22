Single trailing-one CAVLC pending-word experiment
================================================

Source ready and frozen 2026-10-22. No builds/tests/benchmarks/profiles run by
implementing agent. Main and all other experimental trees are unchanged.

Tree: /tmp/h264-parity-target-20261022/cavlc-single-next
Baseline: exact copy of /tmp/h264-parity-target-20261022/round7 (repaired main,
limit64 packed level table, direct known-zero branch). Both baseline writer and
md.go matched current main before copying. No held end3 extraction, table128,
slow-packed, bounded-loop, or flush experiments included.

Rationale / actual scope:
- Both Go and native already combine coefficient token plus trailing signs.
  A block with one +/-1 coefficient therefore has two writes when total_zeros
  is present. This experiment combines those two writes and skips empty level/
  run setup only when their complete bit string fits without flushing.
- Parent's verified 616-frame counters: single +/-1 is 8,267,823/41,424,464
  Road calls (19.96%) and 734,356/8,217,552 PDF calls (8.94%). Road luma4x4
  contributes 5,770,019 such calls. Those counts do not measure pending-word fit.
- Existing matched sparse gap is about 42--52 ns/Go block vs 28--36 ns/native;
  sparse fixtures also contain other levels, so these values do not predict
  the gain from this specialization. Frame-level measurement remains required.

Only production change:
  pkg/h264/internal/encoder/set_mb_syn_cavlc.go
  Adds one bounded branch after existing token/sign construction. No extraction,
  general level/run loop, known-zero, table data, flush, or output arithmetic edits.

New tests:
  pkg/h264/internal/encoder/cavlc_single_test.go
  TestCavlcSingleAllPositionsAndPrefixes
  TestCavlcSingleFailures
  TestCavlcSingleAliasedWriterState
  TestCavlcSingleInputOutputAlias

Correctness invariants:
- Require totalCoeffs==1 and trailingOnes==1; no level escape/overflow or run
  symbol is reachable. Other coefficients use the unchanged writer.
- Require the existing token/sign symbol to fit strictly before inspecting the
  total-zero table. Otherwise the original first flush/partial write/panic must
  precede any invalid zero-table index, including malformed CHROMA_DC metadata.
- Require combined length strictly less than ILeftBits; exact fills and flushes
  fall through to the original writer. Counts above32 and all negative/zero
  counts also use the original path.
- The unsigned identity ((bits<<a)|token)<<b|zeros equals the combined pending
  update modulo32bits. Existing token/sign construction is retained exactly.
- Existing bs.uninit writes cursor, current bits and left count in original
  order. No bytes are emitted by the new branch, and input extraction still
  completes before any output write. Alias/partial-panic tests compare entire
  buffers and writer structs, including output aliasing writer scalar fields.
- Total-zero table is still read live, not cached or compiled into a new table.
  Its earlier read occurs only when preceding work has no external writes.

Reference tests cover all single positions/signs, all18 nC values and all32
pending positions for end0/3/14/15, scalar/SIMD extraction controls, nil/short
inputs, invalid nC, invalid chroma zero indices, short output/cursors, malformed
left counts, exact boundary fills, and input/output/writer-state aliases.
Existing full coefficient-domain/overflow/dense/known-zero suites remain intact.

Native matched fixture support (isolated only, no native source edits):
  pkg/h264/internal/encoder/cavlc_native_full_test.go (copied unchanged)
  pkg/h264/internal/encoder/cavlc_native_full_bench_test.go (copied unchanged)
  pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt (original333)
  pkg/h264/internal/encoder/testdata/cavlc_native_single_fixtures.txt (new447)
  generate_single_fixtures.py (deterministic extension generator)

The new fixture retains every original suite and adds18 timed32-block suites:
  end3/end14/end15 x single-one/single-level x prefix0/prefix17/prefix31.
Single-one spans every coefficient position with alternating +/-1, varying nC
and both end14 residual properties. Single-level uses +/-2 as a close fallback
control. Another96 untimed suites exercise single +/-1 at all initial prefixes.
Same external fixture works with existing baseline/native binaries; rebuild only
candidate. Harness files need not be merged into production main.

Parent-only commands (CPU serialized by root):

  candidate=/tmp/h264-parity-target-20261022/cavlc-single-next
  review=/tmp/h264-parity-target-20261022/cavlc-native-review
  binary="$review/single-cavlc.test"
  fixtures="$candidate/pkg/h264/internal/encoder/testdata/cavlc_native_single_fixtures.txt"
  CAVLC_GO_TREE="$candidate" CAVLC_GO_TEST="$binary" \
    bash "$review/run_review.sh" build-go
  CAVLC_NATIVE_BINARY="$review/native_cavlc" CAVLC_NATIVE_FIXTURES="$fixtures" \
    GOMAXPROCS=1 "$binary" -test.run '^Test' -test.count=1
  CAVLC_NATIVE_BINARY="$review/native_cavlc" CAVLC_NATIVE_FIXTURES="$fixtures" \
    GOMAXPROCS=1 GODEBUG=cpu.ssse3=off,cpu.popcnt=off \
    "$binary" -test.run '^TestCavlc' -test.count=1

Targeted tests can use -test.run '^TestCavlcSingle'. Native-only parity can use
'^TestCavlcNative'. Race checks and non-amd64 compile remain root decisions.

  CAVLC_GO_TEST="$binary" CAVLC_NATIVE_FIXTURES="$fixtures" \
    OUTPUT_DIR="$review/results-single" \
    BENCH_FILTER='(end3|end14|end15)/(single-one|single-level|zero|known-zero|sparse|dense|mixed)/prefix17' \
    SAMPLES=6 MILLISECONDS=300 bash "$review/run_review.sh" bench

Prefer root's rotating repaired-round7/candidate/native workflow with the same
external fixture for all three binaries. Expand final segment to
(prefix0|prefix17|prefix31) for all-prefix controls. Do not accept a single-one
microbenchmark win at the cost of broad zero/dense/mixed or full-codec regressions.
