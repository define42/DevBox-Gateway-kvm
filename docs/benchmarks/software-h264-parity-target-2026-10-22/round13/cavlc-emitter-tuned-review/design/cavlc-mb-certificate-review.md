# CAVLC macroblock certificate: source-only design review

Status: design only, 2026-10-22. No implementation, formatting, builds, tests, benchmarks or profiles were run for this review. The tuned emitter remains a separate experiment under measurement. Do not interpret this note as a selection decision or a third emitter iteration.

One defensible bounded design is a **lazy per-macroblock certificate for the output range versus the writer and entropy tables**. It does not justify eliminating all per-block checks. The likely incremental CPU benefit is small, and added caller branches/frame pressure could erase it.

## Active source evidence

The active Go CAVLC syntax path calls `WelsWriteMbResidual` after QP emission at [svc_set_mb_syn_cavlc.go:245](../design/evidence/svc_set_mb_syn_cavlc.go.txt). The complete residual function starts at [line 266](../design/evidence/svc_set_mb_syn_cavlc.go.txt). Its ordered block calls are:

| Blocks | Go caller lines | Native caller lines |
| --- | --- | --- |
| I16 luma DC | 279 | 323 |
| I16 luma AC loop | 292 | 335 |
| Inter luma group | 313, 320, 327, 334 | 355, 361, 367, 373 |
| Chroma DC | 346, 351 | 384, 388 |
| Chroma AC loops | 365, 378 | 401, 413 |

Go references above use the frozen round11 `svc_set_mb_syn_cavlc.go`. Native references use [codec/encoder/core/src/svc_set_mb_syn_cavlc.cpp](../design/evidence/svc_set_mb_syn_cavlc.cpp.txt); native's active syntax call is [line 296](../design/evidence/svc_set_mb_syn_cavlc.cpp.txt). Both retain sequential per-block error returns; neither is a whole-macroblock bitstream transaction.

Native [WriteBlockResidualCavlc:108](../design/evidence/set_mb_syn_cavlc.cpp.txt) creates per-block level/run arrays, initializes pending bitstream state at line 124, and commits it on successful zero/nonzero returns at lines 152/230. Native does **not** have the proposed Go certificate; this is an attempt to reduce the safety overhead introduced by our checked assembly boundary, not an omitted native algorithm. Native calls `pfCavlcParamCal` at line 130. Current Go [WriteBlockResidualCavlc:87](../source-delta/frozen/candidate/pkg/h264/internal/encoder/set_mb_syn_cavlc.go.txt) instead uses a direct noescape SSSE3 extractor or scalar implementation and does not invoke the supplied function table. No callback can invalidate an intra-call certificate in the current Go chain. A future callback would require re-auditing this assumption.

The experimental leaf performs eleven output/object exclusions in [cavlc_emit_amd64.s:65](../source-delta/frozen/candidate/pkg/h264/internal/encoder/cavlc_emit_amd64.s.txt): private bs, levels, runs, external writer, and seven entropy tables. External/table checks occupy the block ending at line 141; table identities and byte sizes are generated from [cavlc_emit.go](../source-delta/frozen/candidate/pkg/h264/internal/encoder/cavlc_emit.go.txt). Pending-state/cursor/capacity checks start at line 48, and mutable token validation starts at line 163. Those latter checks are not all amortizable.

## Single proposed design

1. Keep public `WriteBlockResidualCavlc` behavior and source layout unchanged. At complete-MB call sites, consider a small private attempt only for eligible end14/15 blocks using the exact nonzero count already loaded at the original point. A count greater than 3 is only an eligibility hint: it cannot replace extraction or establish a semantic invariant. Initially leave I16 DC, whose call supplies an unconditional flag, and chroma DC on the public path.
2. Create a certificate lazily at the first eligible block, after the original call arguments/count reads. Validate the **entire visible output slice**, with overflow-safe address arithmetic, against the external `SBitStringAux` and all seven entropy tables. This proves eight exclusions once, irrespective of subsequent cursor movement. Nil/short/invalid states must decline the attempt without a new panic or store.
3. The certificate retains a typed slice/base and visible length, not an untracked numeric address across calls. Require the current writer's output slice to retain that identity, or invalidate the certificate. This also avoids stale numeric addresses if a Go stack moves. No certificate escapes the MB call or is cached globally/per-slice.
4. Each private attempt still validates the current cursor, left-bit state, dimensions, visible capacity and pointer arithmetic. It checks the **current coefficient load span** against its at-most-132-byte output prefix before extraction. For the normal end14/15 SSSE3 path, the load span is 32 bytes, including the masked sixteenth coefficient for end14. Unexpected short spans decline to the public writer.
5. The helper owns fresh typed/noescape bs, levels and runs, so these newly created objects cannot be aliases of incoming valid buffers. Only this controlled private entry may rely on ownership. The existing raw checked emitter and its direct-alias contract must remain available; do not turn its documented checks into unchecked assumptions for arbitrary callers.
6. Run a private assembly entry sharing the existing emitter coding body and live-table checks. On success, commit exactly the same three external scalar fields at the original block boundary. On overflow, preserve prefix stores and leave external state uncommitted. On unsupported input/table entries, replay through the unchanged public writer. Re-extraction is safe here specifically because the private coefficient/prefix exclusion proves earlier identical stores cannot change the current coefficients. No second full Go writer implementation is needed.

This shares validation only. It does not share scratch or pending bitstream state between blocks, so it is distinct from the held scratch-reuse experiment. It is also distinct from tuning the density gate inside the public writer: the latter changed that writer's stack/register layout and regressed extracted-zero controls in the initial measurement.

## Required safety gates

- Preserve every existing slice evaluation, count load, context calculation and cache/header reload. Inter luma already snapshots four counts before its first block, but later neighbor counts and other pointers are read later. Do not move those reads, snapshot the whole macroblock, or pre-extract future coefficients.
- Output is allowed to alias current/future coefficients or cache data on the public fallback. Fresh per-block coefficient exclusion is essential: an earlier write or writer-state commit can change future cache/DCT data. A certificate for a previously observed DCT pointer alone would not establish ownership of later coefficient slices.
- Output-versus-external-writer exclusion protects the slice header from output stores. Normal success commits affect only cursor/current bits/left bits and cannot change that header. Since there are no callbacks, the entire-output-versus-table proof remains valid across blocks. Mutable table **contents** must still be read at their original stages, including values changed by aliased external-state commits; this design does not snapshot or freeze them.
- Keep the current emitter's stage-by-stage checks for malformed mutable entries, slow suffix/overflow cases and unsupported symbol lengths. A once-per-MB range certificate does not validate their contents.
- Unsupported-path replay must start from the unchanged external state and use an identical prefix. Partial failure remains per block: earlier successful blocks stay committed; a failing current block may have written bytes without committing its writer state. No whole-MB rollback or delayed commit.
- Nil writer, invalid nC, negative/oversized pending state, invalid cursor and short output must reach the original public behavior without eager faults. The lazy attempt must decline safely before dereferencing a nil writer or indexing a short input.
- Keep objects tracked with typed pointers/slices, respect noescape/lifetime contracts, and use generated structure/table sizes. Do not store unchecked uintptr certificates across stack growth.

Independent source review by `parity_structure_review` found the refined design sound in principle, with no concrete ownership blocker. It specifically confirmed that original ordered cache/header reads and fresh coefficient exclusions are necessary. This was a design review, not implementation or runtime validation.

## Evidence and realistic ceiling

The completed initial emitter [benchstat](../design/evidence/benchstat.txt) showed dense/mixed wins but sparse complete-MB regressions and extracted-zero regressions. These measurements do not isolate the guard cost. No measured per-guard speedup exists.

Completed round11 profile text attributes cumulative CPU to the public block writer at 11.56% Road and 10.43% PDF; the complete residual caller is 13.01%/11.72% respectively. See [Road top](../design/evidence/round11-road1-top.txt) and [PDF top](../design/evidence/round11-pdf1-top.txt). Eliminating all of CAVLC is not a possible outcome of this proposal; it removes only part of the entry overhead on a subset of blocks.

Older byte-verified 616-frame density diagnostics give an upper bound before excluding chroma DC, unhandled I16 DC, metadata mismatches and failed certificates:

| Existing artifact | Blocks with at least 4 coefficients | Per frame | Hypothetical net 10 ns/block saving |
| --- | ---: | ---: | ---: |
| [Road counts](../design/evidence/sparse-count-road1-totals.json) | 9,697,773 | 15,743.14 | 0.157ms/frame |
| [PDF counts](../design/evidence/sparse-count-pdf1-totals.json) | 3,714,155 | 6,029.47 | 0.060ms/frame |

At the existing profiled one-slice CPU times, 29.116 ms/frame Road and 10.151 ms/frame PDF, those illustrative savings correspond to about 0.54%/0.59%. Timing sources are [Road profile log](../design/evidence/round11-road1-profile.txt) and [PDF profile log](../design/evidence/round11-pdf1-profile.txt). This is **sensitivity arithmetic, not a performance prediction or matched benchmark**: the counters are from an earlier equivalent-output diagnostic, include 16 warmup frames, and the profile's printed time covers 600 measured frames. Certificate setup, extra branches/calls, fewer eligible blocks and frame/register pressure all reduce the benefit; a slowdown is possible.

## Selection gates before any merge

Do not implement a third emitter iteration until the currently running tuned caller results are assessed. If later authorized, preserve all initial/tuned evidence and use a fresh isolated candidate.

Required correctness coverage includes the native 447-suite and full-signed/mutable-table suites, complete-MB oracle comparisons, every current partial-failure position, fast-to-public transitions near 132 bytes, current/future coefficient and count/cache aliases, external writer aliases, changed output identity, malformed metadata that overstates/understates density, and an assertion that unused callbacks remain unused. Existing entry points include [MB partial failures:70](../source-delta/frozen/candidate/pkg/h264/internal/encoder/cavlc_emit_mb_test.go.txt), [overflow commit order:130](../source-delta/frozen/candidate/pkg/h264/internal/encoder/cavlc_emit_mb_test.go.txt), [MB aliases:146](../source-delta/frozen/candidate/pkg/h264/internal/encoder/cavlc_emit_mb_test.go.txt) and [unused callback/allocation:174](../source-delta/frozen/candidate/pkg/h264/internal/encoder/cavlc_emit_mb_test.go.txt). Add race, CPU-disabled/fallback, guard-page and non-amd64 compile coverage as appropriate to the actual implementation.

Measure the complete MB caller with all zero/sparse/dense/mixed and 20 density controls, plus unchanged public-writer controls. NoResidual, known-zero, extracted-zero, one-block and sparse callers are explicit stop gates. Only repeatable whole-caller gains without meaningful control regressions justify the subsequent matched full-codec/native-stream gate. A microbenchmark of the reduced guard entry is insufficient.

## Source fingerprints recorded for this review

```text
514fa55c68851c39093e5051b54b86fc56ce22d93ce89a6882c9ad7f1b6ab59f  /tmp/h264-parity-target-20261022/round11/pkg/h264/internal/encoder/svc_set_mb_syn_cavlc.go
f2928b4b635af369b87e886c5f1ebc082bf52efa9324b82590f3d1e30851596f  /tmp/h264-parity-target-20261022/cavlc-emitter-tuned-next/pkg/h264/internal/encoder/set_mb_syn_cavlc.go
5a2c4d1b88db0f3e7ba61f397adc3b62c2a7da298417f28ca79ac037f0c884e6  /tmp/h264-parity-target-20261022/cavlc-emitter-tuned-next/pkg/h264/internal/encoder/cavlc_emit_amd64.s
624dccc87af4de81907d3af234e588b254c319b0c6ffa7235b944f0058daad2e  /tmp/h264-parity-target-20261022/cavlc-emitter-tuned-next/pkg/h264/internal/encoder/cavlc_emit.go
27cae72f1daf1380e0b88860178a988d8166912c4490f5fe6ac11e51ad188ff2  /home/define42/git/openh264/codec/encoder/core/src/set_mb_syn_cavlc.cpp
455deae0a431556d4b23c3706822b3e800b2a24548a33a96d116e9ac879f1548  /home/define42/git/openh264/codec/encoder/core/src/svc_set_mb_syn_cavlc.cpp
```
