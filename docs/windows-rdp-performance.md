# Windows RDP performance baseline

The benchmark runs the production Go RDP implementation and Microsoft's RDP
ActiveX client on one Windows runner. A deterministic 1920 × 1080 scrolling
document and moving window exercise H.264 and bitmap output separately at a
target of 30 frames per second. It measures the selected commit without a
gateway deployment, credentials, Linux host, or KVM guest.

**The first live Windows performance baseline is pending.** The existing
[compatibility test](windows-rdp-ci.md) has passed, but its two-frame-per-second
fixture does not establish performance under this workload.

## Run and compare

The **Microsoft RDP performance baseline** job runs alongside the compatibility
test on pushes to `main`, pull requests targeting `main`, and the nightly CI
schedule. It uses the separate
[Windows performance workflow](../.github/workflows/rdp-windows-performance.yml).
To run it manually, select that workflow in Actions, choose a Windows runner,
and select `both`, `h264`, or `bitmap`.

Each graphics mode starts a fresh loopback server and Microsoft client. The
client waits for a valid rendered marker, warms up for five seconds, then
measures for at least 30 seconds. It sends repeated F8 presses and verifies
the corresponding counter change in the rendered image, aiming for six probes
five seconds apart. A complete result requires at least three responses and a
response to every submitted probe. F8 changes only the fixture's input counter;
no keys are sent to a deployed desktop.

An Actions job summary and downloadable JSON provide the baseline. Compare
the same graphics mode, workload, source resolution, capture size, runner
image, client version, and measurement scope across commits. Hosted runner
load varies, so several comparable runs are more useful than one result.

There are no FPS, CPU, bandwidth, or latency regression thresholds yet. The
job still fails for incomplete evidence, incorrect rendering or input,
unexpected disconnection, infrastructure failure, or failed cleanup. A green
job means a valid measurement completed, not that a performance target was met.
Functional deadlines still apply: an input probe must finish within ten
seconds, and completion requires visible progress within the last five seconds.

## Measurements

| Measurement | Interpretation |
| --- | --- |
| Sampled visible updates per second | Distinct frame IDs observed in screenshots during the client measurement window. This is sampled progress, not exact presentation FPS. |
| Unobserved sequence numbers | Server sequence numbers skipped between valid captures. Frames may have appeared between captures; this is not a dropped-frame count. |
| Visible gaps and stalls | Time between observed frame changes. Gaps of at least 250 ms are counted for reporting, without failing on that threshold. Capture scheduling can contribute to a gap. |
| Input response time | Time from submitting each F8 through Microsoft's client to observing its counter change on screen. Includes server pacing, transport, client processing, and screenshot sampling. |
| Capture time and sample interval | Time spent taking screenshots and actual intervals between samples. These expose observation overhead and scheduling limits. |
| Server frame, conversion, encoding, and write times | Service times and distributions for the generated workload. Encoding is unavailable for bitmap mode. These are not client display latency. |
| RDP traffic | Bytes sent and received at the server transport, including TLS and protocol traffic; excludes TCP/IP overhead. |
| Process CPU and resident memory | Server and client sampled separately. 100% CPU means one logical core. Client CPU also includes the harness and screenshot sampling. |

Each report states its measurement scope. Client observations exclude its
five-second warmup. Server service-time and transport aggregates include the
session startup and warmup where specified; process samples span the
orchestrator's sampling window. Do not combine these into a single latency or
assume their rate denominators are identical.

The server's `skippedTicks` counts explicit skips caused by output suppression
or pending acknowledgements. It does not count ticks discarded while encoding
or writing is busy, so it is not a count of all missed target frames.

The remote desktop is always encoded at 1920 × 1080. When the hosted runner's
display is smaller, the client uses smart sizing to fit the visible window;
the captured dimensions are included in the report. This measures full-resolution
server output with scaled client presentation, not native 1080p presentation.
The client requests a 16 ms screenshot interval, but actual intervals are
measured rather than assumed. Visual markers use high-contrast cells, an
inverse row, and a checksum to reject incomplete or invalid captures.

## Artifacts

Each mode uploads JSON evidence and selected screenshots, retained for seven
days, including:

- `benchmark.json`: combined client, server, and process measurements with run
  outcome and measurement scope.
- `client-performance.json`: visible progress, input samples and latency
  summaries, capture overhead, and source/capture dimensions.
- `process-performance.json`: sampled server/client CPU and resident memory.
- `server.json`: encoder, frame counters, transport bytes, service-time
  distributions, and build metadata.
- `client-events.json`, `windows-events.json`, `progress.json`, and
  `result.json`: connection events, execution stage, and cleanup evidence.

Failure artifacts may contain partial measurements. Check the overall outcome
and completeness fields before treating them as a comparable baseline. The
benchmark does not upload credentials, routing tokens, private guest images,
or its temporary private state directory.

## Scope

The benchmark shares the compatibility harness's fixed IPv4 loopback listener,
temporary certificate trust, child-process deadlines, and cleanup. It uses
the production RDP encoders with a generated framebuffer. It does not exercise
KVM, guest applications, virtio capture, gateway authentication, WAN conditions,
multiple concurrent users, or a physical display's presentation timing.

For a deployed desktop, collect [performance diagnostics](performance.md)
while reproducing the workload on the actual Windows computer. The CI baseline
helps detect protocol/encoder regressions; deployment diagnostics identify
costs and delays outside this isolated test.
