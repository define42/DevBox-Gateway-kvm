# PR #3 remaining H.264 optimizations — 2026-10-24

Four remaining ideas from [PR #3](https://github.com/define42/DevBox-Gateway-kvm/pull/3) were adapted to current `main` and measured against baseline `cc750a847437e50d54d360dd4f2d63dd43df4791`. All four have significant improvements in complete-caller component benchmarks. The combined change has a **6.27% lower geometric mean of full-frame wall time**, but **all ten individual full-frame wall-time comparisons are inconclusive**. The four desktop comparisons are also inconclusive, with a descriptive **3.58% lower geometric mean**. These aggregate percentages do not establish a statistically demonstrated overall speedup on this host.

The extracted work consists of:

- **Full macroblock forward DCT:** transform the four luma quadrants through one AVX2 entry after validating their complete input/output spans. Builtin callback selection, scalar/custom callbacks, and overlapping or unusual buffers retain the existing fallback behavior.
- **Macroblock CAVLC accumulator:** carry the 64-bit writer state across residual blocks while retaining the current packed level table, direct extraction kernels and known-zero shortcut. Overflow preserves the last completed block. Short buffers, unusual bit counts and malformed nonzero-count caches use the original blockwise writer.
- **Whole integer motion search:** validate the complete search window and motion-vector cost range once, then perform initial-point selection and diamond search through pointer kernels. The current two-candidate SAD optimization remains in place. Internal callers replacing search callbacks must clear the documented capability flags.
- **Whole fractional refinement:** validate source/reference spans and scratch layout once, then use pointer kernels for half-pixel filtering and SATD scoring. Current paired half-pixel and four-candidate quarter-pixel kernels remain in use; unsupported layouts, block sizes or callbacks retain the checked path.

The extraction targets the newer implementation. The compared PR head is `f0f690adbfb0737d637b1543c7d52fd4ea3c49b8`, whose original base is `641af9e6216055b6a9d918bdcba4ea7d40020750`. The [provenance](results/provenance.json), [selection record](results/selection.json) and [selected source hashes](results/selected-changes.json) identify the baseline and retained changes. The PR's earlier speedup is not used as evidence for this extraction. No new native OpenH264 performance comparison was run.

Measurements ran on Linux/amd64, an Intel Core i7-7700HQ (4 cores/8 logical CPUs, AVX2), using Go 1.27.3, `CGO_ENABLED=0`, and `GOMAXPROCS=4`. The workstation remained active, with other applications and the gateway running. Agent builds and tests were excluded from timing, but the intervals were wide and host activity or frequency variation could still affect the measurements. Samples were not discarded or repeated to obtain significance.

The [measurement harness](harness/measure.py) ran frozen binaries serially, alternating baseline/candidate order over six repetitions. Component benchmarks used CPU 2 and `200ms` per case. Single-slice full-codec runs used CPU 2; four-slice runs used CPUs 0–3. Each codec sample measured 64 frames after 16 warmup frames. Desktop benchmarks used CPUs 0–3 and `600ms` per case. Binary hashes, exact commands and raw logs are recorded in each result directory's manifest. The [preparation script](harness/prepare.py) builds frozen comparisons and prepares fixtures; the [codec harness](harness/compare_test.go.txt) calls the production codec constructor directly.

The codec uses fixed QP, baseline profile, CAVLC, camera usage and low complexity, with adaptive quantization, deblocking and scene-change detection disabled. Frame rate is set to 60 for both variants. Output allocation/copy is included in codec timing.

The full-frame fixtures are Road at 1080p and 720p, PDF at 1080p, and ShortClip at 1080p, with one or four slices. Road1080 also runs at QP32; other cases use QP20. FFmpeg converts/scales the local OpenH264 resource files to I420 before timing. Road has 54 source frames, PDF has 50, and ShortClip has only five; the codec harness cycles these fixtures during warmup and measurement. Fixture commands, sizes and SHA-256 hashes are in [fixtures.json](results/fixtures.json). Codec wall time covers encoding of prepared I420, while the desktop benchmark includes persistent RGB-to-I420 conversion and damage-aware encoding.

Full-codec medians are shown below in milliseconds per frame. Percentages are computed from the unrounded medians of six samples; p-values are from [benchstat](results/codec/benchstat.txt). Lower time is better. Every wall-time comparison has p > 0.05, so the directional median changes are descriptive.

| Codec case | Baseline ms/frame | Combined ms/frame | Median change | p-value | Result |
| --- | ---: | ---: | ---: | ---: | --- |
| ShortClip, 1920x1080, QP20, 1 slice | 31.1846 | 28.6085 | -8.26% | 0.093 | Inconclusive |
| ShortClip, 1920x1080, QP20, 4 slices | 10.7863 | 9.9099 | -8.13% | 0.589 | Inconclusive |
| Road, 1920x1080, QP20, 1 slice | 33.5309 | 31.1176 | -7.20% | 0.132 | Inconclusive |
| Road, 1920x1080, QP20, 4 slices | 12.1733 | 11.1621 | -8.31% | 0.093 | Inconclusive |
| Road, 1920x1080, QP32, 1 slice | 24.9012 | 24.7519 | -0.60% | 0.485 | Inconclusive |
| Road, 1920x1080, QP32, 4 slices | 9.3516 | 9.2555 | -1.03% | 0.699 | Inconclusive |
| PDF, 1920x1080, QP20, 1 slice | 13.5953 | 11.9115 | -12.39% | 0.132 | Inconclusive |
| PDF, 1920x1080, QP20, 4 slices | 4.8502 | 4.6375 | -4.39% | 0.093 | Inconclusive |
| Road, 1280x720, QP20, 1 slice | 15.2173 | 14.0463 | -7.70% | 0.180 | Inconclusive |
| Road, 1280x720, QP20, 4 slices | 5.7477 | 5.5136 | -4.07% | 0.818 | Inconclusive |

The wall-time geometric means are 13.38 ms and 12.54 ms (−6.27%). Process CPU time has a descriptive −6.23% geometric-mean change; Road1080/QP20/four-slice CPU time is significantly lower by 6.83% (p=0.041). PDF1080/QP20 p95 frame time is significantly lower by 18.80% with one slice and 6.87% with four slices. Other CPU/p95 comparisons are inconclusive. Encoded bytes per frame match in every fixed-frame codec case. These secondary metrics are preserved in the full [codec table](results/codec/benchstat.txt); they do not change the inconclusive wall-time finding.

The desktop benchmark alternates two deterministic 1080p views, either scrolling the content pane by eight pixels or changing a 64×32 widget. The following medians include conversion and encoding, with p-values from the [desktop table](results/desktop/benchstat.txt).

| Desktop case | Baseline ms/frame | Combined ms/frame | Median change | p-value | Result |
| --- | ---: | ---: | ---: | ---: | --- |
| Scroll, 1 slice | 9.4234 | 8.1268 | -13.76% | 0.180 | Inconclusive |
| Scroll, 4 slices | 4.4081 | 3.9906 | -9.47% | 0.394 | Inconclusive |
| Widget, 1 slice | 0.3813 | 0.4223 | +10.77% | 0.485 | Inconclusive |
| Widget, 4 slices | 0.4293 | 0.4290 | -0.06% | 0.699 | Inconclusive |

The desktop geometric means are 1.615 ms and 1.557 ms (−3.58%). The Widget/one-slice median is higher by 10.77%, but this comparison is inconclusive; it is included rather than hidden by the aggregate. Desktop allocation counts are unchanged. Unlike fixed-frame codec measurements, time-budgeted desktop runs can execute different numbers of frames, so their small average output-byte differences are not a bitstream parity test.

Component results below compare each candidate against the same production baseline using identical complete-caller fixtures. “Improved” and “regressed” follow benchstat's significant time comparisons; `~` is inconclusive. Reduction ranges cover significant improvements only.

| Component group | Cases | Improved | Regressed | Inconclusive | Significant time reduction |
| --- | ---: | ---: | ---: | ---: | ---: |
| DCT builtin, strides 16/64/2048 | 3 | 3 | 0 | 0 | 35.90–40.89% |
| DCT scalar/wrapped callback controls | 6 | 0 | 0 | 6 | — |
| CAVLC macroblocks, final fixed candidate | 8 | 4 | 0 | 4 | 12.90–24.73% |
| Fractional search: 16×16, 16×8, 8×16, 8×8 | 16 | 14 | 0 | 2 | 9.29–27.51% |
| Fractional search: 8×4, 4×4, 4×8 fallback cases | 12 | 0 | 0 | 12 | — |
| Fractional callback/feature controls | 8 | 0 | 0 | 8 | — |
| Integer search | 40 | 29 | 0 | 11 | 7.98–33.09% |
| Total | 93 | 50 | 0 | 43 | — |

All 1,116 raw component samples report 0 B/op and 0 allocs/op. No significant component regressions were observed, although inconclusive controls do not prove equivalent performance. Final CAVLC gains are inter/sparse (12.90%), intra16/dense (21.59%), intra16/mixed (13.48%) and intra16/sparse (24.73%); inter/dense, inter/mixed and both zero cases are inconclusive. All eight integer early-exit cases are inconclusive.

Raw component tables are archived for [DCT](results/dct-components/benchstat.txt), [final CAVLC](results/cavlc-fixed-components/benchstat.txt) and [motion search](results/motion-components/benchstat.txt). The initial [CAVLC measurements](results/cavlc-components/benchstat.txt) predate the malformed-cache panic-state fix and are **superseded**; they were not pooled with the final six samples. The fixed candidate was selected before combined codec timing. Benchmark sources are retained in [the harness](harness/): [DCT](harness/dct_mb_bench_test.go.txt), [CAVLC](harness/cavlc_mb_bench_test.go.txt), [integer search](harness/svc_motion_estimate_fast_bench_amd64_test.go.txt) and [fractional refinement](harness/md_refine_fast_bench_amd64_test.go.txt).

Validation passed on the final selected source. Exact commands, exit codes and logs are retained in [checks.json](results/validation/checks.json).

- All H.264 and RDP package tests passed. All sixteen external-fixture encoder conformance cases executed and passed, with no skips: [package log](results/validation/packages.log), [conformance log](results/validation/conformance.log).
- Default, AVX2-disabled and all-optional-features-disabled output matched baseline byte-for-byte across ten codec cases each. FFmpeg decoded all **30 candidate streams / 2,400 frames** and ffprobe confirmed eighty frames per stream. [Cross-configuration parity](results/validation/stream-parity.json) links each case to its SHA-256; the full manifests are under [default](results/combined-parity/manifest.json), [AVX2-off](results/parity-avx2-off/manifest.json) and [CPU-off](results/parity-cpu-off/manifest.json).
- Common, encoder and AVC tests passed with `GODEBUG=cpu.avx2=off` and `GODEBUG=cpu.all=off`, and with the race detector (`CGO_ENABLED=1`). Focused new-path tests passed with `-gcflags=all=-d=checkptr=2`. Randomized parity, guard-page, malformed-input and callback tests cover the new paths. [Race log](results/validation/race.log), [pointer-check log](results/validation/checkptr.log).
- `go vet ./pkg/h264/... ./internal/rdp/...` passed. Linux/386 production packages built, and Linux/ARM64 common, encoder and AVC test binaries cross-built successfully. ARM64 runtime performance and correctness were not tested on hardware.
- Repository-configured golangci-lint passed with its compatible Go 1.26.9 toolchain and fresh caches: [lint result](results/validation/lint-fresh-cache.json), [lint log](results/validation/lint-fresh-cache.log). Two earlier runs reused absolute paths from other identical source snapshots, defeating the repository's existing path-based H.264 exclusions; their logs are retained. No lint configuration or production source was changed to resolve this cache issue. The configured lint exclusions cover the mechanical H.264 port; the separate vet and test checks above cover those packages.
- All 31 changed source/test files in the real workspace match the measured candidate hashes. Changed Go files are gofmt-clean and `git diff --check` passes: [source verification](results/validation/source-verification.json). [Tool versions](results/validation/tool-versions.json) record Go, FFmpeg, ffprobe, benchstat and lint.


See [reproduction instructions](reproduce.md) for fresh before/after runs. The [patches](patches/) preserve each selected implementation. Stream files, decoded fixtures and test binaries are omitted from this text archive; their hashes and commands are retained.
