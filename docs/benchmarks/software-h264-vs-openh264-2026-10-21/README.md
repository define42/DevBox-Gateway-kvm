# Go H.264 port versus native OpenH264, 2026-10-21

Native OpenH264 2.6.0 was faster in this matched codec-only comparison: **2.02× with one slice and 1.68× with four slices**. Both complete output streams were byte-identical. This comparison uses the current Go implementation, including the round-4 optimizations.

## Results

Each cell is the median of six per-run averages. Each run encodes 120 measured frames after 16 warmup frames. Native/Go execution order alternates; all runs are serial.

| 1920×1080, QP20 | Go codec wall time | Native codec wall time | Native throughput advantage | Go CPU time | Native CPU time |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 slice / 1 encoder worker | 48.38 ms/frame | 23.96 ms/frame | 2.02× | 48.27 ms/frame | 23.92 ms/frame |
| 4 slices / 4 encoder workers | 17.77 ms/frame | 10.56 ms/frame | 1.68× | 54.84 ms/frame | 32.77 ms/frame |

Wall-time and CPU-time differences are statistically significant for both configurations (benchstat p=0.002, n=6). Native reduces measured wall time by approximately 50% and 41%, respectively. CPU time sums all process threads; it is not frame latency.

The median per-run p95 was 59.73 vs 28.98 ms for one slice, and 22.59 vs 14.30 ms for four slices (Go vs native). These describe only the tested short sequence, not production tail latency.

See [complete statistics](benchstat.txt), [Go samples](go.txt), [native samples](native.txt), and the per-pair logs. Native stdout rounds bytes/frame to an integer; `measure.py` normalizes that field from the exact `timed_bytes` total. [Unnormalized native output](native-raw.txt) is retained. The exact timed totals are identical: 12,213,507 bytes for one slice and 12,246,250 bytes for four slices.

## What was compared

- Intel i7-7700HQ, Linux/amd64; Go 1.27.3, GOMAXPROCS=4; native GCC 13.3.0 release `-O3` with SIMD enabled. Native runtime detection confirms SSE2, SSSE3 and AVX2.
- Native [OpenH264 source commit 4c8c5cdc](https://github.com/cisco/openh264/tree/4c8c5cdc0299407af14751a4c37449ff3d367745), version 2.6.0, the reference version used by the port. The benchmark statically links this build, not the installed distribution library (2.4.1). This is not a comparison against the latest upstream release.
- Five frames from the existing Cisco Absolute Power fixture, bilinearly scaled from 1280×720 to 1920×1080 and converted once by the gateway's RGBA-to-I420 implementation. Both implementations receive the same preloaded I420 bytes and loop through the same five frames.
- The Go harness calls the actual `newCodecEncoder` constructor. Native matches its effective settings: camera real-time, low complexity, QP20, 60 FPS codec clock, one spatial/temporal layer and reference, Baseline/CAVLC, fixed slice counts, no periodic IDR, no rate control, deblocking, frame skipping, scene detection, background detection, adaptive quantization, denoising, load balancing, or LTR; full-range BT.709.
- Timed work is codec encoding plus concatenating its NAL output into a caller-owned buffer in both implementations. Input loading/conversion, initialization, stream verification and output-file writes are outside timing. No unchanged-macroblock hints are supplied.
- The native harness is standalone. It does not measure integration through cgo or a native backend inside the gateway. No production encoder code was changed for this comparison; the Go harness is injected using a temporary `go test -overlay` file.

## Output validation

Both implementations produced identical complete 136-frame streams (16 warmup +120 measured), including parameter sets and the initial IDR:

| Slices | Complete bytes | SHA-256, identical for Go and native |
| --- | ---: | --- |
| 1 | 13,909,704 | `658d0b3dfae43056c220b00cc51eda9b1f8e00e5259dd844d3dc675050ea235d` |
| 4 | 13,948,401 | `2f4edfcf16ef8a86c2c93454d2fd1982b690d962503ad014ec72746c7764fbaf` |

Independent FFmpeg decoding produced 136 frames without errors for each slice configuration. Because the streams match byte-for-byte, bitrate and decoded quality are identical in this comparison. See [parity](parity.txt) and [decoder checks](decoder.txt).

## Limits

This is a loop of five video frames on one active workstation, not a broad codec benchmark or live RDP test. Background activity and CPU frequency changes remain visible in the samples, although native is faster in every paired run. The measured ratio should not be generalized to all video, CPUs, resolutions, quantizers, or screen content.

Results exclude RGB conversion, capture, framing, network writes, ACK waits and client presentation. They do not establish live RDP FPS or a live dashboard change. Go's unchanged-macroblock hints can avoid work on sparse desktop updates; that advantage is not exercised by this full-screen video comparison.

A follow-up [source and profile investigation](why-native-is-faster.md) identifies the remaining SIMD and per-block call differences, and finds little GC overhead.

## Reproduction

Temporary build workspace: `/tmp/openh264-comparison-20261021`. [Build information and binary/input hashes](build-info.txt), [source fingerprints](sources.sha256), [native build log](native-build.log), and [CPU dispatch evidence](native-cpu-flags.txt) are retained.

1. Archive the native source commit into a temporary directory and build `make -j2 BUILDTYPE=Release USE_ASM=Yes libraries` with NASM available. This run extracted the distribution NASM package into `/tmp`; it did not install system packages or change the source checkout.
2. Build `native_bench.cpp` with `g++ -O3 -std=c++17 -I native/codec/api/wels native_bench.cpp native/libopenh264.a -pthread -o native_bench`.
3. Map a virtual `internal/rdp/avc/z_openh264_comparison_test.go` path to a temporary copy of `go_compare_test.go.txt` named `go_compare_test.go` using a Go overlay JSON. Build with `go test -overlay=OVERLAY.json -c -o go.test ./internal/rdp/avc` from the gateway root.
4. Export the existing RGBA fixture by running the test binary with `COMPARE_EXPORT=1`, `COMPARE_RGBA` naming the RGBA file and `COMPARE_I420` naming `clip1080p.i420`; select `-test.run=^TestOpenH264ComparisonExport$`.
5. Place the two binaries, input I420 file and [measurement script](measure.py) in the same temporary directory. Run `python3 measure.py`; run [preflight.py](preflight.py) separately to compare full streams. No builds, other benchmarks or decoder validation should run concurrently with measurement.
