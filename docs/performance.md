# Performance diagnostics with `/perf`

`GET /perf` returns a read-only JSON snapshot of the signed-in user's managed
desktops. It reads the existing in-memory telemetry; it does not benchmark the
machine, start profiling, change settings, restart a VM, or read guest contents.
No extra Docker port, GPU utility, or guest agent is required.

## Collect a report

1. After deploying a build that includes this endpoint, sign in to the gateway
   as the desktop's owner. Administrators also see only their own desktops here.
2. Connect over RDP and reproduce the slow action, such as scrolling a browser.
   Allow at least ten seconds after starting the VM for two telemetry samples.
3. Select **Download diagnostics** on the dashboard, or open
   `https://localhost:8443/perf?download=1` (substitute your gateway address).
4. For intermittent problems, save several reports at least five seconds apart,
   including one while idle and several during the slow action. Include your RDP
   client's name, display resolution, and what you were doing when sharing them.

The download is `devbox-performance.json`. You can attach it to a troubleshooting
conversation for analysis. The URL alone does not grant an assistant access:
requests still require your existing browser session. Never share a session
cookie, password, or RDP routing token.

Deploying a rebuilt gateway interrupts managed desktops. Save guest work before
redeploying. Keep the base and cgroup Compose files:

```sh
docker compose -f compose.yaml -f compose.cgroup.yaml up --build -d gateway
```

Start the desktop again
from the dashboard after deployment if needed. This document does not imply that
the running container has been upgraded automatically.

## HTTP contract

| Request | Result |
| --- | --- |
| `GET /perf` | Pretty-printed JSON for up to 64 owned VMs, sorted by ID. |
| `GET /perf?vm=<id>` | One owned VM; unknown and foreign IDs both return 404. |
| `GET /perf?download=1` | Same JSON with an attachment filename. |
| `GET /perf?vm=<id>&download=1` | Download a report for one owned VM. |

Anonymous, expired, and revoked sessions redirect to `/login`. All responses
inherit `Cache-Control: no-store` and the gateway's security headers. A query
parameter such as `owner` cannot change the authenticated identity. There is no
anonymous debug token, cross-owner admin mode, public pprof route, or server-side report
archive. The report excludes usernames, VM names, image paths, credentials, raw
errors, process arguments, and private addresses. It still contains VM IDs,
timestamps, configuration, and usage data: review these before sharing.

Schema version 1 contains `version`, `generatedAt`, `scope` (`current-user`),
`sampleIntervalSeconds`, `truncated`, `vms`, and explanatory `notes`. An empty
inventory is `vms: []`. Each entry contains `id`, inventory `state`, and
`performance` (the same schema as `GET /vms/{id}/performance`). If `truncated`
is true, request a specific VM by ID. Inventory and individual telemetry samples
are not an atomic snapshot; a VM deleted during collection is omitted. Other
backend errors fail the request instead of returning a misleading partial report.

## Interpret measurements

| Field in `performance` | Meaning |
| --- | --- |
| `state`, `sampledAt` | Telemetry availability and timestamp. Only `live` represents fresh running-worker data; `stale` retains old values, not current measurements. |
| `vcpus`, `guestMemoryBytes` | Configured VM resources, not utilization. |
| `requestedEncoder` | Fixed compatibility label `software` for the bundled Go H.264 encoder. This is not a configuration option. |
| `rdpProfile` | Fixed compatibility label `smooth`: 30 FPS normally, rising to 60 during left-button dragging when client feedback allows it. This is not a configuration option. |
| `encoder`, `connections` | Active RDP backend and viewer count. `software` is the bundled pure Go H.264 encoder, `bitmap` is the bitmap path, `mixed` means viewers use different backends. An empty encoder means no active encoder. |
| `cpuPercent` | Worker CPU including guest execution and encoding; 100% is one host CPU. It is not normalized by `vcpus`. |
| `residentBytes` | Host worker resident memory, not guest used/free RAM. |
| `sampleIntervalSeconds` | Actual interval used for this VM's rate calculations. The report-level field is the nominal five-second export period. |
| `framesSent`, `framesPerSecond` | Lifetime successful sends and recent send rate, aggregated across viewers; not client-displayed FPS. |
| `payloadBytes`, `payloadBytesPerSecond` | Lifetime graphics payload and interval throughput; excludes protocol/transport overhead and does not measure link capacity. |
| `network.sentBytes`, `network.receivedBytes` | Cumulative RDP transport bytes sent from VM → client and received from client → VM, across all connections since this VM process started. |
| `network.sentMbps`, `network.receivedMbps` | Recent RDP download and upload rates respectively, using the actual measured interval and decimal Mbps (1 Mbps = 1,000,000 bits/s). Null until two valid samples are available. |
| `encodeMs`, `writeMs` | Interval average H.264 codec service time and graphics transport write time, in milliseconds. Not percentiles. Capture/conversion is excluded from encoding; write time can include backpressure. |
| `ackMs` | Slowest current viewer's smoothed graphics acknowledgment turnaround, including client processing. Not pure network RTT or input-to-display delay; an old ACK value alone cannot rule out a stalled client. |
| `pipeline` | Additional graphics-stage measurements described below; null for stopped/unavailable/older workers. |

The additive `network` object contains `sentBytes`, `receivedBytes`, `sentMbps`,
and `receivedMbps`; older workers produce `network: null` rather than invented
zeros. Counts include negotiation, TLS records, and all RDP channels at the
worker's RDP endpoint. They exclude TCP/IP overhead and guest internet traffic
and are not an exact external network accounting measure. Unlike graphics
`payloadBytes`, these counters include transport and non-graphics traffic.

RDP totals persist across reconnects and reset when the VM process restarts.
There is no persistent traffic history. The Performance panel displays rates in
Mbps and formats byte totals using IEC units (B, KiB, MiB, GiB, TiB); JSON totals
remain byte counts. The initial sample provides totals while both rates are null.

### Graphics pipeline

The `performance.pipeline` object is additive to schema version 1. Older worker
snapshots remain supported, but produce `pipeline: null`, not invented zeros.
These measurements follow this path:

```text
Guest rendering → virtio transfer/flush → RDP capture/conversion → encoder → transport/ACK
```

| Field in `pipeline` | Meaning |
| --- | --- |
| `transferMs`, `transferBytesPerSecond` | Mean service time per validated virtio guest-to-host transfer and actual copied bytes/second. Missing/invalid guest backing bytes are excluded. |
| `flushMs`, `flushesPerSecond` | Mean time and rate of visible virtio publications. Includes resource-to-RGBA conversion and synchronous display publication, not H.264 encoding. |
| `flushedPixelsPerSecond` | Pixels converted/published per second, including repeated work on the same pixels. Not unique screen pixels or displayed FPS. |
| `fullRefreshesPerSecond`, `scanoutSwitchesPerSecond` | Forced image initialization and scanout resource/viewport changes. A full refresh here is initialization, not every flush whose damage covers the whole screen. |
| `captureMs` | Mean framebuffer capture/copy service time per RDP capture, including acquisition of the framebuffer lock. |
| `convertMs` | Mean colour conversion/scaling service time; bitmap clients instead contribute bitmap comparison/packing time. Aggregated across viewers/backends. |
| `ackBlockedMsPerSecond` | Summed writer-observed time with a pending frame blocked by a full ACK window, beyond ordinary pacing. An ongoing stall is included in each export even if no frames are sent. Can exceed 1000 ms/s with multiple viewers; not network RTT. A blocked pointer/transport write can delay observing a released ACK window and overcount that overlap. |
| `outstandingAcks` | Sum of latest writer-observed outstanding graphics frames across viewers. May lag while a writer is blocked inside a transport write. |
| `sourceWidth`, `sourceHeight`, `outputWidth`, `outputHeight` | Selected virtio scanout (or last captured source fallback) and last captured output geometry for a single viewer; zero means unknown or not exactly one viewer. |

All stage times are **wall-clock service-time averages**, not CPU time,
percentiles, or end-to-end latency. Different stages have different operation
counts: do not add their means to estimate frame latency. With two valid samples,
idle operation rates are zero and stage means without completed work are null.
Rates use the worker's actual measured interval, so a lost export does not inflate
them. Ordinary RDP pacing is not included in ACK-blocked time.

Null values mean unavailable, not zero. A first sample has counters but no rates;
stopped or unsupported workers do not invent measurements. Telemetry becomes
stale after 15 seconds without an update. Counters reset on worker restart.
Configuration may be empty for alternate backends that do not supply it.

Use reports as evidence, not automatic root-cause verdicts. Low FPS on an idle
desktop is expected. Slow encode time suggests codec work is worth investigating;
slow write/ACK time may involve transport backpressure or client processing.
Guest rendering, Go H.264 encoding, and client decoding each have their own
performance costs. The guest uses software rendering; the gateway's 2D display
and H.264 encoder run in Go.

These snapshots do **not** measure guest disk latency, guest GPU use, host-wide
contention, guest application call stacks, or client decoding/display
delays. Those require separate, targeted diagnostics. The endpoint intentionally
does not expose host-wide data to ordinary desktop users.

## Optional administrator CPU profiling

CPU profiling is separate from `/perf`. The native executable defaults it to
disabled; the supplied Compose configuration defaults `GATEWAY_VM_PROFILING` to
`true`. It uses a private Unix socket, never a TCP listener or the gateway's
public HTTP router. See the [configuration reference](configuration.md#storage-and-desktops)
for defaults and overrides.
Only the host operator or container service account with access to the private
VM state directory can connect. A web administrator session does not grant this
access. No heap, goroutine, command-line, trace, or arbitrary pprof endpoints are
registered. No profile is started by reading `/perf`.

To enable the facility explicitly, set this in the Compose project's `.env`, or
export it in the native gateway's environment:

```dotenv
GATEWAY_VM_PROFILING=true
```

After changing the setting, recreate the container or restart the native gateway,
**after saving guest work**, then start the desktop again. This is not a live
toggle for an already running worker.
The equivalent operator flag is `serve -vm-profiling`; a standalone worker accepts
`boot -profile-socket /absolute/private/directory/cpu.sock`. The directory must
already exist, belong to the worker's OS user, and have no group/other permissions.

Managed workers receive a unique `profile-*/cpu.sock` inside their private VM
directory. The socket is mode `0600` and its directory is `0700`. Existing socket
paths are never replaced. Normal shutdown removes the launch's socket/directory;
an abrupt host/container crash can leave an old, non-listening socket. A new
launch uses a new directory. Failure to initialize optional profiling does not
prevent the VM from starting.

### Capture on a Linux Docker host

The default runtime image has no shell or curl. Use **host** tools instead; do not
publish a debug port or install tools into the running container. The following
assumes the default `/var/lib/devbox-kvm` state mount. Adapt the mount destination if
your deployment overrides it. Use your desktop ID from `/perf`.

```sh
perf_container=$(docker compose -f compose.yaml -f compose.cgroup.yaml ps -q gateway)
perf_state=$(docker inspect "$perf_container" --format \
  '{{range .Mounts}}{{if eq .Destination "/var/lib/devbox-kvm"}}{{.Source}}{{end}}{{end}}')
perf_vm='YOUR_VM_ID'
test -n "$perf_state" && sudo find "$perf_state/vms/$perf_vm" \
  -mindepth 2 -maxdepth 2 -type s -name cpu.sock -printf '%T+ %P\n'
```

Copy the relative path
for the **current launch** from the output (normally the only socket; after a
crash, use the newest launch). Do not delete older sockets while a worker may
still own them. The relative socket path below also avoids Linux's short Unix
socket pathname limit for long host Docker-volume paths.

While scrolling in the guest, run a ten-second capture:

```sh
perf_socket='profile-REPLACE_WITH_CURRENT_LAUNCH/cpu.sock'
umask 077
sudo sh -c 'cd "$1" && exec curl --fail --silent --show-error --max-time 40 \
  --unix-socket "$2" "http://localhost/debug/pprof/profile?seconds=10"' \
  sh "$perf_state/vms/$perf_vm" "$perf_socket" > vm-cpu.pprof
```

The HTTP hostname is only a placeholder; curl connects to the Unix socket, not
TCP. Captures default to ten seconds, accept 1–30 seconds, allow one active
capture per worker, and cap the serialized output at 16 MiB. Cancellation stops
collection. Concurrent requests return 409; invalid durations return 400.
Profiling has overhead while recording, so use short, deliberate captures.

Copy the exact deployed executable and inspect the profile with Go installed on
the host:

```sh
docker cp "$perf_container:/usr/local/bin/devbox-kvm" ./devbox-kvm-profile-bin
go tool pprof -top ./devbox-kvm-profile-bin ./vm-cpu.pprof
go tool pprof -top -cum ./devbox-kvm-profile-bin ./vm-cpu.pprof
```

Go's [runtime profiler](https://pkg.go.dev/runtime/pprof#StartCPUProfile) samples
the worker process. It can identify host-side pixel conversion, copying, encoding,
allocation and device-emulation hotspots. It does **not** identify the guest
browser's functions, provide reliable guest-versus-host CPU accounting, measure
off-CPU waits, or explain client decoding delays. Kernel/native work may appear
with limited attribution. Pair a profile with `/perf` reports from the same
activity window, and compare an equal-duration idle capture with a scrolling one.

Profiles contain function names, source/binary paths and runtime details; review
them before sharing. Set `GATEWAY_VM_PROFILING=false` and recreate the gateway
during a planned restart to disable the facility. No profiles are retained
automatically by the server; files saved by the operator remain until removed.
