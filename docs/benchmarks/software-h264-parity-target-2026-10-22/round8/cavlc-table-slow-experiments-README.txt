Two independent source-only CAVLC experiments; do not merge before matched measurement.

Both trees copied repaired main go.mod, go.sum, pkg/, internal/ before editing. Native source and main remain untouched.
No builds, tests, benchmarks, profiles or runtime checks were run. gofmt only.

Baseline source hashes:
  1306e558d25934d27d5ac50e14f6c6b01ffb956fcccd83ab5d8f4203485973b2  pkg/h264/internal/encoder/set_mb_syn_cavlc.go
  e064f736e619ce0b9aa8705cec88cab5983c35d50df08e85d2f29f052fd18823  pkg/h264/internal/encoder/cavlc_fast.go
  8fecf77f0e5e869c83a6b5162591a186c9de6d401553435ecc72cd7641ca1f86  pkg/h264/internal/encoder/wels_func_ptr_def_h.go
  ef72addc8b9d04e4ad747989f044b796d0de24fc9d3af83bdb3ecf1c3389d4fc  pkg/h264/internal/encoder/svc_motion_estimate.go

cavlc-table128-next production change:
  pkg/h264/internal/encoder/cavlc_fast.go: cavlcLevelLimit 64 -> 128; table storage 8 -> 16 KiB. Writer and escape helper unchanged.
  Existing full signed-domain and all internal suffix tests remain; cavlc_packed_test.go adds +/-128 boundary transitions through complete writers.
  The additional table range is [-128,127], including 64/-65 from the mixed native fixture. 128/-129 and larger retain existing escape/overflow.
  Risk to measure: additional 8 KiB L1 data footprint may hurt full encoder despite component gains.

cavlc-slow-packed-next production changes:
  pkg/h264/internal/encoder/cavlc_fast.go: new cavlcLevelPackedSlow returns uint32+bool using identical arithmetic; old structured helper unchanged for table generation/reference.
  pkg/h264/internal/encoder/set_mb_syn_cavlc.go: escape branch consumes packed return directly. Limit remains64; lookup/flush/loops/extraction untouched.
  Direct return retains uint8(length) and uint8(nextSuffix) conversions, plus full uint32(code), so unusual arguments keep old pack results too.
  New cavlc_slow_packed_test.go: TestCavlcSlowPackedDomain covers all65536 int16 values x7 suffixes x2 adjustments; TestCavlcSlowPackedUnusualArguments checks wide coefficients, negative/large suffixes and overflow.

Common harness/bench files in both trees:
  pkg/h264/internal/encoder/cavlc_packed_bench_test.go: adds whole-writer boundary128 case; baseline-compatible. Copy identical bench to baseline before comparing.
  pkg/h264/internal/encoder/cavlc_native_full_test.go
  pkg/h264/internal/encoder/cavlc_native_full_bench_test.go
  pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt

Root-run commands (serial only; set candidate name/binary for each tree):
  review=/tmp/h264-parity-target-20261022/cavlc-native-review
  candidate=/tmp/h264-parity-target-20261022/cavlc-table128-next
  binary=$review/table128-cavlc.test
  CAVLC_GO_TREE="$candidate" CAVLC_GO_TEST="$binary" bash "$review/run_review.sh" build-go
  CAVLC_NATIVE_BINARY="$review/native_cavlc" CAVLC_NATIVE_FIXTURES="$review/go/pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt" GOMAXPROCS=1 "$binary" -test.run "^TestCavlc" -test.count=1
  GOMAXPROCS=1 "$binary" -test.run "^$" -test.bench "^BenchmarkCavlcLevelWriter$" -test.cpu=1 -test.count=6 -test.benchtime=300ms -test.benchmem
  CAVLC_GO_TEST="$binary" OUTPUT_DIR="$review/results-table128" BENCH_FILTER="(end3|end14|end15)/(zero|known-zero|sparse|dense|mixed)/prefix17" SAMPLES=6 MILLISECONDS=300 bash "$review/run_review.sh" bench
  Repeat replacing candidate/binary/output with cavlc-slow-packed-next / slow-packed-cavlc.test / results-slow-packed.
  Prefer rotating baseline/table128/slow-packed/native samples using root measurement runner; above demonstrates individual command parameters.
  Native/Go fixture parity has333suites/3360blocks, including all prefix bits and partial writes on overflow. Retained candidates still need full encoder tests and full-stream/decoder/hash/timing checks.

No sparse/dense speedup claimed before measurement. Do not combine experiments until isolated results justify each one.
