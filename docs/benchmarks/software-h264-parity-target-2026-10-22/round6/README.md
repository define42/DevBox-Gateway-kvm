# Software H.264 performance parity: round six

**Native speed parity remains unmet.** On the matched 1080p fixtures, the current Go codec takes **1.58–1.67× native wall time with one slice** and **1.13–1.19× with four slices**. The linked results also retain measured CPU time; parallel wall time alone does not establish equal CPU efficiency.

This milestone adds three changes to the fourteen retained groups in [round five](../round5/README.md):

- Paired AVX2 multiply-adds for center filters at widths 5, 9 and 17. Full signed-int16 inputs, exact source halos, rounding and output-store order are preserved. Checked production shapes improve **11–20%**; raw kernels improve 25–33%. One unchanged 16-wide checked control measured 3.02% slower, while its raw kernel was neutral. The complete-frame comparison below is the integration check. [Component statistics](center-pairs-next-benchstat.txt).
- Private single-block DCT/IDCT calls pass three typed pointers and two strides instead of three slice headers and two strides: argument area **88→40 bytes**, with unchanged public validation, IDCT alias fallback and kernel bodies. Checked calls improve **7–12%**. Complete sixteen-block I4 reconstruction improves 4–10% in five significant cases; seven cases are inconclusive. [Component and complete-chain statistics](transform-abi-next-benchstat.txt).
- Fused SSE2 chroma DC Hadamard transform, quantization, copying and nonzero counting. The actual chroma encode call uses a private checked dispatcher; the exported scalar and skip routines remain unchanged. Negative parameters and short slices retain the original scalar behavior. Checked calls improve about **44%** including the input reset; the complete two-plane chroma coefficient path improves **5–12% in thirteen significant cases**, with three inconclusive cases. One unchanged scalar control measured 1.64% slower. [Component and complete-chain statistics](hadamard-next-benchstat.txt).

Component results use six alternating pairs, CPU 2, 250 ms per case. These percentages are not full-frame gains.

## Codec comparison against the previous milestone and native

**The before column here is frozen round five, not the original user commit.** It isolates the three newest improvements. Original-commit comparisons for the first fourteen groups are in [round five](../round5/codec-benchstat.txt); the full EncodeDamage comparison below still uses the original commit.

Medians of six per-run averages, 16 warmup and 80 measured frames, rotating previous/Go/native order. One slice uses CPU 2; four slices use CPUs 0–3. Intel i7-7700HQ, Go 1.27.3, CGO disabled, GOMAXPROCS=4. Other workstation processes remain running.

| Case | Round five ms | Current ms | Native ms | Current/native |
| --- | ---: | ---: | ---: | ---: |
| ShortClip/1920x1080/QP20/1Slices | 30.857 | 30.563 | 19.188 | 1.593× |
| ShortClip/1920x1080/QP20/4Slices | 9.683 | 9.684 | 8.577 | 1.129× |
| Road/1920x1080/QP20/1Slices | 30.948 | 30.504 | 19.114 | 1.596× |
| Road/1920x1080/QP20/4Slices | 10.991 | 10.953 | 9.524 | 1.150× |
| Road/1920x1080/QP32/1Slices | 22.256 | 22.280 | 13.370 | 1.666× |
| Road/1920x1080/QP32/4Slices | 8.496 | 8.376 | 7.046 | 1.189× |
| PDF/1920x1080/QP20/1Slices | 11.609 | 11.326 | 7.160 | 1.582× |
| PDF/1920x1080/QP20/4Slices | 4.670 | 4.699 | 3.947 | 1.191× |
| Road/1280x720/QP20/1Slices | 13.389 | 13.355 | 8.312 | 1.607× |
| Road/1280x720/QP20/4Slices | 4.857 | 4.802 | 4.329 | 1.109× |

[Complete statistics](codec-benchstat.txt), [direct native comparison](codec-native-benchstat.txt), [exact medians and CPU time](codec-summary.json). Only Road/QP32/four slices shows a significant incremental wall-time reduction (1.41%, p=0.041) and CPU-time reduction (1.77%, p=0.009); the other nine incremental wall-time comparisons are inconclusive, with no significant regressions. The median wall-time geomean is 0.73% lower. These results do not establish a broad additional whole-frame speedup from this final group. All ten output streams are byte-identical, so the comparison does not trade quality or encoding decisions for speed.

Native is the local matched OpenH264 2.6.0 reference, commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, GCC 13.3 `-O3` with assembly/AVX2. Both use camera real-time, low complexity, QP20 unless labeled QP32, fixed one/four slices, one reference/spatial/temporal layer, Baseline/CAVLC, full-range BT.709 and a 60 FPS codec clock. Rate control, deblocking, periodic IDR, dropping, scene/background detection, AQ, denoising, LTR and load balancing are disabled. [Corpus and matching provenance](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching).

Codec timing includes encoding plus an owning output payload; RGB conversion, initialization, file loading, capture, RDP framing, network/ACK waits and client presentation are excluded. Absolute times from separate rounds are not paired measurements.

## Full EncodeDamage against the original commit

Before is original commit `641af9e6216055b6a9d918bdcba4ea7d40020750`; after includes all seventeen retained groups. Six alternating pairs, 16 warmup and 60 measured frames, matching one/four-slice affinity. This includes RGB conversion, damage preparation, encoding and owning payload construction. The existing full-path harness uses `Options.FrameRate=30`; codec-only uses 60. Capture, network/ACK waits and client presentation are excluded.

| 1080p case | Original ms | Current ms | Median change | p-value |
| --- | ---: | ---: | ---: | ---: |
| ShortClip/1080p/1Slices | 34.928 | 32.145 | -7.97% | 0.002 |
| ShortClip/1080p/4Slices | 10.864 | 10.608 | -2.35% | 0.310 |
| Road/1080p/1Slices | 35.657 | 32.952 | -7.58% | 0.002 |
| Road/1080p/4Slices | 12.476 | 11.845 | -5.06% | 0.002 |
| Dense/1080p/1Slices | 76.673 | 73.238 | -4.48% | 0.002 |
| Dense/1080p/4Slices | 22.713 | 21.496 | -5.36% | 0.002 |

[Full-path statistics](full-benchstat.txt), [exact metrics](full-summary.json). No claim about the live RDP “Encoding average” follows from these local measurements.

## Compiler bounds-check diagnostic

An isolated test executable disables compiler-generated bounds checks only for `pkg/h264/...` using `-B`. It is a diagnostic and is not used by the production build. On six alternating one-slice comparisons (100 measured frames plus 16 warmup), Road improves **5.46%** and PDF **7.29%** (both p=0.002). Their 96-frame diagnostic streams match the normal build byte for byte. [Statistics](bounds-diagnostic-benchstat.txt), [driver](measure_bounds_diagnostic.py), [build command](bounds-diagnostic-build.json), [hash check](bounds-diagnostic-parity.json).

This suggests useful work remains in safely proving bounds once or restructuring hot loops. It does not justify disabling bounds checks, and it explains only part of the much larger native gap. Explicit wrapper checks remain present even in this diagnostic. These 100-frame averages must not be compared directly with the 80-frame codec table above.

## Correctness and remaining work

All ten complete streams match the original Go baseline and native OpenH264 byte for byte. FFmpeg decoded **960 frames** without errors and independent frame counts agreed. [Hashes and decoder evidence](parity.txt).

Package/AVC tests, race checks, AVX2-disabled and optional-CPU-features-disabled tests, Linux ARM64 cross-builds and gopls diagnostics passed. [Commands and status](validation.json). New tests cover full signed arithmetic, QPs, rounding boundaries, aliases, exact visible slices, partial writes on invalid inputs, and guard pages. ARM64 cross-builds establish build compatibility, not ARM64 runtime performance.

[Remaining-gap source review](remaining-gap-review.txt) distinguishes missing SIMD and repeated validation/dispatch work from unmeasured hypotheses. Final [Road flat](round6-road1-top.txt), [Road cumulative](round6-road1-cum.txt), [PDF flat](round6-pdf1-top.txt) and [PDF cumulative](round6-pdf1-cum.txt) profiles retain the current boundary. Road still spends about 14.5% cumulatively in residual writing and 15.7% in fractional refinement; PDF spends about 33% in P-skip processing. These shares include useful work also required by native, and are not estimates of removable overhead. The source review uses the [earlier round-five profiles](../round5/round5-road1-top.txt), which already include CAVLC, intra prediction, chroma and quantization improvements. Smaller kernels alone have not closed the substantial one-slice CPU gap. Exact performance equality has not been demonstrated on these fixtures or on live RDP workloads.

[Complete cumulative patch](encoder-change.patch) applies to the original commit; [source fingerprint](round6-sources.sha256) fixes this milestone. [Measurement driver](measure_round6.py), [codec harness](compare_test.go.txt), [full-path harness](fullscreen_test.go.txt), component drivers and raw samples retain the experiment. The drivers use workspace-specific corpus/native paths that must be adapted for another machine. The raw transform baseline benchmark differs only in its private assembly call signatures; the Hadamard baseline uses the supplied scalar-only test shim.
