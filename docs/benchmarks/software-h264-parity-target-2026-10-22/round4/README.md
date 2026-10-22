# Software H.264 performance parity: round four

The retained changes reduce full 1080p `EncodeDamage` wall time by **4.1–4.7% on the two video fixtures**, including RGB conversion. Native codec performance parity is **not reached**: Go still takes **1.63–1.75× the native time with one slice** at 1080p, and **1.17–1.23× with four slices**. Four-slice CPU work remains about 56–67% higher than native. These are local benchmark results, not live RDP dashboard readings.

This frozen milestone adds three measured changes to [round three](../round3/README.md):

- Two-block AVX2 quantization/scan with shared tables and exact AC/DC shuffles. Checked four-block work improves 14–16%; checked luma work improves 26–28%.
- Simpler inter-luma score/max/mask bookkeeping and fixed nonzero-count stores, retaining callback order and buffer reloads. Nonzero-block cases improve about 5–8%; zero cases are neutral.
- Checked SAD wrappers return a tracked pointer directly after validating the visible span, avoiding an intermediate slice. Most shapes improve 2–13%; 8×16 is neutral.

These component measurements use six alternating pairs, CPU 2, 250 ms per case. [Quantization results](quant-next-benchstat.txt), [inter-luma results](inter-next-benchstat.txt), [SAD wrapper results](sad-next-benchstat.txt). Raw samples accompany each report. Component percentages are not whole-frame percentages.

## Complete codec comparison

Baseline is commit `641af9e6216055b6a9d918bdcba4ea7d40020750`; the after column includes every retained change from rounds three and four. Each value is the median of six per-run averages, 16 warmup and 80 measured frames, rotating baseline/Go/native order. One-slice runs use CPU 2; four-slice runs use CPUs 0–3. Intel i7-7700HQ, Go 1.27.3, CGO disabled, GOMAXPROCS=4. Other workstation processes remain running. Do not compare absolute times from earlier reports with different CPU affinity.

| Case | Before ms | After ms | Native ms | Go/native |
| --- | ---: | ---: | ---: | ---: |
| ShortClip/1920x1080/QP20/1Slices | 33.259 | 31.487 | 19.362 | 1.626× |
| ShortClip/1920x1080/QP20/4Slices | 10.392 | 9.869 | 8.439 | 1.169× |
| Road/1920x1080/QP20/1Slices | 32.900 | 31.569 | 19.051 | 1.657× |
| Road/1920x1080/QP20/4Slices | 11.474 | 11.027 | 9.180 | 1.201× |
| Road/1920x1080/QP32/1Slices | 24.183 | 23.145 | 13.229 | 1.750× |
| Road/1920x1080/QP32/4Slices | 9.090 | 8.700 | 7.080 | 1.229× |
| PDF/1920x1080/QP20/1Slices | 12.331 | 11.669 | 7.167 | 1.628× |
| PDF/1920x1080/QP20/4Slices | 4.967 | 4.859 | 4.169 | 1.165× |
| Road/1280x720/QP20/1Slices | 14.333 | 13.641 | 8.289 | 1.646× |
| Road/1280x720/QP20/4Slices | 5.183 | 4.882 | 4.482 | 1.089× |

[Complete before/after/native statistics](codec-benchstat.txt), [native comparison statistics](codec-native-benchstat.txt), [exact medians](codec-summary.json). Check the reported p-values before attributing individual differences to the code; the native gap is significant on every case.

Both implementations use the local matched OpenH264 2.6.0 reference, commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, native GCC 13.3 `-O3` with assembly/AVX2. Settings remain camera real-time, low complexity, QP20 unless labeled QP32, fixed one/four slices, one reference/spatial/temporal layer, Baseline/CAVLC, full-range BT.709 and 60 FPS codec clock. Rate control, deblocking, periodic IDR, dropping, scene/background detection, AQ, denoising, LTR and load balancing are disabled. See [matching and corpus provenance](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching).

Codec timing includes encoding plus an owning output payload; it excludes RGB conversion, capture, RDP framing, network/ACK waits, initialization and file loading.

## Full `EncodeDamage`

Six alternating before/after pairs, 16 warmup and 60 measured frames, matching one/four-slice affinity. This path includes RGB conversion, damage preparation, encoding and owning payload construction. Its existing harness sets `Options.FrameRate=30`; the codec-only harness above uses 60. It excludes capture, network, ACK credit and client presentation.

| 1080p case | Before ms | After ms | Change | p-value |
| --- | ---: | ---: | ---: | ---: |
| ShortClip/1Slices | 35.074 | 33.418 | -4.72% | 0.002 |
| ShortClip/4Slices | 10.950 | 10.477 | -4.32% | 0.002 |
| Road/1Slices | 35.690 | 34.244 | -4.05% | 0.041 |
| Road/4Slices | 12.588 | 12.047 | -4.30% | 0.015 |
| Dense/1Slices | 76.801 | 75.112 | -2.20% | 0.002 |
| Dense/4Slices | 23.035 | 22.150 | -3.84% | 0.041 |

[Full-path statistics](full-benchstat.txt), [exact metrics including conversion and encode time](full-summary.json). All six wall-time reductions are significant in this run. Conversion was not changed in this milestone; allocation counts remain unchanged. No claim about live “Encoding average” or network throughput follows from these measurements.

## Correctness and reproducibility

All ten complete streams remain byte-identical to the original Go baseline and native encoder. FFmpeg decoded all **960 final frames** without errors, with independent frame counts. [Hashes and decoding evidence](parity.txt).

The H.264/AVC tests, race checks, AVX2-disabled and all-optional-feature-disabled tests, Linux ARM64 cross-builds and gopls diagnostics passed. [Commands and status](validation.json). Quantization coverage includes all signed coefficient values, valid scalar factor domains, exact SSSE3 semantics for arbitrary signed factor bits, aliases and guard pages. Inter-luma tests check all QPs, score boundaries, nonzero state, callback redirection and aliases. Cross-building does not establish ARM64 runtime performance.

[Complete milestone patch](encoder-change.patch) applies to the baseline commit; [source fingerprint](round4-sources.sha256) fixes the code boundary. [Measurement driver](measure_round4.py), [codec harness](compare_test.go.txt), [full harness](fullscreen_test.go.txt), [component driver](measure_next.py) and supporting scripts retain the commands and workspace-specific paths. Supply the established corpus and native harness, adapt paths/overlays, and serialize CPU work. Do not reverse this patch over later improvements.

Fresh [Road flat profile](round4-road1-top.txt), [Road cumulative profile](round4-road1-cum.txt) and [PDF profile](round4-pdf1-top.txt) still show entropy writing, motion costs, intra prediction and skip processing as material costs. The next independent candidates target the CAVLC level-table layout, macroblock metadata layout, SSSE3 chroma filtering and SAD scheduling. None of those candidates is included in this frozen milestone.
