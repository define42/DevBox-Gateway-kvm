# Host and guest disk benchmark

This isolated Linux/amd64 harness measures the same Go workload against a host
file and a guest virtio block device, backed by raw and QCOW2 images. The guest
uses one vCPU, 256 MiB RAM, an externally supplied kernel, and a minimal initramfs
built by the harness. Serial exists only in this benchmark runner; it does not
change product serial settings. No network, desktop, filesystem, or guest shell
is needed.

The driver creates a **new** result directory and disposable disks beneath it.
Choose a directory on the same host filesystem as the real VM disks. It never
opens an existing VM disk. Linux `/dev/kvm`, Go, Python 3, `cpio`, and `qemu-img`
must already be available. Run benchmarks sequentially with other VMs and heavy
disk workloads stopped.

Set `KERNEL` explicitly to a readable, compatible Linux kernel. Relative paths
are resolved against the caller's working directory before the harness enters
the repository root; the examples below use absolute paths.
The kernel needs built-in PCI, virtio PCI, virtio-block, and initramfs support;
the generated initramfs contains no driver modules. No kernel or kernel build
configuration is bundled. Missing or unreadable `KERNEL` inputs are rejected
before the result directory is created. Use the same kernel for baseline and
candidate measurements.

```sh
# Set GO to an installed Go binary if the default go command is unavailable.
KERNEL=/absolute/path/to/bzImage GO=/path/to/go OPS=4096 REPETITIONS=6 SIZE_MIB=64 \
  bash scripts/diskbench/run.sh /same/filesystem/diskbench-before

# Preserve the baseline runner BEFORE modifying production disk code.
CGO_ENABLED=0 go build -o /tmp/diskbench-before ./scripts/diskbench/runner
KERNEL=/absolute/path/to/bzImage RUNNER=/tmp/diskbench-before OPS=512 REPETITIONS=6 \
  bash scripts/diskbench/run.sh /same/filesystem/baseline

# Build the changed runtime and measure with identical settings.
KERNEL=/absolute/path/to/bzImage OPS=512 REPETITIONS=6 \
  bash scripts/diskbench/run.sh /same/filesystem/candidate

# A boot/data-integrity smoke test, not a performance measurement:
KERNEL=/absolute/path/to/bzImage OPS=8 REPETITIONS=1 SIZE_MIB=1 \
  bash scripts/diskbench/run.sh /same/filesystem/diskbench-smoke
```

Each repetition uses seeded random 4 KiB offsets at queue depth one. Disks are
fully populated with nonzero data before measurement; this excludes sparse and
QCOW2 zero-cluster shortcuts. QCOW2 uses 64 KiB clusters and is preallocated by
conversion, so the benchmark measures existing mappings, not image growth.

* `random_read`: the entire working set is read before timing random reads.
  Guest `O_DIRECT` bypasses the guest cache, while gokvm's backend uses the host
  page cache. `host-buffered` is the warmed cache comparison; `host-direct`
  additionally shows filesystem direct I/O. These are explicitly different cache
  paths: guest direct reads are **not** host device direct reads. No global host
  cache dropping or privileged tuning is performed.
* `random_write_sync1`: each write is followed by `File.Sync` within its latency
  interval. Completion includes the requested durability boundary.
* `random_write_sync32`: 32 sequential writes share one `File.Sync`. The final
  partial group is also synchronized within timing. IOPS and mean latency are
  amortized per write; individual p99 latency includes the synchronizing writes
  and does not represent each write's time to durability. Durability occurs at
  the group boundary. This remains queue depth one, not 32 concurrent requests.

Each write phase uses a different nonzero payload; all write offsets are read
back after timing. After each VM closes, the driver
checks QCOW2 metadata, decodes it, and compares all logical bytes with both host
results and the raw guest disk. This checks content and reopen consistency;
it does not simulate power loss or prove a physical drive honors flushes.

The default run can take several minutes on slow synchronous storage. Settings
are `GO`, `KERNEL`, `RUNNER`, `OPS`, `REPETITIONS`, `SIZE_MIB`, `SEED`, `TIMEOUT`
(per guest; default `10m`), and `KEEP_DISKS=1` (default removes scratch disks).
The result directory retains `results.jsonl`, `summary.md`, complete guest serial
logs, initrds, binaries, kernel/binary hashes, runtime diff, host/environment
metadata, QCOW2 validation, and logical disk hashes. JSONL records include per-run
IOPS, average latency, p50/p95/p99/max latency, queue depth, cache mode, and seed.
When `RUNNER` selects a previously built binary, its SHA-256 identifies the
measured implementation; `runtime.diff` and Git metadata describe the current
source tree, which may differ from that saved binary. Record the saved runner's
original commit separately when comparing changes. The initial measurement for
this harness saved its baseline runner from commit `46a2dcf` before runtime edits.

Compare repeated runs, not a single sample. The summary reports medians and the
range of per-run mean latency; it does not claim statistical significance.
Run baseline and candidate serially under the same host conditions, preferably
alternating their order across repeated invocations. Do not interpret a faster
buffered path as better physical storage throughput.
