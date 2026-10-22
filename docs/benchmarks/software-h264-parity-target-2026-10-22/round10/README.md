# Software H.264 performance parity: round ten

**Native speed parity remains unmet.** The matched 1080p Go codec takes **1.44–1.59× native wall time with one slice** in the first pass. Four-slice measurements are highly variable for old Go, new Go and native, so their raw median ratios below do not establish a reliable speed comparison. All checked streams remain byte-identical. Four-slice wall time alone does not establish equal CPU efficiency; CPU metrics are included below.

Two changes join the twenty-nine retained groups in [round nine](../round9/README.md):

- Center half-pixel filtering now performs both passes in one checked assembly call, using compact stack scratch initialized by the first pass. It supports widths 4/5/8/9/16/17, avoids the former Go scratch initialization and second assembly call, and preserves exact input/output spans and alias ordering. Of 20 complete filter cases, 19 improve 9.62–46.85% and one is inconclusive. Of 28 complete motion-refinement cases, five improve 2.31–5.73%, 23 are inconclusive and none significantly regress. All cases allocate zero bytes. [Statistics](center-stack-tuned-review/benchstat.txt).
- Active SAD-only VAA preprocessing now selects AVX2, matching native's selected instruction set. It processes two macroblocks per 32-byte load with independent accumulation chains and an exact 16-byte odd tail. Public validation, per-row frame commits, integer wrapping, input/output aliases and disabled-AVX2 fallback remain covered. Large unmasked caller cases improve 44–51%; alternating-mask large cases improve 12–15%. Across all 30 complete Set+Process cases there are 18 significant wins, three regressions and nine inconclusive cases, with a 23.25% geomean reduction and zero allocations. The regressions are 1280 sparse (+5.16%), 1904 sparse (+3.19%) and 1920 all-unchanged (+2.08%). The all-unchanged case never calls either VAA kernel; sparse cases each execute one three-macroblock run amid unchanged mask traversal. Source inspection does not establish the cause of the entire measured delta. These controls remain in the report. [Statistics](vaa-avx2-review/benchstat.txt).

Component results use six alternating pairs, CPU 2 and 200 ms per case against frozen round nine. Component percentages are not additive frame gains. The initial center prototype improved target widths but regressed some regular-width controls; the selected version supports those widths too. During development an unintended horizontal-dispatch edit failed existing tests before timing. It was corrected, a targeted guard regression test was added, and the horizontal function now matches round nine. [Initial failing evidence](center-stack-tuned-review/initial-failed-tests.txt), [initial prototype statistics](center-stack-review/benchstat.txt). The failing version was never merged into the main tree.

## Complete codec comparison

**Before is frozen round nine.** Six rotating previous/Go/native runs use 16 warmup and 80 measured frames. One slice uses CPU 2; four slices use CPUs 0–3. Intel i7-7700HQ, Go 1.27.3, CGO disabled, GOMAXPROCS=4; other workstation processes remain running.

| Case | Round nine ms | Current ms | Native ms | Change vs nine | p-value | Current/native |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| ShortClip/1920x1080/QP20/1Slices | 29.163 | 29.102 | 19.535 | -0.21% | 0.310 | 1.490× |
| ShortClip/1920x1080/QP20/4Slices | 10.453 | 10.395 | 24.092 | -0.55% | 0.937 | 0.431× |
| Road/1920x1080/QP20/1Slices | 29.055 | 29.360 | 19.436 | 1.05% | 0.589 | 1.511× |
| Road/1920x1080/QP20/4Slices | 18.034 | 17.389 | 11.110 | -3.57% | 1.000 | 1.565× |
| Road/1920x1080/QP32/1Slices | 21.412 | 21.105 | 13.289 | -1.43% | 0.180 | 1.588× |
| Road/1920x1080/QP32/4Slices | 18.277 | 18.200 | 7.100 | -0.42% | 0.937 | 2.563× |
| PDF/1920x1080/QP20/1Slices | 11.126 | 10.977 | 7.605 | -1.34% | 0.589 | 1.443× |
| PDF/1920x1080/QP20/4Slices | 8.792 | 9.755 | 4.407 | 10.96% | 0.699 | 2.214× |
| Road/1280x720/QP20/1Slices | 12.894 | 12.735 | 8.364 | -1.23% | 0.485 | 1.523× |
| Road/1280x720/QP20/4Slices | 10.296 | 9.080 | 5.373 | -11.81% | 0.589 | 1.690× |

There are 0 significant incremental wall-time reductions, 0 significant regressions and 10 inconclusive cases. The median wall-time geomean changes by -0.99%, but the first pass establishes no significant incremental wall-time gain. Four-slice confidence ranges are especially wide, reaching over 100% for some controls. [Complete statistics](codec-benchstat.txt), [native comparison](codec-native-benchstat.txt), [medians and CPU metrics](codec-summary.json).

A bounded confirmation repeats all five four-slice cases plus one preselected Road/QP20 one-slice control, using the same frozen binaries, 80 measured frames, six rotating orders and affinities. No first-pass samples are removed, and no host processes or performance settings are changed. [Confirmation scope](round10-timing-repeat/scope.json), [confirmation statistics](round10-timing-repeat/benchstat.txt), [host snapshot](round10-host-noise.json). The confirmation also finds no significant incremental wall-time improvement or regression in any of its six cases; four-slice variance remains large. Its Road/QP20 one-slice ratio is 1.50× native. A separate [four-slice diagnostic profile](round10-road4-noise-top.txt) also captured slow execution; its leading hotspots remain codec routines. It does not isolate the source of the variability and is not included as a benchmark sample. [Diagnostic scope](round10-road4-noise-scope.txt). Component gains therefore remain distinct from uncertain full-frame incremental gains.

Native is local OpenH264 2.6.0 commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, GCC 13.3 `-O3` with assembly/AVX2. Both use camera real-time, low complexity, QP20 unless QP32 is labeled, one reference/spatial/temporal layer, Baseline/CAVLC, fixed slices, full-range BT.709 and a 60 FPS codec clock. Rate control, deblocking, periodic IDR, dropping, scene/background detection, AQ, denoising, LTR and load balancing are disabled. [Matching provenance](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching).

Codec timing includes encoding and an owning payload. RGB conversion, initialization, loading, capture, framing, network/ACK waits and presentation are excluded. Absolute timings across separate rounds are not paired comparisons.

## Full EncodeDamage comparison

Before is original commit `641af9e6216055b6a9d918bdcba4ea7d40020750`; after includes all thirty-one retained groups. Six alternating pairs include RGB conversion, damage preparation, encoding and owning payload construction, with 16 warmup and 60 measured frames. This harness uses FrameRate=30, unlike the codec-only 60 FPS clock. Capture, network/ACK waits and presentation are excluded.

| 1080p case | Original ms | Current ms | Change | p-value |
| --- | ---: | ---: | ---: | ---: |
| ShortClip/1080p/1Slices | 40.758 | 36.113 | -11.40% | 0.002 |
| ShortClip/1080p/4Slices | 14.377 | 11.945 | -16.91% | 0.026 |
| Road/1080p/1Slices | 41.311 | 36.870 | -10.75% | 0.002 |
| Road/1080p/4Slices | 14.702 | 15.733 | 7.01% | 0.937 |
| Dense/1080p/1Slices | 87.798 | 81.996 | -6.61% | 0.002 |
| Dense/1080p/4Slices | 26.139 | 24.436 | -6.52% | 0.310 |

[Full-path statistics](full-benchstat.txt), [exact metrics](full-summary.json). Road/four-slice and Dense/four-slice cumulative comparisons are inconclusive in this pass; the other four show significant reductions. These measurements do not measure the live RDP “Encoding average”.

## Validation and remaining work

All ten streams match original Go and native byte for byte. FFmpeg decoded all 960 frames without errors; independent frame counts agreed. [Hash/decoder evidence](parity.txt). Package/AVC tests, race checks, AVX2-disabled and optional-features-disabled tests, Linux ARM64 cross-builds, gopls and whitespace checks passed. Race/fallback/cross-build scope now explicitly includes the processing package. [Commands/results](validation.json). Cross-builds establish build compatibility, not ARM64 runtime performance.

The VAA oracle links a separate C++ harness to the actual native library, checks native C/AVX2 and eligible SSE2 against a pixel oracle, then exports native frame and quadrant sums. All 146 cases agree, including 96 aligned SSE2 cases. Go baseline checks three implementations (438 comparisons); the new binary checks four (584), repeated with AVX2 disabled. No native code is linked to production Go. [Oracle scope and reproduction](vaa-native-oracle/README.md), [native result](vaa-native-oracle/native-parity.log), [Go result](vaa-native-oracle/candidate-parity.log), [fallback result](vaa-native-oracle/candidate-avx2-off-parity.log). Archived Go overlay sources use a `.go.txt` suffix; restore `.go` before executing the oracle scripts from this archive.

Two experiments remain unmerged. Reusing CAVLC scratch across a macroblock produced five small complete-caller wins, a no-residual regression and one public-writer regression, with an overall 0.43% geomean increase. Fusing intra4 DC/H/V SAD scoring produced no significant complete-caller wins, four regressions and a 1.81% geomean increase. [CAVLC scratch evidence](cavlc-scratch-review/benchstat.txt), [intra4 evidence](intra4-sad-review/benchstat.txt).

A later isolated trailing-one unrolling experiment, based on this round, also remains unmerged: 67 complete-writer/caller cases yield two significant wins, one unchanged known-zero control regression and 64 inconclusive results (geomean −1.77%). Native 447-suite parity and focused fallback tests pass, but broad speed improvement is unproven. [Statistics](cavlc-trailing-review/benchstat.txt), [decision and scope](cavlc-trailing-review/decision.txt).

Fresh [Road](round10-road1-cum.txt) and [PDF](round10-pdf1-cum.txt) profiles describe the final binary. Stream capture is disabled during profiling; separate diagnostic streams preserve byte parity. [Profile scope](round10-profile-scope.txt). Motion refinement, entropy coding, intra4 prediction/scoring and macroblock bookkeeping still account for substantial CPU time; native parity remains open.

The [cumulative patch](encoder-change.patch) applies to the original commit. The [before-source fingerprint](before-sources.sha256) identifies that original baseline; the codec-before fingerprint is [round nine](../round9/round9-sources.sha256). The [current fingerprint](round10-sources.sha256), [combined runner](measure_round10.py), raw samples and validation logs fix this milestone. Restore `compare_test.go.txt` and `fullscreen_test.go.txt` to their `.go` names before rebuilding the combined runner overlays. Workspace-specific fixture/native paths need adaptation on another machine.
