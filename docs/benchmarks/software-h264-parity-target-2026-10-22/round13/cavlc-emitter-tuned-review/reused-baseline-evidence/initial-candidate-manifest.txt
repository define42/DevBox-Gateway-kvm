CAVLC register-state emitter experiment — SOURCE FROZEN, UNMEASURED

Candidate: /tmp/h264-parity-target-20261022/cavlc-emitter-next
Baseline: /tmp/h264-parity-target-20261022/round11-next
Native source/library remain read-only. Main repo unchanged. Author ran no Go formatting/build/test/benchmark/profile, native compile/run, or CPU measurement. Python source generation/copy/hash only. Root owns all validation/measurement; run gofmt before compiling and record post-format hashes.

Production scope (five files):
  set_mb_syn_cavlc.go: after existing extraction, one nonzero-only checked call; success commits original private bs, overflow returns without commit, fallback continues unchanged original Go body. Known-zero direct path and extracted-zero body remain unchanged.
  cavlc_emit.go: status, capacity and actual object/table-size constants.
  cavlc_emit_amd64.go: typed pointer / noescape declaration and contract.
  cavlc_emit_amd64.s: one no-CALL 16-byte NOSPLIT leaf; scalar AMD64 instructions only, no CPU flag assumption, no R14 or BP writes.
  cavlc_emit_other.go: inlineable fallback stub, retaining current scalar writer.

Guard/replay contract:
  Accept only end3/14/15, 1..end+1 coefficients, 0..end+1-total zeros, left1..32, and cursor>=0 with 132 visible bytes remaining. Check output range against private bs/levels/runs, external SBitStringAux, and all seven mutable entropy tables; sizes derive from Go types. No input coefficient disjointness requirement, because extraction already finished exactly once.
  All later state stays local registers; each table entry is read at its original symbol stage. Packed suffix uses &7 exactly as before; full normal signed-int16 escape arithmetic and overflow are implemented. Unsupported mutable lengths>32 or late invalid table index/suffix return fallback before that symbol. Earlier output is bit-identical to original Go replay, while private and external writer/scratch state remains unchanged. The unchanged Go body then preserves panic/partial output timing. Overflow leaves existing emitted prefix without either writer-state commit.
  Accepted symbols flush at most one 4-byte word, as in the Go writer. At most33 symbols =>132 conservative bytes. No unchecked tail stores. Ignored PEndBuf/IBits/IIndex/PStartBuf remain ignored and unchanged.
  Concurrent readers/writers of output or mutable tables remain data races; no new concurrency contract.

Stop gates / uncertainty:
  The eleven overlap checks, ABI call and enlarged Go frame may erase sparse gains or regress zero/dense controls. Reject on repeatable complete-writer/MB control regressions or no broad caller gain. Do not infer frame benefit from a raw leaf result. Historical counts only establish nonzero opportunity (Road70.20%, PDF71.66%); actual entry eligibility must be measured. This is a larger bounded experiment, not a performance claim.

Independent source review:
  parity_structure_review examined ABI/register arithmetic, signed-int16 escape, shifts including left32, table index bounds/strides, live mutable table stages, capacity and identical-prefix replay. Production review clean. Root also reviewed first-pass guards/arithmetic. Tests/diagnostic review pending final source snapshot.

Tests:
  Frozen round11 public block and complete MB writers are test-only oracles (production has one Go fallback body).
  TestCavlcEmitterSignedDomain covers all65536 coefficient bit patterns, first adjusted level and following levels with each normal suffix0..6 selected through live packed-table mutation. Prefix positions cycle1..32.
  TestCavlcEmitterPrefixesContextsAndFailures covers end3/14/15, all normal/invalid nC, property classes including invalid chroma shape, signed/prefix/sparse/dense cases, visible capacity/cursor/malformed pending state.
  Mutable tests exercise every byte value for live nC/token/total-zero/run tables and packed arbitrary length/suffix words, including unsupported slow suffix. Table/output aliases restore snapshots between candidate/reference calls.
  Coefficient/output alias with late invalid run map proves no extraction replay. External/private state aliases, status/commit/overflow, read-only levels/runs and output protected pages cover exact footprints.
  Whole-MB 0/1/2/4/6/10/16/17/26/27-block fixtures cover zero/sparse/dense/mixtures, partial failures including fast-to-fallback transitions at capacity131/132/133/136/160/192, overflow at each of27 ordered blocks, future coefficient/count/writer aliases, unused extractor callback and zero allocation.
  Existing complete writer/reference/flush/packed/zero tests remain present.
  Imported native harness has expanded447 suites; fixture copies and native tools are test-only, never production.

Root validation commands (cwd candidate):
  /snap/go/current/bin/gofmt -w pkg/h264/internal/encoder/cavlc_emit*.go pkg/h264/internal/encoder/cavlc_scratch_bench_test.go pkg/h264/internal/encoder/set_mb_syn_cavlc.go
  CAVLC_NATIVE_BINARY=/tmp/h264-parity-target-20261022/cavlc-native-review/native_cavlc GOMAXPROCS=1 CGO_ENABLED=0 /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestCavlc' -count=1
  GOMAXPROCS=1 CGO_ENABLED=0 /snap/go/current/bin/go test ./pkg/h264/internal/encoder -count=1
  GOMAXPROCS=1 /snap/go/current/bin/go test -race ./pkg/h264/internal/encoder -run '^TestCavlcEmitter' -count=1
  GODEBUG=cpu.all=off GOMAXPROCS=1 CGO_ENABLED=0 /snap/go/current/bin/go test ./pkg/h264/internal/encoder -run '^TestCavlc' -count=1
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 /snap/go/current/bin/go test -c ./pkg/h264/internal/encoder -o /tmp/h264-parity-target-20261022/cavlc-emitter-arm64.test
  (Then usual gopls/vet/link/disassembly and matched ten-frame-stream/native decoder checks if component results justify full validation.)

Baseline-compatible measurement fixture dependencies:
  Copy only cavlc_scratch_bench_test.go, cavlc_native_full_test.go, cavlc_native_full_bench_test.go and testdata/cavlc_native_fixtures.txt unchanged into a fresh baseline copy. The first file is self-contained and reports blocks/op AFTER b.Loop. Existing CavlcLevelWriter/Residual benchmark fixtures already exist in round11.
  Optionally copy cavlc_emit_eligibility_test.go and cavlc_emit_diagnostic_test.go for fixture entry-eligibility counts; these have no new-production dependencies, only the native fixture loader.
  Do not copy new emitter-specific correctness tests into baseline without its production functions.

Suggested root rotating matched baseline/candidate/native6 triples, pinned CPU2:
  GOMAXPROCS=1 taskset -c 2 <baseline-or-candidate.test> -test.run='^$' -test.bench='^BenchmarkCavlcNativeFull$/^end(3|14|15)$/^(zero|known-zero|sparse|dense|mixed)$/^prefix17$' -test.benchtime=300ms -test.count=1
  GOMAXPROCS=1 taskset -c 2 <baseline-or-candidate.test> -test.run='^$' -test.bench='^(BenchmarkCavlcMacroblock|BenchmarkCavlcLevelWriter)$' -test.benchtime=300ms -test.count=1
  Run from source-tree cwd so imported fixture resolves. Root existing matched native driver can reuse the exact expanded447 fixture and native binary. No raw microbenchmark is needed to decide retention.

Actual clip eligibility diagnostic (BASELINE COMPATIBLE, NEVER TIMING):
  Separate helper/templates/generator: /tmp/h264-parity-target-20261022/cavlc-emitter-diagnostic/
  python3 /tmp/h264-parity-target-20261022/cavlc-emitter-diagnostic/prepare.py --source /tmp/h264-parity-target-20261022/round11-next --output /tmp/h264-parity-target-20261022/cavlc-emitter-counts
  Generator clones source, adds only diagnostic helpers and post-extraction/known-zero hooks, plus lifecycle begin/final hooks. Supports candidate too; removes duplicate test mirror in that case. Requires one active encoder, one layer, one encoder thread and one slice. Counters are intentionally non-atomic, and the invocation must be serial (guard is not concurrency protection). Final JSON line H264_CAVLC_EMITTER_ELIGIBILITY has complete cumulative totals including16 warmup frames, then resets. entry_eligible means initial guard, not eventual success/overflow/late mutable-table replay.
  Build comparison overlay mapping /tmp/h264-parity-target-20261022/cavlc-emitter-counts/internal/rdp/avc/z_openh264_comparison_test.go to /tmp/h264-parity-target-20261022/compare_test.go; use normal root fixture build commands.
  From diagnostic cwd:
  CGO_ENABLED=0 GOMAXPROCS=1 /snap/go/current/bin/go test -c -overlay=/tmp/h264-parity-target-20261022/cavlc-emitter-counts-overlay.json -o /tmp/h264-parity-target-20261022/cavlc-emitter-counts.test ./internal/rdp/avc
  H264_CAVLC_EMITTER_DIAGNOSTIC=1 GOMAXPROCS=1 COMPARE_RUN=1 COMPARE_FRAMES=600 COMPARE_SLICES=1 COMPARE_WIDTH=1920 COMPARE_HEIGHT=1080 COMPARE_QP=20 COMPARE_CASE=road1 COMPARE_I420=/tmp/h264-native-parity-20261021/road1080.i420 COMPARE_STREAM=/tmp/h264-parity-target-20261022/cavlc-emitter-counts-road1.264 taskset -c 2 /tmp/h264-parity-target-20261022/cavlc-emitter-counts.test -test.run='^TestOpenH264Comparison$'
  Repeat with COMPARE_CASE=pdf1 and pdf1080.i420. Byte-compare/hash each616-frame stream to uninstrumented round11 under identical fixture configuration. Ignore all diagnostic timings. Final JSON only, no periodic records to sum.

Changed source manifest and SHA256 before root formatting:
d3bac73621847a0f59324f3167e6fcdeddebbbe40c7fd8444301682875681688  pkg/h264/internal/encoder/cavlc_emit.go
6443b7297ebd32509bfd394d1a03250d8d7c51b8c4e1169b3d049f2bf1e42a4d  pkg/h264/internal/encoder/cavlc_emit_amd64.go
5a2c4d1b88db0f3e7ba61f397adc3b62c2a7da298417f28ca79ac037f0c884e6  pkg/h264/internal/encoder/cavlc_emit_amd64.s
1f18269c9a4bf4d7d6398cd225b719cb4267330faa2491f091cb73ff7c85f118  pkg/h264/internal/encoder/cavlc_emit_amd64_test.go
0777336705d8293cd95cd025d548b639fef9f06189126c1d9f705f30a3191920  pkg/h264/internal/encoder/cavlc_emit_diagnostic_test.go
8f0c44ee67fea59fbdcab418d138477bb13dd50f8abe2dd0cdcfa084fc2b7618  pkg/h264/internal/encoder/cavlc_emit_eligibility_test.go
1d2d69bf06f74838de58f4a85485ba6eec2f5dafd54c27683311b2fd5aa73104  pkg/h264/internal/encoder/cavlc_emit_guard_linux_amd64_test.go
ca8accbfd6ad2f1bde5410619f1260106e0798e670f48b2ce6907f34a5c2149d  pkg/h264/internal/encoder/cavlc_emit_mb_test.go
e3f7d61d78873407e5dd74b128ca22d4eb4103ebc2f46cfe0d2cb5dc93c26a78  pkg/h264/internal/encoder/cavlc_emit_other.go
6bc2fd7440970e795566c5edf1d8384e9600f6e2b60164dc051f685e356a4514  pkg/h264/internal/encoder/cavlc_emit_reference_test.go
e91aa828830edffacf07a163dfab7713511d96928b173daea36bfc40302508dc  pkg/h264/internal/encoder/cavlc_emit_test.go
2538502df865e1f56e0e9c652ddf37a4d07606b8aa3fc05afecc15cab0a83893  pkg/h264/internal/encoder/cavlc_native_full_bench_test.go
fee581e7a1cc34d112d0642036991f4aff84df56f9d3591e99864195e6e38fd7  pkg/h264/internal/encoder/cavlc_native_full_test.go
bb04863da491dfd4e713fd47c23924b38a9c20e395c1cbfe73aed16677be893d  pkg/h264/internal/encoder/cavlc_scratch_bench_test.go
9442db4612a5104fadb68e98d083893fb27a72f5f4b70f329639ac8b00e8615a  pkg/h264/internal/encoder/set_mb_syn_cavlc.go
144dd4b06a8389cf8c0fe22a471207e67ee90cc6a43489a4b302a0a6e6ff95fd  pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt

Separate diagnostic hashes:
8f0c44ee67fea59fbdcab418d138477bb13dd50f8abe2dd0cdcfa084fc2b7618  /tmp/h264-parity-target-20261022/cavlc-emitter-diagnostic/eligibility.go.txt
c02e6d6b2bdfc0e5554d9733e513b68c5b26a65b1b9ff28e690bfdf87479a86a  /tmp/h264-parity-target-20261022/cavlc-emitter-diagnostic/lifecycle.go.txt
8427a8e82e1ebf3eac30f6704bc13fd53ca846f2275a390a1746c9e68523b408  /tmp/h264-parity-target-20261022/cavlc-emitter-diagnostic/prepare.py
