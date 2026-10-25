# Adaptive drag pacing and independent input metadata

Measured 2026-10-25 on an Intel Core i7-7700HQ, Linux amd64, Go 1.27.3,
GOMAXPROCS=8. FreeRDP 3.32.1 with H.264 support renders on a private Xvfb display.
The host also runs a VM; frequency scaling and unrelated host activity were not
controlled. Results characterize this run, not a universal CPU multiplier.

## Implementation

- Named profiles keep a 30-FPS base rate and permit 60 FPS during left-button
  movement. Release or 150 ms without drag motion ends the boost. Absolute and
  relative motion work; wheel pulses and stationary events do not renew it.
- Input notifies the pending writer immediately. Capture still coalesces pending
  damage, respects the ACK window, and does no periodic encoding on an idle screen.
- The codec retains one 60-FPS clock across pacing changes; switching rates does
  not recreate the encoder or request a keyframe.
- ACK congestion history is independent of the requested frame-rate floor,
  including pressure hidden under the slower base rate.
- Input sink and geometry use a short separate mutex. Keyboard and pointer
  dispatch no longer wait for framebuffer conversion. Pixel publication updates
  the geometry pair before notifying frame readers; callbacks run after unlocking.

## Deterministic capture wait

`TestRDPDragPublicationToCaptureWait` uses Go's fake clock, the real writer capture
gate and 120-Hz framebuffer publications. It measures each publication until the
next capture, including coalesced updates; it excludes encoding wall time, guest
rendering, transport, ACKs and physical presentation.

| Pacing | Captures per simulated second | Mean publication-to-capture wait | Maximum in fixture |
| --- | ---: | ---: | ---: |
| Normal | 30 | 16.67 ms | 29.17 ms |
| Active drag | 60 | 8.33 ms | 12.50 ms |

The lower wait is the intended scheduling benefit, not a measured improvement in
live input-to-display latency. Source publication rate and congestion can prevent
60 FPS; legacy linear-framebuffer polling retains its base cadence.

## Paced real-client workload

Six paired samples alternate 30/60 and 60/30 order. Each has 10 unmeasured warmup
frames and 120 measured frames, with the same 1280x720 window-drag fixture, QP20,
two slices and stable 60-FPS codec clock. Each iteration encodes and sends one
frame, waits for its ACK, then sleeps the remaining budget. Late frames begin a
new budget without catch-up bursts. This intentionally measures a paced codec
and client workload, not the production drag detector or its two-frame pipeline.

Medians from [raw samples](raw.txt) and [benchstat](benchstat.txt):

| Metric | 30-FPS target | 60-FPS target |
| --- | ---: | ---: |
| Achieved FPS | 29.44 | 56.60 |
| Mean frame work, excluding pacing sleep | 15.42 ms | 9.72 ms |
| Mean ACK turnaround | 8.16 ms | 5.22 ms |
| ACK p95 | 11.47 ms | 10.84 ms |
| Frames exceeding budget | 0% | 14.58% |
| Server CPU, percent of one core | 40.08% | 47.48% |
| FreeRDP CPU, percent of one core | 30.48% | 37.31% |
| Xvfb CPU, percent of one core | 10.12% | 12.88% |
| Combined CPU, percent of one core | 80.69% | 97.66% |

CPU percentages include every thread in the process; 100% means one full core.
Server CPU includes conversion, encoding, transport, ACK handling and harness
work, but excludes the guest VM. Startup, warmup and teardown are excluded.

Per-frame work and CPU varied substantially between samples and rates. Server
CPU utilization did not differ significantly in this small series (p=0.065).
Combined CPU was about 21% higher (p=0.026), while achieved throughput rose 92%.
These measurements do not establish that doubling the rate costs only 21% more
CPU: power management, scheduling, and shared-host activity were not isolated.
At equal per-frame cost, twice as many changed frames require twice the work.
The ACK p95 difference was also not significant (p=0.093).

## Validation and reproduction

Full repository tests passed in an isolated network namespace for TAP access.
Race tests passed for `internal/rdp` and `internal/virtio`; integration-tag lint
passed for the affected packages. Regression tests cover pending timer changes,
release/expiry, wheel and mode transitions, stable codecs, bitmap pacing,
congestion across repeated switches, and input delivery while capture is blocked.
The paced run acknowledged all 1,440 measured frames without reported decoder
failures.

```bash
# Deterministic scheduling and input-lock regressions:
go test ./internal/virtio -run '^Test(RDP(Interaction|Drag)|FramebufferInput)' -count=1 -v

# Build once, then run the binary serially to keep compilation outside samples:
go test -tags=integration -c -o /tmp/rdp-drag-pacing.test ./internal/rdp
DEVBOX_KVM_REQUIRE_ACK_TEST=1 GOMAXPROCS=8 /tmp/rdp-drag-pacing.test \
  -test.run='^$' -test.bench='^BenchmarkGraphicsDragPacingFreeRDP$/WindowDrag/slices=2/fps=30$' \
  -test.benchtime=120x -test.count=1
# Repeat for fps=60, six pairs, reversing order on alternate pairs.
benchstat -row .name -col /fps raw.txt
```

A live VM dragging check is still needed to measure guest input-to-display
latency on the user's normal resolution, client and server.
