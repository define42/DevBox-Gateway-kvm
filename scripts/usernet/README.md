# Userspace-network throughput and hostile-traffic checks

Run from the repository root on an otherwise idle machine:

```sh
scripts/usernet/run.sh /tmp/usernet-results
```

The result directory must be new. The runner records the toolchain, host limits,
source hashes, ten samples of each TCP benchmark, and two ten-second stress
scenarios. It uses loopback peers only; KVM, root, TAP and internet access are not
required. Dependencies must already be available in the Go module cache.

Options:

- `GOMAXPROCS=4`: default Go execution parallelism for reproducible runs.
- `USERNET_BENCH_COUNT=10`: repetitions of each one-second benchmark.
- `DEVBOX_USERNET_STRESS_DURATION=10s`: duration of each stress phase, from `1s`
  through `5m`.
- `USERNET_SOURCE_DIR=/path/to/checkout`: measure another tree containing the
  same harness.
- `GO=/path/to/go`: select the same Go toolchain for both revisions.

Run the sustained checks with the race detector separately from performance
measurements:

```sh
GOMAXPROCS=4 DEVBOX_USERNET_STRESS_DURATION=10s go test -race -tags=integration \
  ./internal/usernet \
  -run '^TestNetwork(MalformedFloodRecovery|ConnectionSaturationRecovery)$' \
  -count=1 -timeout=90s -v
```

The throughput benchmarks exercise established TCP streams through the synthetic
Go guest's Ethernet link, `Network.Write`/`Read`, gVisor and real host TCP sockets.
They count application bytes in one direction; a small acknowledgment marks each
complete transfer. Uploads and downloads check payload integrity. They retain
legacy local-network access solely to reach the loopback fixture and isolate
steady-state forwarding cost; policy evaluation at connection setup is outside
the timed loop.

The tests flood five fixed malformed Ethernet shapes while maintaining a valid
TCP stream, then separately hold 256 forwarded connections, reject additional
connections, verify recovery after releasing capacity and close an active
network. These are bounded adversarial regressions, not exhaustive fuzzing or a
proof of isolation. The process resource samples include the synthetic guest,
loopback server and test harness; file descriptor counts require Linux `/proc`.

`summary.json` contains medians and observed ranges. It makes no statistical
significance inference. For existing result files:

```sh
python3 scripts/usernet/summarize.py /tmp/old/bench.txt /tmp/new/bench.txt
```

Do not run independent benchmark processes at the same time. These measurements
include in-process guest and fixture overhead, use one active bulk stream, and
exclude KVM/virtio, external networks, DNS, multiple VM scheduling and TAP/passt.
They must not be presented as VM throughput capacity or a backend comparison.
