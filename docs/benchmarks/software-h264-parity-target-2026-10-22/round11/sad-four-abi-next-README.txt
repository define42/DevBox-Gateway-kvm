SAD-four tracked-pointer ABI experiment — SOURCE READY
Base: /tmp/h264-parity-target-20261022/round10-next
Tree: /tmp/h264-parity-target-20261022/sad-four-abi-next
No Go builds, tests, benchmarks or profiles have been run by the author.

Production changes (first two manifest files only):
- Three private noescape SSE2 functions change from two slices to two *byte parameters.
- ABI shrinks from 80 to 48 bytes: src0, srcStride8, ref16, refStride24, height32, sad40.
- Assembly differs only in signatures, TEXT argument size and six initial argument loads.
  A Python source comparison verified every arithmetic/load/store instruction after setup is unchanged.
- The checked wrapper preserves output-array conversion first, both negative-stride checks next,
  encoded span validation then reference halo validation, with exactly the same integer arithmetic.
  Only the two already-validated sub-slices become &buf[offset] tracked pointers.
- All public signatures, dispatch, non-amd64 fallback, source/reference/output alias order remain unchanged.

New coverage:
- Independent scalar SAD expected output over a complete backing copy, then four ordered result stores.
  Output aliases encoded/reference start/end/left/right/up/down, inputs alias each other, zero/overlap
  and normal strides, all seven shapes; compare entire backing storage.
- Frozen round10 checked-wrapper validation and scalar computation oracle compares panic type/text,
  full output and backing for short output 0..5, negative/zero/max strides, short len with larger cap,
  start/tail/halo errors, extreme offsets and output/reference aliases. Valid cases are included.
- Complete DiamondSearch scalar-vs-SSE2 state/cost/cache parity, deterministic one-call stationary
  and four-call X/Y searches, callback origin/order/count, strict top-first ties, fresh local descriptor
  allocation regression for every shape.
- Existing component row-impulse, extreme, SIMD/scalar, exact source guard and output canary tests reused.

Baseline-compatible benchmark:
- Copy ONLY pkg/h264/internal/encoder/diamond_sad_four_caller_bench_amd64_test.go to a baseline copy.
  It is self-contained and references only pre-existing types/APIs/constants; no other new files required.
- BenchmarkDiamondSadFourCaller: all 7 shapes x stride32/2048 x Stationary/MoveX3/MoveY3.
  Initializes direct production SSE2 dispatch, prepares pixels and MVD table outside timing, resets
  mutable ME state per call, executes the whole search, and verifies exact final MV/cost/reference.
  Stationary stops after one four-SAD call, motion fixtures after four; MVD costs are nonzero.
- Existing BenchmarkSampleSadFourRows remains unchanged and needs existing sad_four_simd_amd64_test.go
  fixture table (already in round10-next); BenchmarkSampleSadFourBatched is an optional companion.

Root commands (run serially from candidate tree; repeat benchmarks in baseline with same copied fixture):
/snap/go/current/bin/go test ./pkg/h264/internal/common -run "TestSampleSadFour" -count=1
/snap/go/current/bin/go test ./pkg/h264/internal/encoder -run "TestDiamond(SadFour|CostSelect)|TestMotionEstimate.*DiamondSearch" -count=1
/snap/go/current/bin/go test ./pkg/h264/internal/common ./pkg/h264/internal/encoder
/snap/go/current/bin/go test -race ./pkg/h264/internal/common ./pkg/h264/internal/encoder -run "TestSampleSadFour|TestDiamondSadFour" -count=1
/snap/go/current/bin/go test ./pkg/h264/internal/common -run "^$" -bench "^BenchmarkSampleSadFourRows$" -benchmem -benchtime=200ms -count=6 -cpu=1
/snap/go/current/bin/go test ./pkg/h264/internal/encoder -run "^$" -bench "^BenchmarkDiamondSadFourCaller$" -benchmem -benchtime=200ms -count=6 -cpu=1

Performance is unmeasured. This candidate targets Go checked-wrapper/ABI overhead only, not arithmetic.
Keep only if whole-caller results justify the change; root owns build/escape/stream/decoder/full-frame gates.

SHA256 manifest:
2f6ebac83648260cb1d170085509a0bf515cd094e3b3302f99937b1dd1d49bff  pkg/h264/internal/common/sad_four_simd_amd64.go
0e29619fbb79911c8873b1c70cf7d2e7fa09a5857caeeb09463a862a606154ab  pkg/h264/internal/common/sad_four_simd_amd64.s
8d58055b91c036149602e7c53861ad9fa27deb03231c026322129c79048c04a1  pkg/h264/internal/common/sad_four_pointer_amd64_test.go
b011be5769da1fbc4c13a3c77a80161eadabe69c43cda4eb05ad5dc8a20ce039  pkg/h264/internal/encoder/diamond_sad_four_caller_bench_amd64_test.go
aa953a0d0536f7e22be254e447b414eec29cc6a6c6efa35afe6f00c579c6bafb  pkg/h264/internal/encoder/diamond_sad_four_caller_amd64_test.go
