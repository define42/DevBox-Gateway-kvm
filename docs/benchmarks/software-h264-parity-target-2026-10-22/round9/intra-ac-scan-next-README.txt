Intra16 AC quantization + scan fusion
====================================
Tree: /tmp/h264-parity-target-20261022/intra-ac-scan-next
Baseline: frozen round8 (does not include pending intra DC SSE2 candidate).
Main untouched. No builds/tests/benchmarks/profiles/native jobs run by agent.
Source formatting only.

Changed/new files:
  pkg/h264/internal/encoder/intra_dc_caller_bench_test.go
  pkg/h264/internal/encoder/quant_scan_intra_amd64.go
  pkg/h264/internal/encoder/quant_scan_intra_amd64.s
  pkg/h264/internal/encoder/quant_scan_intra_amd64_test.go
  pkg/h264/internal/encoder/quant_scan_intra_caller_reference_test.go
  pkg/h264/internal/encoder/quant_scan_intra_caller_test.go
  pkg/h264/internal/encoder/quant_scan_intra_generic.go
  pkg/h264/internal/encoder/quant_scan_intra_guard_linux_amd64_test.go
  pkg/h264/internal/encoder/svc_encode_mb.go

Production design:
- WelsEncRecI16x16Y tries one direct checked helper around the original AC loop.
  On rejection, the exact original four quantization callbacks and sixteen scans
  run in their original order. Count updates remain after all sixteen masks.
- Check occurs after DCT and DC processing, using the entry-snapshotted function
  table pointer and live quantization field. A DCT callback may replace the field;
  a custom fallback quantizer may replace the next group callback as before.
- AVX2 only. The callback code pointer is compared once with the built-in
  WelsQuantFour4x4_sse2. Scalar, nil or custom wrapper callbacks fall back.
  reflect.ValueOf(...).Pointer uses a cached built-in PC; measure this overhead
  in the complete caller. No new boolean instrumentation opt-out is required.
- Full visible spans required: residual256 int16, levels256 int16, FF8/MF8
  int16, masks16 uint16. Rejection does no writes and never eagerly panics.
- Reject residual/levels/masks overlaps across all16 blocks, and either table
  overlapping any output. This preserves future-group/table reload semantics.
  FF/MF may overlap one another read-only.
- Private noescape typed-pointer ABI is40 bytes (five pointers). Masks remain
  local to the caller; no indirect function receives local scratch.
- Raw AVX2 arithmetic is the same unsigned word saturation/high multiplication
  and signed word restoration used by WelsQuantFour4x4_sse2, including arbitrary
  signed FF/MF bit patterns, zero coefficients and MinInt16.
- New entry always performs full AC scans and writes the trailing zero. Existing
  inter/chroma max==0 shortcuts cannot be reused here: signed maxima may be zero
  while unsigned magnitudes >=32768 are nonzero. This entry computes no maxima.
- Existing retained quant/scan kernels and assembly are unchanged. New assembly
  repeats the proven packed arithmetic/AC shuffle for eight block pairs.
- Generic builds decline and retain the original loop.

Tests:
  TestIntraACRawAndCheckedParity: independent unsigned arithmetic plus existing
    scalar scan oracle, all QPs, random full signed FF/MF/coefficient vectors,
    zero and high-magnitude patterns, unaligned starts and full canary outputs.
  TestIntraACRawCoefficientDomain: all65536 coefficient values across selected
    DC/AC/half/group boundaries, signed/saturating table extremes.
  TestIntraACPreflightNoMutation: short lengths with larger capacities,
    nil/custom/scalar callbacks, same/future-group outputs, output/table/mask
    aliases; verifies rejection leaves entire backing unchanged.
  TestIntraACReadOnlyTableAlias: coincident and shifted FF/MF overlap allowed.
  TestIntraACExactSpanGuards: both guard boundaries; exact512-byte read/write
    coefficient/level spans,32-byte masks,16-byte read-only FF/MF; raw+checked.
  TestIntraACCompleteCallerParity: all52 QPs, three stride pairs, Zero/DC/Sparse/
    Dense, compares complete buffers/counts/CBP/reconstruction with frozen round8.
  TestIntraACCustomCallbacksAndAliases: custom quantizer installed after DCT,
    replacement during quantization, nil callback, partial callback panic,
    full signed custom DCT, entry function/residual snapshots, same/future-group
    output alias, counts alias and short count partial writes/panic parity.
  TestIntraACShortResidualPartialState: malformed residual lengths through256,
    custom DCT allows reaching AC checks; compares exact partial state/panics.
  TestIntraACCompleteCallerNoAllocs: real per-call local masks, all four contents.

Root-only validation:
  cd /tmp/h264-parity-target-20261022/intra-ac-scan-next
  /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestIntraAC' -count=1
  /snap/go/current/bin/go test -race ./pkg/h264/internal/encoder -run '^TestIntraAC' -count=1
  GODEBUG=cpu.avx2=off /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestIntraAC' -count=1

Complete caller benchmark:
  intra_dc_caller_bench_test.go is copied byte-for-byte from the intra-DC
  candidate. It depends only on round7/round8's existing intra reconstruction
  fixture and existing APIs, so copy unchanged to frozen baseline; no shim.
  /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^$' \
    -bench '^BenchmarkI16Reconstruction$' -benchmem -benchtime=250ms -count=6 -cpu=1
  Use baseline/candidate rotating serial measurements; check zero allocations.

Merging into selected DC/chroma/skip candidates:
  Apply only the new AC if/fallback block to svc_encode_mb.go; preserve their
  separate forward/inverse DC and chroma-skip call replacements. The copied I16
  benchmark is identical to the DC candidate's file and should not be duplicated.
  The frozen complete caller oracle intentionally retains scalar DC transforms;
  it remains a valid independent output reference for the combined exact changes.

No performance claim until root measurements; function identity and alias-check
cost is intentionally included in the complete production caller benchmark.
