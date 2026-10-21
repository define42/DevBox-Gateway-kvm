# Software H.264 performance — 2026-10-21

Implemented four improvements:

- Reconstructed 16×16 luma and 8×8 chroma blocks use checked, unrolled amd64
  copies. Other architectures retain Go row copies. Both buffer spans are
  checked before any write, and the kernels copy only the block's pixels.
- VAA source SAD calculation batches a macroblock row into one SSE2 call while
  preserving all four 8×8 quadrant sums. Valid unchanged-source hints skip
  untouched macroblocks. The hints track the original source reference and POC
  separately from reconstructed-picture reuse, and are discarded after errors,
  skips, incompatible preprocessing, or reference changes.
- The managed gateway exposes `H264_SLICES=1|2|4` / `serve -h264-slices`.
  The default is still one; the existing worker CPU and picture-size caps apply.
- Resized RGBA-to-I420 conversion gathers exact nearest-neighbor samples into
  two bounded scratch rows and reuses AVX2 conversion/change detection. It
  preserves full-range BT.709 arithmetic, chroma averaging, and dirty regions.
  Non-AVX2 hosts and tiny regions use the scalar path.

Measurements use Intel Core i7-7700HQ, Linux amd64, Go 1.27.3,
`GOAMD64=v1`, and `GOMAXPROCS=4`. The baseline is clean commit
`9261ed14829c744bec4981dc54ada9e32ee15e93`; the candidate is this patch.
[Build information](build-info.txt) records the four precompiled test binaries.
Six serial before/after pairs alternated order. No builds, test jobs, or other
benchmarks ran during timing; normal desktop services and an existing VM
remained active, so this is a shared-host measurement.

Desktop fixtures alternate two deterministic 1920×1080 views. Widget updates
change a 64×32 rectangle; scrolling shifts the main content region eight pixels.
Encoder initialization and the first IDR are outside timing. Each sample uses
exactly 200 predicted frames so encoded byte counts are comparable. Conversion
and VAA samples use 500 ms each. Figures below are benchstat medians.

| Workload | Before | After | Time reduction |
| --- | ---: | ---: | ---: |
| Small update, one slice, conversion + encoding | 4.589 ms | 3.397 ms | 25.96% |
| Small update, four slices, conversion + encoding | 3.161 ms | 2.200 ms | 30.40% |
| Scroll, one slice, conversion + encoding | 13.62 ms | 13.07 ms | Inconclusive, p=0.093 |
| Scroll, four slices, conversion + encoding | 7.888 ms | 7.250 ms | 8.10%, p=0.015 |
| RGBA 800×600 → I420 1024×768 | 2.969 ms | 1.530 ms | 48.48% |
| Native RGBA → I420 1024×768 | 615.4 µs | 623.1 µs | Inconclusive, p=0.310 |
| Full VAA SAD, 320×192 | 14.009 µs | 4.041 µs | 71.16% |
| Full VAA SAD, 1920×1088 | 768.0 µs | 314.0 µs | 59.11% |

All other listed reductions have p=0.002, n=6. The full VAA benchmark measures
SIMD batching without unchanged-block hints; the desktop results include both
source-hint skipping and reconstructed-block copies. They do not isolate each
optimization's contribution. Scrolling gains are smaller and less consistent
than the small-update gains. These results measure CPU processing, not client
FPS or end-to-end RDP latency.

Encoded bytes per frame stayed equal before/after for each slice count:
scroll 3,696 bytes with one slice and 6,136 with four; widget 107.1 and 155.1
respectively. Allocation counts remained 12/frame for one slice and 29/frame
for four. The single-slice allocation size grew 24 bytes/frame for the VAA
parameter's hint slice; parallel allocation bytes vary slightly. Conversion and
full VAA remain allocation-free. The widget conversion submetric increased
about 0.2–0.3 µs (3–5%) in this run, while total frame time decreased 26–30%.
Native full-image conversion showed no significant change.

Complete comparisons and raw samples:
[desktop](desktop-benchstat.txt) ([before](desktop-before.txt), [after](desktop-after.txt)),
[conversion](conversion-benchstat.txt) ([before](conversion-before.txt), [after](conversion-after.txt)),
and [VAA](vaa-benchstat.txt) ([before](vaa-before.txt), [after](vaa-after.txt)).

Four slices already existed in the codec; the new setting makes them available
through managed gateway startup. Extra slices reduce latency by using CPU
concurrently and can increase output size. The benchmark requests codec slices
directly, so it does not measure guest contention or the managed worker cap.
Use `H264_SLICES=2` or `4` only with an appropriate CPU budget and compare
concurrent desktop workloads. See [configuration](../../configuration.md#storage-and-desktops)
for the cap and restart requirements. NVENC ignores this setting; software
fallback uses it. No running deployment was restarted during this change.

Validation passed:

- [Affected suites](tests.txt): `go test -p=2 -count=1 -timeout=10m
  ./pkg/h264/... ./internal/rdp/... ./internal/config ./internal/gateway
  ./internal/virtio .`. The run used an isolated network namespace to avoid
  existing service ports; DAC override capabilities were dropped so filesystem
  permission tests retained their normal behavior.
- [Race detector](race.txt): encoder, processing, and AVC packages. Assembly
  memory accesses themselves are not race-instrumented.
- [CGO-disabled suites](purego.txt), [NVENC-tagged suites](nvenc.txt), and
  [AVX2-disabled conversion tests](no-avx2.txt).
- Linux ARM64 cross-compilation of encoder and AVC tests. ARM64 execution was
  not performed. Linux 386 copy/unchanged/VAA tests compiled and executed.
- [Repository-pinned golangci-lint 2.12.2](lint-pinned.txt) with cached Go 1.26.9:
  zero issues. Tests and timing used Go 1.27.3.
- Compose configuration validation and `git diff --check`.

New tests compare SIMD SAD and reconstructed copies against scalar references,
including exact-sized and short buffers, unaligned data, offsets, padding,
guards, and source immutability. VAA tests compare hinted and unhinted encoded
bitstreams over recycled source buffers, padded dimensions, chroma changes,
IDRs, and error recovery, and assert that hints actually engage. They also
cover hint release when preprocessing validation rejects a frame. Scaling tests
use an independent pixel reference across resizing directions, nonzero image
origins, padded strides, partial rectangles, vector tails, one-pixel sources,
maximum output width, and alpha-only changes. Configuration tests cover
validation, defaults, CLI/environment precedence, and child-process forwarding.

To reproduce, build baseline and candidate AVC and processing test binaries
with the same Go toolchain. Put them in a scratch directory as `before.test`,
`after.test`, `vaa-before.test`, and `vaa-after.test`, then run
`python3 measure.py /path/to/scratch` using the [saved runner](measure.py).
Run `benchstat desktop-before.txt desktop-after.txt` (and the corresponding
conversion/VAA pairs) in that directory. The runner overwrites its result files.
Additional committed benchmarks cover reconstructed-block row versus block
copies, full versus masked VAA on the same source pair, and scalar versus SIMD
scaled conversion for full, 64×32, and 2×2 regions.
