Intra16 DC forward/inverse SSE2 prototype
========================================

Source frozen 2026-10-22. No builds, tests, benchmarks, profiles or gopls run by
implementing agent. Main and native source trees untouched.

Tree: /tmp/h264-parity-target-20261022/intra-dc-simd-next
Baseline: exact /tmp/h264-parity-target-20261022/round7 copy. No held CAVLC,
end3 extraction, table128, half-pair or other pending prototypes included.

Native gap:
- Native encode_mb_aux.cpp installs WelsHadamardT4Dc_sse2; the actual Go I16
  caller directly used WelsHadamardT4Dc_c.
- Native decode_mb_aux.cpp installs WelsDequantIHadamard4x4_sse2; Go I16 QP>=12
  directly used WelsDequantIHadamard4x4_c.
- Source patterns inspected first in native core/x86/dct.asm:76 and
  core/x86/quant.asm:330. Assembly carries native attribution.

Production files:
  pkg/h264/internal/encoder/intra_dc_simd_amd64.go (new)
  pkg/h264/internal/encoder/intra_dc_simd_amd64.s (new)
  pkg/h264/internal/encoder/intra_dc_simd_generic.go (new)
  pkg/h264/internal/encoder/svc_encode_mb.go (only two call replacements)

New tests / benchmarks:
  intra_dc_simd_test.go
  intra_dc_simd_amd64_test.go
  intra_dc_guard_linux_amd64_test.go
  intra_dc_bench_test.go
  intra_dc_caller_bench_test.go
  intra_dc_caller_reference_test.go
  intra_dc_caller_test.go
(all in pkg/h264/internal/encoder)

Exactness / dispatch:
- Private hadamardT4Dc and dequantIHadamard4x4 direct dispatchers preserve the
  existing direct-call behavior. Exported scalar functions and function-table
  callbacks are unchanged; custom/nil callbacks are not newly invoked.
- amd64 baseline SSE2, no optional feature gate. Other architectures use scalar.
- Forward raw ABI: (*[16]int16 destination, *[241]int16 residual), noescape.
  Reads exactly sixteen int16 DC positions through index240. Gather columns
  preserve grouped macroblock layout; source is entirely captured before output,
  so every source/destination overlap allowed by the scalar routine remains valid.
- Forward butterflies stay int32. Largest magnitude is bounded by16*32768;
  adding1 and arithmetic divide2 occur before signed saturation to int16.
  Full signed input is covered, not only expected codec residual magnitudes.
- Inverse raw ABI: (*[16]int16 residual, uint64 multiplier), noescape.
  Wrapper receives uint16; raw broadcast uses its low16bits. Both Hadamards and
  the final product are observed modulo2^16 by scalar. Multiplication therefore
  commutes with those linear stages even with scalar's widened final sum/product,
  including MF>=32768 and int32 product overflow. PMULLW preserves exact lowbits.
- Forward requires visible input length241 and output length16. Inverse requires
  length16. Short slices take the original scalar routine directly, preserving
  its sequential partial writes and exact panic order. Larger capacities do not
  authorize raw reads. Full raw spans allow unaligned buffers.
- QP<12 inverse + dequant path is unchanged. All existing quant/scan/AC/IDCT/copy
  callbacks and state order are unchanged.

Tests:
  TestIntraDCForwardCheckedParity: random full signed vectors, clipping edges,
    unaligned spans and source/destination sentinels.
  TestIntraDCForwardAliasesAndShortSlices: overlap positions, exact minimum,
    short input/output partial-write and panic parity against scalar.
  TestIntraDCInverseCheckedParity: all QP multipliers, random full signed vectors,
    all65536 uint16 multipliers against an overflow-rich vector, sentinels.
  TestIntraDCInverseShortSlices: partial-state parity for every short length.
  TestIntraDCRawCoefficientDomain: every int16 value, all-position extrema impulses,
    raw forward/inverse comparison against unchanged scalar routines.
  TestIntraDCRawGuards: guards at both ends, exact241-word read-only input and
    16-word output/in-place input, full signed and unsigned multiplier edges.
  TestIntraDCCallerParity: complete I16, all52 QPs, three stride combinations,
    Zero/DC/Sparse/Dense; compares every mutable coefficient/reconstruction buffer
    against the preserved round7 caller with its two scalar DC transforms.
  TestIntraDCCallerCustomTransformAndFailures: custom full-range DCT output,
    short residual failures, partial state/callback count, ignored DC callbacks.
  TestIntraDCCallerNoAllocs: complete production caller expected zero allocations.

Root-only validation commands (CPU stays serialized):
  candidate=/tmp/h264-parity-target-20261022/intra-dc-simd-next
  cd "$candidate"
  /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestIntraDC' -count=1
  /snap/go/current/bin/go test ./pkg/h264/... ./internal/rdp/avc
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 /snap/go/current/bin/go test -c \
    -o /tmp/h264-parity-target-20261022/intra-dc-arm64.test ./pkg/h264/internal/encoder

Benchmarks:
  BenchmarkIntraDCTransforms: checked/scalar forward and inverse, four MF values;
    inverse includes identical input reset cost. Copy the benchmark to frozen
    baseline plus intra_dc_baseline_shim_test.go.txt renamed to *_test.go.
  BenchmarkI16Reconstruction: baseline-compatible complete production caller,
    24 cases: Zero/DC/Sparse/Dense x QP8/20/32 x strides64/64 and1920/1984.
    QP8 controls the untouched low-QP inverse path. Fixture builds outside timer;
    real DCT, quant, scan/count, AC/DC decisions, dequant and reconstruction remain
    inside. Depends only on existing round7 intra_reconstruction_bench_test.go
    fixture helper. No test shim needed for this full-caller benchmark.

  /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^$' \
    -bench '^(BenchmarkIntraDCTransforms|BenchmarkI16Reconstruction)$' \
    -benchmem -benchtime=250ms -count=6 -cpu=1

Use rotating baseline/candidate comparisons; inspect zero allocations and broad
I16 controls before full codec timings. Stream hash, native encoder parity,
decoder checks and final race/fallback validation remain root-owned. No speedup
claim is made before measurement. Apply only the two call-site edits when merging
into a newer retained baseline, rather than overwriting the entire caller file.
