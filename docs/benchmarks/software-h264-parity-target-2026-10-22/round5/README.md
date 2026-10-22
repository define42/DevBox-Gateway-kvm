# Software H.264 performance parity: round five

The cumulative retained changes reduce codec time by **6.9% geometric mean** versus the original baseline. Full 1080p `EncodeDamage` improves **3.9–6.7% in five statistically significant cases**; ShortClip/four slices has a lower median but does not reach significance in this run. **Native performance parity remains unmet:** at 1080p Go takes **1.59–1.69× native wall time with one slice**, **1.14–1.19× with four slices**, and **1.56–1.62× native CPU time with four slices**. These are local measurements, not live RDP dashboard readings.

This frozen milestone adds six measured improvements to [round four](../round4/README.md):

- Independent accumulators and four-row addressing in 16-wide SAD: checked improvements of roughly 3–9% in the significant cases, with other strides neutral. [Statistics](sad16-next-benchstat.txt).
- Fixed-width stores for Go I4 vertical/horizontal/DC predictors: predictor-plus-SAD improves 25–43%. [Statistics](intra-word-next-benchstat.txt).
- An 8 KiB packed CAVLC level table replacing the 14 KiB entry layout: selected dense-writer cases improve 4–6%, with many medium cases neutral. Slow escape and overflow behavior remains unchanged. [Statistics](cavlc-packed-next-benchstat.txt).
- SSSE3 eight-wide fractional chroma filtering with row reuse and exact nine-byte source spans: checked fractional cases improve 20–29%, with integer/narrow controls neutral. [Statistics](chroma-tuned-benchstat.txt).
- Two-block AVX2 quantization and maximum extraction: ten of twelve checked cases improve 15–18%, with two noisy neutral cases. Aliased coefficient/max buffers retain SSE2 ordering. [Statistics](quant-max-benchstat.txt).
- SSSE3 intra variance over sixteen floored 4×4 means: checked call time falls from about 127 ns to 14–15 ns. [Statistics](vaa-next-benchstat.txt).

Component measurements use six alternating pairs, CPU 2, 250 ms per case. Their percentages are not whole-frame gains.

## Complete codec comparison

Baseline is commit `641af9e6216055b6a9d918bdcba4ea7d40020750`; after includes every retained change from rounds three through five. Values are medians of six run averages, with 16 warmup and 80 measured frames and rotating baseline/Go/native order. One slice uses CPU 2; four slices use CPUs 0–3. Intel i7-7700HQ, Go 1.27.3, CGO disabled, GOMAXPROCS=4. Other workstation processes remain running. Absolute times across separate rounds are not paired measurements.

| Case | Before ms | After ms | Native ms | Go/native |
| --- | ---: | ---: | ---: | ---: |
| ShortClip/1920x1080/QP20/1Slices | 33.403 | 30.923 | 19.325 | 1.600× |
| ShortClip/1920x1080/QP20/4Slices | 10.453 | 9.663 | 8.444 | 1.144× |
| Road/1920x1080/QP20/1Slices | 33.040 | 30.746 | 19.118 | 1.608× |
| Road/1920x1080/QP20/4Slices | 11.451 | 10.847 | 9.173 | 1.183× |
| Road/1920x1080/QP32/1Slices | 24.095 | 22.437 | 13.310 | 1.686× |
| Road/1920x1080/QP32/4Slices | 8.961 | 8.512 | 7.247 | 1.175× |
| PDF/1920x1080/QP20/1Slices | 12.337 | 11.434 | 7.199 | 1.588× |
| PDF/1920x1080/QP20/4Slices | 5.016 | 4.703 | 3.942 | 1.193× |
| Road/1280x720/QP20/1Slices | 14.330 | 13.369 | 8.488 | 1.575× |
| Road/1280x720/QP20/4Slices | 5.356 | 4.859 | 4.521 | 1.075× |

[Before/after/native statistics](codec-benchstat.txt), [direct native comparison](codec-native-benchstat.txt), [exact metrics](codec-summary.json). Nine of ten Go before/after wall-time reductions are significant (p=0.002); PDF/four slices is not (p=0.065). All CPU-time reductions are significant. Native remains significantly faster on all ten cases.

Native reference is local OpenH264 2.6.0, commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, GCC 13.3 `-O3` with assembly/AVX2. Both encoders use camera real-time, low complexity, QP20 unless marked QP32, fixed one/four slices, one reference/spatial/temporal layer, Baseline/CAVLC, full-range BT.709 and a 60 FPS codec clock. Rate control, deblocking, periodic IDR, dropping, scene/background detection, AQ, denoising, LTR and load balancing are disabled. See [matching and corpus provenance](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching).

Codec timing includes encoding plus an owning output payload; it excludes RGB conversion, capture, RDP framing, network/ACK waits, initialization and file loading.

## Full EncodeDamage

Six alternating before/after pairs, 16 warmup and 60 measured frames, matching one/four-slice CPU affinity. This includes RGB conversion, damage preparation, encoding and owning payload construction. The existing full-path harness sets `Options.FrameRate=30`; codec-only uses 60. Capture, network, ACK credit and client presentation are excluded.

| 1080p case | Before ms | After ms | Median change | p-value |
| --- | ---: | ---: | ---: | ---: |
| ShortClip/1080p/1Slices | 34.972 | 32.619 | -6.73% | 0.002 |
| ShortClip/1080p/4Slices | 11.024 | 10.528 | -4.50% | 0.093 |
| Road/1080p/1Slices | 35.688 | 33.612 | -5.82% | 0.002 |
| Road/1080p/4Slices | 12.429 | 11.811 | -4.98% | 0.004 |
| Dense/1080p/1Slices | 76.781 | 73.818 | -3.86% | 0.002 |
| Dense/1080p/4Slices | 22.665 | 21.617 | -4.62% | 0.002 |

[Full-path statistics](full-benchstat.txt), [exact metrics](full-summary.json). Conversion code is unchanged in this milestone; allocations show no significant change. ShortClip/four-slice wall time and most p95 changes are inconclusive. No live “Encoding average” or network-throughput claim follows from these measurements.

## Rejected or held experiments

- Fixed-array macroblock metadata reduced neighbor/cache microbenchmarks but did not give broad frame gains: Road/one slice improved 1.32%, Road/four slices had a higher inconclusive median, and CPU geomean was neutral. Not merged. [Components](smb-next-benchstat.txt), [codec comparison](smb-next-codec-benchstat.txt).
- Sixteen-wide SATD address scheduling improved a few cases but regressed eight-wide controls, including 8×8 raw by 6.71%. Not merged. [Statistics](satd-schedule-next-benchstat.txt).
- Removing the P-skip decision closure gave mostly neutral results and opposing ~2% effects. Not merged. [Statistics](skip-control-benchstat.txt).
- The initial SSSE3 chroma wrapper improved fractional cases but added an extra fallback call that regressed integer/narrow cases. It was replaced by the retained direct checked dispatch. [Initial results](chroma-next-benchstat.txt), [retained results](chroma-tuned-benchstat.txt).

## Correctness and reproducibility

All ten complete streams remain byte-identical to both the original Go baseline and native OpenH264. FFmpeg decoded all **960 final frames** without errors; independent frame counts agree. [Hashes and decoder evidence](parity.txt).

H.264/AVC tests, race checks, AVX2-disabled and optional-CPU-features-disabled tests, Linux ARM64 cross-builds, and gopls diagnostics passed. [Commands and status](validation.json). New tests cover full coefficient ranges, QP tables, aliases/store ordering, negative/zero/overlapping strides where supported, visible slice lengths and guard pages. ARM64 cross-builds do not establish ARM64 runtime behavior or speed.

[Complete milestone patch](encoder-change.patch) applies to the original baseline; [source fingerprint](round5-sources.sha256) fixes the code boundary. [Measurement driver](measure_round5.py), [codec harness](compare_test.go.txt), [full harness](fullscreen_test.go.txt) and supporting scripts retain commands and workspace-specific paths. Supply the established corpus/native harness, adapt paths and overlays, and serialize CPU work. Do not reverse this patch over later improvements.

Fresh [Road flat profile](round5-road1-top.txt), [Road cumulative profile](round5-road1-cum.txt), [PDF flat profile](round5-pdf1-top.txt) and [PDF cumulative profile](round5-pdf1-cum.txt) provide the next profiling boundary. Remaining candidates include paired multiply-add center filters, scalar chroma DC Hadamard/quantization, and single-block DCT/IDCT argument marshalling. They are not part of this frozen milestone.
