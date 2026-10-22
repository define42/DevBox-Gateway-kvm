QUARTER PLAN FALLBACK TUNE — FROZEN SOURCE ONLY, NOT BUILT OR MEASURED

Tree: /tmp/h264-parity-target-20261022/quarter-plan-tuned-next
Baseline: /tmp/h264-parity-target-20261022/round11
Held original: /tmp/h264-parity-target-20261022/quarter-plan-next
Held evidence: /tmp/h264-parity-target-20261022/quarter-plan-review
Main, round12, held source and its reports remain unchanged. Root serializes all
build/test/benchmark CPU work. This agent performed source formatting and checks
only. The original eight-file README contains the native-path and semantic proof;
this document describes only the single bounded tune relative to that candidate.

WHY THE ORIGINAL IS HELD
Root reports 16/20 supported whole-caller cases improved3.67-11.72%, four were
inconclusive, but two narrow caller controls and two validated-disabled controls
regressed. An unchanged public16x16 scorer control also regressed5.20%, so code
placement or timing variance may contribute; the helper hypothesis is not a
complete causal explanation. Preserve and report all original measurements.

Existing objdump supplies concrete boundaries: baseline MeRefineFracPixel reserves
0x2e0 (736) bytes. The held caller reserves0x1a0 (416) bytes, but unconditionally
calls the0x310 (784)-byte meRefineQuarterPlan even for unsupported shapes, then
calls the separate0x178 (376)-byte meRefineQuarterPlanLegacy on fallback. These
figures are from the existing compiled prologues, not inferred struct sizes.

ONLY PRODUCTION CHANGE RELATIVE TO HELD SOURCE
md.go now checks width>=8, then current batch/validated/average/metric capabilities
before entering the compact helper. Every ineligible or failed-preflight case
constructs the descriptor inline and directly invokes original public
MeRefineQuarPixel. The descriptor body is lexically the held legacy builder body,
except indentation, one comment and reuse of the parent's existing reference
stride integer. A source equality assertion verified that complete body.

The original one center callback and four ordered MVD reads remain shared and
unchanged. Eligibility is inspected afterwards, so center callback mutations are
seen at the same stage. Failed preflight never repeats center filtering or MVD
lookups. The compact helper, validation/alias rules, all arithmetic, swaps and
averaging stay byte-for-byte unchanged. The former private legacy helper moves byte-for-byte from quarter_refine_plan.go
to quarter_refine_plan_test.go because only the existing direct parity tests use
it as an independent path; production contains only the inline fallback body.

The parent stack frame may grow again; this tune deliberately trades a smaller
frame for eliminating new fallback call chains. The descriptor is declared only
inside fallback, so successful compact scoring has no source-level descriptor
construction. Whether the compiler emits its zeroing only on fallback must be
checked in the new objdump. No speed or allocation claim is made before testing.

COVERAGE AND MEASUREMENT
Every original new test and frozen full-caller oracle remains unchanged. Existing
coverage includes all seven shapes, scalar dispatch, custom/validated/batch optout,
center callback table/header mutations, aliases, exact spans/guards, strict tie
and swap order, MVD failures and extreme offsets with complete partial state.
These test externally observable behavior; no instrumentation hook was added just
to assert that an internal helper was skipped. New objdump must confirm the call
routing and descriptor initialization behavior.

Use the same47-case matrix: 28 unchanged BenchmarkMeRefineFracPixelCaller cases,
seven unchanged BenchmarkQuarterRefineLocalParams controls and the identical12
external BenchmarkQuarterPlanFallbackCaller controls. Six alternating CPU2 pairs,
200ms per leaf, all zero B/op and allocs/op. Compare against frozen round11, not
against the held candidate; preserve held data as explanatory evidence. Reject
material control regressions even if supported cases improve. No codec/full-frame
or native parity claim follows from these component results.

Prepared runner: /tmp/h264-parity-target-20261022/measure_quarter_plan_tuned.py
Root command (not executed here):
  python3 /tmp/h264-parity-target-20261022/measure_quarter_plan_tuned.py
Default output quarter-plan-tuned-review must be absent. Driver verifies all
source/manifest/overlay fingerprints and held evidence hashes, copies private
working trees, formats copied changed files only, then runs full encoder tests,
focused race, full CPU-all-off, arm64 compile, allocation checks and the exact47
case paired matrix. Source, binary, command, raw sample and benchstat evidence is
retained. Only after this passes should codec/full-frame parity gates be considered.

DELTA VERSUS HELD CANDIDATE
pkg/h264/internal/encoder/md.go is the only production change; SHA256: 8283bfd4b8bfc25d4068f0f116211aa8b9c15348d44cb1c7606d95d59af6aefe
quarter_refine_plan.go is renamed byte-for-byte to quarter_refine_plan_test.go.

CHANGED-FILE SHA256 VERSUS ROUND11
8283bfd4b8bfc25d4068f0f116211aa8b9c15348d44cb1c7606d95d59af6aefe  pkg/h264/internal/encoder/md.go
fb1f9e086f0e693edd7c79c3c484d9c651a88ed37349b4b32c9f244a7c62a0d4  pkg/h264/internal/encoder/quarter_refine_plan_test.go
26253c3864bf4426e1f998717944f8842d1e18b57b7570917388590c0509994e  pkg/h264/internal/encoder/quarter_refine_plan_amd64.go
bb83122437848ff7df2700da78e48c55c861fa68581afd5528e4660c99c48e83  pkg/h264/internal/encoder/quarter_refine_plan_amd64_test.go
e2ab0193c5887cd2bfb806429c3dde91cfdb0123803c07ae1d5d6a7c8f19b0f3  pkg/h264/internal/encoder/quarter_refine_plan_generic.go
04065252a3ce985f573c22ed8ebc165cecafa626147f09d3ab99c040e2db7b1d  pkg/h264/internal/encoder/quarter_refine_plan_guard_linux_amd64_test.go
13df7ca504d01ff7335aed6c1601fa570e06d0b70b5f180ac72a5b3c56c39d1a  pkg/h264/internal/encoder/quarter_refine_plan_reference_amd64_test.go
9530d82e7a5a97c286db5d485511697d765517fbfe0ab691a6b6407e4b5358b3  pkg/h264/internal/encoder/quarter_refine_plan_spans_amd64_test.go
