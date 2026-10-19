# Bounded disk-read concurrency and notifications

Baseline: `76a6323` (2026-10-19). Candidate: the accompanying disk/virtio changes.
These are host component measurements, not a guest disk benchmark or a claim
about browser/RDP responsiveness. Running containers and VM images were untouched.

## Behavior

- Consecutive independent reads can use up to four host workers per block device.
  Workers start lazily, reuse the bounded request snapshot, and exit on Close.
  Single requests keep the direct path. This is still one guest virtqueue, not
  virtio multiqueue; newly arriving work is collected after the current snapshot
  drains.
- Negotiated writeback reads publish as they finish, including out of order.
  Already-ready completions share a notification; batching never waits for a
  slow read. Writes, FLUSH and rejected requests remain ordering barriers.
- Writethrough writes still complete only after batch Sync. Sync errors, dirty
  state, failed IRQ retry, reset and close/drain semantics remain intact.
- Guest data/status aliases, including aliases of queue control memory, take
  the serial path. Descriptor fields are still snapshotted before backend I/O.
- QCOW2 warm reads share a read lock. Lazy L2 cache loading, writes, allocation,
  rollback, Sync and Close remain exclusive. No concurrent metadata allocation
  or asynchronous durability policy was introduced.
- Block completions honor `VIRTQ_AVAIL_F_NO_INTERRUPT` on native amd64/arm64,
  using explicit shared-memory fences. Pending queue/configuration interrupts
  are not cleared by suppression. `purego` and other architectures conservatively
  keep notifying. EVENT_IDX is not advertised. Kicks coalesce into one pending
  wakeup; the existing watchdog/recovery loop remains.

The suppression/publication rules follow
[Virtio 1.2 split-ring notifications](https://docs.oasis-open.org/virtio/virtio/v1.2/virtio-v1.2.html).
Actual KVM level assertions were already coalesced by the shared IRQ-line
manager; this patch does not claim one fewer KVM ioctl for every grouped read.

## Measurements

Intel Core i7-7700HQ, Linux/amd64, Go 1.26.8, `GOMAXPROCS=4`.
Ten pairs of precompiled binaries, 200 ms minimum per case, alternating
before→after and after→before. No other tests/builds/benchmarks were launched
during the comparison. Normal workstation services stayed running; caches were
not dropped, CPU frequency was not fixed, and this was not a dedicated lab host.

| Case | Before median | After median | Comparison |
| --- | ---: | ---: | --- |
| QCOW2 warm 4 KiB read, one caller | 5.999 µs | 5.936 µs | No significant change |
| QCOW2 warm 4 KiB reads, four callers | 7.279 µs/op | 2.178 µs/op | 70.08% less aggregate time/op |
| Virtio raw warm read, one request | 5.616 µs | 5.676 µs | No significant change |
| Virtio raw warm reads, eight-request batch | 45.08 µs | 68.59 µs | **52.13% slower** |
| Virtio read with imposed 1 ms delay, one request | 1.174 ms | 1.171 ms | No significant change |
| Virtio reads with imposed 1 ms delay, eight-request batch | 9.360 ms | 2.486 ms | 73.44% faster batch drain |

The parallel QCOW2 row measures aggregate throughput, not individual-request
latency. The delayed backend deliberately sleeps for 1 ms before each raw read;
it proves overlap under a blocked backend, **not actual SSD latency**. Scheduler
timing makes the observed delay longer than exactly 1 ms.

The cached raw batch is an important trade-off: worker/channel dispatch and
alias checks cost more than the tiny cached I/O saves. Its median penalty is
about 24 µs per eight-request batch. Do not interpret this patch as an all-workload
speedup. Single-request controls show no statistically significant regression.
All reported cases round to zero allocations/op; worker/channel setup is lazy
and amortized, not allocation-free initialization.

Raw results: [virtio before](virtio-before.txt), [virtio after](virtio-after.txt),
[disk before](disk-before.txt), [disk after](disk-after.txt).
Statistics: [virtio comparison](virtio-benchstat.txt),
[disk comparison](disk-benchstat.txt). Each significant timing comparison above
has p < 0.001 with ten samples per version; see the tables for intervals.

## Reproduce

The same benchmark fixtures must be present in both versions. They were added
before saving the baseline binaries; production changes followed afterward.

```sh
go test -c -o /tmp/virtio.test ./internal/virtio
go test -c -o /tmp/disk.test ./internal/disk
GOMAXPROCS=4 /tmp/virtio.test -test.run='^$' \
  -test.bench=BenchmarkBlkReadConcurrency -test.benchmem -test.benchtime=200ms -test.count=10
GOMAXPROCS=4 /tmp/disk.test -test.run='^$' \
  -test.bench='BenchmarkQCOW2(Read4K|ParallelRead4K)$' \
  -test.benchmem -test.benchtime=200ms -test.count=10
```

For an A/B comparison, compile both versions first and alternate their binaries
one repetition at a time, reversing the order on successive rounds. Use
`benchstat before.txt after.txt`. Benchmarks create disposable images. QCOW2
uses an allocated, warmed 64 MiB image; raw queue cases use 4 KiB per request.

An exploratory version created workers per batch and allocated request
snapshots. It was rejected after exposing single-request allocation overhead;
the final comparison above uses the reusable pool/snapshot implementation.

## Validation and limits

Regression coverage includes independent completion, worker bounds/refill,
write/flush/error barriers, writethrough durability, short I/O, ring wrap,
descriptor/guest-buffer aliases, cold metadata reads, read/write exclusion,
reset/close draining, notification suppression/reenabling, and failed IRQ retry.
Affected package tests, race tests, purego block tests, pinned golangci-lint
v2.12.2, vet and a full executable build are checked for this patch. ARM64 is
cross-compiled and its fences inspected, not executed on hardware.

No guest read/fsync p95/p99, cold physical-storage workload, power-loss test,
multi-VM density test, or end-to-end RDP test was run. Concurrent QCOW2 writes,
multiqueue, EVENT_IDX and admitting new requests while a snapshot is blocked
remain outside this change. The pre-existing ordinary block used-length
accounting also remains separate from this concurrency/notification patch.

To use the change, rebuild/recreate the gateway during a planned restart after
saving guest work. There is no new Compose setting, and existing containers were
not redeployed as part of this validation.
