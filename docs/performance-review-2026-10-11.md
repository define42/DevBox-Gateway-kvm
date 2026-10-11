# DevBox-Gateway-kvm performance review — 2026-10-11

**Implementation follow-up:** the six prioritized findings below have now been
addressed in the working tree. See the [fix validation and before/after results](benchmarks/performance-2026-10-11/after/README.md).
This report retains the original revision's findings and measurements as the baseline.

**Workload:** several simultaneous office desktops. **Reviewed revision:** `3decb2ff9b0684269eda0b1ebeabbe73414e5862`. Scope includes provisioning, VM device workers, graphics/RDP, encoding, storage, networking, memory admission, and operational visibility. Production code was not changed.

**Assessment:** prioritize responsiveness and fixed per-desktop overhead before increasing VM limits or encoder threads. Reproducible experiments expose provisioning locks that delay reconnects, graphics backpressure that blocks input, and sparse-copy amplification. Measurements also identify idle timer overhead and inefficient partial GPU transfers. The current implementation already has useful damage tracking, frame coalescing, bounded queues, explicit disk flush support, and conservative memory admission. Those should be preserved.

**Capacity remains unproven:** component benchmarks do not establish a safe number of office users. No simultaneous guest office workload or end-to-end input-to-display test was run. Four default desktops reserve 17 GiB before the additional 1 GiB host headroom; this exceeds a nominal 16 GiB machine even before other host applications. Encoder throughput alone cannot overcome that memory constraint.

## Method and evidence

Measurements ran sequentially on an Intel i7-7700HQ (4 cores/8 threads), NVIDIA Quadro M2200 (4 GiB, driver 580.178.04), Linux 7.0.0-38-generic, and ext4/NVMe storage. Go was pinned to 1.26.9 with `GOMAXPROCS=4`. The interactive host had approximately 15 GiB usable RAM, 9.9 GiB available, and 2.6 GiB swap occupied at the initial snapshot. Background desktop applications remained active. CPU frequency was not pinned and caches were not dropped.

Existing virtio benchmarks used six 500 ms samples; added GPU-transfer benchmarks used six 300 ms samples. Encoder cases used six samples of 100 changed frames per encoder, with 1/2/4 independent encoders and a warmed initial IDR. Hardware cases explicitly required NVENC and passed; no automatic software fallback was accepted. Idle-device tests measured three seconds per case, repeated three times. Separate profiles were used for attribution. Deterministic tests used disposable files, synthetic devices, and controlled transports; they did not modify guest images.

Results below distinguish **reproduced behavior**, **measured component cost**, and **static scaling concern**. Timings are medians unless stated otherwise. Benchmark ranges and raw samples are retained; noisy small differences are not treated as proven improvements. [Evidence, experiment sources, and reproduction instructions](benchmarks/performance-2026-10-11/README.md).

## Findings in recommended order

### 1. High — provisioning can delay reconnecting an unrelated running desktop

**Reproduced behavior.** [`Manager.CreateWithProgress`](../internal/gateway/manager.go#L386) holds the VM record lock throughout cloning, configuration, and sync. Start and Stop also hold the record lock over long operations. [`List`](../internal/gateway/manager.go#L338) waits for each owned record; administrative inventory waits across all users. [`RDP setup`](../internal/gateway/rdp.go#L59) lists all the user's VMs before obtaining the selected endpoint.

A test started one helper VM, paused creation of a second at its copy phase, and requested inventory with a 100 ms deadline. Inventory was still blocked at 101 ms and returned only after copying was released, with a nil error despite the expired context. Direct endpoint lookup for the already-running VM remained available. Thus reconnecting that VM through the usual setup path can wait for the other VM's clone. Already-established proxy streams do not take these locks.

There is also catalog-wide interference: creation retains the global image read lock; a pending deletion/publication writer then blocks subsequent readers, including requests for unrelated images. A second test reproduced the same expired-deadline wait for catalog reads. [Results](benchmarks/performance-2026-10-11/gateway-locks.txt).

**Change:** separate short, readable state snapshots from lifecycle-operation serialization. Use owner-scoped endpoint authorization without a full inventory scan. Pin/open the selected template under the catalog lock, then copy outside that global lock while preserving deletion and publication safety.

**Acceptance:** while a clone is deliberately paused, inventory and existing-desktop reconnects complete within their own deadlines; unrelated catalog operations remain available; duplicate lifecycle operations remain serialized.

### 2. High — graphics backpressure can stall keyboard input

**Reproduced behavior.** [`WriteAVC420Damage`](../internal/rdp/graphics.go#L181) holds `graphics.mu` during synchronous frame transport. The sole protocol reader needs that mutex to process a [frame acknowledgement](../internal/rdp/dynamic.go#L155), so it cannot read subsequent keyboard/mouse messages until the write releases it.

With an imposed 150 ms blocked graphics write, keyboard-only traffic arrived in 8.6–26.7 microseconds. When a valid frame ACK preceded the same keyboard packet, delivery took 150.38–150.66 ms, across three runs. This proves propagation of outbound backpressure into input handling; it does not estimate typical network latency. The 10-second transport write deadline is applied per batch, so it is not necessarily a total-frame bound. [Results](benchmarks/performance-2026-10-11/rdp-input-stall.txt).

**Change:** reserve frame state under a short lock, perform transport I/O outside it, and update acknowledgement/reference state with preserved ordering. Retain the acknowledgement-based frame window and its protocol semantics.

**Acceptance:** ACK followed by input remains responsive during a blocked writer; exercise cancellation, partial writes, concurrent close, and frame-reference ordering under the race detector and a real slow-link RDP session.

### 3. High — non-reflink provisioning expands sparse images and competes with live desktops

**Reproduced behavior plus static concurrency concern.** [`cloneFileWithProgress`](../internal/gateway/manager.go#L954) has a useful FICLONE fast path. Its [fallback](../internal/gateway/creation_progress.go#L42) reads and writes every logical byte without preserving holes. On the test filesystem, a 64 MiB sparse file occupying 8 KiB became a 64 MiB allocated destination; full SHA-256 equality was verified. This deliberately sparse fixture demonstrates the mechanism, not a typical 8192× expansion for all templates. [Results](benchmarks/performance-2026-10-11/provisioning.txt).

There is no host-wide limit on simultaneous desktop copies. `MaxRunning` limits starts, and template imports are single-flight, but independent users can clone concurrently. Full copies and final syncs can consume shared storage bandwidth, capacity, and page cache while office sessions perform latency-sensitive I/O.

**Change:** preserve sparse extents on fallback, check storage capacity, and bound concurrent provisioning with a context-aware queue outside record/catalog locks. Preserve data verification and final durability.

**Acceptance:** compare content and allocated blocks for sparse/raw/QCOW2 fixtures; test reflink failure and cancellation cleanup; measure active guests' p95/p99 disk latency while provisioning.

### 4. Medium — the general HTTP deadline can cancel a valid long clone

**Reproduced with a scaled deadline.** The server sets [`WriteTimeout: 2 * time.Minute`](../internal/gateway/server.go#L170). [`streamCreation`](../internal/gateway/http_creation.go#L19) synchronously flushes progress and cancels its backend context on a failed write/flush. Creation does not extend this deadline, although image uploads have a separate deadline policy.

A live HTTP test using the actual creation streamer, a 25 ms write timeout, and progress after 100 ms showed the backend context changing from healthy to canceled on the failed flush; the client saw an unexpected EOF. A 20 GiB non-reflink copy would cross two minutes below approximately 171 MiB/s per clone, excluding additional setup/sync time. That threshold is arithmetic, not a measured 20 GiB clone.

**Change:** define an explicit provisioning total deadline plus a progress/idle deadline, or use a background operation with reconnectable status. Simply removing all deadlines would lose useful bounds.

### 5. Medium — idle device polling consumes measurable CPU before guest work

**Measured synthetic cost.** A gateway UEFI VM has two block workers polling every 1 ms, a tablet worker every 1 ms, network TX every 10 ms, and GPU work every 16 ms: approximately 3,162.5 nominal timer firings per second per VM. This is not a measured kernel wakeup count; timers may coalesce or drop ticks. Sources: [block](../internal/virtio/blk.go#L189), [input](../internal/virtio/input.go#L283), [network](../internal/virtio/net.go#L282), [GPU](../internal/virtio/gpu.go#L296).

| Synthetic device groups | Median CPU, percentage of one core | Observed range |
|---|---:|---:|
| 0 | 0.022% | 0.011–0.028% |
| 1 | 17.117% | 13.717–19.355% |
| 4 | 27.631% | 25.168–28.285% |

These groups used empty, unnegotiated queues and fake backends, with six worker goroutines per group. There was no KVM execution, guest OS, real network stack, or RDP. All groups shared one Go runtime, unlike production's separate VM processes; extrapolating these numbers linearly to desktop capacity would be invalid. Setup, logging, and forced GC were outside the CPU measurement interval.

The separate CPU profile attributed 40.57% of sampled CPU directly to `runtime.futex` and 16.98% to the runtime syscall entry, with timer/scheduler work also present. This supports wakeup/scheduling overhead as a target; it is not proof of a specific achievable saving. [Measurements](benchmarks/performance-2026-10-11/idle-devices.txt), [profile](benchmarks/performance-2026-10-11/idle-cpu-profile.txt).

**Change:** start with event-driven idle workers and arm recovery polling only when work or interrupts require it. Preserve missed-kick and pending-interrupt recovery. Validate UEFI shared-level IRQs and legacy boot separately. Optional audio adds more polling but is not enabled by the gateway's current defaults.

KVM ioeventfd/irqfd integration could later reduce active notification overhead; neither is currently wired into the runtime. It is a separate project requiring BAR/reset/teardown handling and level-interrupt resampling, not a prerequisite for reducing idle timers. [Linux KVM API](https://docs.kernel.org/virt/kvm/api.html#kvm-ioeventfd).

### 6. Medium — partial GPU transfers repeatedly search preceding backing pages

**Measured component bottleneck.** The [partial-transfer loop](../internal/virtio/gpu.go#L766) calls `backingRead` for each row; [each call](../internal/virtio/gpu.go#L785) starts at backing entry zero. Full-origin, full-width transfers use a single walk. With fragmented 4 KiB backing, tall partial rectangles repeatedly traverse preceding pages.

| Resolution | Full transfer | Browser rectangle, fewer bytes |
|---|---:|---:|
| 1920×1080 | 1.326 ms | 1.773 ms |
| 3840×2160 | 5.538 ms | 10.88 ms |

The browser rectangle excludes 32 pixels at each horizontal edge and 128 total rows. Both paths allocate zero Go heap bytes per operation. The 4K full-transfer estimate was noisy (benchstat ±28%); the browser case was ±3%. In the separate 4K-browser profile, 72.05% of sampled CPU was directly in `GPU.backingRead`, with 27.67% in `memmove`. This strongly identifies backing traversal/copying as the target, without attributing every sample to rescanning. [Measurements](benchmarks/performance-2026-10-11/gpu-transfer-summary.txt), [profile](benchmarks/performance-2026-10-11/gpu-cpu-profile.txt).

**Change:** retain a monotonic scatter/gather cursor across rows or index logical backing offsets. Validate irregular entries, row strides, clipping, invalid guest addresses, and exact copied bytes. Repeat identical benchmarks before claiming a speedup.

## Encoding and desktop density

Dirty-region conversion is already useful, but the [encoder](../internal/rdp/avc/encoder.go#L189) still submits a full retained I420 picture to the codec. Unchanged macroblocks can be skipped, so a widget update is cheaper than scrolling; encode cost is not proportional only to dirty area. The default of one software slice per desktop avoids multiplying encoder workers across guests.

The following results include conversion plus encoding at 1080p. Each session had private fixtures/reference state, one slice, and alternating widget or document-scroll changes. Throughput is aggregate across all encoders. Mean service time measures calls including scheduling during each call; the p95 column is the median of six per-run p95 values, not a pooled percentile.

| Backend / workload | Encoders | Aggregate frames/s | Mean service ms/frame | Per-run p95 median, ms |
|---|---:|---:|---:|---:|
| Software / widget | 1 | 134.7 | 7.41 | 8.76 |
| Software / widget | 2 | 219.4 | 8.81 | 12.91 |
| Software / widget | 4 | 321.0 | 11.08 | 15.38 |
| Software / scroll | 1 | 75.6 | 13.21 | 15.55 |
| Software / scroll | 2 | 131.2 | 14.87 | 21.94 |
| Software / scroll | 4 | 206.6 | 17.73 | 25.77 |
| NVENC / widget | 1 | 163.8 | 6.09 | 6.92 |
| NVENC / widget | 2 | 226.1 | 7.33 | 9.51 |
| NVENC / widget | 4 | 259.6 | 11.42 | 16.40 |
| NVENC / scroll | 1 | 171.9 | 5.81 | 6.55 |
| NVENC / scroll | 2 | 154.0 | 11.11 | 15.31 |
| NVENC / scroll | 4 | 225.0 | 13.78 | 19.34 |

NVENC clearly improves the single-encoder scroll result on this GPU. It is not a universal throughput improvement: four software widget encoders outperformed NVENC here. The four-encoder scroll throughput difference is only about 9%, with overlapping run ranges; treat it as inconclusive for a general throughput claim. [All samples](benchmarks/performance-2026-10-11/concurrent-encoders.txt), [medians](benchmarks/performance-2026-10-11/encoder-medians.json).

Software allocated approximately 23.7 kB/widget frame and 27.3 kB/scroll frame (23,670 and 27,345 bytes); NVENC reported 524 and 4,177 bytes respectively. These are Go allocations, excluding native/GPU allocations, and do not establish retained memory savings. Widget output averaged 107 versus 243 bytes/frame; scroll output 3,695 versus 3,811. There was no matched visual-quality assessment, so output size is not a quality-equivalent compression comparison.

A separate four-software-encoder scroll profile showed 13.53% direct CPU in AVX2 color conversion, with major remaining costs spread across motion analysis, transforms, and copying. Allocation profiling includes fixture construction, encoder startup, and benchmark calibration; its total must not be interpreted as steady-state allocation rate or resident memory. [CPU](benchmarks/performance-2026-10-11/encoder-cpu-profile.txt), [allocations](benchmarks/performance-2026-10-11/encoder-allocation-profile.txt).

These short, unpaced component runs exclude guest rendering, GPU transfer, TLS, client decode, ACK/network delay, and sustained thermal behavior. Their aggregate FPS is not delivered desktop FPS. Keep one slice as the baseline; test an optional 30 FPS office policy against responsiveness and visual quality before adopting it. Do not increase every desktop to four encoder threads based on single-session results.

## Memory, storage, networking, and operational limits

**Memory admission is a strength, but overhead needs calibration.** Defaults are 2 vCPUs, 4 GiB guest RAM, and at most four running VMs. Admission adds 256 MiB per VM plus 1 GiB host headroom. It reserves untouched guest memory, credits anonymous PSS, observes ancestor cgroup limits, and retains reservations until process exit. Four guests require 17 GiB of reservations plus headroom. With the initial 9.9 GiB available snapshot, only roughly two new full reservations fit; that is admission arithmetic, not a tested two-user capacity guarantee. [Admission code](../internal/gateway/manager_memory.go#L10).

Guest RAM uses anonymous mmap with huge-page advice and sits outside the Go heap. Do not treat `GOMEMLIMIT` as a total VM-process memory cap; external mappings/native memory require separate accounting. [Mapping](../internal/machine/memory.go#L15), [Go GC guide](https://go.dev/doc/gc-guide).

For one same-size GPU resource and a connected AVC session, direct host-side graphics buffers total at least 17.5 bytes/pixel: resource backing, converted image, published framebuffer, reader snapshot, and I420. That is approximately 34.6 MiB at 1080p or 138.4 MiB at 4K, excluding codec workspace/references and other resources. Bitmap buffers retained after a switch to AVC can add another 15.8/63.3 MiB. The fixed 256 MiB admission allowance is not an enforced overhead cap. Measure per-child PSS and GPU memory through connect/resize/disconnect cycles. [GPU buffers](../internal/virtio/gpu.go#L625), [framebuffer](../internal/virtio/framebuffer_damage.go#L45), [bitmap](../internal/rdp/bitmap.go#L58).

Cgroup availability currently uses `memory.max - memory.current`; cache generated by cloning may cause conservative admission rejection even when some clean cache could be reclaimed. Process-memory sampling also runs over children under the manager lock. These are static scaling concerns, not measured bottlenecks. Measure cache composition and sampling latency before changing reservations; counting all cache as free would be unsafe. [Availability calculation](../internal/gateway/host_memory.go#L281), [cgroup memory accounting](https://docs.kernel.org/admin-guide/cgroup-v2.html).

**Storage has effective flush batching, with remaining concurrency gaps.** The 32-write benchmark measured 96.80 ms with 32 syncs versus 3.205 ms with one explicit group sync; both allocated zero Go heap bytes. These have different durability boundaries, so the result does not justify dropping requested flushes. [Results](benchmarks/performance-2026-10-11/virtio-summary.txt).

Each block device uses one synchronous request queue, and QCOW2 operations hold an image mutex over I/O. A slow sync can delay reads in that disk's batch. Existing [disk measurements](benchmarks/disk-2026-10-11.md) cover warm, preallocated 64 MiB, QD1, single-VM workloads with valuable readback/reopen/image checks. They do not cover multi-desktop interference, sparse first writes, cold cache, or production UEFI topology. New/zero QCOW2 clusters incur metadata work or full-cluster zeroing absent from overwrite tests. TRIM/discard is not advertised. Measure mixed read/flush p95/p99 before undertaking multiqueue or asynchronous I/O redesign. [Block queue](../internal/virtio/blk.go#L229), [QCOW2](../internal/disk/qcow2.go#L335).

**Network microbenchmarks omit the costly real paths.** The fake-backend TX benchmark is fast and allocation-free, but omits the userspace stack, RX, TCP/UDP relays, and guest scheduling. [`Network.Write`](../internal/usernet/network.go#L233) allocates/copies before a nonblocking send chooses its default, so even a dropped packet pays this cost. RX descriptor gathering also allocates. These are profiling candidates under real browser/download/conferencing traffic, not demonstrated throughput limits. Mapping counts and queues are bounded; 256 UDP relays could retain approximately 32 MiB in relay buffers, plus stack state. Preserve ownership and bounds if introducing packet reuse.

**Shared-office login bursts encounter intentional gates.** The source-IP limiter permits ten attempts/minute, including successful attempts; eight authentication slots reject excess concurrency. Multiple users behind one NAT/VPN therefore share a login budget. Retain anti-abuse controls, but measure rejection/latency and consider per-account failure limits combined with a source-IP ceiling and bounded admission. This is more relevant to larger login bursts than a few steady connected desktops. [Limiter](../internal/gateway/session.go#L196), [HTTP auth](../internal/gateway/http.go#L230).

**Observability needs a managed path.** Existing RDP statistics distinguish copy/convert/encode/write/ACK wait, but the gateway's child boot arguments do not expose their activation. Add controlled per-VM statistics plus provisioning phase time, queue depth, reservation/resident bytes, admission rejections, auth saturation, and disk/network tail latency. Keep identifiers bounded and exclude credentials/tokens. Session-store sweeps, repeated template parsing, and large-inventory scans are lower-priority optimization candidates for this workload.

## Preserve existing good behavior

Native cursor updates are separate from video; the legacy cursor microbenchmark is not a native-cursor cost estimate. RDP frame production parks when idle, coalesces damage, reuses conversion buffers, and normally limits frames in flight to two while acknowledgements are enabled (clients can suspend ACKs). Small framebuffer damage measured 4.919 microseconds versus 3.490 ms for full 1080p publication, showing why damage tracking matters. Full-frame timing was noisy; this is a component comparison, not an FPS promise.

Steady-state gateway RDP forwarding streams through two `io.Copy` directions without lifecycle locks or whole-frame buffering. Guest RAM is demand-paged, KVM execution is isolated per VM process, and the runtime releases scheduling capacity around blocking syscalls. Audio queues are bounded and producers do not wait on slow clients; audio is optional and was not included in default idle measurements. None of these areas justifies a broad rewrite based on this review.

## Validation needed before promising desktop capacity

Use the intended deployment host, real guest image, and real RDP clients. Start with 1/2/4 desktops at 1080p, adding higher counts only if memory and service objectives allow. Include disconnected idle, connected idle, typing, document scrolling, browser navigation, mixed downloads, simultaneous logins/reconnects, and one clone while other desktops work. Run software and supported hardware encoding separately; include a 4K case only if required.

After warm-up, run each sustained workload for at least 15 minutes and repeat independently. Measure input-to-display p50/p95/p99, delivered FPS and frame age, encode/write/ACK times, per-VM CPU and PSS, GPU utilization/memory, host memory pressure and swap activity, disk latency/queue depth, and network RTT/loss/throughput. Introduce controlled slow-client/backpressure cases. Set a user-experience target before deriving the supported desktop count; an example target to validate is p95 input-to-display below 100 ms on the intended LAN.

Implement lifecycle/input lock fixes first, then sparse bounded provisioning and deadline handling, then idle-worker and GPU traversal improvements in separate changes. Repeat identical component experiments and the simultaneous-office workload after each change. The report's sources and raw evidence are retained to make those comparisons reviewable; no runtime optimization or production capacity certification is claimed here.
