# Software H.264 video: next optimization targets — 2026-10-21

This is a review of the completed two-pass implementation, using fresh CPU
profiles of the [full-screen replay](../software-h264-fullscreen-2026-10-21/README.md).
The most promising remaining work is motion-cost batching, intra prediction,
and coefficient processing. These are measured hotspots, not measured speedups
for the proposed changes. No production code or configuration was changed.

## Evidence and scope

The current production sources match all 380 Go/assembly files in the preserved
candidate tree, plus go.mod/go.sum. Profiles use its existing test binary on an
Intel i7-7700HQ, Linux amd64, Go 1.27.3, GOMAXPROCS=4, software H.264, QP20,
1080p, whole-screen damage. Three runs were serialized: 300 video frames with
one slice, 300 with four slices, and 120 dense-texture frames with one slice.
The profile covers setup and 16 warmup frames as well as the timed loop.

The video input is only five original 720p frames upscaled before measurement
and repeated. The dense input is moving pseudorandom texture, not natural
video. Thus these profiles guide work on the observed paths; they do not
represent every film, capture source, resolution or host. The earlier
unprofiled timings remain the timing reference. CPU profiling adds overhead.

| Target in one-slice profile | Video CPU share | Dense stress CPU share |
|---|---:|---:|
| Fractional-pixel motion refinement, cumulative | 22.35% | 16.79% |
| AVX2 8×8 SATD kernel, flat | 9.77% | 5.63% |
| 16×16 plane prediction, flat | 5.20% | 1.99% |
| CAVLC residual writer, cumulative | 9.34% | 42.00% |
| Nonzero coefficient count, flat | 2.96% | 1.94% |
| Coefficient residual score, flat | 2.10% | Not sampled |
| Four-block dequantization, flat | 2.34% | 2.09% |
| Forward 4×4 transform kernel, flat | 3.24% | 1.79% |

Cumulative entries include callees and overlap other entries; do not add them.
Even eliminating an entire target cannot save more than its share of sampled
CPU for that workload. Four-slice profiling shows the same leading kernels.
Its aggregate CPU is about three times wall time; that alone cannot identify
slice imbalance. Raw profiles and text summaries are included here.

## Output-preserving optimization candidates

1. **Batch SATD and fractional candidate costs.**
   [sample_simd.go](../../../pkg/h264/internal/encoder/sample_simd.go) computes
   16×16 SATD through four 8×8 calls. The AVX2 8×8 implementation separately
   transforms and reduces four 4×4 blocks. Dedicated 16×16, 16×8 and 8×16
   kernels, with simultaneous block processing, can reduce repeated entry,
   load and reduction work. A subsequent fused averaging-plus-SATD operation
   in [md.go](../../../pkg/h264/internal/encoder/md.go) can avoid materializing
   and rereading every quarter-pixel candidate. Preserve the sum of normalized
   4×4 costs, rounded averages, candidate order, strict ties, and the winning
   prediction buffer. Substituting a larger Hadamard transform changes decisions.

2. **Vectorize remaining intra prediction.**
   [get_intra_predictor.go](../../../pkg/h264/internal/encoder/get_intra_predictor.go)
   still generates plane predictors pixel by pixel; horizontal prediction is
   also scalar. Start with exact row recurrences and SIMD generation. Then
   combine prediction with SAD scoring where
   [WelsMdI16x16](../../../pkg/h264/internal/encoder/svc_base_layer_md.go)
   currently writes each candidate and rereads it for scoring. This applies
   to intra modes evaluated inside P-frames, not just keyframes. Preserve
   signed arithmetic, rounding, clipping, neighbor availability, signaling
   costs and tie order. Existing combined helpers are scalar; just enabling
   those hooks is not a proven optimization.

3. **Fuse coefficient metadata work and vectorize dequantization.**
   [svc_encode_mb.go](../../../pkg/h264/internal/encoder/svc_encode_mb.go)
   repeatedly quantizes, scans, scores and counts coefficients. The count,
   scoring and two scan functions together consume about 6.1% of video CPU.
   One nonzero mask can supply count and run-based scoring, and later assist
   CAVLC extraction. Preserve zigzag order, chroma/I16 DC handling, and the
   exact residual-dropping thresholds. Four-block dequantization remains
   scalar in [decode_mb_aux.go](../../../pkg/h264/internal/encoder/decode_mb_aux.go);
   SIMD low-word multiplication matches its explicit int16 truncation.
   A smaller first step is up-front length proofs or fixed-array views in
   [encode_mb_aux.go](../../../pkg/h264/internal/encoder/encode_mb_aux.go).
   [Saved disassembly](coefficient-objdump.txt) shows repeated bounds checks: nine per iteration in
   four-block dequantization, 26 across the 16 scan assignments, and one per
   iteration in nonzero counting. Retain bounds safety; do not disable checks.

4. **Optimize CAVLC coefficient extraction and common level coding.**
   [set_mb_syn_cavlc.go](../../../pkg/h264/internal/encoder/set_mb_syn_cavlc.go)
   scans backwards again to collect runs/levels. Reusing nonzero metadata can
   remove that repeated search. A bounded lookup for common level/suffix
   combinations is a further candidate: preserve first-level adjustment,
   suffix evolution, overflow behavior and the scalar fallback for uncommon
   levels. The source profile confirms time in those calculations, especially
   on dense residuals; a table's net benefit still needs measurement. The
   existing writer already buffers 32 bits, so basic buffering is not missing.
   Keep stack scratch from escaping through indirect function calls.

5. **Batch forward transforms and stop failed skip evaluation earlier.**
   [dct_simd_amd64.s](../../../pkg/h264/internal/encoder/dct_simd_amd64.s)
   invokes the single-block kernel four times for an 8×8 group. Process
   adjacent blocks together. Separately, luma skip evaluation currently
   transforms all four 8×8 groups before testing whether each can be skipped.
   Compute and test groups in order, returning on the first failure, with
   normal encoding recomputing the required data. This needs parity tests
   covering fallback, scratch state, quantization and group ordering.

The proposed changes above should retain the encoded output when implemented
correctly. Their gains cannot be added or predicted from hotspot percentages;
measure each independently, then the combined encoder, against byte-identical
streams and independent decoding.

## Confirmed forward-transform bounds defect

The current SSE2 forward transform loads eight bytes where the wrapper
validates a four-byte row. Source `MOVD` memory-to-XMM instructions at
`dct_simd_amd64.s:59/60/67/68/75/76/83/84` emit eight-byte `MOVQ`; the final
row can read four bytes beyond the valid input span. The same helper is used
by the four-block transform. Normal padded pictures hide this issue.

All eight isolated guard-page cases faulted: 4×4 and four-block 8×8 transforms,
tight and padded strides, and each input guarded independently. Scalar
execution passed first. Core dumps were disabled and faults were recovered.
Fix the exact-width load before expanding this assembly and retain the tests.
This is a correctness fix, not a claimed performance gain. Evidence:
[dct-guard-repro.txt](dct-guard-repro.txt),
[dct-objdump.txt](dct-objdump.txt), and
[regression test](dct_guard_repro_linux_amd64_test.go.txt). The test reuses the
existing `idctGuardedBuffer` test helper. It was run only in an isolated tree.

## Runtime choices and tradeoffs

The existing encoder already uses LOW_COMPLEXITY, one reference frame, CAVLC,
no deblocking or periodic IDR, and disabled adaptive/background detection.
There is no unused lower preset to switch on.

- Verify the live backend and effective slice count first. The managed cap in
  [rdp_workers.go](../../../internal/virtio/rdp_workers.go) reserves guest and
  service CPU capacity. With two guest CPUs, four available Go processors
  permit only one slice; four slices need at least seven under that policy.
  The prior four-slice replay bypassed the cap. Consider an explicit worker
  CPU-budget policy if more video throughput is required, with session-density
  measurements rather than automatically raising every session's worker count.
- QP24/28/32, fewer search candidates or reduced fractional refinement may
  reduce work, but change output, compression or quality. QP is already
  configurable. These need a separate time/CPU/bitrate/visual-quality matrix;
  no speed or quality claim for them is made here.
- Instrument per-slice elapsed time before changing partitioning. Fixed
  equal-macroblock slices can have unequal work, but usually each worker has
  only one job, so work stealing alone cannot split the slowest slice.
  Adaptive boundaries alter prediction and bitstreams. Simply enabling
  `BUseLoadBalancing` disables the current parallel fast path and is not a fix.
- Further output ownership, frame-copy and GC redesign is lower priority:
  video `memmove` is 1.1% and malloc/GC do not appear in the top 50. Existing
  RDP stage stats should still be used to separate encode cost from capture,
  writes, pacing and client acknowledgments in live sessions.

## Profile-guided build experiment

This particular profile-guided build is **not recommended**. It used the
one-slice video profile for compilation and was compared with the unchanged
standard binary in six serial alternating pairs. Times below are medians of
six samples:

| Workload, one slice | Standard ms/frame | PGO ms/frame | Result |
|---|---:|---:|---|
| Video clip | 66.483 | 65.456 | Inconclusive, p=0.093 |
| Dense stress | 142.473 | 148.942 | **4.54% longer**, p=0.004 |

These are fresh unprofiled measurements; compare paired versions within this
run, rather than their absolute times against an earlier run. Both tested
76-frame streams remain byte-identical to the standard build, as recorded in
[pgo-parity.txt](pgo-parity.txt). This rules out adopting this particular narrow
training profile as a shortcut; it does not rule out PGO trained on a broader,
representative workload later.

## Reproduction and verification

The [earlier full-screen report](../software-h264-fullscreen-2026-10-21/README.md)
contains fixture preparation and the benchmark harness. `profile.py` recreates
these serialized CPU captures with the preserved current binary and fixture.
Inspect with `go tool pprof -top BINARY PROFILE` or `-list=FUNCTION`.

The PGO binary was built from the unchanged isolated candidate using:

```sh
go test -c -pgo=/tmp/h264-video-next-20261021/clip-1.cpu.pprof \
  -o /tmp/h264-video-next-20261021/pgo.test ./internal/rdp/avc
```

`measure-pgo.py` runs six serial alternating pairs using unprofiled binaries.
Clip uses 60 timed frames per sample; dense uses 16, both after 16 warmup
frames. The PGO training profile contains the same short clip used for its
clip comparison; dense is a separate control absent from training. Thus even
a positive result would require broader profile and playback coverage before
adopting PGO for releases. Build hashes, profile summaries, raw timings and
parity evidence are included. No production implementation was changed.
