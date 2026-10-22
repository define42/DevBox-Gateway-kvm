Chroma skip Hadamard SIMD experiment
====================================
Tree: /tmp/h264-parity-target-20261022/chroma-skip-simd-next
Baseline: frozen /tmp/h264-parity-target-20261022/round7
Source only: no builds, tests, benchmarks, profiles or native compilation run.
Main tree untouched.

Production rationale:
  Native codec/encoder/core/x86/quant.asm:247 has packed MMX Hadamard + quantization.
  The Go production WelsTryPUVskip caller previously called the scalar function
  directly, performing signed division to derive a threshold on every call.
  New amd64 SSE2 helper gathers exactly four int16 words at indices 0/16/32/48,
  wraps both Hadamard stages exactly as Go int16, forms unsigned abs including
  MinInt16, then uses PMULHUW to detect a nonzero quantized coefficient.
  For ff >= 0 and mf >= 2, q=floor(65535/mf) is in [2,32767] and q-ff is in
  [-32765,32767], so the scalar int16 threshold cannot wrap. For magnitude a,
  a > q-ff iff (a+ff)*mf >= 65536. a+ff <= 65535; both packed words are exact.
  This removes division within the checked domain. Signed ff/mf, mf 0/1 and
  short visible slices use the original unchanged scalar implementation.
  Division-by-zero therefore retains precedence over bounds errors.
  No source words are written; return value is normalized to Go's 0/1 rather
  than the native MMX kernel's count. Raw ABI: three 8-byte arguments plus a
  4-byte return (28 bytes); return uses MOVL, no ambiguous MOVD store.
  The direct production caller and SSE2 function-table entry use this wrapper.
  Generic architectures retain scalar behavior.

Changed files:
  pkg/h264/internal/encoder/hadamard_skip_amd64.go
  pkg/h264/internal/encoder/hadamard_skip_amd64.s
  pkg/h264/internal/encoder/hadamard_skip_amd64_test.go
  pkg/h264/internal/encoder/hadamard_skip_bench_amd64_test.go
  pkg/h264/internal/encoder/hadamard_skip_bench_test.go
  pkg/h264/internal/encoder/hadamard_skip_caller_bench_test.go
  pkg/h264/internal/encoder/hadamard_skip_generic.go
  pkg/h264/internal/encoder/hadamard_skip_guard_linux_amd64_test.go
  pkg/h264/internal/encoder/hadamard_skip_test.go
  pkg/h264/internal/encoder/quant_simd_amd64.go
  pkg/h264/internal/encoder/svc_encode_mb.go

Coverage:
  Full signed16 coefficient domain at each DC position; seven-value edge
  combinations across all four positions; independent int32-linear transform
  oracle; all QP threshold +/-1 boundaries; arbitrary signed ff/mf; minimum
  visible49-word span and longer capacity; division/bounds panic text; nil
  input; unaligned views and untouched canaries; read-only guard pages at both
  ends of the exact98-byte span; raw 0/1 normalization.
  Complete WelsTryPUVskip compares a frozen original caller over all52 QPs,
  both chroma planes, zero/DC boundary/DC reject/sparse AC/AC reject/dense/
  extreme patterns. Additional fixtures cover short coefficients, nil context
  fields, invalid UV, callback mutation/panic and residual/level aliasing.
  Results include full coefficient/SDCT storage, callback order and panic text.

Root validation commands from the candidate tree:
  /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestHadamardSkip' -count=1
  /snap/go/current/bin/go test -race ./pkg/h264/internal/encoder -run '^TestHadamardSkip' -count=1

Baseline-compatible complete caller benchmark:
  Copy only hadamard_skip_caller_bench_test.go unchanged into the baseline.
  /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^$' -bench '^BenchmarkChromaSkipCaller$' -benchmem -count=6
  This includes reset of the mutated64-word residual input per invocation.
  It uses only pre-existing types/APIs; no baseline shim is required.

Candidate-only scalar/checked/raw comparisons:
  /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^$' -bench '^BenchmarkHadamardSkip(Raw)?$' -benchmem -count=6
  To compare the checked benchmark across baseline binaries, copy its generic
  benchmark file plus the caller benchmark fixture file and add this test-only
  baseline shim (do not copy the raw assembly benchmark to the baseline):
    func hadamardQuant2x2Skip(rs []int16, ff, mf int16) int32 {
        return WelsHadamardQuant2x2Skip_c(rs, ff, mf)
    }

No speedup is claimed until root runs serialized measurements. In particular,
check DC rejection and AC-processing controls, where assembly call/validation
cost may consume the arithmetic savings.
