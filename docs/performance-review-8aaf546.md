# DevBox-Gateway-kvm performance review

This is an archived review of the revision below, not current deployment
guidance. In particular, its loopback-worker transport description predates
gateway-only RDP over private Unix sockets; see the [current gateway guide](gateway.md).

**Reviewed:** `8aaf546d47e5c079b0a6ccc9a34ef35c88ffeb8d`, 2026-10-11, after the RDP password-validation changes.
**Workload:** several simultaneous office desktops, primarily 1920×1080. Reviewed gateway/authentication,
provisioning, graphics/encoding/RDP, block storage, networking, memory admission, VM scheduling and observability.

The earlier performance fixes are largely present and useful. The highest-priority remaining problems are
an inherited two-minute creation deadline, a shared-IP authentication budget that rejects valid office users,
and disk completions held behind later slow operations. Encoding remains a major density cost. NVENC roughly
halves measured CPU per changed frame on this host, but does not consistently improve throughput.

**This review does not establish a safe simultaneous desktop count.** Four default desktops require
18 GiB available at the initial memory-admission snapshot; this exceeds the test host's total RAM.
Neither encoder backend demonstrated four continuously scrolling 1080p desktops at 60 changed frames/s.
Guest applications, transport, client decoding and input-to-display latency were not included in those measurements.

Production code and running services were unchanged. New files contain only this review and its evidence.

## Evidence and method

[Raw results, experiment sources and reproduction commands](benchmarks/performance-8aaf546/README.md)
are preserved with [medians/ranges/sample counts](benchmarks/performance-8aaf546/summary.json) and
[benchstat output](benchmarks/performance-8aaf546/benchstat.txt).

Measurements ran serially on an i7-7700HQ, four physical cores/eight logical CPUs, Quadro M2200 with 4 GiB
VRAM and driver 580.178.04, Linux 7.0.0-38-generic, Go 1.26.9, `GOMAXPROCS=4`. The host reported 15.45 GiB
total RAM, about 4.97 GiB available and 2.56 GiB occupied swap. An existing gateway container used about
4.88 GiB. Existing guests, browser and development tools stayed running. CPU frequency was not fixed and
caches were not dropped. [Environment snapshot](benchmarks/performance-8aaf546/environment.json).

Most benchmarks have six samples; controlled disk and idle-device experiments have three. Encoder cases
use private state per session, one slice, a warmed initial IDR and 100 changed frames per encoder per run.
Forced NVENC initialization succeeded; fallback was not accepted. The two-frame synthetic widget/scroll
fixtures do not represent the full variety of office applications or their bandwidth requirements.

Findings distinguish **reproduced behavior**, **measured component cost**, and **code-confirmed risk with
unmeasured production impact**. All reported timings are medians unless stated otherwise. This turn ran
component benchmarks and targeted reproductions, not a fresh full regression suite or a multi-guest load test.

## Findings and priorities

### 1. High: the configured creation timeout is still capped at two minutes

**Reproduced through the actual HTTP routes with a fake creation backend.** With `VM_CREATE_TIMEOUT=30m`,
the backend received a context with **1m59.999841409s remaining**. The general request wrapper in
[`http.go:65`](../internal/gateway/http.go#L65) supplies a two-minute parent context and only exempts image
uploads. [`http_creation.go:87`](../internal/gateway/http_creation.go#L87) creates a longer child context,
which cannot extend its parent. Queueing for the creation semaphore also consumes this window.

Large non-reflink copies and queued morning provisioning can therefore fail despite the configured
30-minute limit. The previous fix extended creation write deadlines but missed this parent deadline.
The reproduction inspects the actual deadline immediately; it does not run a 30-minute clone.
[Evidence](benchmarks/performance-8aaf546/gateway-reproductions.txt).

**Fix next:** select the route-specific creation timeout before installing the parent request context,
while preserving caller cancellation and bounded progress writes. Add a route-level test with a configured
deadline exceeding two minutes, plus cancellation during queueing and copying.

### 2. High for an office behind one NAT: valid logins exhaust a shared ten-attempt budget

**Reproduced using real browser login handlers and the RDP authentication method with a fake authenticator.**
Six distinct users successfully signed in through the web from one IP; only **four of their six subsequent
correct RDP authentications succeeded**. The other two were rejected by the rate limit.
[Evidence](benchmarks/performance-8aaf546/gateway-reproductions.txt).

[`session.go:206`](../internal/gateway/session.go#L206) counts every attempt, including successes, against
ten attempts per minute per IP. Both [web login](../internal/gateway/http.go#L231) and
[RDP authentication](../internal/gateway/rdp.go#L100) spend that budget. Four users consume eight checks
before reconnects. It is hard-coded, independent of configured desktop capacity.

**Fix next:** retain fresh LDAP password validation and abuse protection, but separate account/failure
controls from a configurable aggregate IP burst budget appropriate for shared offices. Test valid mixed
web/RDP bursts, reconnects and failed-password floods. Merely removing rate limiting would lose protection.

### 3. Medium, potentially high during storage contention: completed reads wait for later flushes

**Reproduced with a controlled backing store.** A READ completed in the backend, then a later FLUSH was
held for 100 ms. The guest's used-ring index and READ status remained unpublished until that FLUSH returned.
Observed completion waits were 100.46–100.49 ms across three runs. This demonstrates added waiting; the
100 ms is imposed by the experiment, not a measured typical disk delay.
[Evidence](benchmarks/performance-8aaf546/disk-head-of-line.txt).

[`blk.go:269`](../internal/virtio/blk.go#L269) executes the complete pending batch before
[publishing any completion](../internal/virtio/blk.go#L302). A later slow sync therefore delays an already
finished foreground read. There is one request queue per block device, and QCOW2 serializes backend I/O
under an image mutex. These are per-device/image locks, not one cross-VM lock; shared storage contention
can nevertheless lengthen the blocking operations.

**Fix next:** publish safely completed requests before later potentially slow operations, preserving
FLUSH ordering, descriptor validation, writethrough durability and error reporting. Test READ→FLUSH,
write→READ→FLUSH, sync errors and interrupt visibility. Measure guest read/fsync p95/p99 before undertaking
a larger asynchronous or multiqueue redesign.

### 4. High density cost: small updates still require full-picture encoding

**Measured cost, with code-confirmed mechanism.** Dirty rectangles reduce RGB→I420 conversion, but each
changed frame still reaches the full-picture [codec call](../internal/rdp/avc/encoder.go#L189).
The [AVC target is 60 FPS](../internal/virtio/rdp.go#L626), adjusted for client feedback rather than a
shared host CPU budget. NVENC also [copies the complete I420 planes](../internal/rdp/avc/internal/nvenc/nvenc.c#L140)
for each submitted frame: calculated 3.11 MB at 1080p or 12.44 MB at 4K, before other copies.

Four-encoder CPU measurements show **10.39/17.37 ms CPU per widget/scroll frame in software**, versus
**5.16/9.09 ms with NVENC**. Hardware encoding frees CPU, but four-session scrolling throughput remained
similar. Full tables and qualifications follow below.

**Next experiment:** expose an office frame-rate ceiling and compare 30 versus 60 FPS with real clients,
including input latency and text quality. Preserve one software slice by default, damage tracking,
frame coalescing and ACK backpressure. Do not infer capacity from aggregate encoder FPS or raise thread
counts without measuring guest contention.

### 5. Medium: browser password hashing produces about 100,000 allocations per login

**Measured isolated guest-password derivation, excluding LDAP/TLS.**

| Password length | Time/hash | Go bytes/hash | Allocations/hash |
| --- | ---: | ---: | ---: |
| 12 bytes | 38.97 ms | 6.4007 MB | 100,009 |
| 72 bytes | 65.70 ms | 6.4009 MB | 100,011 |
| 1,024 bytes, accepted maximum | 404.37 ms | 6.4027 MB | 100,011 |

[Raw samples](benchmarks/performance-8aaf546/password-hash.txt). [`GuestPasswordHash`](../internal/gateway/auth.go#L376)
uses 100,000 SHA-512-crypt rounds. The pinned `github.com/tredoe/osutil@v1.5.0` implementation allocates the
64-byte digest using `C.Sum(nil)` each round (`user/crypt/sha512_crypt/sha512_crypt.go:142–166`), explaining
approximately 6.4 MB of short-lived allocations.

Every successful [browser login](../internal/gateway/http.go#L237), including reconnect-only logins, holds
one of eight shared authentication slots through hashing. RDP authentication uses the same slots and rejects
immediately when they are full. RDP password checks do not themselves derive this guest hash. Hashing is
not context-interruptible; cancellation is checked afterwards. Concurrent eight-login latency and LDAP
service latency were not measured.

**Next change:** eliminate avoidable per-round allocation while preserving hash format, salt handling,
round count and interoperability test vectors. Consider a separate bounded CPU budget for provisioning
hashes so they do not monopolize RDP authentication slots. Keep fresh password validation; do not cache
plaintext credentials or reduce the password work factor as a performance shortcut.

### 6. Medium, unmeasured duration: memory accounting holds the global manager lock

[`Manager.Start`](../internal/gateway/manager.go#L539) holds `m.mu` while
[`checkMemory`](../internal/gateway/manager_memory.go#L27) reads each worker's
[`smaps_rollup`](../internal/gateway/process_memory.go#L22) and host/cgroup accounting. Inventory, endpoint
lookup, other starts and child-exit accounting need this same lock. The earlier immutable VM snapshots
removed long per-VM lifecycle waits, but this separate global startup critical section remains.

**Next step:** measure lock hold times with realistically touched guest memory. Snapshot process identity
and reservations, perform accounting outside the general manager lock under admission-specific
serialization, then revalidate before reserving. Preserve conservative charging and exit/start race safety.

### 7. Medium, conditional: independently quiet proxy directions can disconnect a valid desktop

[`proxyConn.Read`](../internal/gateway/rdp.go#L309) installs a five-minute read deadline independently in
each direction. [`proxyRDP`](../internal/gateway/rdp.go#L321) closes both sides when either copy returns.
Traffic in the other direction cannot extend an already pending read. An unchanged screen and stationary
cursor generate no continuous worker output; there is no steady-state worker heartbeat. Automatic
reconnection is disabled in the generated RDP file.

This is a code-confirmed policy risk, **not a demonstrated five-minute native-client disconnection**.
Application clock updates or protocol exchanges may prevent it. A truly quiet direction can still end an
otherwise valid session and force another authentication, adding pressure to finding 2.

**Next test/change:** run real clients with quiet and one-way activity beyond five minutes. Align inactivity
with bidirectional session activity and authorization lifetime, or implement an appropriate protocol
keepalive while keeping bounded resource use and shutdown behavior.

## Encoder measurements and interpretation

The first series measured 1/2/4 encoders, forced software or NVENC. Throughput is aggregate; service time
includes scheduling during `EncodeDamage` but excludes dispatch queue wait. The p95 column is the median
of six per-run p95 values, not a pooled percentile or input-to-display latency.
[Samples](benchmarks/performance-8aaf546/encoders.txt).

| Backend / update | Encoders | Aggregate frames/s | Mean service ms/frame | Per-run p95 median, ms |
| --- | ---: | ---: | ---: | ---: |
| Software / widget | 1 | 131.95 | 7.57 | 8.68 |
| Software / widget | 2 | 225.25 | 8.57 | 12.44 |
| Software / widget | 4 | 305.45 | 11.78 | 15.35 |
| Software / scroll | 1 | 74.95 | 13.33 | 15.08 |
| Software / scroll | 2 | 127.05 | 15.34 | 21.94 |
| Software / scroll | 4 | 199.50 | 18.22 | 25.95 |
| NVENC / widget | 1 | 163.25 | 6.11 | 7.09 |
| NVENC / widget | 2 | 226.80 | 7.31 | 9.44 |
| NVENC / widget | 4 | 262.60 | 11.27 | 16.21 |
| NVENC / scroll | 1 | 163.70 | 6.10 | 7.74 |
| NVENC / scroll | 2 | 158.40 | 10.66 | 15.17 |
| NVENC / scroll | 4 | 213.60 | 14.68 | 20.52 |

A **separate series** adds process-wide user+system CPU accounting over four encoders. Its wall-clock
results differ, especially for widgets, illustrating shared-host variation. Keep these series separate.
[CPU samples](benchmarks/performance-8aaf546/encoder-cpu.txt).

| Backend / update | CPU ms/frame | Aggregate frames/s in this series | Go allocated bytes/frame | Projected CPU-seconds/s at 4×30 changed FPS | At 4×60 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Software / widget | 10.39 | 349.5 | 23,672 | 1.25 | 2.49 |
| Software / scroll | 17.37 | 211.0 | 27,344 | 2.08 | 4.17 |
| NVENC / widget | 5.16 | 257.0 | 525 | 0.62 | 1.24 |
| NVENC / scroll | 9.09 | 215.2 | 4,180 | 1.09 | 2.18 |

CPU projections multiply measured CPU/frame by requested changed frames/second. They are not physical-core
counts or paced capacity tests. CPU time includes all benchmark-process threads, runtime and dispatch
overhead, including native driver CPU; it excludes GPU engine time and guest/rendering/network/client work.
Go allocation accounting excludes native/GPU allocations. Four-session benchmark B/op is divided by four.

Every four-session scrolling sample was below the 240 aggregate FPS required for four 60-FPS streams.
Encoder-only results leave more room at 30 FPS, but do not prove four complete desktops are supportable.
Idle desktops do not continuously encode: these figures apply to continuously changing content.

The [software CPU profile](benchmarks/performance-8aaf546/encoder-cpu-top.txt) locates codec macroblock work,
RGB conversion and copying as major costs. It includes setup/calibration, so its percentages are not exact
steady-state-only shares. The [allocation profile](benchmarks/performance-8aaf546/encoder-alloc-top.txt)
estimates 480.90 MiB allocated across fixture construction, initialization, calibration and encoding
(pprof labels this binary unit MB); that is not RSS or the retained memory of four desktops. Initial IDRs
are warmed outside the timed loops; refresh/resize/retry IDR bursts were not measured. The separate profile
includes setup and warm IDRs. No sustained GPU utilization, thermal,
VRAM-capacity, image-quality or multi-monitor test was performed.

## RDP relay and LDAP setup costs

The gateway [terminates frontend TLS](../internal/gateway/rdp.go#L42) and opens a separate
[TLS connection to the pinned loopback worker](../internal/gateway/rdp.go#L221). Steady-state bytes are
decrypted and re-encrypted at the gateway. The relay uses two bounded `io.Copy` streams rather than
buffering complete frames. Explicit copy/dispatcher buffers account for approximately 68 KiB per connection
with the pinned implementation, excluding TLS state, stacks and socket buffers. This is calculated storage,
not measured RSS. Password validation is setup-only; the relay does not repeatedly query LDAP during traffic.

Every [LDAP authentication](../internal/gateway/auth.go#L160) opens a fresh TCP/TLS connection, binds the
user, searches identity/group attributes and closes the connection. There is no connection pool or TLS
client session cache. Fresh password/group checks must remain mandatory. Connection pooling or resumption
would need careful identity isolation and evidence that handshake latency matters.

The [RDP setup context](../internal/gateway/rdp.go#L60) has a fixed ten-second total deadline starting before
backend negotiation and MCS setup. Consequently, LDAP receives only the remaining time; setting
`LDAP_AUTH_TIMEOUT` above ten seconds cannot extend RDP setup. Authentication-slot admission also happens
after opening the backend connection. These are code-confirmed setup constraints, with unmeasured impact
under slow-directory or connection-burst conditions.

Existing correctness tests do not establish sustained relay throughput, TLS CPU/byte, concurrent connection
latency or real LDAP p95/p99. Measure these separately; the codec and transport-envelope benchmarks exclude
the gateway's encrypted relay. Keep backend certificate verification and fresh LDAP validation intact.

## Graphics, copying and retained memory

| Component / operation | Median | What this establishes |
| --- | ---: | --- |
| RGB→I420, full 1080p / 4K | 1.663 / 6.757 ms | Full conversion cost, zero Go allocations |
| RGB→I420, 64×32 damage at 1080p / 4K | 1.648 / 1.736 µs | Damage conversion already saves substantial work |
| Framebuffer publish + reader copy, full 1080p / 64×32 | 2.754 ms / 4.856 µs | Combined update cycle; 144 B and two allocations per cycle |
| GPU transfer, fragmented backing, full / browser rectangle at 1080p | 1.174 / 1.021 ms | Current transfer path, zero Go allocations |
| Same GPU transfer at 4K | 4.959 / 4.666 ms | 4K remains a substantial copy workload |
| Bitmap full-damage input, 1080p / 4K | 28.424 / 114.948 ms | Full conversion/comparison with one pixel changing |
| Bitmap 64×32 damage, 1080p / 4K | 29.959 / 33.650 µs | Small-damage path; not full-screen network throughput |
| Graphics envelope, 64 KiB / 256 KiB / 1 MiB payload | 14.38 / 55.54 / 270.63 µs | One allocation roughly payload-sized, excluding sockets/TLS |

Sources: [conversion](benchmarks/performance-8aaf546/conversion.txt),
[framebuffer/device](benchmarks/performance-8aaf546/virtio.txt),
[fragmented transfers](benchmarks/performance-8aaf546/gpu-transfer.txt),
[bitmap/transport](benchmarks/performance-8aaf546/rdp.txt).
The bitmap `Full` fixture changes one pixel and supplies full damage; it is not a fully changing display.
The legacy `GPUDesktop/FlushCursor` fixture includes a full framebuffer path and must not be interpreted
as the production native-cursor cost.

Additional code-confirmed opportunities, not measured live-desktop regressions:

- **Conditional bitmap retention:** after a bitmap frame, [two packed buffers](../internal/rdp/bitmap.go#L58)
  can remain when [AVC activates](../internal/virtio/rdp.go#L611). At 32bpp this is calculated 15.8 MiB/1080p
  or 63.3 MiB/4K per connection. Release/recreate them on mode transitions with fallback coverage.
- **Hidden animation:** minimized/disconnected handling suppresses encoding, but
  [guest GPU flushes](../internal/virtio/gpu.go#L886) still convert and publish pixels. Measure equal animations
  visible, minimized and disconnected. Deferred work must preserve guest `RESOURCE_FLUSH` boundaries.
- **Large-frame allocation:** [AVC wrapping](../internal/rdp/graphics.go#L559) and
  [ZGFX wrapping](../internal/rdp/dynamic.go#L354) each copy complete payloads. Optimize only after profiling
  IDR/resize bursts; the transport benchmark excludes the AVC wrapper, TLS and socket backpressure.
- **Bitmap fallback:** a genuinely complete uncompressed 1080p32bpp repaint contains approximately
  8.3 MB before framing. Verify AVC negotiation on actual clients; the synthetic bitmap timing does not
  establish available network capacity.

With one GPU resource and matching guest/client geometry, resource pixels, converted GPU RGBA, published
framebuffer, reader snapshot and retained I420 account for approximately **17.5 bytes/pixel**:
34.6 MiB at 1080p or 138.4 MiB at 4K. This calculated storage excludes codec/native workspace, conditional
bitmap buffers, extra GPU resources and guest RAM. The 256 MiB admission overhead is an allowance, not an
enforced limit; 4K and unusual images need observed peak RSS, not this floor alone.

## Storage and networking

Warm preallocated QCOW2 measurements show 4 KiB reads at **1.52 µs**, buffered overwrites at **2.34 µs**,
and write-plus-sync at **3.12 ms**. A 32-write virtio group measured **99.86 ms in writethrough** versus
**3.54 ms with negotiated writeback and a group flush**. These cases have different sync boundaries;
they do not justify discarding flushes or weakening durability. [Disk](benchmarks/performance-8aaf546/disk.txt),
[virtio group](benchmarks/performance-8aaf546/virtio.txt).

The QCOW2 read/write fixtures use a warm, preallocated 64 MiB working set and queue-depth-one overwrites.
The virtio group uses 32×4 KiB overwrites in a raw file; the blocked-FLUSH reproduction uses a fake backend.
These omit cold startup, sparse first writes, simultaneous saves and competing VMs. New QCOW2 clusters require
[allocation and metadata updates](../internal/disk/qcow2.go#L621); mapped zero clusters can require
[a full-cluster zero write](../internal/disk/qcow2.go#L832) before a small write. This first-write amplification
needs its own guest-visible measurement.

The [L2 table cache](../internal/disk/qcow2.go#L561) retains all loaded tables. A fully touched 20 GiB image
needs calculated L2 storage of about **2.5 MiB with 64 KiB clusters**, but **320 MiB with 512-byte clusters**,
excluding map overhead. Supported QCOW2 imports can [bypass normalization](../internal/gateway/images_qcow2.go#L109).
This is a conditional memory concern for small-cluster templates; normal 64 KiB templates are inexpensive.
Consider a bounded L2 cache if those imports are supported operationally. The separate refcount cache is bounded.

Networking has several remaining efficiency limits:

- RX descriptor assembly allocates **24 B/one allocation** for one segment, **72 B/two** for two, and
  **744 B/five** for 16 segments. It allocates even on empty reads: 24 B or 72 B in the tested cases.
  Median nonempty assembly/copy times were 168/243/724 ns; these fake-backend numbers are not network throughput.
  [Evidence](benchmarks/performance-8aaf546/net-rx.txt), [descriptor gathering](../internal/virtio/net.go#L251).
- Default userspace TX networking copies guest memory into the virtio packet buffer, then incoming-channel
  storage, then gVisor buffers. RX copies through `PacketBuffer.ToView`, the virtio RX buffer and guest memory.
  [TX enqueue](../internal/usernet/network.go#L233) allocates its copy before selecting the channel case,
  including when a full queue drops it. Buffer reuse requires explicit ownership rules.
- One RX/TX pair, 32-entry rings, no advertised network offloads or indirect/event-index support,
  and receive-side per-packet interrupt calls limit batching. Kicks/acknowledgements use userspace MMIO. Shared UEFI
  level IRQ handling already coalesces unchanged assertions: this is **not** one KVM IRQ syscall per packet.
  See [network features](../internal/virtio/net.go#L104), [queue size](../internal/virtio/common.go#L9),
  [modern features](../internal/virtio/modern.go#L179), [IRQ coalescing](../internal/machine/pci_irq.go#L33).

Prioritize real browser/download/QUIC profiles, drop/retransmission counters and guest softirq/VM-exit
measurements before implementing offloads or eventfd integration. The existing fake TX benchmark's
zero allocations validate its descriptor-copy path only. No end-to-end gVisor/TAP/network comparison was run.

## Memory admission, CPU fairness and provisioning capacity

Default desktops receive two vCPUs and 4,096 MiB RAM. Admission charges **4,352 MiB per desktop** including
overhead, plus **1,024 MiB host headroom**, and retains reservations until process exit.
[Defaults](../internal/config/config.go#L108), [admission](../internal/gateway/manager_memory.go#L10).

| Simultaneous default desktops | Required available memory at initial snapshot |
| --- | ---: |
| 2 | 9,728 MiB / 9.5 GiB |
| 4 | 18,432 MiB / 18 GiB |
| 8 | 35,840 MiB / 35 GiB |

These are minimum admission calculations, not total machine-size or performance recommendations.
Explicit memory budgets, cgroup limits and current pressure can lower capacity. Default maximum running
desktops is four; raising it does not create memory or CPU headroom. This host cannot admit four default
desktops from an idle group. Its current available-memory snapshot must not be used to count additional
VMs without accounting for already reserved/resident guests.

Guest RAM is demand-paged, aligned and THP-advised. Each vCPU goroutine is correctly locked to an OS
thread and uses scheduler-aware syscalls. Guest CPU count does not limit the worker's encoder/network CPU. Launch and
Compose do not set per-desktop CPU/I/O weights or quotas, so a busy desktop, two allowed clones and an
independent import can contend with foreground office work. Measure this interference before choosing
weights or admission thresholds.

Cgroup accounting conservatively subtracts `memory.current`, including file cache, and preserves an
initial group budget until all workers stop. This can reduce admission following large file activity;
production impact was not reproduced. Linux documents file-cache accounting in
[cgroup v2 memory controls](https://docs.kernel.org/admin-guide/cgroup-v2.html).
Go's memory limit excludes guest `mmap` and native/GPU allocations; a Go heap limit cannot replace total
process/cgroup capacity accounting. [Go GC guide](https://go.dev/doc/gc-guide).

## Prior fixes and lower-priority work

The [earlier review](performance-review-2026-10-11.md) remains a historical baseline. Current source
retains immutable inventory snapshots, direct owner-scoped RDP lookup, pinned template descriptors,
short catalog locks, sparse clone fallback, bounded creation concurrency and concurrent shutdown.
The parent creation deadline in finding 1 qualifies the earlier claim that timeout handling was fully fixed.

Graphics ACK state is no longer held across outbound transport I/O; input processing avoids the previously
reproduced ACK/write lock stall. Encoding waits for ACK capacity, the idle writer parks, and native cursor
updates avoid full framebuffer copies. GPU backing traversal advances across rows instead of restarting
at entry zero. The prior changes should be preserved.

Synthetic empty-device groups now measured median CPU of **0.012% baseline**, **0.383% for one group**,
and **0.480% for four groups**, expressed as a percentage of one logical CPU's time. Each group has six
workers in one Go process; these are not idle guest CPU estimates. They support the effectiveness of
the prior polling change. [Raw three-run samples](benchmarks/performance-8aaf546/idle.txt).
Optional audio still has 1 ms/10 ms workers, but managed gateway launch currently omits audio, so this
is not a default-desktop finding.

Visible dashboards poll health every two seconds and inventory every five seconds, suppressing hidden-tab
and overlapping work. Templates are reparsed per response and session pruning scans maps under a shared
mutex. At several desktops these are lower priority than the measured codec, disk and authentication work.

## Follow-up validation and observability

The first implementation batch should address findings 1–3 with behavior-focused tests. Follow with
password-hash allocation reduction and a configurable frame ceiling, each measured before and after.
Treat manager-lock restructuring, network offloads, asynchronous disks and rendering deferral as separate
changes supported by profiles and correctness tests.

To establish office capacity, run actual guest applications and native RDP clients for at least 15 minutes
at 1/2/4 desktops, with independently active users. Include typing, document scrolling, browser tabs,
autosave/fsync, downloads, idle/minimized sessions, reconnects and one concurrent provision/import.
Repeat software/NVENC at 30/60 FPS, then 4K and slow links separately. Keep image quality comparable.
Set an explicit latency target before calling a desktop count supported; none was provided for this review.

Collect input-to-display p50/p95/p99, delivered/encoded/dropped frames, ACK wait, encoder backend and
copy/convert/encode/write times; per-worker user/system CPU, RSS/anonymous/file memory, swap and pressure;
GPU engine/VRAM use; disk read/write/fsync p95/p99 and host queue latency; network retransmissions/drops,
VM exits and interrupts; login stage durations and rejection reasons; creation queue/copy/sync duration.

The worker has optional RDP stage statistics, but gateway launch does not expose them. Wire those into a
controlled diagnostic setting and add bounded aggregate metrics for authentication, admission and device
queues. Do not log passwords, tokens or packet payloads. This closes the largest current evidence gap:
relating component costs to the responsiveness users actually experience.
