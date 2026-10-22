Motion-vector SSE2 fill — frozen source experiment, 2026-10-22

Candidate: /tmp/h264-parity-target-20261022/update-mv-simd-next
Baseline: /tmp/h264-parity-target-20261022/round10
Main and other candidate trees are untouched. Source-only preparation: no Go
format/build/test/benchmark, native executable or profile run by this agent.

SOURCE EVIDENCE AND EXPECTED CEILING
Native md.cpp:487 installs UpdateMbMv_sse2; mb_copy.asm:359-373 broadcasts the
copied 32-bit vector and writes four 16-byte groups. Go md.go:272 still loops
through sixteen checked stores, and md.go:377 initializes that scalar callback.
The round10 PDF profile samples UpdateMbMv_c at 0.07s / 6.42s = 1.09% flat;
earlier PDF profiles also sampled it. This is a small bounded native gap, not a
claim of substantial frame speedup or an explanation of the entire native gap.
The held SMB layout and P-skip closure experiments did not implement this fill.

PRODUCTION SCOPE: THREE FILES
1. vaa_intra_simd_amd64.go: existing amd64 initializer selects UpdateMbMv_sse2
   only when WELS_CPU_SSE2 is requested. The common initializer still first
   installs UpdateMbMv_c; a later flags=0 call resets correctly. The existing
   !amd64 initializer remains a no-op, so non-amd64 keeps the scalar callback.
   SSE2 is mandatory on amd64; no optional SSSE3/AVX instruction is used.
2. update_mv_simd_amd64.go: typed-pointer/noescape private kernel and checked
   callback. A visible length below16 calls the original scalar function so
   prefix stores and exact index panic remain unchanged. The by-value vector
   components are packed without arithmetic changes. The public scalar function
   is untouched. No callers or custom callbacks are bypassed.
3. update_mv_simd_amd64.s: leaf $0-12 ABI, pointer at0, uint32 at8; one dword
   load, PSHUFL broadcast and four MOVOU stores at0/16/32/48, then return.
   Exactly64 output bytes; unaligned origins supported, no destination reads,
   no calls or stack scratch. Native source attribution is retained.

TESTS (UNRUN BY AUTHOR)
- Existing TestIntraVaaSIMDDispatch updated only to expect the newly selected
  motion-vector callback; unrelated inter-analysis callback remains scalar.
- TestUpdateMbMvSIMDDispatch: flags0/SSE2/SSSE3/mixed/AVX2/reset selection.
- TestUpdateMbMvSIMDFullComponentRange: all65536 int16 patterns in each component
  with two independent full-range pairings; exact sixteen entries and guards.
- TestUpdateMbMvSIMDExactSpan: all16 byte origins, independent x/y extreme
  combinations, raw/checked output compared to independent component byte writes.
- TestUpdateMbMvSIMDShortSlices: visible lengths0..18, both exact/spare capacity,
  nil input, entire backing storage and panic text versus unchanged scalar.
- TestUpdateMbMvSIMDCopiedValueAliases: source value taken from each destination
  entry must be copied before all output writes.
- TestUpdateMbMvSIMDGuardPages: raw/checked64-byte spans adjacent to either guard,
  and every short1..15-vector visible span preserves scalar writes then panic.
- TestUpdateMbMvSIMDSkipCaller: real WelsMdPSkipEnc versus retained reference;
  all seven scenarios, SAD/SATD, callback mutation trace/final state; separate
  short0..16-vector partial-panic cases, including MV output aliasing the
  predictor cache and full backing-storage comparisons.
- TestUpdateMbMvSkipCallerVisits: wraps ONLY pfUpdateMbMv, retaining real built-in
  MC/cost/transform callbacks as in the benchmark, and checks three successive
  calls (warmup and steady state) for all fourteen full caller fixtures.

FULL CALLER CLASSIFICATION (ASSERTED BY THE VISIT TEST)
Direct, PredThreshold, RefThreshold, TransformSuccess, each SAD and SATD:
  eight measured cases, accepted=true, exactly one pfUpdateMbMv call each.
RejectLuma, RejectU, RejectV, each SAD and SATD:
  six no-fill controls, accepted=false, zero pfUpdateMbMv calls each.
The counting callback exists only in tests, never in a timed benchmark loop.

BENCHMARKS / BASELINE DEPENDENCIES
Copy ONLY update_mv_bench_test.go into a new isolated round10 baseline copy.
It uses baseline APIs and needs no shim. Existing dependency already in round10:
  skip_dct_quant_caller_bench_test.go (fixture type/helper and result sink).
BenchmarkUpdateMbMv: Scalar and Dispatched x one hot block /8160 frame blocks x
four SMV origins0/1/2/3 (byte offsets0/4/8/12). Sixteen direct cases. Production
InitIntraAnalysisVaaInfo selects the measured callback in both trees. blocks/op
is reported after b.Loop, and output checks follow timing.
BenchmarkUpdateMbMvSkipCaller: the fourteen active real MC/cost/transform cases
above; explicitly initializes production analysis dispatch because the reused
fixture otherwise pins pfUpdateMbMv to scalar. Initialization/checks are untimed.
No raw-kernel-only timing is used to select the candidate.

Candidate parity-test dependencies already present in round10:
  skip_dct_quant_caller_test.go (frozen complete caller/reference comparison),
  skip_dct_quant_caller_bench_test.go (fixtures),
  intra_dc_guard_linux_amd64_test.go (guard allocator).

ROOT COMMANDS (ROOT SERIALIZES ALL CPU JOBS)
cd /tmp/h264-parity-target-20261022/update-mv-simd-next
/snap/go/current/bin/gofmt -w pkg/h264/internal/encoder/update_mv_simd_amd64.go pkg/h264/internal/encoder/update_mv_simd_amd64_test.go pkg/h264/internal/encoder/update_mv_simd_guard_linux_amd64_test.go pkg/h264/internal/encoder/update_mv_bench_test.go pkg/h264/internal/encoder/vaa_intra_simd_amd64.go pkg/h264/internal/encoder/vaa_intra_simd_amd64_test.go
CGO_ENABLED=0 GOMAXPROCS=2 /snap/go/current/bin/go test -tags integration ./pkg/h264/internal/encoder -run '^Test(UpdateMbMv|IntraVaaSIMDDispatch)' -count=1
CGO_ENABLED=0 GOMAXPROCS=2 /snap/go/current/bin/go test -tags integration ./pkg/h264/internal/encoder -count=1
GODEBUG=cpu.all=off CGO_ENABLED=0 GOMAXPROCS=2 /snap/go/current/bin/go test -tags integration ./pkg/h264/internal/encoder -run '^Test(UpdateMbMv|IntraVaaSIMDDispatch)' -count=1
CGO_ENABLED=1 GOMAXPROCS=2 /snap/go/current/bin/go test -race -tags integration ./pkg/h264/internal/encoder -run '^Test(UpdateMbMv|IntraVaaSIMDDispatch)' -count=1
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 /snap/go/current/bin/go test -c -tags integration ./pkg/h264/internal/encoder -o /tmp/h264-parity-target-20261022/update-mv-arm64.test

Repeat in both isolated benchmark trees, alternating order at least six pairs,
using root's fixed CPU affinity and existing benchstat process:
CGO_ENABLED=0 GOMAXPROCS=2 /snap/go/current/bin/go test -tags integration ./pkg/h264/internal/encoder -run '^$' -bench '^BenchmarkUpdateMbMv(SkipCaller)?$' -benchtime=200ms -count=1

If checked fill and full caller data justify retention, existing full-frame
hash/decode/fallback/codec comparisons remain required. Kernel call/ABI overhead
could erase the small operation's gain, and six no-fill controls must not regress.
No speedup is asserted here.

FROZEN SOURCE SHA256 (before root gofmt)
f8ca45490526ab19aba2ce9e06c22cebe128fc4d3aa8971573e048389cbb0ded  pkg/h264/internal/encoder/vaa_intra_simd_amd64.go
6a38f289976797c6bf15cc854d88dca656c9180ae2b2f1ee9a14a2be992aedde  pkg/h264/internal/encoder/update_mv_simd_amd64.go
5db2cd51821e8af4f175f39d2bbfd83b9514efba45a1631170db61342edd54bf  pkg/h264/internal/encoder/update_mv_simd_amd64.s
5aa1e10b9dbcc043ce7420583867a34ccd3a2ac4c2f581237ab05e5c192393f8  pkg/h264/internal/encoder/vaa_intra_simd_amd64_test.go
7ea3bcd28615523cf79b5f7f4f34c6bab4a718e571c57bac6395b0cfe71767bf  pkg/h264/internal/encoder/update_mv_simd_amd64_test.go
166f997358c05334dfe4f624dd8dc06d4971d88bdf1c590853a4ccad928098ba  pkg/h264/internal/encoder/update_mv_simd_guard_linux_amd64_test.go
854ea02a520d30fcd7d8376e1d4e593f7cf12bba07ba6632bfc56b38ac432a9e  pkg/h264/internal/encoder/update_mv_bench_test.go
