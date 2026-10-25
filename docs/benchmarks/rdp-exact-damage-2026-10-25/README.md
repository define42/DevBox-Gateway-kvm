# Exact I420 damage tracking

The converter now identifies unchanged 16x16 blocks inside reported damage,
using exact previous/current Y, U and V samples. These blocks can use the
encoder's existing unchanged-block paths. No extra image scan or full-frame
snapshot is introduced. Refreshes, geometry changes and retries still bypass
hints. Parallel conversion workers own complete macroblock rows, and overlapping
damage can only clear previously accumulated unchanged bits.

The AVX2 path records both 16-pixel block results while converting 32 pixels;
narrow edges retain the existing eight-pixel SIMD kernel. Portable conversion
performs the same sample comparisons. Conversion telemetry now includes this
fused hint preparation; Encoding records codec execution and output handling.

## Measured result

The isolated encoder benchmark shows a modest CPU reduction, but the real-client
benchmark does **not establish lower window-drag frame latency**. This change
must not be presented as a demonstrated fix for perceived dragging delay.

Medians of 12 runs, two effective slices, 1280x720, QP 20:

| Workload / measurement | Before | After | Interpretation |
| --- | ---: | ---: | --- |
| Window drag: isolated conversion + encoding | 2.474 ms | 2.384 ms | Difference not significant, p=0.052 |
| Window drag: isolated server CPU/frame | 4.224 ms | 4.059 ms | -3.91%, p=0.010 |
| Window drag: conversion + encode + send + ACK | 6.603 ms | 6.622 ms | No significant difference, p=0.799 |
| Window drag: ACK mean | 3.570 ms | 3.537 ms | No significant difference, p=0.887 |
| Window drag: server CPU/frame with client | 5.387 ms | 5.345 ms | No significant difference, p=0.319 |
| Scroll: isolated conversion + encoding | 5.637 ms | 5.382 ms | -4.52%, p=0.028 |
| Scroll: isolated server CPU/frame | 8.521 ms | 8.165 ms | -4.18%, p=0.006 |
| Scroll: conversion + encode + send + ACK | 12.687 ms | 12.458 ms | -1.81%, p=0.004 |
| Scroll: server CPU/frame with client | 10.700 ms | 10.215 ms | -4.53%, p<0.001 |

The measured H.264 bytes/frame and allocation counts are unchanged. Isolated
encoding remains at 19 allocations/frame; allocated bytes increase by about
46 bytes for scrolling and 51 bytes for window dragging, from the larger
parallel-conversion closure. Those are not full-picture allocations.

All 4,800 measured frames in the final real-client comparison had valid ACK
samples, zero early-ACK races and no reported decoder errors. Each isolated
case processed 4,800 frames per variant. Timing is unpaced and excludes live
input handling, capture/pacing waits, VM execution and physical presentation.
CPU totals count scheduled time across all process threads, not elapsed time.

## Method

- Intel Core i7-7700HQ @ 2.80 GHz, Linux amd64, Go 1.27.3, GOMAXPROCS=8.
- Real client: FreeRDP 3.32.1 (`bf217a5`), AVC420, software GDI, Xvfb,
  loopback TLS, one frame outstanding.
- Ten warmup frames per invocation. Encoder-only runs use 400 measured frames;
  real-client runs use 100. Source pictures alternate identically in both builds.
- Twelve paired rounds per benchmark. Odd rounds run before then after; even
  rounds reverse the order. Every workload ran serially, with no agent builds,
  tests or lint competing. Unrelated host activity and CPU frequency were not
  controlled, so results apply to these fixtures and this host.
- Baseline production code is `de5929e`; both builds include the same current
  slice/CPU benchmark instrumentation. A Go build overlay restores baseline
  `encoder.go`, `encoder_prepare.go`, `encoder_damage.go` and its old test file
  for the before binary. The working tree was not reset. New exact-conversion
  helpers are unreachable from the baseline's preparation path.
- The initial prototype used scalar rectangle edges and showed no clear gain
  amid substantial host noise. Its results are retained as `initial-*` files.
  The final implementation restores SIMD for those edges; the reported final
  comparison uses freshly built binaries containing that implementation.

Compile both baseline and candidate test binaries with `go test -tags=integration
-c -o <binary> ./internal/rdp`, adding `-overlay=<baseline-overlay.json>` for the
baseline. Run from `internal/rdp`, alternating binary order for twelve rounds:

```sh
GOMAXPROCS=8 <binary> -test.run='^$' \
  -test.bench='^BenchmarkGraphicsEncodeDamage$/.*/^slices=2$' \
  -test.benchtime=400x -test.count=1 -test.timeout=30s

GOMAXPROCS=8 DEVBOX_KVM_REQUIRE_ACK_TEST=1 <binary> -test.run='^$' \
  -test.bench='^BenchmarkGraphicsACKFreeRDP$/.*/^slices=2$' \
  -test.benchtime=100x -test.count=1 -test.timeout=30s

benchstat encode-before.txt encode-after.txt
benchstat before.txt after.txt
```

Artifacts: [isolated before](encode-before.txt), [isolated after](encode-after.txt),
[isolated comparison](encode-benchstat.txt), [real-client before](before.txt),
[real-client after](after.txt), [real-client comparison](benchstat.txt),
[initial prototype comparison](initial-benchstat.txt).

## Correctness checks

Exact masks are checked against independent full I420 sample comparisons for
RGBA/BGRX, native/scaled images, chroma-only edits, ignored alpha/X bytes,
repeated and overlapping damage, partial edge blocks and multiple workers.
SIMD tests cover each luma/chroma position, preserved cleared bits, scalar pixel
parity and guard-page bounds. Existing FFmpeg multi-frame decode, configured-QP
pixel comparisons, refresh/retry and prepared-frame ownership checks pass.

Additional validation includes the full repository tests in an isolated network
namespace, race detection for AVC, the AVX2-disabled fallback, ARM64 cross-build,
real FreeRDP integration and integration-tagged RDP lint.
