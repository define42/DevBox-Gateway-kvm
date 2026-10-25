# RDP ACK turnaround: 1, 2 and 4 slices

Four slices was fastest for encoding, sending and waiting for an ACK in both
workloads on this host. Window-drag ACK latency did not differ significantly
between the three settings. Two slices had the lowest mean ACK and smallest
H.264 payloads during scrolling; four slices had the lowest scrolling p95 ACK.

A [follow-up CPU measurement](cpu.md) quantifies the tradeoff: four slices
used about 45% more gateway CPU per window-drag frame than one, and 29%
more than two. Two slices offers a better balance of CPU cost and latency.

## Results

All times are milliseconds. Each entry is the median of 12 per-run statistics;
each run measured 100 frames after 10 warmup frames. The p95 column is the
median of the per-run p95 values, not a pooled percentile of all frames.
Total time includes conversion, encoding, sending and waiting for the ACK.

| Workload | Slices | ACK mean | ACK p50 | ACK p95 | Total time | H.264 bytes/frame |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Window dragging | 1 | 3.197 | 2.940 | 4.513 | 7.194 | 6,042 |
| Window dragging | 2 | 3.219 | 2.996 | 4.434 | 6.182 | 5,201 |
| Window dragging | 4 | 3.251 | 3.023 | 4.532 | 5.665 | 5,723 |
| Scrolling | 1 | 6.538 | 6.697 | 9.878 | 16.293 | 81,049 |
| Scrolling | 2 | 5.770 | 5.526 | 9.157 | 11.953 | 63,495 |
| Scrolling | 4 | 5.927 | 5.793 | 7.919 | 10.538 | 72,134 |

Benchstat reports no significant difference in window-drag mean or p95 ACK
for any pair. Four slices reduces total window-drag time by 21.25% versus one
and 8.36% versus two. For scrolling, two slices reduces mean ACK by 11.74%
versus one; four slices has 2.72% higher mean ACK than two but 13.51% lower p95
and 11.84% lower total time. Those total-time and scrolling comparisons have
p < 0.001. All 7,200 measured frames supplied valid ACK samples, with no ACK
races or client-reported decode failures.

These results favor four effective slices for total frame latency on this
host, and two for mean scrolling ACK and bandwidth. They do not justify
claiming that more slices alone fixes the window-drag ACK delay.

## Conditions and limits

- Base revision: `de5929e`, with the ACK benchmark parameterized by slice count.
- Linux amd64, Intel Core i7-7700HQ @ 2.80 GHz, Go 1.27.3, GOMAXPROCS=8.
- FreeRDP 3.32.1 (`bf217a5`), invoked through `xfreerdp-h264`, software GDI,
  AVC420, private Xvfb, loopback TLS and one frame outstanding.
- Identical 1280x720 alternating source frames, QP 20, ordinary H.264 damage
  updates. The fixture verifies that the effective slice count matches 1/2/4.
- The same test binary ran serially for every case; no other agent tests,
  linters or benchmarks ran during measurement. Other host activity is not
  controlled. The order below balances each setting's position in the run.
- ACK measures the production post-write-to-ACK timestamps, excluding
  conversion, encoding and socket writes. Xvfb does not measure physical
  display presentation or input-to-display latency. This workload is unpaced
  and excludes active VM load, WAN transport, startup IDRs and production's
  two-frame ACK window.
- Live workers also cap slices by CPU budget:
  `min(requested, max(1, GOMAXPROCS - max(1, guestCPUs) - 1))`.
  For example, four Go processors with two guest vCPUs permits one slice;
  four slices with two guest vCPUs needs at least seven Go processors.
  Check the effective slice count in session telemetry/logs before comparing
  a deployed session. No production defaults were changed by this experiment.

## Reproduction

The maintained targets now test and benchmark all six workload/slice cases:

```sh
make test-rdp-ack
make bench-rdp-ack
```

For this measurement, compile once and execute each slice selection with the
following command, substituting 1, 2 or 4 for the final `slices=2` selector:

```sh
go test -tags=integration -c -o /tmp/devbox-rdp-ack-slices.test ./internal/rdp
cd internal/rdp
GOMAXPROCS=8 DEVBOX_KVM_REQUIRE_ACK_TEST=1 /tmp/devbox-rdp-ack-slices.test \
  -test.run='^$' \
  -test.bench='^BenchmarkGraphicsACKFreeRDP$/.*/^slices=2$' \
  -test.benchtime=100x -test.count=1 -test.timeout=30s
```

Each selection runs Scroll and WindowDrag. Run six rounds in these slice
orders, then repeat the six rounds once: `(1,2,4)`, `(2,4,1)`, `(4,1,2)`,
`(4,2,1)`, `(2,1,4)`, `(1,4,2)`. Combine stdout into `raw.txt`.

```sh
benchstat -col '/slices@(1 2 4)' raw.txt
benchstat -col '/slices@(2 4)' \
  -filter '.unit:(ack_mean_ms OR ack_p95_ms OR sec/op OR bytes/frame)' raw.txt
```

Artifacts: [raw runs](raw.txt), [all comparisons](benchstat.txt),
[two versus four](two-vs-four.txt).

Validation: all six real-client integration cases passed; integration-tagged
RDP lint passed; all repeated benchmark invocations passed.
