# CPU cost of 1, 2 and 4 H.264 slices

For window dragging, four slices used 44.55% more gateway CPU per frame than
one slice; two used 12.20% more. Four slices used 28.83% more gateway CPU than
two for a 3.80% reduction in total frame time in this follow-up run. These
measurements favor two slices when balancing CPU cost and window-drag latency;
four remains the fastest option with spare CPU capacity.

## Results

Each value is the median of 12 runs, each with 100 measured frames after 10
warmup frames. CPU time sums scheduled time across all threads of each process.
It is CPU work per frame, not elapsed time or a percentage of the whole machine.
Total frame time is elapsed conversion, encoding, sending and ACK waiting.

| Workload | Slices | Gateway CPU ms/frame | Change vs 1 | FreeRDP CPU ms/frame | Xvfb CPU ms/frame | Total frame time, ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Window dragging | 1 | 3.787 | baseline | 3.443 | 1.549 | 6.193 |
| Window dragging | 2 | 4.249 | +12.20% | 3.375 | 1.602 | 5.147 |
| Window dragging | 4 | 5.474 | +44.55% | 3.585 | 1.677 | 4.951 |
| Scrolling | 1 | 8.978 | baseline | 6.999 | 1.810 | 14.168 |
| Scrolling | 2 | 8.689 | -3.21% | 6.365 | 1.910 | 10.283 |
| Scrolling | 4 | 11.200 | +24.75% | 6.739 | 1.978 | 9.352 |

For scrolling, four slices used 28.89% more gateway CPU than two and reduced
total frame time by 9.05%. All gateway CPU and total-time comparisons in these
runs have p < 0.001 in benchstat. Client CPU for window dragging did not differ
significantly between one and two, or one and four; four used 6.22% more than
two (p < 0.001). Gateway, client and Xvfb CPU are reported separately because
the client normally runs on another machine.

For an illustrative sustained 30 frames/s of the same window-drag workload,
the gateway CPU estimates are 11.4%, 12.7% and 16.4% of **one CPU core** for
one, two and four slices. These are calculated as CPU ms/frame × 30 / 10;
they are not measured paced utilization. They exclude guest VM CPU and other
production work. Four versus one adds about five percentage points of one
core in this estimate, rather than 45 percentage points of host utilization.

## Measurement and limits

The host, client, resolution and balanced order match the [latency study](README.md).
A newly compiled test binary includes process CPU snapshots taken immediately
before and after the measured loop. Linux process CPU clocks sum user and
kernel CPU time across all threads. The server uses CLOCK_PROCESS_CPUTIME_ID;
FreeRDP and Xvfb use their own process CPU clocks. Startup, warmup and teardown
are excluded. Gateway CPU includes conversion, encoding, TLS/socket writes,
ACK processing, the Go runtime and benchmark overhead, including the final
loop bookkeeping. It does not isolate encoder CPU alone.

All 36 invocations ran serially without agent tests, lint or builds competing
with them. Every one of the 7,200 measured frames had a valid post-write ACK
sample; there were no ACK races, reported decode failures or failed runs.
Other host activity and CPU frequency are not controlled. This follow-up had
lower absolute elapsed times than the earlier latency study, so compare
settings within each run set instead of attributing differences between sets
to the measurement code. The relative CPU cost also depends on the content,
frame size and effective slice count. These fixtures exclude active VM load,
WAN transport and physical display presentation.

## Reproduction and validation

Use the same compile/run commands and 12 balanced rounds as in the
[latency study](README.md), with a binary built from the CPU-instrumented
benchmark, and save output as `cpu-raw.txt`. The maintained
`make bench-rdp-ack` target now emits CPU metrics too.

```sh
benchstat -col '/slices@(1 2 4)' \
  -filter '.unit:(server_cpu_ms/frame OR client_cpu_ms/frame OR xvfb_cpu_ms/frame OR total_cpu_ms/frame OR sec/op)' \
  cpu-raw.txt
benchstat -col '/slices@(2 4)' \
  -filter '.unit:(server_cpu_ms/frame OR client_cpu_ms/frame OR xvfb_cpu_ms/frame OR total_cpu_ms/frame OR sec/op)' \
  cpu-raw.txt
```

Artifacts: [raw runs](cpu-raw.txt), [CPU comparisons](cpu-benchstat.txt),
[two versus four](cpu-two-vs-four.txt).

Validation: the CPU smoke benchmark and all six real-client integration cases
passed; integration-tagged RDP lint and all repeated benchmark invocations
passed. No production defaults changed.
