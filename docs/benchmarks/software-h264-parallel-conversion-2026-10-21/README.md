# Parallel software H.264 color conversion — 2026-10-21

The software encoder now converts large RGB regions in parallel before slice
encoding starts. Each worker owns complete pairs of output rows, including
corresponding chroma samples. Existing native AVX2, scaled AVX2 and scalar
conversion retain their arithmetic and change detection.

## Implementation

- Parallelism is bounded by effective software slices, GOMAXPROCS, four workers,
  available row pairs and at least 128 Ki pixels per worker. One band runs on
  the caller. A single-worker budget, NVENC, empty regions and small updates
  use the serial converter.
- Both full conversion and explicit full-screen damage enter this path.
  Damage rectangles remain sequential, including overlap introduced by mapping
  or alignment. Worker-local change flags are combined only after joining all
  workers, before encoding or updating frame state.
- Full image coordinates are retained while scaling. Source padding, nonzero
  origins, destination guards, retained pixels and 2x2 chroma boundaries keep
  their existing behavior. There are no persistent worker goroutines or
  additional full-frame buffers.
- The codec-policy benchmark now marks its manually constructed encoder as
  software, matching the public constructor and exercising this path.

## Results

Medians of six alternating pairs; lower time is better. The four-slice
full-damage clip replay fell from **17.59 to 15.38 ms/frame**, a **12.53%**
reduction (p=0.026). Its conversion stage fell from **1.524 to 0.718 ms**,
**52.90%** (p=0.002). Encoding-stage variability also contributes to the
whole-frame result; do not treat 12.53% as a universal gain from conversion.

| Workload | Before ms/frame | After ms/frame | Finding |
|---|---:|---:|---|
| Clip, 4 slices, full damage | 17.59 | 15.38 | 12.53% lower, p=0.026 |
| Clip, 4 slices, nil damage | 16.30 | 15.16 | Inconclusive, p=0.132 |
| Clip, 1 slice, full damage | 42.31 | 42.37 | Inconclusive, p=0.699 |
| Dense motion, 4 slices | 31.68 | 31.12 | Inconclusive, p=0.699 |
| Desktop scroll, 4 slices | 5.097 | 4.401 | 13.65% lower, p=0.041 |
| Desktop widget, 4 slices | 0.754 | 0.768 | Inconclusive, p=0.699 |
| Desktop scroll, 1 slice | 9.195 | 9.019 | Inconclusive, p=0.589 |
| Desktop widget, 1 slice | 1.048 | 1.137 | Inconclusive, p=0.065 |

Four-slice nil-damage and dense conversion stages also fell by about 52% and
51%, respectively, but their total frame-time differences were inconclusive.
The full-damage clip's p95 (22.73 to 21.07 ms) and process CPU time (51.98 to
48.54 CPU-ms/frame) were also inconclusive. All bytes/frame values match.
See [clip](clip-benchstat.txt), [dense](dense-benchstat.txt) and
[desktop](desktop-benchstat.txt) for all samples' confidence intervals.

The short one-slice widget control reported a 0.82 microsecond conversion
increase despite an inconclusive total-frame result. A follow-up using ten
alternating 240-frame pairs found no significant difference in either total
time (1.208 to 1.224 ms, p=0.853) or conversion (5.553 to 5.646 microseconds,
p=0.684). Both initial and follow-up data are retained; see
[longer widget control](widget-control-benchstat.txt).

Converter-only measurements, independent of codec work:

| Input/update | 1 worker ms | 2 workers ms | 4 workers ms |
|---|---:|---:|---:|
| Native 1080p | 1.500 | 0.870 | 0.612 |
| 720p scaled to 1080p | 3.566 | 1.865 | 1.356 |
| Large partial 1080p region | 0.613 | 0.403 | 0.274 |
| 64x32 widget | 0.00171 | 0.00172 | 0.00172 |

Large-region reductions are significant (p=0.002, n=6); widget differences are
inconclusive. Four-worker conversion adds eight allocations and approximately
332 bytes per eligible region in the component benchmark. In full clip replay,
this raises allocations from 19 to 27 per frame and allocated bytes by about
0.36%. Serial/small-update conversion remains allocation-free. No persistent
pool is introduced; tuning the 128 Ki-pixel threshold or pooling workers is
future work if measurements justify the added lifecycle complexity.
See [component comparison](conversion-benchstat.txt).

## Verification

- RDP package tests, including FFmpeg interoperability, passed.
- AVC race tests passed. New tests cover two/three/four workers, uneven bands,
  native/up/down/vertical/single-pixel scaling, partial regions, padded sources,
  nonzero origins, output guards, repeated and alpha-only input, a change only
  in the final band, overlap/duplicates, force, codec failure/retry and resizing.
- AVX2-disabled conversion tests passed through the scalar fallback.
- Pinned golangci-lint 2.12.2 with Go 1.26.9 reports zero issues.
- All eight preserved full-screen stream digests and byte counts match the
  baseline: clip/dense x one/four slices x full/nil damage, 76 frames each.
  No expected digests were changed. See [parity.txt](parity.txt).

## Measurement method

Intel i7-7700HQ, Linux amd64, Go 1.27.3, GOMAXPROCS=4. The baseline is the
already optimized round-3 binary, whose production sources match HEAD except
for this change. The candidate uses the same fullscreen replay harness through
an overlay. See [build-info.txt](build-info.txt) and
[sources-after.sha256](sources-after.sha256).

Full-frame comparisons use six serial alternating before/after pairs with no
concurrent builds, tests or profiles. Each clip sample has 16 untimed warmup
frames, then 180 timed frames for four-slice full damage, 120 for four-slice
nil damage, and 60 for the one-slice control. Dense motion uses 32 timed frames;
desktop scroll/widget controls use 60 after their original IDR setup.

The full-frame harness includes conversion and encoding, but excludes capture,
RDP framing, network writes and client decoding. Its clip is only five source
frames scaled from 720p and looped, including an artificial last-to-first
transition; dense motion is synthetic moving pseudorandom data. Results apply
to these fixtures and this host, not every video or live-session frame rate.
Four-slice replay requests four directly; managed RDP can allow fewer after
reserving CPUs for the guest and service. The reservation policy is unchanged.

Component measurements use six 300 ms samples per case and alternate eight
input images. They include conversion scheduling/allocation overhead and
warm the destination and stacks before timing. Native, scaled, large partial
and small-widget cases compare one, two and four workers within one binary.
These component factors are not end-to-end speedup factors.

## Reproduction

The committed `i420_parallel_bench_test.go` supplies the conversion benchmark:

```sh
go test ./internal/rdp/avc -run '^$' -bench '^BenchmarkI420Parallel$' -cpu=4 -benchtime=300ms -count=6
```

Full-frame replay uses the supplementary `encoder_video_review_bench_test.go.txt`
and `encoder_video_review_test.go.txt` from
[the fullscreen review](../software-h264-fullscreen-2026-10-21/) as Go test files
in isolated before/after copies, or equivalent Go overlays. Build test binaries
for the baseline HEAD recorded in `build-info.txt` and this change with the same
Go version. [measure.py](measure.py) records the exact serial pairing, patterns,
frame counts and fixture path used here; adjust its binary/fixture paths for a
new machine. No PGO profile was enabled. Run builds, tests and profiles outside
the timed comparison.
