# Software H.264 optimization round 4 — 2026-10-21

Implemented the five codec optimizations and direct AVC420/ZGFX framing. Encoded output is unchanged. Send/encode overlap remains deferred because no usable live write-time samples were available; the agreed prerequisite was evidence that socket time is significant.

## Changes

- Four-candidate SSE2 SAD shares source loads and bounds validation for all seven block sizes. Up/down/left/right order is unchanged. Narrow SAD reads now use exact four-byte loads.
- CAVLC word flushing is out of line, letting the common symbol write inline. Compiler diagnostics confirm all six production call sites inline with both Go 1.26.9 and Go 1.27.3. The original unsplit bit writer is preserved as an independent test oracle.
- Paired AVX2 inverse transforms reconstruct two adjacent 4×4 blocks together. Horizontal int16 wrapping and widened vertical arithmetic are preserved; unsupported CPUs and overlapping/negative-stride cases retain the existing fallback.
- AVX2 half-pixel filters process sixteen byte samples or eight signed second-pass samples together. Exact tails, halos, rounding, clipping and quarter-pixel composition are preserved. Small horizontal/vertical blocks dispatch directly to SSE2 after one span check.
- Fused SSSE3 quantization, zigzag, maxima and nonzero masks serve inter luma and completed chroma encoding. Zero clearing, AC exclusion, coefficient dropping and overlap semantics are unchanged. I16 and lazy skip paths retain their existing behavior.
- AVC frame metadata and ZGFX headers are generated directly into the reusable dynamic-channel transport batch. Both full-payload intermediate copies/allocations are removed. The encoded input stays caller-owned until the synchronous write returns. ACK credit, resize ordering and frame statistics keep their existing semantics.

The work also closes a real bounds-check gap discovered by the new tests: a Go slice expression can extend to capacity beyond its current length. SIMD block/halo checks now reject such shortened slices before entering assembly.

## Measurement method

Linux/amd64, Intel i7-7700HQ, Go 1.27.3, GOMAXPROCS=4. Baseline is commit `c5d6f239d3182f22ab3fdbf62f66b0285df1bef6` plus the already implemented parallel RGB conversion, not the older serial conversion path. The baseline snapshot matched all 272 entries of the previous conversion round's source manifest. Source and executable fingerprints accompany the results.

Six full-frame pairs alternate old/new order. Every benchmark/build/test ran serially relative to other agent CPU workloads. Normal desktop/browser activity remained on the host, so this is not an isolated performance machine; comparisons use fresh controls within this session rather than absolute times from earlier reports. QP, search behavior, GOP, slicing policy and software thread caps are unchanged.

The clip is the existing five-frame Cisco/OpenH264 fixture, upscaled to 1080p before timing and looped; one transition in five wraps. It is a narrow fixture, not a broad natural-video corpus. Dense motion uses moving pseudorandom pixels and is a stress case. The public encoder benchmark measures conversion plus encoding, not network or display latency. Four-slice cases request four slices directly and bypass the managed production CPU cap, as in the previous reports.

## Component measurements

Six samples per case; times are medians. See the accompanying raw files and benchstat output for all cases and confidence intervals.

| Operation | Before | After | Change |
|---|---:|---:|---:|
| Four-candidate SAD, 16×16, stride 32 | 74.96 ns | 39.40 ns | −47.44% |
| Four-candidate SAD, 16×16, stride 2048 | 98.81 ns | 53.16 ns | −46.20% |
| CAVLC dense residual block | 194.0 ns | 160.6 ns | −17.19% |
| Four-block inverse transform | 59.62 ns | 45.41 ns | −23.84% |
| Horizontal half-pixel, 16×16 | 124.10 ns | 89.12 ns | −28.18% |
| Vertical half-pixel, 16×16 | 154.8 ns | 108.0 ns | −30.22% |
| Center half-pixel, 16×16 | 525.5 ns | 324.9 ns | −38.18% |
| Signed second horizontal pass, width 16 | 325.3 ns | 153.8 ns | −52.74% |
| Quantize + scan, dense DC/AC | 84.13 ns | 52.04 ns | −38.14% |
| Quantize + scan, dense AC-only | 83.61 ns | 52.43 ns | −37.29% |
| Frame and transport preparation, 64 KiB | 22.744 µs | 2.464 µs | −89.17% |
| Frame and transport preparation, 256 KiB | 139.428 µs | 9.125 µs | −93.46% |
| Frame and transport preparation, 1 MiB | 518.91 µs | 34.96 µs | −93.26% |

All tabled improvements have p<0.05. The sparse CAVLC residual result is inconclusive (p=.065); the isolated bit writer improves 24–41%. Zero-only quantization is also inconclusive, with a higher candidate median; nonzero cases improve 36–53%. Small horizontal/vertical 8×8 filters have no significant difference after removing duplicated wrapper checks. One wide-stride 8×8 SAD result is inconclusive; the other measured shapes improve. No universal per-block speedup is claimed.

The framing benchmark uses a discard transport. It isolates framing CPU/allocation costs and does not measure socket throughput. Two allocations proportional to payload size become zero allocations per frame after the one-time 16 KiB batch allocation; the benchmark's tiny reported B/op amortizes that startup allocation. All new codec component paths allocate zero bytes per operation.

## Full-frame measurements

The first six-pair run produced these conversion-plus-encoding wall times. “Inconclusive” means the sample did not establish a change at p<0.05; lower medians alone are not speedup claims.

| 1080p workload | Before | After | Result |
|---|---:|---:|---|
| Clip, four slices, full damage | 21.62 ms | 20.19 ms | Inconclusive, p=.065 |
| Clip, four slices, nil damage | 21.89 ms | 22.07 ms | Inconclusive, p=.818 |
| Clip, one slice, full damage | 55.31 ms | 54.93 ms | Inconclusive, p=.818 |
| Dense, four slices | 47.18 ms | 44.95 ms | Inconclusive, p=.310 |
| Dense, one slice | 136.4 ms | 118.6 ms | −13.06%, p=.041 |
| Desktop scroll, four slices | 6.699 ms | 7.445 ms | Inconclusive, p=.394 |
| Desktop widget, four slices | 1.328 ms | 1.176 ms | Inconclusive, p=.394 |
| Desktop scroll, one slice | 13.06 ms | 12.64 ms | Inconclusive, p=.937 |
| Desktop widget, one slice | 1.673 ms | 1.806 ms | Inconclusive, p=.937 |

In that initial run, four-slice clip CPU time fell 6.31% (66.65→62.45 ms CPU/frame, p=.026), and its encoding stage fell 6.95% (20.49→19.07 ms, p=.026). Four-slice dense CPU time fell 8.38% (157.2→144.0 ms CPU/frame, p=.009). CPU time sums work across threads and is not elapsed frame time. P95 changes were inconclusive. Encoded sizes and encoder allocation counts were unchanged.

Because the clip's total-time result was borderline and desktop medians were noisy, a separate six-pair follow-up used 360 clip frames with four slices, 120 with one slice, 240 scroll frames and 960 widget frames per invocation. Those longer runs were still noisy:

| Follow-up workload | Before | After | Result |
|---|---:|---:|---|
| Clip, four slices, full damage | 19.94 ms | 18.19 ms | Inconclusive, p=.699 |
| Clip, one slice, full damage | 45.87 ms | 45.43 ms | Inconclusive, p=.589 |
| Desktop scroll, four slices | 8.997 ms | 9.204 ms | Inconclusive, p=1.000 |
| Desktop widget, one slice | 1.178 ms | 1.163 ms | Inconclusive, p=.240 |

The longer clip run also failed to confirm a statistically clear CPU or encoding-stage change. Accordingly, the initial 6–8% CPU improvements are evidence from that run, not a reliably reproduced universal gain. There is no demonstrated RDP FPS or display-latency improvement here, and no statistically clear desktop regression. The strong results are the isolated kernels, framing CPU/allocation reduction, and unchanged output. A quieter host and representative live workloads are needed to quantify overall deployment gains.

See [clip](clip-benchstat.txt), [dense](dense-benchstat.txt), [desktop](desktop-benchstat.txt), and [longer follow-up](control-benchstat.txt) for the complete comparisons.


## Correctness and portability

- All `./pkg/h264/...`, `./internal/rdp/...` and `./internal/virtio` tests pass, including FFmpeg decoding and existing QP/resize/damage/slice checks.
- All eight 1080p stream digests and encoded byte counts match the preceding round: clip and dense, one/four slices, full/nil damage, 76 frames per stream. Existing small golden streams also cover QP 0/20/51, refreshes and retained output ownership.
- Randomized/extreme scalar parity, complete quantizer tables, independent CAVLC byte/state checks, exact spans, guard pages, aliasing, negative/short/zero strides and output canaries cover the new kernels.
- RDP byte-for-byte wire parity covers segment/fragment boundaries, empty parts, buffer reuse, maximum metadata and large payloads. Existing short-write, ACK, resize and close tests plus new transport-failure tests pass.
- RDP/AVC race tests pass. Native tests pass with AVX2 disabled, SSSE3 disabled, and all optional CPU features disabled.
- ARM64 cross-compilation of common, encoder and public AVC packages passes. This is a compile check, not ARM64 runtime testing.
- gopls diagnostics are empty; pinned golangci-lint 2.12.2 with Go 1.26.9 reports zero issues.

## Send/encode overlap

Accessible live logs did not provide current RDP write-time samples. Absence of samples does not establish cheap writes or an inactive session. Existing write-stage telemetry includes framing, lock waits and socket writes, so it should be measured again after the copy reduction.

Overlap is intentionally not enabled or hidden behind an unvalidated queue. A safe implementation still needs ACK reservation before encoding, generation-aware cancellation/recovery, one ordered pending access unit, sender completions returned to the statistics owner, and draining/invalidating work around resize. The encoded output already survives the next encode, so a future overlap does not require cloning the payload.

## Reproduction

`measure.py` records six alternating full-frame pairs against the preserved prior binary; `measure-control.py` records the longer follow-up. `components.py` normalizes old/new component benchmark labels and runs benchstat. Build the public full-screen benchmark with the existing overlay harness in `../software-h264-fullscreen-2026-10-21/`; it is deliberately archived separately from normal tests.

```sh
go test ./pkg/h264/... ./internal/rdp/... ./internal/virtio -count=1
go test -race ./internal/rdp/... -count=1
GODEBUG=cpu.avx2=off go test ./pkg/h264/internal/common ./pkg/h264/internal/encoder ./internal/rdp/avc
GODEBUG=cpu.ssse3=off go test ./pkg/h264/internal/common ./pkg/h264/internal/encoder ./internal/rdp/avc
GODEBUG=cpu.all=off go test ./pkg/h264/internal/common ./pkg/h264/internal/encoder ./internal/rdp/avc
go test ./pkg/h264/internal/common -run '^$' -bench 'Benchmark(SampleSadFourBatched|McAVX2)' -benchtime=250ms -count=6 -cpu=4 -benchmem
go test ./pkg/h264/internal/encoder -run '^$' -bench 'Benchmark(IDctPairedKernel|QuantScanFour4x4|CavlcBitWriter)$' -benchtime=250ms -count=6 -cpu=4 -benchmem
go test ./internal/rdp -run '^$' -bench '^BenchmarkGraphicsFrameStream$' -benchtime=250ms -count=6 -cpu=4 -benchmem
```
