FROZEN SOURCE CANDIDATE: intra4 scratch cache-line isolation
Base: /tmp/h264-parity-target-20261022/round11-next
Candidate: /tmp/h264-parity-target-20261022/intra4-scratch-isolation-next
Main repository is untouched by this candidate. No Go builds, tests, benchmarks,
profiles, native compiles or decoder jobs have been run by this agent.
Independent source review: parity_entropy_review reported clean, no changes requested.

PRODUCTION CHANGE
AllocMbCacheAligned now obtains prediction[32], previous[16]bool and remaining[16]int8
from one pointer-free allocation with a leading and trailing cpu.CacheLinePad.
Original lengths, capacities, zero initialization and field disjointness are retained.
A full-cap append still allocates. FreeMbCache is unchanged, including its historical
retention of a derived prediction view. No macroblock/slice struct layout changes,
hot-loop edits, callbacks, unchecked pointers or unsafe production code are introduced.

The active64 bytes cannot share a cache line with another allocation when the hardware
line is no larger than cpu.CacheLinePad (64 on amd64,128 on arm64). Both guards are
needed because the object's base need not be line aligned. This is not a guarantee
about every processor's geometry. The tests/diagnostic skip geometry on architectures
where x/sys supplies zero padding. On amd64 this changes three payload allocations
(total64 bytes) to one192-byte object: +128 bytes per slice, +512 bytes for four slices,
and two fewer initialization allocations. It does not allocate per frame or macroblock.

SOURCE EVIDENCE AND LIMITS
Go svc_encode_slice.go:672-681 originally allocates32-byte prediction and two16-byte
mode buffers separately. InitSliceList:797-832 allocates complete slices serially;
InitSliceThreadInfo:905 and InitOneSliceInThread:850-861 give each fixed-slice worker
its own slice. wels_task_management.go:209-244 uses distinct worker task/slice/bitstream
state and reduces task results after the worker barrier.

svc_base_layer_md.go:574 writes the small predictor for I4 candidates; lines699-707
write the two mode flags per accepted4x4 block. svc_set_mb_syn_cavlc.go:52-69 consumes
those flags. Their writes are worker owned, not synchronization data.

The matched local Go runtime internal/runtime/gc/sizeclasses.go has16/32-byte class
minimum alignment16/32: separate small objects can share a64-byte cache line.
The larger pointer-free scratch sizes384,512,768,816 and2560 round to classes with
minimum alignment at least128 in this runtime. runtime/msize.go:18-30 only adds the
malloc header for pointerful objects, so these arrays/SDCTCoeff are unaffected.
This is runtime-version evidence, not a Go language alignment guarantee.

Native codec/encoder/core/src/svc_encode_slice.cpp:772-793 ALSO allocates these buffers
separately; it does not use one per-slice arena here. Its WelsMallocz delegates through
CMemoryAlign. codec/common/src/memory_align.cpp:65-94,117-139 rounds each payload to
m_nCacheLineSize. encoder_ext.cpp:2209-2216 selects cache-line flags and:2333 constructs
the allocator with that alignment. Native separate small buffers therefore start on
separate selected alignment boundaries.

The Go task-result array is written once per worker, not per macroblock. Fixed-slice
thread scratch ownership is stable. Embedded iDiamondSad and MD state are not these
small separate allocations. RC-off update callbacks do not establish a hot slice-tail
sharing cause. This candidate deliberately does not pad broader metadata or redesign
arenas. The saved four-slice profile shows broad codec CPU cost and noisy timing;
source layout risk alone does NOT prove contention or explain the whole scaling gap.

DIAGNOSTIC
intra4_scratch_diagnostic_test.go is self-contained and baseline-compatible. It uses
actual InitializeExt for1920x1080 camera real-time low complexity, QP20, RC-off,
matching one/four fixed slices and the benchmark's feature settings. It records
addresses for target scratch, large scratch, DCT, MD state, MB cache, bit writer and
thread bitstream. SUMMARY counts distinct cross-owner lines and the subset touching
small target scratch. It reports observed placement, not measured contention.
runtime.KeepAlive retains codec/context/slices until all uintptr comparisons/logs end.
Run baseline and candidate diagnostics; several counts can expose allocator placement
variation. No assertion or fixture forces baseline packing.

TEST COVERAGE
TestIntra4ScratchAllocationContract: exact len/cap, zero initialization, append capacity,
field disjointness, FreeMbCache and reallocation with old views still alive.
TestIntra4ScratchCacheLineIsolation: padding layout proof and32 actual AllocMbCacheAligned
owners checked against all their scratch ranges, with explicit lifetime retention.
TestIntra4ScratchCallerParity: full WelsMdI4x4Fast versus original separate allocations;
QP0/20/51, four contents, four neighbor masks, early-exit/full-search cost thresholds.
TestIntra4ScratchConcurrentCaller: four independently owned caller fixtures versus serial
state after32 calls each, suitable for the race detector. No forced packed layout.

BENCHMARK
BenchmarkIntra4ScratchWorkers is self-contained and baseline-compatible. It runs the
complete existing I4 caller (prediction, scoring, transform, reconstruction), with
workers1/4, strides32/32 and1920/1984, Flat/Smooth/Noise/FirstExit. Fixtures use production
allocation and are built completely one at a time, like slice construction. Goroutines
start before ResetTimer and results are written once per worker. b.N counts total
macroblock calls across workers, so ns/op measures caller throughput, NOT frame latency.
Four-worker cases require GOMAXPROCS>=4. Full-frame alternating measurements must decide
retention; address sharing or component gains alone are insufficient.

BASELINE DEPENDENCIES
Copy ONLY intra4_scratch_bench_test.go and intra4_scratch_diagnostic_test.go to the frozen
baseline package. Both are independent of the candidate type. The candidate semantics
test uses both files' helpers, so it is not a baseline dependency. Existing production
files should not be copied for the baseline.

ROOT COMMANDS (NOT RUN HERE)
From the candidate root:
  /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestIntra4Scratch' -count=1
  /snap/go/current/bin/go test -race ./pkg/h264/internal/encoder -run '^TestIntra4Scratch(AllocationContract|CacheLineIsolation|CallerParity|ConcurrentCaller)$' -count=1
From both baseline and candidate, after copying the two baseline-compatible files:
  H264_SCRATCH_DIAGNOSTIC=1 CGO_ENABLED=0 GOMAXPROCS=4 /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestIntra4ScratchAllocationDiagnostic$' -count=3 -v
  CGO_ENABLED=0 /snap/go/current/bin/go test -c ./pkg/h264/internal/encoder -o /tmp/<baseline-or-candidate>-intra4-scratch.test
Then alternate precompiled binaries in matched order, e.g.:
  GOMAXPROCS=4 taskset -c 2 /tmp/<binary> -test.run '^$' -test.bench '^BenchmarkIntra4ScratchWorkers$/^workers1$' -test.benchmem -test.benchtime=200ms -test.count=1
  GOMAXPROCS=4 taskset -c 0-3 /tmp/<binary> -test.run '^$' -test.bench '^BenchmarkIntra4ScratchWorkers$/^workers4$' -test.benchmem -test.benchtime=200ms -test.count=1
Repeat alternating pairs six times and run benchstat. Root should also run its usual
whole-package, CPU-fallback, race, generic/arm64 build, stream/hash/decoder and full-frame
gates before retention. This allocation-only candidate changes no SIMD dispatch.

CHANGED-FILE SHA256
8375807b544e441def7d0a30f55f09c3a1f68313874dff9c2150881b7a4a0529  pkg/h264/internal/encoder/svc_encode_slice.go
a32e347708c4eb3e48da1d667a08644785fc91674006faecf93fc6ac62d66e04  pkg/h264/internal/encoder/intra4_scratch.go
b9ab545875607a8c335095c2f9102494b9c316a3548264e2a0f6ca64537ceb4f  pkg/h264/internal/encoder/intra4_scratch_bench_test.go
da8c4f3f8ca4e695758e15936d4c13db52649d8e352415ed725313238880034d  pkg/h264/internal/encoder/intra4_scratch_diagnostic_test.go
8bc67f8ff73f47416aa659147f70251b97b64f15c26d34c7aee756a7d5dff513  pkg/h264/internal/encoder/intra4_scratch_test.go
