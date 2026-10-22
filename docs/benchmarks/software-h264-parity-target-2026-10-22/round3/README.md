# Native parity work: first validated batch, 2026-10-22

Performance parity has **not** been reached. This batch preserves byte-identical native output and reduces several measured hot paths. In the matched codec suite, Go still takes **1.65–1.76× native time with one slice** and **1.14–1.22× with four slices**. These are wall-time ratios on the tested workstation, not production dashboard measurements.

This directory records a frozen milestone. Later experiments are measured separately against this source snapshot.

## Retained changes

- Four-block AVX2 inverse transform, replacing two sequential paired transforms. Horizontal int16 wrapping and widened vertical arithmetic remain intact; coefficient/output overlap retains the previous load/write order.
- Quarter-pixel refinement reuses its complete input/output span validation before invoking typed-pointer averaged-SATD kernels. Candidate order, tie behavior and prediction buffers remain unchanged.
- Narrow single-candidate SAD packs rows into both SSE2 lanes. Eight-wide four-candidate SAD also packs two rows and reuses reference pairs.
- Exact-width 8×16 and 16×16 copies accelerate reconstruction and integer motion. Smaller copies retain scalar paths. Integer chroma motion avoids a redundant call through the scalar interpolation dispatcher.

The implementation preserves visible-length bounds checks, unusual-stride/alias fallbacks and CPU feature dispatch. No encoder settings, motion-search rules, quantization decisions, quality targets or frame-dropping behavior changed.

## Component measurements

Six alternating before/after pairs, pinned to logical CPU 2, `GOMAXPROCS=4`, Go 1.27.3, `CGO_ENABLED=0`, Intel i7-7700HQ. Initial components used 200 ms per case; the SAD selection run used 150 ms, and final copy/four-candidate checks used 200 ms.

| Retained path | Before ns | After ns | Change |
| --- | ---: | ---: | ---: |
| Four-block IDCT, checked | 45.61 | 32.79 | −28.09% |
| Quarter refinement 16×16, no winner | 216.6 | 195.9 | −9.58% |
| Quarter refinement 8×8, winner | 169.7 | 145.8 | −14.08% |
| Single SAD 8×16, checked | 14.62 | 12.40 | −15.18% |
| Single SAD 8×8, checked | 10.85 | 10.21 | −5.90% |
| Four-candidate SAD 8×16, stride 32 | 34.32 | 31.57 | −8.00% |
| Reconstruction copy 8×16 | 29.12 | 18.74 | −35.65% |
| Reconstruction copy 16×16 | 28.11 | 18.84 | −33.00% |
| Integer luma AVX2 copy 16×16 | 32.74 | 25.00 | −23.65% |

All listed changes have p=0.002 at n=6. Some other cases remain inconclusive; these selected rows do not imply a corresponding percentage improvement in whole-frame encoding. No listed component allocates. Smaller-copy and restored 16-wide four-candidate controls show no significant regression in the final run.

[IDCT/refinement results](round1-components-benchstat.txt), [narrow SAD and rejected-candidate results](round2-components-benchstat.txt), [final copies/four-candidate results](final-components-benchstat.txt). Raw samples are retained beside each report.

## Complete codec measurements

Each row is the median of six per-run averages, with 16 warmup and 80 measured frames. Before/after/native run serially in all six rotating orders. One-slice runs use CPU 2; four-slice runs use CPUs 0–3, one logical CPU from each physical core. Other workstation processes remain running. This affinity differs from earlier reports, so absolute timings must not be compared across reports as if the conditions were identical.

| Case | Before ms | After ms | Native ms | Go/native |
| --- | ---: | ---: | ---: | ---: |
| ShortClip/1920x1080/QP20/1Slices | 33.326 | 32.704 | 19.255 | 1.698× |
| ShortClip/1920x1080/QP20/4Slices | 10.235 | 10.061 | 8.379 | 1.201× |
| Road/1920x1080/QP20/1Slices | 32.809 | 32.086 | 19.181 | 1.673× |
| Road/1920x1080/QP20/4Slices | 11.407 | 11.204 | 9.510 | 1.178× |
| Road/1920x1080/QP32/1Slices | 24.216 | 23.520 | 13.341 | 1.763× |
| Road/1920x1080/QP32/4Slices | 8.891 | 8.748 | 7.278 | 1.202× |
| PDF/1920x1080/QP20/1Slices | 12.426 | 11.818 | 7.164 | 1.650× |
| PDF/1920x1080/QP20/4Slices | 5.002 | 4.797 | 3.941 | 1.217× |
| Road/1280x720/QP20/1Slices | 14.356 | 13.961 | 8.375 | 1.667× |
| Road/1280x720/QP20/4Slices | 5.235 | 4.997 | 4.381 | 1.140× |

Significant whole-frame reductions: ShortClip/four slices −1.70% (p=0.009), Road 1080p QP20/one slice −2.21% (p=0.009), Road QP32/one slice −2.88% (p=0.004), PDF/one slice −4.89% (p=0.002), PDF/four slices −4.09% (p=0.015), and Road 720p/four slices −4.56% (p=0.041). The remaining before/after wall-time differences are inconclusive. Native remains faster; no parity claim is supported.

Four-slice process CPU time is also materially higher in Go: for Road 1080p QP20, 32.80 ms versus native 20.08 ms per frame. The smaller four-slice wall-time ratio does not mean the CPU work is nearly equal.

[Full statistics](codec-benchstat.txt), [exact medians](codec-summary.json), [baseline](codec-before.txt), [candidate](codec-after.txt), [native](codec-native.txt).

Codec timing includes encoding and concatenation into an owning payload. It excludes RGB conversion, capture, RDP framing, socket writes, ACK waits, initialization and input-file loading. Full `EncodeDamage` and live “Encoding average” were not measured for this milestone.

## Rejected experiments

- CAVLC state passed by scalar arguments prevented fast-path inlining. Explicit production fast paths with a by-value slow flush still regressed dense residual writing by roughly 6–20%. Both were removed; the original writer remains.
- Native-style 16-wide four-candidate SAD row reuse regressed 16×16 at stride 32 by about 12%, with other 16-wide cases neutral. Its assembly was restored; the faster eight-wide change remains.
- Eight-row SIMD copies lost to the existing scalar implementation after checked-call overhead. They were removed. Only the larger measured winners remain.
- Experimental PGO, trained from Road one/four slices and PDF, produced no Road gain and only about 1% on ShortClip/PDF. No build configuration or profile was added. [PGO statistics](pgo-check-benchstat.txt).

## Matching and correctness

Baseline commit: `641af9e6216055b6a9d918bdcba4ea7d40020750`. Native is the local OpenH264 2.6.0 reference, commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, GCC 13.3 `-O3`, assembly and AVX2 enabled. This is the port's matched reference version, not a claim about the latest upstream release.

Both use camera real-time, low complexity, 60 FPS codec clock, QP20 unless labeled QP32, one reference/spatial/temporal layer, fixed one/four slices, Baseline/CAVLC and full-range BT.709 metadata. Rate control, deblocking, periodic IDR, frame skipping, scene/background detection, adaptive quantization, denoising, LTR and load balancing are disabled. Fixtures are the established ShortClip, Road 1080p/720p, and PDF sequences; [input provenance](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching).

All ten complete before/after/native streams have matching SHA-256 hashes. FFmpeg decoded all **960 final frames** with `-xerror` and no diagnostics; frame counts were checked independently. [Hashes and frame counts](parity.txt).

The H.264 and AVC tests, race checks, AVX2-disabled/all-optional-feature-disabled tests, Linux ARM64 cross-builds and gopls checks passed. [Validation commands and exit status](validation.json). Cross-builds do not establish ARM64 runtime performance. New tests cover extremes, independent arithmetic references, every-pixel impulses, aliases, signed/overlapping strides, visible-length rejection and per-row inaccessible guard pages.

## Reproduction and remaining gap

The workspace is `/tmp/h264-parity-target-20261022`. [Build metadata](round3-build-info.json), [baseline fingerprints](before-sources.sha256), [milestone fingerprints](round3-sources.sha256), and [complete milestone patch](encoder-change.patch) retain the source boundary. The patch includes tests and new files. Start from the baseline commit and apply it in a temporary checkout; do not reverse it over later work.

The scripts retain workspace-specific paths. Place the `.go.txt` overlay harnesses at their `.go` paths, adapt the overlays and `cases.json`, and provide the matched `native_bench` from the [earlier reproducible comparison](../../software-h264-native-parity-2026-10-22/README.md#reproduction). `validate_measure_round3.py` builds the frozen candidate, measures final components, checks streams/decoding, then runs `measure.py` for native timing. `validate.py` records safety/architecture checks. All builds, tests, decoders, profiles and timing processes were serialized.

The initial profiles still attribute substantial CPU to quantization/scanning, inter-luma bookkeeping, entropy coding and motion search. [Road flat/cumulative](before-road1-top.txt), [cumulative](before-road1-cum.txt), [PDF](before-pdf1-top.txt), [four-slice Road](before-road4-top.txt), and [native flat sample](native-road1-flat.txt) retain the diagnostic evidence. Profile percentages from different runtimes are not directly comparable without normalizing by CPU time per frame. The next isolated experiments address wider quantization, scalar inter-luma bookkeeping and checked SAD wrapper overhead.
