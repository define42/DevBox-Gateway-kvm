# Userspace-network measurements — 2026-10-21

The forwarding harness passed sustained malformed-frame and connection-saturation
checks before and after the network hardening. The hardened code also passed the
same checks with Go's race detector. Bulk TCP throughput and allocations are now
measurable with a repeatable local harness; the observed variation is too wide to
claim a performance improvement or regression from these samples.

## Scope and environment

The baseline is `79a82e3df12d5f067d9d6b05e2316c21c98ce6a9` with the new measurement
harness applied. The hardened sample contains the destination-policy and bounded
DNS-service changes in the working tree. Both use Go **1.27.2**, Linux
**7.0.0-38-generic**, an **Intel Core i7-7700HQ**, and **GOMAXPROCS=4**. The host has
eight logical CPUs. The open-file limit is 1,048,576; the harness does not install
CPU or memory cgroup limits. Cgroup CPU/memory limit files were not visible at the
paths checked by the runner, so host resource isolation is not established here.

Each benchmark uses an established connection through:

```text
synthetic gVisor guest TCP -> guest Ethernet bridge -> Network.Write
-> production gVisor forwarder -> host loopback TCP socket
```

Replies return through `Network.Read` and the guest Ethernet bridge. The fixture
uses legacy local-network access to reach its loopback server. Policy evaluation
happens at connection setup, outside the timed loop. No external packets or DNS
requests are generated. Host sockets, the synthetic guest and the server all run
in the test process; their overhead and allocations are included.

Source hashes and environment records are retained in
[baseline-environment.txt](network-hardening-2026-10-21/baseline-environment.txt)
and [hardened/environment.txt](network-hardening-2026-10-21/hardened/environment.txt).
Subsequent helper-parameter renaming and lint comments do not change measured
behavior. Other host work was discouraged during the timing windows, but CPUs
were not pinned or reserved, and these are not controlled laboratory results.

## TCP throughput and allocations

Ten one-second samples were collected for every case. MB/s counts payload bytes
in the named direction, excluding the small acknowledgment after each block.
Payload integrity is checked. The table shows the median and observed minimum to
maximum, with every sample retained in the raw files.

| Operation | Baseline MB/s, median (range) | Hardened MB/s, median (range) | Baseline B/op, median | Hardened B/op, median | Baseline / hardened allocations per operation, median |
| --- | ---: | ---: | ---: | ---: | ---: |
| Upload 64 KiB | 74.42 (36.06–109.91) | 73.38 (55.33–89.03) | 74,654.5 | 74,421 | 127.5 / 126 |
| Upload 1 MiB | 113.00 (78.42–161.33) | 141.75 (82.55–175.53) | 1,211,080.5 | 1,214,611 | 1,863 / 1,853 |
| Download 64 KiB | 50.74 (40.85–67.58) | 48.66 (30.05–70.59) | 75,259.5 | 75,213 | 143.5 / 144.5 |
| Download 1 MiB | 124.53 (0.23–162.29) | 112.00 (70.97–201.01) | 1,231,055.5 | 1,241,844 | 2,136.5 / 2,197.5 |

One baseline 1 MiB download sample completed only one iteration in about 4.64
seconds; its cause was not traced. It remains in the results. Queue drops, TCP
retransmission and host scheduling are possible contributors, not established
causes. No statistical-significance claim is made.

Raw measurements: [baseline](network-hardening-2026-10-21/baseline-bench.txt),
[hardened](network-hardening-2026-10-21/hardened/bench.txt), and
[machine-readable summary](network-hardening-2026-10-21/comparison.json).
These values describe this synthetic single-stream path. KVM, virtio device
processing, external links, DNS, multiple VMs and TAP/passt were not benchmarked.

## Sustained hostile traffic and recovery

Each phase lasts ten seconds after fixture setup. The malformed-frame scenario
submits five fixed invalid Ethernet shapes in batches of 128 per millisecond,
while probing an established TCP stream every 50 ms. This covers invalid frame
sizes, truncated IPv4, impossible IPv4 lengths and invalid ARP. It verifies that
the valid stream works both during the flood and after it ends.

The saturation scenario holds **256** forwarded TCP connections, repeatedly
attempts additional connections and requires rejection without a dial timeout.
An existing connection must continue echoing application bytes. It then closes
the holders, establishes and exercises a new connection, and requires
`Network.Close` to finish with that connection active. Writes after shutdown
must return `net.ErrClosed`.

| Run | Malformed frames submitted | Successful TCP probes during flood | Connections held / additional attempts rejected | Close latency |
| --- | ---: | ---: | ---: | ---: |
| Baseline | 1,167,360 | 200 | 256 / 58,535 | 2.005 ms |
| Hardened | 1,181,440 | 200 | 256 / 55,746 | 2.254 ms |
| Hardened with race detector | 1,138,560 | 172 | 256 / 8,115 | 1.777 ms |

All scenarios passed. The lower attempt/probe counts with the race detector are
recorded observations; the tests do not require a particular packet rate.

Periodic whole-process observations during the hardened runs:

| Scenario | Peak sampled Go heap | Peak sampled goroutines | Peak sampled file descriptors |
| --- | ---: | ---: | ---: |
| Malformed flood | 7,386,752 bytes | 23 | 11 |
| Saturated forwarding | 47,592,344 bytes | 787 | 1,031 |
| Malformed flood with race detector | 7,370,992 bytes | 22 | 11 |
| Saturated forwarding with race detector | 47,119,256 bytes | 786 | 1,031 |

These are diagnostic samples including both network stacks, host server, sockets
and test machinery. They are not per-worker production resource budgets or
continuous maxima; Go heap figures exclude native memory and race-detector
metadata. The tests assert observable traffic and shutdown behavior rather than
using machine-dependent memory thresholds.

Logs: [baseline](network-hardening-2026-10-21/baseline-stress.txt),
[hardened](network-hardening-2026-10-21/hardened/stress.txt), and
[hardened with race detector](network-hardening-2026-10-21/hardened/stress-race.txt).
DNS saturation, resolver socket bounds, TCP-client admission, cancellation and
fallback have separate regression coverage in
[`dns_limits_test.go`](../internal/usernet/dns_limits_test.go).

## Reproduce

Run the hardened measurements from the repository root:

```sh
scripts/usernet/run.sh /tmp/usernet-hardened
GOMAXPROCS=4 DEVBOX_USERNET_STRESS_DURATION=10s go test -race -tags=integration \
  ./internal/usernet \
  -run '^TestNetwork(MalformedFloodRecovery|ConnectionSaturationRecovery)$' \
  -count=1 -timeout=90s -v
```

To recreate the baseline using the same harness and toolchain, create a fresh
directory and copy only the test harness into the archived source:

```sh
mkdir /tmp/usernet-baseline-source
git archive 79a82e3df12d5f067d9d6b05e2316c21c98ce6a9 go.mod go.sum internal/usernet \
  | tar -x -C /tmp/usernet-baseline-source
cp internal/usernet/network_bench_test.go internal/usernet/network_stress_test.go \
  internal/usernet/network_test.go /tmp/usernet-baseline-source/internal/usernet/
USERNET_SOURCE_DIR=/tmp/usernet-baseline-source \
  scripts/usernet/run.sh /tmp/usernet-baseline-results
python3 scripts/usernet/summarize.py \
  /tmp/usernet-baseline-results/bench.txt /tmp/usernet-hardened/bench.txt
```

Keep the baseline and hardened Go toolchain identical. Run benchmarks serially on
an otherwise idle host. See the [runner documentation](../scripts/usernet/README.md)
for durations and repetition controls. The bounded stress cases are useful
regression checks, not exhaustive protocol fuzzing or evidence of a guest-escape
security boundary.
