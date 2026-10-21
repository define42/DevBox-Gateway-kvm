# Software H.264: video optimization pass 3 — 2026-10-21

This pass implements all five output-preserving targets from the
[video review](../software-h264-video-next-2026-10-21/README.md) and fixes its
confirmed forward-transform overread. The comparison baseline is the working
implementation at the start of this pass, including both previous optimization
passes. Results below are additional gains over that baseline.

## Changes

1. **Motion scoring:** AVX2 SATD processes several independent 4×4 transforms
   together for 8×8, 16×8, 8×16 and 16×16 blocks. Quarter-pixel refinement fuses
   rounded averaging with SATD and materializes only the final winning
   prediction. It preserves per-4×4 normalization, strict tie order and buffer
   swaps. Unsupported CPUs, narrow blocks, SAD mode and overlapping scratch
   retain the existing paths.
2. **Intra prediction:** SSE2 generates luma/chroma horizontal, DC and plane
   predictors. Combined vertical/horizontal/DC SAD scoring avoids writing
   rejected predictions. Signed shifts, clipping, availability, penalties and
   ties retain their original behavior.
3. **Coefficients:** a bounded SSE2 nonzero mask and SSSE3 scan-plus-mask
   operation supply counts and residual scores without repeated coefficient
   passes. Shared masks feed inter, intra and chroma processing. SSE2
   dequantization preserves low-word multiplication; unsupported architectures
   and overlapping buffers retain scalar behavior.
4. **CAVLC:** a roughly 14 KiB table handles common signed levels and suffix
   transitions; the original arithmetic handles larger levels and overflow.
   Measurements rejected a mask-based run/level extractor: the original scalar
   extraction is faster in this writer. The final implementation combines
   scalar extraction with the lookup table. Per-block scratch stays on the stack.
5. **Transforms and skip:** the forward transform processes two adjacent 4×4
   blocks together. Luma skip transforms and checks one 8×8 group at a time,
   stopping at the first rejection. Ordinary encoding regenerates any scratch
   it subsequently needs.

The old 4×4 DCT used assembly `MOVD` memory loads that Go encoded as eight-byte
loads. They now read exactly four bytes. Review also caught the same assembler
width pitfall in the new intra SAD output stores; those now write exactly four
bytes. Guard-page tests cover both. Alias range checks retain real pointers
across calls so stack movement cannot stale a saved integer address.

Quality, QP, search decisions, codec preset, GOP, CPU-budget policy and managed
slice cap are unchanged. No PGO profile is enabled.

## End-to-end measurements

Medians of six serial alternating pairs; lower time is better. Full-screen
video frame time fell **28.10% with one slice** and **26.10% with four slices**.
All comparisons retain identical bytes per frame. Absolute times vary with
host load/frequency; compare before/after pairs in this run, not absolute
values against earlier reports.

| Workload | Slices | Before ms/frame | After ms/frame | Time reduction | p |
|---|---:|---:|---:|---:|---:|
| Video, full damage | 1 | 71.04 | 51.07 | 28.10% | 0.002 |
| Video, nil damage | 1 | 71.05 | 51.94 | 26.90% | 0.002 |
| Video, full damage | 4 | 29.13 | 21.53 | 26.10% | 0.004 |
| Video, nil damage | 4 | 29.12 | 21.57 | 25.93% | 0.002 |
| Dense motion, full damage | 1 | 175.33 | 138.73 | 20.87% | 0.002 |
| Dense motion, full damage | 4 | 66.88 | 49.30 | 26.29% | 0.004 |
| Desktop scroll | 1 | 14.67 | 12.29 | 16.22% | 0.015 |
| Desktop scroll | 4 | 7.96 | 6.89 | 13.43% | 0.004 |
| Desktop widget | 1 | 1.64 | 1.57 | Inconclusive | 1.000 |
| Desktop widget | 4 | 1.25 | 1.04 | Inconclusive | 0.180 |

For full-damage video, 1 slice(s): CPU 71.22→51.18 ms/frame; p95 wall time 86.85→57.67 ms; 4 slice(s): CPU 85.60→63.50 ms/frame; p95 wall time 35.50→24.75 ms.
The one-slice result still exceeds the 33.3 ms budget for 30 fps; the four-slice
clip replay fits it. Dense stress remains slower than that budget. These are
encoder replay results, not a live-session frame-rate promise.

Raw results and confidence intervals:
[video](clip-benchstat.txt), [dense motion](dense-benchstat.txt),
[desktop controls](desktop-benchstat.txt).


## Method and scope

Intel i7-7700HQ, Linux amd64, Go 1.27.3, GOMAXPROCS=4. The software encoder uses
1920×1080, QP20 and a 30 fps timestamp clock. Each sample has 16 untimed warmup
frames. Video uses 60 timed frames; dense stress uses 16. Six pairs alternate
before/after order. Builds, tests and profiles are stopped during timing.
Each operation includes RGBA conversion and H.264 encoding through
`Encoder.EncodeDamage`; the harness also reports encode-only time, CPU time,
p95, bytes and allocation counts. It does not include capture, network writes,
client decoding or display latency.

The video fixture is five 1280×720 source frames from the local OpenH264
`Cisco_Absolute_Power_1280x720_30fps.yuv`, bilinearly upscaled before timing and
looped. One transition in five is the artificial last-to-first wrap. Dense
stress is eight moving pseudorandom frames, useful for heavy residual/CAVLC
work but not representative natural video. Whole-screen damage marks no
macroblock clean; nil damage performs full conversion without hints.
Desktop controls alternate scrolling content or a small changing widget,
using 60 timed frames after their original initial-IDR setup.

The four-slice harness requests four workers directly. Live managed RDP may
choose fewer: the existing cap reserves guest CPUs and a service CPU. With
GOMAXPROCS=4 and two guest CPUs, that policy allows one slice. Check the actual
backend and effective slices in runtime logs when comparing a live session.
These replays establish performance on the tested host and inputs, not a
universal live-video frame-rate guarantee.

## Component measurements

Six samples per component. SATD and DCT compare the preserved baseline and
final binaries; the other rows compare reference and optimized paths within
the final binary. All measured component paths allocate zero bytes per call.
These numbers explain individual mechanisms and are not additive.

| Operation | Reference ns/op | Optimized ns/op | Time reduction |
|---|---:|---:|---:|
| 16×16 AVX2 SATD, preserved baseline → final | 158.95 | 60.61 | 61.87% |
| Four forward 4×4 transforms, preserved baseline → final | 54.02 | 36.47 | 32.49% |
| Quarter-pixel refinement (already using batched SATD) | 555.25 | 434.45 | 21.76% |
| Luma + chroma intra mode decision, scalar predictors → SIMD + fused SAD | 1584.50 | 542.80 | 65.74% |
| Scan/count/score, five nonzeros | 36.05 | 18.34 | 49.13% |
| Four-block dequantization, scalar → SIMD | 37.23 | 11.83 | 68.23% |
| CAVLC dense small levels, original → final | 239.50 | 188.10 | 21.46% |

The isolated 8×8 SATD change was inconclusive; larger blocks show clear gains.
Direct four-byte DCT loads also reduced the single-block transform time by
8.46% in this run. Full details: [SATD](satd-benchstat.txt),
[transforms](dct-benchstat.txt), [component samples](components.txt).


CAVLC was selected with an additional alternating-order comparison of three
writers on sparse, dense, moderate (signed 8..63) and mixed/escape coefficient
corpora. The mask-plus-table version regressed the mixed corpus by 13.08%
(p=0.041), so its extractor was removed. The final scalar-extractor-plus-table
version reduced dense writer time by 18.91% and moderate-level writer time by
21.11% against the original writer (p=0.009/0.004); sparse and mixed changes
were inconclusive. This is a component result, separate from full-frame gains.
See [CAVLC comparison](cavlc-confirmation-benchstat.txt). In its files,
`Original` means the frozen original writer, `Current` is the rejected
mask-plus-table prototype, and `Candidate` is the selected implementation.

To repeat that isolated selection experiment, copy the three supplementary
`cavlc_*_test.go.txt` files here into the final encoder test package with `.go`
extensions, build it as `variant-a.test`, and run `cavlc-confirm.py` beside the
binary. These test-only artifacts preserve the rejected extractor for the
comparison; it is absent from production.

## Correctness and portability

- All affected `pkg/h264/...` and `internal/rdp/...` tests pass.
- All 12 preserved optimization stream goldens pass, covering QP0/20/51,
  one/four slices, odd sizes, damage transitions and IDRs.
- Before/after hashes and byte counts match for eight full-screen cases:
  clip/dense × one/four slices × full/nil damage, 76 frames each. See
  [parity.txt](parity.txt). No expected digests were changed.
- FFmpeg interoperability tests pass, including native, upscaled and downscaled
  damage streams with one/four slices.
- Exhaustive coefficient masks; all 52 QPs across coefficient state and lazy
  skip tests; full signed-int16 CAVLC level coverage plus randomized complete
  writer/state comparisons; randomized SIMD/scalar parity and extreme values.
- Guard pages, exact-length and short-buffer checks, signed/zero/narrow
  strides, overlap semantics, candidate ties and winner-buffer state.
- Race tests pass for the codec and RDP encoder; final modified encoder/RDP
  packages were checked again after review fixes.
- AVX2/SSSE3-disabled tests, 386 execution, pure-Go builds and ARM64 encoder
  cross-compilation pass. ARM64 was compiled, not executed.
- Pinned golangci-lint 2.12.2 with Go 1.26.9 reports zero issues.

## Reproduction

The [earlier full-screen report](../software-h264-fullscreen-2026-10-21/README.md)
contains the unchanged video benchmark and digest harness and fixture creation.
Build baseline and candidate `internal/rdp/avc` test binaries as `before.test`
and `after.test`, with those harness files in both trees. Build their encoder
package tests as `before-encoder.test` and `after-encoder.test`; the baseline
also receives only the `BenchmarkSampleSatdBlocks` fixture for a same-harness
comparison. No baseline production source is changed.

Place these binaries and `clip1080p.rgba` in one directory and run
`python3 measure.py DIRECTORY`. The script serializes all measurements.
Raw samples, benchstat comparisons, build/fixture hashes and verification
logs accompany this report. Component microbenchmarks are useful for explaining
individual changes; their speedups cannot be added to predict total gains.
