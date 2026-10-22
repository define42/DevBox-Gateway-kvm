Bounded CAVLC emitter density gate — SOURCE FROZEN, UNMEASURED

Candidate: /tmp/h264-parity-target-20261022/cavlc-emitter-tuned-next
Source copied from validated/formatted /tmp/h264-parity-target-20261022/cavlc-emitter-measure-next (round11 base). Initial frozen candidate, initial measured working tree and cavlc-emitter-review results remain unchanged. Main is untouched. Author ran no Go/native formatting, builds, tests, benchmarks or profiles; only source edits/copy and Python data reading/hashes.

Only production delta from initial measured candidate:
  set_mb_syn_cavlc.go: move the existing checked emitter call INSIDE the existing totalCoeffs>3 branch, after iCount=3, and restrict it to end14/15. The existing <=3 branch remains Go-only; chroma DC end3 always remains Go-only. No changes to assembly, ABI, pointer checks, mutable table access, fallback/replay, overflow or state commit.

Why this bounded gate:
  Initial complete MB sparse cases (two coefficients) regressed3–11%; tiny end3 blocks could not amortize the leaf's fixed guards/ABI. Dense/boundary/mixed end14/15 callers improved. This gate avoids adding a separate zero/sparse predicate and preserves the original source branch for short blocks. Four coefficients with three trailing ones may still be too little work; new whole-MB threshold controls intentionally expose that risk instead of selecting only favorable dense fixtures. No claim that >3 is an optimal cutoff or that frame performance improves.

Exact saved-binary evidence, read without executing objdump:
  cavlc-emitter-review/base-writer-objdump.txt: frame SUBQ $0xf8, SP.
  cavlc-emitter-review/after-writer-objdump.txt: frame SUBQ $0x108, SP (+16 bytes); matches44-byte extractor ABI0 vs60-byte emitter ABI0 argument/result areas. Both symbols start0x61a0a0.
  Both already materialize/copy the full40-byte private bs before extraction. Taking &bs did not newly create that object work.
  Initial candidate also spills totalZeros before the total==0 branch (0x61a419), reloads total at the next classification branch and recomputes private levels/runs addresses after SIMD extraction. Baseline spills zeros only after the nonzero token branch. The baseline total>3 source branch compiled into CMOV; tuned machine layout cannot be inferred from source and must be inspected after root builds.
  This revision does not pack the ABI or promise to remove the+16-byte frame. Extracted-zero and complete sparse controls remain stop gates.

Correctness/test delta:
  cavlc_emit_test.go SignedDomain now keeps4 or5 coefficients, so all65536 signed inputs and suffix0..6 still exercise the emitter through the new production gate. Lower three coefficients are3; the first/second encoded level remains the exhaustive value/controlled suffix target. No assembly change.
  MutableTables and CoefficientAliasReplay now use four nonzeros at positions0/1/2/7, retaining totalZeros4 and firstRun4. Token/total-zero mutations target totalCoeffs4. The slow-level variant places64 at position2, immediately after the first encoded2, so live suffix mutations still reach the slow path. Public alias replay remains exposed through the tuned production gate.
  cavlc_emit_density_bench_test.go is baseline-compatible, depends only on existing cavlc_scratch_bench_test.go fixtures. Complete InterAll26-block and I16All27-block callers, coefficient counts3/4/5/6/8, Small and Trailing3 patterns:20 cases. Chroma DC count is clamped4. Metrics after b.Loop; validation outside timed loop.
  cavlc_emit_density_test.go compares all20 fixtures at all32 pending positions against frozen block/whole-MB oracles.
  All initial native447, full-domain, malformed/mutable-table, alias/replay, partial failure and protected-page tests remain; initial working tree's unused test import fix is inherited.
  Diagnostic mirror now counts caller density/end exclusions explicitly before raw entry preflight. Matching standalone source generator lives in cavlc-emitter-tuned-diagnostic; initial diagnostic stays unchanged. Counters remain one encoder/thread/slice, include warmup, and are never timing evidence.

Root validation/measurement:
  Run gofmt on changed Go files; then same initial full encoder/native447, focused emitter race and CPU-all-off gates.
  Copy the original four baseline fixtures plus cavlc_emit_density_bench_test.go into a FRESH round11 baseline. Native447 correctness fixture remains unchanged; timing must continue to use external/default333 fixture.
  Six alternating pinned CPU2 200ms pairs: original52 caller/level +15 public cases, plus '^BenchmarkCavlcEmitterDensity$'20 complete-caller threshold cases. Compare all controls; do not summarize only selected wins. No native timing reruns needed.
  Whole-writer/MB gains with neutral zero/sparse/threshold controls are required before a full-codec gate. No main merge based on raw leaf speed.

Production review of initial emitter was clean; this gate delta has been sent to parity_structure_review. Root owns all CPU validation. Baseline and initial results stay frozen.

Delta from initial validated/formatted candidate:
4f57e90da736d17816fe54281bff3b7db8d5d422787520e99810306c89718139  pkg/h264/internal/encoder/cavlc_emit_density_bench_test.go
d4363a2fc638152aa27e69a6a362b2c3347e9fbdafcaabee207cc7ce853f570a  pkg/h264/internal/encoder/cavlc_emit_density_test.go
73996cc45cc1897753dfd79385b14d765e8585caefe57df0dea789ff011cfbfa  pkg/h264/internal/encoder/cavlc_emit_eligibility_test.go
4d6736eca430f4a09b385d34cc796195f62ef029627e118b39d0c88d89f08a14  pkg/h264/internal/encoder/cavlc_emit_test.go
f2928b4b635af369b87e886c5f1ebc082bf52efa9324b82590f3d1e30851596f  pkg/h264/internal/encoder/set_mb_syn_cavlc.go

Full delta from frozen round11 (pre-root-format hashes):
624dccc87af4de81907d3af234e588b254c319b0c6ffa7235b944f0058daad2e  pkg/h264/internal/encoder/cavlc_emit.go
6443b7297ebd32509bfd394d1a03250d8d7c51b8c4e1169b3d049f2bf1e42a4d  pkg/h264/internal/encoder/cavlc_emit_amd64.go
5a2c4d1b88db0f3e7ba61f397adc3b62c2a7da298417f28ca79ac037f0c884e6  pkg/h264/internal/encoder/cavlc_emit_amd64.s
9d6423db6fe0d29b6949e81a6354832216e523844054fe945544c4d6d91f77f1  pkg/h264/internal/encoder/cavlc_emit_amd64_test.go
4f57e90da736d17816fe54281bff3b7db8d5d422787520e99810306c89718139  pkg/h264/internal/encoder/cavlc_emit_density_bench_test.go
d4363a2fc638152aa27e69a6a362b2c3347e9fbdafcaabee207cc7ce853f570a  pkg/h264/internal/encoder/cavlc_emit_density_test.go
03f2ce43e47ca81cd0a9a578a89f140726b3fe0ef5744d33ce42a97ee0719ffa  pkg/h264/internal/encoder/cavlc_emit_diagnostic_test.go
73996cc45cc1897753dfd79385b14d765e8585caefe57df0dea789ff011cfbfa  pkg/h264/internal/encoder/cavlc_emit_eligibility_test.go
ca0b2cc2dc36e633ab771c1e00a29e48de1fcefcadc3e26acef3b9cc763fb172  pkg/h264/internal/encoder/cavlc_emit_guard_linux_amd64_test.go
16120f6f1d3983c3327ba5c80f1705e5c0c453975730e230bd1001bb27039eeb  pkg/h264/internal/encoder/cavlc_emit_mb_test.go
e3f7d61d78873407e5dd74b128ca22d4eb4103ebc2f46cfe0d2cb5dc93c26a78  pkg/h264/internal/encoder/cavlc_emit_other.go
8dfc1cf763aadcdfabdacfbc88221b3b393da4c5109aca978c4655b9898c8d54  pkg/h264/internal/encoder/cavlc_emit_reference_test.go
4d6736eca430f4a09b385d34cc796195f62ef029627e118b39d0c88d89f08a14  pkg/h264/internal/encoder/cavlc_emit_test.go
2538502df865e1f56e0e9c652ddf37a4d07606b8aa3fc05afecc15cab0a83893  pkg/h264/internal/encoder/cavlc_native_full_bench_test.go
fee581e7a1cc34d112d0642036991f4aff84df56f9d3591e99864195e6e38fd7  pkg/h264/internal/encoder/cavlc_native_full_test.go
bb04863da491dfd4e713fd47c23924b38a9c20e395c1cbfe73aed16677be893d  pkg/h264/internal/encoder/cavlc_scratch_bench_test.go
f2928b4b635af369b87e886c5f1ebc082bf52efa9324b82590f3d1e30851596f  pkg/h264/internal/encoder/set_mb_syn_cavlc.go
144dd4b06a8389cf8c0fe22a471207e67ee90cc6a43489a4b302a0a6e6ff95fd  pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt

Tuned diagnostic hashes:
73996cc45cc1897753dfd79385b14d765e8585caefe57df0dea789ff011cfbfa  /tmp/h264-parity-target-20261022/cavlc-emitter-tuned-diagnostic/eligibility.go.txt
c02e6d6b2bdfc0e5554d9733e513b68c5b26a65b1b9ff28e690bfdf87479a86a  /tmp/h264-parity-target-20261022/cavlc-emitter-tuned-diagnostic/lifecycle.go.txt
214dc5efdd7c60bad08e0468f88be2d9b69beb144c32fbb0e1c0401e96e1da2e  /tmp/h264-parity-target-20261022/cavlc-emitter-tuned-diagnostic/prepare.py
