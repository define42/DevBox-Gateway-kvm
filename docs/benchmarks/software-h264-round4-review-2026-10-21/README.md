# Software H.264: remaining acceleration opportunities

Review of `c5d6f239d3182f22ab3fdbf62f66b0285df1bef6`, after all three optimization
passes. This review adds profiles and recommendations only. No encoder code,
quality settings, search policy or CPU allocation changed.

## Fresh profile evidence

The current production sources match the final round-3 benchmark binary's
source manifest. That binary was profiled serially on the same Intel i7-7700HQ,
Linux amd64, Go 1.27.3, with GOMAXPROCS=4, 1080p, QP20 and full-screen damage.
The clip cases each ran 300 timed frames; dense motion ran 120. See
[build provenance](build-info.txt), [source manifest](sources-after.sha256),
and the saved `*-run.txt`, `*-top.txt`, `*-cum.txt` and `*.pprof` files.

| Path | Clip, 1 slice | Clip, 4 slices | Dense, 1 slice |
|---|---:|---:|---:|
| Residual writing, cumulative | 14.85% | 15.53% | 47.02% |
| CAVLC bit writer, cumulative (included above) | 1.55% | 1.69% | 13.88% |
| CAVLC run/level extraction, flat (included above) | 3.49% | 3.77% | 10.77% |
| Fractional motion refinement, cumulative | 18.55% | 21.10% | 14.72% |
| Four half-pixel filter kernels, combined flat | 8.20% | 8.67% | 7.20% |
| Four-candidate integer SAD, cumulative | 3.90% | 5.57% | 1.88% |
| Single-block inverse-transform kernel, flat | 3.63% | 3.10% | 2.33% |
| Fast 4x4 intra selection, cumulative | 4.77% | 6.02% | 6.29% |
| RGBA-to-I420 kernel, flat | 3.43% | 3.83% | 1.43% |

The four filter kernels are `mcHorVer20SSE2`, `mcHorVer02SSE2`,
`mcVerticalRawSSE2` and `mcHorizontalRawToU8SSE2`. Cumulative entries overlap
their callees and other rows. Percentages are shares of sampled CPU, not
predicted speedups or fractions of four-slice wall time.

These profiles include setup and warmup, although the benchmark's ns/op
excludes them. Profile-run times are not a new before/after performance result.
The clip still contains only five source frames, scaled from 720p and looped;
dense motion is moving pseudorandom data. Neither is a broad video corpus.
Capture, RDP framing, socket writes and client display are outside these runs.
See the [round-3 methodology](../software-h264-round3-2026-10-21/README.md)
for fixture details and the last controlled performance comparison.

## Recommended experiments that can preserve output

**Additional priority for four-slice latency: parallel RGB-to-I420 conversion.**
This item is now implemented; see the [implementation and measurements](../software-h264-parallel-conversion-2026-10-21/README.md).
Conversion is serial before slice encoding starts. Its small aggregate CPU
share understates its contribution to wall time: the controlled round-3
four-slice medians were 1.848 ms conversion within 21.53 ms/frame, about 8.6%.
The fresh profile run records 1.746 ms within 19.509 ms/frame, about 8.9%,
although that single profiled run is not a controlled timing comparison.
Ideal four-way scaling of the controlled conversion time would save about
1.39 ms, or 6.4% of total frame time, before scheduling, cache and bandwidth
costs. This is a theoretical estimate, not a measured improvement.

[The existing converter](../../../internal/rdp/avc/encoder.go#L300) already
operates on independent pairs of output rows. Partition large output regions
into disjoint bands with even Y boundaries, call the existing conversion
helper with the full image and global output coordinates, then OR worker-local
change flags after joining all workers. This preserves each 2x2 chroma block
and the scaled path's mapping and private scratch. Conversion workers finish
before codec workers start, so reusing the same concurrency budget does not
double simultaneous worker demand. Run one band on the caller and cap the
others by the effective budget and available row pairs. Keep small updates
and a one-worker budget serial. Overlapping damage rectangles must not be
processed concurrently; the first implementation can parallelize each large
rectangle internally while preserving the outer rectangle order. Explicit
full-screen damage must take the parallel path as well as nil damage.

Benchmark 1/2/4 conversion workers, complete four-slice encoding, scaled input,
small damage and p95 latency. Validate identical I420 bytes and change flags
(including repeated/alpha-only frames), stream hashes, scalar fallback and
race behavior. This is a good early experiment alongside batched SAD and the
CAVLC nonflush split; the earlier CPU-only ranking underweighted it.

1. **Reduce CAVLC writer overhead.**
   [The bit writer](../../../pkg/h264/internal/encoder/set_mb_syn_cavlc.go#L40)
   combines a small accumulator update with its larger 32-bit flush branch.
   It remains an actual call for each level and run. First split out the flush
   so the nonflush path can inline, and check the generated code. The saved
   source profiles show the cost on the actual level/run call sites.
   Preserve signed shifts, exact flush boundaries and the existing partial
   buffer writes without state commitment when a later level overflows.

   [Residual callers](../../../pkg/h264/internal/encoder/svc_set_mb_syn_cavlc.go#L292)
   also reduce already-known current-block nonzero counts to a boolean.
   A specialized internal writer could use the count to stop extraction after
   the final known coefficient and handle zero tokens without full writer
   setup. The present zero flag already skips coefficient extraction; do not
   claim that as new work. Counts must describe the current block, not the
   neighbor-derived entropy context. AC-only counts cannot serve DC blocks,
   zero counts must suppress stale level storage, and QP retries must refresh
   metadata. Keep the general writer contract for other callers.

   This is the largest remaining area for dense stress, but the bit-writer
   fast path alone addresses less than 2% of ordinary clip CPU. Benchmark
   sparse, dense, mixed/escape levels and complete frames separately. Do not
   repeat the rejected standalone mask extractor from round 3.

2. **Implement a genuinely batched four-candidate SAD kernel.**
   [sampleSadFourSSE2](../../../pkg/h264/internal/common/sad_four_simd.go#L5)
   calls four complete single-candidate functions. Each repeats source loads,
   bounds validation and the Go/assembly transition. One SSE2/AVX2 kernel can
   load each source row once and accumulate up/down/left/right costs together.
   Start with 16x16 and 8x8, then measure smaller shapes. Preserve candidate
   ordering, exact edge reads and supported stride behavior. This changes
   arithmetic batching, not search effort. It is a focused ordinary-video
   candidate with a clearer implementation scope than a search redesign.

   Separately, the out-of-range branch in
   [diamond search](../../../pkg/h264/internal/encoder/svc_motion_estimate.go#L254)
   repeats an unchanged position with `continue`. `break` can remove those
   redundant iterations; its likely contribution is small.

3. **Widen and specialize half-pixel filtering.**
   [Interpolation](../../../pkg/h264/internal/common/mc_simd_amd64.go#L115)
   remains SSE2. Its signed second pass handles only four output pixels per
   iteration in
   [mcHorizontalRawToU8SSE2](../../../pkg/h264/internal/common/mc_simd_amd64.s#L616).
   An AVX2 path could process eight widened intermediate values, while the
   first pass can process more byte input lanes. Specialize common 16/17-wide
   blocks and their exact tails. A later experiment can combine the two
   passes to reduce scratch traffic. Preserve signed intermediate values,
   final rounding/clipping and filter halo bounds; padded internal storage is
   not permission to overread public helper buffers. Quarter-pixel avg+SATD
   fusion is already implemented and is not a new proposal.

4. **Batch inverse reconstruction and fuse quantization with scanning.**
   [Four-block IDCT](../../../pkg/h264/internal/encoder/idct_simd_amd64.s#L12)
   still calls the single 4x4 kernel four times. Pairing independent blocks,
   as with the existing forward DCT, can fill more SIMD lanes and share setup.
   Preserve horizontal int16 wrapping before the int32 vertical transform,
   exact-width stores, and alias/stride fallbacks. DC-only chroma is a smaller
   companion experiment: the inter reconstruction path still runs full
   transforms when chroma CBP is one and no AC coefficients exist.

   [Inter luma quantization](../../../pkg/h264/internal/encoder/svc_encode_mb.go#L172)
   writes quantized coefficients and maxima, then performs up to four
   separate scan/mask calls. A combined 8x8 kernel could produce coefficients,
   scanned levels and four masks without those reloads and dispatch checks.
   Scan/mask costs 2.15% of one-slice clip CPU and quant-max another 1.21%;
   caller bookkeeping is additional but cannot all be attributed to this
   proposal. Both operations already use SIMD. Preserve zero/one/>one
   classification, AC exclusion, residual-drop thresholds and alias behavior.

5. **Fuse 4x4 intra prediction and SAD evaluation.**
   [WelsMdI4x4Fast](../../../pkg/h264/internal/encoder/svc_base_layer_md.go#L522)
   still writes a 16-byte prediction and calls the metric for each candidate.
   The production low-complexity path uses SAD here, despite using SATD for
   fractional motion refinement. Fuse the initial DC/H/V scores, retain H and
   V costs for directional branching, then consider directional predictor
   scoring without materializing every rejected candidate. Reconstruct the
   winner before moving to the next block. Preserve the DC/H/V tie order,
   predicted-mode penalty and neighbor availability. Merely populating the
   unused combined-SATD hook would not accelerate this fast path. The whole
   function's 4.8–6.3% includes reconstruction and is not all removable cost.

For the next implementation batch, start with row-band conversion,
four-candidate SAD and the CAVLC nonflush split, then paired IDCT. Measure each
independently before keeping it. Wider interpolation and fused quant/scan are
larger follow-ups.
Require unchanged stream hashes, decoder interoperability, SIMD/scalar parity
and edge/alias tests, followed by serial paired full-frame benchmarks on clip,
dense and desktop controls. Broaden the natural-video corpus before claiming
general full-screen playback gains.

## RDP throughput and explicit tradeoffs

- **Check effective parallelism first.**
  [Automatic slice selection](../../../internal/virtio/rdp_workers.go#L7)
  still defaults to one. Explicit requests are capped at
  `min(requested, max(1, cpus-max(1, guestCPUs)-1))`, with a further codec limit
  of four. GOMAXPROCS=4 and two guest CPUs permits only one; four-slice replay
  bypasses this managed policy. Requesting more slices on a host with spare
  CPU may improve latency, but spends CPU available to guests and changes
  slice boundaries/encoded bytes. Do not silently raise this policy.
- **Remove complete-payload framing copies when live telemetry supports it.**
  [avc420FrameDamage](../../../internal/rdp/graphics.go#L561) copies Annex-B
  into a new graphics frame, and
  [segmentGraphics](../../../internal/rdp/dynamic.go#L356) copies it again
  before the reusable transport batch. Reuse bounded buffers first, or stream
  the metadata and payload into the final batch. Transport batching already
  exists. These costs are outside the encoder profiles above.
  Current `stats.written` surrounds the whole `WriteAVC420Damage` call, so it
  includes framing and lock waits as well as socket writes. Inspect live
  write/encode averages, maxima and ACK wait before deciding whether to
  overlap stages; distinguish framing CPU from actual blocked I/O if needed.
- **Measure slice imbalance before changing scheduling.** Running one lane
  on the waiting caller can remove one goroutine launch, likely a small gain.
  Work stealing offers little with one fixed slice per worker. Adaptive slice
  boundaries or extra slices require evidence of imbalance and change coding
  decisions. The existing load-balancing flag forces the serial path.
- **Reduced search or higher QP are separate speed/quality choices.** The
  current encoder already uses low complexity. A more aggressive preset
  could reduce diamond-search or subpixel effort, but requires quality and
  bitrate comparisons. Higher QP changes visual quality and may reduce dense
  residual work. Neither should be presented as preserving output.
- **Do not introduce a queue of stale frames.** Capture is already gated by
  pacing and ACK credit, and damage is coalesced. An ordered one-frame overlap
  of encode and transport is only worthwhile if live socket time is material;
  it needs credit reservation and resize/refresh generation handling. Dropping
  encoded P-frames would break the reference chain for later frames.

No new PGO profile is proposed: the previous narrow clip-trained experiment
failed to help the clip and regressed dense motion. GC is not a leading cost
in these samples; serial RGBA conversion remains a worthwhile latency target
despite its small aggregate CPU share.
