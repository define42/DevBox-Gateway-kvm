QUARTER PLAN CANDIDATE — FROZEN SOURCE, NOT BUILT OR MEASURED

Tree: /tmp/h264-parity-target-20261022/quarter-plan-next
Baseline: /tmp/h264-parity-target-20261022/round11
Main and round12 are untouched. No Go build/test/benchmark/profile/native job was
run by this agent. Source formatting only. Root owns serialized CPU validation.

PURPOSE AND NATIVE EVIDENCE
Go MeRefineFracPixel formerly constructed a328-byte SQuarRefineParams on amd64
(md.go:400-413,597-696), including nine slice headers and offset arrays. The public
fused quarter scorer checks two output ranges, encoded range and eight source
views, often repeatedly covering the same three source families
(sample_simd.go:94-124). sample_quarter_four_amd64.go:18-33 then translates the plan
into another128-byte tracked-pointer descriptor array for the existing batch leaf.
These are representation sizes from field layout, not claims about compiler frame
sizes or emitted zeroing; root should inspect escape/frame output after building.

Native OpenH264 codec/encoder/core/src/md.cpp:511-522 uses a112-byte pointer-only
quarter plan, populated in MeRefineFracPixel before inline MeRefineQuarPixel at
532-573. Native still runs separate half SATD calls and separate quarter averaging
and SATD: the existing Go fused arithmetic is NOT a missing native fused kernel.
This experiment targets Go caller bookkeeping only; every arithmetic kernel stays
unchanged. It is distinct from retained quarter scoring/half scoring/center-filter
fusion and held SATD scheduling/16-wide four-SAD scheduling.

Round11 Road: total17.99s, MeRefineFracPixel2.57s cumulative/0.36s flat; quarter fused
caller0.86s cumulative versus raw batch0.33s. The remaining0.53s includes required
winner averaging, checks and fallback work, not just removable bookkeeping. Even
counting every caller-flat sample, the ceiling is strictly below0.89s (4.95% total
CPU), and practical gain may be far lower. PDF refinement is0.29s/6.23s. This cannot
close the entire native gap. Complete-caller and frame gates decide retention.

IMPLEMENTATION
md.go completes the same selected center callback and int16 half-motion update,
then evaluates the same four MVD costs in top/bottom/left/right order. Candidate
slice-header assignments moved to a cold fallback; they have no external effects
or index accesses, so moving MVD reads ahead of those assignments changes no
observable operation. Half filtering/scoring and final MV/copy logic are unchanged.

The private amd64 compact helper checks exactly the existing batch capabilities,
metric/table selection and supported8/16-wide shape (including8x4). It validates
encoded plus both quarter outputs and three unions derived from the selected half
direction, excludes output/input aliases, constructs the four tracked-pointer
records once, and invokes existing noescape satdAvgFourAVX2 directly. Integer
reference and half/center input aliases are read-only and remain allowed.

No pointer is converted to uintptr across a call. Existing sampleMemoryRange holds
unsafe.Pointer for comparisons; all new descriptor pointers are tracked *byte.
No local descriptor is passed via a function value. Cost addition uses int32 wrap,
strict < and original candidate order. Every winning candidate swaps buffers in
the same sequence; the final winner alone uses the same averaging callback.

Every failed preflight is side-effect-free and invokes the original public scorer
through meRefineQuarterPlanLegacy with the exact old descriptor mapping. The center
callback is never repeated. Generic builds return false immediately. Public
MeRefineQuarPixel, SQuarRefineParams, callbacks, markers and assembly are unchanged.
Instrumentation using the existing batch opt-out continues through the old route.
Unusual or overlapping row strides, invalid offsets, malformed views and output
aliases conservatively fall back with the original partial output/panic behavior.
Custom filters retain entry input/stride/cost snapshots, live post-filter output
headers and fresh quarter encoded fields. Narrow/custom/nonbatch paths now have
extra helper boundaries despite the smaller parent frame: control benchmarks are
mandatory and a regression can reject the entire candidate.

TESTS AND INDEPENDENCE
quarter_refine_plan_reference_amd64_test.go is the full frozen round11 production
MeRefineFracPixel function with only its name changed; source equality was checked.
It uses unchanged public MeRefineQuarPixel, not the new compact builder.
TestQuarterPlanCompleteCaller compares all seven shapes across normal motion,
all four forced half winners/ties, SAD metric, recomputation, negative/overlap/zero
strides, input/output/scratch aliases, shortened views, custom callbacks, header/
function-table/stride mutations, center callback replacements, extreme encoded
offsets, and half/quarter MVD partial failures. Full pixels, MV/cost, filter event
order, view lengths and panic type/text are compared against that frozen caller.
TestQuarterPlanScalarCaller constructs scalar functions before the first score so
it is safe on CPUs lacking AVX2, not just on AVX2 hosts with GODEBUG disabled.
TestQuarterPlanExactSpansAndTies compares minimum union spans, signed cost overflow,
strict ties, multiple improvements, final bytes and buffer-swap parity.
TestQuarterPlanPreflightDoesNotMutate checks one-byte-short visible lengths despite
larger backing caps, unsupported shapes, negative/extreme offsets/strides, callback
opt-outs and aliases without changing any state.
TestQuarterPlanExactUnionGuards puts exact visible source/output spans against both
page edges; sources are read-only. Existing per-row raw batch guards still apply.

BASELINE-COMPATIBLE COMPLETE CALLER BENCHMARK
Use existing BenchmarkMeRefineFracPixelCaller from
sample_quarter_caller_bench_amd64_test.go: all seven shapes, strides32/2048, motion
false/true (28 cases), full half filters/scoring, optional center, quarter plan,
winning average and final copy. It constructs parameters inside each production
call and reports allocations. This fixture and its dependencies already exist
unchanged in frozen round11, so NO benchmark files need copying to baseline.
Existing TestMeRefineFracPixelNoPerBlockAllocs and TestQuarterRefinePerInvocationParamsNoAllocs
remain unchanged. Do not accept scorer-only timings as evidence for this candidate.

ROOT COMMANDS (NOT RUN HERE)
From candidate:
  go test ./pkg/h264/internal/encoder -run '^(TestQuarterPlan|TestHalfSatdPair|TestQuarterRefine|TestSampleAvgSatdFour|TestMeRefineFracPixel)' -count=1
  go test -race ./pkg/h264/internal/encoder -run '^(TestQuarterPlan|TestHalfSatdPair|TestQuarterRefine|TestMeRefineFracPixel)' -count=1
  GODEBUG=cpu.avx2=off go test ./pkg/h264/internal/encoder -count=1
  GODEBUG=cpu.all=off go test ./pkg/h264/internal/encoder -count=1
  GOARCH=arm64 CGO_ENABLED=0 go test -c -o /tmp/quarter-plan-arm64.test ./pkg/h264/internal/encoder
Inspect compiler escape/frame output: MeRefineFracPixel must not heap-allocate
per-call plans; new candidates/costs only cross the direct noescape raw call.
Build precompiled round11/candidate encoder binaries, then alternate six pairs:
  GOMAXPROCS=4 taskset -c 2 <binary> -test.run '^$' -test.bench '^BenchmarkMeRefineFracPixelCaller$' -test.benchmem -test.benchtime=200ms -test.count=1
Use benchstat with every control retained. If caller gains survive, use standard
whole-package, stream/native hash, decoder and paired codec/full EncodeDamage gates.

CHANGED-FILE SHA256
00825129567bbd81ba515012db3d469e334e7efca4bd7b0c4102420e3000fcbe  pkg/h264/internal/encoder/md.go
fb1f9e086f0e693edd7c79c3c484d9c651a88ed37349b4b32c9f244a7c62a0d4  pkg/h264/internal/encoder/quarter_refine_plan.go
26253c3864bf4426e1f998717944f8842d1e18b57b7570917388590c0509994e  pkg/h264/internal/encoder/quarter_refine_plan_amd64.go
bb83122437848ff7df2700da78e48c55c861fa68581afd5528e4660c99c48e83  pkg/h264/internal/encoder/quarter_refine_plan_amd64_test.go
e2ab0193c5887cd2bfb806429c3dde91cfdb0123803c07ae1d5d6a7c8f19b0f3  pkg/h264/internal/encoder/quarter_refine_plan_generic.go
04065252a3ce985f573c22ed8ebc165cecafa626147f09d3ab99c040e2db7b1d  pkg/h264/internal/encoder/quarter_refine_plan_guard_linux_amd64_test.go
13df7ca504d01ff7335aed6c1601fa570e06d0b70b5f180ac72a5b3c56c39d1a  pkg/h264/internal/encoder/quarter_refine_plan_reference_amd64_test.go
9530d82e7a5a97c286db5d485511697d765517fbfe0ab691a6b6407e4b5358b3  pkg/h264/internal/encoder/quarter_refine_plan_spans_amd64_test.go
