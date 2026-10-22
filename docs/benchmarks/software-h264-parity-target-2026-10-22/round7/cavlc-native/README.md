# Matched full CAVLC writer review

This isolated harness compares the frozen round6 Go writer with the actual
`WriteBlockResidualCavlc` linked from the supplied native OpenH264 static library.
No writer implementation is copied into the C++ benchmark. The Go tree is a full
copy of `/tmp/h264-parity-target-20261022/round6` plus the fixture/test files below.
The root agent built and executed this harness during round seven. The zero-path
and direct-update drivers require all native/reference parity tests to pass before
writing timing results; the archived statistics record those completed runs.

## Scope and fixture equivalence

One timed operation initializes a writer and encodes 32 coefficient blocks while
preserving pending output bits between those calls. It includes extraction,
trailing ones, token/level/zero/run coding, buffer writes, state commit and return
checks. Both languages use the same manifest, 64-byte coefficient-record stride,
16-byte aligned full 16-word input arrays, and 4096-byte output buffer. The output
buffer is allocated once outside the timer and is not cleared per iteration.
There is no byte copying, final stream flush, fixture parsing, logging or native
process startup inside the measured region. Go benchmark-loop overhead and the
C++ outer loop remain; this is a complete-call throughput comparison, not a
subtracted kernel estimate.

`testdata/cavlc_native_fixtures.txt` contains 333 suites and 3,360 blocks:

- 45 timed suites: end indices 3/14/15, zero/known-zero/sparse/dense/mixed
  coefficients, and initial prefixes of 0/17/31 bits, each with 32 blocks.
- 96 additional success suites exercise every initial accumulator position.
- 192 overflow suites place a full-range coefficient after a valid block and
  after valid levels within its own block; the expected return is 64.

The timed patterns vary signs, 0–3 trailing ones, nC across all four token-table
classes, AC property for end14, and packed-table/escape levels. Chroma DC uses
end3/property3/nC17. All remaining coefficients after each end index are zero:
this is required by the native SIMD extractor. Synthetic distributions make
component differences visible but do not establish full-frame performance.

`TestCavlcNativeFixtures` compares each block against the existing independent
arithmetic reference. `TestCavlcNativeFullParity` invokes native dumps for both
automatic and scalar extraction and checks return value, the complete buffer,
cursor/pending bits/remaining bits, start/end bounds, untouched iBits/iIndex and
input preservation after every block. Comparing all 4096 bytes checks partial
writes on overflow even though the failing writer leaves its committed cursor
unchanged. The comparison stays inside the native input domain; invalid nC,
undersized buffers and short coefficient slices are excluded.

Primary measurements use each implementation's actual automatic extraction
selection. The native harness reports its selected extractor on stderr. Optional
scalar measurements disable Go SSSE3/POPCNT and use native CPU flags zero; those
are diagnostics, not a replacement for automatic-dispatch results.

## Files

- `generate_fixtures.py`: deterministic shared manifest generator.
- `native_cavlc.cpp`: actual-library dump and complete-writer throughput harness.
- `go/pkg/h264/internal/encoder/cavlc_native_full_test.go`: manifest loader,
  independent-reference checks and native full-state parity.
- `go/pkg/h264/internal/encoder/cavlc_native_full_bench_test.go`: checked whole
  writer benchmark with identical names and ns/op / ns/block units.
- `run_review.sh`: sequential build, parity and interleaved measurement commands.

## Commands for the CPU owner

Run builds and measurements serially, with no other CPU jobs. These commands do
not edit the main repository or native source:

```bash
review=/tmp/h264-parity-target-20261022/cavlc-native-review
bash "$review/run_review.sh" build
bash "$review/run_review.sh" parity
MODE=scalar bash "$review/run_review.sh" parity
```

A focused six-case first comparison (all complete 32-block sequences):

```bash
BENCH_FILTER='(end14|end15)/(sparse|dense|mixed)/prefix17' \
  SAMPLES=8 MILLISECONDS=400 bash "$review/run_review.sh" bench
benchstat "$review/results-auto/go.txt" "$review/results-auto/native.txt"
```

Omit `BENCH_FILTER` for the complete 45-case matrix. Use `MODE=scalar` and a
separate output directory for the scalar diagnostic. The script alternates which
implementation runs first on each sample and launches no concurrent benchmarks.
The default complete matrix can take several minutes. If pinning CPU affinity,
wrap the script in `taskset -c <allowed-core>`; both processes inherit that same
affinity. Keep power/load conditions fixed. Inspect individual case results and
repeat unexplained regressions rather than relying only on a geomean.

For a different Go candidate, copy the two Go fixture source files into its
encoder package, then compile that tree using:

```bash
CAVLC_GO_TREE=/tmp/path/to/candidate \
  CAVLC_GO_TEST="$review/candidate-cavlc.test" \
  bash "$review/run_review.sh" build-go
CAVLC_GO_TEST="$review/candidate-cavlc.test" bash "$review/run_review.sh" parity
CAVLC_GO_TEST="$review/candidate-cavlc.test" \
  OUTPUT_DIR="$review/results-candidate" \
  BENCH_FILTER='(end14|end15)/(sparse|dense|mixed)/prefix17' \
  SAMPLES=8 MILLISECONDS=400 bash "$review/run_review.sh" bench
```

The manifest path remains the same absolute shared file by default; no duplicate
fixture is needed in the candidate. Compare candidate versus baseline Go as well
as native. Existing whole-writer/stream/decoder checks remain necessary before
retaining an optimization.

## Native build provenance

The harness links `/tmp/openh264-comparison-20261021/native/libopenh264.a` with
headers from `/home/define42/git/openh264`. Its compile options mirror the
library's recorded optimized amd64 build (`-O3 -DNDEBUG -m64 -DX86_ASM
-DHAVE_AVX2 -fno-strict-aliasing -fPIC -fstack-protector-all`). The library is not
rebuilt or modified. The native writer source in the header tree and static
library build snapshot matches SHA256
`27cae72f1daf1380e0b88860178a988d8166912c4490f5fe6ac11e51ad188ff2`.

The benchmark prints Go benchmark-format records without a CPU suffix, matching
Go's `-test.cpu=1` naming. `blocks/op` is always 32, and `ns/block` is `ns/op / 32`.
Native zero-allocation metrics describe its timed region; Go reports its measured
allocations. Build commands, parity results and performance results should be
recorded separately before drawing conclusions.
