SOURCE FROZEN — center-filter-stack-tuned-next (horizontal dispatch corrected)
Tree: /tmp/h264-parity-target-20261022/center-filter-stack-tuned-next
Based on frozen center-filter-stack-next, which was based on round9.
Main and original candidate untouched. No builds/tests/benchmarks/CPU jobs run.

Changes versus initial candidate:
pkg/h264/internal/common/mc_center_stack_guard_linux_amd64_test.go
pkg/h264/internal/common/mc_center_stack_amd64_test.go
pkg/h264/internal/common/mc_avx2_amd64.go
pkg/h264/internal/common/mc_center_stack_amd64.go
pkg/h264/internal/common/mc_center_stack_amd64.s

Merge manifest versus round9:
pkg/h264/internal/common/mc_center_stack_reference_amd64_test.go
pkg/h264/internal/common/mc_center_stack_guard_linux_amd64_test.go
pkg/h264/internal/common/mc_center_stack_amd64_test.go
pkg/h264/internal/common/mc_avx2_amd64.go
pkg/h264/internal/common/mc_center_stack_amd64.go
pkg/h264/internal/common/mc_center_stack_bench_amd64_test.go
pkg/h264/internal/common/mc_center_stack_amd64.s

Reason for tuning
The initial checked refinement widths won10.5–26.1%, but regular4/8 controls
regressed after adding a legacy helper call. This variant routes all regular
4/8/16 and refinement5/9/17 widths through the same combined leaf. Other
unusual widths retain exactly the previous fallback implementation.

Safety proof for added regular widths
- Same validated wrapper, exact pointers, stack frame752, noCALL/NOSPLIT,
  source-before-output ordering and CPU/negative/empty/visible bounds behavior.
- Width4 uses9 vertical taps: exact8byte+overlapping4byte load ending at byte8,
  stores9words, then full-int16 horizontal pair reads exactly18bytes and
  stores4bytes once per row. Matches previous SSE2 output order.
- Width8 uses13 vertical taps:8bytes+4bytes+1byte, stores13words; horizontal
  pair reads exactly26bytes and stores8bytes once per row. Matches previous
  AVX2 output order.
- Width16 uses21 taps: first16 columns across all rows then8 columns at13.
  Both passes overlap columns13..15 with identical values, all before output.
  Horizontal loads read exactly42bytes and store8bytes at0 then8 at8 per row,
  matching previous AVX2 output order.
- Source aliases cannot observe scratch writes. Every source read completes
  before output. Destination rows (including zero/short strides) retain the
  same complete row/group order. No new alias fallback is needed.
- Largest scratch footprint remains17*22*2=748. New regular shapes use only
  18/26/42 initialized bytes per44byte scratch row; no padding is read.
- Generic/raw full-int16 horizontal entries unchanged; their existing signed
  extreme tests still apply. The combined raw entry accepts byte source pixels.

Coverage
The independent int64 full36tap convolution, all heights1..17, raw+checked,
source/destination zero/overlap/large strides, canaries, aliased entire-backing
comparison against frozen round9, fresh goroutine stacks, and exact per-row
read-only guard tests now include all six4/5/8/9/16/17 widths. Original fallback
and partial-panic tests retain regular and unusual-width cases.

Commands for root, serialized (NOT RUN by agent):
cd /tmp/h264-parity-target-20261022/center-filter-stack-tuned-next
go test ./pkg/h264/internal/common -run '^(TestMcCenterStack|TestMcCenterPairs|TestMcAVX2)' -count=1
go test -race ./pkg/h264/internal/common -run '^TestMcCenterStack' -count=1
GODEBUG=cpu.avx2=off go test ./pkg/h264/internal/common -run '^TestMcCenterStack' -count=1
go test ./pkg/h264/internal/encoder -run '^(TestHalf|TestQuarter|TestMeRefine|TestFractional)' -count=1
go test ./pkg/h264/internal/common -run '^$' -bench '^(BenchmarkMcCenterWholeCaller|BenchmarkMcAVX2Refinement)$' -benchmem -benchtime=250ms -count=6 -cpu=1
go test ./pkg/h264/internal/encoder -run '^$' -bench '^BenchmarkMeRefineFracPixelCaller$' -benchmem -benchtime=250ms -count=6 -cpu=1

The complete checked BenchmarkMcCenterWholeCaller file is byte-identical to
initial candidate and remains baseline-compatible. Complete refinement caller
benchmark is unchanged. Initial-candidate root checks passed linker/frame/
fresh-stack/objdump; rerun for this variant before retaining.

Correction after independent review / failed initial tests
- Restored ONLY McHorVer20_avx2's refinement dispatch to widths5/9/17. The
  intended McHorVer22_avx2 combined-center dispatch remains4/5/8/9/16/17.
- Root preserved initial failed tests and source before this correction. No
  candidate benchmarks were run with the bad dispatch; main is unaffected.
- Added TestMcCenterStackHorizontalDispatcherBoundary: regular horizontal
  widths4/8/16, heights1/4/16, exact read-only width+5 source row guards and
  width destination row guards at both ends; independent6tap pixel oracle.
  Existing TestMcAVX2GuardPages/RefinementGuardPages coverage remains intact.
- Corrected source requires root to rerun full common/encoder tests before
  benchmarking. Agent did not run builds/tests/benchmarks.

Corrected frozen SHA256
642296b3ec1bc7a204d7a5f31f4e27f9d76297624efcebf834d1dadc43030d54  pkg/h264/internal/common/mc_avx2_amd64.go
458cc71baebbb3ca81106f50903cf97f8893ac93ee502aaaa36bfe9b318713c5  pkg/h264/internal/common/mc_center_stack_amd64.go
602d489c00cb3cffc160765b66eae9505ad3a20c3a165dd3c2843bf928326445  pkg/h264/internal/common/mc_center_stack_amd64.s
ba0401203d3b25f801f56abbcd16ea596ab63cf13d358c59b292848fc253954a  pkg/h264/internal/common/mc_center_stack_amd64_test.go
2bcb5f9fa686a7e47b0041166a86343b3342bfdb51ac1ada78c0482e8127ca36  pkg/h264/internal/common/mc_center_stack_guard_linux_amd64_test.go
51982511a57c2baf324695e84324c7f74641adcca8fdd10baa7015ca3afa49eb  pkg/h264/internal/common/mc_center_stack_reference_amd64_test.go
fabc9bebca485f541cb3b94f2d60af35d059b66d0101d5fafe12e7b79f146146  pkg/h264/internal/common/mc_center_stack_bench_amd64_test.go
