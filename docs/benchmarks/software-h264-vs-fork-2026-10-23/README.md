# Current Go H.264 versus the current OpenH264 fork — 2026-10-23

The native fork is **1.46–1.57× faster on the tested single-slice video and PDF workloads**, using **31–36% less encoding time**. With identical unchanged-macroblock hints, the sparse desktop case is **4.15× faster with one slice**. Four-slice timings are much more variable: several cases significantly favor the fork, but 1080p road video at QP20 is inconclusive. These are fresh codec measurements, not reused numbers from the earlier native comparison.

Compared revisions:

- Ours: gateway `afd046db6dc6044ccd49080fcef8d52a96c02852`, current `pkg/h264` through the production AVC constructor. All 621 codec/AVC/module files in the build manifest were unchanged at the final verification. Existing local `.env.example` and `compose.yaml` edits were unrelated to this harness.
- Native: [`define42/openh264` at `3aa03c1224e7ee31ded9a3294fc726da1a00235b`](https://github.com/define42/openh264/commit/3aa03c1224e7ee31ded9a3294fc726da1a00235b), verified as the default branch head before acquisition. The binary reports OpenH264 2.7.0. This replaces the old `4c8c5cdc` reference used by the previous reports.

No production encoder code or configuration was changed for this test.

## Encoding time

Values are medians of six per-run mean frame times. The ratio is our median divided by the fork's median; values above one favor the fork. The p-values compare the two sets of six run averages, using benchstat's default test, without a multiple-comparison correction.

| Workload | Slices | Ours ms/frame | Fork ms/frame | Ours/fork | Wall-time result |
|---|---:|---:|---:|---:|---|
| Road, 1920×1080, QP20 | 1 | 30.332 | 20.074 | 1.51× | Fork faster, p=0.002 |
| Road, 1920×1080, QP20 | 4 | 14.621 | 18.141 | 0.81× | Inconclusive, p=0.240 |
| Road, 1920×1080, QP32 | 1 | 21.988 | 13.971 | 1.57× | Fork faster, p=0.002 |
| Road, 1920×1080, QP32 | 4 | 21.268 | 7.655 | 2.78× | Fork faster, p=0.002; noisy |
| PDF, 1920×1080, QP20 | 1 | 11.085 | 7.610 | 1.46× | Fork faster, p=0.002 |
| PDF, 1920×1080, QP20 | 4 | 12.437 | 4.078 | 3.05× | Fork faster, p=0.002; noisy |
| Road, 1280×720, QP20 | 1 | 13.213 | 8.779 | 1.51× | Fork faster, p=0.002 |
| Road, 1280×720, QP20 | 4 | 9.989 | 4.864 | 2.05× | Fork faster, p=0.026; noisy |
| Widget, no hints, 1920×1080, QP20 | 1 | 6.403 | 4.304 | 1.49× | Fork faster, p=0.002 |
| Widget, no hints, 1920×1080, QP20 | 4 | 8.312 | 6.145 | 1.35× | Fork faster, p=0.026; noisy |
| Widget, hints, 1920×1080, QP20 | 1 | 1.124 | 0.271 | 4.15× | Fork faster, p=0.002 |
| Widget, hints, 1920×1080, QP20 | 4 | 0.873 | 0.376 | 2.32× | Fork faster, p=0.002; noisy |

The single-slice video/PDF confidence ranges are approximately ±3–7%. Four-slice ranges are often tens of percent and reach ±127% for native 720p road video. Even the hinted four-slice widget has ±74% for Go and ±38% for native. These significant differences describe this experiment; their exact magnitudes should not be treated as stable production ratios. The lower Go median for QP20 road/four slices does **not** establish that Go is faster.

The QP20 road/four-slice case also has a worse native p95: **35.64 ms versus 23.42 ms** for Go (p=0.026), despite its inconclusive average-time comparison. Other measured p95 comparisons favor native. Reported p95 values are medians of per-run p95s, not a pooled latency percentile.

[Complete statistics, including confidence ranges, CPU time, output bytes and p95](benchstat.txt), [machine-readable medians and paired ratios](summary.json), [all run averages: Go](results/measure/go.txt), [native](results/measure/native.txt).

## CPU use and sparse updates

Process CPU time sums time across all threads and differs from frame latency. Single-slice video/PDF CPU savings are approximately 31–37%. For the hinted widget, the native fork uses approximately **76% less CPU time** with either slice setting:

| Hinted widget | Ours CPU ms/frame | Fork CPU ms/frame | Fork reduction |
|---|---:|---:|---:|
| 1 slice | 1.124 | 0.270 | 75.94% |
| 4 slices | 1.571 | 0.378 | 75.96% |

Both encoders receive the same valid hints. This exercises the native fork's newer unchanged-block implementation, including its available run batching and caller-thread handling for small updates. The experiment measures the implementations together and does not isolate the contribution of each optimization. Native motion-vector hints are left null because the current Go production wrapper has no matching API.

The widget is a deterministic 128×64 changing rectangle over the first real PDF frame. It changes 32 of 8,160 macroblocks in each of eight cyclic input frames. The hint generator compares every visible Y/U/V sample of each block against the previous frame, including the loop boundary and cropped bottom row. Hints are disabled on the first encoded frame. A no-hint control uses exactly the same pixels. Within each slice setting, the hinted and unhinted complete widget streams are also identical.

## Matching and measurement

- Linux amd64, Intel Core i7-7700HQ, four physical cores/eight logical CPUs. CPU 2 is used for one-slice tests; CPUs 0–3 are four distinct physical cores for four-slice tests. The existing `powersave` governor and workstation activity are unchanged. Load averages, CPU counters and temperatures are recorded before/after every process.
- Go 1.27.3, `CGO_ENABLED=0`, `GOAMD64=v1`, `GOMAXPROCS=4`, default GC settings. Native GCC/G++ 13.3, release `-O3`, assembly enabled, NASM 2.16.01, **LTO disabled**. A runtime check verifies native AVX2 dispatch. The codec archive is statically linked; libc/libstdc++ remain dynamic. This measures the default optimized native build, not its optional LTO configuration.
- Camera real-time, low complexity, fixed QP20 or QP32, 60 FPS codec clock, one reference/spatial/temporal layer, Baseline/CAVLC, fixed one/four slices, full-range BT.709. Rate control, deblocking, periodic IDR, frame skipping, scene/background detection, adaptive quantization, denoising, load balancing and LTR are disabled. The harnesses emit and compare 63 effective parameter fields, rather than assuming defaults match.
- Each fresh process warms up for 16 frames, then measures 120 video/PDF frames or 480 widget frames. Six repetitions alternate Go/native and native/Go order, with offsets by case and slice setting. Every process runs serially; there are no concurrent builds, tests, decoders or other agent benchmarks during timing. No samples are discarded or selective reruns performed.
- Both encoders use identical preloaded I420 planes. Timing includes the encode call and concatenation into a caller-owned output buffer. Input preparation, loading, initialization, hint generation, CSV writes and validation capture are outside timing. First-frame IDR is in warmup. Native output destruction occurs after the timed call; Go reclamation can occur through GC in later calls. These are encode-call timings rather than a separately matched allocation-lifecycle benchmark.
- Wall time, total process CPU time, each frame's byte count and p95 are retained. CSV frame timings are written after encoding. No encoded streams are captured during measurement. All binary/input hashes are checked before and after the full timing suite, which completed in approximately 294 seconds.

The road fixture contains 54 driving frames, decoded from `VID_1920x1080_cabac_temporal_direct.264`, at 1080p and bilinearly scaled to 720p. The PDF fixture contains 50 frames decoded from `Adobe_PDF_sample_a_1024x768_50Frms.264` and bilinearly scaled to 1080p. The prior decoded fixture cache was reused only after verifying its hashes; both source files also match the current fork's `res/` files. [Fixture hashes and exact decode commands](fixtures.json), [cases](cases.json), [measurement protocol](results/measure/protocol.json).

## Validation and scope

All **12 matching stream pairs** are byte-identical, including parameter sets and initial IDR. FFmpeg independently decoded both versions of each stream with `-xerror`, no diagnostics and the expected frame count: **3,072 frames per encoder, 6,144 total**. Per-frame decoded hashes also match. Output size and decoded quality are therefore identical in these comparisons. [Validation results](results/preflight/validation.json).

The Go and C++ harnesses compile successfully, and all cases validate matching effective settings. Production source fingerprints remain unchanged. [Go/native harness builds](results/build/harness-build.json), [native build and dispatch metadata](results/build/native-build-metadata.json), [source verification](source-verification.json).

This is a software **encoder** comparison on short repeated sequences on one active workstation. Four-slice noise remains substantial despite affinity and alternating order. Widget runs are short (480 frames), especially for native; their confidence ranges are retained. These results do not establish live RDP FPS, latency across the full gateway pipeline, decoder performance, ARM performance, hardware-encoder performance, or the capacity of simultaneous sessions. Capture, pixel conversion, RDP framing, network writes, ACK waits and client presentation are excluded.

## Reproduction

The scripts take an explicit temporary working directory and leave production code untouched. `build_harnesses.py` injects the Go test through an overlay. Prerequisites are Go 1.27.3, GCC/G++, make, NASM, Python 3.11+, FFmpeg/ffprobe, taskset and benchstat. Run on an otherwise quiet machine if possible; do not run builds or other benchmarks during measurement.

From the gateway repository root, with the required tools on `PATH`:

```sh
repo_root="$PWD"
bench_work=$(mktemp -d /tmp/h264-vs-fork.XXXXXX)
bench_scripts="$repo_root/docs/benchmarks/software-h264-vs-fork-2026-10-23"
git clone https://github.com/define42/openh264.git "$bench_work/native-src"
git -C "$bench_work/native-src" checkout --detach 3aa03c1224e7ee31ded9a3294fc726da1a00235b
make -C "$bench_work/native-src" -j4 libopenh264.a CC=gcc CXX=g++ \
  BUILDTYPE=Release CFLAGS_OPT=-O3 USE_ASM=Yes USE_LTO=No ASM=nasm
python3 "$bench_scripts/prepare.py" --work "$bench_work" --res "$bench_work/native-src/res"
python3 "$bench_scripts/build_harnesses.py" --repo "$repo_root" --work "$bench_work"
python3 "$bench_scripts/run.py" --work "$bench_work" --mode preflight
python3 "$bench_scripts/run.py" --work "$bench_work" --mode measure --repetitions 6
python3 "$bench_scripts/summarize.py" --work "$bench_work" --benchstat benchstat
```

The runner refuses to overwrite an existing preflight or measurement directory. It checks identical effective settings and decoded output before allowing measurement. The archived native build used an extracted NASM executable; its exact path/command and version are in the build metadata. CPU-affinity choices must be adapted on hosts with different topology or allowed CPU sets. Reproducing this exact Go version requires checking out `afd046d` in a separate checkout and making these harness files available to the build script.

`archive.py` preserves text evidence and verifies source fingerprints after measurement. Encoded streams, raw I420 inputs, binaries and the native archive stay in the temporary workspace; their hashes are retained in [omitted payloads](omitted-payloads.json), the fixture manifest and build records. [Harness interface details](harness-notes.md).
