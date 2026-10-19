# Virtio conversion and bitmap packing — 2026-10-19

## Changes

The preceding [live RDP profile](../live-rdp-2026-10-19/README.md) identified
full-frame BGRX/BGRA conversion and same-size bitmap packing as host-side
CPU costs. This patch targets those loops without changing framebuffer
publication, pacing, transport, guest rendering, or H.264 encoding.

- `internal/virtio/gpu.go` dispatches BGRX/BGRA rows to an AVX2 byte-shuffle
  kernel when the host CPU and OS support AVX2. Each vector handles eight
  pixels; shorter rows and suffixes retain the scalar implementation.
- BGRA/cursor alpha is preserved, while BGRX desktop alpha is made opaque.
  Conversion stays within the clipped row and retains existing source stride,
  destination stride, scanout, and ownership semantics.
- Other architectures, hosts without AVX2, and builds using `-tags=purego`
  use the Go fallback. No experimental Go features or new dependencies are
  required.
- `internal/rdp/bitmap.go` selects direct row packing when source and output
  dimensions match. This removes scaling division and depth selection from
  the per-pixel loop, preserving RGB565/BGR packing and the reserved 32-bit
  output byte. Actual resizing uses the original nearest-neighbor algorithm
  in a separate helper.

The virtio change benefits both H.264 and bitmap delivery. The bitmap packing
change affects bitmap only. Neither changes the qcow2 guest image.

## Measurement method

- Host: Intel Core i7-7700HQ, Linux amd64.
- Toolchain: Go 1.26.8, `GOAMD64=v1`, `CGO_ENABLED=1`, no `GOFLAGS` override.
- Baseline: existing working tree at HEAD `a7a5125`, including its previous
  uncommitted work, before either production optimization. Benchmark fixtures
  were added before compiling the baseline binaries.
- Precompiled old and new test binaries; ten serial old/new pairs per set,
  `-test.cpu=1 -test.benchtime=200ms -test.benchmem -test.run='^$'`.
- No builds, test jobs, or other benchmarks ran concurrently with the timing
  windows. The host's normal desktop/services remained running; confidence
  intervals still show some background variability.
- `benchstat` compares medians. These isolated CPU/memory benchmarks do not
  measure client display FPS, network latency, or guest browser responsiveness.

## Virtio results

| Conversion workload | Before | After | Time reduction |
| --- | ---: | ---: | ---: |
| BGRX 1920×1080 | 29.811 ms | 2.286 ms | 92.33% |
| BGRA 1920×1080 | 27.170 ms | 2.163 ms | 92.04% |
| BGRX 3840×2160 | 139.25 ms | 12.52 ms | 91.01% |
| BGRX 64×32 dirty region | 27.675 µs | 2.708 µs | 90.21% |
| BGRX 13×7 region with vector tail | 1.408 µs | 0.920 µs | 34.66% |

All listed timing differences have `p=0.000, n=10` as reported by benchstat
(rounded p-values, not literally zero). Conversion remains allocation-free.
Full output: [virtio comparison](virtio-benchstat.txt).

## Bitmap measurement iterations

The first same-size implementation reduced 1080p packing time by 78–85%, but
the unchanged 24/32-bit scaled controls regressed approximately 6%. Compiled
code inspection showed the new call enlarged the scaling function's stack
frame and changed loop layout, despite unchanged scaling arithmetic.

The final implementation separates the original nearest-neighbor loop from
the small dispatcher. Its measurements are recorded separately from the first
attempt; the initial results are retained for transparency. Code layout is a
plausible explanation for the initial regression, not a proven hardware-level
root cause.

### Final bitmap results

| Workload | Before | After | Time reduction |
| --- | ---: | ---: | ---: |
| RGB565 same-size 1080p packing | 121.16 ms | 26.47 ms | 78.15% |
| 24-bit same-size 1080p packing | 98.87 ms | 16.34 ms | 83.48% |
| 32-bit same-size 1080p packing | 99.37 ms | 14.43 ms | 85.48% |
| 32-bit 64×32 region packing | 99.34 µs | 14.78 µs | 85.13% |
| BitmapDamage 1080p full-region update | 104.88 ms | 20.79 ms | 80.18% |
| BitmapDamage 1080p 64×32 update | 126.61 µs | 37.09 µs | 70.70% |

All listed improvements have `p=0.000, n=10`. The three scaled controls have
no statistically significant change (`p=0.684`, `0.218`, and `0.393` for
16/24/32-bit depths). This is not proof of exact performance equivalence.
Packing stays allocation-free. The existing `BitmapDamage` benchmark still
allocates one 18 KiB tile packet per iteration; that was not changed here.

The `BitmapDamage` benchmark changes one pixel per iteration: its full-region
case measures a full scan/packing pass plus the changed-tile update, not a
network send of an entirely changing screen. The callback does no network I/O.
Neither these numbers nor the isolated packing times predict client FPS.

Final [bitmap comparison](bitmap-benchstat.txt), [before samples](bitmap-before.txt),
and [after samples](bitmap-after.txt) are retained. The initial implementation's
[before](bitmap-initial-before.txt) and [after](bitmap-initial-after.txt) samples
are also retained; they are not the final performance result.

## Correctness and compatibility

- Affected packages: `go test ./internal/virtio ./internal/rdp -count=1`.
- Independent row-reference tests cover alpha, unaligned slices, empty rows,
  vector boundaries, scalar tails, source immutability, and guard bytes.
- Viewport tests cover clipped/partial rectangles, nonzero image origins,
  padded stride, both BGR formats, and cursor alpha. Existing ownership,
  scanout, and cursor tests remain in place.
- Linux protected-page tests put inaccessible pages around the readable
  buffers and make source pages read-only. They detect overreads and writes
  that byte sentinels cannot detect.
- Bitmap reference tests cover 16/24/32-bit depths, same-size/scaled images,
  nonzero origins, padded stride, minimal final-row buffers, empty/partial
  regions, reserved bytes, and source immutability.
- Runtime fallback tested with `GODEBUG=cpu.avx2=off`; portable build tested
  with `-tags=purego`.
- Race tests cover bitmap, GPU, framebuffer, and row conversion. Pure-Go race
  tests also run because assembly memory accesses are not race-instrumented.
- `go vet ./internal/virtio ./internal/rdp` and Linux ARM64 test-binary
  cross-compilation passed. ARM64 execution was not performed.
- The repository-pinned golangci-lint 2.12.2 passed for both affected packages.
  The separately installed 2.13.2 enabled a new `exhaustruct_v5` check despite
  the configuration disabling the previous name; no lint rules were changed
  to accommodate that unrelated tool-version mismatch.

## Reproduction and deployment

Benchmarks:

```sh
go test -run='^$' -bench=BenchmarkGPUResourceConversion -benchmem -count=10 ./internal/virtio
go test -run='^$' -bench='BenchmarkBitmapScaleRegion|BenchmarkBitmapDamage/1080p' -benchmem -count=10 ./internal/rdp
```

For comparisons, compile each version with `go test -c` first, then alternate
old and new binaries using the flags above. Do not benchmark both concurrently.
Raw samples and comparison summaries are stored beside this report. Temporary
binaries and additional diagnostics remain in `/tmp/devbox-pixel-perf-4yW9tgLe`.

The running Compose deployment was not restarted or rebuilt during this
change. Rebuild the gateway image and restart the managed VM workers to use
the new binary. The next live validation should repeat the same browser/RDP
workload and compare `/perf` flush/conversion timings and worker CPU. No
live FPS or end-to-end latency improvement is claimed by these microbenchmarks.
