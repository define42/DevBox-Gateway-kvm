# Software H.264 performance work, 2026-10-22

The completed changes reduce full 1080p road-video encoding time by **about 8% with both one and four slices**, including RGB conversion, while preserving output. Native OpenH264 remains **1.60–1.84× faster** in the matched codec-only suite; performance parity has not been reached.

## Changes

- Four-block AVX2 forward transform, with the previous SSE2 behavior retained for aliases and negative strides.
- SSSE3/POPCNT CAVLC extraction, with exact stores and scalar fallback for small blocks, short inputs and unsupported CPUs.
- Packed SATD reduction and horizontal half-pixel filtering; typed-pointer SATD assembly arguments retain the existing visible-length checks.
- Typed-pointer single-SAD and quantize/scan calls. The separate four-candidate SAD ABI experiment was removed because its benchmark did not show a reliable gain.
- Completed inter-luma quantization and scanning now process 16 blocks in one checked assembly call. Aliases, including quantization tables overlapping later groups, use the original sequential calls. Lazy skip decisions remain unchanged.

No search thresholds, prediction choices, quantizers, frame dropping, rate control or bitstream settings were changed. Component results and native-call limitations are recorded in [components.md](components.md).

## Complete-frame measurements

| Full 1080p EncodeDamage | Before ms/frame | After ms/frame | Change | p, n=6 |
| --- | ---: | ---: | ---: | ---: |
| ShortClip / 1Slices | 44.90 | 41.25 | -8.1% | 0.004 |
| ShortClip / 4Slices | 17.25 | 15.81 | -8.4% | 0.093 |
| Road / 1Slices | 45.13 | 41.45 | -8.2% | 0.002 |
| Road / 4Slices | 18.89 | 17.33 | -8.2% | 0.009 |
| Dense / 1Slices | 99.09 | 89.11 | -10.1% | 0.002 |
| Dense / 4Slices | 33.41 | 30.05 | -10.0% | 0.093 |

All three single-slice gains and the four-slice road-video gain are statistically significant. The four-slice ShortClip and Dense wall-time reductions remain inconclusive at n=6. Dense four-slice CPU time did fall significantly: 112.66 → 99.95 ms/frame (−11.28%, p=0.009).

For road video, the separately recorded encoding stage fell from 43.42 → 39.78 ms with one slice and 18.04 → 16.43 ms with four slices. These are benchmark values, not observations of a live RDP dashboard.

See [full-pipeline statistics](full-benchstat.txt), [before samples](full-before.txt), and [after samples](full-after.txt).

| Codec-only case | Before ms | After ms | Change | Native ms | Go/native |
| --- | ---: | ---: | ---: | ---: | ---: |
| ShortClip/1920x1080/QP20/1Slices | 52.31 | 47.99 | -8.3% | 27.54 | 1.74× |
| ShortClip/1920x1080/QP20/4Slices | 19.85 | 17.74 | -10.6% | 9.94 | 1.78× |
| Road/1920x1080/QP20/1Slices | 43.07 | 46.05 | +6.9% | 25.88 | 1.78× |
| Road/1920x1080/QP20/4Slices | 19.64 | 19.41 | -1.2% | 10.56 | 1.84× |
| Road/1920x1080/QP32/1Slices | 31.10 | 32.63 | +4.9% | 18.59 | 1.75× |
| Road/1920x1080/QP32/4Slices | 15.08 | 14.19 | -5.8% | 7.89 | 1.80× |
| PDF/1920x1080/QP20/1Slices | 17.53 | 15.58 | -11.1% | 9.12 | 1.71× |
| PDF/1920x1080/QP20/4Slices | 7.90 | 7.63 | -3.5% | 4.75 | 1.60× |
| Road/1280x720/QP20/1Slices | 22.26 | 20.02 | -10.1% | 11.93 | 1.68× |
| Road/1280x720/QP20/4Slices | 10.27 | 9.43 | -8.2% | 5.56 | 1.70× |

**The codec-only before/after differences are inconclusive for every case in this run.** The active workstation produced wide intervals, including apparent slowdowns in two road-video medians. These are descriptive medians, not evidence of either a gain or regression. Native remains significantly faster in every matched case (p=0.002–0.004). See [before/after statistics](codec-before-after-benchstat.txt) and [native statistics](codec-native-benchstat.txt), plus [Go baseline](codec-before.txt), [Go candidate](codec-after.txt), and [native samples](codec-native.txt).

The codec comparison uses six serial repetitions, rotating through all six permutations of before/after/native execution order. Each process warms up for 16 frames and measures 80 frames. Tables report the median of the six per-run averages. Process CPU time sums all threads; it is different from frame latency.

The separate `EncodeDamage` comparison includes RGB conversion, macroblock damage preparation, codec encoding and the returned owning payload. It uses full-frame damage, 16 warmup frames and 60 measured frames, six alternating before/after pairs. It excludes capture, RDP framing, socket writes, ACK waits and client presentation.

## Inputs and matching

The baseline is the frozen working tree immediately before this round, including earlier encoder optimizations; it is not a clean `HEAD` checkout. Before and after encoder source fingerprints are retained. [encoder-change.patch](encoder-change.patch) records this round's complete encoder delta; reverse it only in a temporary copy of the final tree to reconstruct the measured baseline, then verify `before-sources.sha256`. Both Go comparison binaries use Go 1.27.3, `CGO_ENABLED=0`, and `GOMAXPROCS=4` on an Intel i7-7700HQ.

Native is the same optimized OpenH264 2.6.0 static build used in the [previous comparison](../software-h264-vs-openh264-2026-10-21/README.md), source commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, GCC 13.3.0 `-O3`, assembly and AVX2 enabled. This is a comparison with the port's reference version, not the latest upstream release.

Both codec harnesses use the production Go encoder's effective configuration: camera real-time, low complexity, 60 FPS codec clock, QP20 unless labeled QP32, one reference/spatial/temporal layer, fixed one/four slices, Baseline/CAVLC, full-range BT.709 metadata; no rate control, deblocking, periodic IDR, frame skipping, scene/background detection, adaptive quantization, denoising, LTR or load balancing. Input is the same preloaded I420 data; encoding and concatenation into an owning output buffer are timed. File loading, conversion, initialization, stream validation and output-file writes are outside codec timing.

The expanded corpus comprises:

- **ShortClip:** the original five-frame Cisco Absolute Power excerpt, scaled to 1920×1080 and converted by the gateway's RGB conversion.
- **Road:** 54 frames from the local OpenH264 `res/VID_1920x1080_cabac_temporal_direct.264` driving sequence, decoded to 1080p I420; also bilinearly scaled to 720p. Tested at QP20 and, at 1080p, QP32.
- **PDF:** 50 frames from `res/Adobe_PDF_sample_a_1024x768_50Frms.264`, decoded and bilinearly scaled to 1920×1080.
- **Dense:** the existing eight-frame deterministic high-entropy motion fixture, used in the complete `EncodeDamage` comparison.

These are broader inputs than the earlier five-frame loop, but remain short repeated sequences on one active workstation. No CPU affinity or host isolation was imposed. Results do not establish sustained production FPS, live dashboard averages, performance on other CPUs, or ARM64 speed.

## Output and safety validation

All ten codec cases produced byte-identical before/after/native streams, including the initial IDR and parameter sets. Each complete stream contains 96 frames. FFmpeg decoded each final stream with `-xerror` and no diagnostics; frame counts were independently checked. See [parity.txt](parity.txt).

Kernel tests cover scalar/native parity, exhaustive CAVLC masks and coefficient values, SATD binary patterns and pixel impulses, all quantizers, aliases, visible slice lengths, unaligned data and guard pages. Final validation passed: the full H.264 and RDP test suites; race checks for common kernels, encoder and AVC integration; AVX2-, SSSE3-, POPCNT- and all-optional-features-disabled runs; Linux ARM64 cross-builds of common, encoder and AVC tests; gopls diagnostics; and the repository lint configuration (Go 1.26.9 for the pinned linter). Cross-builds do not constitute ARM64 runtime or speed testing. Commands and exit status are in [validation.json](validation.json), with individual `validation-*.txt` logs.

## Final profile and remaining work

A separate final 300-frame road-video profile, one slice at QP20, contains 12.31 seconds of sampled CPU. Fractional motion refinement accounts for 17.87% cumulative CPU; `WriteBlockResidualCavlc` accounts for 11.94% cumulative / 7.31% flat; SATD assembly accounts for 5.20% flat. Cumulative entries overlap and must not be added. The new DCT kernel accounts for 1.87% flat. [Flat profile](after-profile-top.txt), [cumulative profile](after-profile-cum.txt), [profile data](after.pprof).

The profile taken before the final luma batch attributed 3.38% cumulative / 1.15% flat to the four-block quantize/scan wrapper, motivating the measured batch. [Recorded pre-batch profile](before-luma-profile-top.txt). These profiles explain where work remains; their wall times are not substitutes for the paired benchmarks.

The remaining native gap calls for more work in fractional refinement and the complete entropy writer. Native's small SATD shapes use both AVX2 lanes more effectively, and native coefficient extraction remains faster on ordinary partial blocks. This round does not establish the proposed within-5% parity target.

## Reproduction

Measurement workspace: `/tmp/h264-native-parity-20261021`; runs began on October 21 and finished on October 22. The retained scripts preserve that run's paths; adjust them when moving the workspace.

1. Prepare the native static library as described in the previous comparison. The component helpers and their exact link commands are in [components.md](components.md).
2. Use `prepare.py` to decode the two local reference streams. `fixtures.json` records commands, frame counts and hashes. Reuse the original short-clip I420/RGBA fixtures described in the previous report.
3. Copy `compare_test.go.txt` and `fullscreen_test.go.txt` to temporary `.go` files and map virtual files in `internal/rdp/avc` to them using Go test overlays. Build before/after AVC test binaries from the frozen baseline/current tree, and build `native_bench.cpp` against the same static library. `build.py` records the exact commands used in this run.
4. Run `measure.py --preflight` for full stream comparison and independent decoding. Run `measure.py` separately for codec timing and `measure_full.py` for complete `EncodeDamage` timing. Run `summarize.py` and `benchstat` on the resulting samples.
5. Run `validate.py` separately. Do not run builds, tests, profiles, decoders or other benchmarks concurrently with timing.
