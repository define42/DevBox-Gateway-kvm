# Software H.264 performance parity: round nine

**Native speed parity remains unmet.** The matched 1080p Go codec takes **1.50–1.64× native wall time with one slice** and **1.13–1.14× with four slices**. All checked streams remain byte-identical. The CPU metrics below also matter; four-slice wall time alone does not establish equal efficiency.

Four changes join the twenty-five retained groups in [round eight](../round8/README.md):

- SSE2 forward and inverse intra16 DC transforms replace scalar calls. Checked kernels improve 60–64%; 22 of 24 complete intra16 reconstruction cases improve 2.1–14.2%, with two inconclusive and no significant caller regressions. The unchanged scalar-forward control regresses 2.55%, while the four other scalar controls are inconclusive; this control change is not credited to the SIMD implementation. [Statistics](intra-dc-review/benchstat.txt).
- The lazy luma-skip path fuses each 8×8 group's DCT and quantization/max computation into one checked AVX2 call. It retains early rejection before later groups and preserves custom callbacks and malformed/alias fallbacks. All 16 eligible lazy-caller cases improve 19.5–43%; the complete skip encoder's eight transform cases improve 5.3–25.1%, with six no-transform controls inconclusive. A negative-stride fallback control regresses 3.26%; the frame fixtures use positive strides. Native uses separate calls here: this fusion specifically reduces Go overhead. [Statistics](skip-dct-quant-review/benchstat.txt).
- A checked AVX2 call quantizes and scans all 16 intra16 AC blocks, replacing four quantizer calls and sixteen scans. All 24 complete reconstruction cases improve 25.8–40.7%. Every AC block is scanned, including signed-domain cases where a maximum-based shortcut would be incorrect. [Statistics](intra-ac-review/benchstat.txt).
- Chroma skip uses a 312-byte startup cache of 52 QP thresholds, guarded by equality with both live effective quantization parameters. This removes division without changing mutable-table behavior. Of 36 complete-caller cases, 26 improve 1.7–18.8%, ten are inconclusive and none significantly regress. [Statistics](chroma-skip-qp-review/benchstat.txt).

These are separate component comparisons, not additive frame gains. Six alternating pairs use CPU 2 and 200 ms per case. Intra AC compares against round eight; the other three compare against round seven, with unchanged complete-caller benchmark fixtures. The combined frame comparison below uses round eight. The new paths introduce no allocation regressions; the reconstruction and chroma caller cases allocate zero bytes.

## Complete codec comparison

**Before is frozen round eight.** Six rotating previous/Go/native runs use 16 warmup and 80 measured frames. One slice uses CPU 2; four slices use CPUs 0–3. Intel i7-7700HQ, Go 1.27.3, CGO disabled, GOMAXPROCS=4; other workstation processes remain running.

| Case | Round eight ms | Current ms | Native ms | Change vs eight | p-value | Current/native |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| ShortClip/1920x1080/QP20/1Slices | 30.281 | 29.451 | 19.317 | -2.74% | 0.004 | 1.525× |
| ShortClip/1920x1080/QP20/4Slices | 9.577 | 9.462 | 8.292 | -1.21% | 0.394 | 1.141× |
| Road/1920x1080/QP20/1Slices | 30.074 | 29.602 | 19.526 | -1.57% | 0.180 | 1.516× |
| Road/1920x1080/QP20/4Slices | 10.484 | 10.574 | 9.251 | 0.86% | 0.240 | 1.143× |
| Road/1920x1080/QP32/1Slices | 22.133 | 21.779 | 13.274 | -1.60% | 0.026 | 1.641× |
| Road/1920x1080/QP32/4Slices | 8.286 | 8.104 | 7.143 | -2.20% | 0.041 | 1.135× |
| PDF/1920x1080/QP20/1Slices | 11.205 | 11.013 | 7.324 | -1.71% | 0.009 | 1.504× |
| PDF/1920x1080/QP20/4Slices | 4.709 | 4.682 | 4.091 | -0.59% | 0.937 | 1.144× |
| Road/1280x720/QP20/1Slices | 13.028 | 12.886 | 8.497 | -1.09% | 0.065 | 1.517× |
| Road/1280x720/QP20/4Slices | 4.751 | 4.668 | 4.377 | -1.76% | 0.394 | 1.066× |

There are 4 significant incremental wall-time reductions, 0 significant regressions and 6 inconclusive cases. The median wall-time geomean changes by -1.36%. [Complete statistics](codec-benchstat.txt), [native comparison](codec-native-benchstat.txt), [medians and CPU metrics](codec-summary.json).

Native is local OpenH264 2.6.0 commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, GCC 13.3 `-O3` with assembly/AVX2. Both use camera real-time, low complexity, QP20 unless QP32 is labeled, one reference/spatial/temporal layer, Baseline/CAVLC, fixed slices, full-range BT.709 and a 60 FPS codec clock. Rate control, deblocking, periodic IDR, dropping, scene/background detection, AQ, denoising, LTR and load balancing are disabled. [Matching provenance](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching).

Codec timing includes encoding and an owning payload. RGB conversion, initialization, loading, capture, framing, network/ACK waits and presentation are excluded. Absolute timings across separate rounds are not paired comparisons.

## Full EncodeDamage comparison

Before is original commit `641af9e6216055b6a9d918bdcba4ea7d40020750`; after includes all twenty-nine retained groups. Six alternating pairs include RGB conversion, damage preparation, encoding and owning payload construction, with 16 warmup and 60 measured frames. This harness uses FrameRate=30, unlike the codec-only 60 FPS clock. Capture, network/ACK waits and presentation are excluded.

| 1080p case | Original ms | Current ms | Change | p-value |
| --- | ---: | ---: | ---: | ---: |
| ShortClip/1080p/1Slices | 35.015 | 31.295 | -10.62% | 0.002 |
| ShortClip/1080p/4Slices | 10.984 | 10.093 | -8.11% | 0.004 |
| Road/1080p/1Slices | 35.841 | 32.068 | -10.53% | 0.002 |
| Road/1080p/4Slices | 12.516 | 11.647 | -6.94% | 0.065 |
| Dense/1080p/1Slices | 77.468 | 72.761 | -6.08% | 0.002 |
| Dense/1080p/4Slices | 22.933 | 21.603 | -5.80% | 0.002 |

[Full-path statistics](full-benchstat.txt), [exact metrics](full-summary.json). These measurements do not measure the live RDP “Encoding average”.

## Validation and remaining work

All ten streams match original Go and native byte for byte. FFmpeg decoded all 960 frames without errors; independent frame counts agreed. [Hash/decoder evidence](parity.txt). Package/AVC tests, race checks, AVX2-disabled and optional-features-disabled tests, Linux ARM64 cross-builds, gopls and whitespace checks passed. [Commands/results](validation.json). Cross-builds establish build compatibility, not ARM64 runtime performance.

Component tests cover full signed coefficient/parameter domains, all QPs, exact guard-page spans, aliases, callback replacement and partial failures against independent scalar or frozen-caller references. Production checks and encoding settings remain intact.

A direct SSE2 chroma-skip prototype was left out: six complete-caller cases improved, five regressed and 25 were inconclusive, with only a 0.16% geomean reduction. The threshold-cache alternative above has stronger complete-caller results. [Held SIMD statistics](chroma-skip-review/benchstat.txt).

Native's combined intra-SATD callbacks are absent in Go but unused by the matched camera/low-complexity dispatch, including IDR frames. Their absence does not explain this benchmark gap. Active intra4 prediction/scoring, motion refinement, entropy coding and macroblock bookkeeping remain candidates for measured work. Fresh [Road](round9-road1-cum.txt) and [PDF](round9-pdf1-cum.txt) profiles describe the final binary; stream capture is disabled during profiling. [Profile scope](round9-profile-scope.txt).

The [cumulative patch](encoder-change.patch) applies to the original commit. The [before-source fingerprint](before-sources.sha256) also identifies that original baseline; the codec-before fingerprint is [round eight](../round8/round8-sources.sha256). The [current fingerprint](round9-sources.sha256), [combined runner](measure_round9.py), raw samples and validation logs fix this milestone. Workspace-specific fixture/native paths need adaptation on another machine.
