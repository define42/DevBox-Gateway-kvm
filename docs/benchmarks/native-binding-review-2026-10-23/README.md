# Native Go binding latency review — 2026-10-23

The native v0.1.0 path is faster than the old single-slice encoder on this workload, but **slower per frame than the old four-slice configuration**. It uses less aggregate CPU. A lower dashboard encoding average does not establish smoother client video.

The reported live session was 29.6 FPS at 800×600. Whether that resolution describes the full desktop or just the player was not confirmed. No live ACK, conversion, or client-presentation measurements were available, so this review does not establish the cause of that session's stuttering.

## Actual adapter measurements

800×600 full-motion road fixture, QP 32. Median of six run means and six per-run percentiles; units are milliseconds. CPU time sums all threads and is not elapsed latency.

| Implementation | Mean | p95 | p99 | CPU/frame |
|---|---:|---:|---:|---:|
| Pure Go, 1 slice | 6.106 | 8.208 | 9.916 | 6.126 |
| Pure Go, 4 slices | 2.507 | 3.953 | 5.026 | 6.231 |
| Native v0.1.0 adapter, 1 slice | 3.978 | 5.628 | 6.319 | 4.033 |
| Native binding without metadata adapter, 1 slice | 3.995 | 5.749 | 6.781 | 4.048 |

- Each configuration encoded 1,440 timed frames across six independent processes, after 16 warmup frames per process. No encoding call exceeded 16.67 ms, and no Go GC occurred during these measured loops. This excludes initialization, the initial IDR, capture, conversion, transmission, and client presentation; sparse updates and later forced IDRs were not measured here.
- Native versus four-slice Go: about 59% longer elapsed encode time, but about 35% less aggregate CPU per frame. Native versus single-slice Go: about 35% shorter elapsed encode time. See benchstat files for repeat variability and significance.
- Native raw binding and production adapter were statistically indistinguishable. This tests the extra adapter work, **not** cgo versus direct C++: both paths retain cgo and the binding's allocation/copies.
- Native and single-slice Go produced identical decoded pixels for all 54 validation frames. Native, one-slice Go, and four-slice Go all decoded successfully. Four slices have different bitstream sizes and boundaries, so their output is not assumed byte-identical.

## Concrete migration findings

1. **Four requested slices become one native slice.** `internal/rdp/avc/openh264.go:24-39` discards the requested count because the external v0.1.0 binding hardcodes one codec worker and one slice. The inspected running Docker environment requested `H264_SLICES=4`, QP 32, software encoding, and the smooth profile. Previous four-slice codec benchmarks do not describe this binding.
2. **That also disables parallel color conversion.** `internal/rdp/avc/i420_parallel.go` and `i420_bgrx.go` choose conversion workers from the effective codec count (`e.threads`), now one. Conversion should instead use the already host-budget-capped requested worker budget independently of actual codec slices, while retaining existing runtime/area limits and NVENC behavior. Conversion holds the framebuffer publication mutex for the entire preparation, including worker completion (`internal/virtio/framebuffer_damage.go:220`).
3. **There is native wrapper overhead, but it is already timed.** The binding performs two cgo calls per ordinary frame (encode and free), allocates/copies encoded output into C storage, then allocates/copies it into Go. It borrows the raw Y/U/V planes instead of copying them at the Go/C boundary. The adapter additionally converts damage masks and scans the compressed Annex-B stream for SPS metadata; SPS rewriting allocates another output. The outer timer in `encoder.go:240-256` includes these costs and scheduler re-entry. There is no new native encode-request queue.
4. **The encoding display is only an arithmetic mean.** `manager_performance.go:257` computes interval encode time divided by encoded frames. Capture, conversion, writing, pacing, ACK wait, and client presentation are outside it. Slow frame gaps are not represented by this average.

## Conversion measurements

Actual production preparation functions on alternating deterministic full-frame images, no encoding. Six runs of 200 timed frames per configuration. Median run means, milliseconds. The four-worker *budget* is capped to three actual conversion workers at 800×600 by the existing area threshold; 1080p permits four. The experiment sets `e.threads` directly to isolate the currently coupled conversion count.

| Resolution | Input | Serial | Parallel budget 4 |
|---|---|---:|---:|
| 800×600 | BGRX | 0.388 | 0.280 |
| 800×600 | RGBA | 0.399 | 0.276 |
| 1920×1080 | BGRX | 1.768 | 0.887 |
| 1920×1080 | RGBA | 1.775 | 0.889 |

At 800×600 the direct BGRX conversion regression is only about 0.1 ms/frame on this host. At 1080p it is about 0.9 ms/frame. These are real costs, but the 800×600 result does not explain a roughly 33 ms delivery interval on its own. Production normally uses BGRX with the shipped 2D GPU configuration; scaled or other-format paths can differ.

## What 29.6 FPS does and does not tell us

- The office profile caps AVC at 30 FPS; smooth caps it at 60. Bitmap fallback is capped at 30. Profiles are applied when a VM launches. This implementation does not negotiate a client FPS setting.
- A 30 FPS source can legitimately produce about 30 sent FPS even under smooth: unchanged frames are skipped rather than duplicated.
- ACK feedback can impose a lower rate independently of encoding. The two-frame window and `graphics.go:366-405` pacing use the maximum of base interval, smoothed ACK turnaround / 2, and queue pressure. Around 67 ms ACK turnaround can produce a roughly 33.5 ms interval. Reported nonzero client queue depth can also increase a 16.7 ms base to 33.3 ms. These are explanations allowed by the code, not observations of this user's session.
- Server-sent FPS does not measure client presentation smoothness. To identify the live limiter, compare the actual desktop output dimensions, active encoder/profile, encodeMs, conversionMs, ACK turnaround, queued bytes/outstanding frames, and frame-delivery gaps during the stutter. Frame-gap p95/p99 is currently missing from the dashboard.

## Reproducibility and limitations

The original road1080 fixture from `../software-h264-vs-fork-2026-10-23/` was scaled to 800×600 with FFmpeg bilinear scaling, yuv420p, 54 frames. Both encoders read exactly the same resulting I420 bytes (SHA-256 in environment.json). Each run cycles that sequence, including its wrap discontinuities. This is a full-motion workload, not the user's exact video.

Harness source is included as `.go.txt` files and was applied with a Go overlay, leaving production code untouched. `run_800.py` and `run_conversion.py` record the exact run environment and commands; their temporary paths must be adjusted or recreated for a fresh run. Encoding run order alternates forward/reverse, timing is serial, GOMAXPROCS=4, process affinity CPUs 0-3, Go 1.27.3. Conversion subcase order is fixed. This was an active laptop with a powersave governor, not an isolated benchmark machine; raw repeats and process CPU measurements are retained.

The initial local `native.test` accidentally reused a cached cgo build linked to a scalar-only library. Its native CPU detector returned zero. **All results from that binary were discarded.** The corrected `native-asm.test` was rebuilt using `GOCACHE=/tmp/devbox-native-review/gocache-asm` and `PKG_CONFIG_PATH=/tmp/devbox-openh264-check/sdk/lib/pkgconfig`; full CPU detection and native SSE2/AVX2 symbols were verified. The SDK build uses -O3 with x86 assembly. Go's cache does not reliably invalidate cgo package output when an external static archive is replaced at the same path.

A read-only copy of the running Docker executable was also inspected: it contains the v0.1.0 module, real CPUID-based native feature detection, and native SSE2/AVX2 routines. The discarded scalar benchmark therefore did not describe that Docker executable. Runtime executable SHA-256: `ffacd5f8087363e24e6244a36c630e39ed1b9c9faa73417706c9dc2ed635ca1a`.

Go allocation statistics exclude C allocations. No end-to-end latency, real client decode scheduling, native slice-parallel decoder behavior, thermal throttling, or source publication cadence was measured. No production files or running services were changed by this review.
