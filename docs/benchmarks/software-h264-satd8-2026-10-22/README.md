# H.264 eight-row SATD, 2026-10-22

Eight-wide AVX2 SATD now uses both 128-bit lanes to process eight rows per pass. Checked 8×8 calls take **28% less time**, and checked 8×16 calls take **36% less time**, in the final paired measurements. Complete encoded streams remain identical to the previous Go implementation and native OpenH264.

## Implementation

Each lane holds a separate four-row band: rows 0/4, 1/5, 2/6 and 3/7 are loaded together. Sums and differences occupy both halves of each lane, eliminating the unused word positions in the previous layout. One pass computes four independent 4×4 Hadamard costs; this retains the original sum-of-4×4 metric.

Both plain and fused averaged SATD use the new path. Averaging retains `(a+b+1)>>1` rounding. An 8×12 block processes eight rows, restores the original coefficient layout, and finishes its four-row remainder. The 8×4 loader remains available. Repeated 16-wide batches now branch directly to the corresponding loader instead of repeating width dispatch.

All new loads read exactly eight bytes. Visible slice-length checks, independent signed/zero/overlapping strides, read-only input aliases and CPU dispatch remain intact. Pair residuals are bounded by 510, vertical word intermediates by 2040, and the sum before widening by 4080; accumulation retains 32-bit lanes. Search decisions, quantization and encoder settings are unchanged.

## Component results

Median nanoseconds per call on Intel Core i7-7700HQ, Linux/amd64, Go 1.27.3, `CGO_ENABLED=0`, `GOAMD64=v1`, `GOMAXPROCS=4`. Six alternating before/after pairs used 500 ms per benchmark case, with both Go and native processes pinned to logical CPU 2. Both trees contain identical test and benchmark sources. The baseline is the frozen working tree immediately before this change, including the preceding motion-filter improvements.

| Call | Before ns | After ns | Time change | p, n=6 |
| --- | ---: | ---: | ---: | ---: |
| Plain 8×8, checked | 24.20 | 17.38 | -28.19% | 0.002 |
| Plain 8×16, checked | 38.22 | 24.37 | -36.25% | 0.002 |
| Plain 8×8, raw | 18.16 | 11.95 | -34.22% | 0.002 |
| Plain 8×16, raw | 32.59 | 18.93 | -41.91% | 0.002 |
| Averaged 8×8, checked | 30.02 | 22.79 | -24.10% | 0.002 |
| Averaged 8×12, checked | 37.48 | 30.52 | -18.58% | 0.002 |
| Averaged 8×16, checked | 45.35 | 31.31 | -30.95% | 0.002 |

All listed improvements are statistically significant. The unchanged four-row averaged case and all 16-wide controls show no significant difference. Every component case remains at zero bytes and allocations per call. [Before samples](component-before.txt), [after samples](component-after.txt), [benchstat](component-benchstat.txt) and [exact medians](component-summary.json) retain all cases.

The final implementation was first measured without affinity while a live `devbox-kvm` workload was active; that run had substantially wider variance. It is retained as [unpinned statistics](unpinned-component-benchstat.txt), with [before](unpinned-component-before.txt), [after](unpinned-component-after.txt) and [native](unpinned-component-native.txt) samples. The longer pinned run reduced migration noise. The workstation was not isolated, and other processes were left running.

## Native comparison

The same input bytes, offsets and strides were used for the native helper, with 100,000 warmup and 20,000,000 measured calls per shape in each of six samples. The retained native OpenH264 release library is built from commit `4c8c5cdc0299407af14751a4c37449ff3d367745` using GCC 13.3 and AVX2 assembly.

| Shape | Go checked ns | Go raw ns | Native ns |
| --- | ---: | ---: | ---: |
| 8×8 | 17.38 | 11.95 | 9.04 |
| 8×16 | 24.37 | 18.93 | 15.50 |

Native remains faster in this microbenchmark. Go raw calls use a generic assembly entry and the Go ABI; native uses unchecked, shape-specific entries and the native ABI. Checked Go calls additionally validate buffer spans. These timings include different call/setup and benchmark-loop costs and do not isolate SIMD instruction cost. There is no native averaged-SATD comparison in this helper. [Native samples](component-native.txt), [helper source](native_satd8.cpp.txt).

## Complete-frame results

Six alternating before/after pairs used 16 warmup and 60 measured frames per case, at 1080p with one and four slices. `EncodeDamage` includes RGB conversion, damage preparation, encoding and the owning output buffer. Capture, RDP framing, network writes, ACK waits and presentation are excluded. These runs used all available CPUs with `GOMAXPROCS=4`, without affinity or host isolation.

| Full EncodeDamage | Before ms | After ms | Time change | p, n=6 |
| --- | ---: | ---: | ---: | ---: |
| ShortClip / 1Slices | 40.607 | 40.681 | Inconclusive | 0.699 |
| ShortClip / 4Slices | 16.372 | 15.898 | Inconclusive | 0.394 |
| Road / 1Slices | 41.509 | 41.308 | Inconclusive | 0.818 |
| Road / 4Slices | 16.814 | 16.742 | Inconclusive | 0.589 |
| Dense / 1Slices | 87.306 | 87.264 | Inconclusive | 0.818 |
| Dense / 4Slices | 29.499 | 30.453 | Inconclusive | 0.240 |

None of the six complete-frame cases shows a statistically significant change. This run establishes the SATD component improvement, but it does not establish a reduction in complete-frame time or the live “Encoding average”. The apparent median changes in both directions remain inconclusive.

[Full statistics](full-benchstat.txt), [before samples](full-before.txt), [after samples](full-after.txt) and [exact medians](full-summary.json). These are paired workstation measurements, not live RDP dashboard averages or a new overall native-encoder speed ratio. The active live workload limits conclusions about small frame-time changes.

## Correctness

All ten complete before/after/native streams have matching sizes and SHA-256 hashes: ShortClip 1080p QP20, Road 1080p QP20/QP32, PDF 1080p QP20, and Road 720p QP20, each with one and four slices. FFmpeg decoded all **960 final frames** without errors. [Stream hashes and frame counts](parity.txt); [fixture/configuration details](../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching).

All H.264 and AVC package tests, race checks, AVX2-disabled and all-optional-features-disabled runs, Linux ARM64 test cross-builds, and gopls diagnostics passed. [Commands and status](validation.json). Cross-builds do not establish ARM64 runtime behavior or performance.

New tests use an independent two-dimensional matrix reference, random/extreme blocks, every-pixel impulses, heights 4/8/12/16, mixed signed/zero/overlapping strides, aliased inputs and slices whose capacity exceeds their visible length. Linux guard tests place every eight-byte row against its own inaccessible page and mark source pages read-only. Existing native-corpus and scalar parity tests also pass.

## Reproduction

Measurement workspace: `/tmp/h264-satd8-20261022`. The [production patch](satd-change.patch) contains only this round's assembly delta; reverse it in a temporary copy to reconstruct the baseline. Keep the same three `sample_satd8*.go` test files in both trees. [Before fingerprints](before-sources.sha256), [final fingerprints](after-sources.sha256), [changed files](changed-files.txt) and [build metadata](build-info.txt) record the source and toolchain scope.

The retained Python scripts resolve their own directory as the measurement workspace. Copy them there and adjust machine-specific paths/CPU affinity as needed. Build `before-encoder.test` and `after-encoder.test` with `CGO_ENABLED=0 go test -c -o ... ./pkg/h264/internal/encoder` from the respective trees. Copy [native_satd8.cpp.txt](native_satd8.cpp.txt) to `native_satd8.cpp`, then build:

```sh
g++ -O3 -std=c++17 native_satd8.cpp /tmp/openh264-comparison-20261021/native/libopenh264.a -pthread -o native_satd8
python3 measure_components.py
python3 summarize_components.py
```

For stream and frame checks, copy [compare_test.go.txt](compare_test.go.txt) and [fullscreen_test.go.txt](fullscreen_test.go.txt) to `.go` files in the workspace. Use the [before](before-overlay.json) and [after](after-overlay.json) overlays with `CGO_ENABLED=0 go test -overlay=... -c -o ... ./internal/rdp/avc`, producing the respective `before-avc.test` and `after-avc.test`. The overlays add test harnesses without changing production files.

[measure.py](measure.py) `--preflight` uses [cases.json](cases.json) and the matching retained `native_bench` executable to verify streams and decoding. [measure_full.py](measure_full.py) expects the earlier short-clip RGBA file and workspace `road1080.rgba` linked to `/tmp/h264-native-parity-20261021/road1080.rgba`; Dense frames are generated by the harness. Run benchstat on `full/before.txt` and `full/after.txt`, save `full-benchstat.txt`, then run [summarize_full.py](summarize_full.py). [validate.py](validate.py) records correctness and cross-build checks. All builds, tests, decoding and measurements were serialized.
