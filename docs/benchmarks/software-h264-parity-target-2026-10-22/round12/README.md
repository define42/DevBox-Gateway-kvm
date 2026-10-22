# Software H.264 performance parity: round twelve

**Native speed parity remains unmet.** Matched 1080p one-slice Go median times are **1.48–1.57× native**; four-slice wall ratios are 0.45–1.28×. CPU ratios across those 1080p cases are 0.54–1.57×. At least one four-slice wall comparison has a confidence range above 20%; those medians are especially uncertain. All ten checked streams remain byte-identical to original Go and native. Similar wall time alone would not establish equal CPU efficiency.

This round adds SSE2 horizontal-down, horizontal-up and vertical-right I4 predictors to the thirty-three groups retained in [round eleven](../round11/README.md), for **thirty-four groups**. Native installs directional MMXEXT predictors; Go had scalar directional construction/copying. Checked Go wrappers now gather the exact source bytes in the original order and pass copied values to an SSE2 leaf. Source aliases, unusual strides, validation/panic order and exact sixteen-byte output stores are preserved. Native's wider source reads are not copied. Other predictors and encoder settings remain unchanged. [Source and native references](intra4-directional-next-README.txt).

The six dispatched predictor cases improve 36.04–49.73%; all six unchanged scalar controls are inconclusive. Of 22 complete `WelsMdI4x4Fast` caller cases, ten improve 3.41–7.01%, twelve are inconclusive and none significantly regress. The caller-only descriptive geomean is −3.06%; all 408 samples report zero allocations. The two first-block/DC128 controls are inconclusive. Six alternating pairs use CPU 2 and 200 ms per case against frozen round eleven. These component results are not additive frame gains. [Component audit](intra4-directional-review/README.md), [statistics](intra4-directional-review/benchstat.txt).

## Selection and the predeclared longer confirmation

**Decision: retained.** Retain the directional predictors for their demonstrated component benefit: all six dispatched predictor cases improve 36.04–49.73%, and ten of twenty-two complete I4 caller cases improve 3.41–7.01%, with no significant caller regression and zero allocations. All correctness, stream-parity, decoder, fallback and cross-build gates pass. The first codec pass was mixed (ShortClip +1.44%, Road QP20 −1.52%); the single predeclared longer confirmation found all five one-slice cases inconclusive and did not reproduce the ShortClip regression. This supports retaining a bounded component optimization, not claiming a demonstrated full-frame gain or equal performance. All six incremental full-frame results remain inconclusive; native parity remains unmet. Both passes are preserved separately and no further significance-seeking repeat is planned.

The original ten-case codec pass had a ShortClip 1080p/one-slice regression of +1.44% (p=0.026), a Road QP20 1080p/one-slice improvement of −1.52% (p=0.041), and eight inconclusive cases. All six incremental full `EncodeDamage` comparisons were inconclusive. Four-slice timing was noisy. These observations remain archived and are not replaced by the confirmation.

One bounded confirmation was declared before its results: **all five one-slice codec settings**, six alternating round-eleven/candidate pairs, 240 measured plus 16 warmup frames, CPU 2 and the same frozen binaries. There were no native reruns. It uses a separate longer run; its samples are not pooled with the first 80-frame pass.

| One-slice case | Round eleven ms | Candidate ms | Wall change | CPU change | p-value |
| --- | ---: | ---: | ---: | ---: | ---: |
| ShortClip/1920x1080/QP20/1Slices | 40.544 | 39.034 | -3.72% | -3.72% | 0.310 |
| Road/1920x1080/QP20/1Slices | 39.974 | 40.320 | +0.86% | +1.44% | 0.818 |
| Road/1920x1080/QP32/1Slices | 30.204 | 30.080 | -0.41% | -0.04% | 0.699 |
| PDF/1920x1080/QP20/1Slices | 15.797 | 13.677 | -13.42% | -13.41% | 0.818 |
| Road/1280x720/QP20/1Slices | 16.737 | 16.128 | -3.64% | -3.35% | 0.937 |

The confirmation has 0 significant wall-time reductions, 0 significant regressions and 5 inconclusive cases. Its descriptive median wall-time geomean is -4.20%; that aggregate has no separate significance test. [Predeclared scope](confirmation/provenance.json), [all raw statistics](confirmation/benchstat.txt), [metrics](confirmation/summary.json), [explicit decision](decision.json). Inconclusive results do not prove equal performance, and this confirmation does not establish a full-frame or live RDP gain.

The confirmation driver verified the binary hashes and preserved the first-pass artifacts before and after its run. A **separate post-run** check compared all five fixture cases (4 distinct input files) against the validated first-pass hashes; all matched. No initial or mid-confirmation fixture hash check is claimed. [Post-run input verification](confirmation/post-run-input-verification.json), [archive preservation audit](decision-archive-audit.json).

## Complete codec comparison

**The baseline is frozen round eleven.** Six rotating triples compare round eleven/current/native with 16 warmup and 80 measured frames per run. One-slice cases use CPU 2; four-slice cases use CPUs 0–3. Linux amd64, Intel i7-7700HQ, Go 1.27.3, CGO disabled and GOMAXPROCS=4. Other workstation activity was not disabled. Stream capture is off during timing.

| Case | Round eleven ms | Current ms | Native ms | Change vs eleven | p-value | Wall/native | CPU/native |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| ShortClip/1920x1080/QP20/1Slices | 28.034 | 28.438 | 19.182 | +1.44% | 0.026 | 1.483× | 1.482× |
| ShortClip/1920x1080/QP20/4Slices | 11.567 | 9.177 | 20.365 | -20.66% | 0.180 | 0.451× | 0.541× |
| Road/1920x1080/QP20/1Slices | 28.420 | 27.989 | 18.917 | -1.52% | 0.041 | 1.480× | 1.479× |
| Road/1920x1080/QP20/4Slices | 26.954 | 23.105 | 27.137 | -14.28% | 0.589 | 0.851× | 0.945× |
| Road/1920x1080/QP32/1Slices | 20.713 | 20.550 | 13.115 | -0.79% | 0.589 | 1.567× | 1.566× |
| Road/1920x1080/QP32/4Slices | 25.557 | 26.530 | 20.777 | +3.81% | 0.699 | 1.277× | 1.474× |
| PDF/1920x1080/QP20/1Slices | 10.750 | 10.571 | 7.104 | -1.66% | 0.485 | 1.488× | 1.488× |
| PDF/1920x1080/QP20/4Slices | 14.694 | 12.277 | 10.557 | -16.45% | 0.699 | 1.163× | 1.406× |
| Road/1280x720/QP20/1Slices | 12.349 | 12.477 | 8.294 | +1.03% | 0.394 | 1.504× | 1.504× |
| Road/1280x720/QP20/4Slices | 13.436 | 12.744 | 9.682 | -5.15% | 0.818 | 1.316× | 1.500× |

This incremental comparison has 1 significant reductions, 1 significant regressions and 8 inconclusive cases. The descriptive median wall-time geomean changes by -5.79%; CPU geomean changes by -6.82%. Inconclusive results do not establish equality or improvement. [Three-way statistics](run/codec/benchstat.txt), [current/native statistics](run/codec/native-benchstat.txt), [raw medians and CPU metrics](run/codec/summary.json).

Native is OpenH264 2.6.0 commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, built with GCC 13.3 `-O3` and assembly/AVX2. Its actual executable and statically linked OpenH264 archive are fingerprinted; the system C/C++ runtime remains dynamically linked. Both implementations use camera real-time, low complexity, QP20 unless QP32 is labeled, one reference/spatial/temporal layer, Baseline/CAVLC, fixed slices, full-range BT.709 and a 60 FPS codec clock. Rate control, deblocking, periodic IDR, frame dropping, scene/background detection, AQ, denoising, LTR and load balancing are disabled. [Matching settings](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching), [exact binaries and inputs](run/provenance.json), [binary hashes](run/binary-hashes.json).

Codec timing includes encoding and an owning payload. RGB conversion, initialization, loading, capture, framing, network/ACK waits and presentation are excluded. Absolute times across separate rounds are not paired comparisons.

## Full EncodeDamage comparison

Six rotating triples compare the original commit `641af9e6216055b6a9d918bdcba4ea7d40020750`, frozen round eleven and current source. Each uses 16 warmup and 60 measured frames, full-screen damage, RGB conversion, damage preparation, encoding and owning payload construction. This harness uses FrameRate=30; codec-only timing uses 60 FPS. Capture, socket writes, ACK waits and presentation are excluded.

| 1080p case | Original ms | Round eleven ms | Current ms | Change vs eleven | p-value | Change vs original | p-value |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| ShortClip/1080p/1Slices | 48.559 | 43.915 | 43.174 | -1.69% | 1.000 | -11.09% | 0.240 |
| ShortClip/1080p/4Slices | 18.766 | 16.835 | 17.893 | +6.28% | 0.485 | -4.65% | 0.589 |
| Road/1080p/1Slices | 50.672 | 46.105 | 45.280 | -1.79% | 0.485 | -10.64% | 0.132 |
| Road/1080p/4Slices | 20.911 | 19.034 | 18.799 | -1.23% | 0.937 | -10.10% | 0.240 |
| Dense/1080p/1Slices | 103.344 | 101.247 | 97.239 | -3.96% | 0.180 | -5.91% | 0.041 |
| Dense/1080p/4Slices | 35.518 | 33.568 | 34.035 | +1.39% | 0.589 | -4.18% | 0.485 |

Against round eleven there are 0 significant reductions, 0 significant regressions and 6 inconclusive cases (descriptive geomean -0.22%). Against the original there are 1 significant reductions, 0 significant regressions and 5 inconclusive cases (descriptive geomean -7.81%). [Incremental statistics](run/full/incremental-benchstat.txt), [cumulative statistics](run/full/cumulative-benchstat.txt), [three-way metrics](run/full/summary.json). These timings do not measure the live RDP “Encoding average”.

## Correctness, held candidates and provenance

All ten current streams match saved original-Go and native streams byte for byte. FFmpeg decoded all 960 frames without errors; ffprobe independently counted 96 frames per case. Capture and decoding ran outside timing. [Stream hashes and counts](run/parity/parity.json), [decoder evidence](run/parity/parity.txt).

Full H264/AVC package tests, four-package race checks, AVX2-disabled and optional-features-disabled tests, four Linux ARM64 test-binary cross-builds, formatting and gopls on this round's changed Go files passed on the isolated selected snapshot. Cross-builds establish build compatibility, not ARM64 runtime performance. Nonverbose package logs do not prove that every optional external-fixture test ran. [Validation commands](run/validation.json), [gopls file scope](run/gopls-scope.json). Directional tests cover scalar/random/extreme parity, exact span guards, aliases, signed/overlapping strides, malformed-input panic order, custom callback behavior, complete I4 state and zero allocations.

The initial register-state **CAVLC emitter remains held**. Six paired complete-caller/public measurements include dense and mixed gains, but significant sparse macroblock, short chroma and extracted-zero regressions reject it for this milestone. Native 447-suite correctness, race and CPU-off checks passed after correcting an unused test import; these are correctness comparisons, not native timing runs. The initial failed build, correction, frozen/working source fingerprints and all six timing pairs remain archived. The two verbose test logs are gzip-compressed with verified decompressed hashes. [Decision](cavlc-emitter-review/decision.txt), [complete statistics](cavlc-emitter-review/benchstat.txt), [source provenance](cavlc-emitter-review/provenance.json), [compression metadata](compression.json). A separately tuned experiment is not part of this selected source or these timings.

The earlier allocation-isolation experiment also remains held, and its existing archive is preserved byte for byte. [Scratch evidence](scratch-isolation-review/README.md). No fresh round-twelve CPU profiles or call-count diagnostics were collected by this runner; earlier profile findings remain scoped to their respective binaries.

The selected source has 573 H264 files and exactly eight changed/added files against round eleven. [Delta manifest](run/changes-vs-round11.json), [current fingerprint](run/current-sources.sha256), [round-eleven fingerprint](run/round11-sources.sha256), [original measurement fingerprint](run/original-sources.sha256). The original measurement tree contains five benchmark-only additions while its 427 original tracked H264 files remain unchanged; [prior baseline audit](run/round11-baseline-audit.json) records that distinction. The round-eleven binary's prior formatting-only rebuild equivalence remains in [its audit](run/round11-whitespace-cleanup.json).

The [cumulative patch](encoder-change.patch) replays on the pristine original Git tree to exactly reproduce all 573 measured H264 files. Post-validation merge verifies the main H264 tree equals the measured snapshot and leaves the Git index untouched. [Merge record](round12-main-merged.json), [archive manifest](archive-manifest.json), [artifact hashes](archive-files.sha256). Binaries, encoded streams and duplicate source snapshots are omitted from the repository archive, with [payload hashes](omitted-payloads.json). The [runner](measure_round12.py), [archiver](save_round12.py), [report helper](write_round12_report.py) and raw logs preserve the procedure. Restore archived harness `.go.txt`/`.cpp.txt` files to their original names before rebuilding overlays; local fixture/native paths require adaptation on another machine.

The final decision-aware [archive helper](save_round12_decision.py) and [report helper](write_round12_decision_report.py) preserve the frozen first-pass helpers and add the confirmation without overwriting prior evidence.
