Half SATD pair: unsupported-shape dispatch tuning
================================================
Tree: /tmp/h264-parity-target-20261022/half-satd-pair-tuned-next
Source baseline: half-satd-pair-next (original half-pair experiment, including existing tests).
No main-tree changes. No builds, tests, benchmarks, profiling or native compilation run by this agent.

Changed relative to half-satd-pair-next (only these two files):
  pkg/h264/internal/encoder/md.go
  pkg/h264/internal/encoder/sample_half_pair_amd64_test.go

Production rationale:
  MeRefineFracPixel now checks uiBlockSize <= BLOCK_8x8 before entering
  sampleHalfSatdPairEnabled. The four supported IDs are contiguous 0..3;
  narrow IDs are 4..6. This removes narrow capability/helper work without
  changing supported shape checks, helper ABI, per-axis span validation,
  filter/scoring/MVD order, tie comparisons, stack descriptors or snapshots.
  Do not claim a gain until the existing whole-caller benchmark is repeated.
  The exact-shape switch remains unchanged to avoid mixing another variable.

Additional test coverage:
  TestHalfSatdPairFilterSnapshots covers all seven shapes and filters replacing
  encoded/reference headers and offsets, encoded/reference stride fields,
  context function-list pointer, cost-table pointer, individual cost callback,
  vertical output header and horizontal output header.
  Saved entry inputs/callback continue through both half axes; fresh output
  headers are observed immediately after each filter. Changed encoded/cost
  fields intentionally fail in quarter refinement; tests compare the complete
  prior scratch/prediction state, live half output buffers and panic text.
  Callback replacement clears the relevant optimization capability.

Full half-pair merge set relative to its pre-half-pair baseline (unchanged nine-file manifest):
  pkg/h264/internal/encoder/md.go
  pkg/h264/internal/encoder/sample.go
  pkg/h264/internal/encoder/wels_func_ptr_def_h.go
  pkg/h264/internal/encoder/sample_half_pair.go
  pkg/h264/internal/encoder/sample_half_pair_amd64.go
  pkg/h264/internal/encoder/sample_half_pair_amd64.s
  pkg/h264/internal/encoder/sample_half_pair_generic.go
  pkg/h264/internal/encoder/sample_half_pair_amd64_test.go
  pkg/h264/internal/encoder/sample_half_pair_guard_linux_amd64_test.go

Targeted validation from this tree:
  /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^Test(HalfSatdPair|MeRefineFracPixelNoPerBlockAllocs|QuarterRefinePerInvocationParamsNoAllocs)' -count=1

Existing baseline-compatible complete-caller benchmark (file unchanged):
  /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^$' -bench '^BenchmarkMeRefineFracPixelCaller$' -benchmem -count=6

Compare the same pre-half baseline and original half-pair candidate, interleaved
serially, including all narrow controls. Compiler/disassembly confirmation of
whether the original helper was inlined is useful for explaining any difference.
