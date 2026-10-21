# Software H.264 second optimization pass — 2026-10-21

This compares the staged first optimization pass against the five additional
improvements below. The baseline was preserved before implementation, on top of
commit `9261ed14829c744bec4981dc54ada9e32ee15e93`. Encoding settings are unchanged.
The pre-existing Git index was preserved.

## Implemented

1. Reuse reconstructed samples when the recycled destination still holds the
   preceding prediction source. This requires matching allocation, layout, and
   successful-frame generation, plus the previous block's **final zero-motion
   P_SKIP**. The normal reference-copy path remains the fallback.
2. Check eligible unchanged blocks before full neighbor-cache setup. The early
   path preserves QP initialization, picture/reference cursors, skip metadata,
   slice boundaries, and the full predictor fallback.
3. Accumulate damage separately for each codec-owned original picture, then
   copy contiguous dirty macroblock runs. This preserves changes from earlier
   frames when a buffer is recycled. Missing hints, incompatible preprocessing,
   IDR, errors, and configuration changes force a full refresh.
4. Omit zero-residual inter transforms and add exact SSE2 inverse transforms
   and fractional chroma interpolation. Other architectures retain scalar
   implementations. Bounds and alias checks preserve scalar access behavior.
5. Reuse frame metadata and per-slice MD/CAVLC scratch, clearing borrowed
   references after use. Allocate internal downsampling pictures on demand.
   Returned access units still own their storage.

The reconstruction proof deliberately does not trust `sMvList` as the final
motion vector: it is search metadata. A QP51/four-slice golden comparison exposed
that distinction. An owned, reused bitmap now captures final coded skip motion
after all slice workers finish, and a regression test covers the misleading
zero-search/nonzero-final-vector case. Direct DC reconstruction calls also keep
local coefficient arrays on the stack; indirect dispatch would introduce a
heap allocation per affected macroblock.

## End-to-end measurements

Intel i7-7700HQ, Go 1.27.3, Linux amd64, `GOMAXPROCS=4`, `-test.cpu=4`.
Six serial before/after pairs alternated order. No agent builds or other task
benchmarks ran concurrently; normal host services remained active. Values below
are medians. These are workload measurements on this host, not session capacity
or universal speedup claims.

| Workload | Before | After | Frame/work time change |
|---|---:|---:|---:|
| 1080p widget, 1 slice | 3.244 ms | 1.263 ms | -61.1% |
| 1080p widget, 4 slices | 2.033 ms | 0.910 ms | -55.2% |
| 1080p scroll, 1 slice | 12.842 ms | 12.252 ms | -4.6% |
| 1080p scroll, 4 slices | 6.965 ms | 6.659 ms | -4.4% |
| 1024×768 moving gradient/block | 16.549 ms | 13.896 ms | -16.0% |
| Four concurrent office30 streams, 120 frames total | 466.682 ms | 427.936 ms | -8.3% median; inconclusive |

The first five comparisons have benchstat p-values below 0.01. The concurrent
session result has `p=0.065`, so no reliable timing improvement is claimed for
it. The four-slice widget candidate was variable (benchstat interval ±40%).
Reported compressed-byte counts are unchanged; separate golden tests compare
complete access-unit bytes, including refreshes.

Desktop tests use 200 changed frames after an initial IDR. Motion uses 100
frames and includes the first encoded frame. The office workload uses one
120-frame operation per sample after warming each of four encoders; it has no
real-time pacing. Construction and first-frame startup use 10 operations per
sample. See [measure.py](measure.py) and [build-info.txt](build-info.txt) for the
runner, compiler details, baseline patch hash, and binary hashes.

## Allocation and startup results

| Desktop workload | Allocated bytes/frame before → after | Objects/frame before → after |
|---|---:|---:|
| Widget, 1 slice | 22,361 → 1,146 | 12 → 8 |
| Widget, 4 slices | 49,300 → 1,519 | 29 → 19 |
| Scroll, 1 slice | 26,024 → 4,810 | 12 → 8 |
| Scroll, 4 slices | 55,999 → 8,355 | 29 → 19 |

Small-update allocation bytes fall 94.9–96.9%; scrolling falls 81.5–85.1%.
The concurrent 120-frame workload drops from 3,153,328 to 663,344 allocated
bytes. Motion's allocation result includes first-frame preprocessing, so it
should not be interpreted as a steady-state allocation rate.

First-frame startup allocations fall from **41.24 to 35.28 MiB** with one slice
and **71.23 to 65.30 MiB** with four slices: about 5.9–6.0 MiB less per encoder.
These are cumulative allocations for construction, one initial frame, and
teardown, not an RSS measurement. The six avoided downsampling arrays contain
6,266,880 sample bytes (about 6 MiB including allocator rounding).

Preprocessing initializes on the first frame. Constructor-only allocation is
therefore slightly higher with retained metadata/scratch (about 29–60 KB), and
its time is statistically unchanged. Startup time including the first frame
improves 4.7% with one slice; the four-slice change is inconclusive. Both
constructor and startup results are retained to make this distinction explicit.

The isolated four-block inverse transform measures 201.55 → 63.36 ns (3.2×),
and 8×8 fractional chroma measures 217.85 → 37.82 ns (5.8×). Both scalar and SIMD
kernels allocate zero bytes in these tests. These kernel ratios are not added
to, or substituted for, the end-to-end results.

## Validation

- Affected packages pass: `./pkg/h264/...`, `./internal/rdp/...`,
  `./internal/config`, `./internal/gateway`, `./internal/virtio`, and the root
  package. Network-sensitive tests used an isolated network namespace.
- Race checks pass for the codec, encoder, common kernels, processing, and AVC;
  final focused checks cover reuse, resize, and golden bitstreams.
- All 12 baseline golden sequences match: QP0/20/51, requested one/four slices
  (the small frame clamps to one), 66×50 and 320×192, moving/chroma damage, missing hints, unchanged frames, and forced IDR.
  The same digests pass in the executable 386 scalar build.
- Independent FFmpeg decoding passes for native, downscaled, and upscaled
  damage sequences and multiple slices.
- SIMD tests cover full int16 extremes, random coefficients, all chroma
  fractions, exact-length and guard-page buffers, aliases, signed strides,
  coded-block patterns, and portable fallbacks.
- Reuse tests cover uneven original-buffer rotation, accumulated damage,
  failures, IDR, QP changes, and actual resize/reinitialization round trips.
  Metadata/scratch ownership and lazy downsampling parity also pass.
- AVX2-disabled, CGO-disabled, and NVENC-tagged checks pass. ARM64 test packages
  cross-compile; they were not executed on ARM hardware.
- Repository-pinned golangci-lint 2.12.2 reports zero issues, using the cached
  Go 1.26.9 toolchain supported by that linter.

Raw before/after samples and benchstat output are saved for `desktop`, `motion`,
`sessions`, `construction`, and `startup`. `idct.txt`, `chroma.txt`, and
`downsampling-construction.txt` contain the isolated measurements. Validation
logs are included alongside them. Precompiled comparison binaries and the
preserved baseline source remain under `/tmp/h264-round2-20261021` in this
workspace; `measure.py BEFORE AFTER OUTPUT [WORKLOAD ...]` accepts their paths.
