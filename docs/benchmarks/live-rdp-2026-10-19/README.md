# Live Compose / RDP performance validation — 2026-10-19

## Outcome

The managed VM used KVM and the RDP H.264 path used NVIDIA NVENC. During
Firefox scrolling, the largest identified host-side graphics cost was the
virtio GPU's full-frame BGRX/BGRA-to-RGBA conversion, not the H.264 encoder.
Firefox reported **WebRender (Software)** inside the guest. Bitmap delivery
was substantially more expensive than NVENC in the measured workload.

This is a live-workload diagnosis, not a maximum-FPS, WAN, or guest-storage
capacity certification. No performance implementation changes were made as
part of this validation.

## Environment and provenance

- Host: Intel Core i7-7700HQ, 4 physical cores / 8 logical CPUs, approximately
  15.4 GiB RAM, NVMe storage on ext4.
- GPU: NVIDIA Quadro M2200, 4 GiB, driver 580.178.04.
- Deployment: `compose.yaml` plus `compose.nvidia.yaml`, rebuilt and started
  with `GATEWAY_VM_PROFILING=true`; NVIDIA container runtime verified.
- Source: HEAD `a7a5125` plus the existing dirty working tree. This was not a
  pristine checkout. Deployed image ID:
  `sha256:dcb46a96b9c109a320d60632b9b9151bc099007e989a64fceb477551b98e79e5`.
- Profiled executable build ID: `bec6d7deaaf25064dfdc91ddc2be0f8e5e9c73fb`.
  Profiles were symbolized with a copy of that deployed executable.
- VM: isolated demo-account desktop `perf-validation`, image
  `rocky9-xfce-v2.4.16.qcow2`, 2 vCPUs, 4 GiB configured memory, office RDP
  profile, one viewer. KVM VM and two vCPU file descriptors were verified.
- Container had no CPU quota, CPU-set restriction, or memory limit; the
  worker could run on logical CPUs 0–7. Two guest vCPUs do not imply exclusive
  access to two host cores.
- Client: FreeRDP 3.32.1, local X11, loopback transport, TLS with a pinned
  certificate, clipboard sharing disabled. The client reported a verbose
  assertion build, which is a potential measurement caveat.
- Primary samples span 2026-10-18 21:50–22:01 UTC, or 23:50–00:01 CEST across
  the local midnight boundary.

## Method

Signed in through the normal gateway flow using the explicitly authorized
documented demo account. Created and started a separate VM, downloaded its
RDP connection file, logged into the graphical desktop with FreeRDP, and
opened Firefox's English Wikipedia Linux article.

Collected serial 30-second windows using `pidstat`, `mpstat`, `iostat`,
`vmstat`, NVIDIA utilization, and six authenticated `/perf` snapshots roughly
five seconds apart. CPU profiles were collected through the worker's private
Unix socket, not through a public profiling endpoint. A scrolling run without
CPU profiling followed the main profiled run.

The verified scrolling workload alternated 60 wheel-down and 60 wheel-up
events at 120 ms per event, repeated four times. Screenshots verified page
movement. This is approximately 8.3 wheel events per second, so observed
server update rates must **not** be interpreted as an FPS ceiling. Input,
host metrics, and cached telemetry were not atomically synchronized.

Compared NVENC at actual guest/output resolutions 1920×1080 and 1280×720,
plus bitmap at 1920×1080. Restored the guest to 1920×1080 afterward. A later
attempt using faster input did not produce verified page movement and had a
delayed capture start; all `fast-720` results are excluded. Initial page-load
data did not provide a reliable matched startup benchmark and is also
excluded from the comparison.

## Measured scrolling comparison

CPU values are 30-second `pidstat` averages; 100% represents one logical CPU.
Stage durations and payload rates are means of six interval averages, not
operation-weighted whole-run means. MB/s is decimal. These are individual
live runs, not a statistically controlled repeated benchmark series.

| Measurement | NVENC 1080p, unprofiled repeat | Bitmap 1080p, profiled | NVENC 720p, unprofiled |
| --- | ---: | ---: | ---: |
| Worker CPU, including guest execution | 290.0% | 348.6% | 253.6% |
| Worker guest-execution CPU | 188.1% | 183.8% | 183.9% |
| Worker host user + system CPU | 101.9% | 164.8% | 69.6% |
| FreeRDP client CPU | 78.4% | 73.4% | 37.7% |
| Server update batches/s, observed range | 8.8–10.4 | 3.0–4.4 | 8.4–9.4 |
| Virtio flush elapsed time, interval-average range | 44.2–50.1 ms | 52.2–58.8 ms | 19.2–24.3 ms |
| RDP conversion / bitmap packing | 8.0 ms | 179 ms | 3.2 ms |
| NVENC encode elapsed time | 8.0 ms | N/A | 3.7 ms |
| RDP payload throughput | 1.20 MB/s | 11.65 MB/s | 0.55 MB/s |
| Host idle CPU | 27.0% | 5.8% | 32.7% |
| NVMe utilization, average | 0.73% | 0.95% | 1.46% |

Bitmap conversion includes packing/comparison work and is not equivalent to
the H.264 encoding metric. Bitmap updates are successful nonempty update
batches, not individual tiles or displayed client FPS. Bitmap transport writes
averaged approximately 89 ms, versus 1.1–2.5 ms interval averages for the
NVENC 1080p repeat. Bitmap ACK timing was unavailable.

The 720p run reduced host graphics work and bandwidth, but did not establish
an FPS improvement: guest CPU remained close to the two-vCPU allocation and
the input workload was moderate. Bitmap also ran with CPU profiling enabled
and substantially less host idle time, so these runs cannot establish a
precise causal speedup ratio.

## Profile findings

### 1. Full-frame virtio conversion is the first host-side target

The profiled 1080p NVENC scrolling window accumulated 90.10 CPU-seconds over
30 seconds. `resourceImageRectInto` accounted for **21.57 cumulative
CPU-seconds**, equivalent to approximately **0.72 logical CPU** continuously.
Line attribution identifies the scalar BGRX/BGRA pixel conversion loop in
`internal/virtio/gpu.go:1009–1012`, including channel rearrangement and alpha
handling. The branch supports both BGRX and BGRA; the profile does not
distinguish those resource formats. Source-line attribution is approximate.

In that window `/perf` showed:

- 16.0–17.6 virtio flushes/s, each covering exactly 2,073,600 pixels, a full
  1920×1080 frame.
- Zero forced full refreshes and zero scanout switches: full-screen damage
  occurred with a stable scanout, not repeated resource/scanout recreation.
- 45.4–49.8 ms flush time, 3.6–3.9 ms transfer time, and 131–148 MB/s of
  transfer copy traffic.
- 3.76–4.09 ms capture, 7.29–8.55 ms RGBA-to-I420 conversion, and
  7.37–8.12 ms NVENC encode time.

The profile assigned 3.66 CPU-seconds to RGBA-to-I420 conversion and only
0.83 CPU-seconds to NVENC encoding calls. These CPU samples are not directly
comparable to elapsed per-frame encode durations, which include waiting.

`runtime.memmove` consumed another 5.47 CPU-seconds, including approximately
1.96 seconds in capture copies, 1.80 in backing reads, and 1.64 in framebuffer
publication. Parent profile totals overlap their children and must not be
added together.

### 2. Bitmap has a separate avoidable packing cost

The bitmap profile attributed **17.87 cumulative CPU-seconds** to
`BitmapEncoder.scaleRegion`. The source-coordinate calculation at
`internal/rdp/bitmap.go:134` accounted for 13.67 cumulative CPU-seconds even
though source and output dimensions were both 1920×1080. A same-size packing
fast path could avoid the generic per-pixel scaling divisions. This still
requires correct pixel-format conversion; it is not simply an RGBA memcpy.

Bitmap also paid the shared virtio conversion cost, approximately 18.27
CPU-seconds in its profile. Switching away from NVENC is not supported as a
performance remedy by these measurements.

### 3. Guest software rendering and CPU pressure remain important

Firefox `about:support` explicitly reported **WebRender (Software)**.
Guest execution consumed approximately 184–188% CPU during scrolling,
close to the two-vCPU allocation. NVENC accelerates the host video encoder,
not the guest browser compositor.

The host Go profile sees guest execution through KVM calls, not individual
Firefox or guest-kernel functions. It therefore cannot establish exactly
which guest functions consume that CPU or equate this time with virtualization
overhead. Guest-side profiling is still needed
before attributing all guest load to rendering or changing vCPU allocation.

### 4. Transport and pacing are not fully resolved

The profiled NVENC run reported 42.9–97.2 ms smoothed ACK turnaround and
0–23.25 ms/s blocked on a full ACK window. This does not show a persistent
full-window stall, but it does not exclude ordinary ACK-adapted frame pacing.
Current frame interval and client queue depth are not exported. Server sends
are not client presentation FPS, and no input-to-display latency was measured.

## Idle, memory, and storage observations

- Settled idle `/perf` CPU was approximately 4–5.5%, with almost no frames.
  The complete idle capture averaged 7.3% worker CPU including early residual
  activity; client CPU was 0.17%. No persistent idle redraw churn was observed.
- Guest `free` reported approximately 2.26 GiB available and no configured
  swap. The worker recorded no major page faults during the measured windows.
- Host swap was already allocated before testing, but there was negligible
  swap-in and no observed swap-out during the captures. Allocated swap alone
  is not evidence of current memory pressure.
- Active-workload NVMe utilization averaged less than 1.5%, peaking at 4.6%,
  with small queues. There was no evidence of a host-storage contention
  bottleneck during this browser workload. Worker-specific disk counters were
  unavailable; these storage observations are host-wide.
- Guest block latency, uncached/random IOPS, fsync/durability, and sustained
  guest-disk throughput were **not** benchmarked. These observations do not
  establish a general qcow2 or storage performance limit.

## Telemetry and operational checks

- Authenticated `/perf` returned HTTP 200 with `Cache-Control: no-store`,
  current-user scope, and only the demo user's VM.
- Live snapshots updated approximately every five seconds and reported the
  actual backend transitions NVENC → bitmap → NVENC and tested geometry.
- The profiling directory and socket had permissions 0700 and 0600.
  Anonymous `/perf` redirected with HTTP 303; the public
  `/debug/pprof/profile?seconds=1` path returned HTTP 404.
- After disconnecting the test client, telemetry correctly cleared the
  connection count, encoder, and outstanding ACK gauge.
- The test VM is retained for reproduction but stopped after validation.
  Gateway and LDAP Compose services remain running with profiling enabled.
  No unrelated VMs or orphan containers were removed.
- The gateway Stop action opened the guest's XFCE power confirmation dialog.
  Shutdown required reconnecting and confirming **Shut Down** in that dialog.
  The worker exited, `/perf` reported stopped, and the private profiling
  socket was removed. No forced termination was used.

## Recommended next changes

1. Optimize and benchmark the measured BGRX/BGRA-to-RGBA conversion loop, with
   row/vectorized fast paths where appropriate. Preserve alpha, stride,
   viewport, and partial-damage correctness; then repeat this live workload.
2. Add a same-size bitmap packing fast path avoiding generic scaling division.
   Retain NVENC as the preferred backend for this workload.
3. Investigate redundant full-frame copies and safe conversion coalescing.
   Preserve framebuffer publication ownership: later, unflushed guest writes
   must not become visible accidentally.
4. Export current pacing interval, client queue depth, and publication/input
   counters to distinguish upstream update frequency from delivery pacing.
5. Profile Firefox inside the guest. Treat reduced actual guest resolution
   as a measured CPU/bandwidth tradeoff, not a proven FPS fix. Do not assume
   adding vCPUs is free on this four-physical-core host sharing the RDP client.

## Evidence and limitations

Private raw artifacts are retained at `/tmp/devbox-live-perf-A2mY1oRQ`
(directory mode 0700), including the deployed binary, CPU profiles, host
metrics, `/perf` samples, screenshots, and capture scripts. This temporary
directory is not durable storage and may be cleaned by the OS. RDP connection
files and raw artifacts are not included in this report's repository changes.

The cumulative CPU-profile text summaries are preserved alongside this
report as [NVENC 1080p](nvenc-1080-cpu-cumulative.txt) and
[bitmap 1080p](bitmap-1080-cpu-cumulative.txt). They retain the executable
build ID, capture timestamp, sample totals, and function attribution.

Valid labels are `idle-1080`, `scroll-1080`, `scroll-1080-repeat`,
`bitmap-1080`, and `scroll-720`. The primary profiled NVENC capture began at
21:51:55 UTC; its unprofiled repeat at 21:53:41; bitmap at 21:56:00; and
720p at 22:00:17. Exclude `fast-720` and unmatched initial page-load data.

The client and VM shared the same host. Background host activity, SMT,
profiling overhead, and the client build can influence these measurements.
No separate-device/WAN validation, packet-loss testing, GPU passthrough,
multi-viewer load test, guest CPU flame graph, or end-to-end latency test was
performed. The evidence identifies actionable local graphics costs, not a
single proven explanation for every perceived delay.
