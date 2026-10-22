INITIAL FIVE-MVC UNIQUE-CANDIDATE DIAGNOSTIC — SOURCE ONLY

No production implementation or performance experiment is included. Do not merge
these files. Main, round12, both frozen quarter-plan candidates and their measured
evidence are unchanged. No Go build/test/encode/profile/benchmark was run here.

Read-only proposal and exact source/safety/ceiling review:
  /tmp/h264-parity-target-20261022/remaining-motion-after-quarter-plan-review.txt
Native also evaluates these MVCs separately. This diagnostic determines whether a
new Go batch would have enough distinct work to justify an implementation; it does
not identify an existing native six-candidate kernel. InitialPoint's entire Road
CPU share is only6.28% and its PDF share1.28%; practical gains would be much lower.

Overlay directory: /tmp/h264-parity-target-20261022/initial-mvc-diagnostic-overlay
Baseline: /tmp/h264-parity-target-20261022/round11
Exactly three files:
- z_initial_mvc_diagnostic.go: counters and side-effect-free observation helpers.
- svc_motion_estimate.go: one record hook after original MVP clipping/reference
  offset calculation and before any SAD callback.
- welsEncoderExt.go: begin-frame and final-Uninitialize counter hooks.
Removing those three hook lines produces byte-identical frozen baseline files;
that assertion is repeated by the driver. All pixel/cost evaluation, callback,
MVD lookup, candidate comparison and output code stays in its original order.

The new observer records every initial call; other-block and non-five-MVC cases
short-circuit. Eligible16x16/five-MVC calls clip the same five vectors, combine them
with the original clipped MVP, and count1..6 unique MVs in first-occurrence order.
It reports total unique and duplicate slots, slots equal to MVP, and a histogram.
A second histogram counts conservative future-fast-route eligibility: known exact
built-in SAD callback, existing capability, nonnegative strides, visible encoded
and reference spans, and safe MVD indices. Each failed check increments only its
first exclusion reason. These labels do not change any codec decisions.

No coefficient, pixel or MVD table element is read by the observer. Extra indices
are bounded; malformed views only increment counters and return to original code,
which retains its existing panic operation/order. Integer reference offsets retain
the original int32 multiply/add before int conversion. The MVD-base-in-table check
is deliberately conservative for pathological offset/delta cancellation. Custom
callbacks are never called or bypassed by the observer. Its speculative histogram
is an entry snapshot; only the built-in preflight histogram is a batching claim.

Enable only H264_MVC_UNIQUE_DIAGNOSTIC=1. The driver runs one serial encoder with
one layer, one thread and one slice. Counters are non-atomic; owner checks are not a
concurrency guard. Unsupported configurations are not activated and produce no
valid616-frame record, so the driver fails instead of adding a codec panic.
Counters reset on the first accepted frame, accumulate through all616 frames and
emit one final JSON line at Uninitialize, prefixed H264_MVC_UNIQUE. They then reset.
No periodic logging is needed. All timing lines from this binary are invalid as
performance evidence and are never compared or converted to benchstat.

Prepared serial driver (not executed):
  python3 /tmp/h264-parity-target-20261022/measure_initial_mvc_diagnostic.py
It requires fresh initial-mvc-diagnostic-review output, snapshots the exact1010
frozen source files, builds a private AVC binary with these overlays and the
unchanged compare/fullscreen fixture overlays, then runs Road and PDF separately
on CPU2, GOMAXPROCS1,1920x1080,QP20,one slice,600 frames+16 warmup.

Each output stream must match the frozen uninstrumented round11 stream across all
616 encoded frames. Expected Road SHA256:
  fec3891d0f30674fd132b1cffade2b79b2ff6e9855574c5c233dcd2703e11d31
Expected PDF SHA256:
  ee3b0368c76e429a2be6a280c61038e0c50c76170c2c845f0fb2e80298eef9a3
The runner also requires the exact previous initial-call/eligible totals and checks
all histogram, duplicate and exclusion identities. Raw logs, streams, commands,
source/overlay/binary/input hashes and JSON summaries are retained. Only parity-
verified counts may inform a later implementation. Saved prior extra-SAD counts
are included for comparison, with no instrumentation timing claim.

SOURCE HASHES
6dac8e8fa6ed82574b7f609cdb486b84ce68bbcb18902f7fbd9cf28cdc1af21e  svc_motion_estimate.go
9ce2167fea96c73d8a5692f636a24e2b2b5ba35c4497c648873efd1e4e885cb5  welsEncoderExt.go
7d6c7ff5efe860a276b9352960db30557da40fc89a2afc2206f6fc2e72fe1b00  z_initial_mvc_diagnostic.go

Overlay manifest SHA256: 894e90faf56ef0821da8c9749a8642d469ae65589f53e6b25ae7504fcc2443d7
Driver SHA256: 87e7c1a04ad131ec90c607b1d527042d2b059bce0792f080317992c14bd8b103
