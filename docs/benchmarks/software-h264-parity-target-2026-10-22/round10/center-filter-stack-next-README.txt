Center-filter stack experiment — source frozen, not built/tested/measured
Base: /tmp/h264-parity-target-20261022/round9
Tree: /tmp/h264-parity-target-20261022/center-filter-stack-next

pkg/h264/internal/common/mc_center_stack_reference_amd64_test.go
pkg/h264/internal/common/mc_center_stack_guard_linux_amd64_test.go
pkg/h264/internal/common/mc_center_stack_amd64_test.go
pkg/h264/internal/common/mc_avx2_amd64.go
pkg/h264/internal/common/mc_center_stack_amd64.go
pkg/h264/internal/common/mc_center_stack_bench_amd64_test.go
pkg/h264/internal/common/mc_center_stack_amd64.s

Production design
- McHorVer22_avx2 keeps all public entry checks, CPU fallback, dst-then-src
  validation, negative/zero stride handling, visible lengths, and callbacks.
- Only widths 5/9/17 with heights 1..17 use a new direct typed-pointer noescape
  leaf. Other widths retain their exact kernels in a separate Go helper.
- Old scratch was 17*24 int16 = 816 zero-initialized bytes. The combined leaf
  uses rows of 22 int16 and a 752-byte local frame (748 actual tap bytes).
- Both existing arithmetic stages are copied without changes: byte-pair
  vertical row reuse then full-int16 paired VPMADDWD horizontal filtering.
  There are no assembly CALL instructions and only one final VZEROUPPER.
- Widths 5/9/17 first initialize 10/14/22 words per row. Second-pass loads
  access exactly words [0,width+4]. Width17 first writes 16 columns across
  all rows, then the final8 columns at offset14; all source reads complete
  before ANY output store. The output 8+8+4 / 8+4 / 4+4 group and row order
  is identical, preserving in-place sources and overlapping destination rows.
- Scratch contains no pointers and is never exposed. Typed input pointer
  arguments remain live; R14 (Go g register) is not touched.
- ROOT MUST VERIFY actual NOSPLIT linker chain budget: local frame752 plus
  frame metadata should fit the amd64 792-byte limit. No stack check has been
  disabled beyond a normal no-call NOSPLIT leaf. If compiler-generated wrapper
  chaining exceeds the budget, do not suppress the linker check; revisit the
  design. No source-stage assertion of link/runtime safety is being made.

Read-only native/profile evidence
- native common/src/mc.cpp:1041+ uses uninitialized ENFORCE_STACK_ALIGN_2D
  scratch (macro definition common/inc/macros.h:62) across two kernels.
- Round8 McHorVer22_avx2: 0.15s flat /0.49s cumulative out of18.35CPU seconds.
  Removing scratch zeroing and one boundary cannot eliminate all 0.15s,
  much less the full 0.49s. The previous paired arithmetic remains unchanged.
- Quarter preflight remains 11 spans (0.21s cumulative); per-axis validation
  cannot simply move across custom filter callbacks. No unchecked public API
  or broad validation hoisting is introduced by this experiment.

New tests
- TestMcCenterStackIndependentParity: independent int64 36-tap convolution,
  all widths/heights1..17, byte extrema/patterns/random, zero/overlapping/large
  source strides, overlapping destination rows, exact-len views/canaries,
  checked and raw paths, source immutability.
- TestMcCenterStackAliasedStoreOrder: frozen round9 checked entry vs new raw
  and checked calls, shared source/destination backing, source/dest row overlap
  and all heights/widths. Entire backing storage compared.
- TestMcCenterStackFallbackAndPartialPanics: unchanged public fallback/panic
  and partial output against frozen round9; short visible lengths, unsupported
  widths, invalid offsets, nonpositive dimensions and negative strides.
- TestMcCenterStackFreshGoroutineStacks: fresh goroutines, changing height/
  width and dirty scratch; root link/test checks required.
- TestMcCenterStackExactRowGuards: read-only source rows bounded on both sides
  by PROT_NONE pages, exact width+5 bytes and height+5 rows; exact destination
  rows, raw and checked paths. No widened source reads or output writes.
- Existing full-int16 raw horizontal tests remain unchanged and must still
  pass. New combined raw input is byte pixels; it cannot receive arbitrary
  int16 intermediate taps. Existing TestMcCenterPairsFullSignedRange and
  TestMcCenterPairsTapLanesAndRounding cover unchanged private raw entry.

Root commands (serialized; NOT RUN by agent)
cd /tmp/h264-parity-target-20261022/center-filter-stack-next
go test ./pkg/h264/internal/common -run '^(TestMcCenterStack|TestMcCenterPairs|TestMcAVX2Refinement)' -count=1
go test -race ./pkg/h264/internal/common -run '^TestMcCenterStack' -count=1
GODEBUG=cpu.avx2=off go test ./pkg/h264/internal/common -run '^TestMcCenterStack' -count=1
go test ./pkg/h264/internal/encoder -run '^(TestHalf|TestQuarter|TestMeRefine|TestFractional)' -count=1
go test ./pkg/h264/internal/common -run '^$' -bench '^(BenchmarkMcCenterWholeCaller|BenchmarkMcAVX2Refinement)$' -benchmem -benchtime=250ms -count=6 -cpu=1
go test ./pkg/h264/internal/encoder -run '^$' -bench '^BenchmarkMeRefineFracPixelCaller$' -benchmem -benchtime=250ms -count=6 -cpu=1

BenchmarkMcCenterWholeCaller is baseline-compatible: copy only its new
mc_center_stack_bench_amd64_test.go to the frozen baseline. All public symbols
and fixture helpers already existed. Other listed benchmark sources already
match round9. Normal 4/8/16-wide control cases expose extra fallback call cost.
After link, inspect mcCenterRefineAVX2 and McHorVer22_avx2 machine code to
confirm stack frame, source scratch addressing, absence of Go scratch zeroing
on refinement widths, and no unexpected allocations.
