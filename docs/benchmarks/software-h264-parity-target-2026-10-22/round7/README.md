# Software H.264 performance parity: round seven

**Native speed parity remains unmet.** The matched 1080p Go codec takes **1.54–1.65× native wall time with one slice** and **1.07–1.15× with four slices**. All checked streams remain byte-identical. Parallel wall time does not establish equal CPU efficiency; CPU measurements are retained below.

Five changes join the seventeen retained groups in [round six](../round6/README.md):

- Complete SSE2 luma/chroma plane predictors include coefficient reduction. Exact top loads and byte-only left gathers preserve reference bounds and aliases; invalid/nonpositive strides retain the legacy path. Checked prediction improves 19–26%; ten of twelve complete luma/chroma mode-chain cases improve 6–15%, with two inconclusive. [Statistics](plane-component-benchstat.txt).
- One AVX2 call scores four quarter-pixel candidates while sharing encoded row transforms. Existing span/alias validation, strict tie order, scratch swaps and winner generation remain. With local parameters created per invocation, the corrected quarter-refinement path improves all five wide shapes by 6.4–11.5%; narrow controls are inconclusive. The complete `MeRefineFracPixel` caller improves ten of twenty-eight cases by 2.4–8.4%, with eighteen inconclusive and no significant regressions; every case allocates zero bytes. [Corrected caller statistics](quarter-caller-benchstat.txt). The [preliminary component statistics](motion-component-benchstat.txt) reused parameters outside the timer and are retained only as kernel evidence.
- Four fixed-shape SATD wrappers call the unchanged private kernel directly after the same ordered checks. Thirteen of sixteen actual dispatch cases improve 4–14%; three are inconclusive. All generic and 4×4 controls are inconclusive. [Statistics](satd-wrapper-component-benchstat.txt).
- Scalar motion-cost temporaries replace a four-element local array. Evaluation, alias stores, ties, overflow and failure order remain identical. Six helper cases improve 4–14%; the helper occupies a small part of frame time. [Statistics](diamond-select-benchstat.txt).
- A known-zero CAVLC branch returns before coefficient scratch setup. It preserves nil-writer/nC validation order and flush/commit behavior. The initial early-return change improves known-zero sequences 17.6–18.5%; direct pending-bit updates then improve that path a further 51–55%, reaching 6.4–7.1 ns/block versus native 6.3 ns/block. No significant nonzero regressions were measured. These are two separately paired experiments, not a single combined percentage. [Initial statistics](cavlc-native/zero-benchstat.txt), [direct-update statistics](cavlc-native/direct-benchstat.txt).

Component results use six alternating pairs or rotating triples, CPU 2, 200 ms per case (diamond and CAVLC use 250 ms). These percentages are not full-frame gains. All final measured Go components allocate zero bytes. [Complete caller statistics](quarter-caller-benchstat.txt), [allocation regression confirmation](quarter-allocation-regression-confirmed.log), [compiler escape analysis](quarter-noescape-compiler-summary.txt). The first batch-callback version hid a per-call allocation because its benchmark reused parameters created before the timer; the corrected caller benchmark creates the local parameters on every invocation.

## Complete codec comparison

**Before here is frozen round six, not the original commit.** Six rotating previous/Go/native runs use 16 warmup and 80 measured frames. One slice uses CPU 2; four slices use CPUs 0–3. Intel i7-7700HQ, Go 1.27.3, CGO disabled, GOMAXPROCS=4; other workstation processes remain running.

| Case | Round six ms | Current ms | Native ms | Change vs six | p-value | Current/native |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| ShortClip/1920x1080/QP20/1Slices | 30.466 | 30.177 | 19.206 | -0.95% | 0.240 | 1.571× |
| ShortClip/1920x1080/QP20/4Slices | 9.728 | 9.406 | 8.778 | -3.31% | 0.041 | 1.072× |
| Road/1920x1080/QP20/1Slices | 30.617 | 29.994 | 19.444 | -2.04% | 0.065 | 1.543× |
| Road/1920x1080/QP20/4Slices | 10.722 | 10.683 | 9.414 | -0.37% | 0.937 | 1.135× |
| Road/1920x1080/QP32/1Slices | 22.497 | 21.881 | 13.251 | -2.74% | 0.015 | 1.651× |
| Road/1920x1080/QP32/4Slices | 8.751 | 8.501 | 7.375 | -2.85% | 0.394 | 1.153× |
| PDF/1920x1080/QP20/1Slices | 11.400 | 11.347 | 7.309 | -0.46% | 0.589 | 1.552× |
| PDF/1920x1080/QP20/4Slices | 4.698 | 4.700 | 4.193 | 0.05% | 0.589 | 1.121× |
| Road/1280x720/QP20/1Slices | 13.412 | 13.085 | 8.350 | -2.44% | 0.026 | 1.567× |
| Road/1280x720/QP20/4Slices | 4.944 | 4.785 | 4.416 | -3.22% | 0.180 | 1.084× |

There are 3 significant incremental wall-time reductions, 0 significant regressions and 7 inconclusive cases. Several four-slice samples have wide variability from the shared workstation, so inconclusive differences must not be treated as parity or a measured gain. The median wall-time geomean changes by -1.84%. [Complete statistics](codec-benchstat.txt), [direct native comparison](codec-native-benchstat.txt), [exact medians and CPU time](codec-summary.json).

Native is the locally matched OpenH264 2.6.0 commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, GCC 13.3 `-O3` with assembly/AVX2. Both use camera real-time, low complexity, QP20 unless labeled QP32, fixed one/four slices, one reference/spatial/temporal layer, Baseline/CAVLC, full-range BT.709 and a 60 FPS codec clock. Rate control, deblocking, periodic IDR, dropping, scene/background detection, AQ, denoising, LTR and load balancing are disabled. [Input and matching provenance](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching).

Codec timing includes encoding and an owning payload. RGB conversion, initialization, file loading, capture, RDP framing, network/ACK waits and presentation are excluded. Absolute times from different rounds are not paired comparisons.

## Full EncodeDamage comparison

Before is original commit `641af9e6216055b6a9d918bdcba4ea7d40020750`; after contains all twenty-two retained groups. Six alternating pairs include RGB conversion, damage preparation, encoding and owning payload construction. There are 16 warmup and 60 measured frames. This existing harness uses FrameRate=30; codec-only uses 60. Capture, network/ACK waits and client presentation are excluded.

| 1080p case | Original ms | Current ms | Change | p-value |
| --- | ---: | ---: | ---: | ---: |
| ShortClip/1080p/1Slices | 35.643 | 31.781 | -10.83% | 0.002 |
| ShortClip/1080p/4Slices | 11.351 | 10.026 | -11.68% | 0.004 |
| Road/1080p/1Slices | 35.684 | 32.386 | -9.24% | 0.002 |
| Road/1080p/4Slices | 12.626 | 11.488 | -9.01% | 0.180 |
| Dense/1080p/1Slices | 77.706 | 72.761 | -6.36% | 0.002 |
| Dense/1080p/4Slices | 22.762 | 21.461 | -5.71% | 0.002 |

[Full-path statistics](full-benchstat.txt), [exact metrics](full-summary.json). These local measurements do not measure the live RDP “Encoding average”.

## What the native CAVLC comparison revealed

The harness links the actual native static-library writer; it does not copy the native implementation. It checks 333 suites containing 3,360 blocks against native automatic and scalar extraction, including every pending-bit position and partial output on overflow. All byte/state/error checks passed. The timing region includes 32 continuous block writes with identical coefficients, nC, property, pending bits, alignment and output capacity. [Harness and scope](cavlc-native/README.md), [source](cavlc-native/native_cavlc.cpp), [full comparison](cavlc-native/zero-benchstat.txt).

Dense small-coefficient sequences approach native throughput. Known-zero blocks also approach native after the retained direct-state update; extracted-zero and sparse/mixed nonzero blocks still have overhead. The packed level table helps dense inputs; it cannot help zero blocks. This evidence motivated the retained early known-zero branch.

The alternative bounded-range rewrite of level/run loops was **rejected**: its first dense tests improved 2–5%, but the expanded matched corpus exposed 0.8–3.1% regressions in chroma/zero cases. It is absent from production. [Initial statistics](cavlc-bounds-benchstat.txt), [expanded statistics](cavlc-native/matched-benchstat.txt). The initial expanded run has twelve matched native cases plus three Go-only known-zero controls because its native filter omitted the hyphenated name. The later zero-path run fixes the filter and matches all fifteen cases; do not compare mismatched geomeans.

## Integration regression caught and corrected

The first five-change integration regressed codec time 1.8–6.1% in six significant cases. Full `EncodeDamage` exposed 5,909–10,506 allocations/frame instead of 8–30: passing a local `SQuarRefineParams` through an indirect callback made it escape. The final implementation uses a private capability flag and a direct architecture-specific helper, preserving existing callback opt-outs. A regression test and complete-caller benchmark create parameters inside each invocation. The failed attempt is preserved in [allocation-regression evidence](allocation-regression/codec-benchstat.txt) and is excluded from the final table.

## Validation and remaining gap

All ten complete streams match both original Go and native byte for byte. FFmpeg decoded 960 frames without errors and independent frame counts agreed. [Hashes and decoder evidence](parity.txt). Package/AVC tests, race checks, AVX2-disabled and optional-CPU-features-disabled tests, Linux ARM64 cross-builds and gopls diagnostics passed. [Commands/results](validation.json). ARM64 cross-builds establish build compatibility, not runtime performance.

CPU profiling disables diagnostic stream capture; the hash comparison uses separately captured streams. Process-wide profiles still include setup and warmup. [Profile scope](round7-profile-scope.txt).

Final [Road flat](round7-road1-top.txt), [Road cumulative](round7-road1-cum.txt), [PDF flat](round7-pdf1-top.txt) and [PDF cumulative](round7-pdf1-cum.txt) profiles preserve the next optimization boundary. [Call-count diagnostic](call-count-diagnostic-README.txt) measures actual zero-block and initial-search frequencies on the same Road/PDF fixtures; its timings are not benchmarks. [Diagnostic stream equality](call-count-parity.json) checks all 616 frames per fixture. [Remaining-gap review](remaining-gap-review.txt) distinguishes measured opportunities from untested hypotheses.

The [cumulative patch](encoder-change.patch) applies to the original commit; the [source fingerprint](round7-sources.sha256) fixes this milestone. [Driver](measure_round7.py), [codec harness](compare_test.go.txt), [full-path harness](fullscreen_test.go.txt), component drivers and raw samples retain the experiment. Workspace-specific fixture/native paths need adaptation on another machine. No encoding settings, quality decisions or production safety checks were relaxed.
