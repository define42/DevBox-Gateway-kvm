# Office profile encoding budget — 2026-10-12

Measured on Linux/amd64, Intel Core i7-7700HQ, Go 1.27.1, GOMAXPROCS=4,
on revision `c3c00b7` plus the office-profile changes.

The benchmark uses four independent, warmed software encoders, one slice each,
at 1920×1080. One operation encodes one nominal second's configured frame budget:
240 pictures for `smooth` versus 120 for `office`. Both profiles alternate the
same scrolling fixture on every encode. Ten interleaved, serial comparisons
avoided concurrent test/benchmark load.

| Metric per nominal-second budget | Smooth (60 FPS) | Office (30 FPS) | Change |
| --- | ---: | ---: | ---: |
| Full-picture encodes | 240 | 120 | −50% |
| Median elapsed encoding work | 1.296 s | 0.620 s | −52.20% |
| Allocated bytes | 6.261 MiB | 3.132 MiB | −49.98% |

Elapsed-time confidence intervals were ±7% and ±12%; benchstat reports
p=0.000, n=10. See [raw samples](profiles.txt) and [complete comparison](benchstat.txt).

This isolates the cost of the smaller work budget, not a faster per-frame codec.
It does **not** simulate real-time delivery, benchmark input latency, or prove
four simultaneous clients sustain 30 FPS. Real workloads, shared-host contention,
and coalescing alter both encoding time and compressed output. Runtime tests
separately verify the 30/60-FPS pacing, retained damage, and ACK slowdown.

Reproduce from the repository root with an installed `benchstat`:

```bash
profile_bench_dir=$(mktemp -d /tmp/gokvm-office-profile.XXXXXX)
go test -c -o "$profile_bench_dir/avc.test" ./internal/rdp/avc
set -o pipefail
for profile_trial in {1..10}; do
  "$profile_bench_dir/avc.test" -test.run '^$' \
    -test.bench '^BenchmarkOfficeProfileBudget$' \
    -test.benchtime=1x -test.cpu=4 -test.benchmem || exit
done | tee "$profile_bench_dir/profiles.txt"
benchstat -col '/profile@(smooth60 office30)' -row .name "$profile_bench_dir/profiles.txt"
```
