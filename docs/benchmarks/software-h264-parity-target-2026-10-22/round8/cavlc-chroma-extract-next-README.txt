CAVLC chroma-DC four-coefficient extraction candidate
==================================================

Source-ready, frozen 2026-10-22. No builds/tests/benchmarks/profiles run by the
implementing agent. Main repository and native OpenH264 sources unchanged.

Tree:
  /tmp/h264-parity-target-20261022/cavlc-chroma-extract-next
Baseline:
  Verified repaired main, packed CAVLC table limit 64, direct known-zero writer.
  No table128 / slow-packed / bounded-loop / flush experiments included.

Production files to apply if validated/measured:
  pkg/h264/internal/encoder/set_mb_syn_cavlc.go (only adds end3 dispatch branch)
  pkg/h264/internal/encoder/cavlc_extract_chroma_amd64.go (new)
  pkg/h264/internal/encoder/cavlc_extract_chroma_amd64.s (new)
  pkg/h264/internal/encoder/cavlc_extract_chroma_generic.go (new)

Test files:
  pkg/h264/internal/encoder/cavlc_extract_test.go (test dispatch mirrors writer)
  pkg/h264/internal/encoder/cavlc_extract_chroma_amd64_test.go (new)
  pkg/h264/internal/encoder/cavlc_extract_chroma_guard_linux_amd64_test.go (new)
  pkg/h264/internal/encoder/cavlc_extract_chroma_writer_test.go (new)

Copied unchanged from cavlc-native-review/go for isolated validation/benchmarks:
  pkg/h264/internal/encoder/cavlc_native_full_test.go
  pkg/h264/internal/encoder/cavlc_native_full_bench_test.go
  pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt
These harness additions need not be merged into production main.

Design / invariants:
- Private cavlcExtract4SSSE3(*[4]int16, *[16]uint8, *[16]int16, *int32) int32.
- One MOVQ loads exactly 8 coefficient bytes; no input stores.
- Saturating pack only classifies zero/nonzero. The original full int16 words
  remain in a separate register for PSHUFB extraction, including -32768.
- Original four-bit mask indexes 16-entry metadata and shuffle tables.
  Metadata gives reverse-order runs, count, and total zeros. The final run
  includes leading zeros exactly like the scalar function.
- Stores only count populated levels/runs, then count; zero writes neither
  level nor run. Tests initialize all unused output elements with canaries.
- Kernel uses SSSE3 and SSE2, not POPCNT. Eligibility requires end==3 and
  visible len>=4; a larger capacity never authorizes wider reads.
- Existing end14/15 fast path is checked first and unchanged. Existing writer
  initialization, nC validation, partial-output and overflow behavior unchanged.
- Private output scratch is disjoint. Exported CavlcParamCal_c remains intact
  for externally observable sequential aliases and invalid-input behavior.
- Input may alias the bitwriter output: full extraction still finishes before
  the first bitwriter output store. Dedicated failure/alias tests exercise this.
- Generic non-amd64 capability check remains constant false.

Coverage added:
- All 16 masks with extremes/random full signed coefficients and unaligned input.
- All 65536 int16 values in each of four positions, exact output/counter canaries.
- Visible length/end eligibility controls including end14/end15.
- Linux raw guard pages before/after exact 8-byte read-only source, 32-byte level
  scratch, 16-byte run scratch and 4-byte count. All masks and boundary pairs.
- Full writer reference parity for short input, nil writer, invalid nC, short
  output, partial-bit prefixes, overflow and aliased input/output.
- Existing native 333-suite full-writer fixtures, and complete 32-block writer
  sequence benchmark for end3/end14/end15, zero/known-zero/sparse/dense/mixed,
  prefix0/17/31. End14/15 and known-zero are unchanged-path controls.

Parent-only validation commands (CPU remains serialized by root):

  candidate=/tmp/h264-parity-target-20261022/cavlc-chroma-extract-next
  review=/tmp/h264-parity-target-20261022/cavlc-native-review
  binary="$review/chroma-extract-cavlc.test"
  CAVLC_GO_TREE="$candidate" CAVLC_GO_TEST="$binary" \
    bash "$review/run_review.sh" build-go
  CAVLC_NATIVE_BINARY="$review/native_cavlc" \
    CAVLC_NATIVE_FIXTURES="$review/go/pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt" \
    GOMAXPROCS=1 "$binary" -test.run '^Test' -test.count=1
  GODEBUG=cpu.ssse3=off,cpu.popcnt=off CAVLC_GO_TEST="$binary" \
    CAVLC_GODEBUG=cpu.ssse3=off,cpu.popcnt=off \
    bash "$review/run_review.sh" parity
  GODEBUG=cpu.popcnt=off CAVLC_GO_TEST="$binary" \
    CAVLC_GODEBUG=cpu.popcnt=off bash "$review/run_review.sh" parity

The last setting intentionally keeps new end3 SSSE3 extraction enabled while
old 16-coefficient POPCNT-dependent extraction falls back. Also run the new
TestCavlcExtract4 tests under those settings if checking feature dispatch alone.
A non-amd64 cross-compile and normal package race checks are appropriate if kept.

Complete writer timings (compare to repaired-main limit64 binary, not round6):

  CAVLC_GO_TEST="$binary" OUTPUT_DIR="$review/results-chroma-extract" \
    BENCH_FILTER='(end3|end14|end15)/(zero|known-zero|sparse|dense|mixed)/prefix17' \
    SAMPLES=6 MILLISECONDS=300 bash "$review/run_review.sh" bench

Use root's rotating baseline/candidate/native timing workflow for acceptance.
Expanded prefix controls: set BENCH_FILTER to the same expression with
(prefix0|prefix17|prefix31) as its final segment. These are whole writer sequence
benchmarks, including extraction and output state, not isolated table lookups.
No speedup is asserted before measurement; check end14/15 controls for dispatch
or compiler-layout regressions even though their arithmetic/kernel is unchanged.
