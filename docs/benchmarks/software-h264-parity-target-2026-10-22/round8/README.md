# Software H.264 performance parity: round eight

**Native speed parity remains unmet.** The matched 1080p Go codec takes **1.52–1.64× native wall time with one slice** and **1.11–1.16× with four slices**. All checked streams remain byte-identical. Four-slice wall time does not establish equal CPU efficiency; the complete CPU metrics are retained below.

Three changes join the twenty-two retained groups in [round seven](../round7/README.md):

- A checked SSE2 call scores the initial predictor and sole distinct motion candidate together, sharing encoded rows. Duplicate candidates skip redundant loop work. Clipping, strict ties, directional callbacks and malformed-input fallbacks remain. Nine of ten eligible caller cases improve 16–22%; one is inconclusive. All ten wide duplicate controls improve 5–14%, with no significant regression among the remaining controls. [Complete caller statistics](sad-two-tuned-review/benchstat.txt).
- An AVX2 call scores each half-pixel candidate pair while sharing encoded transforms. Vertical scoring remains before the horizontal filter. Fifteen of sixteen supported complete-refinement cases improve 2.5–8.5%; one is inconclusive. Eleven of twelve narrow controls are inconclusive; the 8×4/stride32/no-motion control regresses 3.11%. This tradeoff is retained explicitly for assessment against the full-frame results, not hidden by the component average. Every refinement case allocates zero bytes. [Statistics](half-pair-tuned-review/benchstat.txt).
- Chroma AC keep/drop scoring stops once the decision is fixed, after all four blocks have already been quantized and scanned. Five of sixteen complete-function cases improve 6.4–9.5%, eleven are inconclusive, and none significantly regress. [Statistics](chroma-decision-review/benchstat.txt).

These are component gains, not frame gains. Six alternating pairs use CPU 2 and 200 ms per motion case or 250 ms per chroma case. The initial paired-SAD prototype's int32 assembly result stores were corrected before any retention; the original fallback overhead was also tuned. [Original results](sad-two-review/benchstat.txt), [final source manifest](sad-two-tuned-ready.txt). Caller-local motion-state allocation controls remain equal to baseline; they are distinct from zero-allocation reused-state cases.

## Complete codec comparison

**Before is frozen round seven.** Six rotating previous/Go/native runs use 16 warmup and 80 measured frames. One slice uses CPU 2; four slices use CPUs 0–3. Intel i7-7700HQ, Go 1.27.3, CGO disabled, GOMAXPROCS=4; other workstation processes remain running.

| Case | Round seven ms | Current ms | Native ms | Change vs seven | p-value | Current/native |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| ShortClip/1920x1080/QP20/1Slices | 29.877 | 29.614 | 19.250 | -0.88% | 0.004 | 1.538× |
| ShortClip/1920x1080/QP20/4Slices | 9.423 | 9.303 | 8.405 | -1.26% | 0.589 | 1.107× |
| Road/1920x1080/QP20/1Slices | 29.967 | 29.596 | 19.061 | -1.24% | 0.093 | 1.553× |
| Road/1920x1080/QP20/4Slices | 10.550 | 10.613 | 9.266 | 0.60% | 0.818 | 1.145× |
| Road/1920x1080/QP32/1Slices | 21.780 | 21.745 | 13.234 | -0.16% | 0.310 | 1.643× |
| Road/1920x1080/QP32/4Slices | 8.370 | 8.201 | 7.091 | -2.02% | 0.589 | 1.157× |
| PDF/1920x1080/QP20/1Slices | 11.310 | 11.148 | 7.336 | -1.44% | 0.015 | 1.520× |
| PDF/1920x1080/QP20/4Slices | 4.629 | 4.559 | 4.029 | -1.50% | 0.240 | 1.132× |
| Road/1280x720/QP20/1Slices | 13.072 | 12.906 | 8.393 | -1.27% | 0.002 | 1.538× |
| Road/1280x720/QP20/4Slices | 4.815 | 4.696 | 4.484 | -2.47% | 0.093 | 1.047× |

There are 3 significant incremental wall-time reductions, 0 significant regressions and 7 inconclusive cases. The median wall-time geomean changes by -1.17%. Inconclusive differences and noisy four-slice samples do not establish parity. [Complete statistics](codec-benchstat.txt), [native comparison](codec-native-benchstat.txt), [medians and CPU metrics](codec-summary.json).

Native is local OpenH264 2.6.0 commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, GCC 13.3 `-O3` with assembly/AVX2. Both use camera real-time, low complexity, QP20 unless QP32 is labeled, one reference/spatial/temporal layer, Baseline/CAVLC, fixed slices, full-range BT.709 and a 60 FPS codec clock. Rate control, deblocking, periodic IDR, dropping, scene/background detection, AQ, denoising, LTR and load balancing are disabled. [Matching provenance](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching).

Codec timing includes encoding and an owning payload. RGB conversion, initialization, loading, capture, framing, network/ACK waits and presentation are excluded. Absolute timings across separate rounds are not paired comparisons.

## Full EncodeDamage comparison

Before is original commit `641af9e6216055b6a9d918bdcba4ea7d40020750`; after includes all twenty-five retained groups. Six alternating pairs include RGB conversion, damage preparation, encoding and owning payload construction, with 16 warmup and 60 measured frames. This existing harness uses FrameRate=30, unlike the codec-only 60 FPS clock. Capture, network/ACK waits and presentation are excluded.

| 1080p case | Original ms | Current ms | Change | p-value |
| --- | ---: | ---: | ---: | ---: |
| ShortClip/1080p/1Slices | 34.930 | 31.344 | -10.27% | 0.002 |
| ShortClip/1080p/4Slices | 11.178 | 10.177 | -8.96% | 0.002 |
| Road/1080p/1Slices | 35.951 | 32.161 | -10.54% | 0.002 |
| Road/1080p/4Slices | 12.422 | 11.440 | -7.91% | 0.002 |
| Dense/1080p/1Slices | 77.079 | 71.390 | -7.38% | 0.002 |
| Dense/1080p/4Slices | 22.857 | 21.491 | -5.97% | 0.002 |

[Full-path statistics](full-benchstat.txt), [exact metrics](full-summary.json). These measurements do not measure the live RDP “Encoding average”.

## Experiments left out

- Expanding the CAVLC level table from 8 to 16 KiB helped mixed-level components, but all ten complete-codec time changes were inconclusive. Its native stream hashes and 960 decoded frames passed. [Frame statistics](cavlc-table128-next-codec-benchstat.txt).
- A packed slow-level return had no broad benefit and regressed a dense control. [Statistics](cavlc-remaining-review/packed-benchstat.txt).
- SIMD extraction of four chroma-DC coefficients improved sparse/dense cases by 1.6–4%, but regressed extracted-zero controls by 2.1–5.6%. [Statistics](cavlc-chroma-review/benchstat.txt).
- Combining pending-bit writes for a sole ±1 coefficient improved its three targeted cases by 3.2–3.9%, but regressed single-±2 controls by 1.0–3.1% and an end14 dense control by 2.6%. Other sparse cases did not improve. [Statistics](cavlc-single-review/benchstat.txt).

The single-coefficient experiment passed 447 shared suites against the actual native writer in automatic and scalar modes, plus independent Go reference/failure/alias tests. These passes establish correctness, not sufficient performance benefit. Its [fixture generator](generate_single_fixtures.py) extends the [round-seven native harness](../round7/cavlc-native/README.md). The timing region is 32 consecutive complete block writes; all three implementations load the same fixture.

Diagnostic-only counters find sole ±1 coefficients in 19.96% of Road CAVLC calls and 8.94% of PDF calls. Counts cover 616 encoded frames, including 16 warmup; stream hashes match the prior counter run and final round-seven streams. [Road totals](sparse-count-road1-totals.json), [PDF totals](sparse-count-pdf1-totals.json). The counters use a serial, single-encoder diagnostic tree and their timings are discarded.

## Validation and remaining work

All ten streams match original Go and native byte for byte. FFmpeg decoded all 960 frames without errors; independent counts agreed. [Hash/decoder evidence](parity.txt). Package/AVC tests, race checks, AVX2-disabled and optional-features-disabled tests, Linux ARM64 cross-builds, gopls and whitespace checks passed. [Commands/results](validation.json). Cross-builds establish build compatibility, not ARM64 runtime performance.

The source audit used [round-seven profiles](../round7/round7-road1-cum.txt); fresh [Road](round8-road1-cum.txt) and [PDF](round8-pdf1-cum.txt) profiles of the final round-eight binary are also retained. Stream capture is disabled during profiling; [scope and limitations](round8-profile-scope.txt). Native still has SSE2 intra16 DC transforms where Go uses scalar processing. Lazy luma-skip DCT followed by quantization is another bounded fusion candidate; native itself uses separate calls there. Neither new prototype has a retained speedup claim here. CAVLC sparse/zero extraction and higher-level motion/macroblock work still contribute to the gap. Production memory checks and encoding settings remain intact.

The [cumulative patch](encoder-change.patch) applies to the original commit. The [before-source fingerprint](before-sources.sha256) also identifies that original baseline; the codec-before fingerprint is [round seven](../round7/round7-sources.sha256). The [source fingerprint](round8-sources.sha256), [combined runner](measure_round8.py), raw component/full-frame samples and validation logs fix this milestone. Workspace-specific fixture/native paths need adaptation on another machine.
