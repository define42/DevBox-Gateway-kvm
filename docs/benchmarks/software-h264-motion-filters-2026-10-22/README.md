# H.264 motion-filter improvements, 2026-10-22

The refinement filters now handle their actual extra-row/column shapes in SIMD. The measured horizontal 5/9/17-wide cases take **37–45% less time**, vertical cases **39–56% less**, and center cases **34–46% less**. All ten complete test streams remain byte-identical to both the previous Go implementation and native OpenH264.

## Changes and bounds

- **Horizontal:** dedicated AVX2 paths for widths 5, 9 and 17 process two rows together. The ninth and seventeenth outputs use SIMD instead of a scalar tail. Exact 10/14/22-byte input loads cover only the required horizontal halo; odd heights require no padded row.
- **Vertical:** five adjacent-row pairs stay in registers. Advancing an output row loads one new source row, instead of reloading six. Narrow vectors load/store exactly their width; wide tails overlap preceding, identical outputs. The same reuse applies to the signed first pass of the center filter.
- **Center:** widths 5, 9 and 17 finish with an overlapping vector instead of a scalar tail. Arithmetic widens signed intermediates before filtering and clips only after the final rounding step.

Public wrappers retain CPU-feature checks, visible-length validation, negative-stride fallbacks and the previous source/destination alias behavior. Vertical column-group processing requires disjoint destination rows; shorter destination strides retain the previous ordering. Center filtering uses private scratch storage. Search decisions, quantizers and bitstream settings are unchanged.

## Component measurements

All values are median nanoseconds per complete checked filter call. Six before/after sample pairs ran serially in alternating order, with a 200 ms benchmark duration per case, on an Intel Core i7-7700HQ using Go 1.27.3 and `GOMAXPROCS=4`. The baseline is the frozen, already-optimized working tree immediately before this round, not a clean `HEAD` checkout.

| Filter / dimensions | Before ns | After ns | Time change | p, n=6 |
| --- | ---: | ---: | ---: | ---: |
| Horizontal / 5×4 | 39.54 | 21.84 | −44.75% | 0.002 |
| Horizontal / 9×8 | 62.52 | 37.39 | −40.19% | 0.002 |
| Horizontal / 17×16 | 112.55 | 70.96 | −36.95% | 0.002 |
| Horizontal / 16×16 | 74.45 | 75.86 | Inconclusive | 0.180 |
| Vertical / 4×5 | 38.39 | 23.33 | −39.24% | 0.002 |
| Vertical / 8×9 | 62.53 | 30.55 | −51.14% | 0.002 |
| Vertical / 16×17 | 121.80 | 54.01 | −55.66% | 0.002 |
| Vertical / 16×16 | 109.10 | 53.05 | −51.38% | 0.002 |
| Center / 5×5 | 112.65 | 74.57 | −33.80% | 0.002 |
| Center / 9×9 | 196.8 | 107.9 | −45.19% | 0.002 |
| Center / 17×17 | 429.4 | 232.1 | −45.96% | 0.002 |
| Center / 16×16 | 321.6 | 201.3 | −37.40% | 0.002 |

All eleven changed cases improved significantly. The unchanged 16×16 horizontal path shows no statistically significant difference. Every case remains at zero bytes and zero allocations per operation. The table describes component latency, not complete-frame or native-relative speed.

Raw evidence: [before samples](component-before.txt), [after samples](component-after.txt), [benchstat comparison](component-benchstat.txt).

## Complete-frame measurements

Six alternating before/after pairs used 16 warmup frames and 60 measured frames per case. Each call includes RGB conversion, damage preparation, encoding and the owning output buffer. Capture, RDP framing, socket writes, ACK waits and presentation are excluded.

| Full 1080p EncodeDamage | Before ms | After ms | Time change | p, n=6 |
| --- | ---: | ---: | ---: | ---: |
| ShortClip / 1Slices | 36.68 | 35.64 | -2.84% | 0.002 |
| ShortClip / 4Slices | 12.85 | 12.54 | Inconclusive | 0.310 |
| Road / 1Slices | 37.94 | 36.23 | -4.51% | 0.002 |
| Road / 4Slices | 14.24 | 14.39 | Inconclusive | 0.937 |
| Dense / 1Slices | 81.50 | 78.75 | -3.37% | 0.002 |
| Dense / 4Slices | 23.73 | 23.22 | Inconclusive | 0.310 |

All three single-slice gains are statistically significant. The four-slice results are inconclusive, including the road median's apparent 1.1% increase; the run establishes neither improvement nor regression there. The separately measured one-slice road encoding stage fell from 36.45 to 34.71 ms (−4.78%, p=.002). Conversion time and encoded bytes per frame showed no significant changes.

[Full statistics](full-benchstat.txt), [before samples](full-before.txt), [after samples](full-after.txt), [machine-readable medians](full-summary.json). These timings come from this round's paired run on an active workstation without CPU affinity or host isolation; do not compare their absolute values directly with earlier reports or treat them as live RDP dashboard averages.


## Output and validation

All ten cases produced identical complete before/after/native streams: ShortClip 1080p QP20, Road 1080p QP20/QP32, PDF 1080p QP20, and Road 720p QP20, each with one and four slices. Each stream contains 96 decoded frames, for **960 successfully decoded final frames**. Full stream sizes and SHA-256 values are recorded in [parity.txt](parity.txt). The corpus and matching codec settings are described in the [preceding benchmark report](../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching).

All final checks passed: H.264 and AVC integration tests; race checks for common kernels, encoder and AVC; AVX2-disabled and all-optional-features-disabled runs; Linux ARM64 test cross-builds; and gopls diagnostics. Commands and exit status are recorded in [validation.json](validation.json). ARM64 cross-builds do not constitute runtime or speed testing. Focused coverage exercises scalar parity, heights through 31, every binary six-tap pattern, full signed center-pass inputs, exact source/destination lengths, guard pages, clipping, aliases and overlapping or negative strides.

These runs do not measure live RDP capture, socket writes, ACK waits or presentation. They do not establish a changed native OpenH264 performance ratio or speed on other CPUs.


## Reproduction

Measurement workspace: `/tmp/h264-motion-shapes-20261022`. The [source delta](filter-change.patch) contains this round's changes only; reverse it in a temporary copy of the final tree to reconstruct the production baseline. Preserve the current `mc_refine_bench_test.go` in both trees when building component benchmark binaries. [Before source fingerprints](before-sources.sha256), [final production fingerprints](after-production.sha256), [changed files](changed-files.txt), and [build information](build-info.txt) record the scope.

The retained Python scripts use their own directory as the measurement workspace. Copy them into that workspace and adjust paths as necessary. [measure_components.py](measure_components.py) consumes `before-common.test` and `after-common.test`, built with `CGO_ENABLED=0 go test -c ./pkg/h264/internal/common` from the respective trees. It runs the same checked-filter benchmark in alternating order, then invokes benchstat.

For complete-frame and stream tests, copy [compare_test.go.txt](compare_test.go.txt) and [fullscreen_test.go.txt](fullscreen_test.go.txt) to `.go` files in the measurement workspace, then use the [before](before-overlay.json) and [after](after-overlay.json) overlays with `CGO_ENABLED=0 go test -overlay=... -c ./internal/rdp/avc`. The overlays map virtual test files without changing production source. [measure.py](measure.py) `--preflight` checks full streams against the matching native executable; [cases.json](cases.json) names the preloaded I420 fixtures. The native build and source fixtures are retained from the preceding comparison.

[measure_full.py](measure_full.py) runs the full `EncodeDamage` benchmark. It expects the earlier short-clip RGBA file and `road1080.rgba` in the measurement workspace; the latter was linked to `/tmp/h264-native-parity-20261021/road1080.rgba`. Dense frames are generated by the harness. [validate.py](validate.py) records the separate correctness and cross-build commands. Builds, validation, decoding and timing were serialized.
