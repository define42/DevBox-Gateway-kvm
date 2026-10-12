# Performance evidence for 8aaf546

Review date: 2026-10-11. Production revision: `8aaf546d47e5c079b0a6ccc9a34ef35c88ffeb8d`.
Read the [performance review](../../performance-review-8aaf546.md) for conclusions and limitations.

All measurements ran serially with Go 1.26.9, `GOMAXPROCS=4`, on the shared host described in
[environment.json](environment.json). Existing gateway/guest processes remained running.
No production source, service configuration or running service was changed. No full guest
office workload or input-to-display latency measurement was performed.

## Evidence

| Files | What they measure |
| --- | --- |
| `virtio.txt`, `rdp.txt`, `conversion.txt`, `disk.txt` | Existing component benchmarks; six samples each |
| `encoders.txt` | Forced software/NVENC, 1/2/4 independent 1080p encoders; six samples |
| `encoder-cpu.txt` | Separate four-encoder series including process user+system CPU/frame; six samples |
| `gpu-transfer.txt` | GPU transfers with 4 KiB fragmented backing; six samples |
| `idle.txt` | Synthetic idle device groups; three samples per group size |
| `gateway-reproductions.txt` | Actual route/authentication methods with fake backend/authenticator: inherited creation deadline and shared IP login budget |
| `password-hash.txt` | Real guest password hashing at 12/72/1024 bytes; six samples |
| `disk-head-of-line.txt` | Completed READ held behind a controlled later 100 ms FLUSH; three samples |
| `net-rx.txt` | RX descriptor assembly with fake backend, including empty reads; six samples |
| `encoder-profile.txt`, `encoder.*.pprof`, `encoder-*-top.txt` | Separate software scroll profile, four encoders, 200 measured frames/encoder |
| `summary.json` | Median/min/max/count by series, benchmark and unit |
| `benchstat.txt` | Statistical presentation of repeated benchmark measurements |

`commands.txt` is the original argument log, not shell-escaped commands. Use the quoted
commands below. `experiments/*.go.txt` are preserved review fixtures; normal `go test ./...`
does not compile them. The deadline, login-budget and disk tests **assert current adverse
behavior** to demonstrate it; convert their expectations before using them as regression tests.

## Reproduce

Run from the repository root at the reviewed revision, with dependencies already available.
These commands are sequential. NVENC requires CGO, the repository's native build prerequisites,
a compatible NVIDIA GPU and driver. Forced backend initialization fails instead of silently
falling back to software.

```sh
export GOTOOLCHAIN=go1.26.9 GOMAXPROCS=4 CGO_ENABLED=1
python3 docs/benchmarks/performance-8aaf546/prepare_overlays.py /tmp/gokvm-review-8aaf546

go test ./internal/virtio -run '^$' -bench 'Benchmark(GPUDesktop|FramebufferDamage|NetTx|BlkWriteFlushGroup)$' -benchmem -benchtime=300ms -count=6
go test ./internal/rdp -run '^$' -bench 'Benchmark(BitmapDamage|GraphicsTransport)$' -benchmem -benchtime=300ms -count=6
go test ./internal/rdp/avc -run '^$' -bench '^BenchmarkI420Damage$' -benchmem -benchtime=300ms -count=6
go test -tags=nvenc -overlay=/tmp/gokvm-review-8aaf546/encoders.json ./internal/rdp/avc -run '^$' -bench '^BenchmarkConcurrentDesktops$' -benchmem -benchtime=100x -count=6 -timeout=8m
go test ./internal/disk -run '^$' -bench '^BenchmarkQCOW2' -benchmem -benchtime=200ms -count=6
go test -overlay=/tmp/gokvm-review-8aaf546/gpu.json ./internal/virtio -run '^$' -bench '^BenchmarkReviewGPUTransfer$' -benchmem -benchtime=300ms -count=6
go test -overlay=/tmp/gokvm-review-8aaf546/idle.json ./internal/virtio -run '^TestPerfIdleGatewayDevices$' -v -count=3

go test -overlay=/tmp/gokvm-review-8aaf546/gateway-current.json ./internal/gateway -run '^TestPerfReview' -v
go test -overlay=/tmp/gokvm-review-8aaf546/gateway-current.json ./internal/gateway -run '^$' -bench '^BenchmarkPerfReviewGuestPasswordHash$' -benchmem -benchtime=200ms -count=6
go test -overlay=/tmp/gokvm-review-8aaf546/runtime-current.json ./internal/virtio -run '^TestReviewReadCompletionWaitsForLaterFlush$' -v -count=3
go test -overlay=/tmp/gokvm-review-8aaf546/runtime-current.json ./internal/virtio -run '^$' -bench '^BenchmarkReviewNetRx$' -benchmem -benchtime=300ms -count=6
go test -tags=nvenc -overlay=/tmp/gokvm-review-8aaf546/encoders-cpu.json ./internal/rdp/avc -run '^$' -bench '^BenchmarkConcurrentDesktops/(software|nvenc)/1080p/(Widget|Scroll)/4Sessions$' -benchmem -benchtime=100x -count=6 -timeout=5m

# Separate profile run; setup and calibration are included in profile totals.
go test -overlay=/tmp/gokvm-review-8aaf546/encoders-cpu.json ./internal/rdp/avc -run '^$' -bench '^BenchmarkConcurrentDesktops/software/1080p/Scroll/4Sessions$' -benchtime=200x -count=1 -cpuprofile=/tmp/gokvm-review-8aaf546/encoder.cpu.pprof -memprofile=/tmp/gokvm-review-8aaf546/encoder.alloc.pprof -o /tmp/gokvm-review-8aaf546/avc.test
go tool pprof -top /tmp/gokvm-review-8aaf546/avc.test /tmp/gokvm-review-8aaf546/encoder.cpu.pprof
go tool pprof -top -alloc_space /tmp/gokvm-review-8aaf546/avc.test /tmp/gokvm-review-8aaf546/encoder.alloc.pprof
```

To regenerate the checked-in summaries from the preserved raw text:

```sh
python3 docs/benchmarks/performance-8aaf546/summarize.py
```

Encoder throughput is aggregate across encoders and excludes guest rendering, RDP transport,
client decoding and frame pacing. Four-session B/op and allocs/op describe four frames.
CPU/frame includes all threads in the benchmark process, Go runtime and dispatch overhead;
it excludes other host processes and GPU engine execution time. The fixed 100-frame runs
warm the initial IDR before timing. Periodic IDRs are disabled; refresh/resize/retry IDR
bursts were not measured. The separate profile includes setup and warm IDRs. Profiles include initialization/calibration: total allocated bytes
are neither steady-state bytes/frame nor resident memory. Client image quality was not assessed.

The existing bitmap `Full` benchmark changes one pixel but supplies full-frame damage;
it measures full conversion/comparison, not a fully changing screen's network throughput.
QCOW2 read/write tests use a warmed, preallocated 64 MiB image with queue-depth-one overwrites.
The virtio flush group uses a 128 KiB raw file; the completion-order reproduction uses a fake backend. RX/TX
microbenchmarks do not exercise gVisor, real sockets, guest scheduling or real interrupt load.
