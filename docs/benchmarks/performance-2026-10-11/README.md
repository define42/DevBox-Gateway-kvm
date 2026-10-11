# Performance-review evidence — 2026-10-11

Companion to the [full review](../../performance-review-2026-10-11.md) of revision `3decb2ff9b0684269eda0b1ebeabbe73414e5862`, focused on several simultaneous office desktops.

[Implementation follow-up and before/after measurements](after/README.md) are
stored separately from this original baseline. The adverse-behavior experiments
here describe the reviewed revision; run current regression tests to validate fixes.

These are component measurements and controlled failure experiments, not an end-to-end capacity certification. Production source was unchanged. The experiments under `experiments/` have `.go.txt` extensions so normal repository tests do not discover them. Some deliberately assert current adverse behavior; a fix should change their expectation before they become regression tests.

| Evidence | Contents |
|---|---|
| `environment.json` | Host, revision, toolchain, device, filesystem, and sampling metadata |
| `virtio.txt`, `virtio-summary.txt` | Existing device/framebuffer/disk-flush microbenchmarks, six samples |
| `gpu-transfer.txt`, `gpu-transfer-summary.txt` | Fragmented guest backing, full versus partial 1080p/4K transfer |
| `concurrent-encoders.txt`, `concurrent-encoders-summary.txt` | Forced software/NVENC; 1/2/4 independent encoders; six samples |
| `encoder-medians.json` | Median of each reported metric for each encoder case |
| `idle-devices.txt` | Three-second CPU measurements for 0/1/4 synthetic device groups, three repetitions |
| `gateway-locks.txt` | Inventory and catalog requests blocked beyond their context deadlines |
| `provisioning.txt` | Sparse-copy expansion with full content verification; scaled HTTP deadline cancellation |
| `rdp-input-stall.txt` | ACK/input lock propagation under a controlled 150 ms graphics-write block |
| `*-profile.txt`, `*.pprof` | Separate CPU/allocation attribution runs; not used in benchmark medians |
| `encoder-profile-run.txt` | Profiled software-scroll benchmark execution |

CPU profiles cover the entire profiling invocation. In particular, encoder CPU and allocation profiles include setup, warmed IDRs, fixture construction, and benchmark calibration; allocation totals are neither steady-state allocation rates nor retained-memory estimates. Benchmark `B/op` and `allocs/op` use the timed region. For concurrent encoders, divide these group metrics by `frames/op` to obtain per-frame values. The p95 median is the median of six separately calculated run percentiles.

## Reproduction

Run from the repository root at the reviewed revision. Use a disposable checkout if testing another revision. The helper maps preserved sources into otherwise absent test paths using Go overlays; it writes manifests only to the directory you select. Commands run sequentially and use temporary files/fake backends, without starting guest VMs or modifying existing images.

```sh
export GOTOOLCHAIN=go1.26.9
export GOMAXPROCS=4
review_evidence="$PWD/docs/benchmarks/performance-2026-10-11"
review_tmp="$(mktemp -d /tmp/gokvm-performance-review.XXXXXX)"
python3 "$review_evidence/prepare_overlays.py" "$review_tmp"
```

Record the actual host CPU, kernel, toolchain, GPU/driver, filesystem, available RAM, swap activity, and competing workloads alongside a new run. Do not silently combine results across hosts/settings or overwrite the dated evidence.

```sh
go test ./internal/virtio -run '^$' \
  -bench 'Benchmark(GPUDesktop|FramebufferDamage|FramebufferRead|NetTx|BlkWriteFlushGroup)$' \
  -benchmem -benchtime=500ms -count=6 > "$review_tmp/virtio.txt"

go test -overlay="$review_tmp/gpu.json" ./internal/virtio -run '^$' \
  -bench '^BenchmarkReviewGPUTransfer$' \
  -benchmem -benchtime=300ms -count=6 > "$review_tmp/gpu-transfer.txt"

go test -overlay="$review_tmp/idle.json" ./internal/virtio \
  -run '^TestPerfIdleGatewayDevices$' -v -count=3 \
  > "$review_tmp/idle-devices.txt" 2>&1

go test -overlay="$review_tmp/gateway.json" ./internal/gateway \
  -run '^TestPerformanceReview' -v -count=1 \
  > "$review_tmp/gateway-experiments.txt" 2>&1

go test -overlay="$review_tmp/backpressure.json" ./internal/rdp \
  -run '^TestPerfGraphicsWriteBlocksKeyboardBehindACK$' -v -count=3 -timeout=15s \
  > "$review_tmp/rdp-input-stall.txt" 2>&1
```

The full encoder comparison requires the repository's NVENC build prerequisites, CGO, NVIDIA libraries, and a supported available GPU. It explicitly fails when the requested backend cannot initialize. The original full comparison used `-tags=nvenc` and 100 frames per encoder per sample:

```sh
CGO_ENABLED=1 go test -tags=nvenc -overlay="$review_tmp/encoders.json" \
  ./internal/rdp/avc -run '^$' -bench '^BenchmarkConcurrentDesktops$' \
  -benchmem -benchtime=100x -count=6 -timeout=5m \
  > "$review_tmp/concurrent-encoders.txt" 2>&1
```

For software-only replication, omit the tag and select `-bench '^BenchmarkConcurrentDesktops/software/'`. Do not present that as a replicated hardware comparison.

Separate attribution runs:

```sh
go test -overlay="$review_tmp/gpu.json" ./internal/virtio -run '^$' \
  -bench '^BenchmarkReviewGPUTransfer/3840x2160/Browser$' -benchtime=3s -count=1 \
  -cpuprofile="$review_tmp/gpu.cpu" -o "$review_tmp/gpu.test"
go tool pprof -top -nodecount=20 "$review_tmp/gpu.test" "$review_tmp/gpu.cpu"

go test -overlay="$review_tmp/idle.json" ./internal/virtio \
  -run '^TestPerfIdleGatewayDevices$/^groups=1$' -count=2 \
  -cpuprofile="$review_tmp/idle.cpu" -o "$review_tmp/idle.test"
go tool pprof -top -nodecount=20 "$review_tmp/idle.test" "$review_tmp/idle.cpu"

go test -overlay="$review_tmp/encoders.json" ./internal/rdp/avc -run '^$' \
  -bench '^BenchmarkConcurrentDesktops/software/1080p/Scroll/4Sessions$' \
  -benchtime=200x -count=1 -cpuprofile="$review_tmp/encoder.cpu" \
  -memprofile="$review_tmp/encoder.mem" -o "$review_tmp/encoder.test"
go tool pprof -top -nodecount=20 "$review_tmp/encoder.test" "$review_tmp/encoder.cpu"
go tool pprof -top -alloc_space -nodecount=15 "$review_tmp/encoder.test" "$review_tmp/encoder.mem"
```

Use `benchstat` on the new raw benchmark files to obtain distributions and compare the same case across revisions. No optimized implementation was measured in this review, so the report does not claim before/after speedups. The standard summaries' geomeans across different operations have no desktop-capacity interpretation.
